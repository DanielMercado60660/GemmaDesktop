package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/tools"
	"gemmadesktop.local/app/internal/workspace"
)

type syntheticPythonFrame struct{ path, function string }

func syntheticPythonFailure(kind, method, identity, exception string, frames ...syntheticPythonFrame) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s (%s)\n----------------------------------------------------------------------\nTraceback (most recent call last):\n", kind, method, identity)
	for _, frame := range frames {
		fmt.Fprintf(&b, "  File %q, line 7, in %s\n    synthetic_call()\n", frame.path, frame.function)
	}
	summary := "errors=1"
	if kind == "FAIL" {
		summary = "failures=1"
	}
	fmt.Fprintf(&b, "%s: synthetic failure\n\nRan 1 test in 0.001s\n\nFAILED (%s)\n", exception, summary)
	return b.String()
}

func TestPythonRegressionFailureAttributesExecutedTestsToOriginalProduction(t *testing.T) {
	method, identity := "test_contract", "checks.test_repair.Contract.test_contract"
	entry := syntheticPythonFrame{"/workspace/checks/test_repair.py", method}
	production := syntheticPythonFrame{"/workspace/project/worker.py", "solve"}
	valid := syntheticPythonFailure("ERROR", method, identity, "ValueError", entry, production)
	assertion := syntheticPythonFailure("FAIL", method, identity, "AssertionError", entry)
	multiple := strings.Split(assertion, "\nRan ")[0] + strings.Split(valid, "\nRan ")[0] + "\nRan 2 tests in 0.001s\n\nFAILED (failures=1, errors=1)\n"
	modules, originals := []string{"checks.test_repair"}, []string{"project/worker.py"}
	tests := []struct {
		name, output string
		want         bool
	}{
		{"implementation_exception", valid, true},
		{"assertion_in_executed_test", assertion, true},
		{"multiple_valid_failures", multiple, true},
		{"crlf_traceback", strings.ReplaceAll(valid, "\n", "\r\n"), true},
		{"ordinary_test_method_without_underscore", strings.ReplaceAll(valid, "test_contract", "testContract"), true},
		{"default_runTest", strings.ReplaceAll(valid, "test_contract", "runTest"), true},
		{"unselected_module", strings.ReplaceAll(valid, "checks.test_repair", "checks.test_other"), false},
		{"loader_identity", strings.ReplaceAll(valid, identity, "unittest.loader._FailedTest.test_contract"), false},
		{"test_typo", syntheticPythonFailure("ERROR", method, identity, "NameError", entry), false},
		{"new_helper_exception", syntheticPythonFailure("ERROR", method, identity, "ValueError", entry, syntheticPythonFrame{"/workspace/new_helper.py", "helper"}), false},
		{"protected_cli_exception", syntheticPythonFailure("ERROR", method, identity, "ValueError", entry, syntheticPythonFrame{"/workspace/main.py", "main"}), false},
		{"foreign_source_exception", strings.ReplaceAll(valid, "/workspace/project/worker.py", "/other/project/worker.py"), false},
		{"traversal_frame", strings.ReplaceAll(valid, "/workspace/project/worker.py", "/workspace/../project/worker.py"), false},
		{"no_test_method_frame", syntheticPythonFailure("ERROR", method, identity, "ValueError", production), false},
		{"module_import_not_test_execution", syntheticPythonFailure("ERROR", method, identity, "ValueError", syntheticPythonFrame{entry.path, "<module>"}, production), false},
		{"body_imports_original_assertion", syntheticPythonFailure("FAIL", method, identity, "AssertionError", entry, syntheticPythonFrame{production.path, "<module>"}), false},
		{"missing_traceback", "ERROR: " + method + " (" + identity + ")\nValueError\nRan 1 test", false},
		{"missing_traceback_delimiter", strings.Replace(valid, "Traceback (most recent call last):\n", "", 1), false},
		{"missing_exception_line", strings.ReplaceAll(valid, "ValueError: synthetic failure", ""), false},
		{"missing_final_summary", strings.Replace(valid, "FAILED (errors=1)", "", 1), false},
		{"zero_tests", strings.Replace(valid, "Ran 1 test", "Ran 0 tests", 1), false},
		{"wrong_failure_count", strings.Replace(valid, "errors=1", "errors=2", 1), false},
		{"trailing_error_after_summary", valid + "extra error\n", false},
		{"mixed_valid_and_test_typo", valid + syntheticPythonFailure("ERROR", method, identity, "NameError", entry), false},
		{"mixed_unsupported_error_header", valid + "ERROR: setUpClass (checks.test_repair.Contract)\nRuntimeError\n", false},
		{"wrapped_import_failure", strings.Replace(valid, "ValueError: synthetic failure", "ModuleNotFoundError: synthetic missing dependency\n\nDuring handling of the above exception, another exception occurred:\n\nValueError: synthetic failure", 1), false},
	}
	for _, function := range []string{"setUp", "tearDown", "setUpClass", "tearDownClass", "setUpModule", "tearDownModule", "asyncSetUp", "asyncTearDown", "_callSetUp", "_callTearDown"} {
		tests = append(tests, struct {
			name, output string
			want         bool
		}{"lifecycle_" + function, syntheticPythonFailure("ERROR", method, identity, "ValueError", entry, syntheticPythonFrame{entry.path, function}, production), false})
	}
	for _, exception := range []string{"ImportError", "ModuleNotFoundError", "SyntaxError", "IndentationError", "TabError", "MemoryError", "RecursionError", "TimeoutError", "KeyboardInterrupt", "SystemExit", "OSError", "FileNotFoundError", "PermissionError", "ConnectionError"} {
		tests = append(tests, struct {
			name, output string
			want         bool
		}{"infrastructure_" + exception, syntheticPythonFailure("ERROR", method, identity, exception, entry, production), false})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := pythonRegressionFailure(execution.Result{ExitCode: 1, Stderr: test.output}, modules, originals)
			if got != test.want {
				t.Fatalf("classification=%v want=%v", got, test.want)
			}
		})
	}
	for _, result := range []execution.Result{{ExitCode: 0, Stderr: valid}, {ExitCode: 1, Stderr: valid, Truncated: true}, {ExitCode: 1, Stderr: valid, Cancelled: true}} {
		if pythonRegressionFailure(result, modules, originals) {
			t.Fatal("incomplete or successful command supplied red evidence")
		}
	}
	if pythonRegressionFailure(execution.Result{ExitCode: 1, Stderr: valid}, modules, nil) {
		t.Fatal("exception accepted without a declared original production path")
	}
}

