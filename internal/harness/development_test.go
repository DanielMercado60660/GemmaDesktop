package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func developmentManager(t *testing.T, grade func(context.Context, GradeRequest) (Grade, error)) (*Manager, Spec, Content) {
	t.Helper()
	pack := fixturePackForTestV2()
	m, err := Open(Config{Root: t.TempDir(), Store: &memoryStore{}, Packs: []FixturePack{pack}, GradeWithSpec: grade,
		Solve: func(_ context.Context, req SolveRequest, _ func(Event)) (SolveResult, error) {
			if err := os.WriteFile(filepath.Join(req.Root, "logic.py"), []byte("candidate output"), 0600); err != nil {
				return SolveResult{}, err
			}
			return SolveResult{Content: "completed model answer"}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	spec := Spec{Mode: "native", FixturePackID: pack.ID, WorkspaceID: "workspace", AgentID: "agent", TaskIDs: []string{"development"}, Model: "fixture-model", ModelDigest: "fixture-digest", RuntimeVersion: "fixture-runtime", RuntimeIdentity: Doc{"backend": "fixture", "revision": "fixed", "artifactDigest": "fixture-digest"}, AppVersion: "frozen-host", ContextSize: 32768, BudgetSeconds: 600, ExecutorIdentity: "sha256:fixture-executor", ToolchainVersion: "sha256:fixture-executor", AuthorityHash: "frozen-authority", FrozenCapability: json.RawMessage(`{"z":1,"a":2}`), Agent: Doc{"maxSteps": 40, "temperature": 0.2}, ApprovedSkills: map[string]json.RawMessage{"skill": json.RawMessage(`{"z":1,"a":2}`)}, ApprovedWorkflows: map[string]json.RawMessage{"workflow": json.RawMessage(`{"nodes":[],"revision":"frozen"}`)}, FrozenAgents: map[string]json.RawMessage{"delegate": json.RawMessage(`{"id":"delegate","revision":"frozen"}`)}, ApprovedDelegateIDs: []string{"delegate"}}
	base := Content{Instructions: "initial instructions", ToolGuidance: "initial guidance", Lessons: []string{"initial lesson"}, SkillIDs: []string{"skill"}, WorkflowID: "workflow", DelegateAgentIDs: []string{"delegate"}}
	return m, spec, base
}

func startDevelopmentBaseline(t *testing.T, m *Manager, s Spec, base Content) (Experiment, Trial) {
	t.Helper()
	x, err := m.Start(s, base, Author{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Advance(x.ID); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if err = m.get("experiment", x.ID, &x); err != nil {
		t.Fatal(err)
	}
	if len(x.TrialIDs) != 1 || x.Status != "awaiting_codex" {
		t.Fatalf("baseline unavailable: %+v", x)
	}
	trial, err := m.trial(x.TrialIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	return x, trial
}

func completeDevelopment(t *testing.T, m *Manager, x Experiment, base Content) Experiment {
	t.Helper()
	for round := 0; round < 12; round++ {
		rows, err := m.config.Store.List("harness_packet")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			p, _ := decode[Packet](row)
			if p.ExperimentID != x.ID || p.Response != nil {
				continue
			}
			content := base
			content.Instructions = "candidate " + p.Stage
			response := PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: Author{Kind: "codex", Identity: "proposal-author"}, Content: &content, Notes: "public trace evidence"}
			if p.Stage == "reflection" {
				response.Content = nil
				response.AuthoredBy.Identity = "independent-reviewer"
				response.Verdict = "approved"
				response.Evidence = p.Evidence
			}
			if _, err = m.Submit(response); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = m.Advance(x.ID); err != nil {
			t.Fatal(err)
		}
		waitIdle(t, m)
		if err = m.get("experiment", x.ID, &x); err != nil {
			t.Fatal(err)
		}
		if x.Status == "completed" {
			return x
		}
	}
	t.Fatalf("development did not complete: %+v", x)
	return x
}

func TestDevelopmentCarryForwardPreservesFullContentAndFrozenAuthority(t *testing.T) {
	m, s, base := developmentManager(t, nil)
	x, _ := startDevelopmentBaseline(t, m, s, base)
	x = completeDevelopment(t, m, x, base)
	prior, err := m.revision(x.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	s.DevelopmentBaseRevisionID = prior.ID
	next, err := m.Start(s, Content{Instructions: "must not replace explicit prior content"}, Author{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.revision(next.Spec.BaseRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if Hash(got) != Hash(prior) || next.Spec.DevelopmentSourceExperimentID != x.ID || next.Spec.DevelopmentBaseRevisionID != prior.ID || !frozenSpecValid(next.Spec) {
		t.Fatal("carry-forward changed content, provenance or frozen identity", next)
	}
	for name, mutate := range map[string]func(*Spec){
		"target":    func(s *Spec) { s.AgentID = "other" },
		"model":     func(s *Spec) { s.ModelDigest = "other" },
		"authority": func(s *Spec) { s.AuthorityHash = "changed" },
		"workflow": func(s *Spec) {
			s.ApprovedWorkflows = map[string]json.RawMessage{"workflow": json.RawMessage(`{"changed":true}`)}
		},
		"delegate": func(s *Spec) {
			s.FrozenAgents = map[string]json.RawMessage{"delegate": json.RawMessage(`{"changed":true}`)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := s
			mutate(&copy)
			if _, err := m.Start(copy, base, Author{}); err == nil {
				t.Fatal("incompatible carry-forward admitted")
			}
		})
	}
	rows, _ := m.config.Store.List("harness_packet")
	for _, row := range rows {
		if row["experimentId"] == x.ID && row["stage"] == "reflection" {
			p, _ := decode[Packet](row)
			p.Response.Verdict = "rejected"
			_ = m.put("packet", p)
		}
	}
	if _, err := m.Start(s, base, Author{}); err == nil {
		t.Fatal("rejected provenance admitted")
	}
}

func TestDevelopmentCanRetainInitialBaselineWithoutFabricatedReview(t *testing.T) {
	m, s, base := developmentManager(t, nil)
	x, _ := startDevelopmentBaseline(t, m, s, base)
	x.Status = "failed"
	if err := m.put("experiment", x); err != nil {
		t.Fatal(err)
	}
	s.DevelopmentBaseRevisionID = x.Spec.BaseRevisionID
	next, err := m.Start(s, base, Author{})
	if err != nil {
		t.Fatal(err)
	}
	if next.Spec.BaseRevisionID != x.Spec.BaseRevisionID {
		t.Fatal("initial baseline lost")
	}
	base.Instructions = "different product baseline"
	if _, err = m.Start(s, base, Author{}); err == nil {
		t.Fatal("changed baseline projected silently")
	}
}

func TestDevelopmentGradeOpaqueIdempotentAndSeparateFromCompletion(t *testing.T) {
	var calls atomic.Int32
	m, s, base := developmentManager(t, func(_ context.Context, r GradeRequest) (Grade, error) {
		calls.Add(1)
		return Grade{TrialID: r.Trial.ID, GraderHash: r.Task.GraderHash, Passed: false, Checks: []Check{{Name: "HIDDEN_SENTINEL expected-value", Passed: false, Error: "HIDDEN_SENTINEL assertion"}, {Name: "integrity", Passed: true}}}, nil
	})
	x, trial := startDevelopmentBaseline(t, m, s, base)
	if !frozenSpecValid(x.Spec) {
		t.Fatal("new admission hash did not survive embedded JSON persistence")
	}
	before, _ := treeHash(trial.Root)
	grade, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID)
	if err != nil {
		t.Fatal(err)
	}
	if grade.Passed || grade.CheckCount != 2 || grade.FailedCount != 1 || grade.Infrastructure != "" || !grade.GradingFinished || grade.ResultHash == "" || trial.Status != "completed" {
		t.Fatalf("completion misclassified or grade incomplete: %+v", grade)
	}
	again, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID)
	if err != nil || Hash(grade) != Hash(again) || calls.Load() != 1 {
		t.Fatal("idempotent grade replayed", again, err, calls.Load())
	}
	after, _ := treeHash(trial.Root)
	if before != after {
		t.Fatal("grading changed solver files")
	}
	rows, _ := m.config.Store.List("harness_development_grade")
	raw, _ := json.Marshal(rows)
	wire, _ := json.Marshal(grade)
	if strings.Contains(string(raw), "HIDDEN_SENTINEL") || strings.Contains(string(wire), "HIDDEN_SENTINEL") {
		t.Fatal("opaque development grade leaked hidden details")
	}
	grade.Passed = true
	if err := m.put("development_grade", grade); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID); err == nil {
		t.Fatal("tampered grade reused")
	}
}

func TestDevelopmentGradeErrorsAreOpaqueAndNotRetried(t *testing.T) {
	var calls atomic.Int32
	m, s, base := developmentManager(t, func(_ context.Context, r GradeRequest) (Grade, error) {
		calls.Add(1)
		return Grade{TrialID: r.Trial.ID, GraderHash: r.Task.GraderHash, Error: "HIDDEN_SENTINEL ordinary diagnostic"}, errors.New("HIDDEN_SENTINEL malicious/assertion payload")
	})
	x, trial := startDevelopmentBaseline(t, m, s, base)
	grade, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID)
	if err != nil || grade.Infrastructure != "grading" || grade.Passed {
		t.Fatal("grader error not opaque", grade, err)
	}
	_, err = m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID)
	if err != nil || calls.Load() != 1 {
		t.Fatal("failed grading replayed", err, calls.Load())
	}
	rows, _ := m.config.Store.List("harness_development_grade")
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "HIDDEN_SENTINEL") {
		t.Fatal("hidden error persisted in optimizer-facing record")
	}
}

func TestDevelopmentGradeRefusesTamperedAndNonDevelopmentInputs(t *testing.T) {
	for _, name := range []string{"output", "input", "spec", "revision", "unrelated", "running", "validation"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			m, s, base := developmentManager(t, func(context.Context, GradeRequest) (Grade, error) { calls.Add(1); return Grade{}, nil })
			x, trial := startDevelopmentBaseline(t, m, s, base)
			switch name {
			case "output":
				_ = os.WriteFile(filepath.Join(trial.Root, "logic.py"), []byte("external mutation"), 0600)
			case "input":
				trial.InputHash = "tampered"
				_ = m.put("trial", trial)
			case "spec":
				x.Spec.ContextSize++
				_ = m.put("experiment", x)
			case "revision":
				r, _ := m.revision(trial.RevisionID)
				r.Content.Instructions = "tampered"
				_ = m.put("revision", r)
			case "unrelated":
				trial.ExperimentID = "other"
				_ = m.put("trial", trial)
			case "running":
				trial.Status = "running"
				_ = m.put("trial", trial)
			case "validation":
				trial.TaskID = "validation"
				_ = m.put("trial", trial)
			}
			if _, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID); err == nil || calls.Load() != 0 {
				t.Fatal("invalid trial reached grader", err, calls.Load())
			}
		})
	}
}

