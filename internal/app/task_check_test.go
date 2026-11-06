package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/inference"
)

type taskCheckAdmission struct {
	request  engine.Request // frozen checkpoint request (what resume decodes)
	outgoing inference.ChatRequest
	manifest ContextManifest // context.get view
}

// holdFirst keeps the first generation open until the run is cancelled, so
// that run deterministically ends incomplete and the next turn gets recovery.
func newTaskCheckHarness(t *testing.T, holdFirst bool) (*integrationHarness, func() []inference.ChatRequest) {
	var mu sync.Mutex
	var seen []inference.ChatRequest
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		mu.Lock()
		seen = append(seen, req)
		n := len(seen)
		mu.Unlock()
		if holdFirst && n == 1 {
			<-r.Context().Done()
			return
		}
		fmt.Fprintln(w, `{"done":true,"message":{"content":"Finished."}}`)
	})
	return h, func() []inference.ChatRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]inference.ChatRequest{}, seen...)
	}
}

// Only admission and the first generation are asserted; the run is then
// stopped so completion-time engine behavior cannot affect these checks.
func admitTaskCheckRun(t *testing.T, h *integrationHarness, seen func() []inference.ChatRequest, contract Doc) taskCheckAdmission {
	t.Helper()
	before := len(seen())
	p := Doc{"sessionId": h.sessionID, "prompt": "Fix the calculator"}
	if contract != nil {
		p["verificationContract"] = contract
	}
	id := str(callIntegration(t, h.core, "run.start", p), "id")
	waitIntegration(t, func() bool { return len(seen()) > before })
	_, _ = h.core.Call(t.Context(), "run.cancel", Doc{"id": id})
	waitIntegration(t, func() bool {
		r, err := h.core.Store.Get("run", id)
		return err == nil && !active(str(r, "status")) && !h.core.workspaceBusy(h.workID)
	})
	cp, err := h.core.Store.Get("checkpoint", id)
	if err != nil {
		t.Fatal(err)
	}
	var out taskCheckAdmission
	b, _ := json.Marshal(cp["request"])
	if err = json.Unmarshal(b, &out.request); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(callIntegration(t, h.core, "context.get", Doc{"runId": id}))
	if err = json.Unmarshal(b, &out.manifest); err != nil {
		t.Fatal(err)
	}
	out.outgoing = seen()[before]
	return out
}

