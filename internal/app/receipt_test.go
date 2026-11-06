package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
)

func TestReceiptKeepsLatestEntriesAndExactOmissionFlag(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := &Core{Store: s}
	for i := 0; i < 100; i++ {
		if err := s.Put("toolcall", Doc{"id": fmt.Sprintf("call-%03d", i), "callId": fmt.Sprint(i), "runId": "run", "createdAt": fmt.Sprintf("%06d", i), "tool": "run_command", "status": "completed", "arguments": Doc{"executable": "python", "workingDirectory": "nested"}, "result": Doc{"data": Doc{"exitCode": 0, "workingDirectory": "/workspace/nested", "readOnly": true}}}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := c.runReceipt("run")
	if err != nil || r["entriesLimited"] != false {
		t.Fatalf("exactly100 falsely marked omitted: %v %v", r, err)
	}
	if err := s.Put("toolcall", Doc{"id": "call-100", "callId": "100", "runId": "run", "createdAt": "000100", "tool": "run_command", "status": "completed", "result": Doc{"data": Doc{"exitCode": 0}}}); err != nil {
		t.Fatal(err)
	}
	r, err = c.runReceipt("run")
	if err != nil {
		t.Fatal(err)
	}
	commands := r["commands"].([]Doc)
	if r["entriesLimited"] != true || r["toolCalls"] != 101 || len(commands) != 100 || commands[0]["callId"] != "1" || commands[99]["callId"] != "100" {
		t.Fatalf("latest100/count wrong: %v", r)
	}
	if commands[0]["workingDirectory"] != "/workspace/nested" || commands[0]["readOnly"] != true {
		t.Fatal("receipt did not prefer observed execution metadata")
	}
}

func TestRecoveryHasTotalByteBoundAndRetainsOmittedUncertainty(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 205; i++ {
		call := Doc{"id": fmt.Sprintf("call-%03d", i), "createdAt": fmt.Sprintf("%06d", i), "runId": "run", "tool": "edit_file", "status": "completed", "arguments": Doc{"path": fmt.Sprintf("file-%d", i), "content": "DO-NOT-RECOVER-SOURCE", "oldText": "DO-NOT-RECOVER-SOURCE", "newText": "DO-NOT-RECOVER-SOURCE"}, "result": Doc{"data": Doc{"afterHash": "new"}}}
		if i == 0 {
			call["status"] = "failed"
			call["result"] = Doc{"data": Doc{"outcomeUnknown": true}}
		}
		if err := s.Put("toolcall", call); err != nil {
			t.Fatal(err)
		}
	}
	text, err := (&Core{Store: s}).recoveryContext(Doc{"id": "run", "status": "failed", "error": strings.Repeat("\x01", 100000)})
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > 16<<10 || strings.Contains(text, "DO-NOT-RECOVER-SOURCE") {
		t.Fatalf("recovery escaped bound/redaction: %d", len(text))
	}
	var body Doc
	if err := json.Unmarshal([]byte(text[strings.Index(text, "{"):]), &body); err != nil {
		t.Fatal(err)
	}
	if number(body, "unresolvedTools", 0) != 1 || number(body, "omittedActions", 0) < 185 {
		t.Fatalf("omitted uncertainty/count lost: %v", body)
	}
}

func TestSnapshotArtifactMetadataAndExplicitFullGet(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	doc := Doc{"id": "artifact", "name": "file.py", "runId": "run", "before": strings.Repeat("before", 10000), "after": strings.Repeat("after", 10000)}
	if err := s.Put("artifact", doc); err != nil {
		t.Fatal(err)
	}
	c := &Core{Store: s, runtime: Doc{}}
	snapshot, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	artifacts := snapshot["artifacts"].([]Doc)
	if len(artifacts) != 1 || artifacts[0]["name"] != "file.py" || artifacts[0]["before"] != nil || artifacts[0]["after"] != nil {
		t.Fatalf("snapshot artifacts: %v", artifacts)
	}
	full, err := c.Call(t.Context(), "artifact.get", Doc{"id": "artifact"})
	if err != nil || obj(full)["before"] != doc["before"] || obj(full)["after"] != doc["after"] {
		t.Fatal("full lazy fetch lost artifact", err)
	}
}

func TestReceiptUsesOwnedToolOutcomesNotModelClaims(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := &Core{Store: s}
	run := Doc{"id": "run", "sessionId": "session"}
	if err = s.Put("run", run); err != nil {
		t.Fatal(err)
	}
	for _, call := range []Doc{
		{"id": "1", "runId": "run", "callId": "failed-test", "tool": "run_command", "status": "failed", "arguments": Doc{"executable": "python3", "args": []string{"-m", "unittest"}, "readOnly": true}, "result": Doc{"data": Doc{"exitCode": 1, "stderr": "AssertionError"}}, "error": "process exited with code 1"},
		{"id": "2", "runId": "run", "callId": "write", "tool": "edit_file", "status": "completed", "arguments": Doc{"path": "main.py"}, "result": Doc{"data": Doc{"beforeHash": "old", "afterHash": "new"}}},
		{"id": "3", "runId": "run", "tool": "write_file", "status": "started", "arguments": Doc{"path": "uncertain.py"}},
		{"id": "4", "runId": "other-run", "tool": "run_command", "status": "completed", "result": Doc{"data": Doc{"exitCode": 0}}},
	} {
		if err = s.Put("toolcall", call); err != nil {
			t.Fatal(err)
		}
	}
	c.finishRun(run, engine.Result{Content: "All tests passed and every file was verified."}, nil)
	stored, err := s.Get("run", "run")
	if err != nil {
		t.Fatal(err)
	}
	r := obj(stored["receipt"])
	if stored["status"] != "failed" {
		t.Fatalf("uncertain effects marked completed: %v", stored)
	}
	if number(r, "toolCalls", 0) != 3 || number(r, "failedTools", 0) != 1 || number(r, "unresolvedTools", 0) != 1 {
		t.Fatalf("wrong journal counts: %v", r)
	}
	commands := r["commands"].([]any)
	if len(commands) != 1 || number(obj(commands[0]), "exitCode", -99) != 1 {
		t.Fatalf("model prose overrode observed failure: %v", commands)
	}
	changes := r["changes"].([]any)
	if len(changes) != 1 || str(obj(changes[0]), "path") != "main.py" || str(obj(changes[0]), "afterHash") != "new" {
		t.Fatalf("uncertain write counted as success: %v", changes)
	}
}

func TestReceiptDoesNotInventVerificationForPlainAnswer(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := (&Core{Store: s}).runReceipt("no-tools")
	if err != nil || r["toolCalls"] != 0 || len(r["commands"].([]Doc)) != 0 || len(r["changes"].([]Doc)) != 0 {
		t.Fatalf("%v %v", r, err)
	}
}

func TestReceiptPreservesPublicationConflictSeparateFromProcessExit(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data := Doc{"exitCode": 0, "publication": "conflict", "workspaceChanged": true, "snapshotDigest": "frozen-input", "image": "sha256:toolchain", "commandId": "owned-container", "changes": []Doc{{"path": "generated.ts", "beforeHash": "old", "afterHash": "new"}}}
	if err = s.Put("toolcall", Doc{"id": "call", "callId": "call", "runId": "run", "tool": "run_command", "status": "failed", "arguments": Doc{"executable": "npm"}, "result": Doc{"data": data}}); err != nil {
		t.Fatal(err)
	}
	r, err := (&Core{Store: s}).runReceipt("run")
	if err != nil {
		t.Fatal(err)
	}
	commands := r["commands"].([]Doc)
	if len(commands) != 1 || commands[0]["publication"] != "conflict" || commands[0]["workspaceChanged"] != true || number(commands[0], "exitCode", -1) != 0 || commands[0]["snapshotDigest"] != "frozen-input" || commands[0]["changes"] == nil {
		t.Fatalf("execution provenance lost: %#v", commands)
	}
}

func TestReceiptIncludesGovernedSetupAndBinaryMaterialization(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, call := range []Doc{
		{"id": "setup", "callId": "setup", "runId": "run", "tool": "setup_dependencies", "status": "completed", "arguments": Doc{"packageManager": "npm"}, "result": Doc{"data": Doc{"exitCode": 0, "packageCount": 3, "downloadedBytes": 200, "lockHash": "locked", "dependencyDigest": "private-environment", "readOnly": true}}},
		{"id": "asset", "callId": "asset", "runId": "run", "tool": "skill_materialize", "status": "completed", "arguments": Doc{"targetPath": "images/figure.png"}, "result": Doc{"data": Doc{"path": "images/figure.png", "afterHash": "exact-bytes", "bytes": 42, "rawBytes": "NOT-IN-PRESENTATION"}}},
	} {
		if err = s.Put("toolcall", call); err != nil {
			t.Fatal(err)
		}
	}
	r, err := (&Core{Store: s}).runReceipt("run")
	if err != nil {
		t.Fatal(err)
	}
	commands, changes := r["commands"].([]Doc), r["changes"].([]Doc)
	if len(commands) != 1 || len(changes) != 1 || r["entriesLimited"] != false || commands[0]["dependencyDigest"] != "private-environment" || number(commands[0], "packageCount", 0) != 3 || changes[0]["path"] != "images/figure.png" || changes[0]["afterHash"] != "exact-bytes" {
		t.Fatalf("new actions lost: %v", r)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "NOT-IN-PRESENTATION") {
		t.Fatal("binary payload leaked into receipt")
	}
}

func TestUnknownMCPOutcomeCannotBecomeVerifiedCompletion(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := &Core{Store: s}
	run := Doc{"id": "uncertain", "sessionId": "session"}
	if err = s.Put("run", run); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("toolcall", Doc{"id": "mcp-call", "runId": "uncertain", "tool": "mcp_call", "status": "failed", "arguments": Doc{"tool": "external_operation"}, "result": Doc{"data": Doc{"outcomeUnknown": true}}, "error": "connection lost after dispatch"}); err != nil {
		t.Fatal(err)
	}
	c.finishRun(run, engine.Result{Content: "Done."}, nil)
	stored, _ := s.Get("run", "uncertain")
	receipt := obj(stored["receipt"])
	if stored["status"] != "failed" || number(receipt, "unresolvedTools", 0) != 1 {
		t.Fatalf("unknown effect hidden: %v", stored)
	}
	recovery, err := c.recoveryContext(stored)
	if err != nil || !strings.Contains(recovery, `"outcomeUnknown":true`) {
		t.Fatalf("uncertainty lost: %q %v", recovery, err)
	}
}

func TestReceiptMarksOnlyHostStartedVerification(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := &Core{Store: s}
	run := Doc{"id": "run", "sessionId": "session"}
	if err = s.Put("run", run); err != nil {
		t.Fatal(err)
	}
	// Both IDs carry the "tool_" prefix: the engine gives it to an unnamed model
	// call too, so only the host verifier's own event may mark a check.
	verified := Doc{"data": Doc{"command": []string{"go", "test", "./..."}, "verificationPassed": true, "readOnly": true, "sourceDigest": "same", "sourceDigestAfter": "same", "exitCode": 0}}
	for _, call := range []Doc{
		{"id": "run::tool_model", "callId": "tool_model", "runId": "run", "createdAt": "1", "tool": "verify_task", "status": "completed", "arguments": Doc{"hostInitiated": true}, "result": verified},
		{"id": "run::tool_host", "callId": "tool_host", "runId": "run", "createdAt": "2", "tool": "verify_task", "status": "completed", "arguments": Doc{}, "result": verified},
		{"id": "run::tool_echo", "callId": "tool_echo", "runId": "run", "createdAt": "3", "tool": "verify_task", "status": "failed", "arguments": Doc{}, "result": Doc{"data": Doc{"exitCode": 1}}},
	} {
		if err = s.Put("toolcall", call); err != nil {
			t.Fatal(err)
		}
	}
	// Journal events in the shape executeRun writes them (obj(event) + runId).
	for _, journal := range []struct {
		run   string
		event engine.Event
	}{
		{"run", engine.Event{Type: "tool_started", Title: "verify_task", Tool: "verify_task", Data: engine.ToolCall{ID: "tool_model", Name: "verify_task", Arguments: map[string]any{"hostInitiated": true, "callId": "tool_model"}}}},
		{"run", engine.Event{Type: "tool_started", Title: "verify_task", Tool: "verify_task", Data: map[string]any{"id": "tool_host", "name": "verify_task", "arguments": map[string]any{}, "callId": "tool_host", "hostInitiated": true}}},
		{"run", engine.Event{Type: "tool_completed", Title: "verify_task", Tool: "verify_task", Data: map[string]any{"callId": "tool_model", "hostInitiated": true}}},
		{"other-run", engine.Event{Type: "tool_started", Title: "verify_task", Tool: "verify_task", Data: map[string]any{"callId": "tool_echo", "hostInitiated": true}}},
	} {
		d := obj(journal.event)
		d["runId"] = journal.run
		if _, err = s.Event(d); err != nil {
			t.Fatal(err)
		}
	}
	c.finishRun(run, engine.Result{Content: "Done.", Verification: "passed"}, nil)
	stored, err := s.Get("run", "run")
	if err != nil {
		t.Fatal(err)
	}
	commands := obj(stored["receipt"])["commands"].([]any)
	if len(commands) != 3 {
		t.Fatalf("commands: %v", commands)
	}
	for _, raw := range commands {
		command := obj(raw)
		_, marked := command["hostInitiated"]
		if want := command["callId"] == "tool_host"; marked != want || marked && command["hostInitiated"] != true {
			t.Fatalf("host provenance wrong for %v: %v", command["callId"], command)
		}
		// tool_echo's only start event belongs to another run, so this run's
		// journal proves neither starter; it must not default to the model.
		_, unrecorded := command["initiatorUnrecorded"]
		if want := command["callId"] == "tool_echo"; unrecorded != want || unrecorded && command["initiatorUnrecorded"] != true {
			t.Fatalf("unrecorded starter wrong for %v: %v", command["callId"], command)
		}
	}
}

// End to end on the ordinary run path with a fixture model: the model writes,
// calls verify_task itself under a "tool_" ID, and answers; the host then runs
// its completion check. Both checks are declined, so no process runs, yet each
// leaves a journal intent, and only the host's carries the marker.
func TestReceiptMarksHostCompletionCheckOnRunPath(t *testing.T) {
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		tools := 0
		for _, m := range req.Messages {
			if m.Role == "tool" {
				tools++
			}
		}
		switch tools {
		case 0:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"write_once","function":{"name":"write_file","arguments":{"path":"approved.txt","content":"approved content","expectedHash":"missing"}}}]},"done":true}`)
		case 1:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"tool_model_check","function":{"name":"verify_task","arguments":{}}}]},"done":true}`)
		default:
			fmt.Fprintln(w, `{"message":{"content":"Done."},"done":true}`)
		}
	})
	id := str(callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Fix the calculator", "verificationContract": Doc{"command": []string{"/usr/bin/true"}}}), "id")
	for _, tool := range []string{"write_file", "verify_task", "verify_task"} {
		pending := pendingIntegration(t, h.core, id)
		if pending["tool"] != tool {
			t.Fatalf("approval for %v, expected %s", pending["tool"], tool)
		}
		callIntegration(t, h.core, "approval.respond", Doc{"id": pending["id"], "allow": tool == "write_file"})
	}
	finished := finishedIntegration(t, h.core, id, "completed")
	checks := map[string]Doc{}
	for _, raw := range obj(finished["receipt"])["commands"].([]any) {
		if command := obj(raw); command["tool"] == "verify_task" {
			checks[str(command, "callId")] = command
		}
	}
	if len(checks) != 2 || checks["tool_model_check"] == nil {
		t.Fatalf("expected the model's and the host's checks: %v", checks)
	}
	for callID, command := range checks {
		_, marked := command["hostInitiated"]
		if host := callID != "tool_model_check"; marked != host || host && command["hostInitiated"] != true || command["initiatorUnrecorded"] != nil {
			t.Fatalf("host provenance wrong for %s: %v", callID, command)
		}
	}
}

