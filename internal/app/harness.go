package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"path/filepath"
	"strings"
	"time"
)

func (c *Core) openHarness() error {
	tasks, e := harness.LoadFixtures()
	if e != nil {
		return e
	}
	m, e := harness.Open(harness.Config{Root: filepath.Join(c.DataDir, "harness"), Store: c.Store, Tasks: tasks, Solve: c.executeHarnessSolver, Grade: c.gradeHarnessTrial, ValidateEnvironment: c.validateHarnessEnvironment, Activate: c.activateHarnessRevision, Upstream: c.runHarnessUpstream})
	if e != nil {
		return e
	}
	c.harness = m
	return nil
}
func (c *Core) harnessSummaries() Doc {
	d := Doc{"available": c.harness != nil, "upstreamRevision": harness.UpstreamRevision, "methodVersion": harness.MethodVersion}
	if c.harness == nil {
		return d
	}
	d["active"] = c.harness.Active()
	d["cacheCleanup"] = c.harnessCacheSummary()
	for _, k := range []string{"experiment", "packet", "revision", "lesson", "evaluation", "promotion"} {
		v, e := c.harness.List(k)
		if e != nil {
			d["error"] = e.Error()
			break
		}
		d[k+"s"] = v
	}
	return d
}
func (c *Core) harnessCall(ctx context.Context, method string, p Doc) (any, error) {
	if c.harness == nil {
		return nil, errors.New("Harness services are unavailable")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	switch method {
	case "harness.status":
		return c.harnessSummaries(), nil
	case "harness.fixtures.list":
		return Doc{"items": c.harness.Fixtures()}, nil
	case "harness.experiments.start":
		return c.startHarness(ctx, p)
	case "harness.experiments.advance":
		c.videoGuard.Lock()
		defer c.videoGuard.Unlock()
		if c.videoBusy() {
			return nil, errors.New("Wait for the current video job")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		busy := len(c.runs) > 0 || len(c.evals) > 0
		if busy {
			return nil, errors.New("Wait for active chats and comparisons before starting harness work")
		}
		return c.harness.Advance(str(p, "id"))
	case "harness.experiments.cancel", "harness.evaluations.cancel":
		return Doc{}, c.harness.Cancel(str(p, "id"))
	case "harness.packets.submit":
		var response harness.PacketResponse
		b, _ := json.Marshal(p)
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if e := decoder.Decode(&response); e != nil {
			return nil, e
		}
		return c.harness.Submit(response)
	case "harness.lessons.history":
		return c.harness.LessonHistory(str(p, "id"))
	case "harness.experiments.export":
		return c.harness.Export(str(p, "id"))
	case "harness.lessons.setStatus":
		return c.harness.SetLessonStatus(str(p, "id"), str(p, "status"))
	case "harness.evaluations.start", "harness.evaluations.resume":
		c.videoGuard.Lock()
		defer c.videoGuard.Unlock()
		if c.videoBusy() {
			return nil, errors.New("Wait for the current video job")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		busy := len(c.runs) > 0 || len(c.evals) > 0
		if busy {
			return nil, errors.New("Wait for active chats and comparisons")
		}
		if method == "harness.evaluations.resume" {
			return c.harness.ResumeEvaluation(str(p, "id"))
		}
		return c.harness.StartEvaluation(str(p, "experimentId"))
	case "harness.promote":
		return c.harness.Promote(ctx, str(p, "evaluationId"))
	case "harness.rollback":
		return c.harness.Rollback(ctx, str(p, "id"))
	}
	parts := strings.Split(method, ".")
	if len(parts) == 3 {
		kind := strings.TrimSuffix(parts[1], "s")
		switch kind {
		case "experiment", "revision", "packet", "trial", "lesson", "evaluation", "promotion":
			if parts[2] == "list" {
				v, e := c.harness.List(kind)
				return Doc{"items": v}, e
			}
			if parts[2] == "get" {
				return c.harness.Get(kind, str(p, "id"))
			}
		}
	}
	return nil, errors.New("Unknown harness operation")
}
func (c *Core) startHarness(ctx context.Context, p Doc) (any, error) {
	workspaceID, e := required(p, "workspaceId")
	if e != nil {
		return nil, e
	}
	if _, e = c.Store.Get("workspace", workspaceID); e != nil {
		return nil, e
	}
	agent, e := c.Store.Get("agent", str(p, "agentId"))
	if e != nil {
		return nil, e
	}
	f, e := c.FreezeAgent(agent)
	if e != nil {
		return nil, e
	}
	if f.Profile.ExecutionProfile != "docker" {
		return nil, errors.New("Harness coding fixtures require an explicit Docker capability profile")
	}
	base := harness.Content{Instructions: str(agent, "instructions"), DelegateAgentIDs: append([]string{}, f.Profile.DelegateAgentIDs...), Lessons: []string{}}
	capture := engine.Request{Instructions: defaultInstructions + "\n\n" + str(agent, "instructions")}
	f.Configure(&capture)
	binding, e := c.applyHarnessBinding(workspaceID, agent, &capture, &f)
	if e != nil {
		return nil, e
	}
	if binding != nil {
		raw, _ := json.Marshal(binding["revision"])
		var r harness.Revision
		if e = json.Unmarshal(raw, &r); e != nil {
			return nil, e
		}
		base = r.Content
		f.Skills = capture.SkillPackages
		agent = clone(agent)
		agent["baselineHarnessRevision"] = r.ID
		f.Digest = ""
		f.Digest = contentDigest(f)
	}
	controls, e := agentGenerationControls(agent)
	if e != nil {
		return nil, e
	}
	agent = clone(agent)
	agent["thinking"] = controls.thinking
	agent["temperature"] = controls.temperature
	agent["maxOutputTokens"] = controls.maxOutputTokens
	agent["toolProtocol"] = controls.toolProtocol
	settings := c.Settings()
	model := str(agent, "model")
	if model == "" {
		model = str(settings, "defaultModel")
	}
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	identity, e := c.Client().ModelIdentity(readCtx, model, "completion")
	if e != nil {
		return nil, e
	}
	version, e := c.Client().Version(readCtx)
	if e != nil {
		return nil, e
	}
	b, _ := json.Marshal(p["taskIds"])
	var ids []string
	if e = json.Unmarshal(b, &ids); e != nil {
		return nil, e
	}
	frozen, _ := json.Marshal(f)
	skills := map[string]json.RawMessage{}
	selected := []string{}
	for _, s := range f.Skills {
		b, _ := json.Marshal(s)
		skills[s.ID] = b
		selected = append(selected, s.ID)
	}
	effectiveTools := []string{}
	for _, n := range []string{"list_files", "read_file", "search_files", "write_file", "edit_file", "move_file", "make_directory", "run_command", "delegate_readonly"} {
		if contains(f.Profile.Tools, n) {
			effectiveTools = append(effectiveTools, n)
		}
	}
	var workflowIDs []string
	if p["workflowIds"] != nil {
		raw, _ := json.Marshal(p["workflowIds"])
		if e = json.Unmarshal(raw, &workflowIDs); e != nil {
			return nil, e
		}
	}
	approvedWorkflows, frozenAgents, e := c.freezeHarnessChoices(agent, workflowIDs, workspaceID)
	if e != nil {
		return nil, e
	}
	if base.WorkflowID != "" {
		frozenWorkflow, e := c.harnessSelectedWorkflow(workspaceID, str(agent, "id"))
		if e != nil {
			return nil, e
		}
		raw, _ := json.Marshal(frozenWorkflow)
		if approved := approvedWorkflows[base.WorkflowID]; len(approved) > 0 && harness.Hash(approved) != harness.Hash(json.RawMessage(raw)) {
			return nil, errors.New("selected workflow differs from active baseline snapshot")
		}
		approvedWorkflows[base.WorkflowID] = raw
		resolved, e := c.freezeRunAgents(agent, frozenWorkflow, workspaceID)
		if e != nil {
			return nil, e
		}
		for _, f := range resolved {
			b, _ := json.Marshal(f)
			frozenAgents[str(f.Agent, "id")] = b
		}
	}
	base.SkillIDs = selected
	spec := harness.Spec{ApprovedWorkflows: approvedWorkflows, FrozenAgents: frozenAgents, ApprovedDelegateIDs: append([]string{}, f.Profile.DelegateAgentIDs...), SolverTools: effectiveTools, Mode: str(p, "mode"), WorkspaceID: workspaceID, AgentID: str(agent, "id"), TaskIDs: ids, Model: identity.Name, ModelDigest: identity.Digest, RuntimeVersion: version, Agent: agent, ContextSize: number(p, "contextSize", number(settings, "contextSize", 32768)), FrozenCapability: frozen, ExecutorIdentity: f.ExecutionImage, ToolchainVersion: f.ExecutionImage, ApprovedSkills: skills, BudgetSeconds: number(p, "budgetSeconds", 1200), AppVersion: harnessBinaryIdentity()}
	if spec.Mode == "upstream" {
		_, spec.PythonIdentity, e = harnessPythonIdentity()
		if e != nil {
			return nil, e
		}
		spec.UpstreamAdapterHash, e = harnessAdapterIdentity()
		if e != nil {
			return nil, e
		}
	}
	if spec.ContextSize < 1024 || spec.ContextSize > 131072 {
		return nil, errors.New("contextSize must be 1024–131072")
	}
	return c.harness.Start(spec, base, harness.Author{Kind: "baseline", Identity: str(agent, "id")})
}
func (c *Core) validateHarnessEnvironment(ctx context.Context, s harness.Spec) error {
	if err := c.reconcileHarnessCaches(ctx); err != nil {
		return err
	}
	if s.Mode == "upstream" {
		digest, e := harnessAdapterIdentity()
		if e != nil {
			return e
		}
		if digest != s.UpstreamAdapterHash {
			return errors.New("upstream adapter changed after admission")
		}
	}
	if s.AppVersion != harnessBinaryIdentity() {
		return errors.New("application/grader binary changed after experiment admission")
	}
	identity, e := c.Client().ModelIdentity(ctx, s.Model, "completion")
	if e != nil {
		return e
	}
	if identity.Digest != s.ModelDigest {
		return errors.New("solver model digest changed after experiment admission")
	}
	v, e := c.Client().Version(ctx)
	if e != nil {
		return e
	}
	if v != s.RuntimeVersion {
		return errors.New("Ollama runtime changed after experiment admission")
	}
	_, e = c.executorForConfig("docker", s.ExecutorIdentity)
	return e
}

// Bindings are workspace+agent scoped. They never overwrite personal settings
// or the globally editable agent document; ordinary runs resolve this binding.
func (c *Core) activateHarnessRevision(ctx context.Context, workspaceID, agentID, previous, next string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if _, e := c.Store.Get("workspace", workspaceID); e != nil {
		return e
	}
	if _, e := c.Store.Get("agent", agentID); e != nil {
		return e
	}
	id := harness.Hash([]string{workspaceID, agentID})
	current, e := c.Store.Get("harness_binding", id)
	if e != nil && !strings.Contains(e.Error(), "entity not found") {
		return e
	}
	activeID := str(current, "revisionId")
	if activeID == next {
		return nil
	}
	if activeID != "" && activeID != previous {
		return errors.New("target harness changed since promotion/rollback was requested")
	}
	return c.Store.Put("harness_binding", Doc{"id": id, "workspaceId": workspaceID, "agentId": agentID, "revisionId": next, "previousId": previous, "updatedAt": time.Now().UTC().Format(time.RFC3339Nano)})
}
