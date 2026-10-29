package harness

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu    sync.Mutex
	rows  map[string]Doc
	order []string
}

func (s *memoryStore) Get(k, id string) (Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.rows[k+"/"+id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return asDoc(d), nil
}
func (s *memoryStore) Put(k string, d Doc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := k + "/" + d["id"].(string)
	if s.rows == nil {
		s.rows = map[string]Doc{}
	}
	if _, ok := s.rows[key]; !ok {
		s.order = append(s.order, key)
	}
	s.rows[key] = asDoc(d)
	return nil
}
func (s *memoryStore) List(k string) ([]Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Doc{}
	for _, key := range s.order {
		if len(key) > len(k) && key[:len(k)+1] == k+"/" {
			out = append(out, asDoc(s.rows[key]))
		}
	}
	return out, nil
}

// The in-memory repository provides the same metadata contract as SQLite.
// Production has no fallback that decodes complete trace/result records.
func (s *memoryStore) ListHarnessMetadata(kind string) ([]Doc, error) {
	rows, e := s.List("harness_" + kind)
	if e != nil {
		return nil, e
	}
	for _, d := range rows {
		if kind == "evaluation" {
			d["recordedResults"] = lenSlice(d["results"])
		}
		for _, key := range []string{"context", "result", "response", "content", "results", "library", "approvedWorkflow"} {
			delete(d, key)
		}
		if spec, ok := d["spec"].(map[string]any); ok {
			for _, key := range []string{"agent", "frozenCapability", "frozenCapabilities", "approvedSkills", "skills", "approvedWorkflows", "workflows", "frozenAgents", "agents"} {
				delete(spec, key)
			}
		}
	}
	return rows, nil
}
func testManager(t *testing.T) (*Manager, Spec, *int) {
	t.Helper()
	count := 0
	tasks, e := LoadFixtures()
	if e != nil {
		t.Fatal(e)
	}
	m, e := Open(Config{Root: t.TempDir(), Store: &memoryStore{}, Tasks: tasks, Solve: func(_ context.Context, r SolveRequest, _ func(Event)) (SolveResult, error) {
		count++
		if _, e := os.Stat(filepath.Join(r.Root, "task.json")); !os.IsNotExist(e) {
			t.Fatal("grader manifest leaked")
		}
		return SolveResult{Content: "observed output", Usage: Doc{"eval_count": 10}, ToolCalls: []ToolObservation{}}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	s := Spec{Mode: "native", WorkspaceID: "workspace", AgentID: "agent", TaskIDs: []string{"python-ledger"}, Model: "gemma", ModelDigest: "sha256:abc", RuntimeVersion: "0.40.11", ExecutorIdentity: "repo@sha256:pinned", ApprovedSkills: map[string]json.RawMessage{}}
	return m, s, &count
}
func waitIdle(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for m.Active() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.Active() {
		t.Fatal("job did not finish")
	}
}
func TestNativeFourRolloutsThreeStagesAndPacketIdentity(t *testing.T) {
	m, s, count := testManager(t)
	x, e := m.Start(s, Content{Instructions: "base"}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Advance(x.ID); e != nil {
		t.Fatal(e)
	}
	waitIdle(t, m)
	for round := 0; round < 4; round++ {
		packets, _ := m.config.Store.List("harness_packet")
		for _, d := range packets {
			p, _ := decode[Packet](d)
			if p.Response != nil {
				continue
			}
			a := Author{Kind: "codex", Identity: "proposal-author"}
			c := &Content{Instructions: "improved " + p.Stage}
			r := PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: a, Content: c, Notes: "observed trace"}
			if p.Stage == "reflection" {
				r.Content = nil
				r.AuthoredBy.Identity = "independent-reviewer"
				r.Verdict = "approved"
				r.Evidence = p.Evidence
			}
			bad := r
			bad.RequestHash = "stale"
			if _, e = m.Submit(bad); e == nil {
				t.Fatal("stale packet accepted")
			}
			if _, e = m.Submit(r); e != nil {
				t.Fatal(e)
			}
			if _, e = m.Submit(r); e != nil {
				t.Fatal("idempotent response rejected", e)
			}
			r.Notes = "changed"
			if _, e = m.Submit(r); e == nil {
				t.Fatal("response overwritten")
			}
		}
		current, _ := m.Get("experiment", x.ID)
		if current["status"] == "completed" {
			break
		}
		if _, e = m.Advance(x.ID); e != nil {
			t.Fatal(e)
		}
		waitIdle(t, m)
	}
	var got Experiment
	if e = m.get("experiment", x.ID, &got); e != nil {
		t.Fatal(e)
	}
	if got.Status != "completed" || got.Rollouts != 4 || got.ImprovementStages != 3 || *count != 4 {
		t.Fatalf("accounting: %+v calls%d", got, *count)
	}
	trial, e := m.trial(got.TrialIDs[0])
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(trial.Root, "tampered.txt"), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = m.SolveTrial(context.Background(), got, trial.TaskID, trial.Label, trial.RevisionID); e == nil {
		t.Fatal("changed output reused")
	}
}
func TestImmutablePermissionSurfaceAndAuthorSeparation(t *testing.T) {
	m, s, _ := testManager(t)
	if _, e := m.Start(s, Content{SkillIDs: []string{"unapproved"}}, Author{}); e == nil {
		t.Fatal("unapproved skill accepted")
	}
	x, e := m.Start(s, Content{Instructions: "base"}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	p, e := m.MakePacket(x, "python-ledger", "proposal_inv", x.Spec.BaseRevisionID, "mission", []string{"evidence"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	r := PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: Author{Kind: "gemma", Identity: "solver"}, Content: &Content{}}
	if _, e = m.Submit(r); e == nil {
		t.Fatal("solver accepted as improver")
	}
	r.AuthoredBy = Author{Kind: "codex", Identity: "author"}
	if _, e = m.Submit(r); e != nil {
		t.Fatal(e)
	}
	review, e := m.MakePacket(x, "python-ledger", "reflection", x.Spec.BaseRevisionID, "review", []string{"evidence"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Submit(PacketResponse{ID: review.ID, RequestHash: review.RequestHash, AuthoredBy: r.AuthoredBy, Verdict: "approved", Evidence: []string{"evidence"}}); e == nil {
		t.Fatal("author graded own candidate")
	}
}
func TestFixtureSplitAndOracleSeparation(t *testing.T) {
	tasks, e := LoadFixtures()
	if e != nil {
		t.Fatal(e)
	}
	counts := map[string]int{}
	for _, task := range tasks {
		counts[task.Stack]++
		counts[task.Split]++
		if len(task.Cases) < 5 {
			t.Fatal("missing cases")
		}
		for p := range task.Public {
			if p == "reference.json" || p == "task.json" {
				t.Fatal("oracle in public files")
			}
		}
	}
	for _, key := range []string{"python", "node", "go", "development", "validation", "heldout"} {
		if counts[key] != 6 {
			t.Fatal(key, counts[key])
		}
	}
}
func gateFixture() ([]Task, []EvalResult) {
	tasks := []Task{{ID: "val", Split: "validation", GraderHash: "h"}, {ID: "held", Split: "heldout", GraderHash: "h"}}
	rows := []EvalResult{}
	for _, task := range tasks {
		for repeat := 0; repeat < 3; repeat++ {
			for _, arm := range []string{"baseline", "candidate"} {
				passed := arm == "candidate" || repeat > 0
				rows = append(rows, EvalResult{TaskID: task.ID, Split: task.Split, Repeat: repeat, Arm: arm, TrialID: task.ID + arm, Grade: Grade{Passed: passed, GraderHash: "h", Checks: []Check{{Name: "integrity: data", Passed: true, Critical: true}, {Name: "solver completed", Passed: passed}}}, DurationMS: 100, Tokens: 100, TokenAccounting: inference.NewTokenAccounting(1, 1, 1)})
			}
		}
	}
	return tasks, rows
}
func TestPromotionGateRetainsFailuresAndRequiresHeldoutImprovement(t *testing.T) {
	tasks, rows := gateFixture()
	g := EvaluateGate(rows, tasks)
	if !g.Eligible || g.BaselineSuccesses != 2 || g.CandidateSuccesses != 3 {
		t.Fatalf("valid repair rejected %+v", g)
	}
	for i := range rows {
		if rows[i].Split == "heldout" && rows[i].Arm == "baseline" {
			rows[i].Grade.Passed = true
		}
	}
	if EvaluateGate(rows, tasks).Eligible {
		t.Fatal("validation-only gain promoted")
	}
	tasks, rows = gateFixture()
	rows[0].Grade.Checks[0].Passed = false
	if EvaluateGate(rows, tasks).Eligible {
		t.Fatal("integrity failure promoted")
	}
	tasks, rows = gateFixture()
	rows = rows[:len(rows)-1]
	if EvaluateGate(rows, tasks).Eligible {
		t.Fatal("missing repeat promoted")
	}
	tasks, rows = gateFixture()
	for i := range rows {
		if rows[i].Arm == "candidate" {
			rows[i].Tokens = 121
		}
	}
	if EvaluateGate(rows, tasks).Eligible {
		t.Fatal("token regression promoted")
	}
}
func TestHeldoutExposureAcrossExperiments(t *testing.T) {
	m, s, _ := testManager(t)
	m.config.Grade = func(context.Context, Task, Trial) (Grade, error) { return Grade{}, errors.New("test stop") }
	x, e := m.Start(s, Content{}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	x.Status = "completed"
	x.CandidateID = x.Spec.BaseRevisionID
	if e = m.put("experiment", x); e != nil {
		t.Fatal(e)
	}
	v, e := m.StartEvaluation(x.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = m.Cancel(v.ID)
	waitIdle(t, m)
	y, e := m.Start(s, Content{}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	y.Status = "completed"
	y.CandidateID = y.Spec.BaseRevisionID
	_ = m.put("experiment", y)
	if _, e = m.StartEvaluation(y.ID); e == nil {
		t.Fatal("heldout pool reused as unseen")
	}
}

func lenSlice(v any) int { values, _ := v.([]any); return len(values) }