func TestPythonProductionPathsExcludeProtectedTestsAndNewFiles(t *testing.T) {
	task := harness.Task{Public: map[string]string{"project/worker.py": "", "main.py": "", "public_test.py": "", "test_extra.py": "", "tests/helper.py": "", "conftest.py": "", "README.md": ""}, ProtectedFiles: []string{"main.py"}, RegressionTestGlobs: []string{"test_*.py"}}
	paths := pythonProductionPaths(task)
	if len(paths) != 1 || paths[0] != "project/worker.py" {
		t.Fatal("production attribution includes nonproduction paths", paths)
	}
}

// Synthetic public inputs only. These controls exercise the actual grader and
// Docker execution; they neither use an LLM nor inspect any real task assertions.
func TestHarnessRegressionClassificationLiveDockerSyntheticControls(t *testing.T) {
	image := os.Getenv("GEMMA_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("opt-in installed immutable Docker image; no inference")
	}
	if !strings.HasPrefix(image, "sha256:") || len(image) != 71 {
		t.Fatal("provide a full immutable Docker image identity")
	}
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "controls.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := harness.Open(harness.Config{Root: filepath.Join(root, "harness"), Store: db})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c := &Core{DataDir: root, Store: db, harness: m}
	runner, err := c.executorForConfig("docker", image)
	if err != nil {
		t.Fatal(err)
	}
	original := "def solve(x):\n    raise ValueError('synthetic implementation defect')\n"
	repair := "def solve(x):\n    return x['count'] * 3\n"
	regression := "import unittest\nfrom logic import solve\nclass RepairTests(unittest.TestCase):\n    def test_contract(self):\n        self.assertEqual(solve({'count': 2}), 6)\n"
	public := map[string]string{
		"logic.py":       original,
		"main.py":        "import json,sys\nfrom logic import solve\nprint(json.dumps([solve(x) for x in json.loads(sys.argv[1])]))\n",
		"public_test.py": "from logic import solve\nassert solve({'count': 2}) == 6\n",
		"README.md":      "Return the integer count multiplied by three. Preserve original public inputs; add and verify a regression.\n",
	}
	task := harness.Task{ID: "synthetic-regression-control", Stack: "python", Contract: "coding", Public: public, ProtectedFiles: []string{"main.py", "public_test.py", "README.md"}, Command: []string{"python3", "public_test.py"}, GradeCommand: []string{"python3", "main.py"}, RegressionTestGlobs: []string{"test_*.py"}, Cases: []harness.Case{{Name: "synthetic-zero", Input: json.RawMessage(`{"count":0}`), Expected: json.RawMessage(`0`)}, {Name: "synthetic-positive", Input: json.RawMessage(`{"count":2}`), Expected: json.RawMessage(`6`)}, {Name: "synthetic-negative", Input: json.RawMessage(`{"count":-1}`), Expected: json.RawMessage(`-3`)}}}
	task.GraderHash = harness.Hash(task)
	for _, variant := range []struct {
		name, implementation, test string
		want                       bool
	}{
		{"valid_repair", repair, regression, true},
		{"unrepaired", original, regression, false},
		{"incorrect_repair", "def solve(x):\n    return 0\n", regression, false},
		{"broken_test_import", repair, strings.ReplaceAll(regression, "from logic import solve", "from absent_module import solve"), false},
		{"vacuous_test", repair, "import unittest\nclass RepairTests(unittest.TestCase):\n    def test_contract(self):\n        self.assertTrue(True)\n", false},
		{"setup_only", repair, "import unittest\nfrom logic import solve\nclass RepairTests(unittest.TestCase):\n    def setUp(self):\n        solve({'count': 2})\n    def test_contract(self):\n        self.assertTrue(True)\n", false},
		{"teardown_only", repair, "import unittest\nfrom logic import solve\nclass RepairTests(unittest.TestCase):\n    def test_contract(self):\n        self.assertTrue(True)\n    def tearDown(self):\n        solve({'count': 2})\n", false},
	} {
		t.Run(variant.name, func(t *testing.T) {
			id := "synthetic-" + variant.name
			work := filepath.Join(root, "harness", "trials", id, "workspace")
			if err := os.MkdirAll(work, 0700); err != nil {
				t.Fatal(err)
			}
			for name, body := range public {
				if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for name, body := range map[string]string{"logic.py": variant.implementation, "test_repair.py": variant.test} {
				if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			toolset, err := tools.New(work, tools.Options{Mode: "auto", Executor: runner, Verification: &execution.VerificationContract{Command: task.Command, ProtectedPaths: task.ProtectedFiles, RequireRegressionTest: true}, Approve: func(context.Context, tools.Approval) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			verification, verifyErr := toolset.Execute(t.Context(), "verify_task", Doc{})
			toolset.Close()
			call := harness.ToolObservation{Tool: "verify_task", Data: obj(verification.Data), StartSequence: 3, Sequence: 4}
			if verifyErr != nil {
				call.Error = verifyErr.Error()
			}
			trial := harness.Trial{ID: id, Status: "completed", TaskID: task.ID, Result: harness.SolveResult{Content: "synthetic control; not model-generated", ToolCalls: []harness.ToolObservation{{Tool: "write_file", StartSequence: 1, Sequence: 2}, call}}}
			data, _ := json.Marshal(trial)
			row := Doc{}
			_ = json.Unmarshal(data, &row)
			if err := db.Put("harness_trial", row); err != nil {
				t.Fatal(err)
			}
			before, err := workspace.ScanCommandTree(t.Context(), work, "", workspace.DefaultPrivatePaths)
			if err != nil {
				t.Fatal(err)
			}
			grade, err := c.gradeHarnessRequest(t.Context(), harness.GradeRequest{Spec: harness.Spec{ExecutorIdentity: image}, Task: task, Trial: trial})
			if err != nil || grade.Passed != variant.want {
				t.Fatalf("synthetic verdict=%v want=%v infrastructure=%v", grade.Passed, variant.want, err)
			}
			after, err := workspace.ScanCommandTree(t.Context(), work, "", workspace.DefaultPrivatePaths)
			if err != nil || before.Digest != after.Digest {
				t.Fatal("synthetic grading changed source", err)
			}
		})
	}
}
