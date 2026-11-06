package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/workspace"
)

func TestHarnessLargeCommandObservationRetainsStatusAndProvenance(t *testing.T) {
	for _, exitCode := range []int{0, 17} {
		t.Run(fmt.Sprint(exitCode), func(t *testing.T) {
			// Synthetic executor result only: no process, model or Docker runs.
			data := obj(execution.Result{Stdout: strings.Repeat("npm verification output\n", 3000), Stderr: strings.Repeat("npm diagnostic\n", 3000), ExitCode: exitCode, ReadOnly: true, Truncated: false, Cancelled: false, Signal: "", DurationMS: 321, Environment: "fixture", WorkingDirectory: ".", CommandID: "owned-command", Image: "sha256:" + strings.Repeat("d", 64), SnapshotDigest: strings.Repeat("a", 64), DependencyDigest: strings.Repeat("b", 64), Publication: "unchanged", JournalPath: "/fixture/journal.json", Changes: []workspace.CommandChange{{Path: "source.go", BeforeHash: strings.Repeat("c", 64), AfterHash: strings.Repeat("e", 64), Size: 123}}})
			data["receiptId"] = "run:node:call"
			original, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			entry := harness.ToolObservation{Tool: "run_command", CallID: "call", Arguments: Doc{"executable": "npm", "args": []string{"test"}, "readOnly": true}, Content: string(original), Data: data, StartSequence: 3, Sequence: 4, Artifacts: []harness.Doc{{"path": "journal.json"}}}
			if exitCode != 0 {
				entry.Error = "process exited with code 17"
			}
			saved := boundHarnessObservation(entry)
			if saved.Content != entry.Content[:harnessObservationTextLimit]+"\n[Observation truncated]" || saved.Error != entry.Error || saved.CallID != entry.CallID || !reflect.DeepEqual(saved.Artifacts, entry.Artifacts) {
				t.Fatal("observation identity/error/text contract changed")
			}
			for _, key := range []string{"receiptId", "exitCode", "readOnly", "truncated", "cancelled", "signal", "durationMs", "workingDirectory", "environment", "commandId", "image", "snapshotDigest", "dependencyDigest", "publication", "workspaceChanged", "journalPath"} {
				if !reflect.DeepEqual(saved.Data[key], data[key]) {
					t.Fatalf("lost/changed %s: got %v want %v", key, saved.Data[key], data[key])
				}
			}
			encoded, err := json.Marshal(saved.Data)
			if err != nil || len(encoded) > harnessObservationMetadataLimit {
				t.Fatal("metadata exceeds bound", len(encoded), err)
			}
			if strings.Contains(string(encoded), "npm verification output") || strings.Contains(string(encoded), "npm diagnostic") {
				t.Fatal("output payload copied into metadata")
			}
			if saved.Data["observationDataTruncated"] != true || saved.Data["observationPayloadOmitted"] != true || saved.Data["truncated"] != false || saved.Data["changeCount"] != float64(1) {
				t.Fatal("projection flags conflate command/output facts", saved.Data)
			}
			// Exercise the real final-verification predicate on serialized storage
			// shape, after a preceding edit. A long successful receipt stays useful;
			// nonzero/error or writable commands never become verification success.
			rawSaved, _ := json.Marshal(saved)
			var restored harness.ToolObservation
			if err := json.Unmarshal(rawSaved, &restored); err != nil {
				t.Fatal(err)
			}
			write := harness.ToolObservation{Tool: "edit_file", Sequence: 2}
			if got := harnessVerificationAfterEdits([]string{"npm", "test"}, []harness.ToolObservation{write, restored}); got != (exitCode == 0) {
				t.Fatal("truncated command receipt misclassified", got, exitCode)
			}
			restored.Arguments["readOnly"] = false
			restored.Data["readOnly"] = false
			if harnessVerificationAfterEdits([]string{"npm", "test"}, []harness.ToolObservation{write, restored}) {
				t.Fatal("writable command gained verification authority")
			}
			// Projection maps must not alias the original result, including nested
			// change summaries; that original result still goes to the model.
			saved.Data["publication"] = "test mutation"
			saved.Data["changes"].([]any)[0].(map[string]any)["path"] = "different.go"
			after, _ := json.Marshal(data)
			if string(after) != string(original) || entry.Content != string(original) {
				t.Fatal("saved projection mutated original model result")
			}
		})
	}
}

