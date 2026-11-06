package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/execution"
)

func TestCommandMisuseRules(t *testing.T) {
	type command struct {
		name, executable string
		args             []string
		docker           bool
		err              []string // required substrings; nil means the argv must pass untouched
	}
	cases := []command{
		{"unittest verbose", "python3", []string{"-m", "unittest", "-v"}, false, nil},
		{"unittest discover", "python3", []string{"-m", "unittest", "discover", "-s", "tests", "-v"}, false, nil},
		{"grep alternation", "grep", []string{"a|b", "file.txt"}, false, nil},
		{"node test runner", "node", []string{"--test"}, false, nil},
		{"argument with a space", "python3", []string{"script.py", "my file.txt"}, false, nil},
		{"absolute python -c", "/opt/homebrew/bin/python3", []string{"-c", "print(1)"}, false, nil},
		{"python -c code with operators", "python3", []string{"-c", "import sys; print(1 if sys.argv else 2) or 3 > 2"}, false, nil},
		{"grep literal operator", "grep", []string{"-rn", "&&", "scripts"}, false, nil},
		{"git grep literal pipe", "git", []string{"grep", "-n", "|"}, false, nil},
		{"find exec terminator", "find", []string{".", "-name", "*.pyc", "-exec", "rm", "{}", ";"}, false, nil},
		{"expr comparison", "expr", []string{"2", ">", "1"}, false, nil},
		{"wrapped pattern tool", "timeout", []string{"10", "grep", "&&", "f.txt"}, false, nil},
		{"delimiter option value", "python3", []string{"split.py", "--sep", "|"}, false, nil},
		{"semicolon delimiter", "python3", []string{"csv_tool.py", "-d", ";"}, false, nil},
		{"positional after double dash", "python3", []string{"tool.py", "--", "&&"}, false, nil},
		{"go ldflags value", "go", []string{"build", "-ldflags", "-s -w", "./..."}, false, nil},
		{"go ldflags equals form", "go", []string{"build", "-ldflags=-X main.v=1 -s", "./..."}, false, nil},
		{"go run pattern", "go", []string{"test", "-run", "TestA|TestB", "./..."}, false, nil},
		{"negative calculator operand", "python3", []string{"calc.py", "-5 + 3"}, false, nil},
		{"python -c leading dash code", "python3", []string{"-c", "-x if False else print(1)"}, false, nil},
		{"pytest keyword expression", "pytest", []string{"-k", "test_a or test_b"}, false, nil},
		{"npm script args after double dash", "npm", []string{"test", "--", "--grep fast tests"}, false, nil},
		{"path with spaces", "/Applications/My App/tool", []string{"--version"}, false, nil},
		{"venv path with spaces", "/Users/me/my project/.venv/bin/python", []string{"-m", "pytest"}, false, nil},
		{"docker shell", "sh", []string{"-c", "python3 -m unittest -v && echo ok"}, true, nil},
		{"docker bash script", "bash", []string{"scripts/test.sh"}, true, nil},
		{"make target", "make", []string{"test"}, false, nil},

		{"packed executable", "python3 -m unittest", []string{"-v"}, false, []string{"executable must be one program", `{"executable":"python3","args":["-m","unittest","-v"]}`}},
		{"packed absolute executable", "/opt/homebrew/bin/python3 -m pytest", nil, false, []string{`{"executable":"/opt/homebrew/bin/python3","args":["-m","pytest"]}`}},
		{"packed executable in docker", "node --test", nil, true, []string{`{"executable":"node","args":["--test"]}`}},
		{"trailing space executable", "python3 ", []string{"x.py"}, false, []string{`{"executable":"python3","args":["x.py"]}`}},
		{"repeated executable", "python3", []string{"python3", "-m", "unittest"}, false, []string{"args must not repeat the executable", `{"executable":"python3","args":["-m","unittest"]}`}},
		{"repeated basename", "/opt/homebrew/bin/python3", []string{"python3", "x.py"}, false, []string{"args must not repeat the executable", `{"executable":"/opt/homebrew/bin/python3","args":["x.py"]}`}},
		{"repeated absolute executable", "/usr/bin/git", []string{"/usr/bin/git", "status"}, true, []string{"args must not repeat the executable"}},
		{"bash -c", "bash", []string{"-c", "python3 -m unittest -v"}, false, []string{"no shell is available", "child processes", `{"executable":"python3","args":["-m","unittest","-v"]}`}},
		{"sh script", "/bin/sh", []string{"run.sh"}, false, []string{"no shell is available", "run the program directly"}},
		{"zsh", "zsh", []string{"-c", "ls | wc -l"}, false, []string{"no shell is available"}},
		{"dash", "dash", nil, false, []string{"no shell is available"}},
		{"fish", "/opt/homebrew/bin/fish", nil, false, []string{"no shell is available"}},
		{"chained commands", "python3", []string{"-m", "unittest", "&&", "echo", "ok"}, false, []string{"does not use a shell", `"&&"`, "one program per call", `{"executable":"python3","args":["-m","unittest"]}`}},
		{"operator after option", "pytest", []string{"-q", "2>&1"}, false, []string{"does not use a shell", `"2>&1"`}},
		{"operators in docker", "go", []string{"test", "./...", "|", "tee", "log"}, true, []string{"does not use a shell", `"|"`}},
		{"packed flags", "python3", []string{"-m unittest -v"}, false, []string{"packs several arguments", `{"executable":"python3","args":["-m","unittest","-v"]}`}},
		{"packed flags later", "pytest", []string{"tests", "-x -q"}, false, []string{`"-x -q" packs several arguments`, `{"executable":"pytest","args":["tests","-x","-q"]}`}},
		{"packed long flags", "node", []string{"--test --watch"}, false, []string{"packs several arguments"}},
		{"packed flags versioned python", "python3.12", []string{"-m pytest"}, true, []string{"packs several arguments"}},
		{"cd", "cd", []string{"src"}, false, []string{"cd is not a program", `"workingDirectory":"src"`}},
		{"cd chain", "cd src && python3 -m unittest", nil, false, []string{`{"executable":"python3","args":["-m","unittest"],"workingDirectory":"src"}`}},
		{"cd chain in args", "cd", []string{"pkg", "&&", "go", "test", "./..."}, true, []string{`{"executable":"go","args":["test","./..."],"workingDirectory":"pkg"}`}},
		{"cd chain example stops at the next operator", "cd src && python3 x.py && echo ok", nil, false, []string{`{"executable":"python3","args":["x.py"],"workingDirectory":"src"}`}},
		{"pasted shell line", "python3 -m pytest -q | tail -5", nil, false, []string{"run one program per call", `{"executable":"python3","args":["-m","pytest","-q"]}`}},
	}
	for _, op := range []string{"|", "||", "&&", ";", ">", ">>", "<", "2>&1", "&"} {
		cases = append(cases, command{"operator " + op, "python3", []string{"tests/test_x.py", op, "out.txt"}, false, []string{"does not use a shell", `{"executable":"python3","args":["tests/test_x.py"]}`}})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := commandMisuse(c.executable, c.args, c.docker)
			if c.err == nil {
				if err != nil {
					t.Fatalf("legitimate argv rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("misuse accepted")
			}
			for _, want := range c.err {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q lacks %q", err, want)
				}
			}
		})
	}
}

