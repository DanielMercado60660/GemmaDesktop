package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBoundedDiagnosticsRetainTail(t *testing.T) {
	b := &BoundedBuffer{Limit: 256}
	b.Write([]byte("test session started\n"))
	for i := 0; i < 1000; i++ {
		b.Write([]byte("verbose progress line\n"))
	}
	b.Write([]byte("AssertionError: row count mismatch\nFAILED (failures=1)\n"))
	out, truncated := b.Snapshot()
	if !truncated || len(out) > 256 || !strings.Contains(out, "test session started") || !strings.Contains(out, "AssertionError: row count mismatch") || !strings.Contains(out, "FAILED (failures=1)") {
		t.Fatalf("failure diagnostics lost: %q", out)
	}
	for _, limit := range []int{0, 1, 3, 19, 20, 31, 255} {
		b = &BoundedBuffer{Limit: limit}
		b.Write([]byte(strings.Repeat("你好", 100)))
		out, _ = b.Snapshot()
		if len(out) > limit || !utf8.ValidString(out) {
			t.Fatalf("invalid bounded UTF-8 limit=%d output=%q", limit, out)
		}
	}
}

func TestNestedPythonSQLiteWorkflow(t *testing.T) {
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("real sandbox qualification is opt-in")
	}
	root := t.TempDir()
	project := filepath.Join(root, "nested")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"store.py": `import sqlite3
def open_store():
    connection = sqlite3.connect('records.db')
    connection.execute('pragma journal_mode=wal')
    connection.execute('create table if not exists records (id integer primary key, label text not null)')
    return connection
`, "test_store.py": `import unittest
from store import open_store
class StoreTests(unittest.TestCase):
    def test_transaction(self):
        connection = open_store()
        connection.execute('delete from records')
        connection.execute('insert into records(label) values (?)', ('saved',))
        connection.commit()
        self.assertEqual(connection.execute('select label from records').fetchall(), [('saved',)])
        connection.close()
`, "pending.py": `import time
from pathlib import Path
from store import open_store
connection = open_store()
connection.execute('insert into records(label) values (?)', ('uncommitted',))
Path('transaction-ready').write_text('ready')
time.sleep(60)
connection.commit()
`}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runner := NewRunner()
	python := "/opt/homebrew/opt/python@3.14/bin/python3.14"
	request := Request{Workspace: root, WorkingDirectory: "nested", Executable: python, Args: []string{"-m", "unittest", "-v"}}
	r, err := runner.Run(context.Background(), request)
	if err != nil || r.ExitCode != 0 || !strings.Contains(r.Stderr, "OK") {
		t.Fatalf("SQLite unittest %+v %v", r, err)
	}
	canonicalProject, _ := filepath.EvalSymlinks(project)
	if r.WorkingDirectory != canonicalProject {
		t.Fatalf("reported directory %q", r.WorkingDirectory)
	}
	if _, err = os.Stat(filepath.Join(project, "records.db")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "records.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("database created outside requested directory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct {
		r   Result
		err error
	}, 1)
	go func() {
		r, e := runner.Run(ctx, Request{Workspace: root, WorkingDirectory: "nested", Executable: python, Args: []string{"pending.py"}})
		finished <- struct {
			r   Result
			err error
		}{r, e}
	}()
	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(project, "transaction-ready")); e == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		cancel()
		t.Fatal("transaction never started")
	}
	cancel()
	select {
	case done := <-finished:
		if !errors.Is(done.err, context.Canceled) || !done.r.Cancelled {
			t.Fatalf("cancel %+v %v", done.r, done.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled SQLite process stayed alive")
	}
	r, err = runner.Run(context.Background(), Request{Workspace: root, WorkingDirectory: "nested", Executable: python, Args: []string{"-c", "import sqlite3; c=sqlite3.connect('records.db'); print(c.execute('pragma integrity_check').fetchone()[0]); print(c.execute('select count(*) from records').fetchone()[0])"}})
	if err != nil || r.ExitCode != 0 || strings.TrimSpace(r.Stdout) != "ok\n1" {
		t.Fatalf("SQLite rollback after cancellation %+v %v", r, err)
	}
	r, err = runner.Run(context.Background(), Request{Workspace: root, WorkingDirectory: "nested", Executable: python, Args: []string{"-m", "venv", "--without-pip", ".venv"}})
	if err != nil || r.ExitCode != 0 {
		t.Fatalf("venv fixture %+v %v", r, err)
	}
	venv := filepath.Join(project, ".venv")
	r, err = runner.Run(context.Background(), Request{Workspace: root, WorkingDirectory: "nested", Executable: filepath.Join(venv, "bin", "python3"), Args: []string{"-c", "import sys; print(sys.prefix)"}})
	canonicalVenv, _ := filepath.EvalSymlinks(venv)
	if err != nil || r.ExitCode != 0 || strings.TrimSpace(r.Stdout) != canonicalVenv {
		t.Fatalf("virtualenv interpreter identity lost %+v %v", r, err)
	}
	for _, bad := range []string{"../outside", project, ".git"} {
		request.WorkingDirectory = bad
		if _, err = runner.Run(context.Background(), request); err == nil {
			t.Errorf("accepted working directory %q", bad)
		}
	}
}

func TestCancellationAfterProcessGroupChange(t *testing.T) {
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("real sandbox qualification is opt-in")
	}
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	script := fmt.Sprintf("import os,time\nfrom pathlib import Path\nos.setpgid(0,%d)\nos.setsid()\nPath('detached-ready').write_text('ready')\ntime.sleep(60)\n", syscall.Getpgrp())
	go func() {
		_, err := NewRunner().Run(ctx, Request{Workspace: root, Executable: "/opt/homebrew/opt/python@3.14/bin/python3.14", Args: []string{"-c", script}})
		done <- err
	}()
	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "detached-ready")); err == nil {
			ready = true
			break
		}
		select {
		case err := <-done:
			t.Fatalf("detachment fixture exited before cancellation: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		cancel()
		t.Fatal("detachment fixture did not become ready")
	}
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if time.Since(started) > time.Second {
			t.Fatal("cancellation waited for pipe timeout after process changed groups")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process survived cancellation after changing groups")
	}
}
