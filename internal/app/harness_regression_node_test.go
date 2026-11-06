package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/workspace"
)

func syntheticNodeFailure(code, name, failureType, location, stack string) string {
	return fmt.Sprintf("TAP version 13\n# Subtest: contract\nnot ok 1 - contract\n  ---\n  duration_ms: 0.2\n  type: 'test'\n  location: '%s:3:1'\n  failureType: '%s'\n  error: 'synthetic failure'\n  code: '%s'\n  name: '%s'\n  stack: |-\n%s  ...\n1..1\n# tests 1\n# suites 0\n# pass 0\n# fail 1\n# cancelled 0\n# skipped 0\n# todo 0\n", location, failureType, code, name, stack)
}

func TestNodeRegressionFailureAttributesExecutedTestsToOriginalProduction(t *testing.T) {
	testPath := "/workspace/checks/repair.test.cjs"
	stack := "    exports.solve (/workspace/project/worker.cjs:2:5)\n    TestContext.<anonymous> (" + testPath + ":3:35)\n    Test.run (node:internal/test_runner/test:1047:25)\n"
	valid := syntheticNodeFailure("ERR_TEST_FAILURE", "TypeError", "testCodeFailure", testPath, stack)
	assertion := syntheticNodeFailure("ERR_ASSERTION", "AssertionError", "testCodeFailure", testPath, "    TestContext.<anonymous> ("+testPath+":3:35)\n")
	selected, originals := []string{"checks/repair.test.cjs"}, []string{"project/worker.cjs"}
	tests := []struct {
		name, output string
		want         bool
	}{
		{"production_exception", valid, true},
		{"ordinary_assertion", assertion, true},
		{"async_exception", strings.Replace(valid, "TestContext.<anonymous>", "async TestContext.<anonymous>", 1), true},
		{"esm_file_url", strings.ReplaceAll(valid, "/workspace/", "file:///workspace/"), true},
		{"range_error", strings.Replace(valid, "TypeError", "RangeError", 1), true},
		{"native_iteration_after_production_origin", syntheticNodeFailure("ERR_TEST_FAILURE", "TypeError", "testCodeFailure", testPath, "    /workspace/project/worker.cjs:2:5\n    Array.map (<anonymous>)\n"+stack), true},
		{"native_iteration_cannot_supply_origin", syntheticNodeFailure("ERR_TEST_FAILURE", "TypeError", "testCodeFailure", testPath, "    Array.map (<anonymous>)\n"+stack), false},
		{"test_only_error", strings.Replace(valid, "/workspace/project/worker.cjs", testPath, 1), false},
		{"bare_test_callback_before_production_caller", syntheticNodeFailure("ERR_TEST_FAILURE", "TypeError", "testCodeFailure", testPath, "    "+testPath+":3:59\n"+stack), false},
		{"foreign_throw_before_production_caller", syntheticNodeFailure("ERR_TEST_FAILURE", "TypeError", "testCodeFailure", testPath, "    callback (/outside/foreign.cjs:3:9)\n"+stack), false},
		{"unrecognized_throw_before_production_caller", syntheticNodeFailure("ERR_TEST_FAILURE", "TypeError", "testCodeFailure", testPath, "    ambiguous native callback\n"+stack), false},
		{"no_executed_test_frame", strings.Replace(valid, "TestContext.<anonymous>", "helper", 1), false},
		{"wrong_selected_location", strings.Replace(valid, "location: '"+testPath, "location: '/workspace/other.test.cjs", 1), false},
		{"undeclared_production", strings.Replace(valid, "/workspace/project/worker.cjs", "/workspace/new_helper.cjs", 1), false},
		{"hook_failure", strings.Replace(valid, "testCodeFailure", "hookFailed", 1), false},
		{"unhandled_async", strings.Replace(valid, "testCodeFailure", "unhandledRejection", 1), false},
		{"syntax_error", strings.Replace(valid, "TypeError", "SyntaxError", 1), false},
		{"abort_error", strings.Replace(valid, "TypeError", "AbortError", 1), false},
		{"timeout_error", strings.Replace(valid, "TypeError", "TimeoutError", 1), false},
		{"missing_module_code", strings.Replace(valid, "ERR_TEST_FAILURE", "MODULE_NOT_FOUND", 1), false},
		{"filesystem_error_code", strings.Replace(valid, "ERR_TEST_FAILURE", "ENOENT", 1), false},
		{"runtime_import_stack", strings.Replace(valid, "    Test.run", "    Module.require (node:internal/modules/cjs/loader:1505:32)\n    Test.run", 1), false},
		{"wrapper_without_exception", strings.Replace(valid, "  name: 'TypeError'\n", "", 1), false},
		{"malformed_diagnostics", strings.Replace(valid, "  ...\n", "", 1), false},
		{"mismatched_assertion_name", strings.Replace(valid, "ERR_TEST_FAILURE", "ERR_ASSERTION", 1), false},
		{"zero_tests", strings.Replace(valid, "# tests 1", "# tests 0", 1), false},
		{"cancelled_summary", strings.Replace(valid, "# cancelled 0", "# cancelled 1", 1), false},
		{"ambiguous_url_host", strings.Replace(valid, "/workspace/project/worker.cjs", "file://host/workspace/project/worker.cjs", 1), false},
		{"escaped_url_separator", strings.Replace(valid, "/workspace/project/worker.cjs", "file:///workspace/project%2fworker.cjs", 1), false},
		{"traversal", strings.Replace(valid, "/workspace/project/worker.cjs", "/workspace/../project/worker.cjs", 1), false},
		{"mixed_invalid_failure", valid + strings.Replace(valid, "testCodeFailure", "hookFailed", 1), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nodeRegressionFailure(execution.Result{ExitCode: 1, Stdout: test.output}, selected, originals); got != test.want {
				t.Fatalf("classification=%v want=%v", got, test.want)
			}
		})
	}
	for _, result := range []execution.Result{{ExitCode: 0, Stdout: valid}, {ExitCode: 1, Stdout: valid, Truncated: true}, {ExitCode: 1, Stdout: valid, Cancelled: true}, {ExitCode: 1, Stderr: "ERR_ASSERTION"}} {
		if nodeRegressionFailure(result, selected, originals) {
			t.Fatal("invalid result supplied red evidence")
		}
	}
	if nodeRegressionFailure(execution.Result{ExitCode: 1, Stdout: valid}, selected, nil) {
		t.Fatal("exception accepted without declared original production")
	}
}

