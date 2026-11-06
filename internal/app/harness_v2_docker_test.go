package app

import (
	"context"
	"encoding/json"
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

// Trusted checked-in oracle repairs exercise the production grader and actual
// Docker isolation. This opt-in test performs no model inference, optimization,
// candidate selection or qualification exposure in the user's data directory.
func TestHarnessV2LiveDockerReferences(t *testing.T) {
	image := os.Getenv("GEMMA_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("opt-in installed immutable toolchain; no inference")
	}
	if !strings.HasPrefix(image, "sha256:") || len(image) != 71 {
		t.Fatal("provide a full immutable Docker image identity")
	}
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "oracle.db"))
	if err != nil {
		t.Fatal(err)
	}
	packs, err := harness.LoadFixturePacks()
	if err != nil {
		t.Fatal(err)
	}
	m, err := harness.Open(harness.Config{Root: filepath.Join(root, "harness"), Store: db, Packs: packs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(); db.Close() })
	c := &Core{DataDir: root, Store: db, harness: m}
	runner, err := c.executorForConfig("docker", image)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("production v2 grader oracle image=%s; no LLM; private paths=%q", image, workspace.DefaultPrivatePaths)
	count := 0
	check := func(t *testing.T, pack harness.FixturePack, task harness.Task, variant string) {
		t.Helper()
		id := pack.ID + "-" + task.ID + "-" + variant
		trialRoot := filepath.Join(root, "harness", "trials", id, "workspace")
		if err := os.MkdirAll(trialRoot, 0700); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{}
		for name, body := range task.Public {
			files[name] = body
		}
		repair, err := harness.ReferenceRepairV2(pack.ID, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range repair {
			_, original := task.Public[name]
			if variant != "test-only" || !original {
				files[name] = body
			}
		}
		if variant == "protected" {
			files[task.ProtectedFiles[0]] += "\n# altered protected control\n"
		}
		for name, body := range files {
			dest := filepath.Join(trialRoot, name)
			if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(dest, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		result := harness.SolveResult{Content: "trusted fixture reference; not model-generated"}
		if task.Contract == "read_only" {
			result.Content = string(task.ExpectedAnswer)
		} else {
			toolset, err := tools.New(trialRoot, tools.Options{Mode: "auto", Executor: runner, Verification: &execution.VerificationContract{Command: task.Command, ProtectedPaths: task.ProtectedFiles, RequireRegressionTest: true}, Approve: func(context.Context, tools.Approval) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			verification, verifyErr := toolset.Execute(t.Context(), "verify_task", Doc{})
			toolset.Close()
			if variant == "reference" && verifyErr != nil {
				t.Fatal("reference host verification", verifyErr, verification)
			}
			call := harness.ToolObservation{Tool: "verify_task", Arguments: Doc{}, Data: obj(verification.Data), StartSequence: 3, Sequence: 4}
			if verifyErr != nil {
				call.Error = verifyErr.Error()
			}
			result.ToolCalls = []harness.ToolObservation{{Tool: "write_file", StartSequence: 1, Sequence: 2}, call}
		}
		trial := harness.Trial{ID: id, Status: "completed", TaskID: task.ID, Result: result}
		b, _ := json.Marshal(trial)
		record := Doc{}
		_ = json.Unmarshal(b, &record)
		if err = db.Put("harness_trial", record); err != nil {
			t.Fatal(err)
		}
		before, err := workspace.ScanCommandTree(t.Context(), trialRoot, "", workspace.DefaultPrivatePaths)
		if err != nil {
			t.Fatal(err)
		}
		grade, err := c.gradeHarnessRequest(t.Context(), harness.GradeRequest{Spec: harness.Spec{ExecutorIdentity: image, FixturePackID: pack.ID}, Task: task, Trial: trial})
		if err != nil {
			t.Fatal("production grader infrastructure", err)
		}
		if grade.Passed != (variant == "reference") {
			b, _ := json.MarshalIndent(grade, "", "  ")
			t.Fatalf("%s incorrect verdict: %s", variant, b)
		}
		if variant == "protected" {
			found := false
			for _, check := range grade.Checks {
				if check.Critical && !check.Passed {
					found = true
				}
			}
			if !found {
				t.Fatal("protected control lacked critical integrity failure", grade)
			}
		}
		after, err := workspace.ScanCommandTree(t.Context(), trialRoot, "", workspace.DefaultPrivatePaths)
		if err != nil || before.Digest != after.Digest {
			t.Fatal("grading changed immutable source", err)
		}
		t.Logf("%s: pass=%v checks=%d", variant, grade.Passed, len(grade.Checks))
	}
	for _, pack := range packs {
		for _, task := range pack.Tasks {
			count++
			t.Run(pack.ID+"/"+task.ID, func(t *testing.T) { check(t, pack, task, "reference") })
		}
	}
	if count != 18 {
		t.Fatal("oracle schedule changed", count)
	}
	for _, pack := range packs {
		if pack.ID != "coding-diagnostics-v2" {
			continue
		}
		for _, task := range pack.Tasks {
			if task.ID != "duration-units" {
				continue
			}
			for _, variant := range []string{"test-only", "protected"} {
				t.Run("negative/"+variant, func(t *testing.T) { check(t, pack, task, variant) })
			}
		}
	}
	cleanupRows, err := db.List("harness_cache_cleanup")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range cleanupRows {
		if row["status"] != "completed" {
			t.Fatal("oracle disposable cache cleanup incomplete", row)
		}
	}
}
