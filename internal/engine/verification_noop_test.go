package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gemmadesktop.local/app/internal/tools"
)

func editReplayReceipt() map[string]any {
	return map[string]any{"applied": true, "alreadyApplied": true, "replacements": 0, "beforeHash": "current-source-hash", "afterHash": "current-source-hash"}
}

func TestHostFileNoOpPreservesVerificationAndChangeState(t *testing.T) {
	receipts := []struct {
		name, tool string
		data       map[string]any
	}{{"existing_directory", "make_directory", map[string]any{"created": false, "exists": true}}}
	for _, zero := range []any{int(0), int64(0), float64(0), json.Number("0")} {
		data := editReplayReceipt()
		data["replacements"] = zero
		receipts = append(receipts, struct {
			name, tool string
			data       map[string]any
		}{fmt.Sprintf("edit_replay_%T", zero), "edit_file", data})
	}
	for _, receipt := range receipts {
		for _, passed := range []bool{false, true} {
			for _, changed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/passed=%v/changed=%v", receipt.name, passed, changed), func(t *testing.T) {
					v := &verificationState{passed: passed, changed: changed}
					v.observe(ToolCall{Name: receipt.tool}, ToolResult{Data: receipt.data}, nil)
					if v.verified() != passed || v.changedWorkspace() != changed {
						t.Fatalf("no-op changed tracker state: passed=%v changed=%v", v.verified(), v.changedWorkspace())
					}
				})
			}
		}
	}
}

func TestHostFileNoOpRejectsMissingConflictingAndForeignReceipts(t *testing.T) {
	type fixture struct {
		name, tool string
		data       map[string]any
		isError    bool
		err        error
		changed    bool
	}
	cases := []fixture{
		{name: "missing_receipt", tool: "edit_file", changed: true},
		{name: "prose_is_not_receipt", tool: "edit_file", data: map[string]any{"message": "Already applied; no changes"}, changed: true},
		{name: "directory_missing_created", tool: "make_directory", data: map[string]any{"exists": true}, changed: true},
		{name: "directory_missing_exists", tool: "make_directory", data: map[string]any{"created": false}, changed: true},
		{name: "directory_created", tool: "make_directory", data: map[string]any{"exists": true, "created": true}, changed: true},
		{name: "directory_wrong_types", tool: "make_directory", data: map[string]any{"exists": "true", "created": "false"}, changed: true},
		{name: "failed_directory", tool: "make_directory", data: map[string]any{"exists": true, "created": false}, err: errors.New("failed after receipt"), changed: false},
		{name: "error_result", tool: "edit_file", data: editReplayReceipt(), isError: true, changed: false},
		{name: "failed_result", tool: "edit_file", data: editReplayReceipt(), err: errors.New("unknown failure"), changed: false},
	}
	for _, tool := range []string{"write_file", "move_file", "delete_file", "skill_materialize", "mcp_call", "future_tool"} {
		data := editReplayReceipt()
		data["created"], data["exists"] = false, true
		cases = append(cases, fixture{name: "foreign_" + tool, tool: tool, data: data, changed: true})
	}
	for _, key := range []string{"applied", "alreadyApplied", "replacements", "beforeHash", "afterHash"} {
		data := editReplayReceipt()
		delete(data, key)
		cases = append(cases, fixture{name: "missing_" + key, tool: "edit_file", data: data, changed: true})
	}
	for _, update := range []struct {
		name, key string
		value     any
	}{
		{"not_applied", "applied", false}, {"not_replay", "alreadyApplied", false},
		{"replacement_happened", "replacements", float64(1)}, {"fractional", "replacements", 0.5},
		{"negative", "replacements", -1}, {"string_zero", "replacements", "0"},
		{"nan", "replacements", math.NaN()}, {"infinity", "replacements", math.Inf(1)},
		{"empty_hash", "beforeHash", ""}, {"changed_hash", "afterHash", "different"},
		{"unknown_outcome", "outcomeUnknown", true}, {"malformed_unknown", "outcomeUnknown", "false"},
		{"payload_error", "isError", true}, {"pending", "needsInput", true},
		{"cancelled", "cancelled", true}, {"workspace_changed", "workspaceChanged", true},
	} {
		data := editReplayReceipt()
		data[update.key] = update.value
		cases = append(cases, fixture{name: update.name, tool: "edit_file", data: data, changed: true})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := &verificationState{passed: true}
			v.observe(ToolCall{Name: tc.tool}, ToolResult{Data: tc.data, IsError: tc.isError}, tc.err)
			if v.verified() || v.changedWorkspace() != tc.changed {
				t.Fatalf("unproven no-op retained verification or misreported change: passed=%v changed=%v (want false/%v)", v.verified(), v.changedWorkspace(), tc.changed)
			}
		})
	}
}

