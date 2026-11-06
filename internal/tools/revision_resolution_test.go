package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/workspace"
)

type revisionStep struct {
	tool     string
	args     map[string]any
	external string // overwrite path from outside the Manager instead of calling a tool
	err      string // error substring; "conflict" requires workspace.ErrConflict
	approved string // expectedHash the approval must carry; "" means no approval
	note     bool
}

func TestExpectedHashResolution(t *testing.T) {
	const original, external, first, second = "value = 1\n", "value = 99\n", "first\n", "second\n"
	h0, hx, h1 := workspace.Hash([]byte(original)), workspace.Hash([]byte(external)), workspace.Hash([]byte(first))
	garble := func(h string) string { return strings.Repeat("0", 12) + h[12:] }
	read := revisionStep{tool: "read_file", args: map[string]any{"path": "a.py"}}
	for _, c := range []struct {
		name  string
		steps []revisionStep
		final map[string]string // expected file contents; "" means absent
	}{
		{"absent hash for unread file", []revisionStep{
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": first}, err: "a.py has not been read in this run or changed since you last read it; call read_file on it first"},
		}, map[string]string{"a.py": original}},
		{"absent hash after read uses that revision", []revisionStep{read,
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2"}, approved: h0},
		}, map[string]string{"a.py": "value = 2\n"}},
		{"partial line-range read still counts", []revisionStep{
			{tool: "read_file", args: map[string]any{"path": "a.py", "startLine": 1, "lineCount": 1}},
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": first, "expectedHash": ""}, approved: h0},
		}, map[string]string{"a.py": first}},
		{"miscopied hash repaired to the revision last read", []revisionStep{read,
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": first, "expectedHash": garble(h0)}, approved: h0, note: true},
		}, map[string]string{"a.py": first}},
		{"miscopied hash for unread file conflicts", []revisionStep{
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": first, "expectedHash": garble(h0)}, err: "conflict"},
		}, map[string]string{"a.py": original}},
		{"stale read hash after external change conflicts", []revisionStep{read, {external: external},
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": first, "expectedHash": h0}, err: "conflict"},
		}, map[string]string{"a.py": external}},
		{"miscopied hash after external change conflicts", []revisionStep{read, {external: external},
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "99", "newText": "3", "expectedHash": garble(h0)}, err: "conflict"},
		}, map[string]string{"a.py": external}},
		{"absent hash after external change needs a new read", []revisionStep{read, {external: external},
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "99", "newText": "3"}, err: "changed since you last read it"},
			read,
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "99", "newText": "3"}, approved: hx},
		}, map[string]string{"a.py": "value = 3\n"}},
		{"missing reused after create", []revisionStep{
			{tool: "write_file", args: map[string]any{"path": "new.py", "content": first, "expectedHash": "missing"}, approved: "missing"},
			{tool: "write_file", args: map[string]any{"path": "new.py", "content": second, "expectedHash": "missing"}, approved: h1, note: true},
		}, map[string]string{"new.py": second}},
		{"null hash is treated as omitted", []revisionStep{
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2", "expectedHash": nil}, err: "has not been read in this run"},
			read,
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2", "expectedHash": nil}, approved: h0},
			{tool: "write_file", args: map[string]any{"path": "n.py", "content": first, "expectedHash": nil}, approved: "missing"},
		}, map[string]string{"a.py": "value = 2\n", "n.py": first}},
		{"hash for a file that does not exist explains how to create it", []revisionStep{
			{tool: "write_file", args: map[string]any{"path": "n.py", "content": first, "expectedHash": h0}, err: "conflict: the file does not exist, so omit expectedHash (or use missing) to create it"},
			{tool: "write_file", args: map[string]any{"path": "n.py", "content": first}, approved: "missing"},
		}, map[string]string{"n.py": first}},
		{"missing for an unread existing file conflicts", []revisionStep{
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": first, "expectedHash": "missing"}, err: "conflict"},
		}, map[string]string{"a.py": original}},
		{"own earlier revision stays stale", []revisionStep{read,
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": first, "expectedHash": h0}, approved: h0},
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": second, "expectedHash": h0}, err: "conflict"},
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": second}, approved: h1},
		}, map[string]string{"a.py": second}},
		{"delete without hash after read", []revisionStep{read,
			{tool: "delete_file", args: map[string]any{"path": "a.py"}, approved: h0},
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2"}, err: "no such file"},
		}, map[string]string{"a.py": ""}},
		{"stale delete conflicts before approval", []revisionStep{read, {external: external},
			{tool: "delete_file", args: map[string]any{"path": "a.py", "expectedHash": h0}, err: "conflict"},
		}, map[string]string{"a.py": external}},
		{"edit naming its own earlier revision applies to the current one", []revisionStep{read,
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "value", "newText": "total", "expectedHash": h0}, approved: h0},
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "1", "newText": "2", "expectedHash": h0}, approved: workspace.Hash([]byte("total = 1\n")), note: true},
		}, map[string]string{"a.py": "total = 2\n"}},
		{"edit naming its own earlier revision still needs a unique current match", []revisionStep{read,
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "value", "newText": "total", "expectedHash": h0}, approved: h0},
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "value", "newText": "other", "expectedHash": h0}, err: "not found"},
		}, map[string]string{"a.py": "total = 1\n"}},
		{"edit naming an earlier revision after an external change conflicts", []revisionStep{read,
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "value", "newText": "total", "expectedHash": h0}, approved: h0},
			{external: external},
			{tool: "edit_file", args: map[string]any{"path": "a.py", "oldText": "99", "newText": "3", "expectedHash": h0}, err: "conflict"},
		}, map[string]string{"a.py": external}},
		{"move without a read uses the current revision", []revisionStep{
			{tool: "move_file", args: map[string]any{"path": "a.py", "destination": "b.py"}, approved: h0},
		}, map[string]string{"a.py": "", "b.py": original}},
		{"move with an invented hash uses the current revision", []revisionStep{
			{tool: "move_file", args: map[string]any{"path": "a.py", "destination": "b.py", "expectedHash": garble(h0)}, approved: h0, note: true},
		}, map[string]string{"a.py": "", "b.py": original}},
		{"move with a stale hash still conflicts", []revisionStep{read, {external: external},
			{tool: "move_file", args: map[string]any{"path": "a.py", "destination": "b.py", "expectedHash": h0}, err: "conflict"},
		}, map[string]string{"a.py": external, "b.py": ""}},
		{"move carries the revision to its destination", []revisionStep{read,
			{tool: "move_file", args: map[string]any{"path": "a.py", "destination": "b.py"}, approved: h0},
			{tool: "edit_file", args: map[string]any{"path": "b.py", "oldText": "1", "newText": "2"}, approved: h0},
			{tool: "write_file", args: map[string]any{"path": "a.py", "content": first}, approved: "missing"},
		}, map[string]string{"a.py": first, "b.py": "value = 2\n"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "a.py")
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			var approvals []Approval
			m, err := New(root, Options{ArtifactDir: t.TempDir(), Approve: func(_ context.Context, a Approval) error { approvals = append(approvals, a); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			for i, s := range c.steps {
				if s.external != "" {
					if err := os.WriteFile(path, []byte(s.external), 0600); err != nil {
						t.Fatal(err)
					}
					continue
				}
				before := len(approvals)
				r, err := m.Execute(t.Context(), s.tool, s.args)
				switch {
				case s.err == "conflict":
					if !errors.Is(err, workspace.ErrConflict) || !strings.Contains(err.Error(), "call read_file with path") {
						t.Fatalf("step %d: want conflict with read_file hint, got %v", i, err)
					}
				case strings.HasPrefix(s.err, "conflict: "):
					if !errors.Is(err, workspace.ErrConflict) || !strings.Contains(err.Error(), strings.TrimPrefix(s.err, "conflict: ")) || strings.Contains(err.Error(), "read_file") {
						t.Fatalf("step %d: want conflict %q, got %v", i, s.err, err)
					}
				case s.err != "":
					if err == nil || !strings.Contains(err.Error(), s.err) {
						t.Fatalf("step %d: want error %q, got %v", i, s.err, err)
					}
				case err != nil:
					t.Fatalf("step %d: %v", i, err)
				}
				if s.approved == "" {
					if len(approvals) != before {
						t.Fatalf("step %d reached approval: %+v", i, approvals[before:])
					}
				} else if len(approvals) != before+1 || approvals[before].Arguments["expectedHash"] != s.approved {
					t.Fatalf("step %d: approval did not carry the resolved revision %q: %+v", i, s.approved, approvals[before:])
				}
				if s.tool == "read_file" {
					continue
				}
				data, _ := r.Data.(map[string]any)
				note, _ := data["revisionNote"].(string)
				if s.note != (note != "") || s.note && (!strings.Contains(note, s.approved[:12]) || !strings.Contains(r.Content, "revisionNote")) {
					t.Fatalf("step %d: revision note %q in %s", i, note, r.Content)
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

func TestExpectedHashIsOptionalInSchemas(t *testing.T) {
	for _, d := range Catalogue() {
		switch d.Name {
		case "write_file", "edit_file", "move_file", "delete_file":
			for _, r := range d.Parameters["required"].([]string) {
				if r == "expectedHash" {
					t.Fatalf("%s still requires expectedHash", d.Name)
				}
			}
			p := d.Parameters["properties"].(map[string]any)["expectedHash"].(map[string]any)
			if p["description"] != "Optional SHA-256 revision from read_file. Omit it to use the revision you last read in this run; use missing only when creating a new file." {
				t.Fatalf("%s hash description: %v", d.Name, p["description"])
			}
		}
	}
}

func TestNormalizedEditMatchIsApprovedAndApplied(t *testing.T) {
	root := t.TempDir()
	const content = "keep\r\ndef f():  \r\n    return 1\r\n"
	path := filepath.Join(root, "f.py")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	var approvals []Approval
	m, err := New(root, Options{ArtifactDir: t.TempDir(), Approve: func(_ context.Context, a Approval) error { approvals = append(approvals, a); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Execute(t.Context(), "read_file", map[string]any{"path": "f.py"}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Execute(t.Context(), "edit_file", map[string]any{"path": "f.py", "oldText": "def f():\n    return 1\n", "newText": "def f():\n    return 2\n"}); err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || !strings.Contains(approvals[0].Reason, "trailing-whitespace") || approvals[0].Arguments["expectedHash"] != workspace.Hash([]byte(content)) {
		t.Fatalf("normalized match not disclosed in approval: %+v", approvals)
	}
	if got, _ := os.ReadFile(path); string(got) != "keep\r\ndef f():\n    return 2\n" {
		t.Fatalf("wrong span replaced: %q", got)
	}
	for _, old := range []string{"def f():\n  return 2", "def f():\n\treturn 2", "def f():\n      return 2"} {
		if _, err = m.Execute(t.Context(), "edit_file", map[string]any{"path": "f.py", "oldText": old, "newText": "y"}); err == nil || !strings.Contains(err.Error(), "was not found in f.py") || len(approvals) != 1 {
			t.Fatalf("indentation difference matched: %v", err)
		}
	}
	_, err = m.Execute(t.Context(), "edit_file", map[string]any{"path": "f.py", "oldText": "return 2", "newText": "return 3"})
	if err != nil || len(approvals) != 2 || strings.Contains(approvals[1].Reason, "trailing-whitespace") {
		t.Fatalf("exact match described as normalized: %v %+v", err, approvals)
	}
}

func TestTaskProtectionPrecedesRevisionResolution(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests", "test_public.py"), []byte("assert True\n"), 0600); err != nil {
		t.Fatal(err)
	}
	approvals := 0
	m, err := New(root, Options{ArtifactDir: t.TempDir(), Verification: &execution.VerificationContract{Command: []string{"python3"}, ProtectedPaths: []string{"tests/test_public.py"}}, Approve: func(context.Context, Approval) error { approvals++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Execute(t.Context(), "read_file", map[string]any{"path": "tests/test_public.py"}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"write_file", "delete_file"} {
		args := map[string]any{"path": "tests/test_public.py"}
		if tool == "write_file" {
			args["content"] = "assert False\n"
		}
		if _, err = m.Execute(t.Context(), tool, args); err == nil || !strings.Contains(err.Error(), "protected by the task contract") || approvals != 0 {
			t.Fatalf("%s: %v approvals=%d", tool, err, approvals)
		}
	}
}
