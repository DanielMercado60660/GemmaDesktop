package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
)

// Opt-in qualification uses the production solver and independent Docker grader.
// No candidate code is imported into this host process and no personal profile
// is opened. Failed model tasks stay failed in the immutable result record.
func TestPhase2LivePilot(t *testing.T) {
	if os.Getenv("GEMMA_PHASE2_LIVE") != "1" {
		t.Skip("opt-in live Gemma/Docker qualification")
	}
	data := os.Getenv("GEMMA_PHASE2_DATA")
	image := os.Getenv("GEMMA_PHASE2_IMAGE")
	if !filepath.IsAbs(data) || image == "" {
		t.Fatal("GEMMA_PHASE2_DATA must be a fresh absolute directory and GEMMA_PHASE2_IMAGE an installed immutable toolchain")
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("live qualification directory must not already exist")
	}
	c, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 65*time.Minute)
	defer cancel()
	if _, err = c.executionCall(ctx, "execution.configure", Doc{"image": image}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(data, "target")
	if err = os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	w, err := c.addWorkspace(Doc{"path": target, "name": "Phase 2 isolated qualification"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.saveCapability(Doc{"version": 1, "revision": 0, "name": "Pilot coding tools", "tools": []string{"list_files", "read_file", "search_files", "write_file", "edit_file", "make_directory", "run_command"}, "skillIds": []string{}, "mcp": []any{}, "delegateAgentIds": []string{}, "executionProfile": "docker", "projectGuidance": true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.saveAgent(Doc{"name": "26B coding qualification", "instructions": "Inspect the complete public project contract before editing. Repair the implementation, add meaningful regression tests, and execute the prescribed verification after the final change. Keep original tests and requirements intact. Use the exact Linux command names provided by the tool. Do not stop at a plan.", "model": "gemma4:26b-mlx", "maxSteps": 80, "mode": "approval", "thinking": "on", "maxOutputTokens": 16384, "temperature": 1.0, "toolProtocol": "native", "capabilityProfileId": obj(p)["id"]})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"python-ledger", "node-inventory", "go-inventory"}
	raw, err := c.startHarness(ctx, Doc{"workspaceId": obj(w)["id"], "agentId": obj(a)["id"], "mode": "native", "taskIds": ids, "contextSize": 65536, "budgetSeconds": 1200})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(raw)
	var experiment harness.Experiment
	if err = json.Unmarshal(b, &experiment); err != nil {
		t.Fatal(err)
	}
	tasks, err := harness.LoadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]harness.Task{}
	for _, task := range tasks {
		byID[task.ID] = task
	}
	report := Doc{"version": 1, "startedAt": store.Now(), "experimentId": experiment.ID, "conditions": experiment.Spec, "qualification": "Fresh solver runs; independent isolated checks; no candidate files repaired manually", "results": []Doc{}}
	records := []Doc{}
	write := func() {
		report["results"] = records
		blob, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(data, "pilot-report.json"), append(blob, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write()
	for _, id := range ids {
		t.Logf("PILOT_START %s", id)
		trial, e := c.harness.SolveTrial(ctx, experiment, id, "fresh-pilot", experiment.Spec.BaseRevisionID)
		record := Doc{"taskId": id, "trialId": trial.ID, "status": trial.Status, "outputHash": trial.OutputHash, "result": trial.Result}
		if e != nil {
			record["error"] = e.Error()
			records = append(records, record)
			write()
			t.Errorf("%s solver infrastructure: %v", id, e)
			continue
		}
		grade, e := c.gradeHarnessTrial(ctx, byID[id], trial)
		record["grade"] = grade
		if e != nil {
			record["graderError"] = e.Error()
		}
		records = append(records, record)
		write()
		t.Logf("PILOT_RESULT %s status=%s passed=%v error=%v", id, trial.Status, grade.Passed, e)
		if e != nil || !grade.Passed || trial.Status != "completed" {
			t.Errorf("%s did not qualify; retained trial %s", id, trial.ID)
		}
	}
	report["finishedAt"] = store.Now()
	write()
	t.Logf("PILOT_REPORT %s", filepath.Join(data, "pilot-report.json"))
}
