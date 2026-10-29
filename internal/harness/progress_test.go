package harness

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluationProgressFreezesScheduleAndQualifiesLegacyFallback(t *testing.T) {
	repo := &memoryStore{}
	tasks := []Task{{ID: "dev", Split: "development", PublicHash: "dev", GraderHash: "policy"}, {ID: "validation", Split: "validation", PublicHash: "validation", GraderHash: "policy"}, {ID: "heldout", Split: "heldout", PublicHash: "heldout", GraderHash: "policy"}}
	m, err := Open(Config{Root: filepath.Join(t.TempDir(), "harness"), Store: repo, Tasks: tasks})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	x, err := m.Start(Spec{Mode: "native", TaskIDs: []string{"dev"}, WorkspaceID: "workspace", AgentID: "agent", ModelDigest: "digest", RuntimeVersion: "runtime", ExecutorIdentity: "sha256:pinned"}, Content{}, Author{})
	if err != nil {
		t.Fatal(err)
	}
	x.Status, x.CandidateID = "completed", x.Spec.BaseRevisionID
	if err = m.put("experiment", x); err != nil {
		t.Fatal(err)
	}
	v, err := m.StartEvaluation(x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.ExpectedResults != 12 {
		t.Fatal("admitted schedule size incorrect", v.ExpectedResults)
	}
	waitIdle(t, m)
	v.Status = "interrupted"
	v.Results = []EvalResult{{TrialID: "failed", Grade: Grade{Passed: false}}, {TrialID: "completed", Grade: Grade{Passed: true}}}
	if err = m.put("evaluation", v); err != nil {
		t.Fatal(err)
	}
	check := func(wantExpected bool) {
		t.Helper()
		rows, e := m.List("evaluation")
		if e != nil || len(rows) != 1 {
			t.Fatal(rows, e)
		}
		if rows[0]["recordedResults"] != 2 {
			t.Fatal("failed attempt dropped from progress", rows)
		}
		if _, ok := rows[0]["results"]; ok {
			t.Fatal("full results leaked into list")
		}
		value, exists := rows[0]["expectedResults"]
		if exists != wantExpected {
			t.Fatal("unexpected schedule certainty", rows)
		}
		if exists && value != 12 && value != float64(12) {
			t.Fatal("frozen schedule changed", rows)
		}
	}
	check(true)
	full, err := m.Get("evaluation", v.ID)
	if err != nil || len(full["results"].([]any)) != 2 {
		t.Fatal("full detail changed", full, err)
	}
	if _, ok := full["recordedResults"]; ok {
		t.Fatal("list-only progress persisted into full detail")
	}
	delete(full, "expectedResults")
	if err = repo.Put("harness_evaluation", full); err != nil {
		t.Fatal(err)
	}
	check(true) // Exact legacy fixture identity permits current schedule inference.
	m.tasks["new-heldout"] = Task{ID: "new-heldout", Split: "heldout", PublicHash: "new", GraderHash: "policy"}
	check(false) // A different current corpus must not invent a legacy denominator.
	if err = m.put("evaluation", v); err != nil {
		t.Fatal(err)
	}
	check(true) // Explicitly frozen schedule remains valid metadata after changes.
	if _, err = m.StartEvaluation(x.ID); err == nil || !strings.Contains(err.Error(), "fixture policy changed") {
		t.Fatal("changed corpus admitted", err)
	}
}