func (a taskCheckAdmission) system() string {
	parts := []string{}
	for _, m := range a.outgoing.Messages {
		if m.Role == "system" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func (a taskCheckAdmission) effectiveInstructions(t *testing.T) string {
	t.Helper()
	for _, s := range a.manifest.Sources {
		if s.Kind == "effective_instructions" {
			return s.Content
		}
	}
	t.Fatal("context.get has no effective instructions record")
	return ""
}

func (a taskCheckAdmission) declares(name string) bool {
	for _, tool := range a.outgoing.Tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}

func TestTaskCheckOrdinaryRunWithContractInstructsAndEnablesFeedback(t *testing.T) {
	h, seen := newTaskCheckHarness(t, false)
	a := admitTaskCheckRun(t, h, seen, Doc{"command": []string{"python3", "-m", "pytest", "tests/test calc.py"}, "workingDirectory": "pkg", "protectedPaths": []string{"tests/test_calc.py", "README.md"}})
	if !a.request.VerificationRequired || !a.request.VerificationFeedback {
		t.Fatalf("checkpoint request required=%v feedback=%v", a.request.VerificationRequired, a.request.VerificationFeedback)
	}
	want := []string{
		"Task check: the user declared a check for this task: python3 -m pytest 'tests/test calc.py' (working directory: pkg).",
		"call verify_task (it takes no arguments)",
		"the host runs it again and tells you if it fails",
		"Do not modify these protected paths: tests/test_calc.py, README.md.",
	}
	for _, s := range want {
		if !strings.Contains(a.request.Instructions, s) {
			t.Fatalf("frozen instructions lack %q:\n%s", s, a.request.Instructions)
		}
	}
	if !strings.HasPrefix(a.request.Instructions, defaultInstructions) || strings.Index(a.request.Instructions, "Task check:") <= len(defaultInstructions) {
		t.Fatal("task check must follow the agent instructions")
	}
	if !strings.Contains(a.system(), want[0]) || !strings.Contains(a.system(), want[3]) || !a.declares("verify_task") {
		t.Fatal("model request lacks the task check or verify_task", a.system())
	}
	if a.effectiveInstructions(t) != a.request.Instructions {
		t.Fatal("context.get effective instructions differ from the frozen request")
	}
	next := admitTaskCheckRun(t, h, seen, Doc{"command": []string{"/usr/bin/true"}})
	if strings.Count(next.system(), "Task check:") != 1 || strings.Contains(next.request.Instructions, "pytest") {
		t.Fatal("a later turn must carry only its own task check", next.system())
	}
}

func TestTaskCheckAbsentWithoutContract(t *testing.T) {
	h, seen := newTaskCheckHarness(t, false)
	a := admitTaskCheckRun(t, h, seen, nil)
	if a.request.VerificationRequired || a.request.VerificationFeedback {
		t.Fatalf("checkpoint request required=%v feedback=%v", a.request.VerificationRequired, a.request.VerificationFeedback)
	}
	if strings.Contains(a.request.Instructions, "Task check") || strings.Contains(a.system(), "Task check") || strings.Contains(a.effectiveInstructions(t), "Task check") || a.declares("verify_task") {
		t.Fatal("task check exposed without a declared contract")
	}
}

func TestTaskCheckPrecedesRecoveryContext(t *testing.T) {
	h, seen := newTaskCheckHarness(t, true)
	admitTaskCheckRun(t, h, seen, nil)
	a := admitTaskCheckRun(t, h, seen, Doc{"command": []string{"/usr/bin/true"}})
	check, recovery := strings.Index(a.request.Instructions, "Task check:"), strings.Index(a.request.Instructions, "Host recovery context")
	if check < 0 || recovery < 0 || check > recovery {
		t.Fatalf("task check=%d recovery=%d:\n%s", check, recovery, a.request.Instructions)
	}
	if !strings.Contains(a.system(), "Task check:") || !strings.Contains(a.system(), "Host recovery context") {
		t.Fatal("model request lost the task check or recovery text")
	}
}

// The workflow runtime clears feedback for every node and the outer workflow
// agent has no host verifier, so forcing feedback here could only re-drive the
// whole graph with reminders. Workflows keep their configured value.
func TestTaskCheckWorkflowRunKeepsConfiguredFeedback(t *testing.T) {
	h := newIntegrationHarness(t, func(http.ResponseWriter, *http.Request, inference.ChatRequest) {
		t.Error("output-only workflow requested inference")
	})
	wf := callIntegration(t, h.core, "workflow.save", Doc{"name": "Checked", "nodes": []Doc{{"id": "emit", "type": "output", "prompt": "done"}}, "edges": []Doc{}})
	run := callIntegration(t, h.core, "workflow.run", Doc{"id": wf["id"], "sessionId": h.sessionID, "prompt": "seed", "verificationContract": Doc{"command": []string{"/usr/bin/true"}}})
	id := str(run, "id")
	finishedIntegration(t, h.core, id, "completed")
	cp, err := h.core.Store.Get("checkpoint", id)
	if err != nil {
		t.Fatal(err)
	}
	var req engine.Request
	b, _ := json.Marshal(cp["request"])
	if err = json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	if !req.VerificationRequired || req.VerificationFeedback || !strings.Contains(req.Instructions, "Task check:") || strings.Contains(req.Instructions, "host runs it again") {
		t.Fatalf("workflow admission required=%v feedback=%v:\n%s", req.VerificationRequired, req.VerificationFeedback, req.Instructions)
	}
	events, err := h.core.Store.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	started := 0
	for _, ev := range events {
		if ev["type"] == "verification_feedback" {
			t.Fatal("workflow re-driven by completion feedback", ev)
		}
		if ev["type"] == "node_started" && ev["nodeId"] == "emit" {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("workflow node started %d times", started)
	}
}

func TestTaskCheckInstructionText(t *testing.T) {
	v := &execution.VerificationContract{Command: []string{"/usr/bin/true"}}
	text := taskCheckInstructions(v, true, true)
	if !strings.Contains(text, "this task: /usr/bin/true (working directory: the workspace root).") || strings.Contains(text, "protected") || strings.Contains(text, "regression") {
		t.Fatal(text)
	}
	if len(text) > 500 {
		t.Fatalf("task check is %d bytes", len(text))
	}
	// A repair round leaves room for an edit and an answer only, so the text
	// must not make a third verify_task call look required after a failure.
	if !strings.Contains(text, "fix the cause and finish without calling verify_task again, because the host re-runs the check.") {
		t.Fatal("feedback instructions contradict the repair feedback", text)
	}
	if text := taskCheckInstructions(v, true, false); strings.Contains(text, "host runs it again") || strings.Contains(text, "host re-runs") {
		t.Fatal("completion feedback promised without host feedback")
	}
	v = &execution.VerificationContract{Command: []string{"sh", "-c", "it's $HOME; ok", ""}, WorkingDirectory: ".", RequireRegressionTest: true}
	text = taskCheckInstructions(v, true, true)
	if !strings.Contains(text, `this task: sh -c 'it'\''s $HOME; ok' '' (working directory: the workspace root).`) || !strings.Contains(text, "regression test") {
		t.Fatal(text)
	}
	v = &execution.VerificationContract{Command: []string{"go", strings.Repeat("x", 10000)}}
	if text = taskCheckInstructions(v, true, true); len(text) > 3000 || !strings.Contains(text, "[truncated]") {
		t.Fatalf("unbounded task check rendering: %d bytes", len(text))
	}
}

func TestTaskCheckQualifiedProfileKeepsFrozenFeedback(t *testing.T) {
	v := &execution.VerificationContract{Command: []string{"/usr/bin/true"}}
	for _, frozen := range []bool{false, true} {
		req := engine.Request{Instructions: "qualified", VerificationFeedback: frozen}
		admitTaskCheck(&req, v, true, false)
		if !req.VerificationRequired || req.VerificationFeedback != frozen || strings.Contains(req.Instructions, "host runs it again") != frozen {
			t.Fatalf("frozen=%v request=%+v", frozen, req)
		}
		// Workflows (qualified or not) keep their value and never get the host check.
		for _, qualified := range []bool{false, true} {
			req = engine.Request{Instructions: "workflow", VerificationFeedback: frozen}
			admitTaskCheck(&req, v, qualified, true)
			if !req.VerificationRequired || req.VerificationFeedback != frozen || strings.Contains(req.Instructions, "host runs it again") {
				t.Fatalf("workflow qualified=%v frozen=%v request=%+v", qualified, frozen, req)
			}
		}
	}
	req := engine.Request{Instructions: "ordinary", ReadOnly: true}
	admitTaskCheck(&req, v, false, false)
	if !req.VerificationFeedback || strings.Contains(req.Instructions, "host runs it again") {
		t.Fatal("read-only runs receive no completion feedback, so none is promised", req.Instructions)
	}
}

// A toolSet that hides verify_task gets neither the host check nor reminders,
// so admission must not force feedback or tell the model to call it.
func TestTaskCheckWithoutFocusedVerifyTaskNamesNoTool(t *testing.T) {
	v := &execution.VerificationContract{Command: []string{"go", "test", "./..."}, ProtectedPaths: []string{"calc_test.go"}}
	for _, set := range []string{"inspect", "research"} {
		req := engine.Request{Instructions: "ordinary", HarnessMechanisms: engine.HarnessMechanisms{ToolSet: set}}
		admitTaskCheck(&req, v, false, false)
		if !req.VerificationRequired || req.VerificationFeedback || strings.Contains(req.Instructions, "verify_task") || strings.Contains(req.Instructions, "host runs it again") {
			t.Fatalf("%s admission feedback=%v:\n%s", set, req.VerificationFeedback, req.Instructions)
		}
		for _, want := range []string{"Task check: the user declared a check for this task: go test ./... (working directory: the workspace root).", "cannot execute it, so do not claim it passed", "Do not modify these protected paths: calc_test.go."} {
			if !strings.Contains(req.Instructions, want) {
				t.Fatalf("%s admission lacks %q:\n%s", set, want, req.Instructions)
			}
		}
		// A qualified profile keeps its evaluated flag, but the text still
		// cannot promise a check its catalogue cannot run.
		req = engine.Request{VerificationFeedback: true, HarnessMechanisms: engine.HarnessMechanisms{ToolSet: set}}
		admitTaskCheck(&req, v, true, false)
		if !req.VerificationFeedback || strings.Contains(req.Instructions, "verify_task") || strings.Contains(req.Instructions, "host runs it again") {
			t.Fatalf("qualified %s admission:\n%s", set, req.Instructions)
		}
	}
	for _, set := range []string{"", "all", "coding"} {
		req := engine.Request{HarnessMechanisms: engine.HarnessMechanisms{ToolSet: set}}
		admitTaskCheck(&req, v, false, false)
		if !req.VerificationFeedback || !strings.Contains(req.Instructions, "call verify_task") || !strings.Contains(req.Instructions, "the host runs it again") {
			t.Fatalf("toolSet %q lost the check:\n%s", set, req.Instructions)
		}
	}
	if text := taskCheckInstructions(v, false, true); strings.Contains(text, "verify_task") || strings.Contains(text, "host runs it again") {
		t.Fatal("uncallable check promised a tool or completion feedback", text)
	}
}

// Only a run that changed the workspace gets the host completion check. A
// question finishes with its answer and no approval prompt; after a write the
// host asks to run the frozen check, and declining it leaves the run
// unverified with a journal entry instead of repair feedback.
func TestTaskCheckHostCheckRunsOnlyAfterAChange(t *testing.T) {
	contract := Doc{"command": []string{"/usr/bin/true"}}
	approvalsFor := func(t *testing.T, h *integrationHarness, id string) []Doc {
		all, err := h.core.Store.List("approval")
		if err != nil {
			t.Fatal(err)
		}
		var out []Doc
		for _, a := range all {
			if a["runId"] == id {
				out = append(out, a)
			}
		}
		return out
	}
	t.Run("question", func(t *testing.T) {
		h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
			fmt.Fprintln(w, `{"done":true,"message":{"content":"Division by zero on line 3."}}`)
		})
		id := str(callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Why does the calculator fail?", "verificationContract": contract}), "id")
		finished := finishedIntegration(t, h.core, id, "completed")
		if approvals := approvalsFor(t, h, id); len(approvals) != 0 || finished["verification"] != "unverified" {
			t.Fatalf("unchanged run was checked: approvals=%v run=%v", approvals, finished)
		}
	})
	t.Run("change", func(t *testing.T) {
		h := newIntegrationHarness(t, writeFixture)
		id := str(callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Fix the calculator", "verificationContract": contract}), "id")
		write := pendingIntegration(t, h.core, id)
		if write["tool"] != "write_file" {
			t.Fatal("expected the model's write first", write)
		}
		callIntegration(t, h.core, "approval.respond", Doc{"id": write["id"], "allow": true})
		check := pendingIntegration(t, h.core, id)
		if check["tool"] != "verify_task" {
			t.Fatal("host completion check was not routed through verify_task approval", check)
		}
		callIntegration(t, h.core, "approval.respond", Doc{"id": check["id"], "allow": false})
		if finished := finishedIntegration(t, h.core, id, "completed"); finished["verification"] != "unverified" {
			t.Fatal("declined check counted as verification", finished)
		}
		events, err := h.core.Store.Events(id, 0)
		if err != nil {
			t.Fatal(err)
		}
		journal := 0
		for _, ev := range events {
			if ev["type"] == "verification_feedback" {
				journal++
				if data := obj(ev["data"]); data["delivered"] != false || data["outcome"] != "not_run" {
					t.Fatal("declined check was not journaled as undelivered", ev)
				}
			}
		}
		if journal != 1 {
			t.Fatalf("verification journal entries=%d", journal)
		}
	})
}