// Exercise the actual built-in receipt through the same JSON shape used by the
// app facade (numeric values become float64), rather than invent a no-op result.
func realNoOpToolExecutor(t *testing.T, root string, approvals *int) func(ToolCall) (ToolResult, error) {
	t.Helper()
	mgr, err := tools.New(root, tools.Options{ArtifactDir: t.TempDir(), Approve: func(context.Context, tools.Approval) error { *approvals++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return func(call ToolCall) (ToolResult, error) {
		result, err := mgr.Execute(tools.WithRevisionScope(t.Context(), call.NodeID), call.Name, call.Arguments)
		var data map[string]any
		encoded, marshalErr := json.Marshal(result.Data)
		if marshalErr == nil {
			marshalErr = json.Unmarshal(encoded, &data)
		}
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return ToolResult{Content: result.Content, Data: data, IsError: err != nil}, err
	}
}

func TestExistingDirectoryOnlyNeverVerifiesOrRepairsPreExistingFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "reports"), 0700); err != nil {
		t.Fatal(err)
	}
	approvals, noOp := 0, false
	execute := realNoOpToolExecutor(t, root, &approvals)
	f := &hostVerificationFixture{other: func(call ToolCall) (ToolResult, error) {
		result, err := execute(call)
		noOp = call.Name == "make_directory" && err == nil && result.Data["created"] == false && result.Data["exists"] == true
		return result, err
	}}
	req := codingRequest()
	req.Prompt = "Ensure the reports directory exists; make no other changes."
	req.Tools = []ToolDefinition{{Name: "make_directory"}, {Name: "verify_task"}}
	replies := []string{`{"done":true,"message":{"tool_calls":[{"id":"ensure-directory","function":{"name":"make_directory","arguments":{"path":"reports"}}}]}}`, finishedReply}
	result, err := f.run(t, t.Context(), req, replies, func(int) (ToolResult, error) {
		return failedCheck("FAIL: unrelated pre-existing problem", "")
	})
	feedback, verifications := f.count("verification_feedback")
	if err != nil || result.Content != "done" || result.Verification != "unverified" || f.calls.Load() != 2 || verifications != 0 || feedback != 0 || approvals != 0 || !noOp {
		t.Fatalf("no-op task triggered checking/repair: result=%+v err=%v calls=%d checks=%d feedback=%d approvals=%d noOp=%v", result, err, f.calls.Load(), verifications, feedback, approvals, noOp)
	}
}

func TestVerifiedEditReplayRetainsPassingCheckWithoutAnotherVerification(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value.py")
	if err := os.WriteFile(path, []byte("value = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	approvals, replayed := 0, false
	execute := realNoOpToolExecutor(t, root, &approvals)
	f := &hostVerificationFixture{other: func(call ToolCall) (ToolResult, error) {
		result, err := execute(call)
		if call.ID == "replay" {
			replayed = err == nil && result.Data["alreadyApplied"] == true && result.Data["replacements"] == float64(0)
		}
		return result, err
	}}
	req := codingRequest()
	req.Prompt = "Change value to 10 and check it."
	req.Tools = []ToolDefinition{{Name: "read_file"}, {Name: "edit_file"}, {Name: "verify_task"}}
	replies := []string{
		`{"done":true,"message":{"tool_calls":[{"id":"read","function":{"name":"read_file","arguments":{"path":"value.py"}}}]}}`,
		`{"done":true,"message":{"tool_calls":[{"id":"edit","function":{"name":"edit_file","arguments":{"path":"value.py","oldText":"value = 1","newText":"value = 10"}}}]}}`,
		`{"done":true,"message":{"tool_calls":[{"id":"own-check","function":{"name":"verify_task","arguments":{}}}]}}`,
		`{"done":true,"message":{"tool_calls":[{"id":"replay","function":{"name":"edit_file","arguments":{"path":"value.py","oldText":"value = 1","newText":"value = 10"}}}]}}`,
		finishedReply,
	}
	result, err := f.run(t, t.Context(), req, replies, func(n int) (ToolResult, error) {
		if n == 1 {
			return passedCheck(), nil
		}
		return failedCheck("FAIL: unrelated newly observed issue", "")
	})
	feedback, verifications := f.count("verification_feedback")
	contents, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || result.Verification != "passed" || result.Content != "done" || f.calls.Load() != 5 || verifications != 1 || feedback != 0 || approvals != 1 || !replayed || string(contents) != "value = 10\n" {
		t.Fatalf("replay invalidated check or repeated edit: result=%+v err=%v readErr=%v calls=%d checks=%d feedback=%d approvals=%d replay=%v contents=%q", result, err, readErr, f.calls.Load(), verifications, feedback, approvals, replayed, contents)
	}
}
