package harness

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ExportRunV2 retains exact public candidate files and observed solver evidence.
// Trusted grader diagnostics, reference repairs and unseen prompts never enter
// this projection. The unredacted record remains app-owned.
func (m *Manager) ExportRunV2(id string) (Doc, error) {
	r, err := m.GetRunV2(id)
	if err != nil {
		return nil, err
	}
	if r.Status == "running" {
		return nil, errors.New("wait for a checkpoint before exporting")
	}
	if err = m.validateRunV2(r); err != nil {
		return nil, err
	}
	publicRun := r
	publicRun.Results = append([]ResultV2{}, r.Results...)
	for i := range publicRun.Results {
		row := &publicRun.Results[i]
		row.Grade.Checks = append([]Check{}, row.Grade.Checks...)
		row.Grade.Error = ""
		if row.InfrastructureError != "" {
			row.InfrastructureError = "infrastructure evidence unavailable; inspect the private run record"
		}
		for j := range row.Grade.Checks {
			row.Grade.Checks[j].Error = ""
		}
	}
	publicRun.Error = ""
	out := Doc{"format": "gemma-harness-export-v2", "run": publicRun, "arms": []ArmV2{}, "trials": []Doc{}, "batches": []BatchV2{}, "publicFixtures": []Task{}, "limitations": []string{"Grader sources, expected answers and diagnostics are excluded.", "Artifact bytes are exact; inspect them before sharing. Known private metadata is redacted."}}
	arms := []ArmV2{}
	for _, armID := range r.ArmIDs {
		a, e := m.GetArmV2(armID)
		if e != nil {
			return nil, e
		}
		arms = append(arms, a)
	}
	out["arms"] = arms
	batches := []BatchV2{}
	for _, bid := range r.BatchIDs {
		b, e := m.GetBatchV2(bid)
		if e != nil {
			return nil, e
		}
		batches = append(batches, b)
	}
	out["batches"] = batches
	seen := map[string]bool{}
	trials := []Doc{}
	total := int64(0)
	for _, result := range r.Results {
		if result.TrialID == "" {
			continue
		}
		t, e := m.trial(result.TrialID)
		if e != nil {
			return nil, e
		}
		files, e := exportTrialFilesV2(t.Root, &total)
		if e != nil {
			return nil, e
		}
		d := asDoc(t)
		d["files"] = files
		trials = append(trials, d)
		seen[t.TaskID] = true
	}
	out["trials"] = trials
	public := []Task{}
	for _, t := range m.packs[r.PackID].Tasks {
		if seen[t.ID] {
			t.Cases = nil
			t.Probe = nil
			t.ExpectedAnswer = nil
			public = append(public, t)
		}
	}
	out["publicFixtures"] = public
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	var safe Doc
	if err = json.Unmarshal(b, &safe); err != nil {
		return nil, err
	}
	redactExport(map[string]any(safe), filepath.Dir(m.config.Root))
	safe["payloadHash"] = Hash(safe)
	safe["createdAt"] = m.now()
	return safe, nil
}

func exportTrialFilesV2(root string, total *int64) (map[string]Doc, error) {
	files := map[string]Doc{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in trial export")
		}
		if d.IsDir() {
			if p != root && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return errors.New("unsupported or oversized export file")
		}
		if *total+info.Size() > 64<<20 {
			return errors.New("export exceeds 64 MiB")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		*total += int64(len(b))
		rel, _ := filepath.Rel(root, p)
		digest := sha256.Sum256(b)
		files[rel] = Doc{"encoding": "base64", "sha256": fmt.Sprintf("%x", digest), "size": len(b), "content": base64.StdEncoding.EncodeToString(b)}
		return nil
	})
	return files, err
}
