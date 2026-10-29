package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDependencyLocksRejectUnboundedOrExecutableSources(t *testing.T) {
	for _, s := range []string{"package @ https://private.local/a.whl --hash=sha256:" + strings.Repeat("a", 64), "--extra-index-url https://evil.test", "package==1.0", "-e .", "pkg==1.0 --hash=md5:abcd"} {
		if _, e := parseWheelLock(s); e == nil {
			t.Errorf("accepted wheel source %q", s)
		}
	}
	if p, e := parseWheelLock("six==1.17.0 \\\n --hash=sha256:" + strings.Repeat("a", 64)); e != nil || len(p) != 1 {
		t.Fatalf("%+v %v", p, e)
	}
	for _, resolved := range []string{"https://evil.test/x.tgz", "http://registry.npmjs.org/x.tgz", "https://registry.npmjs.org/x.tgz?token=abc", "file:../private"} {
		b, _ := json.Marshal(map[string]any{"lockfileVersion": 3, "packages": map[string]any{"node_modules/pkg": map[string]any{"resolved": resolved, "integrity": "sha512-" + strings.Repeat("A", 86) + "=="}}})
		if _, e := parseNPMLock(b); e == nil {
			t.Errorf("accepted npm source %s", resolved)
		}
	}
	for _, s := range []string{"../../private v1.0.0 h1:abc", "module v1.0.0 h1:abc", "module version h1:abc"} {
		if _, e := parseGoSum(s); e == nil {
			t.Errorf("accepted Go lock %q", s)
		}
	}
}
func TestDependencyDenialDoesNotFetchOrCreateCache(t *testing.T) {
	state := t.TempDir()
	d, e := NewDockerExecutor(DockerConfig{StateDir: state, Image: "sha256:" + strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"test","version":"1.0.0"}`), 0644)
	os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3,"packages":{}}`), 0644)
	called := false
	_, e = d.ProvisionDependencies(t.Context(), DependencyRequest{Workspace: root, PackageManager: "npm"}, func(context.Context, Approval) error { called = true; return ErrDenied })
	if !errors.Is(e, ErrDenied) || !called {
		t.Fatalf("%v %v", called, e)
	}
	if _, e = os.Stat(filepath.Join(state, "cache")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("denied setup created cache")
	}
}
func TestPrivateCacheRejectsGuestSymlink(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, "planted"))
	if e := ensureExecutionDir(filepath.Join(root, "planted", "escape")); e == nil {
		t.Fatal("cache symlink accepted")
	}
	if _, e := os.Stat(filepath.Join(outside, "escape")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("host directory escaped")
	}
}

