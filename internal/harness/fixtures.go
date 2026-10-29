package harness

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
)

// Grader cases and reference repairs are never copied into solver workspaces.
//
//go:embed fixtures/*/task.json fixtures/*/reference.json fixtures/manifest.json
var fixtureFiles embed.FS

func LoadFixtures() ([]Task, error) {
	paths, e := fs.Glob(fixtureFiles, "fixtures/*/task.json")
	if e != nil {
		return nil, e
	}
	out := []Task{}
	for _, p := range paths {
		b, e := fixtureFiles.ReadFile(p)
		if e != nil {
			return nil, e
		}
		var raw struct {
			Task
			Cases []Case `json:"cases"`
			Probe *Probe `json:"probe"`
		}
		if e = json.Unmarshal(b, &raw); e != nil {
			return nil, e
		}
		t := raw.Task
		t.Cases = raw.Cases
		t.Probe = raw.Probe
		t.PublicHash = Hash(t.Public)
		t.GraderHash = Hash(struct {
			Cases                 []Case
			PublicHash            string
			Command, GradeCommand []string
			Probe                 *Probe
			ProtectedFiles        []string
			Policy                string
		}{t.Cases, t.PublicHash, t.Command, t.GradeCommand, t.Probe, t.ProtectedFiles, MethodVersion + "-grader-policy-v2"})
		if len(t.Cases) < 5 || len(t.Public) < 3 {
			return nil, fmt.Errorf("incomplete fixture %s", t.ID)
		}
		out = append(out, t)
	}
	if len(out) != 18 {
		return nil, fmt.Errorf("expected 18 fixtures, found %d", len(out))
	}
	return out, nil
}
func ReferenceRepair(id string) (map[string]string, error) {
	b, e := fixtureFiles.ReadFile("fixtures/" + id + "/reference.json")
	if e != nil {
		return nil, e
	}
	out := map[string]string{}
	e = json.Unmarshal(b, &out)
	return out, e
}