func TestHarnessCommandObservationMetadataBoundsAndExplicitOmissions(t *testing.T) {
	changes := make([]any, 1000)
	for i := range changes {
		changes[i] = map[string]any{"path": strings.Repeat("\x00", 1000), "beforeHash": strings.Repeat("a", 64), "afterHash": strings.Repeat("b", 64), "size": float64(i), "body": "PRIVATE-CHANGE-BODY"}
	}
	changes[0] = map[string]any{"path": "kept.go", "beforeHash": "before", "afterHash": "after", "size": float64(3), "body": "PRIVATE-CHANGE-BODY"}
	changes[1] = map[string]any{"path": strings.Repeat("p", 1025)}
	data := harness.Doc{"exitCode": float64(23), "readOnly": true, "cancelled": true, "truncated": false, "publication": "conflict", "image": strings.Repeat("x", 1<<20), "receiptId": "exact-id", "changes": changes, "changeCount": float64(1234), "changesLimited": true, "stdout": "PRIVATE-STDOUT", "stderr": "PRIVATE-STDERR", "body": "PRIVATE-BODY", strings.Repeat("unknown", 1000): map[string]any{"deep": "PRIVATE-UNKNOWN"}}
	entry := harness.ToolObservation{Tool: "run_command", Content: strings.Repeat("x", harnessObservationTextLimit+1), Data: data}
	saved := boundHarnessObservation(entry)
	b, err := json.Marshal(saved.Data)
	if err != nil || len(b) > harnessObservationMetadataLimit {
		t.Fatal("pathological metadata exceeds total JSON bound", len(b), err)
	}
	for _, key := range []string{"exitCode", "readOnly", "cancelled", "truncated", "publication", "receiptId", "changeCount", "changesLimited"} {
		if !reflect.DeepEqual(saved.Data[key], data[key]) {
			t.Fatal("status/count changed under pressure", key, saved.Data[key])
		}
	}
	if _, exists := saved.Data["image"]; exists || !reflect.DeepEqual(saved.Data["observationOmittedMetadataFields"], []string{"image"}) {
		t.Fatal("oversized identity was clipped or silently omitted", saved.Data)
	}
	kept := saved.Data["changes"].([]any)
	if len(kept) == 0 || len(kept) > harnessObservationChangesLimit || saved.Data["observationChangesAvailable"] != 1000 || saved.Data["observationChangesRetained"] != len(kept) || saved.Data["observationChangesOmitted"] != float64(1234-len(kept)) {
		t.Fatal("change count/omissions do not describe projection")
	}
	if saved.Data["observationUnknownFieldsOmitted"] != true || !reflect.DeepEqual(saved.Data["observationOmittedPayloadFields"], []string{"stdout", "stderr", "body"}) || strings.Contains(string(b), "PRIVATE-") {
		t.Fatal("payload omission not enforced/explicit")
	}
}

func TestHarnessSmallObservationsAndOtherToolBoundsUnchanged(t *testing.T) {
	for _, length := range []int{0, 20, harnessObservationTextLimit} {
		entry := harness.ToolObservation{Tool: "run_command", Content: strings.Repeat("x", length), Data: Doc{"stdout": "small output", "exitCode": float64(0)}, Error: "kept", Sequence: 2, StartSequence: 1}
		if saved := boundHarnessObservation(entry); !reflect.DeepEqual(saved, entry) {
			t.Fatal("small observation changed", length)
		}
	}
	entry := harness.ToolObservation{Tool: "read_file", Content: strings.Repeat("x", harnessObservationTextLimit+1), Data: Doc{"content": "large text"}}
	if saved := boundHarnessObservation(entry); saved.Data != nil || saved.Content != entry.Content[:harnessObservationTextLimit]+"\n[Observation truncated]" {
		t.Fatal("unrelated tool observation behavior changed")
	}
}

func TestHarnessObservationKeepsHostStartedVerification(t *testing.T) {
	data := Doc{"command": []any{"go", "test"}, "verificationPassed": true, "readOnly": true, "sourceDigest": "same", "sourceDigestAfter": "same", "exitCode": float64(0), "receiptId": "trial::tool_host", "stdout": "PRIVATE-STDOUT", "hostInitiated": true}
	entry := harness.ToolObservation{Tool: "verify_task", CallID: "tool_host", Arguments: Doc{}, Content: strings.Repeat("x", harnessObservationTextLimit+1), Data: data, StartSequence: 3, Sequence: 4}
	saved := boundHarnessObservation(entry)
	if saved.Data["hostInitiated"] != true || saved.Data["receiptId"] != "trial::tool_host" || saved.Data["exitCode"] != float64(0) || saved.Data["observationDataTruncated"] != true {
		t.Fatalf("clipped check lost provenance: %v", saved.Data)
	}
	if b, _ := json.Marshal(saved.Data); strings.Contains(string(b), "PRIVATE-STDOUT") {
		t.Fatal("output payload copied into metadata")
	}
	// Clipping keeps the conservative grading: without the full receipt a
	// check, host-started or not, is not verification evidence.
	if harnessVerificationAfterEdits([]string{"go", "test"}, []harness.ToolObservation{{Tool: "edit_file", Sequence: 2}, saved}) {
		t.Fatal("clipped verify_task gained verification authority")
	}
	delete(entry.Data, "hostInitiated")
	if _, marked := boundHarnessObservation(entry).Data["hostInitiated"]; marked {
		t.Fatal("model-started check gained host provenance")
	}
	if small := (harness.ToolObservation{Tool: "verify_task", Content: "{}", Data: Doc{"hostInitiated": true}}); !reflect.DeepEqual(boundHarnessObservation(small), small) {
		t.Fatal("small observation changed")
	}
}

