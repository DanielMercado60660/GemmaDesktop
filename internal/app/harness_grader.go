package app

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

func copyHarnessTree(source, dest string) error {
	var size int64
	count := 0
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in candidate output")
		}
		rel, _ := filepath.Rel(source, path)
		if d.IsDir() {
			if rel != "." && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dest, rel), 0700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("special file in candidate output")
		}
		size += info.Size()
		count++
		if size > 50<<20 || count > 2000 {
			return errors.New("candidate output exceeds grading bound")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, rel), b, 0600)
	})
}
func (c *Core) gradeHarnessTrial(ctx context.Context, task harness.Task, trial harness.Trial) (grade harness.Grade, retErr error) {
	grade = harness.Grade{TrialID: trial.ID, GraderHash: task.GraderHash, Checks: []harness.Check{}}
	if strings.Contains(trial.Error, "disposable Harness cache cleanup failed") {
		grade.Checks = append(grade.Checks, harness.Check{Name: "infrastructure: disposable cache released", Passed: false, Critical: true, Error: trial.Error})
		return grade, errors.New(trial.Error)
	}
	experiment, e := c.harness.Get("experiment", trial.ExperimentID)
	if e != nil {
		return grade, e
	}
	var x harness.Experiment
	b, _ := json.Marshal(experiment)
	if e = json.Unmarshal(b, &x); e != nil {
		return grade, e
	}
	var frozen FrozenAgent
	_ = json.Unmarshal(x.Spec.FrozenCapability, &frozen)
	runner, e := c.executorForConfig("docker", x.Spec.ExecutorIdentity, frozen.ExecutionPrivatePaths)
	if e != nil {
		return grade, e
	}
	root, e := c.harness.TrialRoot(trial.ID)
	if e != nil {
		return grade, e
	}
	if trial.Status != "completed" && trial.Status != "failed" && trial.Status != "interrupted" {
		return grade, errors.New("only a stopped immutable trial can be graded")
	}
	trialCleanup, e := c.beginHarnessCache("trial", trial.ID, root, x.Spec.ExecutorIdentity)
	if e != nil {
		return grade, e
	}
	defer c.finishHarnessGradeCache(ctx, runner, trialCleanup, &grade, &retErr)
	gradeRoot, e := os.MkdirTemp(filepath.Join(c.DataDir, "harness"), "grading-")
	if e != nil {
		return grade, e
	}
	cleanup, e := c.beginHarnessCache("grader", trial.ID, gradeRoot, x.Spec.ExecutorIdentity)
	if e != nil {
		_ = os.RemoveAll(gradeRoot)
		return grade, e
	}
	defer c.finishHarnessGradeCache(ctx, runner, cleanup, &grade, &retErr)
	if e = copyHarnessTree(root, gradeRoot); e != nil {
		return grade, e
	}
	// The trusted checker keeps expected values in the host controller and sends
	// only inputs to a separate isolated candidate process. No host imports occur.
	inputs := []json.RawMessage{}
	expected := []json.RawMessage{}
	for _, test := range task.Cases {
		inputs = append(inputs, test.Input)
		expected = append(expected, test.Expected)
	}
	payload, _ := json.Marshal(inputs)
	args := append(append([]string{}, task.GradeCommand[1:]...), string(payload))
	output, e := runner.Run(ctx, execution.Request{Workspace: gradeRoot, Executable: task.GradeCommand[0], Args: args, ReadOnly: true, Timeout: 2 * time.Minute, MaxOutput: 128 << 10})
	if e != nil && !candidateProcessFailure(ctx, output, e) {
		return grade, e
	}
	valid := e == nil && output.ExitCode == 0 && !output.Truncated && !output.Cancelled
	grade.Checks = append(grade.Checks, harness.Check{Name: "candidate CLI completed", Passed: valid, Critical: false, Error: output.Stderr})
	var actual []json.RawMessage
	if err := json.Unmarshal([]byte(output.Stdout), &actual); err != nil {
		valid = false
	}
	grade.Checks = append(grade.Checks, harness.Check{Name: "JSON result count", Passed: valid && len(actual) == len(expected)})
	for i, test := range task.Cases {
		passed := false
		if valid && i < len(actual) {
			var a, z any
			_ = json.Unmarshal(actual[i], &a)
			_ = json.Unmarshal(expected[i], &z)
			passed = reflect.DeepEqual(a, z)
		}
		grade.Checks = append(grade.Checks, harness.Check{Name: test.Name, Passed: passed, Critical: test.Critical})
	}
	protected := true
	for path, body := range task.Public {
		if contains(task.ProtectedFiles, path) || path == "README.md" || strings.Contains(path, "public_test") || strings.HasSuffix(path, "logic_test.go") {
			b, err := os.ReadFile(filepath.Join(gradeRoot, path))
			if err != nil || string(b) != body {
				protected = false
			}
		}
	}
	grade.Checks = append(grade.Checks, harness.Check{Name: "integrity: original public tests and requirements retained", Passed: protected, Critical: true})
	freshTest := false
	_ = filepath.WalkDir(gradeRoot, func(path string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(gradeRoot, path)
		_, original := task.Public[rel]
		name := strings.ToLower(d.Name())
		if !original && (strings.Contains(name, "test") || strings.Contains(name, "spec")) {
			b, _ := os.ReadFile(path)
			if len(b) > 40 {
				freshTest = true
			}
		}
		return nil
	})
	grade.Checks = append(grade.Checks, harness.Check{Name: "new regression test artifact present (meaningfulness requires independent review)", Passed: freshTest})
	grade.Checks = append(grade.Checks, harness.Check{Name: "prescribed verification after final edit", Passed: harnessVerificationAfterEdits(task.Command, trial.Result.ToolCalls)})
	verification, e := runner.Run(ctx, execution.Request{Workspace: gradeRoot, Executable: task.Command[0], Args: task.Command[1:], ReadOnly: true, Timeout: 2 * time.Minute, MaxOutput: 128 << 10})
	if e != nil && !candidateProcessFailure(ctx, verification, e) {
		return grade, e
	}
	grade.Checks = append(grade.Checks, harness.Check{Name: "independent public command", Passed: verification.ExitCode == 0 && !verification.Truncated && !verification.Cancelled, Error: verification.Stderr})
	if task.Probe != nil {
		for rel, body := range task.Probe.Files {
			if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
				return grade, errors.New("invalid trusted probe path")
			}
			path := filepath.Join(gradeRoot, rel)
			if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				return grade, e
			}
			if e = os.WriteFile(path, []byte(body), 0600); e != nil {
				return grade, e
			}
		}
		observed, err := runner.Run(ctx, execution.Request{Workspace: gradeRoot, Executable: task.Probe.Command[0], Args: task.Probe.Command[1:], ReadOnly: true, Timeout: 2 * time.Minute, MaxOutput: 128 << 10})
		if err != nil && !candidateProcessFailure(ctx, observed, err) {
			return grade, err
		}
		var actual, expected any
		parse := json.Unmarshal([]byte(observed.Stdout), &actual)
		_ = json.Unmarshal(task.Probe.Expected, &expected)
		grade.Checks = append(grade.Checks, harness.Check{Name: task.Probe.Name, Passed: err == nil && observed.ExitCode == 0 && !observed.Truncated && parse == nil && reflect.DeepEqual(actual, expected), Error: observed.Stderr})
	}
	grade.Passed = true
	for _, check := range grade.Checks {
		grade.Passed = grade.Passed && check.Passed
	}
	return grade, nil
}