// A failed host check followed by a long repair round can leave its start
// event outside Events' newest-3000 window. The receipt must then say the
// starter is unrecorded, not attribute the host's check to the model.
func TestReceiptLeavesCheckUnattributedOutsideEventWindow(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	run := Doc{"id": "run", "sessionId": "session"}
	if err = s.Put("run", run); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("toolcall", Doc{"id": "run::tool_early", "callId": "tool_early", "runId": "run", "createdAt": "1", "tool": "verify_task", "status": "failed", "arguments": Doc{}, "result": Doc{"data": Doc{"exitCode": 1}}}); err != nil {
		t.Fatal(err)
	}
	events := []Doc{obj(engine.Event{Type: "tool_started", Title: "verify_task", Tool: "verify_task", Data: map[string]any{"id": "tool_early", "name": "verify_task", "arguments": map[string]any{}, "nodeId": "", "callId": "tool_early", "hostInitiated": true}})}
	for range 3000 {
		events = append(events, Doc{"type": "text", "title": "Assistant", "detail": "repairing"})
	}
	for _, event := range events {
		event["runId"] = "run"
		if _, err = s.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	(&Core{Store: s}).finishRun(run, engine.Result{Content: "Done."}, nil)
	stored, err := s.Get("run", "run")
	if err != nil {
		t.Fatal(err)
	}
	commands := obj(stored["receipt"])["commands"].([]any)
	if len(commands) != 1 || obj(commands[0])["initiatorUnrecorded"] != true || obj(commands[0])["hostInitiated"] != nil {
		t.Fatalf("check outside the window must stay unattributed: %v", commands)
	}
}
