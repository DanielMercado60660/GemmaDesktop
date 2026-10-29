package execution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/workspace"
)

func TestDockerFailedCleanupBlocksRunAndProvisionUntilReconciled(t *testing.T) {
	state := t.TempDir()
	binary := filepath.Join(t.TempDir(), "docker-fixture")
	d, e := NewDockerExecutor(DockerConfig{StateDir: state, Image: "sha256:" + strings.Repeat("a", 64), DockerPath: binary})
	if e != nil {
		t.Fatal(e)
	}
	// The fixture models a daemon that completed execution but refuses removal.
	script := `#!/bin/sh
shift 2
case "$1" in
version) echo 29.4.0;;
image) echo '{"Id":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","Os":"linux","Architecture":"arm64"}';;
create) echo created;;
start|stop) exit 0;;
inspect) case "$3" in *Labels*) echo OWNER;; *) echo '{"Running":false,"ExitCode":0}';; esac;;
rm) test -e "$HOME/allow-remove";;
ps) exit 0;;
*) exit 1;;
esac
`
	script = strings.ReplaceAll(script, "OWNER", d.owner)
	if e = os.WriteFile(binary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"test","version":"1.0.0"}`), 0644)
	os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3,"packages":{}}`), 0644)
	r, e := d.Run(t.Context(), Request{Workspace: root, Executable: "true"})
	if e == nil || r.Publication != "recovery_required" {
		t.Fatalf("%+v %v", r, e)
	}
	if status, err := d.RecoveryStatus(); status != "blocked" || err == nil {
		t.Fatal("failed cleanup was not visible to status", status, err)
	}
	r, e = d.Run(t.Context(), Request{Workspace: root, Executable: "true"})
	if e == nil || r.CommandID != "" || !strings.Contains(e.Error(), "reconciliation") {
		t.Fatalf("second command admitted: %+v %v", r, e)
	}
	for _, pm := range []string{"npm", "pip", "go"} {
		called := false
		_, e = d.ProvisionDependencies(t.Context(), DependencyRequest{Workspace: root, PackageManager: pm}, func(context.Context, Approval) error { called = true; return nil })
		if e == nil || called || !strings.Contains(e.Error(), "reconciliation") {
			t.Fatalf("%s admitted after cleanup failure: %v approval=%v", pm, e, called)
		}
	}
	os.WriteFile(filepath.Join(d.config.StateDir, "allow-remove"), nil, 0600)
	if _, e = d.Reconcile(t.Context()); e != nil {
		t.Fatal(e)
	}
	if status, err := d.RecoveryStatus(); status != "ready" || err != nil {
		t.Fatal("successful recovery remained blocked", status, err)
	}
	r, e = d.Run(t.Context(), Request{Workspace: root, Executable: "true"})
	if e != nil || r.Publication != "applied" {
		t.Fatalf("did not recover: %+v %v", r, e)
	}
}

