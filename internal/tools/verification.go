package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/workspace"
)

func (m *Manager) verifyTask(ctx context.Context) (Result, error) {
	v := m.options.Verification
	if v == nil {
		return Result{}, errors.New("no host verification contract")
	}
	args := map[string]any{"executable": v.Command[0], "args": v.Command[1:], "workingDirectory": v.WorkingDirectory, "readOnly": true}
	if err := m.approve(ctx, "verify_task", args, "Run the frozen task verification command with source mounted read-only."); err != nil {
		return Result{}, err
	}
	before, err := workspace.ScanCommandTree(ctx, m.workspace.Path, "", workspace.DefaultPrivatePaths)
	if err != nil {
		return Result{}, err
	}
	executor := m.options.Executor
	if executor == nil {
		executor = execution.NewRunner()
	}
	r, runErr := executor.Run(ctx, execution.Request{Workspace: m.workspace.Path, WorkingDirectory: v.WorkingDirectory, Executable: v.Command[0], Args: append([]string{}, v.Command[1:]...), ReadOnly: true, ProtectedPaths: append([]string{}, v.ProtectedPaths...), Timeout: 2 * time.Minute})
	after, scanErr := workspace.ScanCommandTree(ctx, m.workspace.Path, "", workspace.DefaultPrivatePaths)
	passed := runErr == nil && scanErr == nil && r.ExitCode == 0 && r.ReadOnly && !r.Truncated && !r.Cancelled && !r.WorkspaceChanged && before.Digest == after.Digest
	outcome := checkOutcome(r, runErr, passed, r.WorkspaceChanged || scanErr == nil && before.Digest != after.Digest)
	if !passed && runErr == nil {
		runErr = fmt.Errorf("verification failed: exit=%d, source unchanged=%v, complete output=%v", r.ExitCode, scanErr == nil && before.Digest == after.Digest, !r.Truncated)
	}
	// Bounded process encoding shrinks only the output tails, so a noisy check
	// cannot drop the receipt fields that decide whether verification passed.
	result := encodeProcess(r)
	data, _ := result.Data.(map[string]any)
	contractBytes, _ := json.Marshal(v)
	data["verificationPassed"] = passed
	data["checkOutcome"] = outcome
	data["sourceDigest"] = before.Digest
	data["sourceDigestAfter"] = after.Digest
	data["contractDigest"] = workspace.Hash(contractBytes)
	data["command"] = append([]string{}, v.Command...)
	encoded, _ := json.Marshal(data)
	result.Content = string(encoded)
	return result, errors.Join(runErr, scanErr)
}

// checkOutcome says whether a receipt that did not pass is evidence about the
// source; it never decides verificationPassed. "failed": the check ran to an
// exit status or changed source while running, so repair feedback is
// meaningful. "not_run": no process outcome exists, because the executor
// failed before the target started (invalid request, sandbox or Docker
// unavailable, target not executable) or lost its exit status. "host_limit":
// a host bound cut the check short (timeout, cancellation, CPU, file size,
// memory), or it exited 0 without a trustworthy receipt (truncated output, a
// writable mount, an executor error after exit). Editing source cannot fix
// either of those.
func checkOutcome(r execution.Result, runErr error, passed, sourceChanged bool) string {
	switch {
	case passed:
		return "passed"
	// The Seatbelt wrapper's ulimit -t/-f end a check by signal; that is the
	// CPU and file-size form of the wall-clock timeout.
	case r.Cancelled || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || r.Publication == "discarded_oom" || r.Signal != "" && (r.Signal == syscall.SIGXCPU.String() || r.Signal == syscall.SIGXFSZ.String()):
		return "host_limit"
	// Runner returns a zero Result (no environment) when preparation fails and
	// exit -1 without a signal when the process could not start; Docker keeps
	// exit -1 until it has read the container's exit status.
	case runErr != nil && (errors.Is(runErr, execution.ErrUnavailable) || r.Environment == "" || r.ExitCode < 0 && r.Signal == ""):
		return "not_run"
	case wrapperFailed(r):
		return "not_run"
	case sourceChanged || r.ExitCode != 0 || r.Signal != "":
		return "failed"
	}
	return "host_limit"
}

// wrapperFailed reports that the executor's fixed /bin/sh launcher, which runs
// as $0 "gemma-exec", could not exec the target (missing, not executable,
// denied) and said so under that name before any target output. Docker's
// launcher exits 126/127; Seatbelt's runs under set -e and exits nonzero
// (1 on macOS) with a nil error, so the exit status alone looks like a test
// failure. A target forging this only forgoes its own repair feedback; it can
// never make a check pass.
func wrapperFailed(r execution.Result) bool {
	if r.Signal != "" || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "gemma-exec: ") {
		return false
	}
	return r.Environment == "docker-linux" && (r.ExitCode == 126 || r.ExitCode == 127) || strings.HasPrefix(r.Environment, "macOS Seatbelt") && r.ExitCode != 0
}
