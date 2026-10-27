package execution

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
)

const TerminalRetention = 512 << 10

type TerminalInfo struct {
	ID          string `json:"id"`
	Workspace   string `json:"workspace"`
	Running     bool   `json:"running"`
	ExitCode    int    `json:"exitCode"`
	Environment string `json:"environment"`
}
type TerminalOutput struct {
	ID        string `json:"id"`
	Data      string `json:"data"`
	Offset    int64  `json:"offset"`
	Truncated bool   `json:"truncated"`
	Running   bool   `json:"running"`
	ExitCode  int    `json:"exitCode"`
}
type terminalSession struct {
	mu     sync.Mutex
	info   TerminalInfo
	file   *os.File
	cmd    *exec.Cmd
	output []byte
	base   int64
	done   chan struct{}
}
type TerminalManager struct {
	mu        sync.Mutex
	sessions  map[string]*terminalSession
	authorize Authorizer
	closed    bool
}

func NewTerminalManager(authorize Authorizer) *TerminalManager {
	return &TerminalManager{sessions: map[string]*terminalSession{}, authorize: authorize}
}

func (m *TerminalManager) Start(ctx context.Context, workspace string) (TerminalInfo, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return TerminalInfo{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return TerminalInfo{}, err
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return TerminalInfo{}, errors.New("workspace directory required")
	}
	if err = Require(ctx, m.authorize, Approval{Tool: "terminal.start", Arguments: map[string]any{"workspace": abs}, Workspace: abs, Environment: "user host terminal · unrestricted user account", Reason: "Open an interactive host shell. Commands typed here can access your account's files and network; agents cannot write to this terminal."}); err != nil {
		return TerminalInfo{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return TerminalInfo{}, errors.New("terminal manager is closed")
	}
	active := 0
	for _, s := range m.sessions {
		s.mu.Lock()
		if s.info.Running {
			active++
		}
		s.mu.Unlock()
	}
	if active >= 4 {
		return TerminalInfo{}, errors.New("at most four interactive terminals may run")
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return TerminalInfo{}, err
	}
	id := hex.EncodeToString(token[:])
	cmd := exec.Command("/bin/zsh", "-f")
	cmd.Dir = abs
	home, _ := os.UserHomeDir()
	cmd.Env = []string{"HOME=" + home, "PATH=" + SafePATH, "SHELL=/bin/zsh", "TERM=xterm-256color", "LANG=en_US.UTF-8", "COLORTERM=truecolor"}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		return TerminalInfo{}, err
	}
	s := &terminalSession{info: TerminalInfo{ID: id, Workspace: abs, Running: true, ExitCode: -1, Environment: "user host terminal · agents have no access"}, file: f, cmd: cmd, done: make(chan struct{})}
	m.sessions[id] = s
	initial := s.info
	go func() {
		buf := make([]byte, 8192)
		for {
			n, e := f.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.output = append(s.output, buf[:n]...)
				if len(s.output) > TerminalRetention {
					drop := len(s.output) - TerminalRetention
					for drop < len(s.output) && !utf8.RuneStart(s.output[drop]) {
						drop++
					}
					s.output = s.output[drop:]
					s.base += int64(drop)
				}
				s.mu.Unlock()
			}
			if e != nil {
				break
			}
		}
	}()
	go func() {
		_ = cmd.Wait()
		s.mu.Lock()
		s.info.Running = false
		s.info.ExitCode = cmd.ProcessState.ExitCode()
		s.mu.Unlock()
		_ = f.Close()
		close(s.done)
	}()
	return initial, nil
}
func (m *TerminalManager) session(id string) (*terminalSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("terminal not found")
	}
	return s, nil
}
func (m *TerminalManager) Write(id, data string) error {
	s, err := m.session(id)
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return errors.New("terminal input exceeds limit")
	}
	s.mu.Lock()
	running := s.info.Running
	s.mu.Unlock()
	if !running {
		return errors.New("terminal has exited")
	}
	_, err = io.WriteString(s.file, data)
	return err
}
func (m *TerminalManager) Resize(id string, cols, rows uint16) error {
	s, err := m.session(id)
	if err != nil {
		return err
	}
	if cols < 10 || cols > 500 || rows < 2 || rows > 300 {
		return errors.New("invalid terminal dimensions")
	}
	return pty.Setsize(s.file, &pty.Winsize{Cols: cols, Rows: rows})
}
func (m *TerminalManager) Read(id string, offset int64) (TerminalOutput, error) {
	s, err := m.session(id)
	if err != nil {
		return TerminalOutput{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	truncated := offset < s.base
	if offset < s.base {
		offset = s.base
	}
	if offset > s.base+int64(len(s.output)) {
		return TerminalOutput{}, errors.New("terminal offset is ahead of output")
	}
	start := int(offset - s.base)
	end := min(len(s.output), start+(64<<10))
	if end > start && s.info.Running {
		last := end - 1
		for last > start && !utf8.RuneStart(s.output[last]) {
			last--
		}
		if !utf8.FullRune(s.output[last:end]) {
			end = last
		}
	}
	data := strings.ToValidUTF8(string(s.output[start:end]), "�")
	return TerminalOutput{ID: id, Data: data, Offset: s.base + int64(end), Truncated: truncated, Running: s.info.Running, ExitCode: s.info.ExitCode}, nil
}
func (m *TerminalManager) Stop(id string) error {
	s, err := m.session(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	running := s.info.Running
	s.mu.Unlock()
	if !running {
		return nil
	}
	if err = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	select {
	case <-s.done:
		return nil
	case <-time.After(500 * time.Millisecond):
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	}
	select {
	case <-s.done:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("terminal %s did not exit after cancellation", id)
	}
}
func (m *TerminalManager) Close() error {
	m.mu.Lock()
	m.closed = true
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if e := m.Stop(id); e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}
