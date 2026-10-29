package app

import (
	"database/sql"
	"gemmadesktop.local/app/internal/engine"
	"os"
	"path/filepath"
	"testing"
)

func recoverySkill(t *testing.T) (string, engine.SkillPackage) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: recovery\ndescription: immutable recovery fixture\n---\nUse the exact resource."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "data.bin"), []byte{0, 255, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := engine.SnapshotSkill(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return dir, p
}

func TestSkillImportResumesAfterPublicationAndDatabaseFailure(t *testing.T) {
	c := milestoneCore(t)
	source, pack := recoverySkill(t)
	db, err := sql.Open("sqlite", c.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER fail_skill_insert BEFORE INSERT ON entities WHEN NEW.kind='skill' BEGIN SELECT RAISE(ABORT,'fixture failure after file publication'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = c.importSkill(Doc{"path": source}); err == nil {
		t.Fatal("database failure was hidden")
	}
	if err = verifySkillDirectory(filepath.Join(c.DataDir, "skills", pack.Digest), pack.Digest); err != nil {
		t.Fatal("fixture did not reach directory publication", err)
	}
	if _, err = c.Store.Get("skill", pack.ID); err == nil {
		t.Fatal("failed publication has a skill record")
	}
	if _, err = db.Exec(`DROP TRIGGER fail_skill_insert`); err != nil {
		t.Fatal(err)
	}
	imported, err := c.importSkill(Doc{"path": source})
	if err != nil || str(obj(imported), "digest") != pack.Digest {
		t.Fatal("same-version retry failed", imported, err)
	}
	if _, err = c.archiveSkill(pack.ID); err != nil {
		t.Fatal(err)
	}
	resourceID := skillResourceID(pack.Digest, "assets/data.bin")
	if err = c.Store.Delete("artifact", resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = c.importSkill(Doc{"path": source}); err != nil {
		t.Fatal("unarchive failed", err)
	}
	if _, err = c.Store.Get("artifact", resourceID); err != nil {
		t.Fatal("unarchive did not repair resource registration", err)
	}
}

func TestSkillImportRejectsConflictingUnrecordedDirectory(t *testing.T) {
	c := milestoneCore(t)
	source, pack := recoverySkill(t)
	dir := filepath.Join(c.DataDir, "skills", pack.Digest)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	conflict := []byte("---\nname: conflict\ndescription: must not be replaced\n---\nKeep these bytes.")
	if err := os.WriteFile(path, conflict, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.importSkill(Doc{"path": source}); err == nil {
		t.Fatal("conflicting directory was reused")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(conflict) {
		t.Fatal("conflicting bytes were overwritten", err)
	}
	if _, err = c.Store.Get("skill", pack.ID); err == nil {
		t.Fatal("conflicting version was recorded")
	}
}

func TestLegacySkillFreezesDigestAndPreservesSelectedID(t *testing.T) {
	c := milestoneCore(t)
	dir, pack := recoverySkill(t)
	if err := c.Store.Put("skill", Doc{"id": "legacy-selected", "path": dir, "name": pack.Name}); err != nil {
		t.Fatal(err)
	}
	profile, err := c.saveCapability(Doc{"name": "Legacy", "tools": []string{"read_file"}, "skillIds": []string{"legacy-selected"}, "executionProfile": "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Doc{"id": "a", "capabilityProfileId": str(obj(profile), "id")}
	frozen, err := c.FreezeAgent(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(frozen.Skills) != 1 || frozen.Skills[0].ID != "legacy-selected" {
		t.Fatal("legacy selected identity changed", frozen.Skills)
	}
	saved, err := c.Store.Get("skill", "legacy-selected")
	if err != nil || str(saved, "digest") != pack.Digest || number(saved, "version", 0) != 1 {
		t.Fatal("legacy digest was not persisted", saved, err)
	}
	if _, err = c.Store.Get("artifact", skillResourceID(pack.Digest, "assets/data.bin")); err != nil {
		t.Fatal("legacy resource was not registered", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "assets", "data.bin"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = c.FreezeAgent(agent); err == nil {
		t.Fatal("legacy version changed silently")
	}
	if err = engine.ValidateSkillPackages(frozen.Skills); err != nil {
		t.Fatal("previously frozen resource changed", err)
	}
}
