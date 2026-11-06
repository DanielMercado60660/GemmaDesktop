package app

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
)

func gradeReadOnlyHarness(task harness.Task, trial harness.Trial, root string) harness.Grade {
	g := harness.Grade{TrialID: trial.ID, GraderHash: task.GraderHash, Checks: []harness.Check{}}
	var actual, expected any
	err := json.Unmarshal([]byte(trial.Result.Content), &actual)
	expectedErr := json.Unmarshal(task.ExpectedAnswer, &expected)
	g.Checks = append(g.Checks, harness.Check{Name: "independent inspection answer", Passed: err == nil && expectedErr == nil && reflect.DeepEqual(actual, expected)})
	preserved := true
	for name, body := range task.Public {
		b, e := os.ReadFile(filepath.Join(root, name))
		if e != nil || string(b) != body {
			preserved = false
		}
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			preserved = false
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			preserved = false
			return nil
		}
		if _, ok := task.Public[filepath.ToSlash(rel)]; !ok {
			preserved = false
		}
		return nil
	})
	g.Checks = append(g.Checks, harness.Check{Name: "integrity: read-only task inputs unchanged", Passed: preserved, Critical: true})
	g.Passed = preserved && g.Checks[0].Passed && trial.Status == "completed" && !trial.Result.Incomplete
	return g
}

// New tests run alone on both repaired and original public source. Existing
// failing public tests cannot supply the red half of this regression evidence.
func (c *Core) gradeHarnessRegression(ctx context.Context, spec harness.Spec, task harness.Task, root, trialID string, runner execution.Executor) (checks []harness.Check, retErr error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if _, old := task.Public[rel]; old {
			return nil
		}
		for _, pattern := range task.RegressionTestGlobs {
			match, e := filepath.Match(pattern, filepath.Base(rel))
			if e != nil {
				return e
			}
			if match {
				files = append(files, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) == 0 || len(files) > 16 {
		return []harness.Check{{Name: "new regression tests discovered", Passed: false}}, nil
	}
	command, names, err := regressionOnlyCommand(task.Stack, root, files)
	if err != nil {
		return []harness.Check{{Name: "new regression tests discovered", Passed: false, Error: err.Error()}}, nil
	}
	green, e := runner.Run(ctx, execution.Request{Workspace: root, Executable: command[0], Args: command[1:], ReadOnly: true, Timeout: 2 * time.Minute, MaxOutput: 128 << 10})
	if e != nil && !candidateProcessFailure(ctx, green, e) {
		return nil, e
	}
	discovered := regressionDiscovery(task.Stack, green, names)
	checks = append(checks, harness.Check{Name: "new regression tests discovered", Passed: discovered})
	checks = append(checks, harness.Check{Name: "new regression tests pass on repair", Passed: discovered && e == nil && green.ExitCode == 0 && !green.Truncated && !green.Cancelled, Error: green.Stderr})
	broken, err := os.MkdirTemp(filepath.Join(c.DataDir, "harness"), "grading-regression-original-")
	if err != nil {
		return checks, err
	}
	cleanup, err := c.beginHarnessCache("grader", trialID, broken, spec.ExecutorIdentity)
	if err != nil {
		_ = os.RemoveAll(broken)
		return checks, err
	}
	defer func() {
		bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		retErr = errors.Join(retErr, c.finishHarnessCache(bounded, runner, cleanup))
	}()
	if err = copyHarnessTree(root, broken); err != nil {
		return checks, err
	}
	for rel, body := range task.Public {
		path := filepath.Join(broken, rel)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return checks, err
		}
		if err = os.WriteFile(path, []byte(body), 0600); err != nil {
			return checks, err
		}
	}
	red, e := runner.Run(ctx, execution.Request{Workspace: broken, Executable: command[0], Args: command[1:], ReadOnly: true, Timeout: 2 * time.Minute, MaxOutput: 128 << 10})
	if e != nil && !candidateProcessFailure(ctx, red, e) {
		return checks, e
	}
	originalPaths := pythonProductionPaths(task)
	if task.Stack == "node" || task.Stack == "javascript" {
		originalPaths = nodeProductionPaths(task)
	}
	failed := red.ExitCode > 0 && !red.Truncated && !red.Cancelled && regressionDiscovery(task.Stack, red, names) && assertionFailure(task.Stack, red, names, originalPaths...)
	checks = append(checks, harness.Check{Name: "new regression tests expose original defect", Passed: failed, Error: red.Stderr})
	return checks, nil
}

func regressionOnlyCommand(stack, root string, files []string) ([]string, []string, error) {
	switch stack {
	case "python":
		modules := []string{}
		for _, f := range files {
			m := strings.TrimSuffix(strings.ReplaceAll(f, "/", "."), ".py")
			if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`).MatchString(m) {
				return nil, nil, errors.New("regression test module name is invalid")
			}
			modules = append(modules, m)
		}
		return append([]string{"python3", "-m", "unittest", "-v"}, modules...), modules, nil
	case "node", "javascript":
		return append([]string{"node", "--test"}, files...), files, nil
	case "go":
		names := []string{}
		for _, f := range files {
			tree, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, f), nil, 0)
			if err != nil {
				return nil, nil, err
			}
			for _, decl := range tree.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !regexp.MustCompile(`^Test[A-Z0-9_]`).MatchString(fn.Name.Name) {
					continue
				}
				names = append(names, fn.Name.Name)
			}
		}
		if len(names) == 0 {
			return nil, nil, errors.New("no new Go test functions discovered")
		}
		return []string{"go", "test", "-json", "-count=1", "-run", "^(" + strings.Join(names, "|") + ")$", "./..."}, names, nil
	}
	return nil, nil, errors.New("unsupported regression discovery stack")
}

func regressionDiscovery(stack string, r execution.Result, names []string) bool {
	if r.Truncated || r.Cancelled {
		return false
	}
	out := r.Stdout + "\n" + r.Stderr
	switch stack {
	case "python":
		return regexp.MustCompile(`Ran [1-9][0-9]* tests?`).MatchString(out)
	case "node", "javascript":
		return regexp.MustCompile(`(?m)^# tests [1-9][0-9]*`).MatchString(out)
	case "go":
		for _, line := range strings.Split(r.Stdout, "\n") {
			var e struct{ Action, Test string }
			if json.Unmarshal([]byte(line), &e) == nil && e.Action == "run" && contains(names, e.Test) {
				return true
			}
		}
	}
	return false
}
func assertionFailure(stack string, r execution.Result, names []string, originalPaths ...string) bool {
	switch stack {
	case "python":
		return pythonRegressionFailure(r, names, originalPaths)
	case "node", "javascript":
		return nodeRegressionFailure(r, names, originalPaths)
	case "go":
		for _, line := range strings.Split(r.Stdout, "\n") {
			var e struct{ Action, Test string }
			if json.Unmarshal([]byte(line), &e) == nil && e.Action == "fail" && contains(names, e.Test) {
				return true
			}
		}
	}
	return false
}

// Only declared, mutable original production modules may supply exception-based
// red evidence. Candidate test files, protected inputs and new files cannot.
func pythonProductionPaths(task harness.Task) []string {
	paths := []string{}
	for name := range task.Public {
		if filepath.Ext(name) != ".py" || contains(task.ProtectedFiles, name) {
			continue
		}
		base := filepath.Base(name)
		test := base == "test.py" || base == "conftest.py" || strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
		for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(name)), "/") {
			test = test || part == "test" || part == "tests"
		}
		for _, pattern := range task.RegressionTestGlobs {
			match, _ := filepath.Match(pattern, base)
			test = test || match
		}
		if !test {
			paths = append(paths, filepath.ToSlash(name))
		}
	}
	sort.Strings(paths)
	return paths
}

