package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedPublicationRejectsAllChangesBeforeApplying(t *testing.T) {
	root, snapshot, journal, base := commandFixture(t)
	if err := os.WriteFile(filepath.Join(snapshot, "input.txt"), []byte("rewritten protected input"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "legitimate.txt"), []byte("repair"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := PublishCommand(t.Context(), root, snapshot, journal, base, DefaultPrivatePaths, []string{"input.txt"})
	if err == nil || r.Status != "discarded_task_protection" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = os.Stat(filepath.Join(root, "legitimate.txt")); !os.IsNotExist(err) {
		t.Fatal("partial publication despite protected change")
	}
	b, err := os.ReadFile(filepath.Join(root, "input.txt"))
	if err != nil || string(b) != "original\n" {
		t.Fatalf("protected original changed: %q %v", b, err)
	}
}

func TestProtectedPathsIncludeCaseAndAncestorMutations(t *testing.T) {
	for _, p := range []string{"tests", "tests/public.py", "TESTS/PUBLIC.PY"} {
		if !TaskProtected(p, []string{"tests/public.py"}) {
			t.Fatal(p)
		}
	}
	if TaskProtected("tests/new.py", []string{"tests/public.py"}) {
		t.Fatal("new test blocked")
	}
	for _, paths := range [][]string{{"../escape"}, {"/absolute"}, {"."}} {
		if ValidateProtectedPaths(paths) == nil {
			t.Fatal(paths)
		}
	}
}
