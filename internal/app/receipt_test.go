package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/engine"
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
