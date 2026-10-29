package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/workspace"
)

func TestRevisionPreflightRejectsStaleAndMissingCollisionsBeforeApproval(t *testing.T) {
	const relative = "tests/test_regressions.py"
	const original = "def test_regression():\n    assert True\n"
	for _, tool := range []string{"write_file", "edit_file"} {
		for _, expected := range []string{workspace.Hash([]byte("older revision")), "missing"} {
			label := "stale"
			if expected == "missing" {
				label = "missing_collision"
			}
			t.Run(tool+"/"+label, func(t *testing.T) {
				root, artifacts := t.TempDir(), t.TempDir()
				path := filepath.Join(root, relative)
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				approvals := 0
				var events []Event
				m, err := New(root, Options{ArtifactDir: artifacts, Approve: func(context.Context, Approval) error { approvals++; return nil }, Emit: func(event Event) { events = append(events, event) }})
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				args := map[string]any{"path": relative, "expectedHash": expected}
				if tool == "write_file" {
					args["content"] = "proposed replacement\n"
				} else {
					args["oldText"], args["newText"] = "assert True", "assert False"
				}
				result, err := m.Execute(t.Context(), tool, args)
				assertRevisionHint(t, err, relative, workspace.Hash([]byte(original)))
				if approvals != 0 || len(result.Artifacts) != 0 {
					t.Fatalf("rejected revision reached approval/artifacts: approvals=%d result=%+v", approvals, result)
				}
				if len(events) != 2 || events[1].Type != "tool_failed" || events[1].Detail != err.Error() {
					t.Fatalf("audit did not retain the same recovery hint: %+v", events)
				}
				assertRevisionFileAndNoArtifacts(t, path, original, artifacts)
			})
		}
	}
}

func TestRevisionPreflightDoesNotReplaceChecksAfterApproval(t *testing.T) {
	const relative = "tests/test_regressions.py"
	const original = "original code\n"
	const external = "external change during approval\n"
	for _, scenario := range []struct {
		name     string
		tool     string
		existing bool
	}{
		{"write_existing", "write_file", true},
		{"edit_existing", "edit_file", true},
		{"create_collision", "write_file", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root, artifacts := t.TempDir(), t.TempDir()
			path := filepath.Join(root, relative)
			if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			expected := "missing"
			if scenario.existing {
				expected = workspace.Hash([]byte(original))
				if err := os.WriteFile(path, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
			}
			approvals := 0
			m, err := New(root, Options{ArtifactDir: artifacts, Approve: func(context.Context, Approval) error {
				approvals++
				return os.WriteFile(path, []byte(external), 0600)
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			args := map[string]any{"path": relative, "expectedHash": expected}
			if scenario.tool == "write_file" {
				args["content"] = "model change\n"
			} else {
				args["oldText"], args["newText"] = "original code", "model change"
			}
			result, err := m.Execute(t.Context(), scenario.tool, args)
			assertRevisionHint(t, err, relative, workspace.Hash([]byte(external)))
			if approvals != 1 || len(result.Artifacts) != 0 {
				t.Fatalf("race handling changed: approvals=%d result=%+v", approvals, result)
			}
			assertRevisionFileAndNoArtifacts(t, path, external, artifacts)
		})
	}
}

func TestWriteRevisionPreflightKeepsSymlinkGuard(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	outsidePath := filepath.Join(outside, "private.py")
	if err := os.WriteFile(outsidePath, []byte("outside source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(root, "linked.py")); err != nil {
		t.Fatal(err)
	}
	approvals := 0
	m, err := New(root, Options{Approve: func(context.Context, Approval) error { approvals++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.Execute(t.Context(), "write_file", map[string]any{"path": "linked.py", "expectedHash": "missing", "content": "replacement"}); err == nil || approvals != 0 {
		t.Fatalf("symlink collision reached approval: %v count=%d", err, approvals)
	}
	data, err := os.ReadFile(outsidePath)
	if err != nil || string(data) != "outside source" {
		t.Fatalf("outside file changed: %q %v", data, err)
	}
}

func assertRevisionHint(t *testing.T, err error, path, currentHash string) {
	t.Helper()
	if !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("revision conflict identity was lost: %v", err)
	}
	if !strings.Contains(err.Error(), "read_file with path "+strconv.Quote(path)) || !strings.Contains(err.Error(), "revision conflict for "+strconv.Quote(path)) {
		t.Fatalf("missing exact affected-path recovery hint: %v", err)
	}
	if strings.Contains(err.Error(), currentHash) {
		t.Fatal("error disclosed a fresh hash without a read")
	}
}

func assertRevisionFileAndNoArtifacts(t *testing.T, path, expected, artifacts string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != expected {
		t.Fatalf("file mutated: got %q, err=%v", data, err)
	}
	entries, err := os.ReadDir(artifacts)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected revision left artifacts: %v %v", entries, err)
	}
}