func TestDevelopmentGradeCancellationAndReservationDoNotReplay(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	m, s, base := developmentManager(t, func(ctx context.Context, _ GradeRequest) (Grade, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return Grade{}, ctx.Err()
	})
	x, trial := startDevelopmentBaseline(t, m, s, base)
	done := make(chan DevelopmentGrade, 1)
	errs := make(chan error, 1)
	go func() { g, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID); done <- g; errs <- err }()
	<-entered
	if _, err := m.Advance(x.ID); err == nil {
		t.Fatal("solver work overlapped grading")
	}
	if err := m.Cancel(x.ID); err != nil {
		t.Fatal(err)
	}
	grade := <-done
	if err := <-errs; err != nil || grade.Infrastructure != "cancelled" || grade.Passed {
		t.Fatal("cancellation not retained", grade, err)
	}
	// Simulate a process dying after the persisted reservation: the same
	// immutable operation must expose unknown/interrupted, never execute again.
	grade.Infrastructure = "interrupted"
	grade.GradingFinished = false
	grade.ResultHash = ""
	grade.ResultHash = Hash(grade)
	_ = m.put("development_grade", grade)
	again, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID)
	if err != nil || again.Infrastructure != "interrupted" || again.GradingFinished || calls.Load() != 1 {
		t.Fatal("uncertain grader effects replayed", again, err, calls.Load())
	}
}