var pythonFailureHeader = regexp.MustCompile(`(?m)^(FAIL|ERROR): ([A-Za-z_][A-Za-z0-9_]*) \(([^\r\n)]+)\)[^\r\n]*\r?$`)
var pythonAnyFailureHeader = regexp.MustCompile(`(?m)^(FAIL|ERROR):[^\r\n]*\r?$`)
var pythonTraceFrame = regexp.MustCompile(`(?m)^  File "([^"\r\n]+)", line [0-9]+, in ([^\r\n]+)\r?$`)
var pythonExceptionLine = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_.]*)(?::[^\r\n]*)?\r?$`)
var pythonRunSummary = regexp.MustCompile(`(?m)^Ran ([1-9][0-9]*) tests? in [0-9.]+s$`)
var pythonFailedSummary = regexp.MustCompile(`(?m)^FAILED \(([^\n)]+)\)$`)

func pythonFailureSummary(out string, headers [][]int) bool {
	runs := pythonRunSummary.FindAllStringSubmatchIndex(out, -1)
	summaries := pythonFailedSummary.FindAllStringSubmatchIndex(out, -1)
	if len(runs) == 0 || len(summaries) == 0 {
		return false
	}
	run, summary := runs[len(runs)-1], summaries[len(summaries)-1]
	if run[0] < headers[len(headers)-1][0] || run[1] > summary[0] || strings.TrimSpace(out[summary[1]:]) != "" {
		return false
	}
	counts := map[string]int{}
	for _, field := range strings.Split(out[summary[2]:summary[3]], ", ") {
		parts := strings.Split(field, "=")
		if len(parts) != 2 {
			return false
		}
		if _, duplicate := counts[parts[0]]; duplicate {
			return false
		}
		value, err := strconv.Atoi(parts[1])
		if err != nil || value < 0 {
			return false
		}
		switch parts[0] {
		case "failures", "errors", "skipped", "expected failures":
		default:
			return false
		}
		counts[parts[0]] = value
	}
	failures, errors := 0, 0
	for _, header := range headers {
		if out[header[2]:header[3]] == "FAIL" {
			failures++
		} else {
			errors++
		}
	}
	return failures == counts["failures"] && errors == counts["errors"]
}

func pythonFramePath(name string) string {
	name = strings.TrimPrefix(name, "/workspace/")
	if name == "" || strings.Contains(name, "\\") || filepath.IsAbs(name) || filepath.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
		return ""
	}
	return name
}

func pythonInfrastructureException(name string) bool {
	parts := strings.Split(name, ".")
	switch parts[len(parts)-1] {
	case "ImportError", "ModuleNotFoundError", "SyntaxError", "IndentationError", "TabError",
		"MemoryError", "RecursionError", "KeyboardInterrupt", "SystemExit", "GeneratorExit",
		"TimeoutError", "InterruptedError", "OSError", "IOError", "EnvironmentError",
		"FileNotFoundError", "PermissionError", "ConnectionError", "BrokenPipeError",
		"ConnectionAbortedError", "ConnectionRefusedError", "ConnectionResetError",
		"ChildProcessError", "ProcessLookupError", "NotADirectoryError", "IsADirectoryError", "BlockingIOError":
		return true
	}
	return false
}

// A unittest ERROR can demonstrate a real original implementation defect, but
// its spelling alone cannot distinguish that from import/setup/test mistakes.
// Require a discovered test-method frame and attribute the terminal exception
// to original production code. Every failing block must qualify, so a valid
// regression cannot mask an unrelated broken test or infrastructure error.
func pythonRegressionFailure(r execution.Result, modules, originalPaths []string) bool {
	if r.ExitCode <= 0 || r.Truncated || r.Cancelled {
		return false
	}
	out := strings.ReplaceAll(r.Stdout+"\n"+r.Stderr, "\r\n", "\n")
	headers := pythonFailureHeader.FindAllStringSubmatchIndex(out, -1)
	if len(headers) == 0 || len(headers) != len(pythonAnyFailureHeader.FindAllStringIndex(out, -1)) || !pythonFailureSummary(out, headers) {
		return false
	}
	for i, header := range headers {
		kind, method, identity := out[header[2]:header[3]], out[header[4]:header[5]], out[header[6]:header[7]]
		if !strings.HasPrefix(method, "test") && method != "runTest" {
			return false
		}
		module := ""
		for _, name := range modules {
			if len(name) > len(module) && strings.HasPrefix(identity, name+".") && strings.HasSuffix(identity, "."+method) && !strings.Contains(identity, "._FailedTest.") {
				module = name
			}
		}
		if module == "" {
			return false
		}
		end := len(out)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		block := out[header[1]:end]
		frames := pythonTraceFrame.FindAllStringSubmatchIndex(block, -1)
		traceback := strings.Index(block, "Traceback (most recent call last):\n")
		if len(frames) == 0 || traceback < 0 || traceback > frames[0][0] {
			return false
		}
		executed := false
		for _, frame := range frames {
			path, function := block[frame[2]:frame[3]], strings.TrimSpace(block[frame[4]:frame[5]])
			switch function {
			case "<module>", "setUp", "tearDown", "setUpClass", "tearDownClass", "setUpModule", "tearDownModule", "asyncSetUp", "asyncTearDown", "_callSetUp", "_callTearDown":
				return false
			}
			if strings.HasSuffix(path, "/unittest/loader.py") {
				return false
			}
			if pythonFramePath(path) == strings.ReplaceAll(module, ".", "/")+".py" && function == method {
				executed = true
			}
		}
		if !executed {
			return false
		}
		// Reject infrastructure/import errors even if subsequently wrapped by a
		// normal exception in candidate code.
		for _, exception := range pythonExceptionLine.FindAllStringSubmatch(block, -1) {
			if pythonInfrastructureException(exception[1]) {
				return false
			}
		}
		last := frames[len(frames)-1]
		exception := pythonExceptionLine.FindStringSubmatch(block[last[1]:])
		if exception == nil || exception[1] == "FAILED" || exception[1] == "OK" || exception[1] == "Traceback" {
			return false
		}
		if kind == "ERROR" && !contains(originalPaths, pythonFramePath(block[last[2]:last[3]])) {
			return false
		}
	}
	return true
}
