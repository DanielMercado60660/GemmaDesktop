package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTest(t *testing.T) (*Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w, dir
}
func TestPathBoundary(t *testing.T) {
	w, dir := openTest(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600)
	os.Symlink(outside, filepath.Join(dir, "link"))
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "filelink"))
	for _, p := range []string{"../secret", "a/../../secret", filepath.Join(outside, "secret"), "link/secret", "filelink", ".git/config", ".env", ".env.production", ".ssh/id_rsa"} {
		if _, err := w.Read(p); err == nil {
			t.Errorf("read accepted %q", p)
		}
		if _, err := w.Write(p, "changed", "missing"); err == nil {
			t.Errorf("write accepted %q", p)
		}
	}
	data, _ := os.ReadFile(filepath.Join(outside, "secret"))
	if string(data) != "outside" {
		t.Fatal("outside file changed")
	}
	if _, err := w.Write(".env.example", "DEMO=true", "missing"); err != nil {
		t.Fatal(err)
	}
}
func TestHashAndAtomicCreate(t *testing.T) {
	w, _ := openTest(t)
	a, err := w.Write("file.txt", "first", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write("file.txt", "bad", "missing"); !errors.Is(err, ErrConflict) {
		t.Fatalf("create existing: %v", err)
	}
	if _, err = w.Write("file.txt", "bad", "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	b, err := w.Replace("file.txt", "first", "second", a.AfterHash)
	if err != nil || b.Before != "first" || b.After != "second" {
		t.Fatalf("replace: %+v %v", b, err)
	}
	if _, err = w.Replace("file.txt", "absent", "bad", b.AfterHash); err == nil {
		t.Fatal("ambiguous replacement accepted")
	}
}
func TestHardLinksAndBinary(t *testing.T) {
	w, dir := openTest(t)
	os.WriteFile(filepath.Join(dir, "original"), []byte("text"), 0600)
	os.Link(filepath.Join(dir, "original"), filepath.Join(dir, "linked"))
	if _, err := w.Read("linked"); err == nil {
		t.Fatal("hard link accepted")
	}
	os.WriteFile(filepath.Join(dir, "binary"), []byte{0, 1, 2}, 0600)
	if _, err := w.Read("binary"); err == nil {
		t.Fatal("binary accepted")
	}
}
func TestSearchLimitCancelAndProtectedPaths(t *testing.T) {
	w, dir := openTest(t)
	os.WriteFile(filepath.Join(dir, "text"), []byte(strings.Repeat("needle\n", 10)), 0600)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("needle"), 0600)
	r, err := w.Search(context.Background(), "", "needle", false, 2)
	if err != nil || len(r.Matches) != 2 || !r.Truncated {
		t.Fatalf("search: %+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = w.Search(ctx, "", "needle", false, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
func TestMoveDeleteRevisionAndDestination(t *testing.T) {
	w, dir := openTest(t)
	a, _ := w.Write("a", "saved", "missing")
	w.Write("exists", "keep", "missing")
	if _, err := w.Move("a", "exists", a.AfterHash); err == nil {
		t.Fatal("overwrote destination")
	}
	if _, err := w.Delete("a", "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete stale: %v", err)
	}
	if err := w.Mkdir("nested"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Move("a", "nested/b", a.AfterHash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source remains")
	}
	d, err := w.Delete("nested/b", a.AfterHash)
	if err != nil || d.Before != "saved" {
		t.Fatalf("delete: %+v %v", d, err)
	}
}

func TestDirectoryBoundaryAndPythonSearch(t *testing.T) {
	w, root := openTest(t)
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "package", "tests"), 0700)
	os.Symlink(outside, filepath.Join(root, "escape"))
	os.Mkdir(filepath.Join(root, ".aws"), 0700)
	for _, p := range []string{"../outside", outside, "escape", "escape/nested", ".aws"} {
		if _, err := w.DirectoryPath(p); err == nil {
			t.Errorf("working directory accepted %q", p)
		}
	}
	got, err := w.DirectoryPath("package/tests")
	if err != nil || got != filepath.Join(w.Path, "package", "tests") {
		t.Fatalf("working directory %q %v", got, err)
	}
	os.MkdirAll(filepath.Join(root, ".venv", "lib"), 0700)
	os.WriteFile(filepath.Join(root, ".venv", "lib", "dependency.py"), []byte("needle"), 0600)
	os.WriteFile(filepath.Join(root, "package", "store.py"), []byte("needle"), 0600)
	r, err := w.Search(context.Background(), ".", "needle", false, 10)
	if err != nil || len(r.Matches) != 1 || r.Matches[0].Path != "package/store.py" {
		t.Fatalf("dependency search noise: %+v %v", r, err)
	}
}

func TestReplaceMismatchExplainsRecoveryWithoutUnescaping(t *testing.T) {
	w, _ := openTest(t)
	content := "first\nsecond\nrepeat repeat\n"
	f, err := w.Write("source.py", content, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Replace("source.py", `first\nsecond`, "changed", f.AfterHash); err == nil || !strings.Contains(err.Error(), "was not found") || !strings.Contains(err.Error(), "JSON escape") {
		t.Fatalf("missing match diagnostic: %v", err)
	}
	if _, err = w.Replace("source.py", "repeat", "changed", f.AfterHash); err == nil || !strings.Contains(err.Error(), "matched 2 times") || !strings.Contains(err.Error(), "surrounding text") {
		t.Fatalf("ambiguous match diagnostic: %v", err)
	}
	got, err := w.Read("source.py")
	if err != nil || got.Content != content || got.Hash != f.AfterHash {
		t.Fatal("mismatch mutated the file")
	}
}
