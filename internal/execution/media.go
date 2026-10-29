package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type MediaRequest struct {
	Program   string
	Input     string
	OutputDir string
	Args      []string
	Timeout   time.Duration
	MaxOutput int
}

// RunMedia is a host-owned media worker, not an agent command capability. Its
// input is read-only and its sole writable data scope is a private output dir.
// The fixed launcher execs one process; fork and network remain denied.
func RunMedia(ctx context.Context, req MediaRequest) (Result, error) {
	if runtime.GOOS != "darwin" {
		return Result{}, ErrUnavailable
	}
	if req.Program != "ffmpeg" && req.Program != "ffprobe" {
		return Result{}, errors.New("unsupported media executable")
	}
	executable, err := filepath.EvalSymlinks(filepath.Join("/opt/homebrew/bin", req.Program))
	if err != nil {
		return Result{}, err
	}
	input, err := filepath.Abs(req.Input)
	if err != nil {
		return Result{}, err
	}
	st, err := os.Lstat(input)
	if err != nil || !st.Mode().IsRegular() {
		return Result{}, errors.New("media input must be a regular private file")
	}
	output, err := filepath.EvalSymlinks(req.OutputDir)
	if err != nil {
		return Result{}, err
	}
	if output == "/" || output == filepath.Dir(input) {
		return Result{}, errors.New("media outputs must be separate from the immutable input")
	}
	if req.Timeout <= 0 || req.Timeout > 2*time.Minute {
		req.Timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	tmp, err := os.MkdirTemp("", "gemma-media-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tmp)
	tmp, _ = filepath.EvalSymlinks(tmp)
	profile := sandboxProfile(true, false) + "\n(allow file-read* (literal (param \"MEDIA_INPUT\")))\n(allow file-write* (subpath (param \"MEDIA_OUTPUT\")))\n"
	args := []string{"-D", "WORKSPACE=" + output, "-D", "PRIVATE_TMP=" + tmp, "-D", "EXECUTABLE=" + executable, "-D", "MEDIA_INPUT=" + input, "-D", "MEDIA_OUTPUT=" + output, "-p", profile, "/bin/sh", "-c", `set -e; ulimit -c 0; ulimit -t 120; ulimit -f 2048; ulimit -n 128; exec "$@"`, "gemma-media", executable}
	args = append(args, req.Args...)
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	cmd.Dir = output
	cmd.Env = []string{"PATH=" + SafePATH, "HOME=" + tmp, "TMPDIR=" + tmp, "LANG=C", "LC_ALL=C"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return cmd.Process.Kill()
	}
	limit := req.MaxOutput
	if limit < 1 || limit > 2<<20 {
		limit = 1 << 20
	}
	stdout := &BoundedBuffer{Limit: limit}
	stderr := &BoundedBuffer{Limit: 128 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	start := time.Now()
	err = cmd.Run()
	out, ot := stdout.Snapshot()
	errs, et := stderr.Snapshot()
	res := Result{Stdout: out, Stderr: errs, Truncated: ot || et, DurationMS: time.Since(start).Milliseconds(), ExitCode: -1, Environment: "macOS Seatbelt · fixed media process · network/fork denied", ReadOnly: true}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		res.Cancelled = true
		return res, ctx.Err()
	}
	if strings.Contains(errs, "sandbox_init:") || strings.Contains(errs, "sandbox-exec:") {
		return res, fmt.Errorf("%w: %s", ErrUnavailable, errs)
	}
	if err != nil {
		return res, fmt.Errorf("%s failed: %w: %s", req.Program, err, errs)
	}
	if res.ExitCode != 0 || res.Truncated {
		return res, fmt.Errorf("%s output incomplete: exit=%d truncated=%v", req.Program, res.ExitCode, res.Truncated)
	}
	return res, nil
}