func TestDockerLiveDependencyProvisioning(t *testing.T) {
	if os.Getenv("GEMMA_TEST_DEPENDENCIES") != "1" {
		t.Skip("opt-in public dependency downloads: GEMMA_TEST_DEPENDENCIES=1")
	}
	approve := func(context.Context, Approval) error { return nil }
	t.Run("npm", func(t *testing.T) {
		d, root := liveDocker(t)
		b, e := boundedPublicFetch(t.Context(), "https://registry.npmjs.org/is-number/7.0.0", "registry.npmjs.org", 1<<20)
		if e != nil {
			t.Fatal(e)
		}
		var metadata struct {
			Dist struct {
				Tarball   string `json:"tarball"`
				Integrity string `json:"integrity"`
			} `json:"dist"`
		}
		json.Unmarshal(b, &metadata)
		os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"qualification","version":"1.0.0","dependencies":{"is-number":"7.0.0"},"scripts":{"postinstall":"touch forbidden-lifecycle"}}`), 0644)
		lock, _ := json.Marshal(map[string]any{"name": "qualification", "version": "1.0.0", "lockfileVersion": 3, "packages": map[string]any{"": map[string]any{"name": "qualification", "version": "1.0.0", "dependencies": map[string]string{"is-number": "7.0.0"}}, "node_modules/is-number": map[string]any{"version": "7.0.0", "resolved": metadata.Dist.Tarball, "integrity": metadata.Dist.Integrity}}})
		os.WriteFile(filepath.Join(root, "package-lock.json"), lock, 0644)
		r, e := d.ProvisionDependencies(t.Context(), DependencyRequest{Workspace: root, PackageManager: "npm"}, approve)
		if e != nil || r.Command.ExitCode != 0 {
			t.Fatalf("%+v %v", r, e)
		}
		t.Logf("npm packages=%d downloaded=%d lock=%s", r.PackageCount, r.DownloadedBytes, r.LockHash)
		if _, e = os.Stat(filepath.Join(root, "node_modules")); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("private dependency leaked to source")
		}
		if _, e = os.Stat(filepath.Join(root, "forbidden-lifecycle")); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("lifecycle executed")
		}
		result, e := d.Run(t.Context(), Request{Workspace: root, ReadOnly: true, Executable: "node", Args: []string{"-e", "if (!require('is-number')(42)) process.exit(1); console.log('npm verified')"}})
		if e != nil || result.ExitCode != 0 {
			t.Fatalf("%+v %v", result, e)
		}
	})
	t.Run("pip", func(t *testing.T) {
		d, root := liveDocker(t)
		b, e := boundedPublicFetch(t.Context(), "https://pypi.org/pypi/six/1.17.0/json", "pypi.org", 1<<20)
		if e != nil {
			t.Fatal(e)
		}
		var m struct {
			URLs []struct {
				Filename string            `json:"filename"`
				Digests  map[string]string `json:"digests"`
			} `json:"urls"`
		}
		json.Unmarshal(b, &m)
		hash := ""
		for _, f := range m.URLs {
			if strings.HasSuffix(f.Filename, ".whl") {
				hash = f.Digests["sha256"]
				break
			}
		}
		if hash == "" {
			t.Fatal("fixture wheel missing")
		}
		os.WriteFile(filepath.Join(root, "requirements.lock"), []byte("six==1.17.0 --hash=sha256:"+hash+"\n"), 0644)
		r, e := d.ProvisionDependencies(t.Context(), DependencyRequest{Workspace: root, PackageManager: "pip"}, approve)
		if e != nil || r.Command.ExitCode != 0 {
			t.Fatalf("%+v %v", r, e)
		}
		t.Logf("pip packages=%d downloaded=%d lock=%s", r.PackageCount, r.DownloadedBytes, r.LockHash)
		result, e := d.Run(t.Context(), Request{Workspace: root, ReadOnly: true, Executable: "python3", Args: []string{"-c", "import six; assert six.__version__ == '1.17.0'; print('wheel verified')"}})
		if e != nil || result.ExitCode != 0 {
			t.Fatalf("%+v %v", result, e)
		}
	})
	t.Run("go", func(t *testing.T) {
		d, root := liveDocker(t)
		os.WriteFile(filepath.Join(root, "go.mod"), []byte("module qualification\n\ngo 1.26.0\n\nrequire github.com/google/uuid v1.6.0\n"), 0644)
		os.WriteFile(filepath.Join(root, "go.sum"), []byte("github.com/google/uuid v1.6.0 h1:NIvaJDMOsjHA8n1jAhLSgzrAzy1Hgr+hNrb57e+94F0=\ngithub.com/google/uuid v1.6.0/go.mod h1:TIyPZe4MgqvfeYDBFedMoGGpEw/LqOeaOT+nhxU+yHo=\n"), 0644)
		os.WriteFile(filepath.Join(root, "uuid_test.go"), []byte("package qualification\nimport (\"testing\"; \"github.com/google/uuid\")\nfunc TestUUID(t *testing.T){if uuid.Nil.String()!=\"00000000-0000-0000-0000-000000000000\" {t.Fatal(\"bad uuid\")}}\n"), 0644)
		r, e := d.ProvisionDependencies(t.Context(), DependencyRequest{Workspace: root, PackageManager: "go"}, approve)
		if e != nil || r.Command.ExitCode != 0 {
			t.Fatalf("%+v %v", r, e)
		}
		t.Logf("go modules=%d downloaded=%d lock=%s", r.PackageCount, r.DownloadedBytes, r.LockHash)
		result, e := d.Run(t.Context(), Request{Workspace: root, ReadOnly: true, Executable: "go", Args: []string{"test", "./..."}})
		if e != nil || result.ExitCode != 0 {
			t.Fatalf("%+v %v", result, e)
		}
	})
}
