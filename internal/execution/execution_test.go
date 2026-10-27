package execution

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApprovalFailClosed(t *testing.T) {
	if err := Require(context.Background(), nil, Approval{}); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := Require(ctx, func(context.Context, Approval) error { called = true; return nil }, Approval{})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled approval released")
	}
	ctx, cancel = context.WithCancel(context.Background())
	err = Require(ctx, func(context.Context, Approval) error { cancel(); return nil }, Approval{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("approval allowed after cancellation")
	}
}
func TestOutputBoundAndDrain(t *testing.T) {
	b := &BoundedBuffer{Limit: 100}
	n, err := b.Write([]byte(strings.Repeat("x", 10000)))
	if n != 10000 || err != nil {
		t.Fatal(n, err)
	}
	s, truncated := b.Snapshot()
	if len(s) != 100 || !truncated || b.Dropped != 9900 {
		t.Fatal("output bound failed")
	}
}
func TestEndpointRules(t *testing.T) {
	for _, u := range []string{"http://example.com", "http://localhost:1234", "file:///etc/passwd", "https://user:password@example.com", "https://example.com/#fragment"} {
		if _, _, err := EndpointClient(u); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	for _, ip := range []string{"10.0.0.1", "127.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "::1", "fd00::1", "64:ff9b::a00:1"} {
		if PublicIP(net.ParseIP(ip)) {
			t.Errorf("accepted %s", ip)
		}
	}
	if !PublicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public address denied")
	}
}
func TestEndpointRedirectAndBodyBound(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.Write([]byte("private")) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(strings.Repeat("x", 100)))
			return
		}
		http.Redirect(w, r, target.URL, 302)
	}))
	defer source.Close()
	c, _, err := EndpointClient(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Get(source.URL); err == nil || targetCalls != 0 {
		t.Fatalf("redirect escaped: %v %d", err, targetCalls)
	}
	BoundJSONResponses(c, 10)
	res, err := c.Get(source.URL + "/large")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if _, err = ReadBounded(res.Body, 1000); err == nil {
		t.Fatal("oversized body accepted")
	}
}
func TestSeatbeltQualification(t *testing.T) {
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("set GEMMA_TEST_SANDBOX=1 to qualify the real macOS sandbox")
	}
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	os.WriteFile(secret, []byte("private"), 0600)
	os.WriteFile(filepath.Join(root, "inside"), []byte("safe"), 0600)
	os.Mkdir(filepath.Join(root, ".git"), 0700)
	os.WriteFile(filepath.Join(root, ".git", "config"), []byte("private metadata"), 0600)
	os.Symlink(outside, filepath.Join(root, "escape"))
	runner := NewRunner()
	if err := runner.Probe(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{secret, filepath.Join(root, "escape", "secret"), filepath.Join(root, ".git", "config")} {
		res, err := runner.Run(context.Background(), Request{Workspace: root, Executable: "/bin/cat", Args: []string{p}})
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode == 0 || strings.Contains(res.Stdout, "private") {
			t.Fatalf("read escaped: %s %+v", p, res)
		}
	}
	res, err := runner.Run(context.Background(), Request{Workspace: root, Executable: "/bin/cat", Args: []string{"inside"}})
	if err != nil || res.Stdout != "safe" {
		t.Fatalf("inside read: %+v %v", res, err)
	}
	python := "/opt/homebrew/bin/python3"
	networkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("sandbox connected to the loopback test server") }))
	defer networkServer.Close()
	networkAddress := strings.TrimPrefix(networkServer.URL, "http://")
	_, networkPort, _ := net.SplitHostPort(networkAddress)
	for name, script := range map[string]string{"write": "open('written','w').write('yes')", "outside_write": "open(" + "'" + filepath.Join(outside, "bad") + "'" + ",'w').write('bad')", "fork": "import os;os.fork()", "network": "import socket;socket.create_connection(('127.0.0.1'," + networkPort + "),timeout=1)"} {
		res, err = runner.Run(context.Background(), Request{Workspace: root, Executable: python, Args: []string{"-c", script}})
		if err != nil {
			t.Fatal(err)
		}
		if name == "write" && res.ExitCode != 0 {
			t.Fatalf("workspace write: %+v", res)
		}
		if name != "write" && res.ExitCode == 0 {
			t.Fatalf("sandbox allowed %s: %+v", name, res)
		}
		if name != "write" && !strings.Contains(res.Stderr, "PermissionError") {
			t.Fatalf("%s did not prove policy denial: %+v", name, res)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "test_fixture.py"), []byte("import unittest\nclass Fixture(unittest.TestCase):\n def test_add(self):\n  self.assertEqual(1 + 2, 3)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	res, err = runner.Run(context.Background(), Request{Workspace: root, Executable: "/opt/homebrew/opt/python@3.14/bin/python3.14", Args: []string{"-m", "unittest", "-v"}})
	if err != nil || res.ExitCode != 0 || !strings.Contains(res.Stderr, "OK") {
		t.Fatalf("real unittest command %+v %v", res, err)
	}
	for _, req := range []Request{{Workspace: root, Executable: python, Args: []string{"-c", "open('read_only_violation','w').write('bad')"}, ReadOnly: true}, {Workspace: root, Executable: python, Args: []string{"-c", "open('.ENV','w').write('bad')"}}} {
		res, err = runner.Run(context.Background(), req)
		if err != nil || res.ExitCode == 0 || !strings.Contains(res.Stderr, "PermissionError") {
			t.Fatalf("write boundary: %+v %v", res, err)
		}
	}
	res, err = runner.Run(context.Background(), Request{Workspace: root, Executable: python, Args: []string{"-c", "print('x'*1000000)"}, MaxOutput: 1024})
	if err != nil || !res.Truncated || len(res.Stdout) > 512 {
		t.Fatalf("output %+v %v", res, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	res, err = runner.Run(ctx, Request{Workspace: root, Executable: "/bin/sleep", Args: []string{"30"}})
	if !errors.Is(err, context.DeadlineExceeded) || !res.Cancelled || time.Since(started) > 3*time.Second {
		t.Fatalf("cancellation %+v %v", res, err)
	}
}

func TestSandboxGitProtectedDiff(t *testing.T) {
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("real sandbox qualification is opt-in")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("/usr/bin/git", args...)
		cmd.Dir = root
		cmd.Env = []string{"PATH=" + SafePATH, "HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %s %v", out, err)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(root, ".env"), []byte("FIXTURE_SECRET=must-not-appear"), 0600)
	os.WriteFile(filepath.Join(root, "ordinary.txt"), []byte("visible change"), 0600)
	git("add", "--", ".env", "ordinary.txt")
	argv := []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "diff", "--cached", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--", "."}
	argv = append(argv, GitProtectedExclusions()...)
	r, err := NewRunner().Run(context.Background(), Request{Workspace: root, Executable: GitExecutable(), Args: argv, ReadOnly: true, AllowGitMetadata: true})
	if err != nil || r.ExitCode != 0 || !strings.Contains(r.Stdout, "visible change") || strings.Contains(r.Stdout, "FIXTURE_SECRET") {
		t.Fatalf("git diff %+v %v", r, err)
	}
}

func TestRealUserTerminal(t *testing.T) {
	if os.Getenv("GEMMA_TEST_TERMINAL") != "1" {
		t.Skip("set GEMMA_TEST_TERMINAL=1 for the user PTY test")
	}
	m := NewTerminalManager(nil)
	if _, err := m.Start(context.Background(), t.TempDir()); !errors.Is(err, ErrDenied) {
		t.Fatalf("terminal unapproved: %v", err)
	}
	m = NewTerminalManager(func(context.Context, Approval) error { return nil })
	defer m.Close()
	info, err := m.Start(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Resize(info.ID, 90, 25); err != nil {
		t.Fatal(err)
	}
	if err = m.Write(info.ID, "printf '\\147\\145\\155\\155\\141\\055\\164\\145\\162\\155\\151\\156\\141\\154\\055\\160\\162\\157\\157\\146\\n'; exit\n"); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	offset := int64(0)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, err := m.Read(info.ID, offset)
		if err != nil {
			t.Fatal(err)
		}
		offset = r.Offset
		output.WriteString(r.Data)
		if !r.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(output.String(), "gemma-terminal-proof") {
		t.Fatalf("no actual terminal output: %q", output.String())
	}
	if err = m.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
}
