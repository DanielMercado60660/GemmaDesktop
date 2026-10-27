// Package execution owns bounded process execution and explicit user terminal sessions.
package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

var ErrUnavailable = errors.New("qualified macOS process sandbox is unavailable; model process execution is disabled")
var ErrDenied = errors.New("operation was not approved")

const DefaultOutputLimit = 128 << 10
const SafePATH = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

type Approval struct {
	Tool        string         `json:"tool"`
	Arguments   map[string]any `json:"arguments"`
	Reason      string         `json:"reason"`
	Workspace   string         `json:"workspace"`
	Environment string         `json:"environment"`
}
type Authorizer func(context.Context, Approval) error

func Require(ctx context.Context, fn Authorizer, a Approval) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fn == nil {
		return ErrDenied
	}
	if err := fn(ctx, a); err != nil {
		return err
	}
	return ctx.Err()
}

type Request struct {
	Workspace        string
	Executable       string
	Args             []string
	Timeout          time.Duration
	MaxOutput        int
	ReadOnly         bool
	AllowGitMetadata bool
}
type Result struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	ExitCode    int    `json:"exitCode"`
	Truncated   bool   `json:"truncated"`
	DurationMS  int64  `json:"durationMs"`
	Cancelled   bool   `json:"cancelled"`
	Environment string `json:"environment"`
	Signal      string `json:"signal,omitempty"`
}

// BoundedBuffer counts discarded bytes without blocking the subprocess's pipes.
type BoundedBuffer struct {
	mu      sync.Mutex
	data    []byte
	Limit   int
	Dropped int64
}

func (b *BoundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	keep := min(n, max(0, b.Limit-len(b.data)))
	b.data = append(b.data, p[:keep]...)
	b.Dropped += int64(n - keep)
	return n, nil
}
func (b *BoundedBuffer) Snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(string(b.data), "�"), b.Dropped > 0
}

type Runner struct{}

func NewRunner() *Runner { return &Runner{} }

func policyName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if c >= 'a' && c <= 'z' {
			fmt.Fprintf(&b, "[%c%c]", c, c-'a'+'A')
		} else if c == '.' {
			b.WriteString("[.]")
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}
func policyNames(names ...string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = policyName(n)
	}
	return strings.Join(parts, "|")
}

func sandboxProfile(readOnly, allowGit bool) string {
	writeRule := ""
	if !readOnly {
		writeRule = "(allow file-write* (subpath (param \"WORKSPACE\")))"
	}
	gitRule := "(deny file-read* (regex #\"(^|/)" + policyName(".git") + "(/|$)\"))"
	if allowGit {
		gitRule = ""
	}
	protected := policyNames(".ssh", ".aws", ".gnupg", ".codex", ".gemmadesktop", ".gemma-desktop") + "|" + policyName(".env") + "([.][^/]+)?"
	writeProtected := policyNames(".git", ".agents", "agents.md")
	return `(version 1)
(deny default)
(import "dyld-support.sb")
(allow syscall*)
(allow mach-bootstrap)
(allow process-exec)
(allow signal (target self))
(allow sysctl-read)
(allow mach-lookup (global-name "com.apple.system.logger") (global-name "com.apple.system.opendirectoryd") (global-name "com.apple.trustd.agent"))
(allow file-read-metadata)
(allow file-read* file-map-executable (subpath "/System") (subpath "/usr") (subpath "/bin") (subpath "/sbin") (subpath "/Library/Apple") (subpath "/Library/Developer") (subpath "/Applications/Xcode.app") (subpath "/opt/homebrew") (subpath (param "WORKSPACE")) (subpath (param "PRIVATE_TMP")) (literal (param "EXECUTABLE")) (literal "/dev/null") (literal "/dev/urandom") (literal "/dev/random"))
(allow file-write* (subpath (param "PRIVATE_TMP")) (literal "/dev/null"))
` + writeRule + `
(deny file-read* file-write* (regex #"(^|/)(` + protected + `)(/|$)"))
(deny file-write* (regex #"(^|/)(` + writeProtected + `)(/|$)"))
` + gitRule + `
(deny network*)
`
}