func candidateProcessFailure(ctx context.Context, r execution.Result, e error) bool {
	return ctx.Err() == nil && (r.ExitCode > 0 || r.Cancelled || errors.Is(e, context.DeadlineExceeded))
}

// A failed mutating command can still have written files. It invalidates prior
// verification just like a successful edit; only a later exact read-only check
// establishes the required ordering. The latest prescribed failure also wins.
func harnessVerificationAfterEdits(prescribed []string, calls []harness.ToolObservation) bool {
	lastMutation, lastCommand := 0, 0
	for _, call := range calls {
		if call.Tool == "write_file" || call.Tool == "edit_file" || call.Tool == "move_file" || call.Tool == "delete_file" || call.Tool == "make_directory" || call.Tool == "run_command" && call.Arguments["readOnly"] != true {
			lastMutation = call.Sequence
		}
		if call.Tool == "run_command" {
			var argv []string
			b, _ := json.Marshal(call.Arguments["args"])
			_ = json.Unmarshal(b, &argv)
			command := append([]string{str(call.Arguments, "executable")}, argv...)
			if reflect.DeepEqual(command, prescribed) {
				lastCommand = 0
				if call.Error == "" && number(call.Data, "exitCode", -1) == 0 && call.Arguments["readOnly"] == true {
					lastCommand = call.StartSequence
				}
			}
		}
	}
	return lastCommand > lastMutation && lastCommand > 0
}

func (c *Core) finishHarnessGradeCache(ctx context.Context, runner execution.Executor, cleanup Doc, grade *harness.Grade, retErr *error) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := c.finishHarnessCache(cleanupCtx, runner, cleanup); err != nil {
		grade.Passed = false
		grade.Checks = append(grade.Checks, harness.Check{Name: "infrastructure: disposable cache released", Passed: false, Critical: true, Error: err.Error()})
		*retErr = errors.Join(*retErr, err)
	}
}
