package harness

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed packs/*/manifest.json packs/*/*/task.json packs/*/*/reference.json
var packFiles embed.FS

func fixtureIdentity(t Task, policy string) string {
	return Hash(struct {
		PublicHash                            string
		Command, GradeCommand, ProtectedFiles []string
		Cases                                 []Case
		Probe                                 *Probe
		Contract, Policy                      string
		RegressionTestGlobs, DiscoveryCommand []string
		ExpectedAnswer                        json.RawMessage
	}{t.PublicHash, t.Command, t.GradeCommand, t.ProtectedFiles, t.Cases, t.Probe, t.Contract, policy, t.RegressionTestGlobs, t.DiscoveryCommand, t.ExpectedAnswer})
}

func packDigest(p FixturePack) string {
	rows := []any{p.ID, p.Version, p.GraderPolicy}
	tasks := append([]Task{}, p.Tasks...)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	for _, t := range tasks {
		rows = append(rows, []string{t.ID, t.Split, t.PublicHash, t.GraderHash, t.ExposureFamily, t.Prompt, t.Contract})
	}
	return Hash(rows)
}

func ValidateFixturePack(p FixturePack) error {
	if p.ID == "" || p.Version == "" || p.GraderPolicy == "" || len(p.Tasks) == 0 || len(p.Tasks) > 100 {
		return errors.New("fixture pack requires a versioned identity and bounded tasks")
	}
	seen := map[string]bool{}
	families := map[string]string{}
	for _, t := range p.Tasks {
		if t.ID == "" || seen[t.ID] || t.ExposureFamily == "" || t.Prompt == "" {
			return errors.New("fixture task identity is missing or duplicated")
		}
		seen[t.ID] = true
		if t.Split != "development" && t.Split != "validation" && t.Split != "heldout" {
			return errors.New("invalid fixture split")
		}
		if prior := families[t.ExposureFamily]; prior != "" && prior != t.Split {
			return errors.New("fixture family crosses development/qualification splits")
		}
		families[t.ExposureFamily] = t.Split
		if t.Contract != "coding" && t.Contract != "read_only" {
			return errors.New("unsupported task contract")
		}
		if len(t.Command) == 0 || len(t.GradeCommand) == 0 || len(t.Cases) < 3 || len(t.Public) < 3 {
			return errors.New("incomplete fixture contract")
		}
		for name := range t.Public {
			if name == "." || name == ".." || path.IsAbs(name) || path.Clean(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, "../") {
				return errors.New("unsafe fixture path")
			}
		}
		for _, name := range t.ProtectedFiles {
			if _, ok := t.Public[name]; !ok {
				return errors.New("protected path absent from fixture")
			}
		}
		if t.PublicHash != Hash(t.Public) || t.GraderHash != fixtureIdentity(t, p.GraderPolicy) {
			return errors.New("fixture content identity mismatch")
		}
	}
	if p.Digest != packDigest(p) {
		return errors.New("fixture pack digest mismatch")
	}
	return nil
}

// LoadFixturePacks does not alter LoadFixtures or the v1 global fixture digest.
// Trusted graders/references are embedded, never returned by the metadata API.
func LoadFixturePacks() ([]FixturePack, error) {
	paths, err := fs.Glob(packFiles, "packs/*/manifest.json")
	if err != nil {
		return nil, err
	}
	out := []FixturePack{}
	for _, name := range paths {
		b, err := packFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		var p FixturePack
		if err = json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		taskPaths, _ := fs.Glob(packFiles, path.Dir(name)+"/*/task.json")
		for _, taskPath := range taskPaths {
			b, err = packFiles.ReadFile(taskPath)
			if err != nil {
				return nil, err
			}
			var raw struct {
				Task
				Cases          []Case          `json:"cases"`
				Probe          *Probe          `json:"probe"`
				ExpectedAnswer json.RawMessage `json:"expectedAnswer"`
			}
			if err = json.Unmarshal(b, &raw); err != nil {
				return nil, err
			}
			t := raw.Task
			t.Cases = raw.Cases
			t.Probe = raw.Probe
			t.ExpectedAnswer = raw.ExpectedAnswer
			t.PublicHash = Hash(t.Public)
			t.GraderHash = fixtureIdentity(t, p.GraderPolicy)
			p.Tasks = append(p.Tasks, t)
		}
		p.Digest = packDigest(p)
		if err = ValidateFixturePack(p); err != nil {
			return nil, fmt.Errorf("pack %s: %w", p.ID, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// ReferenceRepairV2 is for isolated oracle qualification, never solver exposure.
func ReferenceRepairV2(packID, taskID string) (map[string]string, error) {
	if path.Base(packID) != packID || path.Base(taskID) != taskID {
		return nil, errors.New("invalid fixture reference")
	}
	b, err := packFiles.ReadFile("packs/" + packID + "/" + taskID + "/reference.json")
	if err != nil {
		return nil, err
	}
	var out map[string]string
	err = json.Unmarshal(b, &out)
	return out, err
}

func (m *Manager) Packs() []Doc {
	out := []Doc{}
	for _, p := range m.packs {
		tasks := []Doc{}
		for _, t := range p.Tasks {
			tasks = append(tasks, Doc{"id": t.ID, "name": t.Name, "stack": t.Stack, "split": t.Split, "publicHash": t.PublicHash, "graderHash": t.GraderHash, "contract": t.Contract})
		}
		out = append(out, Doc{"id": p.ID, "version": p.Version, "digest": p.Digest, "graderPolicy": p.GraderPolicy, "tasks": tasks})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	return out
}

func (m *Manager) taskForSpec(s Spec, id string) (Task, bool) {
	if s.FixturePackID == "" {
		t, ok := m.tasks[id]
		return t, ok
	}
	for _, t := range m.packs[s.FixturePackID].Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}
func (m *Manager) fixtureDigestForSpec(s Spec) string {
	if s.FixturePackID == "" {
		return m.fixtureDigest()
	}
	return m.packs[s.FixturePackID].Digest
}

// TasksForSpec exposes only admitted public development material to the sidecar.
func (m *Manager) TasksForSpec(s Spec) ([]Task, error) {
	if s.FixturePackID != "" {
		if err := ValidateFixturePack(m.packs[s.FixturePackID]); err != nil {
			return nil, err
		}
	}
	if s.FixtureDigest != m.fixtureDigestForSpec(s) {
		return nil, errors.New("fixture collection changed since admission")
	}
	out := []Task{}
	for _, id := range s.TaskIDs {
		t, ok := m.taskForSpec(s, id)
		if !ok || t.Split != "development" {
			return nil, errors.New("only admitted development tasks may reach optimizer")
		}
		t.Cases = nil
		t.Probe = nil
		t.ExpectedAnswer = nil
		t.GradeCommand = nil
		out = append(out, t)
	}
	return out, nil
}
