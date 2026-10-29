package app

import (
	"encoding/json"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/workspace"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestHarnessWorkflowCapabilitiesCannotExpandParentOrModel(t *testing.T) {
	parent := FrozenAgent{Agent: Doc{"id": "parent"}, Profile: CapabilityProfile{Tools: []string{"read_file", "delegate_readonly"}, DelegateAgentIDs: []string{"child"}}}
	parent.Digest = contentDigest(parent)
	child := FrozenAgent{Agent: Doc{"id": "child", "model": "unapproved", "temperature": 1.9}, Profile: CapabilityProfile{Tools: []string{"read_file", "write_file", "delegate_readonly"}, DelegateAgentIDs: []string{"outsider"}}}
	child.Digest = contentDigest(child)
	b, _ := json.Marshal(child)
	spec := harness.Spec{Model: "fixed", Agent: Doc{"temperature": 0.2, "thinking": "off", "maxOutputTokens": 8192, "toolProtocol": "native"}, ApprovedDelegateIDs: []string{"child"}, FrozenAgents: map[string]json.RawMessage{"child": b}}
	p, agents, e := harnessSelectedAgents(spec, harness.Content{DelegateAgentIDs: []string{"child"}}, parent)
	if e != nil {
		t.Fatal(e)
	}
	req := engine.Request{Model: spec.Model, Tools: []engine.ToolDefinition{{Name: "read_file"}, {Name: "write_file"}, {Name: "delegate_readonly"}}}
	resolved, e := harnessWorkflowResolver(req, p, agents)("child")
	if e != nil {
		t.Fatal(e)
	}
	if resolved.Model != "fixed" || len(resolved.Tools) != 2 || resolved.Tools[0].Name != "read_file" {
		t.Fatal("workflow expanded parent authority", resolved.Tools)
	}
	for _, f := range agents {
		if str(f.Agent, "id") == "child" && (str(f.Agent, "model") != "fixed" || len(f.Profile.DelegateAgentIDs) != 0) {
			t.Fatal("child settings or delegation authority expanded", f)
		}
	}
	if _, e = harnessWorkflowResolver(req, p, agents)("unknown"); e == nil {
		t.Fatal("unfrozen agent resolved")
	}
	child.Agent["model"] = "tampered"
	b, _ = json.Marshal(child)
	spec.FrozenAgents["child"] = b
	if _, _, e = harnessSelectedAgents(spec, harness.Content{}, parent); e == nil {
		t.Fatal("tampered frozen agent accepted")
	}
}

func TestHarnessVerificationRequiresLatestSuccessfulCheckAfterEveryEffectAttempt(t *testing.T) {
	command := []string{"python3", "public_test.py"}
	check := func(start, end int, failed bool) harness.ToolObservation {
		r := harness.ToolObservation{Tool: "run_command", Arguments: Doc{"executable": "python3", "args": []string{"public_test.py"}, "readOnly": true}, Data: Doc{"exitCode": 0}, StartSequence: start, Sequence: end}
		if failed {
			r.Error = "command failed"
			r.Data["exitCode"] = 1
		}
		return r
	}
	write := harness.ToolObservation{Tool: "edit_file", Sequence: 2}
	if !harnessVerificationAfterEdits(command, []harness.ToolObservation{write, check(3, 4, false)}) {
		t.Fatal("ordered verification rejected")
	}
	for _, calls := range [][]harness.ToolObservation{
		{check(1, 3, false), write},
		{write, check(3, 4, false), check(5, 6, true)},
		{write, check(3, 4, false), {Tool: "run_command", Arguments: Doc{"readOnly": false}, Error: "exit 1 after partial write", Sequence: 6}},
		{write, check(3, 4, false), {Tool: "write_file", Error: "uncertain write", Sequence: 6}},
	} {
		if harnessVerificationAfterEdits(command, calls) {
			t.Fatal("stale verification accepted", calls)
		}
	}
}

