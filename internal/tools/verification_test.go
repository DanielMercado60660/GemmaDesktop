package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/workspace"
)

type verifierFixture struct {
	requests []execution.Request
	change   bool
}

func (*verifierFixture) Description() string { return "recording verifier" }
func (x *verifierFixture) Run(_ context.Context, r execution.Request) (execution.Result, error) {
	x.requests = append(x.requests, r)
	if x.change {
		if err := os.WriteFile(filepath.Join(r.Workspace, "source.py"), []byte("external edit"), 0600); err != nil {
			return execution.Result{}, err
		}
	}
	return execution.Result{ReadOnly: r.ReadOnly, ExitCode: 0, Publication: "read_only"}, nil
}

func TestVerificationUsesFrozenCommandAndRejectsConcurrentSourceChange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.py"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	x := &verifierFixture{}
	contract := &execution.VerificationContract{Command: []string{"python3", "-m", "unittest"}, ProtectedPaths: []string{"public_test.py"}}
	m, err := New(root, Options{Executor: x, Verification: contract, Approve: func(context.Context, Approval) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	contract.Command[0] = "untrusted replacement"
	if _, err = m.Execute(t.Context(), "verify_task", map[string]any{"executable": "anything"}); err == nil {
		t.Fatal("model chose verification executable")
	}
	r, err := m.Execute(t.Context(), "verify_task", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	b, _ := json.Marshal(r.Data)
	_ = json.Unmarshal(b, &data)
	if data["verificationPassed"] != true || data["sourceDigest"] == "" || data["sourceDigest"] != data["sourceDigestAfter"] {
		t.Fatal(data)
	}
	if len(x.requests) != 1 || x.requests[0].Executable != "python3" || !x.requests[0].ReadOnly || !reflect.DeepEqual(x.requests[0].ProtectedPaths, []string{"public_test.py"}) {
		t.Fatal(x.requests)
	}
	x.change = true
	if _, err = m.Execute(t.Context(), "verify_task", map[string]any{}); err == nil {
		t.Fatal("stale source verification passed")
	}
}

func TestTaskProtectionRejectsFileMutationAndReachesCommandExecutor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "public_test.py"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	x := &verifierFixture{}
	approvals := 0
	m, err := New(root, Options{Executor: x, Verification: &execution.VerificationContract{Command: []string{"python3", "-m", "unittest"}, ProtectedPaths: []string{"public_test.py"}}, Approve: func(context.Context, Approval) error { approvals++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Execute(t.Context(), "write_file", map[string]any{"path": "public_test.py", "content": "erase tests", "expectedHash": workspace.Hash([]byte("preserve"))}); err == nil {
		t.Fatal("protected test overwritten")
	}
	if approvals != 0 {
		t.Fatal("protected edit requested approval")
	}
	if _, err = m.Execute(t.Context(), "run_command", map[string]any{"executable": "python3", "args": []string{"script.py"}}); err != nil {
		t.Fatal(err)
	}
	if len(x.requests) != 1 || !reflect.DeepEqual(x.requests[0].ProtectedPaths, []string{"public_test.py"}) {
		t.Fatal("command lost protection", x.requests)
	}
}

type noisyVerifier struct{ exit int }

func (*noisyVerifier) Description() string { return "noisy verifier" }
func (x *noisyVerifier) Run(_ context.Context, r execution.Request) (execution.Result, error) {
	// Escaping-heavy output whose encoding exceeds the 128 KiB tool-result cap.
	noisy := strings.Repeat("\"quoted\"\t\\path\n", 12<<10)
	return execution.Result{ReadOnly: r.ReadOnly, ExitCode: x.exit, Stdout: noisy, Stderr: noisy, Publication: "read_only"}, nil
}

func TestVerificationReceiptSurvivesLargeOutput(t *testing.T) {
	for _, exit := range []int{0, 1} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "source.py"), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		m, err := New(root, Options{Executor: &noisyVerifier{exit: exit}, Verification: &execution.VerificationContract{Command: []string{"python3", "-m", "unittest"}}, Approve: func(context.Context, Approval) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		r, runErr := m.Execute(t.Context(), "verify_task", map[string]any{})
		m.Close()
		data, _ := r.Data.(map[string]any)
		if data == nil || data["verificationPassed"] != (exit == 0) || data["readOnly"] != true || data["sourceDigest"] == "" || data["sourceDigest"] != data["sourceDigestAfter"] || data["truncated"] != true {
			t.Fatalf("exit=%d lost receipt fields: %v", exit, data)
		}
		if (runErr == nil) != (exit == 0) || len(r.Content) > 129<<10 || !strings.Contains(r.Content, "verificationPassed") {
			t.Fatalf("exit=%d err=%v content=%d bytes", exit, runErr, len(r.Content))
		}
	}
}

type outcomeExecutor struct {
	result execution.Result
	err    error
	write  bool // the check changes source while it runs
}

func (*outcomeExecutor) Description() string { return "outcome fixture" }
func (x *outcomeExecutor) Run(_ context.Context, r execution.Request) (execution.Result, error) {
	if x.write {
		if err := os.WriteFile(filepath.Join(r.Workspace, "source.py"), []byte("rewritten by the check"), 0600); err != nil {
			return execution.Result{}, err
		}
	}
	return x.result, x.err
}

// checkOutcome decides whether completion feedback is repair evidence; it must
// never call a check that did not run, or that a host limit cut short, failed.
func TestVerificationReceiptClassifiesCheckOutcome(t *testing.T) {
	ran := func(exit int) execution.Result {
		return execution.Result{ExitCode: exit, ReadOnly: true, Environment: "macOS Seatbelt · single process · network denied", Publication: "read_only"}
	}
	docker := func(exit int, stderr string) execution.Result {
		return execution.Result{ExitCode: exit, Stderr: stderr, ReadOnly: true, Environment: "docker-linux", Publication: "read_only"}
	}
	truncated, cancelled, signalled, oom, notReadOnly := ran(0), ran(-1), ran(-1), docker(137, ""), ran(0)
	truncated.Truncated, cancelled.Cancelled, signalled.Signal, oom.Publication, notReadOnly.ReadOnly = true, true, "segmentation fault", "discarded_oom", false
	launcher := func(exit int, stdout, stderr string) execution.Result {
		r := ran(exit)
		r.Stdout, r.Stderr = stdout, stderr
		return r
	}
	limited := func(sig syscall.Signal) outcomeExecutor {
		r := ran(-1)
		r.Signal = sig.String()
		return outcomeExecutor{result: r, err: fmt.Errorf("sandboxed process terminated by signal %s", r.Signal)}
	}
	for _, tc := range []struct {
		name    string
		x       outcomeExecutor
		outcome string
	}{
		{"passed", outcomeExecutor{result: ran(0)}, "passed"},
		{"exit_nonzero", outcomeExecutor{result: ran(1)}, "failed"},
		{"truncated_nonzero", outcomeExecutor{result: func() execution.Result { r := ran(2); r.Truncated = true; return r }()}, "failed"},
		{"signal", outcomeExecutor{result: signalled, err: errors.New("sandboxed process terminated by signal segmentation fault")}, "failed"},
		{"source_changed", outcomeExecutor{result: ran(0), write: true}, "failed"},
		// Runner.Prepare and other pre-start failures return a zero Result.
		{"error_before_start", outcomeExecutor{err: errors.New("working directory is private")}, "not_run"},
		{"start_failed", outcomeExecutor{result: ran(-1), err: errors.New("fork/exec /usr/bin/sandbox-exec: resource temporarily unavailable")}, "not_run"},
		{"sandbox_unavailable", outcomeExecutor{result: ran(71), err: fmt.Errorf("%w: sandbox-exec: execvp() of 'pytest' failed: No such file or directory", execution.ErrUnavailable)}, "not_run"},
		{"docker_unavailable", outcomeExecutor{result: docker(-1, ""), err: errors.New("Docker daemon is unavailable")}, "not_run"},
		{"docker_missing_executable", outcomeExecutor{result: docker(127, "gemma-exec: 1: exec: pytest: not found\n")}, "not_run"},
		{"docker_exit_127_from_check", outcomeExecutor{result: docker(127, "make: *** [test] Error 127\n")}, "failed"},
		// Seatbelt's set -e launcher exits 1 with a nil error when it cannot
		// exec the target, so only its "gemma-exec: " report tells it apart.
		{"seatbelt_unexecutable", outcomeExecutor{result: launcher(1, "", "gemma-exec: /w/run_tests.sh: Permission denied\n")}, "not_run"},
		{"seatbelt_target_output_first", outcomeExecutor{result: launcher(1, "collected 3 items\n", "gemma-exec: not the launcher\n")}, "failed"},
		{"seatbelt_check_failure", outcomeExecutor{result: launcher(1, "", "FAIL: test_divide\n")}, "failed"},
		{"cpu_limit", limited(syscall.SIGXCPU), "host_limit"},
		{"file_size_limit", limited(syscall.SIGXFSZ), "host_limit"},
		{"truncated_exit_0", outcomeExecutor{result: truncated}, "host_limit"},
		{"timeout", outcomeExecutor{result: cancelled, err: context.DeadlineExceeded}, "host_limit"},
		{"oom", outcomeExecutor{result: oom, err: errors.New("container exceeded its memory limit")}, "host_limit"},
		{"writable_mount_exit_0", outcomeExecutor{result: notReadOnly}, "host_limit"},
		{"error_after_exit_0", outcomeExecutor{result: ran(0), err: errors.New("Docker attach outcome uncertain")}, "host_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "source.py"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			m, err := New(root, Options{Executor: &tc.x, Verification: &execution.VerificationContract{Command: []string{"python3", "-m", "pytest"}}, Approve: func(context.Context, Approval) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			r, runErr := m.Execute(t.Context(), "verify_task", map[string]any{})
			data, _ := r.Data.(map[string]any)
			if data == nil || data["checkOutcome"] != tc.outcome || data["verificationPassed"] != (tc.outcome == "passed") || (runErr == nil) != (tc.outcome == "passed") || !strings.Contains(r.Content, `"checkOutcome":"`+tc.outcome+`"`) {
				t.Fatalf("outcome=%v passed=%v err=%v", data["checkOutcome"], data["verificationPassed"], runErr)
			}
		})
	}
}

// The real Seatbelt launcher, given a declared check it cannot exec, must not
// produce a "failed" receipt whose exit status would read as a test failure.
func TestSeatbeltLauncherFailureIsNotRun(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS-only")
	}
	root := t.TempDir()
	script := filepath.Join(root, "run_tests.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := New(root, Options{Executor: execution.NewRunner(), Verification: &execution.VerificationContract{Command: []string{script}}, Approve: func(context.Context, Approval) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r, runErr := m.Execute(t.Context(), "verify_task", map[string]any{})
	data, _ := r.Data.(map[string]any)
	if errors.Is(runErr, execution.ErrUnavailable) {
		t.Skip("Seatbelt is unavailable here")
	}
	if data == nil || data["checkOutcome"] != "not_run" || data["verificationPassed"] != false || runErr == nil || !strings.HasPrefix(fmt.Sprint(data["environment"]), "macOS Seatbelt") {
		t.Fatalf("outcome=%v exit=%v stderr=%q err=%v", data["checkOutcome"], data["exitCode"], data["stderr"], runErr)
	}
}
