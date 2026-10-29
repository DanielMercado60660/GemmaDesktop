package app

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This fixture substitutes only solver/grader work on three tiny synthetic
// tasks. Manager state transitions, packet authorship, paired evaluation gates,
// output hashes, app dispatch/activation, and SQLite persistence are real. No
// campaign fixtures, provider requests, container processes, or seeded eligible
// evaluation/promotion records participate.
type rollbackIntegration struct {
	c      *Core
	config harness.Config
	dbPath string
	x      harness.Experiment
	p      harness.Promotion
}

func rollbackDecode[T any](t *testing.T, v any) T {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *rollbackIntegration) call(t *testing.T, method string, p Doc) any {
	t.Helper()
	v, err := f.c.Call(t.Context(), method, p)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return v
}

func (f *rollbackIntegration) idle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.c.harness.Active() {
		if time.Now().After(deadline) {
			t.Fatal("synthetic harness operation did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *rollbackIntegration) get(t *testing.T, kind, id string) Doc {
	t.Helper()
	d, err := f.c.Store.Get(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *rollbackIntegration) binding(t *testing.T, w, a string) Doc {
	t.Helper()
	return f.get(t, "harness_binding", harness.Hash([]string{w, a}))
}

func (f *rollbackIntegration) reopen(t *testing.T) {
	t.Helper()
	if err := f.c.harness.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	f.c.Store = db
	f.config.Store = db
	f.c.harness, err = harness.Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
}

func newRollbackIntegration(t *testing.T) *rollbackIntegration {
	t.Helper()
	root := t.TempDir()
	f := &rollbackIntegration{dbPath: filepath.Join(root, "rollback.db")}
	db, err := store.Open(f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	f.c = &Core{DataDir: root, Store: db}
	for _, w := range []string{"workspace-a", "workspace-b"} {
		if err = db.Put("workspace", Doc{"id": w, "path": filepath.Join(root, w), "name": w}); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []string{"agent-a", "agent-b"} {
		if err = db.Put("agent", Doc{"id": a, "name": a, "instructions": "personal instructions stay unchanged"}); err != nil {
			t.Fatal(err)
		}
	}
	tasks := []harness.Task{}
	for _, split := range []string{"development", "validation", "heldout"} {
		public := map[string]string{"README.txt": "Synthetic rollback integration task. Preserve this file.\n"}
		tasks = append(tasks, harness.Task{ID: "rollback-" + split, Split: split, Stack: "synthetic", Public: public, PublicHash: harness.Hash(public), GraderHash: "synthetic-rollback-grader-v1", ProtectedFiles: []string{"README.txt"}})
	}
	f.config = harness.Config{Root: filepath.Join(root, "harness"), Store: db, Tasks: tasks, Activate: f.c.activateHarnessRevision,
		Solve: func(ctx context.Context, r harness.SolveRequest, _ func(harness.Event)) (harness.SolveResult, error) {
			// Keep positive measured elapsed times and an unambiguous passing cost
			// ratio without replacing Manager's measured duration or gate policy.
			delay := 5 * time.Millisecond
			if r.Revision.AuthoredBy.Kind == "baseline" {
				delay = 50 * time.Millisecond
			}
			select {
			case <-ctx.Done():
				return harness.SolveResult{}, ctx.Err()
			case <-time.After(delay):
			}
			if err := os.WriteFile(filepath.Join(r.Root, "answer.txt"), []byte(r.Revision.Content.Instructions), 0600); err != nil {
				return harness.SolveResult{}, err
			}
			return harness.SolveResult{Content: "synthetic solver completed", Usage: Doc{"prompt_eval_count": 10, "eval_count": 10}, TokenAccounting: inference.NewTokenAccounting(1, 1, 1)}, nil
		},
		Grade: func(_ context.Context, task harness.Task, trial harness.Trial) (harness.Grade, error) {
			readme, err := os.ReadFile(filepath.Join(trial.Root, "README.txt"))
			if err != nil {
				return harness.Grade{}, err
			}
			answer, err := os.ReadFile(filepath.Join(trial.Root, "answer.txt"))
			if err != nil {
				return harness.Grade{}, err
			}
			intact := string(readme) == task.Public["README.txt"]
			passed := strings.HasPrefix(string(answer), "candidate ")
			return harness.Grade{Passed: intact && passed, Checks: []harness.Check{{Name: "synthetic protected file", Critical: true, Passed: intact}, {Name: "synthetic answer", Passed: passed}}}, nil
		},
	}
	f.c.harness, err = harness.Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.c.harness.Close(); _ = f.c.Store.Close() })
	f.x, err = f.c.harness.Start(harness.Spec{Mode: "native", WorkspaceID: "workspace-a", AgentID: "agent-a", TaskIDs: []string{"rollback-development"}, ModelDigest: "synthetic-no-provider", RuntimeVersion: "synthetic-no-runtime", ExecutorIdentity: "sha256:synthetic-no-container"}, harness.Content{Instructions: "baseline"}, harness.Author{})
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 6; round++ {
		f.call(t, "harness.experiments.advance", Doc{"id": f.x.ID})
		f.idle(t)
		f.x = rollbackDecode[harness.Experiment](t, f.call(t, "harness.experiments.get", Doc{"id": f.x.ID}))
		if f.x.Status == "completed" {
			break
		}
		rows, err := f.c.harness.List("packet")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row["status"] != "awaiting_codex" {
				continue
			}
			p := rollbackDecode[harness.Packet](t, f.call(t, "harness.packets.get", Doc{"id": str(row, "id")}))
			r := harness.PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: harness.Author{Kind: "codex", Identity: "synthetic-proposer"}, Content: &harness.Content{Instructions: "candidate " + p.Stage}}
			if p.Stage == "reflection" {
				r.Content, r.Verdict, r.Evidence, r.AuthoredBy.Identity = nil, "approved", p.Evidence, "synthetic-independent-reviewer"
			}
			f.call(t, "harness.packets.submit", obj(r))
		}
	}
	if f.x.Status != "completed" || f.x.Rollouts != 4 || f.x.ImprovementStages != 3 {
		t.Fatalf("real native state machine did not complete: %+v", f.x)
	}
	v := rollbackDecode[harness.Evaluation](t, f.call(t, "harness.evaluations.start", Doc{"experimentId": f.x.ID}))
	f.idle(t)
	v = rollbackDecode[harness.Evaluation](t, f.call(t, "harness.evaluations.get", Doc{"id": v.ID}))
	if v.Status != "completed" || len(v.Results) != 12 || !v.Gate.Eligible || !v.Gate.CostComparisonValid {
		t.Fatalf("current paired promotion gates failed: %+v", v.Gate)
	}
	if _, err = f.c.Call(t.Context(), "harness.promote", Doc{"evaluationId": v.ID}); err == nil {
		t.Fatal("promotion bypassed independent review")
	}
	p := rollbackDecode[harness.Packet](t, f.call(t, "harness.packets.get", Doc{"id": v.ReviewPacketID}))
	f.call(t, "harness.packets.submit", obj(harness.PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: harness.Author{Kind: "codex", Identity: "synthetic-independent-reviewer"}, Verdict: "approved", Evidence: p.Evidence}))
	f.p = rollbackDecode[harness.Promotion](t, f.call(t, "harness.promote", Doc{"evaluationId": v.ID}))
	if f.p.Kind != "promote" || f.p.RevisionID != f.x.CandidateID || f.p.PreviousID != f.x.Spec.BaseRevisionID || str(f.binding(t, "workspace-a", "agent-a"), "revisionId") != f.x.CandidateID {
		t.Fatal("real promotion/activation did not persist its scoped binding", f.p)
	}
	return f
}