func TestDevelopmentGradingIsBusyForCompletedExperiment(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	m, s, base := developmentManager(t, func(ctx context.Context, r GradeRequest) (Grade, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return Grade{}, ctx.Err()
		}
		return Grade{TrialID: r.Trial.ID, GraderHash: r.Task.GraderHash, Passed: true, Checks: []Check{{Name: "semantic", Passed: true}}}, nil
	})
	x, trial := startDevelopmentBaseline(t, m, s, base)
	x = completeDevelopment(t, m, x, base)
	if m.Active() {
		t.Fatal("finished experiment still active")
	}
	done := make(chan error, 1)
	go func() { _, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("grader not entered")
	}
	if !m.Active() {
		t.Fatal("grading was invisible to global admission")
	}
	if err := m.launch("other-work", func(context.Context) {}); err == nil {
		t.Fatal("work admitted during grading")
	}
	if _, err := m.Submit(PacketResponse{}); err == nil {
		t.Fatal("packet admitted during grading")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m.Active() {
		t.Fatal("grading gate not released")
	}
}

type pausedDevelopmentStore struct {
	Repository
	paused   atomic.Bool
	snapshot chan struct{}
	release  chan struct{}
}

func (s *pausedDevelopmentStore) Get(kind, id string) (Doc, error) {
	d, err := s.Repository.Get(kind, id)
	if kind == "harness_experiment" && s.paused.CompareAndSwap(false, true) {
		close(s.snapshot)
		<-s.release
	}
	return d, err
}

