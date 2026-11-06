package app

import (
	"context"
	"encoding/json"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Published Harness² recursion and real stdio supervisor; the only substituted
// effect is the production shared Solve callback, with no model inference.
func TestHarnessV2MultiTaskUpstreamCheckpointsAndComposesAll(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	packs, err := harness.LoadFixturePacks()
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{DataDir: root, Store: db}
	var calls atomic.Int32
	m, err := harness.Open(harness.Config{Root: filepath.Join(root, "harness"), Store: db, Packs: packs, Upstream: c.runHarnessUpstream, Solve: func(_ context.Context, r harness.SolveRequest, _ func(harness.Event)) (harness.SolveResult, error) {
		calls.Add(1)
		if len(r.Task.Cases) > 0 || r.Task.Probe != nil || len(r.Task.ExpectedAnswer) > 0 {
			t.Error("private fixture reached solver")
		}
		return harness.SolveResult{Content: "observed public artifact"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	c.harness = m
	t.Cleanup(func() { m.Close(); db.Close() })
	x, err := m.Start(harness.Spec{Mode: "upstream", FixturePackID: "coding-diagnostics-v2", WorkspaceID: "workspace", AgentID: "agent", TaskIDs: []string{"duration-units", "event-join"}, Model: "stub", ModelDigest: "digest", RuntimeVersion: "fixture", ExecutorIdentity: "sha256:fixture", BudgetSeconds: 60}, harness.Content{Instructions: "baseline"}, harness.Author{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Advance(x.ID); err != nil {
		t.Fatal(err)
	}
	var composition harness.Packet
	var final Doc
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := m.List("packet")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row["status"] != "awaiting_codex" {
				continue
			}
			d, err := m.Get("packet", str(row, "id"))
			if err != nil {
				t.Fatal(err)
			}
			var p harness.Packet
			b, _ := json.Marshal(d)
			_ = json.Unmarshal(b, &p)
			response := harness.PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: harness.Author{Kind: "codex", Identity: "proposal-author"}, Content: &harness.Content{Instructions: "reviewed " + p.Stage}, Notes: "development evidence"}
			if p.Stage == "reflection" {
				response.Content = nil
				response.AuthoredBy.Identity = "independent-reflector"
				response.Verdict = "approved"
				response.Evidence = p.Evidence
			}
			if p.Stage == "compose_all" {
				composition = p
				if len(p.Evidence) != 2 {
					t.Fatal("composition omitted development task")
				}
			}
			if _, err = m.Submit(response); err != nil {
				t.Fatal(err)
			}
		}
		if !m.Active() {
			final, err = m.Get("experiment", x.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch str(final, "status") {
			case "completed":
				goto completed
			case "checkpointed", "awaiting_codex":
				if _, err = m.Advance(x.ID); err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("unexpected stopped state", final)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("versioned upstream failed to reach completed checkpoint")
completed:
	if calls.Load() != 8 || number(final, "rollouts", 0) != 8 || number(final, "improvementStages", 0) != 7 || composition.ID == "" {
		t.Fatal("multi-task accounting incomplete", calls.Load(), final)
	}
	packet, err := m.Get("packet", composition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final["candidateId"] != packet["revisionId"] {
		t.Fatal("last-task revision substituted for composed candidate")
	}
	batches, err := m.OptimizationBatchesV2(x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 3 {
		t.Fatalf("expected 3 explicitly resumed batches, got %d", len(batches))
	}
	total := 0
	for _, b := range batches {
		ids, _ := b["attemptIds"].([]any)
		if len(ids) > 3 {
			t.Fatal("batch exceeded attempt cap")
		}
		total += len(ids)
	}
	if total != 8 {
		t.Fatal("cached trials consumed new-attempt budget")
	}
	if _, err = m.StartEvaluation(x.ID); err == nil {
		t.Fatal("new pack entered legacy qualification")
	}
}
