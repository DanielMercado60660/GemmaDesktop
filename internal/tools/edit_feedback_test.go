package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/workspace"
)

func TestEditReplayRequiresSameConversationOperationAndRevision(t *testing.T) {
	for _, scenario := range []string{"same request", "old revision supplied", "different conversation", "external revision", "new operation", "new manager"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "f.py")
			if err := os.WriteFile(file, []byte("value = 1\n"), 0600); err != nil {
				t.Fatal(err)
			}
			approvals := 0
			opts := Options{ArtifactDir: t.TempDir(), Approve: func(context.Context, Approval) error { approvals++; return nil }}
			m, err := New(root, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			ctx := WithRevisionScope(t.Context(), "owner")
			if _, err = m.Execute(ctx, "read_file", map[string]any{"path": "f.py"}); err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"path": "f.py", "oldText": "value = 1", "newText": "value = 10"}
			result, err := m.Execute(ctx, "edit_file", args)
			if err != nil {
				t.Fatal(err)
			}
			data := result.Data.(map[string]any)
			if data["applied"] != true || data["replacements"] != 1 || !strings.Contains(result.Content, "Edit applied successfully") || !strings.Contains(data["diffPreview"].(string), "+value = 10") {
				t.Fatalf("bad edit feedback: %+v", result)
			}
			// oldText remains a substring of newText: blind retry would produce 100.
			switch scenario {
			case "old revision supplied":
				args["expectedHash"] = workspace.Hash([]byte("value = 1\n"))
			case "different conversation":
				ctx = WithRevisionScope(t.Context(), "other")
			case "external revision":
				if err = os.WriteFile(file, []byte("value = 10\n# external\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "new operation":
				args["newText"] = "value = 2"
			case "new manager":
				m2, e := New(root, opts)
				if e != nil {
					t.Fatal(e)
				}
				defer m2.Close()
				m = m2
			}
			if scenario == "different conversation" || scenario == "new manager" {
				// Knowing current contents alone must not count as having applied an edit.
				if _, err = m.Execute(ctx, "read_file", map[string]any{"path": "f.py"}); err != nil {
					t.Fatal(err)
				}
			}
			result, err = m.Execute(ctx, "edit_file", args)
			data, _ = result.Data.(map[string]any)
			if scenario == "same request" || scenario == "old revision supplied" {
				if err != nil || data["alreadyApplied"] != true || data["replacements"] != 0 || approvals != 1 || len(result.Artifacts) != 0 {
					t.Fatalf("unsafe replay: %+v, %v, approvals=%d", result, err, approvals)
				}
				got, _ := os.ReadFile(file)
				if string(got) != "value = 10\n" {
					t.Fatalf("replayed mutation: %s", got)
				}
			} else if data["alreadyApplied"] == true {
				t.Fatalf("wrongly reported replay: %+v", result)
			}
			if scenario == "external revision" && (err == nil || approvals != 1) {
				t.Fatalf("external revision was accepted: %v", err)
			}
		})
	}
}

func TestExistingNewTextIsNotEvidenceOfAppliedEdit(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "f.py"), []byte("already_here = 2\n"), 0600)
	m, err := New(root, Options{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Execute(t.Context(), "read_file", map[string]any{"path": "f.py"})
	_, err = m.Execute(t.Context(), "edit_file", map[string]any{"path": "f.py", "oldText": "absent = 1", "newText": "already_here = 2"})
	if err == nil || !strings.Contains(err.Error(), "oldText was not found") {
		t.Fatalf("false success: %v", err)
	}
}

func TestEditDiffPreviewIsBoundedAndKeepsChangeLocation(t *testing.T) {
	before := "unchanged\n" + strings.Repeat("old "+strings.Repeat("x", 10000)+"\n", 40) + "tail\n"
	after := "unchanged\n" + strings.Repeat("new "+strings.Repeat("y", 10000)+"\n", 40) + "tail\n"
	preview := editDiffPreview(before, after)
	if len(strings.Split(preview, "\n")) > 20 || len(preview) > 4000 || !strings.HasPrefix(preview, "@@ -2,40 +2,40 @@") || strings.Contains(preview, "tail") {
		t.Fatalf("bad preview: %s", preview)
	}
	if editDiffPreview("same", "same") != "No text difference." {
		t.Fatal("unchanged preview")
	}
}

func TestExistingDirectoryIsNoOpIncludingProtectedParent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests", "fixed.py"), []byte("protected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tests", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	approvals := 0
	m, err := New(root, Options{ArtifactDir: t.TempDir(), Verification: &execution.VerificationContract{Command: []string{"python3"}, ProtectedPaths: []string{"tests/fixed.py", "missing/fixed.py"}}, Approve: func(context.Context, Approval) error { approvals++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, name := range []string{"tests", "./tests"} {
		result, err := m.Execute(t.Context(), "make_directory", map[string]any{"path": name})
		if err != nil {
			t.Fatal(err)
		}
		data := result.Data.(map[string]any)
		if data["created"] != false || data["exists"] != true || approvals != 0 {
			t.Fatalf("not a no-op: %+v approvals=%d", result, approvals)
		}
	}
	for _, name := range []string{"tests/fixed.py", "missing", "link", ".agents", "../outside"} {
		if result, err := m.Execute(t.Context(), "make_directory", map[string]any{"path": name}); err == nil {
			t.Fatalf("unsafe no-op %q: %+v", name, result)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(root, "tests", "fixed.py")); string(got) != "protected" {
		t.Fatal("protected input changed")
	}
}

func TestProtectedDirectoryNoOpCannotBecomeMutationAfterGuidance(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	approvals := 0
	m, err := New(root, Options{ArtifactDir: t.TempDir(), Verification: &execution.VerificationContract{Command: []string{"python3"}, ProtectedPaths: []string{"tests/fixed.py"}}, Approve: func(context.Context, Approval) error { approvals++; return nil }, BeforeAccess: func(context.Context, string, map[string]any) (Result, error) {
		return Result{}, os.Remove(filepath.Join(root, "tests"))
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, err = m.Execute(t.Context(), "make_directory", map[string]any{"path": "tests"})
	if err == nil || !strings.Contains(err.Error(), "no directory was created") || approvals != 0 {
		t.Fatalf("race widened no-op authority: %v approvals=%d", err, approvals)
	}
	if _, err = os.Stat(filepath.Join(root, "tests")); !os.IsNotExist(err) {
		t.Fatal("protected ancestor was recreated")
	}
}
