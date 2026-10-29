package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func commandFixture(t *testing.T) (string, string, string, CommandManifest) {
	t.Helper()
	root := t.TempDir()
	copy := t.TempDir()
	journal := filepath.Join(t.TempDir(), "journal")
	if e := os.WriteFile(filepath.Join(root, "input.txt"), []byte("original\n"), 0644); e != nil {
		t.Fatal(e)
	}
	base, e := ScanCommandTree(t.Context(), root, copy, DefaultPrivatePaths)
	if e != nil {
		t.Fatal(e)
	}
	return root, copy, journal, base
}
func TestCommandSnapshotPublicationAndWholeInputFreshness(t *testing.T) {
	t.Run("source delta", func(t *testing.T) {
		root, copy, journal, base := commandFixture(t)
		os.WriteFile(filepath.Join(copy, "input.txt"), []byte("changed\n"), 0644)
		os.Mkdir(filepath.Join(copy, "nested"), 0755)
		os.WriteFile(filepath.Join(copy, "nested", "new.bin"), []byte{0, 1, 2}, 0644)
		p, e := PublishCommand(t.Context(), root, copy, journal, base, DefaultPrivatePaths)
		if e != nil || p.Status != "applied" || len(p.Changes) != 2 {
			t.Fatalf("%+v %v", p, e)
		}
		b, _ := os.ReadFile(filepath.Join(root, "input.txt"))
		if string(b) != "changed\n" {
			t.Fatal(string(b))
		}
		b, _ = os.ReadFile(filepath.Join(root, "nested", "new.bin"))
		if len(b) != 3 {
			t.Fatal(b)
		}
		p, e = RecoverCommandJournal(t.Context(), p.JournalPath)
		if e != nil || p.Status != "committed" {
			t.Fatalf("%+v %v", p, e)
		}
	})
	t.Run("unmodified input changed externally", func(t *testing.T) {
		root, copy, journal, base := commandFixture(t)
		os.WriteFile(filepath.Join(copy, "new.txt"), []byte("derived from original"), 0644)
		os.WriteFile(filepath.Join(root, "input.txt"), []byte("external edit"), 0644)
		p, e := PublishCommand(t.Context(), root, copy, journal, base, DefaultPrivatePaths)
		if !errors.Is(e, ErrConflict) || p.Status != "conflict" {
			t.Fatalf("%+v %v", p, e)
		}
		if _, e = os.Stat(filepath.Join(root, "new.txt")); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("conflicting output published")
		}
	})
	t.Run("external new file", func(t *testing.T) {
		root, copy, journal, base := commandFixture(t)
		os.WriteFile(filepath.Join(copy, "input.txt"), []byte("changed"), 0644)
		os.WriteFile(filepath.Join(root, "external.txt"), []byte("external"), 0644)
		if _, e := PublishCommand(t.Context(), root, copy, journal, base, DefaultPrivatePaths); !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	})
}
func TestCommandSnapshotProtectedAndLinks(t *testing.T) {
	root := t.TempDir()
	copy := t.TempDir()
	for _, name := range []string{".env", ".npmrc", ".netrc", ".pypirc", "AGENTS.md"} {
		os.WriteFile(filepath.Join(root, name), []byte("secret"), 0600)
	}
	os.Mkdir(filepath.Join(root, "node_modules"), 0755)
	os.WriteFile(filepath.Join(root, "node_modules", "private"), []byte("dependency"), 0644)
	m, e := ScanCommandTree(t.Context(), root, copy, DefaultPrivatePaths)
	if e != nil || len(m.Files) != 0 || m.Excluded != 6 {
		t.Fatalf("%+v %v", m, e)
	}
	os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(root, "escape"))
	if _, e = ScanCommandTree(t.Context(), root, "", DefaultPrivatePaths); e == nil {
		t.Fatal("symlink accepted")
	}
	os.Remove(filepath.Join(root, "escape"))
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("canary"), 0644)
	os.Link(outside, filepath.Join(root, "linked"))
	if _, e = ScanCommandTree(t.Context(), root, "", DefaultPrivatePaths); e == nil {
		t.Fatal("hardlink accepted")
	}
}
func TestCanonicalOperationLease(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	os.Symlink(root, alias)
	ctx, release, e := AcquireOperation(t.Context(), root)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	_, nested, e := AcquireOperation(ctx, alias)
	if e != nil {
		t.Fatal(e)
	}
	nested()
	wait, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, e = AcquireOperation(wait, alias); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestInterruptedCommandJournalRecoveryPreservesExternalEdits(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "conflict"}[external], func(t *testing.T) {
			root, copy, dir, base := commandFixture(t)
			os.WriteFile(filepath.Join(copy, "input.txt"), []byte("command change"), 0644)
			p, e := PublishCommand(t.Context(), root, copy, dir, base, DefaultPrivatePaths)
			if e != nil {
				t.Fatal(e)
			}
			b, _ := os.ReadFile(p.JournalPath)
			var j commandJournal
			json.Unmarshal(b, &j)
			j.Status = "applying"
			j.Applied = 0
			saveCommandJournal(dir, j)
			if external {
				os.WriteFile(filepath.Join(root, "input.txt"), []byte("editor change"), 0644)
			}
			p, e = RecoverCommandJournal(t.Context(), p.JournalPath)
			b, _ = os.ReadFile(filepath.Join(root, "input.txt"))
			if external {
				if e == nil || string(b) != "editor change" || p.Status != "recovery_required" {
					t.Fatalf("%+v %v %s", p, e, b)
				}
			} else {
				if e != nil || string(b) != "original\n" || p.Status != "rolled_back" {
					t.Fatalf("%+v %v %s", p, e, b)
				}
			}
		})
	}
}

func TestCommandJournalPreservesModeAndRecoversCreatedParents(t *testing.T) {
	root, copy, dir, _ := commandFixture(t)
	os.Chmod(filepath.Join(root, "input.txt"), 0600)
	os.Chmod(filepath.Join(copy, "input.txt"), 0600)
	base, e := ScanCommandTree(t.Context(), root, "", DefaultPrivatePaths)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(copy, "input.txt"), []byte("modified"), 0600)
	os.MkdirAll(filepath.Join(copy, "new", "nested"), 0755)
	os.WriteFile(filepath.Join(copy, "new", "nested", "file.txt"), []byte("new"), 0644)
	p, e := PublishCommand(t.Context(), root, copy, dir, base, DefaultPrivatePaths)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(root, "input.txt"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", info, e)
	}
	b, _ := os.ReadFile(p.JournalPath)
	var j commandJournal
	json.Unmarshal(b, &j)
	j.Status = "applying"
	if e = saveCommandJournal(dir, j); e != nil {
		t.Fatal(e)
	}
	p, e = RecoverCommandJournal(t.Context(), p.JournalPath)
	if e != nil || p.Status != "rolled_back" {
		t.Fatalf("%+v %v", p, e)
	}
	if _, e = os.Stat(filepath.Join(root, "new")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("created parent directories survived rollback")
	}
	info, e = os.Stat(filepath.Join(root, "input.txt"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("recovery lost file permissions")
	}
}
