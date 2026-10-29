package harness

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowAndDelegateFrozenSelectionsBindRevisionIdentity(t *testing.T) {
	m, s, _ := testManager(t)
	s.ApprovedWorkflows = map[string]json.RawMessage{"review": json.RawMessage(`{"id":"review","nodes":[{"id":"a"}]}`)}
	s.ApprovedDelegateIDs = []string{"reader"}
	s.FrozenAgents = map[string]json.RawMessage{"reader": json.RawMessage(`{"digest":"frozen"}`)}
	c := Content{WorkflowID: "review", DelegateAgentIDs: []string{"reader"}}
	r, e := m.CreateRevision(s, "", c, Author{Kind: "baseline", Identity: s.AgentID})
	if e != nil {
		t.Fatal(e)
	}
	s.ApprovedWorkflows["review"] = json.RawMessage(`{"id":"review","nodes":[{"id":"changed"}]}`)
	r2, e := m.CreateRevision(s, "", c, Author{Kind: "baseline", Identity: s.AgentID})
	if e != nil || r.ID == r2.ID {
		t.Fatal("mutable graph reused revision identity", e)
	}
	for _, bad := range []Content{{WorkflowID: "unknown"}, {DelegateAgentIDs: []string{"admin"}}, {DelegateAgentIDs: []string{"reader", "reader"}}} {
		if ValidateContent(bad, s) == nil {
			t.Fatal("unapproved choice accepted", bad)
		}
	}
}
func TestLessonVersionsCompatibilityAndPacketSnapshot(t *testing.T) {
	m, s, _ := testManager(t)
	x, e := m.Start(s, Content{Instructions: "baseline"}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	l := Lesson{ID: "lesson", Version: 1, ExperimentID: "previous", WorkspaceID: s.WorkspaceID, AgentID: s.AgentID, TaskID: "node-ledger", Status: "provisional", Scope: "stack", Compatibility: Doc{"stack": "python", "modelDigest": s.ModelDigest, "runtimeVersion": s.RuntimeVersion, "methodVersion": MethodVersion, "capabilityHash": Hash(s.FrozenCapability)}, Cue: "When testing persistence", Guidance: "Use a fresh process", CreatedAt: m.now()}
	l, e = m.saveLessonVersion(l)
	if e != nil {
		t.Fatal(e)
	}
	first := l.VersionID
	lib, _ := m.lessonLibrary(x, "python-ledger")
	if len(lib) != 0 {
		t.Fatal("foreign provisional lesson leaked")
	}
	l, e = m.SetLessonStatus(l.ID, "accepted")
	if e != nil {
		t.Fatal(e)
	}
	history, _ := m.LessonHistory(l.ID)
	if len(history) != 2 || history[0].VersionID != first || history[0].Status != "provisional" || l.PreviousVersionID != first {
		t.Fatal("lesson history mutated", history)
	}
	lib, _ = m.lessonLibrary(x, "python-ledger")
	if len(lib) != 1 {
		t.Fatal("accepted compatible lesson not reusable")
	}
	p, e := m.MakePacket(x, "python-ledger", "proposal_inv", x.Spec.BaseRevisionID, "mission", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.SetLessonStatus(l.ID, "rejected"); e != nil {
		t.Fatal(e)
	}
	p2, e := m.MakePacket(x, "python-ledger", "proposal_inv", x.Spec.BaseRevisionID, "mission", nil, nil)
	if e != nil || p.ID != p2.ID || len(p2.Library) != 1 {
		t.Fatal("waiting packet library was replaced", e)
	}
	x.Spec.ModelDigest = "different"
	lib, _ = m.lessonLibrary(x, "python-ledger")
	if len(lib) != 0 {
		t.Fatal("incompatible lesson leaked")
	}
}
func TestInterruptedTrialIsEvidenceNotReplayAndExportExcludesSecrets(t *testing.T) {
	m, s, count := testManager(t)
	x, e := m.Start(s, Content{Instructions: "baseline"}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	trial, e := m.SolveTrial(context.Background(), x, "python-ledger", "base", x.Spec.BaseRevisionID)
	if e != nil {
		t.Fatal(e)
	}
	binary := []byte{0, 255, 128, 65}
	if e = os.WriteFile(filepath.Join(trial.Root, "binary.dat"), binary, 0600); e != nil {
		t.Fatal(e)
	}
	trial.OutputHash, e = treeHash(trial.Root)
	if e != nil {
		t.Fatal(e)
	}
	trial.Status = "interrupted"
	trial.Error = "process stopped"
	trial.Result.ToolCalls = []ToolObservation{{Data: Doc{"authorization": "secret", "journalPath": "/private/secret", "exitCode": 0}}}
	if e = m.put("trial", trial); e != nil {
		t.Fatal(e)
	}
	again, e := m.SolveTrial(context.Background(), x, "python-ledger", "base", x.Spec.BaseRevisionID)
	if e != nil || *count != 1 || again.Status != "interrupted" {
		t.Fatal("interrupted effects replayed", e, *count)
	}
	x.TrialIDs = []string{trial.ID}
	_ = m.put("experiment", x)
	export, e := m.Export(x.ID)
	if e != nil {
		t.Fatal(e)
	}
	second, e := m.Export(x.ID)
	if e != nil || second["payloadHash"] != export["payloadHash"] {
		t.Fatal("export payload is not reproducible", e)
	}
	rows := export["trials"].([]any)
	files := rows[0].(map[string]any)["files"].(map[string]any)
	artifact := files["binary.dat"].(map[string]any)
	decoded, e := base64.StdEncoding.DecodeString(artifact["content"].(string))
	if e != nil || string(decoded) != string(binary) || artifact["encoding"] != "base64" {
		t.Fatal("binary export changed bytes", e)
	}
	b, _ := json.Marshal(export)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "\"cases\"") || strings.Contains(string(b), "_harness_probe.py") {
		t.Fatalf("private export data leaked: secret=%v cases=%v probe=%v", strings.Contains(string(b), "secret"), strings.Contains(string(b), "\"cases\""), strings.Contains(string(b), "_harness_probe.py"))
	}
	if e = os.WriteFile(filepath.Join(trial.Root, "changed.txt"), []byte("tamper"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Export(x.ID); e == nil {
		t.Fatal("tampered output exported")
	}
	if _, e = m.SolveTrial(context.Background(), x, "python-ledger", "base", x.Spec.BaseRevisionID); e == nil {
		t.Fatal("interrupted hash overwritten")
	}
}
func TestEvaluationResumeSkipsRecordedTrialsAndDoesNotReplayInterrupted(t *testing.T) {
	m, s, count := testManager(t)
	x, e := m.Start(s, Content{Instructions: "baseline"}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	x.Status = "completed"
	x.CandidateID = x.Spec.BaseRevisionID
	_ = m.put("experiment", x)
	var task Task
	for _, v := range m.tasks {
		if v.Split == "validation" {
			task = v
			break
		}
	}
	m.tasks = map[string]Task{task.ID: task}
	x.Spec.FixtureDigest = m.fixtureDigest()
	_ = m.put("experiment", x)
	v := Evaluation{ID: "resume", ExperimentID: x.ID, CandidateID: x.CandidateID, BaselineID: x.Spec.BaseRevisionID, Status: "interrupted"}
	tr, e := m.SolveTrial(context.Background(), x, task.ID, "eval-resume-baseline-0", x.Spec.BaseRevisionID)
	if e != nil {
		t.Fatal(e)
	}
	v.Results = []EvalResult{{TaskID: task.ID, Split: task.Split, Repeat: 0, Arm: "baseline", TrialID: tr.ID}}
	tr2, e := m.SolveTrial(context.Background(), x, task.ID, "eval-resume-candidate-0", x.Spec.BaseRevisionID)
	if e != nil {
		t.Fatal(e)
	}
	tr2.Status = "interrupted"
	_ = m.put("trial", tr2)
	_ = m.put("evaluation", v)
	m.config.Grade = func(_ context.Context, t Task, tr Trial) (Grade, error) {
		return Grade{Passed: true, GraderHash: t.GraderHash, Checks: []Check{{Name: "public", Passed: true}}}, nil
	}
	if _, e = m.ResumeEvaluation(v.ID); e != nil {
		t.Fatal(e)
	}
	waitIdle(t, m)
	if e = m.get("evaluation", v.ID, &v); e != nil {
		t.Fatal(e)
	}
	if len(v.Results) != 6 || *count != 6 || v.Status != "completed" {
		t.Fatalf("resume duplicated/missed work results=%d solve=%d status=%s error=%s", len(v.Results), *count, v.Status, v.Error)
	}
	if v.Results[1].Grade.Passed {
		t.Fatal("interrupted trial counted as passing")
	}
}
func TestDeepFixturesRequireMultipleSourceFeatures(t *testing.T) {
	tasks, e := LoadFixtures()
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range tasks {
		if task.ID != "python-ledger" && task.ID != "node-inventory" && task.ID != "go-inventory" {
			continue
		}
		ref, e := ReferenceRepair(task.ID)
		if e != nil {
			t.Fatal(e)
		}
		changed := 0
		for path, body := range ref {
			if original, ok := task.Public[path]; ok && original != body {
				changed++
			}
		}
		if changed < 2 || task.Probe == nil {
			t.Fatal("deep fixture lacks multi-module feature and independent integration probe", task.ID)
		}
	}
}

func TestCancelDuringSolverCannotReopenExperiment(t *testing.T) {
	m, s, _ := testManager(t)
	entered := make(chan struct{})
	m.config.Solve = func(ctx context.Context, _ SolveRequest, _ func(Event)) (SolveResult, error) {
		close(entered)
		<-ctx.Done()
		return SolveResult{}, ctx.Err()
	}
	x, e := m.Start(s, Content{Instructions: "base"}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Advance(x.ID); e != nil {
		t.Fatal(e)
	}
	<-entered
	if e = m.Cancel(x.ID); e != nil {
		t.Fatal(e)
	}
	waitIdle(t, m)
	if e = m.get("experiment", x.ID, &x); e != nil {
		t.Fatal(e)
	}
	packets, _ := m.List("packet")
	if x.Status != "cancelled" || len(packets) != 0 {
		t.Fatal("cancelled campaign reopened", x.Status, packets)
	}
	if _, e = m.Advance(x.ID); e == nil {
		t.Fatal("cancelled campaign resumed")
	}
}

type lessonFailureStore struct {
	Repository
	fail bool
}

func (s *lessonFailureStore) Put(kind string, d Doc) error {
	if kind == "harness_lesson" && s.fail {
		s.fail = false
		return errors.New("simulated lesson-head write failure")
	}
	return s.Repository.Put(kind, d)
}
func TestSubmittedReflectionRetryRepairsMissingLessonHead(t *testing.T) {
	m, s, _ := testManager(t)
	x, e := m.Start(s, Content{Instructions: "base"}, Author{})
	if e != nil {
		t.Fatal(e)
	}
	trial, e := m.SolveTrial(context.Background(), x, "python-ledger", "base", x.Spec.BaseRevisionID)
	if e != nil {
		t.Fatal(e)
	}
	p, e := m.MakePacket(x, "python-ledger", "reflection", x.Spec.BaseRevisionID, "review", []string{trial.ID}, nil)
	if e != nil {
		t.Fatal(e)
	}
	r := PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: Author{Kind: "codex", Identity: "independent-reviewer"}, Verdict: "approved", Evidence: p.Evidence, Lessons: []LessonInput{{Cue: "Persisting a batch", Guidance: "Validate before committing", Evidence: p.Evidence}}}
	m.config.Store = &lessonFailureStore{Repository: m.config.Store, fail: true}
	if _, e = m.Submit(r); e == nil {
		t.Fatal("simulated write did not fail")
	}
	if _, e = m.Submit(r); e != nil {
		t.Fatal("idempotent packet retry did not finish lessons", e)
	}
	lessons, e := m.List("lesson")
	if e != nil || len(lessons) != 1 {
		t.Fatal("lesson head not recovered", e, lessons)
	}
	versions, e := m.LessonHistory(lessons[0]["id"].(string))
	if e != nil || len(versions) != 1 {
		t.Fatal("retry created multiple initial versions", e, versions)
	}
}
