package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
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
