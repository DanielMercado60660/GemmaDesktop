package tools

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestApprovalDenialAndReadOnly(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("original"), 0600)
	m, err := New(root, Options{Approve: func(context.Context, Approval) error { return execution.ErrDenied }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	args := map[string]any{"path": "a", "content": "changed", "expectedHash": workspace.Hash([]byte("original"))}
	if _, err = m.Execute(context.Background(), "write_file", args); !errors.Is(err, execution.ErrDenied) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a"))
	if string(data) != "original" {
		t.Fatal("denied write happened")
	}
	ro, err := New(root, Options{Mode: "read-only"})
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for _, d := range ro.List() {
		if !readTool(d.Name) {
			t.Fatalf("mutator advertised %s", d.Name)
		}
	}
	if _, err = ro.Execute(context.Background(), "run_command", map[string]any{"executable": "echo"}); err == nil {
		t.Fatal("read-only shell accepted")
	}
}
func TestRevisionChangeDuringApprovalAndCheckpoint(t *testing.T) {
	root := t.TempDir()
	artifacts := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("original"), 0600)
	changeDuringApproval := true
	m, err := New(root, Options{ArtifactDir: artifacts, Approve: func(context.Context, Approval) error {
		if changeDuringApproval {
			os.WriteFile(filepath.Join(root, "a"), []byte("user edit"), 0600)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	args := map[string]any{"path": "a", "content": "changed", "expectedHash": workspace.Hash([]byte("original"))}
	if _, err = m.Execute(context.Background(), "write_file", args); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("stale approval accepted %v", err)
	}
	changeDuringApproval = false
	args["expectedHash"] = workspace.Hash([]byte("user edit"))
	r, err := m.Execute(context.Background(), "write_file", args)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Artifacts) != 2 {
		t.Fatalf("no durable before snapshot %+v", r)
	}
	b, err := os.ReadFile(r.Artifacts[1].Path)
	if err != nil || !strings.Contains(string(b), "user edit") {
		t.Fatalf("snapshot %s %v", b, err)
	}
	var saved map[string]any
	if err = json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
}
func TestValidationAndArtifactBoundary(t *testing.T) {
	root := t.TempDir()
	m, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Execute(context.Background(), "read_file", map[string]any{"path": "a", "unexpected": true}); err == nil {
		t.Fatal("unknown argument accepted")
	}
	if _, err = New(root, Options{ArtifactDir: filepath.Join(root, "artifacts")}); err == nil {
		t.Fatal("repository artifact directory accepted")
	}
	for _, d := range m.List() {
		if strings.Contains(d.Name, "terminal") {
			t.Fatal("model can access user terminal")
		}
	}
}

func TestMoveArtifactsDescribeBothFileChanges(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "old.py"), []byte("print('moved')\n"), 0600)
	m, err := New(root, Options{ArtifactDir: t.TempDir(), Approve: func(context.Context, Approval) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r, err := m.Execute(context.Background(), "move_file", map[string]any{"path": "old.py", "destination": "new.py", "expectedHash": workspace.Hash([]byte("print('moved')\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Artifacts) != 3 {
		t.Fatalf("expected checkpoint plus source/destination diffs: %+v", r.Artifacts)
	}
	source, dest := r.Artifacts[1], r.Artifacts[2]
	if source.Path != "old.py" || source.Before == "" || source.After != "" || dest.Path != "new.py" || dest.Before != "" || dest.After != source.Before {
		t.Fatalf("incorrect move artifacts: %+v", r.Artifacts)
	}
}

func TestSnapshotUsesReviewedBeforeImage(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "store.py"), []byte("reviewed"), 0600)
	m, err := New(root, Options{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	before, err := m.workspace.Read("store.py")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "store.py"), []byte("later external edit"), 0600)
	a, err := m.snapshotFiles(context.Background(), []workspace.File{before}, "reviewed snapshot")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files []workspace.File `json:"files"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Content != "reviewed" || manifest.Files[0].Hash != before.Hash {
		t.Fatalf("snapshot reread a later revision: %s", data)
	}
}

func TestConcurrentEditsRejectOneStaleRevision(t *testing.T) {
	root := t.TempDir()
	original := "original\n"
	os.WriteFile(filepath.Join(root, "store.py"), []byte(original), 0600)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	approve := func(context.Context, Approval) error { arrived <- struct{}{}; <-release; return nil }
	m, err := New(root, Options{ArtifactDir: t.TempDir(), Approve: approve})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, content := range []string{"first\n", "second\n"} {
		wg.Add(1)
		go func(content string) {
			defer wg.Done()
			_, e := m.Execute(context.Background(), "write_file", map[string]any{"path": "store.py", "content": content, "expectedHash": workspace.Hash([]byte(original))})
			results <- e
		}(content)
	}
	<-arrived
	// Workspace operations now serialize before approval. The second edit is
	// revision-checked after the first publishes and must fail as stale.
	close(release)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, workspace.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("edits success=%d conflict=%d", success, conflict)
	}
}

func TestPendingApprovalUsesDetachedArguments(t *testing.T) {
	root := t.TempDir()
	pending := make(chan struct{})
	release := make(chan struct{})
	m, err := New(root, Options{Approve: func(context.Context, Approval) error { close(pending); <-release; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	args := map[string]any{"path": "approved.py", "content": "approved content", "expectedHash": "missing"}
	done := make(chan error, 1)
	go func() { _, e := m.Execute(context.Background(), "write_file", args); done <- e }()
	<-pending
	args["path"] = "unapproved.py"
	args["content"] = "unapproved content"
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "approved.py"))
	if err != nil || string(b) != "approved content" {
		t.Fatalf("approved arguments changed: %q %v", b, err)
	}
	if _, err = os.Stat(filepath.Join(root, "unapproved.py")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pending caller mutation bypassed approval")
	}
}

func TestNoisyCommandKeepsStructuredFailure(t *testing.T) {
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("real sandbox qualification is opt-in")
	}
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "nested"), 0700)
	m, err := New(root, Options{Approve: func(ctx context.Context, a Approval) error {
		if a.Arguments["workingDirectory"] != "nested" {
			t.Error("working directory missing from approval")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r, err := m.Execute(context.Background(), "run_command", map[string]any{"executable": "/opt/homebrew/opt/python@3.14/bin/python3.14", "workingDirectory": "nested", "args": []string{"-c", "import sys; sys.stdout.write('start stdout\\n'+'\\x01'*200000+'\\nfinal stdout\\n'); sys.stderr.write('start stderr\\n'+'\\x01'*200000+'\\nAssertionError: SQLite count mismatch\\nFAILED (failures=1)\\n'); sys.exit(1)"}})
	if err == nil {
		t.Fatal("failed process reported success")
	}
	if len(r.Content) > 128<<10 {
		t.Fatal("JSON result exceeds output budget")
	}
	var data map[string]any
	if e := json.Unmarshal([]byte(r.Content), &data); e != nil {
		t.Fatalf("diagnostics are not valid JSON: %v", e)
	}
	if data["exitCode"] != float64(1) || data["truncated"] != true || !strings.Contains(data["stdout"].(string), "final stdout") || !strings.Contains(data["stderr"].(string), "AssertionError: SQLite count mismatch") || !strings.Contains(data["stderr"].(string), "FAILED (failures=1)") {
		t.Fatalf("failure status or diagnostic tail lost: %+v", data)
	}
}

func TestReadOnlySQLiteTestsUsePrivateTemporaryDirectory(t *testing.T) {
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("real sandbox qualification is opt-in")
	}
	root := t.TempDir()
	code := `import pathlib, sqlite3, tempfile, unittest
class DatabaseTests(unittest.TestCase):
    def test_temporary_database(self):
        with tempfile.TemporaryDirectory() as scratch:
            connection = sqlite3.connect(str(pathlib.Path(scratch) / 'test.db'))
            connection.execute('pragma journal_mode=wal')
            connection.execute('create table items (value text)')
            connection.execute('insert into items values (?)', ('preserved',))
            connection.commit()
            self.assertEqual(connection.execute('select value from items').fetchone()[0], 'preserved')
            connection.close()
    def test_workspace_is_protected(self):
        with self.assertRaises(PermissionError):
            pathlib.Path('test_readonly.py').write_text('tampered')
`
	path := filepath.Join(root, "test_readonly.py")
	if err := os.WriteFile(path, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	approved := false
	m, err := New(root, Options{Approve: func(ctx context.Context, a Approval) error {
		approved = true
		if a.Arguments["readOnly"] != true || !strings.Contains(a.Reason, "workspace is read-only") || !strings.Contains(a.Reason, "private temporary directory") {
			t.Fatalf("unclear approval authority: %+v", a)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r, err := m.Execute(context.Background(), "run_command", map[string]any{"executable": "/opt/homebrew/opt/python@3.14/bin/python3.14", "args": []string{"-m", "unittest", "-v"}, "readOnly": true})
	if err != nil {
		t.Fatalf("read-only SQLite tests: %s %v", r.Content, err)
	}
	data := r.Data.(map[string]any)
	if !approved || data["readOnly"] != true || !strings.Contains(data["stderr"].(string), "OK") {
		t.Fatalf("read-only verification did not pass: %s", r.Content)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != code {
		t.Fatal("test code changed despite read-only command")
	}
}

func TestInvalidEditDoesNotRequestApproval(t *testing.T) {
	root := t.TempDir()
	content := "repeat repeat\nunique\n"
	os.WriteFile(filepath.Join(root, "file.py"), []byte(content), 0600)
	approvals := 0
	m, err := New(root, Options{Approve: func(context.Context, Approval) error { approvals++; return execution.ErrDenied }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, old := range []string{"missing", "repeat", `repeat repeat\nunique`} {
		_, err = m.Execute(context.Background(), "edit_file", map[string]any{"path": "file.py", "oldText": old, "newText": "changed", "expectedHash": workspace.Hash([]byte(content))})
		if err == nil || errors.Is(err, execution.ErrDenied) {
			t.Fatalf("invalid edit reached approval: %q %v", old, err)
		}
	}
	if approvals != 0 {
		t.Fatalf("invalid edits requested %d approvals", approvals)
	}
	_, err = m.Execute(context.Background(), "edit_file", map[string]any{"path": "file.py", "oldText": "unique", "newText": "changed", "expectedHash": workspace.Hash([]byte(content))})
	if !errors.Is(err, execution.ErrDenied) || approvals != 1 {
		t.Fatalf("valid edit did not request approval: %v count=%d", err, approvals)
	}
	after, _ := os.ReadFile(filepath.Join(root, "file.py"))
	if string(after) != content {
		t.Fatal("invalid or denied edit changed content")
	}
}

func TestWriteAndEditRequireHashBeforeApproval(t *testing.T) {
	for _, tool := range []string{"write_file", "edit_file"} {
		for _, existing := range []bool{false, true} {
			for _, hashPresent := range []bool{false, true} {
				name := tool + "/new/omitted"
				if existing {
					name = tool + "/existing/omitted"
				}
				if hashPresent {
					name = strings.TrimSuffix(name, "omitted") + "empty"
				}
				t.Run(name, func(t *testing.T) {
					root, artifacts := t.TempDir(), t.TempDir()
					path := filepath.Join(root, "file.py")
					if existing {
						if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					approvals := 0
					m, err := New(root, Options{ArtifactDir: artifacts, Approve: func(context.Context, Approval) error { approvals++; return nil }})
					if err != nil {
						t.Fatal(err)
					}
					defer m.Close()
					args := map[string]any{"path": "file.py"}
					if tool == "write_file" {
						args["content"] = "changed\n"
					} else {
						args["oldText"], args["newText"] = "original", "changed"
					}
					if hashPresent {
						args["expectedHash"] = ""
					}
					result, err := m.Execute(t.Context(), tool, args)
					const recovery = "expectedHash is required; use missing for a new file, otherwise current SHA256 from read_file"
					if err == nil || err.Error() != recovery || approvals != 0 || len(result.Artifacts) != 0 {
						t.Fatalf("hash preflight failed: err=%v approvals=%d result=%+v", err, approvals, result)
					}
					data, readErr := os.ReadFile(path)
					if existing && (readErr != nil || string(data) != "original\n") {
						t.Fatalf("existing file changed: %q %v", data, readErr)
					}
					if !existing && !errors.Is(readErr, os.ErrNotExist) {
						t.Fatalf("new file created: %q %v", data, readErr)
					}
					entries, err := os.ReadDir(artifacts)
					if err != nil || len(entries) != 0 {
						t.Fatalf("rejected request created artifacts: %v %v", entries, err)
					}
				})
			}
		}
	}
}
