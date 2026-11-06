package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/workspace"
)

// One Manager serves every workflow node, fan-out child and delegate of a run,
// so a revision one conversation read or wrote must never stand in for another
// conversation's read: not to fill an omitted hash, not to repair "missing" or
// a miscopied hash, and not as "the current revision you produced".
func TestRevisionMemoryIsScopedToTheConversation(t *testing.T) {
	const original, first, second = "value = 1\n", "first\n", "second\n"
	h0, h1, total := workspace.Hash([]byte(original)), workspace.Hash([]byte(first)), workspace.Hash([]byte("total = 1\n"))
	type step struct {
		scope string
		revisionStep
	}
	read := func(scope string) step {
		return step{scope, revisionStep{tool: "read_file", args: map[string]any{"path": "a.py"}}}
	}
	for _, c := range []struct {
		name  string
		steps []step
		final map[string]string
	}{
		{"another conversation's read does not fill an omitted hash", []step{read("A"),
			{"B", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": first}, err: "a.py has not been read in this run"}},
			{"", revisionStep{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2"}, err: "has not been read"}},
			{"B", revisionStep{tool: "delete_file", args: map[string]any{"path": "a.py"}, err: "has not been read"}},
			{"A", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": first}, approved: h0}},
		}, map[string]string{"a.py": first}},
		{"another conversation's read does not repair missing", []step{read("A"),
			{"B", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": first, "expectedHash": "missing"}, err: "conflict"}},
			{"A", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": first, "expectedHash": "missing"}, approved: h0, note: true}},
		}, map[string]string{"a.py": first}},
		{"another conversation's create does not repair missing", []step{
			{"A", revisionStep{tool: "write_file", args: map[string]any{"path": "new.py", "content": first, "expectedHash": "missing"}, approved: "missing"}},
			{"B", revisionStep{tool: "write_file", args: map[string]any{"path": "new.py", "content": second, "expectedHash": "missing"}, err: "conflict"}},
			{"A", revisionStep{tool: "write_file", args: map[string]any{"path": "new.py", "content": second, "expectedHash": "missing"}, approved: h1, note: true}},
		}, map[string]string{"new.py": second}},
		{"another conversation's earlier revision is not applied as its own", []step{read("A"), read("B"),
			{"A", revisionStep{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "value", "newText": "total"}, approved: h0}},
			{"B", revisionStep{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2", "expectedHash": h0}, err: "conflict"}},
			{"C", revisionStep{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2", "expectedHash": h0}, err: "conflict"}},
			{"B", revisionStep{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2"}, err: "changed since you last read it"}},
			{"A", revisionStep{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2", "expectedHash": h0}, approved: total, note: true}},
		}, map[string]string{"a.py": "total = 2\n"}},
		// Scoping narrows what may fill or repair a hash, never what counts as
		// stale: a hash B could only have copied from A's output still names a
		// real earlier revision, even after B read the current one.
		{"another conversation's earlier revision stays stale here", []step{read("A"),
			{"A", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": first}, approved: h0}},
			read("B"),
			{"B", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": second, "expectedHash": h0}, err: "conflict"}},
			{"B", revisionStep{tool: "delete_file", args: map[string]any{"path": "a.py", "expectedHash": h0}, err: "conflict"}},
			{"B", revisionStep{tool: "move_file", args: map[string]any{"path": "a.py", "destination": "b.py", "expectedHash": h0}, err: "conflict"}},
			{"B", revisionStep{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "first", "newText": "third", "expectedHash": h0}, err: "conflict"}},
			{"B", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": second}, approved: h1}},
		}, map[string]string{"a.py": second, "b.py": ""}},
		{"another conversation's write does not become this one's read", []step{read("A"),
			{"B", revisionStep{tool: "move_file", args: map[string]any{"path": "a.py", "destination": "b.py"}, approved: h0}},
			{"B", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": first}, approved: "missing"}},
			{"A", revisionStep{tool: "edit_file", args: map[string]any{"path": "b.py", "oldText": "1", "newText": "2"}, err: "has not been read"}},
			{"A", revisionStep{tool: "write_file", args: map[string]any{"path": "a.py", "content": second}, err: "has not been read"}},
			{"B", revisionStep{tool: "edit_file", args: map[string]any{"path": "b.py", "oldText": "1", "newText": "2"}, approved: h0}},
		}, map[string]string{"a.py": first, "b.py": "value = 2\n"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "a.py"), []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			var approvals []Approval
			m, err := New(root, Options{ArtifactDir: t.TempDir(), Approve: func(_ context.Context, a Approval) error { approvals = append(approvals, a); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			for i, s := range c.steps {
				before := len(approvals)
				r, err := m.Execute(WithRevisionScope(t.Context(), s.scope), s.tool, s.args)
				switch {
				case s.err == "conflict":
					if !errors.Is(err, workspace.ErrConflict) {
						t.Fatalf("step %d (%s): want conflict, got %v", i, s.scope, err)
					}
				case s.err != "":
					if err == nil || !strings.Contains(err.Error(), s.err) {
						t.Fatalf("step %d (%s): want error %q, got %v", i, s.scope, s.err, err)
					}
				case err != nil:
					t.Fatalf("step %d (%s): %v", i, s.scope, err)
				}
				if s.approved == "" {
					if len(approvals) != before {
						t.Fatalf("step %d (%s) reached approval: %+v", i, s.scope, approvals[before:])
					}
				} else if len(approvals) != before+1 || approvals[before].Arguments["expectedHash"] != s.approved {
					t.Fatalf("step %d (%s): approval did not carry %q: %+v", i, s.scope, s.approved, approvals[before:])
				}
				if s.tool == "read_file" {
					continue
				}
				data, _ := r.Data.(map[string]any)
				if note, _ := data["revisionNote"].(string); s.note != (note != "") {
					t.Fatalf("step %d (%s): revision note %q", i, s.scope, note)
				}
			}
			for name, want := range c.final {
				got, err := os.ReadFile(filepath.Join(root, name))
				if want == "" && !errors.Is(err, os.ErrNotExist) || want != "" && string(got) != want {
					t.Fatalf("%s: got %q (%v), want %q", name, got, err, want)
				}
			}
		})
	}
}

// A context without a scope is the single-conversation default, so callers that
// never share a Manager keep exactly the behaviour they had.
func TestRevisionScopeDefaultsToOneConversation(t *testing.T) {
	if revisionScope(t.Context()) != "" || revisionScope(WithRevisionScope(t.Context(), "fan_item_1")) != "fan_item_1" {
		t.Fatal("revision scope lookup")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.py"), []byte("value = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := New(root, Options{ArtifactDir: t.TempDir(), Approve: func(context.Context, Approval) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Execute(t.Context(), "read_file", map[string]any{"path": "a.py"}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Execute(WithRevisionScope(t.Context(), ""), "edit_file", map[string]any{"path": "a.py", "oldText": "1", "newText": "2"}); err != nil {
		t.Fatal(err)
	}
}
