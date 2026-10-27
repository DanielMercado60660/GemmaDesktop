package engine

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/adk/v2/tool/skilltoolset/skill"
)

func TestSkillsAreBoundedSnapshotsWithoutFilesystemAuthority(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(file, []byte("---\nname: local-guide\ndescription: A test skill.\n---\nRead the reference."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "references"), 0700); err != nil {
		t.Fatal(err)
	}
	resource := filepath.Join(dir, "references", "guide.txt")
	if err := os.WriteFile(resource, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := loadSkills([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resource, []byte("changed after load"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := source.LoadResource(t.Context(), "local-guide", "references/guide.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != "original" {
		t.Fatalf("not isolated: %q %v", data, err)
	}
	if _, err := source.LoadResource(t.Context(), "local-guide", "../secret"); !errors.Is(err, skill.ErrInvalidResourcePath) {
		t.Fatalf("resource traversal: %v", err)
	}
	if err := os.Symlink(file, filepath.Join(dir, "references", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSkills([]string{file}); err == nil {
		t.Fatal("symlink resource was accepted")
	}
}
