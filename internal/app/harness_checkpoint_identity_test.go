package app

import (
	"context"
	"encoding/json"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the actual pinned recursion over stdio. All four harness trees are
// intentionally identical: content cannot identify which stage owns a probe.
func TestHarnessActualCoreIdenticalContentCheckpointIdentity(t *testing.T) {
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
	base := harness.Content{Instructions: "same instructions", ToolGuidance: "same guidance", Lessons: []string{}, SkillIDs: []string{}}
	var calls atomic.Int32
	config := harness.Config{Root: filepath.Join(root, "harness"), Store: db, Packs: packs, Upstream: c.runHarnessUpstream, Solve: func(_ context.Context, req harness.SolveRequest, _ func(harness.Event)) (harness.SolveResult, error) {
		calls.Add(1)
		if harness.Hash(req.Revision.Content) != harness.Hash(base) {
			t.Error("full content changed")
		}
		if err := os.WriteFile(filepath.Join(req.Root, "checkpoint-proof.txt"), []byte("immutable output"), 0600); err != nil {
			return harness.SolveResult{}, err
		}
		return harness.SolveResult{Content: "delivered public artifact"}, nil
	}}
	m, err := harness.Open(config)
	if err != nil {
		t.Fatal(err)
	}
	c.harness = m
	t.Cleanup(func() { _ = m.Close(); _ = db.Close() })
	x, err := m.Start(harness.Spec{Mode: "upstream", FixturePackID: "coding-diagnostics-v2", WorkspaceID: "workspace", AgentID: "agent", TaskIDs: []string{"duration-units"}, Model: "stub", ModelDigest: "digest", RuntimeVersion: "fixture", ExecutorIdentity: "sha256:fixture", BudgetSeconds: 60}, base, harness.Author{})
	if err != nil {
		t.Fatal(err)
	}
	readExperiment := func() harness.Experiment {
		t.Helper()
		d, err := m.Get("experiment", x.ID)
		if err != nil {
			t.Fatal(err)
		}
		var current harness.Experiment
		b, _ := json.Marshal(d)
		if err := json.Unmarshal(b, &current); err != nil {
			t.Fatal(err)
		}
		return current
	}
	if _, err = m.Advance(x.ID); err != nil {
		t.Fatal(err)
	}
	checkpointed := false
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := m.List("packet")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if str(row, "experimentId") != x.ID || str(row, "status") != "awaiting_codex" {
				continue
			}
			var p harness.Packet
			b, _ := json.Marshal(row)
			_ = json.Unmarshal(b, &p)
			response := harness.PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: harness.Author{Kind: "codex", Identity: "proposal-author"}, Content: &base, Notes: "KEEP_BASE: identical content is deliberate"}
			if p.Stage == "reflection" {
				response.Content = nil
				response.AuthoredBy.Identity = "independent-reviewer"
				response.Verdict = "approved"
				response.Evidence = p.Evidence
			}
			if _, err := m.Submit(response); err != nil {
				t.Fatal(err)
			}
		}
		if !m.Active() {
			current := readExperiment()
			switch current.Status {
			case "checkpointed":
				if checkpointed || calls.Load() != 3 || len(current.TrialIDs) != 3 {
					t.Fatalf("checkpoint is not after exactly three fresh probes: calls=%d trials=%d", calls.Load(), len(current.TrialIDs))
				}
				bindings, err := m.UpstreamRevisionBindings(current)
				if err != nil {
					t.Fatal(err)
				}
				if len(bindings) != 4 {
					t.Fatalf("missing accepted stage bindings: %d", len(bindings))
				}
				for _, binding := range bindings {
					wrong := current.Spec.BaseRevisionID
					if binding.Label == "base" {
						wrong = currentCandidateBinding(bindings)
					}
					if _, err := m.UpstreamSolve(context.Background(), x.ID, binding.TaskID, binding.Label, wrong); err == nil {
						t.Fatal("wrong stage revision reached solver", binding.Label)
					}
				}
				if calls.Load() != 3 {
					t.Fatal("mismatch validation performed inference")
				}
				// Reopen the owner as well as its Python sidecar. Stage bindings must
				// come from immutable packets, not an in-memory content lookup.
				if err := m.Close(); err != nil {
					t.Fatal(err)
				}
				m, err = harness.Open(config)
				if err != nil {
					t.Fatal(err)
				}
				c.harness = m
				checkpointed = true
				if _, err := m.Advance(x.ID); err != nil {
					t.Fatal(err)
				}
			case "completed":
				x = current
				goto completed
			default:
				t.Fatalf("unexpected actual-core stopped state %s: %s", current.Status, current.Error)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual core did not complete checkpoint resume")
completed:
	if !checkpointed || calls.Load() != 4 || x.Rollouts != 4 || len(x.TrialIDs) != 4 {
		t.Fatalf("resume duplicated a stage: calls=%d trials=%d", calls.Load(), len(x.TrialIDs))
	}
	seen := map[string]bool{}
	var baseline harness.Trial
	for _, id := range x.TrialIDs {
		d, err := m.Get("trial", id)
		if err != nil {
			t.Fatal(err)
		}
		var trial harness.Trial
		b, _ := json.Marshal(d)
		_ = json.Unmarshal(b, &trial)
		if seen[trial.Label] || trial.InputHash == "" || trial.OutputHash == "" {
			t.Fatal("missing/duplicate immutable stage", trial.Label)
		}
		seen[trial.Label] = true
		if trial.Label == "base" {
			baseline = trial
			if trial.RevisionID != x.Spec.BaseRevisionID {
				t.Fatal("original baseline was rebound")
			}
		}
	}
	for _, label := range []string{"base", "inv", "dom", "final"} {
		if !seen[label] {
			t.Fatal("missing stage", label)
		}
	}
	batches, err := m.OptimizationBatchesV2(x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 {
		t.Fatalf("expected exactly two bounded batches, got %d", len(batches))
	}
	attempts := 0
	for _, batch := range batches {
		ids, _ := batch["attemptIds"].([]any)
		attempts += len(ids)
		if len(ids) > 3 {
			t.Fatal("batch cap exceeded")
		}
	}
	if attempts != 4 {
		t.Fatal("cache replay consumed fresh attempts", attempts)
	}
	if err := c.runHarnessUpstream(context.Background(), m, x); err != nil {
		t.Fatal("intact cached resume", err)
	}
	if calls.Load() != 4 {
		t.Fatal("intact cache replayed inference")
	}
	proof := filepath.Join(config.Root, "trials", baseline.ID, "workspace", "checkpoint-proof.txt")
	if err := os.WriteFile(proof, []byte("tampered output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.runHarnessUpstream(context.Background(), m, x); err == nil || !strings.Contains(err.Error(), "output changed") {
		t.Fatal("cached output tampering accepted", err)
	}
	if err := os.WriteFile(proof, []byte("immutable output"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := m.Get("trial", baseline.ID)
	if err != nil {
		t.Fatal(err)
	}
	d["inputHash"] = "tampered-input"
	if err := db.Put("harness_trial", d); err != nil {
		t.Fatal(err)
	}
	if err := c.runHarnessUpstream(context.Background(), m, x); err == nil || !strings.Contains(err.Error(), "input identity") {
		t.Fatal("cached input tampering accepted", err)
	}
	if calls.Load() != 4 {
		t.Fatal("tampered cache replayed inference")
	}
}

func currentCandidateBinding(bindings []harness.UpstreamRevisionBinding) string {
	for _, binding := range bindings {
		if binding.Label == "final" {
			return binding.RevisionID
		}
	}
	return ""
}
