package app

import (
	"context"
	"encoding/json"
	"fmt"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/workspace"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func harnessTestManager(t *testing.T, mode string, solve func(context.Context, harness.SolveRequest, func(harness.Event)) (harness.SolveResult, error)) (*Core, *harness.Manager, harness.Experiment) {
	t.Helper()
	root := t.TempDir()
	db, e := store.Open(filepath.Join(root, "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	c := &Core{DataDir: root, Store: db}
	tasks, e := harness.LoadFixtures()
	if e != nil {
		t.Fatal(e)
	}
	m, e := harness.Open(harness.Config{Root: filepath.Join(root, "harness"), Store: db, Tasks: tasks, Solve: solve, Grade: c.gradeHarnessTrial, Upstream: c.runHarnessUpstream})
	if e != nil {
		t.Fatal(e)
	}
	c.harness = m
	t.Cleanup(func() { m.Close(); db.Close() })
	image := os.Getenv("GEMMA_TEST_DOCKER_IMAGE")
	if image == "" {
		image = "sha256:test-only"
	}
	x, e := m.Start(harness.Spec{Mode: mode, WorkspaceID: "workspace", AgentID: "agent", TaskIDs: []string{"python-ledger"}, Model: "gemma-test", ModelDigest: "sha256:test", RuntimeVersion: "test", ExecutorIdentity: image, ApprovedSkills: map[string]json.RawMessage{}}, harness.Content{Instructions: "baseline"}, harness.Author{})
	if e != nil {
		t.Fatal(e)
	}
	return c, m, x
}
func TestHarnessUpstreamActualCoreStdioParity(t *testing.T) {
	calls := 0
	solveOrder := []string{}
	packetOrder := []string{}
	c, m, x := harnessTestManager(t, "upstream", func(_ context.Context, r harness.SolveRequest, _ func(harness.Event)) (harness.SolveResult, error) {
		calls++
		solveOrder = append(solveOrder, r.Revision.Content.Instructions)
		return harness.SolveResult{Content: "public artifact", Usage: harness.Doc{"eval_count": 10}, ToolCalls: []harness.ToolObservation{{Tool: "read_file", Content: "public source", Sequence: 1}}}, nil
	})
	if _, e := m.Advance(x.ID); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		rows, e := m.List("packet")
		if e != nil {
			t.Fatal(e)
		}
		for _, d := range rows {
			if d["status"] != "awaiting_codex" {
				continue
			}
			full, e := m.Get("packet", d["id"].(string))
			if e != nil {
				t.Fatal(e)
			}
			var p harness.Packet
			b, _ := json.Marshal(full)
			_ = json.Unmarshal(b, &p)
			packetOrder = append(packetOrder, p.Stage)
			response := harness.PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: harness.Author{Kind: "codex", Identity: "test-proposal-author"}, Content: &harness.Content{Instructions: "reviewed " + p.Stage}, Notes: "public evidence"}
			if p.Stage == "reflection" {
				response.Content = nil
				response.Verdict = "approved"
				response.AuthoredBy.Identity = "test-independent-reviewer"
				response.Evidence = p.Evidence
			}
			if _, e = m.Submit(response); e != nil {
				t.Fatal(e)
			}
		}
		if !m.Active() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if m.Active() {
		_ = m.Cancel(x.ID)
		t.Fatal("upstream worker did not complete")
	}
	d, e := m.Get("experiment", x.ID)
	if e != nil {
		t.Fatal(e)
	}
	if d["status"] != "completed" || number(d, "rollouts", 0) != 4 || number(d, "improvementStages", 0) != 3 || calls != 4 {
		t.Fatalf("actual upstream core parity failed: calls%d %+v", calls, d)
	}
	if !reflect.DeepEqual(solveOrder, []string{"baseline", "reviewed proposal_inv", "reviewed proposal_dom", "reviewed compose"}) {
		t.Fatal("upstream solver order differs from native", solveOrder)
	}
	if !reflect.DeepEqual(packetOrder, []string{"proposal_inv", "proposal_dom", "compose", "reflection"}) {
		t.Fatal("upstream packet order differs from native", packetOrder)
	}
	// Re-entering the published core with completed cached executions must
	// reconstruct accounting and verify output hashes without another solver call.
	b, _ := json.Marshal(d)
	var completed harness.Experiment
	_ = json.Unmarshal(b, &completed)
	if e = c.runHarnessUpstream(context.Background(), m, completed); e != nil {
		t.Fatal("cached upstream resume", e)
	}
	if calls != 4 {
		t.Fatal("cached upstream replayed solver", calls)
	}
}
func TestHarnessLiveDockerFixtureReferences(t *testing.T) {
	image := os.Getenv("GEMMA_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("opt-in installed immutable toolchain; no inference")
	}
	var runner execution.Executor
	c, m, x := harnessTestManager(t, "native", func(ctx context.Context, r harness.SolveRequest, _ func(harness.Event)) (harness.SolveResult, error) {
		repair, e := harness.ReferenceRepair(r.Task.ID)
		if e != nil {
			return harness.SolveResult{}, e
		}
		for rel, body := range repair {
			path := filepath.Join(r.Root, rel)
			if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				return harness.SolveResult{}, e
			}
			if e = os.WriteFile(path, []byte(body), 0600); e != nil {
				return harness.SolveResult{}, e
			}
		}
		// Reference qualification executes checked-in repairs, never model inference.
		out, e := runner.Run(ctx, execution.Request{Workspace: r.Root, Executable: r.Task.Command[0], Args: r.Task.Command[1:], ReadOnly: true, Timeout: 2 * time.Minute, MaxOutput: 128 << 10})
		if e != nil {
			return harness.SolveResult{}, e
		}
		if out.ExitCode != 0 {
			return harness.SolveResult{}, fmt.Errorf("reference command failed: %s %s", out.Stdout, out.Stderr)
		}
		return harness.SolveResult{Content: "fixture reference qualification (not model-generated)", Usage: harness.Doc{"eval_count": 1}, ToolCalls: []harness.ToolObservation{{Tool: "run_command", Arguments: harness.Doc{"executable": r.Task.Command[0], "args": r.Task.Command[1:], "readOnly": true}, Data: harness.Doc{"exitCode": out.ExitCode}, StartSequence: 1, Sequence: 2}}}, nil
	})
	var e error
	runner, e = c.executorForConfig("docker", image)
	if e != nil {
		t.Fatal(e)
	}
	// This fixture runner supplies no additional private paths; both command
	// executions use the Docker executor's default writable exclusions.
	t.Logf("read-only fixture verification provenance: image=%s privatePaths=%q readOnly=true", image, workspace.DefaultPrivatePaths)
	tasks, e := harness.LoadFixtures()
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			trial, e := m.SolveTrial(context.Background(), x, task.ID, "reference", x.Spec.BaseRevisionID)
			if e != nil {
				t.Fatal(e)
			}
			if trial.Status != "completed" {
				t.Fatal(trial.Error)
			}
			grade, e := c.gradeHarnessTrial(context.Background(), task, trial)
			if e != nil {
				t.Fatal(e)
			}
			if !grade.Passed {
				b, _ := json.MarshalIndent(grade, "", "  ")
				t.Fatal(string(b))
			}
			// Grading removes only disposable caches; the immutable solver output
			// must still pass the normal cached-trial output-hash verification.
			cached, e := m.SolveTrial(context.Background(), x, task.ID, "reference", x.Spec.BaseRevisionID)
			if e != nil || cached.OutputHash != trial.OutputHash {
				t.Fatal("cleanup changed immutable final source", e)
			}
			cleanupRows, e := c.Store.List("harness_cache_cleanup")
			if e != nil {
				t.Fatal(e)
			}
			completed := 0
			for _, row := range cleanupRows {
				if row["trialId"] != trial.ID {
					continue
				}
				if row["status"] != "completed" {
					t.Fatal("fixture cache cleanup not complete", row)
				}
				completed++
			}
			if completed != 2 {
				t.Fatal("expected solver and grader cleanup records", completed)
			}
			cacheEntries, e := os.ReadDir(filepath.Join(c.DataDir, "execution", "cache"))
			if e != nil || len(cacheEntries) != 0 {
				t.Fatal("disposable fixture cache retained", len(cacheEntries), e)
			}
			t.Log("reference + isolated independent grader passed", strings.Join(task.Command, " "))
		})
	}
}

