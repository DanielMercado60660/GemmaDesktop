package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDockerRequiresPinnedLocalImage(t *testing.T) {
	for _, image := range []string{"node:22", "--privileged", "", "node@sha256:bad"} {
		if _, e := NewDockerExecutor(DockerConfig{StateDir: t.TempDir(), Image: image}); e == nil {
			t.Fatalf("accepted %q", image)
		}
	}
}

func liveDocker(t *testing.T) (*DockerExecutor, string) {
	t.Helper()
	image := os.Getenv("GEMMA_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("opt-in: GEMMA_TEST_DOCKER_IMAGE must name installed immutable linux/arm64 toolchain image")
	}
	d, e := NewDockerExecutor(DockerConfig{StateDir: t.TempDir(), Image: image})
	if e != nil {
		t.Fatal(e)
	}
	status, e := d.Status(t.Context())
	if e != nil {
		t.Fatalf("Docker qualification unavailable: %+v %v", status, e)
	}
	t.Logf("runtime=%s image=%s imageID=%s", status.Version, status.Image, status.ImageID)
	return d, t.TempDir()
}
func TestDockerLiveProcessTreeAndWriteback(t *testing.T) {
	d, root := liveDocker(t)
	os.WriteFile(filepath.Join(root, "input.txt"), []byte("live edit"), 0644)
	os.WriteFile(filepath.Join(root, ".env"), []byte("secret"), 0600)
	script := `import os, subprocess, pathlib
assert pathlib.Path('input.txt').read_text() == 'live edit'
assert not pathlib.Path('.env').exists()
assert not pathlib.Path('/var/run/docker.sock').exists()
assert not pathlib.Path('/Users').exists()
assert len(pathlib.Path('/proc/net/route').read_text().strip().splitlines()) == 1
subprocess.run(['python3', '-c', "open('output.txt','w').write('child process output')"], check=True)
print('subprocess verified')
`
	r, e := d.Run(t.Context(), Request{Workspace: root, Executable: "python3", Args: []string{"-c", script}})
	if e != nil || r.ExitCode != 0 || r.Publication != "applied" {
		t.Fatalf("%+v %v", r, e)
	}
	b, e := os.ReadFile(filepath.Join(root, "output.txt"))
	if e != nil || string(b) != "child process output" {
		t.Fatalf("%s %v", b, e)
	}
	r, e = d.Run(t.Context(), Request{Workspace: root, Executable: "python3", Args: []string{"-c", "assert open('output.txt').read() == 'child process output'; print('fresh')"}, ReadOnly: true})
	if e != nil || r.ExitCode != 0 || r.Publication != "read_only" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = d.Reconcile(t.Context()); e != nil {
		t.Fatal(e)
	}
}
func TestDockerLiveReadOnlyAndBounds(t *testing.T) {
	d, root := liveDocker(t)
	os.WriteFile(filepath.Join(root, "input.txt"), []byte("original"), 0644)
	script := `import pathlib, tempfile
try:
 pathlib.Path('input.txt').write_text('bad')
 raise AssertionError('source writable')
except OSError: pass
with tempfile.TemporaryDirectory() as d: pathlib.Path(d,'ok').write_text('tmp')
print('A'*20000)
print('TAIL')`
	r, e := d.Run(t.Context(), Request{Workspace: root, Executable: "python3", Args: []string{"-c", script}, ReadOnly: true, MaxOutput: 1024})
	if e != nil || r.ExitCode != 0 || !r.Truncated || !strings.Contains(r.Stdout, "TAIL") {
		t.Fatalf("%+v %v", r, e)
	}
	b, _ := os.ReadFile(filepath.Join(root, "input.txt"))
	if string(b) != "original" {
		t.Fatal(string(b))
	}
}
func TestDockerLiveCancellationKillsEscapedChildren(t *testing.T) {
	d, root := liveDocker(t)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	script := `import os,time
open('unpublished.txt','w').write('must not publish')
if os.fork()==0:
 os.setsid()
 while True: time.sleep(1)
while True: time.sleep(1)`
	r, e := d.Run(ctx, Request{Workspace: root, Executable: "python3", Args: []string{"-c", script}, Timeout: time.Minute})
	if !errors.Is(e, context.DeadlineExceeded) || !r.Cancelled {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = os.Stat(filepath.Join(root, "unpublished.txt")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("cancelled source published")
	}
	ids, e := d.cli(t.Context(), 4096, "ps", "-aq", "--filter", "label=com.gemmadesktop.owner="+d.owner)
	if e != nil || strings.TrimSpace(ids) != "" {
		t.Fatalf("owned container survived: %q %v", ids, e)
	}
}

func TestDockerLiveLockChangeIsolatesDependencies(t *testing.T) {
	d, root := liveDocker(t)
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"first","version":"1.0.0"}`), 0644)
	first, e := d.Run(t.Context(), Request{Workspace: root, ReadOnly: true, Executable: "node", Args: []string{"-e", "require('fs').writeFileSync('node_modules/previous-lock-marker','old dependency')"}})
	if e != nil || first.ExitCode != 0 {
		t.Fatalf("%+v %v", first, e)
	}
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"second","version":"2.0.0"}`), 0644)
	second, e := d.Run(t.Context(), Request{Workspace: root, ReadOnly: true, Executable: "node", Args: []string{"-e", "if(require('fs').existsSync('node_modules/previous-lock-marker')) process.exit(1); console.log('new dependency environment')"}})
	if e != nil || second.ExitCode != 0 || first.DependencyDigest == second.DependencyDigest {
		t.Fatalf("first=%+v second=%+v %v", first, second, e)
	}
	if _, e = os.Stat(filepath.Join(root, "node_modules")); !os.IsNotExist(e) {
		t.Fatal("private dependency directory was published")
	}
}