func TestDevelopmentGradeRechecksStatusAndSubmittedPacketUnderLock(t *testing.T) {
	for _, state := range []string{"running", "submitted-before-worker-wakes"} {
		t.Run(state, func(t *testing.T) {
			var calls atomic.Int32
			m, s, base := developmentManager(t, func(context.Context, GradeRequest) (Grade, error) { calls.Add(1); return Grade{}, nil })
			x, trial := startDevelopmentBaseline(t, m, s, base)
			underlying := m.config.Store
			paused := &pausedDevelopmentStore{Repository: underlying, snapshot: make(chan struct{}), release: make(chan struct{})}
			m.config.Store = paused
			done := make(chan error, 1)
			go func() { _, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID); done <- err }()
			<-paused.snapshot
			m.mu.Lock()
			m.jobs[x.ID] = func() {}
			if state == "running" {
				x.Status = "running"
				_ = m.put("experiment", x)
			} else {
				for _, id := range x.PacketIDs {
					var p Packet
					_ = m.get("packet", id, &p)
					p.Status = "submitted"
					p.Response = &PacketResponse{ID: p.ID}
					_ = m.put("packet", p)
				}
			}
			m.mu.Unlock()
			close(paused.release)
			if err := <-done; err == nil || calls.Load() != 0 {
				t.Fatal("stale packet/status permitted overlapping grading", err, calls.Load())
			}
			m.mu.Lock()
			delete(m.jobs, x.ID)
			m.mu.Unlock()
		})
	}
}

func TestDevelopmentAdmissionRefusalIsOpaqueAndDoesNotReachGrader(t *testing.T) {
	var calls atomic.Int32
	m, s, base := developmentManager(t, func(context.Context, GradeRequest) (Grade, error) { calls.Add(1); return Grade{}, nil })
	x, trial := startDevelopmentBaseline(t, m, s, base)
	m.config.ValidateDevelopmentGrade = func(context.Context) error { return errors.New("HIDDEN_SENTINEL active client") }
	grade, err := m.GradeDevelopmentTrial(t.Context(), x.ID, trial.ID)
	if err != nil || grade.Infrastructure != "admission" || grade.Passed || calls.Load() != 0 {
		t.Fatal("admission did not gate grading", grade, err, calls.Load())
	}
	raw, _ := json.Marshal(grade)
	if strings.Contains(string(raw), "HIDDEN_SENTINEL") {
		t.Fatal("admission payload leaked")
	}
}
