package app

import (
	"context"
	"gemmadesktop.local/app/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApprovalIsDurableAndSingleUse(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := &Core{Store: s, approvals: map[string]chan bool{}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.approve(ctx, "", "file.write", Doc{"path": "example.txt"}, "Write proposed file") }()
	var a Doc
	for a == nil {
		all, e := s.List("approval")
		if e != nil {
			t.Fatal(e)
		}
		if len(all) > 0 {
			a = all[0]
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("approval not recorded")
		case <-time.After(time.Millisecond):
		}
	}
	if a["status"] != "pending" {
		t.Fatal(a)
	}
	if _, e = c.respondApproval(str(a, "id"), true); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if _, e = c.respondApproval(str(a, "id"), true); e == nil {
		t.Fatal("approval replay accepted")
	}
	a, _ = s.Get("approval", str(a, "id"))
	if a["status"] != "approved" {
		t.Fatal(a)
	}
}
func TestFrozenEvaluationDoesNotIncludeSecretsOrFollowLinks(t *testing.T) {
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "main.go"), []byte("package main"), 0600)
	os.WriteFile(filepath.Join(source, ".env"), []byte("SECRET=do-not-copy"), 0600)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("private"), 0600)
	os.Symlink(outside, filepath.Join(source, "link"))
	dest := filepath.Join(t.TempDir(), "frozen")
	digest, count, e := freezeWorkspace(source, dest)
	if e != nil {
		t.Fatal(e)
	}
	if count != 1 || len(digest) != 64 {
		t.Fatal(count, digest)
	}
	for _, name := range []string{".env", "link"} {
		if _, e = os.Stat(filepath.Join(dest, name)); !os.IsNotExist(e) {
			t.Fatal("copied", name)
		}
	}
	os.WriteFile(filepath.Join(source, "main.go"), []byte("changed"), 0600)
	b, _ := os.ReadFile(filepath.Join(dest, "main.go"))
	if string(b) != "package main" {
		t.Fatal("snapshot mutated")
	}
}

func TestSkillImportValidatesBeforePersisting(t *testing.T) {
	dir := t.TempDir()
	s, e := store.Open(filepath.Join(dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := &Core{Store: s, DataDir: dir}
	src := filepath.Join(t.TempDir(), "SKILL.md")
	os.WriteFile(src, []byte("---\nname: broken\ndescription: [\n---\n"), 0600)
	if _, e = c.importSkill(Doc{"path": src}); e == nil {
		t.Fatal("malformed YAML accepted")
	}
	all, _ := s.List("skill")
	if len(all) != 0 {
		t.Fatal("bad import persisted")
	}
	os.WriteFile(src, []byte("---\nname: explain-project\ndescription: Explain project files from evidence.\n---\nRead the source before explaining it.\n"), 0600)
	if _, e = c.importSkill(Doc{"path": src}); e != nil {
		t.Fatal(e)
	}
	if _, e = c.importSkill(Doc{"path": src}); e == nil {
		t.Fatal("duplicate skill accepted")
	}
}