func (f *rollbackIntegration) activateOtherRevision(t *testing.T, workspaceID, agentID, previous, instructions string) harness.Revision {
	t.Helper()
	spec := f.x.Spec
	spec.WorkspaceID, spec.AgentID = workspaceID, agentID
	r, err := f.c.harness.CreateRevision(spec, previous, harness.Content{Instructions: instructions}, harness.Author{Kind: "codex", Identity: "unrelated-test-activation"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.c.activateHarnessRevision(t.Context(), workspaceID, agentID, previous, r.ID); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHarnessRollbackRestoresPersistedBindingAndIsolatesTargets(t *testing.T) {
	f := newRollbackIntegration(t)
	f.activateOtherRevision(t, "workspace-b", "agent-a", "", "other workspace")
	f.activateOtherRevision(t, "workspace-a", "agent-b", "", "other agent")
	otherWorkspace := f.binding(t, "workspace-b", "agent-a")
	otherAgent := f.binding(t, "workspace-a", "agent-b")
	personalAgent := f.get(t, "agent", "agent-a")
	originalPromotion := f.get(t, "harness_promotion", f.p.ID)
	f.reopen(t)
	r := rollbackDecode[harness.Promotion](t, f.call(t, "harness.rollback", Doc{"id": f.p.ID}))
	if r.Kind != "rollback" || r.RevisionID != f.x.Spec.BaseRevisionID || r.PreviousID != f.p.RevisionID || r.WorkspaceID != "workspace-a" || r.AgentID != "agent-a" || r.EvaluationID != f.p.EvaluationID || r.EvidenceHash != f.p.EvidenceHash {
		t.Fatal("rollback lost prior revision, target or provenance", r)
	}
	f.reopen(t)
	binding := f.binding(t, "workspace-a", "agent-a")
	if str(binding, "revisionId") != f.x.Spec.BaseRevisionID || str(binding, "previousId") != f.p.RevisionID {
		t.Fatal("rollback did not survive reopening SQLite", binding)
	}
	if !reflect.DeepEqual(f.get(t, "harness_promotion", r.ID), obj(r)) || !reflect.DeepEqual(f.get(t, "harness_promotion", f.p.ID), originalPromotion) {
		t.Fatal("rollback receipt missing or original promotion mutated")
	}
	if !reflect.DeepEqual(f.binding(t, "workspace-b", "agent-a"), otherWorkspace) || !reflect.DeepEqual(f.binding(t, "workspace-a", "agent-b"), otherAgent) || !reflect.DeepEqual(f.get(t, "agent", "agent-a"), personalAgent) {
		t.Fatal("rollback escaped workspace+agent scope or changed global agent")
	}
	// Admission resolves the restored immutable revision, not a mutable agent
	// document or the now-inactive candidate's instructions.
	req := engine.Request{Instructions: defaultInstructions + "\n\n" + str(personalAgent, "instructions")}
	if _, err := f.c.applyHarnessBinding("workspace-a", personalAgent, &req, &FrozenAgent{}); err != nil || req.Instructions != defaultInstructions+"\n\nbaseline\n\n" {
		t.Fatal("ordinary admission did not resolve restored baseline", err, req.Instructions)
	}
	newer := f.activateOtherRevision(t, "workspace-a", "agent-a", r.RevisionID, "later activation after rollback")
	beforeRetry := f.binding(t, "workspace-a", "agent-a")
	retry := rollbackDecode[harness.Promotion](t, f.call(t, "harness.rollback", Doc{"id": f.p.ID}))
	if !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(f.binding(t, "workspace-a", "agent-a"), beforeRetry) || str(beforeRetry, "revisionId") != newer.ID {
		t.Fatal("idempotent rollback retry overwrote a later activation")
	}
}

func TestHarnessRollbackRejectsStaleActivationWithoutPersistingSuccess(t *testing.T) {
	f := newRollbackIntegration(t)
	f.activateOtherRevision(t, "workspace-a", "agent-a", f.p.RevisionID, "newer unrelated activation")
	bindingBefore := f.binding(t, "workspace-a", "agent-a")
	promotionBefore := f.get(t, "harness_promotion", f.p.ID)
	rollbackID := harness.Hash([]string{f.p.ID, "rollback"})[:32]
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			f.reopen(t)
		}
		if _, err := f.c.Call(t.Context(), "harness.rollback", Doc{"id": f.p.ID}); err == nil || !strings.Contains(err.Error(), "target harness changed") {
			t.Fatal("stale rollback did not reject newer persisted activation", err)
		}
		if !reflect.DeepEqual(f.binding(t, "workspace-a", "agent-a"), bindingBefore) || !reflect.DeepEqual(f.get(t, "harness_promotion", f.p.ID), promotionBefore) {
			t.Fatal("rejected stale rollback changed binding or prior evidence")
		}
		if _, err := f.c.Store.Get("harness_promotion", rollbackID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("rejected stale rollback persisted a success receipt", err)
		}
	}
}