func TestNodeProductionPathsExcludeProtectedAndTestFiles(t *testing.T) {
	task := harness.Task{Public: map[string]string{"project/worker.cjs": "", "module.mjs": "", "main.js": "", "repair.test.js": "", "test_extra.js": "", "tests/helper.cjs": "", "__tests__/helper.mjs": "", "README.md": ""}, ProtectedFiles: []string{"main.js"}, RegressionTestGlobs: []string{"*.test.js"}}
	paths := nodeProductionPaths(task)
	if len(paths) != 2 || paths[0] != "module.mjs" || paths[1] != "project/worker.cjs" {
		t.Fatal("unexpected declared production paths", paths)
	}
}

func TestNodeHarnessRegressionClassificationLiveDockerSyntheticControls(t *testing.T) {
	image := os.Getenv("GEMMA_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("opt-in installed immutable Docker image; no inference")
	}
	if !strings.HasPrefix(image, "sha256:") || len(image) != 71 {
		t.Fatal("provide a full immutable Docker image identity")
	}
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "node-controls.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := &Core{DataDir: root, Store: db}
	runner, err := c.executorForConfig("docker", image)
	if err != nil {
		t.Fatal(err)
	}
	original := "exports.solve = x => { throw new TypeError('synthetic implementation defect'); };\n"
	repair := "exports.solve = x => x.count * 3;\n"
	imports := "const {test,beforeEach,afterEach}=require('node:test');\nconst assert=require('node:assert/strict');\nconst {solve}=require('./logic.cjs');\n"
	testBody := "test('contract',()=>assert.equal(solve({count:2}),6));\n"
	for _, variant := range []struct {
		name, original, implementation, test string
		want                                 bool
	}{
		{"valid_repair", original, repair, imports + testBody, true},
		{"production_array_callback", "exports.solve=x=>[x].map(v=>{throw new TypeError('synthetic implementation defect');})[0];\n", repair, imports + testBody, true},
		{"incorrect_repair", original, "exports.solve=x=>0;\n", imports + testBody, false},
		{"unrepaired", original, original, imports + testBody, false},
		{"broken_import", original, repair, strings.Replace(imports+testBody, "./logic.cjs", "./absent.cjs", 1), false},
		{"vacuous_test", original, repair, imports + "test('contract',()=>assert.equal(1,1));\n", false},
		{"before_hook", original, repair, imports + "beforeEach(()=>solve({count:2}));\ntest('contract',()=>assert.equal(1,1));\n", false},
		{"after_hook", original, repair, imports + "afterEach(()=>solve({count:2}));\ntest('contract',()=>assert.equal(1,1));\n", false},
		{"test_callback_origin", "exports.solve=(x,cb)=>cb();\n", repair, imports + "test('contract',()=>assert.equal(solve({count:2},()=>{throw new TypeError('synthetic test callback mistake');}),6));\n", false},
		{"runtime_missing_import", "exports.solve=x=>require('./absent.cjs');\n", repair, imports + testBody, false},
	} {
		t.Run(variant.name, func(t *testing.T) {
			id := "node-synthetic-" + variant.name
			work := filepath.Join(root, "harness", "trials", id, "workspace")
			if err := os.MkdirAll(work, 0700); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{"logic.cjs": variant.implementation, "repair.test.cjs": variant.test} {
				if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			task := harness.Task{ID: id, Stack: "node", Contract: "coding", Public: map[string]string{"logic.cjs": variant.original}, RegressionTestGlobs: []string{"*.test.cjs"}}
			before, err := workspace.ScanCommandTree(t.Context(), work, "", workspace.DefaultPrivatePaths)
			if err != nil {
				t.Fatal(err)
			}
			checks, err := c.gradeHarnessRegression(t.Context(), harness.Spec{ExecutorIdentity: image}, task, work, id, runner)
			passed := len(checks) == 3
			for _, check := range checks {
				passed = passed && check.Passed
			}
			if err != nil || passed != variant.want {
				t.Fatalf("synthetic verdict=%v want=%v infrastructure=%v checks=%+v", passed, variant.want, err, checks)
			}
			after, err := workspace.ScanCommandTree(t.Context(), work, "", workspace.DefaultPrivatePaths)
			if err != nil || before.Digest != after.Digest {
				t.Fatal("synthetic grading changed source", err)
			}
		})
	}
}