func TestHarnessSolverExistingEditHasDurableCheckpoint(t *testing.T) {
	// Deterministic loopback model fixture; exercises the real shared solver and
	// tool callback, with no provider inference or container command execution.
	c := milestoneCore(t)
	root := filepath.Join(c.DataDir, "harness", "trials", "edit-fixture", "workspace")
	if e := os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	original := []byte("before\n")
	if e := os.WriteFile(filepath.Join(root, "source.txt"), original, 0600); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
			return
		}
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		if calls.Add(1) == 1 {
			response := Doc{"message": Doc{"tool_calls": []any{Doc{"id": "edit-existing", "function": Doc{"name": "edit_file", "arguments": Doc{"path": "source.txt", "oldText": "before", "newText": "after", "expectedHash": workspace.Hash(original)}}}}}, "done": true, "prompt_eval_count": 10, "eval_count": 8}
			_ = json.NewEncoder(w).Encode(response)
		} else {
			fmt.Fprintln(w, `{"message":{"content":"Edited the requested file."},"done":true,"eval_count":12,"prompt_eval_count":20}`)
		}
	}))
	defer server.Close()
	var e error
	c.client, e = inference.NewClient(server.URL)
	if e != nil {
		t.Fatal(e)
	}
	image := "sha256:09c68348396bd61f72b3570279e009741490473e40f8cc70517531696767578c"
	f := FrozenAgent{Agent: Doc{"id": "solver"}, Profile: CapabilityProfile{Version: 1, Name: "trial", ExecutionProfile: "docker", Tools: []string{"edit_file"}}, ExecutionImage: image}
	f.Digest = contentDigest(f)
	raw, _ := json.Marshal(f)
	spec := harness.Spec{AgentID: "solver", Model: "fixture", Agent: Doc{"maxSteps": 4, "thinking": "off", "temperature": 0.2, "maxOutputTokens": 8192, "toolProtocol": "native"}, ContextSize: 65536, BudgetSeconds: 60, ExecutorIdentity: image, FrozenCapability: raw, SolverTools: []string{"edit_file"}}
	result, e := c.executeHarnessSolver(t.Context(), harness.SolveRequest{TrialID: "edit-fixture", Root: root, Prompt: "Change before to after", Spec: spec}, func(ev harness.Event) {
		if ev.Type == "tool_validation_failed" || ev.Type == "error" {
			t.Log(ev.Type, ev.Title, ev.Detail, ev.Data)
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	if !result.TokenAccounting.Complete() || result.TokenAccounting.Attempts != 2 || result.Usage["prompt_eval_count"] != float64(30) || result.Usage["eval_count"] != float64(20) {
		t.Fatalf("shared solver lost terminal accounting: usage=%v accounting=%+v", result.Usage, result.TokenAccounting)
	}
	data, e := os.ReadFile(filepath.Join(root, "source.txt"))
	if e != nil || string(data) != "after\n" {
		t.Fatal("shared solver edit failed", e, string(data), result)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Error != "" || len(result.ToolCalls[0].Artifacts) < 2 {
		t.Fatal("checkpoint/change artifacts missing", result.ToolCalls)
	}
	entries, e := os.ReadDir(filepath.Join(c.DataDir, "harness", "trials", "edit-fixture", "artifacts"))
	if e != nil || len(entries) == 0 {
		t.Fatal("durable before-image absent", e)
	}
}

func TestLegacyComparisonPersistsFinalBoundCapabilityDigest(t *testing.T) {
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		fmt.Fprintln(w, `{"message":{"content":"Observed fixture."},"done":true}`)
	})
	callIntegration(t, h.core, "agent.save", Doc{"id": "candidate", "name": "Candidate", "instructions": "Inspect evidence", "mode": "read-only", "maxSteps": 4})
	spec := harness.Spec{WorkspaceID: h.workID, AgentID: "default", ApprovedSkills: map[string]json.RawMessage{}}
	revision, e := h.core.harness.CreateRevision(spec, "", harness.Content{Instructions: "Bound reviewed instructions"}, harness.Author{Kind: "baseline", Identity: "default"})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.core.Store.Put("harness_binding", Doc{"id": harness.Hash([]string{h.workID, "default"}), "workspaceId": h.workID, "agentId": "default", "revisionId": revision.ID}); e != nil {
		t.Fatal(e)
	}
	value, e := h.core.startEval(Doc{"workspaceId": h.workID, "baselineAgentId": "default", "candidateAgentId": "candidate", "tasks": []string{"Describe this fixture"}})
	if e != nil {
		t.Fatal(e)
	}
	d := obj(value)
	raw, _ := json.Marshal(d["frozenCapabilities"])
	var agents map[string]FrozenAgent
	if e = json.Unmarshal(raw, &agents); e != nil {
		t.Fatal(e)
	}
	for id, f := range agents {
		digest := f.Digest
		f.Digest = ""
		if contentDigest(f) != digest {
			t.Fatal("persisted capability digest does not bind final selection", id)
		}
	}
	waitIntegration(t, func() bool { row, _ := h.core.Store.Get("experiment", str(d, "id")); return row["status"] != "running" })
	row, e := h.core.Store.Get("experiment", str(d, "id"))
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range row["results"].([]any) {
		result := obj(v)
		req := obj(result["request"])
		if req["capabilityId"] != agents[str(result, "agentId")].Digest {
			t.Fatal("request retained pre-binding capability identity", req)
		}
	}
}