func TestHarnessHostChecksAttributeOnlyHostVerifierEvents(t *testing.T) {
	checks := &harnessHostChecks{}
	host := engine.ToolCall{ID: "tool_host", Name: "verify_task", NodeID: "node"}
	model := engine.ToolCall{ID: "tool_model", Name: "verify_task", NodeID: "node", Arguments: map[string]any{"hostInitiated": true, "callId": "tool_model"}}
	// Shapes from internal/engine: the host verifier's map, a model call's ToolCall.
	checks.observe(engine.Event{Type: "tool_started", Tool: "verify_task", NodeID: "node", Data: map[string]any{"id": host.ID, "name": host.Name, "nodeId": "node", "callId": host.ID, "hostInitiated": true}})
	checks.observe(engine.Event{Type: "tool_started", Tool: "verify_task", NodeID: "node", Data: model})
	checks.observe(engine.Event{Type: "tool_completed", Tool: "verify_task", NodeID: "node", Data: map[string]any{"callId": model.ID, "hostInitiated": true}})
	other := host
	other.NodeID = "delegate_x"
	if checks.take(model) || checks.take(other) || !checks.take(host) || checks.take(host) {
		t.Fatal("host verifier attribution is not exact and single-use")
	}
}

// The production solver, engine host verifier and Docker executor, with a
// fixture model: the model writes, runs verify_task itself under a "tool_" ID
// and answers; the host then runs its completion check. The declared command
// starts with "-", which the Docker executor rejects before contacting Docker,
// so no process or container runs and both checks end not_run. Only the host's
// trace entry carries the marker.
func TestHarnessSolverTraceMarksOnlyHostCompletionCheck(t *testing.T) {
	c := milestoneCore(t)
	const trialID = "host-check-fixture"
	root := filepath.Join(c.DataDir, "harness", "trials", trialID, "workspace")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
			return
		}
		var request inference.ChatRequest
		if r.URL.Path != "/api/chat" || json.NewDecoder(r.Body).Decode(&request) != nil {
			http.NotFound(w, r)
			return
		}
		tools := 0
		for _, m := range request.Messages {
			if m.Role == "tool" {
				tools++
			}
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		switch tools {
		case 0:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"write_once","function":{"name":"write_file","arguments":{"path":"regression_test.py","content":"pass\n","expectedHash":"missing"}}}]},"done":true,"prompt_eval_count":10,"eval_count":5}`)
		case 1:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"tool_model_check","function":{"name":"verify_task","arguments":{}}}]},"done":true,"prompt_eval_count":10,"eval_count":5}`)
		default:
			fmt.Fprintln(w, `{"message":{"content":"Done."},"done":true,"prompt_eval_count":10,"eval_count":5}`)
		}
	}))
	defer server.Close()
	var err error
	if c.client, err = inference.NewClient(server.URL); err != nil {
		t.Fatal(err)
	}
	image := "sha256:" + strings.Repeat("d", 64)
	frozen := FrozenAgent{Agent: Doc{"id": "solver"}, Profile: CapabilityProfile{Version: 1, Name: "fixture", ExecutionProfile: "docker", Tools: []string{"write_file", "run_command"}}, ExecutionImage: image}
	frozen.Digest = contentDigest(frozen)
	raw, _ := json.Marshal(frozen)
	spec := harness.Spec{AgentID: "solver", Model: "fixture", Agent: Doc{"maxSteps": 6, "thinking": "off", "temperature": 0.2, "maxOutputTokens": 8192, "toolProtocol": "native", "verificationFeedback": true}, ContextSize: 65536, BudgetSeconds: 60, ExecutorIdentity: image, FrozenCapability: raw, SolverTools: []string{"write_file", "run_command"}}
	task := harness.Task{Contract: "coding", Command: []string{"-fixture-check-never-runs"}}
	result, err := c.executeHarnessSolver(t.Context(), harness.SolveRequest{TrialID: trialID, Root: root, Prompt: "Fix it.", Task: task, Spec: spec}, nil)
	if err != nil || result.Incomplete {
		t.Fatal(result, err)
	}
	checks := map[string]harness.ToolObservation{}
	for _, call := range result.ToolCalls {
		if call.Tool == "verify_task" {
			checks[call.CallID] = call
		}
	}
	if len(result.ToolCalls) != 3 || len(checks) != 2 || checks["tool_model_check"].Tool == "" {
		t.Fatalf("expected a write, the model's check and the host's check: %+v", result.ToolCalls)
	}
	for callID, check := range checks {
		if !strings.Contains(check.Error, "invalid executable") || check.Data["checkOutcome"] != "not_run" {
			t.Fatalf("fixture check %s must be rejected before any process: %+v", callID, check)
		}
		_, marked := check.Data["hostInitiated"]
		if host := callID != "tool_model_check"; marked != host || host && check.Data["hostInitiated"] != true {
			t.Fatalf("host provenance wrong for %s: %v", callID, check.Data)
		}
	}
}
