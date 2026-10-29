package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/tools"
)

// A recording executor intentionally executes no process. Its result echoes
// the actual typed request, so it cannot manufacture a passing readOnly value.
type harnessWireExecutor struct {
	requests []execution.Request
}

func (*harnessWireExecutor) Description() string { return "recording fixture; no process execution" }
func (x *harnessWireExecutor) Run(_ context.Context, req execution.Request) (execution.Result, error) {
	x.requests = append(x.requests, req)
	return execution.Result{ExitCode: 0, ReadOnly: req.ReadOnly, WorkingDirectory: req.WorkingDirectory, Stdout: "fixture verification receipt\n", CommandID: "fixture-command", Publication: "unchanged"}, nil
}

func TestHarnessRunCommandReadOnlySchemaAndNativeTransport(t *testing.T) {
	c := milestoneCore(t)
	const trialID = "readonly-schema-fixture"
	root := filepath.Join(c.DataDir, "harness", "trials", trialID, "workspace")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	requests := make(chan inference.ChatRequest, 8)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
			return
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad fixture request", 400)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "application/x-ndjson")
		if calls.Add(1) == 2 {
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"readonly-wire-call","function":{"name":"run_command","arguments":{"executable":"python3","args":["-m","unittest"],"workingDirectory":".","readOnly":true}}}]},"done":true,"prompt_eval_count":100,"eval_count":12}`)
		} else {
			fmt.Fprintln(w, `{"message":{"content":"Fixture response complete."},"done":true,"prompt_eval_count":130,"eval_count":8}`)
		}
	}))
	defer server.Close()
	var err error
	c.client, err = inference.NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	image := "sha256:" + strings.Repeat("d", 64)
	frozen := FrozenAgent{Agent: Doc{"id": "solver"}, Profile: CapabilityProfile{Version: 1, Name: "fixture", ExecutionProfile: "docker", Tools: []string{"run_command"}}, ExecutionImage: image}
	frozen.Digest = contentDigest(frozen)
	raw, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	spec := harness.Spec{AgentID: "solver", Model: "wire-fixture", Agent: Doc{"maxSteps": 4, "thinking": "off", "temperature": 0.2, "maxOutputTokens": 8192, "toolProtocol": "native"}, ContextSize: 65536, BudgetSeconds: 60, ExecutorIdentity: image, FrozenCapability: raw, SolverTools: []string{"run_command"}}
	// Capture the actual Harness schema. The fixture returns a final answer here,
	// so the production solver never calls Docker or starts a command.
	result, err := c.executeHarnessSolver(t.Context(), harness.SolveRequest{TrialID: trialID, Root: root, Prompt: "Inspect the command declaration.", Spec: spec}, nil)
	if err != nil || result.Incomplete || len(result.ToolCalls) != 0 || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
	request := <-requests
	if len(request.Tools) != 1 || request.Tools[0].Function.Name != "run_command" {
		t.Fatal("production Harness did not expose exactly its allowed command", request.Tools)
	}
	definition := request.Tools[0].Function
	properties, ok := definition.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing command properties", definition.Parameters)
	}
	property, ok := properties["readOnly"].(map[string]any)
	if !ok || property["type"] != "boolean" {
		t.Fatal("readOnly missing, incorrectly cased, or not boolean", properties)
	}
	for key := range properties {
		if strings.EqualFold(key, "readOnly") && key != "readOnly" {
			t.Fatal("unexpected casing alias", key)
		}
	}
	for _, field := range definition.Parameters["required"].([]any) {
		if field == "readOnly" {
			t.Fatal("readOnly unexpectedly changed from an optional parameter")
		}
	}
	if !strings.Contains(definition.Description, "readOnly:true") || !strings.Contains(fmt.Sprint(property["description"]), "Default false") {
		t.Fatal("effective description lost read-only/default guidance", definition)
	}

	// Continue through the same native engine/ADK and governed tools.Manager
	// using the exact captured declaration. Only the executor is replaced:
	// a real Docker command would conflict with the separate live qualification.
	x := &harnessWireExecutor{}
	var approvals []execution.Approval
	manager, err := tools.New(root, tools.Options{Mode: "approval", Executor: x, Approve: func(_ context.Context, a execution.Approval) error {
		approvals = append(approvals, a)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	var callback engine.ToolCall
	var receipt map[string]any
	var completed engine.Event
	output, err := engine.New(c.client).Run(t.Context(), engine.Request{Model: "wire-fixture", Prompt: "Run the fixture verification read-only.", Think: false, MaxSteps: 4, CapabilityID: frozen.Digest, Tools: []engine.ToolDefinition{{Name: definition.Name, Description: definition.Description, Parameters: definition.Parameters}}}, func(event engine.Event) {
		if event.Type == "tool_completed" {
			completed = event
		}
	}, func(ctx context.Context, call engine.ToolCall) (engine.ToolResult, error) {
		callback = call
		if _, err := frozen.Authorize(ctx, call); err != nil {
			return engine.ToolResult{}, err
		}
		value, err := manager.Execute(ctx, call.Name, call.Arguments)
		receipt = obj(value.Data)
		return engine.ToolResult{Content: value.Content, Data: receipt, IsError: err != nil}, err
	})
	if err != nil || output.Incomplete || calls.Load() != 3 {
		t.Fatal(output, err, calls.Load())
	}
	if callback.Name != "run_command" || callback.Arguments["readOnly"] != true || callback.ReadOnly {
		t.Fatal("native boolean was lost or masked by a host read-only override", callback)
	}
	if len(approvals) != 1 || approvals[0].Arguments["readOnly"] != true {
		t.Fatal("approval lost the true boolean", approvals)
	}
	if len(x.requests) != 1 || !x.requests[0].ReadOnly || x.requests[0].Executable != "python3" || !reflect.DeepEqual(x.requests[0].Args, []string{"-m", "unittest"}) {
		t.Fatal("executor received different typed parameters", x.requests)
	}
	if receipt["readOnly"] != true || receipt["commandId"] != "fixture-command" || receipt["exitCode"] != float64(0) {
		t.Fatal("command receipt lost typed readOnly", receipt)
	}
	eventData := obj(completed.Data)
	if obj(eventData["data"])["readOnly"] != true || eventData["callId"] != callback.ID {
		t.Fatal("completed event lost receipt/identity", eventData)
	}
	<-requests // The engine's initial tool request.
	continuation := <-requests
	toolResponses := 0
	for _, message := range continuation.Messages {
		if message.Role == "tool" && message.ToolName == "run_command" {
			toolResponses++
			if !strings.Contains(message.Content, "readOnly: true\n") {
				t.Fatal("model-facing receipt lost readOnly", message)
			}
		}
	}
	if toolResponses != 1 {
		t.Fatal("expected one command receipt in continuation", continuation.Messages)
	}
	t.Log("Actual Harness outbound schema contains optional readOnly with exact casing and boolean type. A native fake-model readOnly:true survives ADK, frozen authorization, manager approval, typed executor request, completion receipt/event and next Ollama tool message; recording executor only, no Docker/process execution.")
}
