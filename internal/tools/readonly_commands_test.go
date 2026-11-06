package tools

import (
	"context"
	"strings"
	"testing"
)

func TestReadOnlyCommandOptInForcesReadOnlyBeforeApprovalAndExecution(t *testing.T) {
	x := &verifierFixture{}
	approvals := 0
	m, err := New(t.TempDir(), Options{Mode: "read-only", AllowReadOnlyCommands: true, Executor: x, Approve: func(_ context.Context, a Approval) error {
		approvals++
		if a.Arguments["readOnly"] != true || !strings.Contains(a.Reason, "workspace is read-only") {
			t.Fatal("approval did not show narrowed authority", a)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, args := range []map[string]any{
		{"executable": "python3", "args": []string{"-c", "print(42)"}},
		{"executable": "python3", "args": []string{"-c", "print(42)"}, "readOnly": false},
	} {
		if _, err = m.Execute(t.Context(), "run_command", args); err != nil {
			t.Fatal(err)
		}
	}
	if approvals != 2 || len(x.requests) != 2 || !x.requests[0].ReadOnly || !x.requests[1].ReadOnly {
		t.Fatal("read-only command policy did not reach executor", x.requests)
	}
	for _, name := range []string{"write_file", "edit_file", "move_file", "delete_file", "make_directory", "mcp_call", "browser_open", "setup_dependencies"} {
		if _, err = m.Execute(t.Context(), name, map[string]any{}); err == nil || !strings.Contains(err.Error(), "workspace changes are forbidden") {
			t.Fatalf("%s was available or lacked recovery guidance: %v", name, err)
		}
	}
	if approvals != 2 || len(x.requests) != 2 {
		t.Fatal("forbidden operations reached approval/executor")
	}
}