func TestDockerRecoveryStatusDoesNotLabelOwnedActiveWorkAbandoned(t *testing.T) {
	d, err := NewDockerExecutor(DockerConfig{StateDir: t.TempDir(), Image: "sha256:" + strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	slot := make(chan struct{}, 1)
	dockerSlots.Store(d.config.StateDir, slot)
	t.Cleanup(func() { dockerSlots.Delete(d.config.StateDir) })
	slot <- struct{}{}
	dir := filepath.Join(d.config.StateDir, "commands", "cmd-active")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = saveDockerLease(dir, dockerLease{Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if state, err := d.RecoveryStatus(); state != "active" || err != nil {
		t.Fatal("owned running lease reported abandoned", state, err)
	}
	<-slot
	if state, err := d.RecoveryStatus(); state != "blocked" || err == nil {
		t.Fatal("abandoned lease not reported", state, err)
	}
}

func TestDockerPrivatePathsRejectMountFieldInjection(t *testing.T) {
	for _, p := range []string{"build,readonly", "build\nnew", "build\rnew", "build\"new"} {
		if _, e := NewDockerExecutor(DockerConfig{StateDir: t.TempDir(), Image: "sha256:" + strings.Repeat("a", 64), PrivatePaths: []string{p}}); e == nil {
			t.Errorf("accepted %q", p)
		}
	}
}

func TestDockerLiveResourcesAndExternalEditConflict(t *testing.T) {
	d, root := liveDocker(t)
	root, _ = filepath.EvalSymlinks(root)
	os.WriteFile(filepath.Join(root, "input.txt"), []byte("original"), 0600)
	script := `import os,pathlib,resource,socket,time
assert os.getuid()!=0
assert pathlib.Path('/sys/fs/cgroup/pids.max').read_text().strip()=='256'
assert pathlib.Path('/sys/fs/cgroup/memory.max').read_text().strip()=='8589934592'
assert pathlib.Path('/sys/fs/cgroup/cpu.max').read_text().split()==['400000','100000']
assert resource.getrlimit(resource.RLIMIT_FSIZE)==(67108864,67108864)
status=pathlib.Path('/proc/self/status').read_text()
assert 'NoNewPrivs:\t1' in status and 'CapEff:\t0000000000000000' in status
for address in [('1.1.1.1',80),('127.0.0.1',11434)]:
 try:
  connection=socket.create_connection(address,timeout=.2)
 except OSError: pass
 else: connection.close(); raise AssertionError('unexpected network access')
pathlib.Path('/cache/ready').write_text('ready')
time.sleep(1)
pathlib.Path('derived.txt').write_text('derived from original')
`
	type outcome struct {
		r Result
		e error
	}
	done := make(chan outcome, 1)
	go func() {
		r, e := d.Run(t.Context(), Request{Workspace: root, Executable: "python3", Args: []string{"-c", script}})
		done <- outcome{r, e}
	}()
	ready := filepath.Join(d.dependencyCache(root), "ready")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, e := os.Stat(ready); e == nil {
			break
		}
		select {
		case out := <-done:
			t.Fatalf("before ready: %+v %v", out.r, out.e)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("container not ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	os.WriteFile(filepath.Join(root, "input.txt"), []byte("external edit"), 0600)
	out := <-done
	if out.e == nil || out.r.Publication != "conflict" || !out.r.WorkspaceChanged {
		t.Fatalf("%+v %v", out.r, out.e)
	}
	if _, e := os.Stat(filepath.Join(root, "derived.txt")); !os.IsNotExist(e) {
		t.Fatal("conflicting derived source published")
	}
}

func TestDockerLiveReconcileOrphansPreservesForeignContainer(t *testing.T) {
	d, root := liveDocker(t)
	suffix := workspace.Hash([]byte(root))[:12]
	foreign := "gemma-test-foreign-" + suffix
	owned := "gemma-" + d.owner[:12] + "-orphan"
	create := func(name, owner string) {
		t.Helper()
		_, e := d.cli(t.Context(), 4096, "create", "--name", name, "--label", "com.gemmadesktop.owner="+owner, "--pull=never", "--network=none", "--pids-limit=16", "--memory=64m", "--cpus=1", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--read-only", "--entrypoint", "/bin/sh", d.config.Image, "-c", "sleep 60")
		if e != nil {
			t.Fatal(e)
		}
	}
	create(foreign, "unrelated-fixture")
	defer d.cli(context.Background(), 4096, "rm", "--force", foreign)
	create(owned, d.owner)
	defer d.removeOwned(context.Background(), owned)
	if _, e := d.cli(t.Context(), 4096, "start", owned); e != nil {
		t.Fatal(e)
	}
	// No lease simulates a crash around persisted container metadata.
	if _, e := d.Reconcile(t.Context()); e != nil {
		t.Fatal(e)
	}
	if _, e := d.cli(t.Context(), 4096, "inspect", owned); e == nil {
		t.Fatal("owned orphan survived")
	}
	if _, e := d.cli(t.Context(), 4096, "inspect", "--format", "{{.Id}}", foreign); e != nil {
		t.Fatalf("foreign container touched: %v", e)
	}
	chains, e := InstalledToolchains(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(chains)
	t.Logf("installed toolchain discovery: %s", b)
}