func TestCommandMisuseIsRejectedBeforeApproval(t *testing.T) {
	x := &testExecutor{}
	approvals := 0
	m, err := New(t.TempDir(), Options{Executor: x, Approve: func(context.Context, Approval) error { approvals++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, err = m.Execute(t.Context(), "run_command", map[string]any{"executable": "python3", "args": []any{"python3", "-m", "unittest"}})
	if err == nil || !strings.Contains(err.Error(), "args must not repeat the executable") || approvals != 0 || x.called {
		t.Fatalf("misuse reached approval or execution: %v approvals=%d", err, approvals)
	}
	if _, err = m.Execute(t.Context(), "run_command", map[string]any{"executable": "python3", "args": []any{"-m", "unittest", "-v"}}); err != nil || approvals != 1 || !x.called {
		t.Fatalf("legitimate command blocked: %v", err)
	}
	if got := x.request.Args; len(got) != 3 || got[0] != "-m" || got[2] != "-v" || x.request.Executable != "python3" {
		t.Fatalf("argv changed: %q %q", x.request.Executable, got)
	}
}

func TestRunCommandDescriptionMatchesExecutor(t *testing.T) {
	var base string
	for _, d := range Catalogue() {
		if d.Name == "run_command" {
			base = d.Description
		}
	}
	docker, err := execution.NewDockerExecutor(execution.DockerConfig{StateDir: t.TempDir(), Image: "sha256:" + strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		executor execution.Executor
		want     string
	}{
		{"default", nil, base},
		{"macOS sandbox", execution.NewRunner(), base},
		{"other host executor", &testExecutor{}, base + " Execution: test isolated executor."},
		{"docker", docker, "Run an approved program using " + docker.Description() + "."},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, err := New(t.TempDir(), Options{Executor: c.executor})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			for _, d := range m.List() {
				if d.Name != "run_command" {
					continue
				}
				docker := c.name == "docker"
				if !strings.HasPrefix(d.Description, c.want) || docker != strings.Contains(d.Description, "Linux commands use the image PATH") || !docker && !strings.Contains(d.Description, "macOS workspace sandbox") {
					t.Fatalf("description %q", d.Description)
				}
				return
			}
			t.Fatal("run_command not listed")
		})
	}
}

func TestProtectedTaskCommandsRunReadOnly(t *testing.T) {
	contract := &execution.VerificationContract{Command: []string{"python3", "-m", "unittest"}, ProtectedPaths: []string{"tests/test_public.py"}}
	const note = "readOnly was set automatically because this task protects files"
	run := func(t *testing.T, executor execution.Executor, contract *execution.VerificationContract, args map[string]any, deny bool) (Approval, Result, error) {
		t.Helper()
		root := t.TempDir()
		var approved Approval
		m, err := New(root, Options{Executor: executor, Verification: contract, Approve: func(_ context.Context, a Approval) error {
			approved = a
			if deny {
				return execution.ErrDenied
			}
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer m.Close()
		r, err := m.Execute(t.Context(), "run_command", args)
		return approved, r, err
	}
	t.Run("set before approval on the host sandbox", func(t *testing.T) {
		x := &testExecutor{}
		a, r, err := run(t, x, contract, map[string]any{"executable": "python3", "args": []any{"-m", "unittest"}}, false)
		data, _ := r.Data.(map[string]any)
		if err != nil || a.Arguments["readOnly"] != true || !strings.Contains(a.Reason, "workspace is read-only") || !x.request.ReadOnly || data["readOnlyNote"] != note || !strings.Contains(r.Content, note) {
			t.Fatalf("approval=%+v request=%+v result=%s err=%v", a, x.request, r.Content, err)
		}
	})
	t.Run("guidance checks see the narrowed command", func(t *testing.T) {
		var seen any
		m, err := New(t.TempDir(), Options{Executor: &testExecutor{}, Verification: contract, Approve: func(context.Context, Approval) error { return nil }, BeforeAccess: func(_ context.Context, name string, args map[string]any) (Result, error) {
			seen = args["readOnly"]
			return Result{}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer m.Close()
		if _, err = m.Execute(t.Context(), "run_command", map[string]any{"executable": "python3"}); err != nil || seen != true {
			t.Fatalf("guard saw readOnly=%v: %v", seen, err)
		}
	})
	t.Run("explicit false is narrowed too", func(t *testing.T) {
		x := &testExecutor{}
		a, r, err := run(t, x, contract, map[string]any{"executable": "python3", "readOnly": false}, false)
		if err != nil || a.Arguments["readOnly"] != true || !x.request.ReadOnly || !strings.Contains(r.Content, note) {
			t.Fatalf("approval=%+v result=%s err=%v", a, r.Content, err)
		}
	})
	t.Run("requested readOnly has no note", func(t *testing.T) {
		x := &testExecutor{}
		_, r, err := run(t, x, contract, map[string]any{"executable": "python3", "readOnly": true}, false)
		if err != nil || !x.request.ReadOnly || strings.Contains(r.Content, "readOnlyNote") {
			t.Fatalf("result=%s err=%v", r.Content, err)
		}
	})
	t.Run("unprotected task keeps writes", func(t *testing.T) {
		x := &testExecutor{}
		a, r, err := run(t, x, &execution.VerificationContract{Command: []string{"python3"}}, map[string]any{"executable": "python3"}, false)
		if err != nil || a.Arguments["readOnly"] != false || x.request.ReadOnly || strings.Contains(r.Content, "readOnlyNote") {
			t.Fatalf("approval=%+v result=%s err=%v", a, r.Content, err)
		}
	})
	t.Run("docker publication keeps writes", func(t *testing.T) {
		docker, err := execution.NewDockerExecutor(execution.DockerConfig{StateDir: t.TempDir(), Image: "sha256:" + strings.Repeat("b", 64)})
		if err != nil {
			t.Fatal(err)
		}
		a, _, err := run(t, docker, contract, map[string]any{"executable": "python3"}, true)
		if !errors.Is(err, execution.ErrDenied) || a.Arguments["readOnly"] != false {
			t.Fatalf("approval=%+v err=%v", a, err)
		}
	})
	t.Run("replaces the sandbox's protected-path error", func(t *testing.T) {
		a, _, err := run(t, nil, contract, map[string]any{"executable": "/usr/bin/true"}, true)
		if !errors.Is(err, execution.ErrDenied) || a.Arguments["readOnly"] != true {
			t.Fatalf("approval=%+v err=%v", a, err)
		}
		// Prepare builds, but never starts, the sandboxed process.
		root := t.TempDir()
		for _, readOnly := range []bool{false, true} {
			cmd, cleanup, err := execution.NewRunner().Prepare(t.Context(), execution.Request{Workspace: root, Executable: "/usr/bin/true", ReadOnly: readOnly, ProtectedPaths: contract.ProtectedPaths})
			if cleanup != nil {
				cleanup()
			}
			protectedErr := err != nil && strings.Contains(err.Error(), "task-protected commands require readOnly:true")
			if protectedErr == readOnly || cmd != nil && cmd.Process != nil {
				t.Fatalf("readOnly=%v: %v", readOnly, err)
			}
		}
	})
}
