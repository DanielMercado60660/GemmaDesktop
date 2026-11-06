package app

import (
	"context"
	"encoding/json"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Only solver/grader effects are substituted. Frozen profiles, the full paired
// schedule, relative gate, independent review and SQLite mutations are real.
func qualifiedProfileFixtureV2(t *testing.T) (*Core, harness.RunV2, harness.ArmV2, Doc) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "profile.db"))
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{DataDir: root, Store: db}
	profile := Doc{"id": "coding", "version": 1, "revision": 1, "name": "Coding", "tools": []string{"read_file", "write_file", "run_command"}, "skillIds": []string{}, "mcp": []any{}, "delegateAgentIds": []string{}, "executionProfile": "docker"}
	agent := Doc{"id": "agent", "name": "Personal name", "instructions": "personal instructions", "model": "baseline", "runtimeId": "ollama-default", "capabilityProfileId": "coding", "thinking": "off", "temperature": 0.2, "maxOutputTokens": 4096, "maxSteps": 24, "toolProtocol": "native", "unrelatedPreference": "retain"}
	for kind, doc := range map[string]Doc{"workspace": {"id": "workspace", "path": root}, "agent": agent, "capability": profile, "settings": {"id": "default", "dockerImage": "sha256:fixture", "contextSize": 32768}} {
		if err = db.Put(kind, doc); err != nil {
			t.Fatal(err)
		}
	}
	frozen, err := c.FreezeAgent(agent)
	if err != nil {
		t.Fatal(err)
	}
	capability, _ := json.Marshal(frozen)
	packs, err := harness.LoadFixturePacks()
	if err != nil {
		t.Fatal(err)
	}
	m, err := harness.Open(harness.Config{Root: filepath.Join(root, "harness"), Store: db, Packs: packs, Solve: func(context.Context, harness.SolveRequest, func(harness.Event)) (harness.SolveResult, error) {
		time.Sleep(2 * time.Millisecond)
		return harness.SolveResult{Content: "synthetic complete response", Usage: Doc{"prompt_eval_count": 10, "eval_count": 10}, TokenAccounting: inference.NewTokenAccounting(1, 1, 1)}, nil
	}, GradeWithSpec: func(_ context.Context, r harness.GradeRequest) (harness.Grade, error) {
		pass := r.Spec.Model == "candidate"
		return harness.Grade{Passed: pass, Checks: []harness.Check{{Name: "semantic", Passed: pass}, {Name: "integrity: originals", Passed: true}, {Name: "authority: contract", Passed: true}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	c.harness = m
	t.Cleanup(func() { m.Close(); db.Close() })
	spec := harness.Spec{WorkspaceID: "workspace", AgentID: "agent", Model: "baseline", ModelDigest: "baseline-digest", RuntimeVersion: "runtime", RuntimeIdentity: Doc{"runtimeId": "ollama-default", "backend": "fixture", "revision": "runtime", "artifactDigest": "baseline-digest"}, AppVersion: "same-corrected-host", ExecutorIdentity: "sha256:fixture", FrozenCapability: capability, AuthorityHash: harnessAuthorityHash(frozen), ProfileBindingHash: harness.Hash(Doc{}), BudgetSeconds: 60, ContextSize: 32768, Agent: agent}
	a, err := m.RegisterArmV2(harness.ArmV2{Name: "Current product", Spec: spec, Provenance: "product:frozen", Revision: harness.Revision{Content: harness.Content{Instructions: "baseline instructions"}}})
	if err != nil {
		t.Fatal(err)
	}
	spec.Agent = clone(agent)
	spec.Model = "candidate"
	spec.ModelDigest = "candidate-digest"
	spec.ContextSize = 65536
	spec.Agent["model"] = "candidate"
	spec.Agent["runtimeId"] = "managed-candidate"
	spec.Agent["temperature"] = 1.0
	spec.Agent["thinking"] = "on"
	spec.Agent["maxSteps"] = 80
	spec.Agent["maxOutputTokens"] = 16384
	spec.Agent["harnessMechanisms"] = Doc{"repeatedFailureFeedback": true}
	spec.Agent["verificationFeedback"] = true
	spec.RuntimeIdentity = Doc{"runtimeId": "managed-candidate", "backend": "fixture-managed", "revision": "runtime", "artifactDigest": "candidate-digest"}
	b, err := m.RegisterArmV2(harness.ArmV2{Name: "Candidate", Spec: spec, Provenance: "diagnostics:selected", Revision: harness.Revision{Content: harness.Content{Instructions: "candidate instructions"}, AuthoredBy: harness.Author{Kind: "codex", Identity: "candidate-author"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := m.CreateEvaluationV2(harness.EvaluationV2Request{PackID: "coding-qualification-v2", BaselineArmID: a.ID, CandidateArmID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	for run.Status != "completed" {
		if _, err = m.AdvanceRunV2(run.ID, harness.BatchLimits{MaxAttempts: 4}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for m.Active() {
			if time.Now().After(deadline) {
				t.Fatal("synthetic qualification timeout")
			}
			time.Sleep(time.Millisecond)
		}
		run, err = m.GetRunV2(run.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(run.Results) != 72 || !run.Gate.Eligible {
		t.Fatal("complete paired gate did not pass", run.Gate)
	}
	if _, err = c.promoteHarnessV2(t.Context(), run.ID); err == nil {
		t.Fatal("unreviewed profile promoted")
	}
	run, err = m.ReviewRunV2(run.ID, harness.ReviewV2{Author: harness.Author{Kind: "codex", Identity: "independent-reviewer"}, Verdict: "approved", EvidenceHash: run.Gate.EvidenceHash, Notes: "synthetic lifecycle evidence only"})
	if err != nil {
		t.Fatal(err)
	}
	return c, run, b, profile
}

func TestHarnessV2PromotionActivatesAndRollsBackWholeProfile(t *testing.T) {
	c, run, arm, _ := qualifiedProfileFixtureV2(t)
	c.runs = map[string]context.CancelFunc{"active": func() {}}
	if _, err := c.promoteHarnessV2(t.Context(), run.ID); err == nil {
		t.Fatal("profile activated while a run was active")
	}
	delete(c.runs, "active")
	before, _ := c.Store.Get("agent", "agent")
	promotion, err := c.promoteHarnessV2(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := c.Store.Get("agent", "agent")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("promotion changed personal agent")
	}
	effective, contextTokens, provenance, err := c.applyQualifiedProfile("workspace", after)
	if err != nil {
		t.Fatal(err)
	}
	if str(effective, "model") != "candidate" || str(effective, "runtimeId") != "managed-candidate" || str(effective, "thinking") != "on" || number(effective, "maxSteps", 0) != 80 || number(effective, "maxOutputTokens", 0) != 16384 || contextTokens != 65536 || provenance == nil {
		t.Fatal("partial profile activation", effective, contextTokens)
	}
	if effective["verificationFeedback"] != true || obj(effective["harnessMechanisms"])["repeatedFailureFeedback"] != true {
		t.Fatal("qualified mechanism choices were not activated", effective)
	}
	frozen, err := c.FreezeAgent(effective)
	if err != nil {
		t.Fatal(err)
	}
	request := engine.Request{Instructions: agentInstructions(effective)}
	if _, err = c.applyHarnessBinding("workspace", effective, &request, &frozen); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request.Instructions, arm.Revision.Content.Instructions) {
		t.Fatal("model switched without harness revision")
	}
	if err = c.activateHarnessRevision(t.Context(), "workspace", "agent", arm.Revision.ID, "other"); err == nil {
		t.Fatal("legacy revision activation split a qualified profile")
	}
	_, err = c.Store.Update("agent", "agent", func(d Doc) error { d["name"] = "Edited personal name"; d["unrelatedPreference"] = "new"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	personal, _ := c.Store.Get("agent", "agent")
	effective, _, _, err = c.applyQualifiedProfile("workspace", personal)
	if err != nil || effective["name"] != "Edited personal name" {
		t.Fatal("unrelated personal edit lost", err)
	}
	if _, err = c.rollbackHarnessV2(t.Context(), str(promotion, "id")); err != nil {
		t.Fatal(err)
	}
	restored, contextTokens, _, err := c.applyQualifiedProfile("workspace", personal)
	if err != nil || str(restored, "model") != "baseline" || contextTokens != 0 {
		t.Fatal("rollback left partial profile", err, restored)
	}
	if _, err = c.harnessSelectedWorkflow("workspace", "agent"); err != nil {
		t.Fatal("empty restored binding broke workflow lookup", err)
	}
	if err = c.activateHarnessRevision(t.Context(), "workspace", "agent", "", "later-legacy-revision"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.rollbackHarnessV2(t.Context(), str(promotion, "id")); err != nil {
		t.Fatal(err)
	}
	binding, _ := c.currentHarnessBinding("workspace", "agent")
	if str(binding, "revisionId") != "later-legacy-revision" {
		t.Fatal("idempotent rollback overwrote a later change")
	}
}

func TestHarnessV2PromotionRejectsAuthorityAndBindingDrift(t *testing.T) {
	c, run, _, profile := qualifiedProfileFixtureV2(t)
	drift := clone(profile)
	drift["tools"] = []string{"read_file", "write_file", "run_command", "search_files"}
	_ = c.Store.Put("capability", drift)
	if _, err := c.promoteHarnessV2(t.Context(), run.ID); err == nil {
		t.Fatal("changed authority accepted")
	}
	_ = c.Store.Put("capability", profile)
	key := harness.Hash([]string{"workspace", "agent"})
	_ = c.Store.Put("harness_binding", Doc{"id": key, "revisionId": "concurrent-change"})
	if _, err := c.promoteHarnessV2(t.Context(), run.ID); err == nil {
		t.Fatal("stale baseline overwrote concurrent binding")
	}
	_ = c.Store.Delete("harness_binding", key)
	promotion, err := c.promoteHarnessV2(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Store.Put("capability", drift)
	agent, _ := c.Store.Get("agent", "agent")
	if _, _, _, err = c.applyQualifiedProfile("workspace", agent); err == nil {
		t.Fatal("post-promotion authority drift granted")
	}
	_ = c.Store.Put("capability", profile)
	_, _ = c.Store.Update("harness_binding", key, func(d Doc) error { d["updatedAt"] = "concurrent change"; return nil })
	if _, err = c.rollbackHarnessV2(t.Context(), str(promotion, "id")); err == nil {
		t.Fatal("stale rollback overwrote newer binding")
	}
}

func TestHarnessV2ConcurrentPromotionHasOneDurableReceipt(t *testing.T) {
	c, run, _, _ := qualifiedProfileFixtureV2(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []Doc{}
	errs := []error{}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := c.promoteHarnessV2(context.Background(), run.ID)
			mu.Lock()
			results = append(results, r)
			errs = append(errs, e)
			mu.Unlock()
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(results[0], results[1]) {
		t.Fatal("concurrent promotion changed receipt")
	}
	rows, err := c.listHarnessPromotionsV2()
	if err != nil || len(rows) != 1 {
		t.Fatal("promotion was not atomic and idempotent", err, len(rows))
	}
}
