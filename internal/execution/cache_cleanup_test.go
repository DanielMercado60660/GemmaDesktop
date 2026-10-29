package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/workspace"
)

func cleanupFixture(t *testing.T) (*DockerExecutor, string) {
	t.Helper()
	d, err := NewDockerExecutor(DockerConfig{StateDir: t.TempDir(), Image: "sha256:" + strings.Repeat("a", 64), DockerPath: "/nonexistent/docker"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d, root
}

func TestReleaseWorkspaceCacheOnlyDeletesExactDisposableCache(t *testing.T) {
	d, root := cleanupFixture(t)
	cache := d.dependencyCache(root)
	private := filepath.Join(cache, "environments", "lock", "node_modules", "package")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "data"), []byte("disposable"), 0600); err != nil {
		t.Fatal(err)
	}
	protected := []string{
		filepath.Join(root, "source.py"),
		filepath.Join(d.config.StateDir, "commands", "retained-evidence", "journal.json"),
		filepath.Join(d.config.StateDir, "cache", "another-workspace", "data"),
	}
	for _, p := range protected {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Dir(protected[2]), filepath.Join(cache, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0500); err != nil {
		t.Fatal(err)
	}
	out, err := d.ReleaseWorkspaceCache(t.Context(), root)
	if err != nil || !out.Removed || out.LogicalBytes != int64(len("disposable")) || out.Entries < 5 {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err = os.Lstat(cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cache retained", err)
	}
	for _, p := range protected {
		if b, e := os.ReadFile(p); e != nil || string(b) != "keep" {
			t.Fatalf("changed unrelated data %s: %q %v", p, b, e)
		}
	}
	out, err = d.ReleaseWorkspaceCache(t.Context(), root)
	if err != nil || out.Removed || out.Entries != 0 {
		t.Fatalf("absent cache must be idempotent and need no Docker: %+v %v", out, err)
	}
}

func TestReleaseWorkspaceCacheBlocksRecoveryAndCanonicalActiveWork(t *testing.T) {
	d, root := cleanupFixture(t)
	cache := d.dependencyCache(root)
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	leaseDir := filepath.Join(d.config.StateDir, "commands", "cmd-pending")
	if err := os.MkdirAll(leaseDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveDockerLease(leaseDir, dockerLease{Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReleaseWorkspaceCache(t.Context(), root); err == nil || !strings.Contains(err.Error(), "reconciliation") {
		t.Fatal("pending recovery did not block cleanup", err)
	}
	if err := saveDockerLease(leaseDir, dockerLease{Status: "finished"}); err != nil {
		t.Fatal(err)
	}
	_, release, err := workspace.AcquireOperation(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err = d.ReleaseWorkspaceCache(ctx, alias)
	release()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("canonical workspace lease bypassed", err)
	}
	if _, err = os.Stat(cache); err != nil {
		t.Fatal("blocked cleanup changed cache", err)
	}
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	dockerSlots.Store(d.config.StateDir, slot)
	defer dockerSlots.Delete(d.config.StateDir)
	ctx2, cancel2 := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel2()
	if _, err = d.ReleaseWorkspaceCache(ctx2, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("guest slot bypassed", err)
	}
	<-slot
	if out, err := d.ReleaseWorkspaceCache(t.Context(), root); err != nil || !out.Removed {
		t.Fatal(out, err)
	}
}

func TestReleaseWorkspaceCacheRejectsSymlinkCacheRoot(t *testing.T) {
	for _, level := range []string{"cache", "key"} {
		t.Run(level, func(t *testing.T) {
			d, root := cleanupFixture(t)
			outside := t.TempDir()
			sentinel := filepath.Join(outside, "keep")
			if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(d.config.StateDir, "cache")
			if level == "key" {
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				target = d.dependencyCache(root)
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}
			if _, err := d.ReleaseWorkspaceCache(t.Context(), root); err == nil {
				t.Fatal("accepted cache symlink")
			}
			if b, err := os.ReadFile(sentinel); err != nil || string(b) != "keep" {
				t.Fatal("followed cache symlink", err)
			}
		})
	}
}

func TestReleaseWorkspaceCacheOverdeepChainRequiresMaintenance(t *testing.T) {
	d, root := cleanupFixture(t)
	deep := d.dependencyCache(root)
	for range 129 {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		out, err := d.ReleaseWorkspaceCache(t.Context(), root)
		if err == nil || !strings.Contains(err.Error(), "explicit maintenance required") || strings.Contains(err.Error(), "retry") || out.Removed || out.Entries != 0 {
			t.Fatalf("overdeep chain must not promise retry progress: %+v %v", out, err)
		}
	}
	if _, err := os.Stat(deep); err != nil {
		t.Fatal("overdeep chain changed", err)
	}
}

func TestDockerLiveWorkspaceCacheReleaseStartsCold(t *testing.T) {
	d, root := liveDocker(t)
	root, _ = filepath.EvalSymlinks(root)
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("preserved"), 0600); err != nil {
		t.Fatal(err)
	}
	seed := `from pathlib import Path
p=Path('/cache/fixture-only');p.mkdir();(p/'data').write_bytes(b'x'*16384);p.chmod(0o500)
print('seeded')`
	first, err := d.Run(t.Context(), Request{Workspace: root, Executable: "python3", Args: []string{"-c", seed}, ReadOnly: true})
	if err != nil || first.ExitCode != 0 {
		t.Fatalf("%+v %v", first, err)
	}
	out, err := d.ReleaseWorkspaceCache(t.Context(), root)
	if err != nil || !out.Removed || out.LogicalBytes < 16384 {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(d.config.StateDir, "commands", first.CommandID, "lease.json")); err != nil {
		t.Fatal("command evidence lost", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "source.txt")); err != nil || string(b) != "preserved" {
		t.Fatal("source changed", err)
	}
	second, err := d.Run(t.Context(), Request{Workspace: root, Executable: "python3", Args: []string{"-c", "from pathlib import Path; assert not Path('/cache/fixture-only').exists(); assert Path('source.txt').read_text() == 'preserved'; print('cold')"}, ReadOnly: true})
	if err != nil || second.ExitCode != 0 || !strings.Contains(second.Stdout, "cold") {
		t.Fatalf("%+v %v", second, err)
	}
	t.Logf("released logicalBytes=%d entries=%d; next command observed cold cache", out.LogicalBytes, out.Entries)
}