// Prepare creates an unstarted sandboxed command and its cleanup function.
// It never falls back to unrestricted host execution.
func (r *Runner) Prepare(ctx context.Context, req Request) (*exec.Cmd, func(), error) {
	if runtime.GOOS != "darwin" {
		return nil, nil, ErrUnavailable
	}
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		return nil, nil, ErrUnavailable
	}
	root, err := filepath.Abs(req.Workspace)
	if err != nil {
		return nil, nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, err
	}
	inf, err := os.Stat(root)
	if err != nil || !inf.IsDir() {
		return nil, nil, errors.New("workspace directory is required")
	}
	if req.Executable == "" || len(req.Args) > 256 {
		return nil, nil, errors.New("invalid process arguments")
	}
	for _, a := range req.Args {
		if strings.IndexByte(a, 0) >= 0 || len(a) > 256<<10 {
			return nil, nil, errors.New("invalid or oversized process argument")
		}
	}
	executable := req.Executable
	if !filepath.IsAbs(executable) {
		if strings.Contains(executable, "/") {
			return nil, nil, errors.New("executable must be an absolute path or PATH command")
		}
		var found string
		for _, dir := range filepath.SplitList(SafePATH) {
			p := filepath.Join(dir, executable)
			if st, e := os.Stat(p); e == nil && st.Mode().IsRegular() && st.Mode()&0111 != 0 {
				found = p
				break
			}
		}
		if found == "" {
			return nil, nil, fmt.Errorf("executable %q was not found on the controlled PATH", executable)
		}
		executable = found
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, nil, err
	}
	tmp, err := os.MkdirTemp("", "gemma-exec-")
	if err != nil {
		return nil, nil, err
	}
	tmp, _ = filepath.EvalSymlinks(tmp)
	cleanup := func() { _ = os.RemoveAll(tmp) }
	// This fixed launcher uses only shell builtins and then execs the reviewed
	// argv. The limits survive exec and do not require a fork-capable wrapper.
	args := []string{"-D", "WORKSPACE=" + root, "-D", "PRIVATE_TMP=" + tmp, "-D", "EXECUTABLE=" + executable, "-p", sandboxProfile(req.ReadOnly, req.AllowGitMetadata), "/bin/sh", "-c", `set -e; ulimit -c 0; ulimit -t 120; ulimit -f 65536; ulimit -n 128; exec "$@"`, "gemma-exec", executable}
	args = append(args, req.Args...)
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + SafePATH, "HOME=" + tmp, "TMPDIR=" + tmp, "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8", "CI=1", "TERM=dumb", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0", "GOCACHE=" + filepath.Join(tmp, "gocache"), "GOPATH=" + filepath.Join(tmp, "gopath"), "GOTOOLCHAIN=local", "npm_config_cache=" + filepath.Join(tmp, "npm"), "npm_config_update_notifier=false", "PYTHONDONTWRITEBYTECODE=1"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd, cleanup, nil
}

func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if req.Timeout <= 0 {
		req.Timeout = 2 * time.Minute
	}
	if req.Timeout > 10*time.Minute {
		req.Timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	cmd, cleanup, err := r.Prepare(ctx, req)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	limit := req.MaxOutput
	if limit <= 0 || limit > 1<<20 {
		limit = DefaultOutputLimit
	}
	out := &BoundedBuffer{Limit: limit / 2}
	stderr := &BoundedBuffer{Limit: limit - limit/2}
	cmd.Stdout = out
	cmd.Stderr = stderr
	start := time.Now()
	err = cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	stdout, ot := out.Snapshot()
	errout, et := stderr.Snapshot()
	res := Result{Stdout: stdout, Stderr: errout, Truncated: ot || et, DurationMS: time.Since(start).Milliseconds(), Environment: "macOS Seatbelt · single process · network denied"}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			res.Signal = status.Signal().String()
		}
	} else {
		res.ExitCode = -1
	}
	if ctx.Err() != nil {
		res.Cancelled = true
		return res, ctx.Err()
	}
	if res.Signal != "" {
		return res, fmt.Errorf("sandboxed process terminated by signal %s", res.Signal)
	}
	if strings.Contains(errout, "sandbox_init:") || strings.Contains(errout, "sandbox-exec:") {
		return res, fmt.Errorf("%w: %s", ErrUnavailable, strings.TrimSpace(errout))
	}
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return res, err
		}
	}
	return res, nil
}

// Probe verifies that the platform can activate the profile; policy behavior is
// additionally covered by integration tests. It does not claim a general security audit.
func (r *Runner) Probe(ctx context.Context, workspace string) error {
	res, err := r.Run(ctx, Request{Workspace: workspace, Executable: "/bin/echo", Args: []string{"gemma-sandbox-ready"}, ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "gemma-sandbox-ready") {
		return fmt.Errorf("%w: exit=%d stderr=%s stdout=%s", ErrUnavailable, res.ExitCode, res.Stderr, res.Stdout)
	}
	return nil
}

// LimitedReader caps inbound protocol bodies without silently accepting overflow.
func ReadBounded(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("response exceeded size limit")
	}
	return bytes.Clone(b), nil
}
