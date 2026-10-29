package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/workspace"
)

type testExecutor struct {
	called  bool
	request execution.Request
}

func (e *testExecutor) Description() string { return "test isolated executor" }
func (e *testExecutor) Run(_ context.Context, r execution.Request) (execution.Result, error) {
	e.called = true
	e.request = r
	return execution.Result{ExitCode: 0, CommandID: "owned", Image: "sha256:pinned", SnapshotDigest: "baseline", Publication: "applied", Changes: []workspace.CommandChange{{Path: "file.txt", BeforeHash: "before", AfterHash: "after"}}}, nil
}
func TestSelectedExecutorApprovalAndProvenance(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "approved", true: "denied"}[denied], func(t *testing.T) {
			x := &testExecutor{}
			m, e := New(t.TempDir(), Options{Executor: x, Approve: func(_ context.Context, a Approval) error {
				if !strings.Contains(a.Reason, "test isolated executor") {
					t.Fatal(a)
				}
				if denied {
					return execution.ErrDenied
				}
				return nil
			}})
			if e != nil {
				t.Fatal(e)
			}
			defer m.Close()
			r, e := m.Execute(t.Context(), "run_command", map[string]any{"executable": "go", "args": []any{"test", "./..."}, "readOnly": true})
			if denied {
				if !errors.Is(e, execution.ErrDenied) || x.called {
					t.Fatalf("%+v %v", r, e)
				}
				return
			}
			if e != nil || !x.called || !x.request.ReadOnly {
				t.Fatalf("%+v %v", r, e)
			}
			data := r.Data.(map[string]any)
			if data["publication"] != "applied" || data["snapshotDigest"] != "baseline" || data["image"] != "sha256:pinned" {
				t.Fatal(data)
			}
		})
	}
}
func TestLargeCommandChangeProjectionRemainsBounded(t *testing.T) {
	v := execution.Result{CommandID: "test", Changes: []workspace.CommandChange{}}
	for i := 0; i < 256; i++ {
		v.Changes = append(v.Changes, workspace.CommandChange{Path: strings.Repeat("\t", 1024)})
	}
	r := encodeProcess(v)
	if len(r.Content) > 128<<10 || r.Data.(map[string]any)["changesLimited"] != true {
		t.Fatal("command projection not bounded")
	}
}
func TestGitInspectionShowsHistoricalSourceAndBlocksCredentials(t *testing.T) {
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("opt-in real native Git sandbox")
	}
	root := t.TempDir()
	git := execution.GitExecutable()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git: %s %v", b, e)
		}
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(root, "source.txt"), []byte("first\n"), 0644)
	os.WriteFile(filepath.Join(root, ".npmrc"), []byte("secret-token"), 0600)
	run("add", ".")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "initial")
	m, e := New(root, Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	r, e := m.Execute(t.Context(), "git_file", map[string]any{"path": "source.txt", "revision": "HEAD"})
	if e != nil || !strings.Contains(r.Content, "first") {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = m.Execute(t.Context(), "git_file", map[string]any{"path": ".npmrc", "revision": "HEAD"}); !errors.Is(e, workspace.ErrProtected) {
		t.Fatal(e)
	}
	r, e = m.Execute(t.Context(), "git_blame", map[string]any{"path": "source.txt"})
	if e != nil || !strings.Contains(r.Content, "first") {
		t.Fatalf("%+v %v", r, e)
	}
}