func TestHarnessUpstreamFailedFinalRetainsCompletedStageAccounting(t *testing.T) {
	calls := 0
	_, m, x := harnessTestManager(t, "upstream", func(_ context.Context, r harness.SolveRequest, _ func(harness.Event)) (harness.SolveResult, error) {
		calls++
		if r.Revision.Content.Instructions == "reviewed compose" {
			return harness.SolveResult{Incomplete: true, Usage: harness.Doc{"eval_count": 16384}}, fmt.Errorf("bounded output limit")
		}
		return harness.SolveResult{Content: "observed public result", Usage: harness.Doc{"eval_count": 10}}, nil
	})
	if _, e := m.Advance(x.ID); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		rows, e := m.List("packet")
		if e != nil {
			t.Fatal(e)
		}
		for _, d := range rows {
			if d["status"] != "awaiting_codex" {
				continue
			}
			full, e := m.Get("packet", str(d, "id"))
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(full)
			var p harness.Packet
			_ = json.Unmarshal(raw, &p)
			response := harness.PacketResponse{ID: p.ID, RequestHash: p.RequestHash, AuthoredBy: harness.Author{Kind: "codex", Identity: "test-proposal-author"}, Content: &harness.Content{Instructions: "reviewed " + p.Stage}, Notes: "Inspect public trace; retain failure"}
			if p.Stage == "reflection" {
				response.Content = nil
				response.Verdict = "approved"
				response.AuthoredBy.Identity = "test-independent-reviewer"
				response.Evidence = p.Evidence
			}
			if _, e = m.Submit(response); e != nil {
				t.Fatal(e)
			}
		}
		if !m.Active() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if m.Active() {
		_ = m.Cancel(x.ID)
		t.Fatal("upstream failure fixture did not complete")
	}
	d, e := m.Get("experiment", x.ID)
	if e != nil {
		t.Fatal(e)
	}
	if d["status"] != "failed" || number(d, "rollouts", 0) != 4 || number(d, "improvementStages", 0) != 3 || calls != 4 {
		t.Fatalf("failed final changed accounting or outcome: calls=%d %+v", calls, d)
	}
	rows, e := m.List("trial")
	if e != nil {
		t.Fatal(e)
	}
	failed := 0
	for _, row := range rows {
		if row["status"] == "failed" {
			failed++
		}
	}
	if len(rows) != 4 || failed != 1 {
		t.Fatal("failed denominator was removed", rows)
	}
}
