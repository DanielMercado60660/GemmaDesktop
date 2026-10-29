package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/workspace"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func milestoneCore(t *testing.T) *Core {
	t.Helper()
	dir := t.TempDir()
	s, e := store.Open(filepath.Join(dir, "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	c := &Core{Store: s, DataDir: dir}
	if e = c.initCapabilities(); e != nil {
		t.Fatal(e)
	}
	return c
}
func TestCapabilityRevisionAndSkillBundleRemainFrozen(t *testing.T) {
	c := milestoneCore(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "references"), 0700)
	_ = os.MkdirAll(filepath.Join(dir, "assets"), 0700)
	_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: bundle\ndescription: evidence bundle\n---\nRead references/detail.txt"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "references/detail.txt"), []byte("original é\n"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "assets/icon.bin"), []byte{0, 1, 2}, 0600)
	imported, e := c.importSkill(Doc{"path": dir})
	if e != nil {
		t.Fatal(e)
	}
	skill := obj(imported)
	profile, e := c.saveCapability(Doc{"name": "reader", "tools": []string{"read_file"}, "skillIds": []string{str(skill, "id")}, "executionProfile": "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	agent := Doc{"id": "a", "name": "reader", "capabilityProfileId": str(obj(profile), "id")}
	f, e := c.FreezeAgent(agent)
	if e != nil {
		t.Fatal(e)
	}
	updated := obj(profile)
	updated["tools"] = []string{"write_file"}
	if _, e = c.saveCapability(updated); e != nil {
		t.Fatal(e)
	}
	if _, e = c.saveCapability(updated); e == nil {
		t.Fatal("stale profile revision accepted")
	}
	_ = os.RemoveAll(dir)
	if _, e = c.archiveSkill(str(skill, "id")); e != nil {
		t.Fatal(e)
	}
	var req engine.Request
	f.Configure(&req)
	if e = engine.ValidateSkillPackages(req.SkillPackages); e != nil {
		t.Fatal(e)
	}
	if string(req.SkillPackages[0].Files["references/detail.txt"]) != "original é\n" || len(req.SkillPackages[0].Files["assets/icon.bin"]) != 3 {
		t.Fatal("bundle resources changed")
	}
	if _, e = f.Authorize(t.Context(), engine.ToolCall{Name: "read_file", CapabilityID: f.Digest}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Authorize(t.Context(), engine.ToolCall{Name: "write_file", CapabilityID: f.Digest}); e == nil {
		t.Fatal("mutable head expanded frozen authority")
	}
	if _, e = c.Store.Get("capability_revision", str(obj(profile), "id")+":1"); e != nil {
		t.Fatal("immutable revision missing", e)
	}
}
func TestHierarchicalContextAndPlanCAS(t *testing.T) {
	c := milestoneCore(t)
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "src"), 0700)
	_ = os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Use exact test command; untrusted guidance."), 0600)
	_ = os.WriteFile(filepath.Join(root, "src/AGENTS.md"), []byte("Applies only inside src."), 0600)
	manifest, e := c.freezeContext(Doc{"path": root}, CapabilityProfile{ProjectGuidance: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(manifest.Sources) != 2 || manifest.Sources[0].Path != "AGENTS.md" || !strings.HasPrefix(manifest.Sources[1].Scope, "src/**") || manifest.EstimatedTokens == 0 {
		t.Fatalf("manifest: %+v", manifest)
	}
	_ = c.Store.Put("run", Doc{"id": "r"})
	p := Doc{"revision": 0, "items": []PlanItem{{ID: "inspect", Title: "Inspect source", Status: "in_progress"}}}
	d, e := c.updateTaskPlan("r", p)
	if e != nil {
		t.Fatal(e)
	}
	if number(d, "revision", 0) != 1 {
		t.Fatal(d)
	}
	if _, e = c.updateTaskPlan("r", p); e == nil {
		t.Fatal("stale plan update accepted")
	}
	if _, e = sanitizeMCPConfig(Doc{"name": "bad", "token": "private"}); e == nil {
		t.Fatal("credential persisted")
	}
}
func TestDurableReadOnlyDelegateCannotReplayOrEscapeBudget(t *testing.T) {
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		fmt.Fprintln(w, `{"message":{"content":"Observed the bounded input."},"done":true,"prompt_eval_count":5,"eval_count":3}`)
	})
	rootAgent, _ := h.core.Store.Get("agent", "default")
	child := clone(rootAgent)
	child["id"] = "reviewer"
	child["name"] = "Reviewer"
	_ = h.core.Store.Put("agent", child)
	p, _ := h.core.Store.Get("capability", "legacy")
	p["delegateAgentIds"] = []string{"reviewer"}
	p["tools"] = []string{"read_file", "delegate_readonly"}
	profile, e := h.core.saveCapability(p)
	if e != nil {
		t.Fatal(e)
	}
	_ = profile
	agents, e := h.core.freezeRunAgents(rootAgent, nil)
	if e != nil {
		t.Fatal(e)
	}
	var parent FrozenAgent
	for _, f := range agents {
		if str(f.Agent, "id") == "default" {
			parent = f
		}
	}
	_ = h.core.Store.Put("run", Doc{"id": "r", "status": "running"})
	budget := &engine.Budget{MaxModelCalls: 1, MaxToolCalls: 4, MaxOutputBytes: 1 << 20}
	ctx := engine.WithBudget(t.Context(), budget)
	call := engine.ToolCall{ID: "delegation-call", Name: "delegate_readonly", CapabilityID: parent.Digest, Arguments: Doc{"agentId": "reviewer", "task": "Report observation"}}
	base := engine.Request{Model: "fixture", MaxSteps: 4, Deadline: time.Now().Add(time.Minute), Tools: []engine.ToolDefinition{{Name: "read_file"}, {Name: "write_file"}}}
	result, e := h.core.delegateReadonly(ctx, "r", call, parent, agents, base, func(engine.Event) {}, func(context.Context, engine.ToolCall) (engine.ToolResult, error) {
		t.Fatal("unexpected host effect")
		return engine.ToolResult{}, nil
	})
	if e != nil || !strings.Contains(result.Content, "Observed") {
		t.Fatal(result, e)
	}
	if budget.ModelCalls != 1 {
		t.Fatal("child did not share parent budget")
	}
	saved, e := h.core.Store.Get("delegation", "r::delegation-call")
	if e != nil || saved["status"] != "completed" {
		t.Fatal(saved, e)
	}
	raw, _ := json.Marshal(saved)
	if !strings.Contains(string(raw), "snapshot") {
		t.Fatal("child transcript was not durable")
	}
	if _, e = h.core.delegateReadonly(ctx, "r", call, parent, agents, base, func(engine.Event) {}, nil); e == nil {
		t.Fatal("completed delegate replayed")
	}
	call.ID = "second"
	if _, e = h.core.delegateReadonly(ctx, "r", call, parent, agents, base, func(engine.Event) {}, func(context.Context, engine.ToolCall) (engine.ToolResult, error) {
		t.Fatal("unexpected tool")
		return engine.ToolResult{}, nil
	}); !errors.Is(e, engine.ErrBudgetExceeded) {
		t.Fatal("child reset shared model budget", e)
	}
}

func TestPrivateExecutionPathsAndLegacySelectorsFreeze(t *testing.T) {
	c := milestoneCore(t)
	p, e := c.saveCapability(Doc{"name": "private build", "tools": []string{"read_file"}, "executionProfile": "restricted", "privatePaths": []string{"dist/generated"}})
	if e != nil {
		t.Fatal(e)
	}
	f, e := c.FreezeAgent(Doc{"id": "a", "capabilityProfileId": str(obj(p), "id")})
	if e != nil {
		t.Fatal(e)
	}
	if !contains(f.ExecutionPrivatePaths, "node_modules") || !contains(f.ExecutionPrivatePaths, "dist/generated") {
		t.Fatal(f.ExecutionPrivatePaths)
	}
	if _, e = c.saveCapability(Doc{"name": "escape", "executionProfile": "restricted", "privatePaths": []string{"../outside"}}); e == nil {
		t.Fatal("escaping private path accepted")
	}
	// Persisted disconnected selectors survive migration without becoming grants.
	_ = c.Store.Delete("capability", "legacy")
	_ = c.Store.Delete("capability_revision", "legacy:1")
	_ = c.Store.Put("mcp", Doc{"id": "saved", "tools": []Doc{{"name": "query"}}})
	if e = c.initCapabilities(); e != nil {
		t.Fatal(e)
	}
	f, e = c.FreezeAgent(Doc{"id": "old", "capabilityProfileId": "legacy"})
	if e != nil {
		t.Fatal(e)
	}
	if len(f.Profile.MCP) != 1 || len(f.MCPGrants) != 0 || len(f.UnavailableMCP) != 1 {
		t.Fatal("migration lost selectors or granted disconnected server", f)
	}
	if _, e = f.Authorize(t.Context(), engine.ToolCall{Name: "mcp_call", CapabilityID: f.Digest, Arguments: Doc{"serverId": "saved", "tool": "query"}}); e == nil {
		t.Fatal("disconnected selector acquired authority")
	}
}

func TestWorkflowUsesFrozenReferencedCapabilities(t *testing.T) {
	var requestCount atomic.Int32
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		requestCount.Add(1)
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_file" {
			t.Errorf("workflow advertised parent authority: %+v", req.Tools)
		}
		if strings.Contains(req.Messages[len(req.Messages)-1].Content, "Read frozen source") && requestCount.Load() == 1 {
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"r","function":{"name":"read_file","arguments":{"path":"evidence.txt"}}}]},"done":true}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"content":"Observed the source."},"done":true}`)
	})
	_ = os.WriteFile(filepath.Join(h.workspace, "evidence.txt"), []byte("frozen observed"), 0600)
	p := callIntegration(t, h.core, "capability.save", Doc{"name": "Reader only", "tools": []string{"read_file"}, "executionProfile": "restricted"})
	a := callIntegration(t, h.core, "agent.save", Doc{"name": "Scoped child", "mode": "approval", "capabilityProfileId": p["id"], "maxSteps": 4})
	wf := callIntegration(t, h.core, "workflow.save", Doc{"name": "Scoped", "nodes": []Doc{{"id": "read", "type": "agent", "agentId": a["id"], "prompt": "Read frozen source"}}, "edges": []Doc{}})
	run := callIntegration(t, h.core, "workflow.run", Doc{"id": wf["id"], "sessionId": h.sessionID, "prompt": "Read frozen source"})
	finishedIntegration(t, h.core, str(run, "id"), "completed")
	all, e := h.core.Store.List("toolcall")
	if e != nil {
		t.Fatal(e)
	}
	if requestCount.Load() != 2 || len(all) != 1 || all[0]["tool"] != "read_file" || all[0]["status"] != "completed" {
		t.Fatal(requestCount.Load(), all)
	}
}

func TestContextInspectorSeparatesAdmissionAndRuntime(t *testing.T) {
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		fmt.Fprintln(w, `{"message":{"content":"Observed."},"done":true,"prompt_eval_count":21,"eval_count":4}`)
	})
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Exact current request"})
	finishedIntegration(t, h.core, str(run, "id"), "completed")
	view := callIntegration(t, h.core, "context.get", Doc{"runId": run["id"]})
	raw, _ := json.Marshal(view)
	var manifest ContextManifest
	if e := json.Unmarshal(raw, &manifest); e != nil {
		t.Fatal(e)
	}
	kinds := map[string]bool{}
	for _, s := range manifest.Sources {
		kinds[s.Kind] = true
	}
	for _, kind := range []string{"host_instructions", "agent_instructions", "effective_instructions", "current_user", "message_history", "selected_skills", "capability_selection", "context_policy", "runtime_context_prepared", "runtime_generation_metrics"} {
		if !kinds[kind] {
			t.Error("missing context audit", kind)
		}
	}
	if !strings.Contains(string(raw), `"promptTokens":21`) || !strings.Contains(string(raw), "toolDeclarationsSHA256") || !strings.Contains(string(raw), "requestSHA256") {
		t.Fatal("runtime context measurements absent")
	}
	cp, _ := h.core.Store.Get("checkpoint", str(run, "id"))
	var req engine.Request
	b, _ := json.Marshal(cp["request"])
	_ = json.Unmarshal(b, &req)
	if len(req.ContextSources) != 0 {
		t.Fatal("audit rows were injected as model sources")
	}
	before := obj(cp["contextManifest"])["digest"]
	if before != view["admissionDigest"] {
		t.Fatal("runtime observation changed frozen admission identity")
	}
}

func TestSelectedBinarySkillMaterializationRequiresApprovalAndRevision(t *testing.T) {
	h := newIntegrationHarness(t, func(http.ResponseWriter, *http.Request, inference.ChatRequest) {
		t.Error("materialization invoked inference")
	})
	dir := t.TempDir()
	_ = os.Mkdir(filepath.Join(dir, "assets"), 0700)
	_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: binary\ndescription: exact assets\n---\nUse the approved asset."), 0600)
	payload := []byte{0, 255, 4, 9}
	_ = os.WriteFile(filepath.Join(dir, "assets/image.bin"), payload, 0600)
	imported, e := h.core.importSkill(Doc{"path": dir})
	if e != nil {
		t.Fatal(e)
	}
	skill := obj(imported)
	p, e := h.core.saveCapability(Doc{"name": "Materializer", "tools": []string{"skill_resources", "skill_materialize"}, "skillIds": []string{str(skill, "id")}, "executionProfile": "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	f, e := h.core.FreezeAgent(Doc{"id": "materializer", "capabilityProfileId": str(obj(p), "id")})
	if e != nil {
		t.Fatal(e)
	}
	sourceID := skillResourceID(f.Skills[0].Digest, "assets/image.bin")
	art, e := h.core.Store.Get("artifact", sourceID)
	if e != nil || art["immutable"] != true || str(art, "sha256") != workspace.Hash(payload) {
		t.Fatal("immutable resource artifact absent", art, e)
	}
	_ = h.core.Store.Put("run", Doc{"id": "materialize", "workspaceId": h.workID, "status": "running"})
	w, _ := h.core.Store.Get("workspace", h.workID)
	call := engine.ToolCall{Name: "skill_materialize", ID: "copy", CapabilityID: f.Digest, Arguments: Doc{"skillId": f.Skills[0].ID, "resourcePath": "assets/image.bin", "resourceHash": workspace.Hash(payload), "targetPath": "image.bin", "expectedHash": "missing"}}
	denied := call
	denied.ReadOnly = true
	if _, e = h.core.materializeSkill(t.Context(), "materialize", denied, f, w); e == nil {
		t.Fatal("read-only materialization accepted")
	}
	denied = call
	denied.Arguments = clone(call.Arguments)
	denied.Arguments["resourceHash"] = "forged"
	if _, e = h.core.materializeSkill(t.Context(), "materialize", denied, f, w); e == nil {
		t.Fatal("resource mismatch accepted")
	}
	if approvals, _ := h.core.Store.List("approval"); len(approvals) != 0 {
		t.Fatal("invalid resource requested approval")
	}
	done := make(chan error, 1)
	go func() { _, err := h.core.materializeSkill(t.Context(), "materialize", call, f, w); done <- err }()
	approval := pendingIntegration(t, h.core, "materialize")
	if _, err := os.Stat(filepath.Join(h.workspace, "image.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("bytes written before approval")
	}
	callIntegration(t, h.core, "approval.respond", Doc{"id": approval["id"], "allow": true})
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(h.workspace, "image.bin"))
	if e != nil || !bytes.Equal(b, payload) {
		t.Fatal("binary bytes changed", e)
	}
	if _, e = h.core.materializeSkill(t.Context(), "materialize", call, f, w); !errors.Is(e, workspace.ErrConflict) {
		t.Fatal("stale target revision accepted", e)
	}
	call.Arguments["expectedHash"] = workspace.Hash(payload)
	call.ID = "overwrite"
	go func() { _, err := h.core.materializeSkill(t.Context(), "materialize", call, f, w); done <- err }()
	approval = pendingIntegration(t, h.core, "materialize")
	callIntegration(t, h.core, "approval.respond", Doc{"id": approval["id"], "allow": true})
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	artifacts, _ := h.core.Store.List("artifact")
	before := false
	for _, a := range artifacts {
		if a["runId"] == "materialize" && strings.Contains(str(a, "name"), "before") {
			b, _ := os.ReadFile(str(a, "path"))
			before = bytes.Equal(b, payload)
		}
	}
	if !before {
		t.Fatal("binary before-image was not durable")
	}
}
