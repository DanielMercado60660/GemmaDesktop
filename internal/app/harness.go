package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"math"
	"path/filepath"
	"strings"
	"time"
)

func (c *Core) openHarness() error {
	tasks, e := harness.LoadFixtures()
	if e != nil {
		return e
	}
	packs, e := harness.LoadFixturePacks()
	if e != nil {
		return e
	}
	m, e := harness.Open(harness.Config{Root: filepath.Join(c.DataDir, "harness"), Store: c.Store, Tasks: tasks, Packs: packs, GradeWithSpec: c.gradeHarnessRequest, Solve: c.executeHarnessSolver, Grade: c.gradeHarnessTrial, ValidateEnvironment: c.validateHarnessEnvironment, ValidateDevelopmentGrade: c.validateDevelopmentGradeAdmission, PrepareAttempt: c.prepareHarnessRuntime, ReleaseRuntime: c.releaseHarnessRuntime, Activate: c.activateHarnessRevision, Upstream: c.runHarnessUpstream})
	if e != nil {
		return e
	}
	c.harness = m
	return nil
}

// The Manager has marked itself active before this check. Clients admitted
// earlier are observed here; later chat/video admission sees the active gate.
// Never hold Core locks across executable grading.
func (c *Core) validateDevelopmentGradeAdmission(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.videoGuard.Lock()
	defer c.videoGuard.Unlock()
	if c.videoBusy() {
		return errors.New("video work is active")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.runs) > 0 || len(c.evals) > 0 || len(c.pulls) > 0 {
		return errors.New("chat or comparison work is active")
	}
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
	if strings.HasPrefix(method, "harness.arms.") || strings.HasPrefix(method, "harness.diagnostics.") || strings.HasPrefix(method, "harness.evaluationsV2.") || strings.HasPrefix(method, "harness.promotionsV2.") || strings.HasPrefix(method, "harness.batchesV2.") || method == "harness.packs.list" {
		return c.harnessV2Call(ctx, method, p)
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
	case "harness.experiments.batches":
		rows, err := c.harness.OptimizationBatchesV2(str(p, "id"))
		return Doc{"items": rows}, err
	case "harness.trials.gradeDevelopment":
		return c.harness.GradeDevelopmentTrial(ctx, str(p, "experimentId"), str(p, "trialId"))
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
	spec, base, err := c.freezeHarnessSpec(ctx, p, str(p, "fixturePackId") != "")
	if err != nil {
		return nil, err
	}
	if value, present := p["baseRevisionId"]; present {
		id, ok := value.(string)
		if !ok || strings.TrimSpace(id) != id || len(id) != 64 {
			return nil, errors.New("baseRevisionId must be an immutable development revision ID")
		}
		spec.DevelopmentBaseRevisionID = id
	}
	return c.harness.Start(spec, base, harness.Author{Kind: "baseline", Identity: spec.AgentID})
}
func (c *Core) freezeHarnessSpec(ctx context.Context, p Doc, v2 bool) (harness.Spec, harness.Content, error) {
	workspaceID, e := required(p, "workspaceId")
	if e != nil {
		return harness.Spec{}, harness.Content{}, e
	}
	if _, e = c.Store.Get("workspace", workspaceID); e != nil {
		return harness.Spec{}, harness.Content{}, e
	}
	agent, e := c.Store.Get("agent", str(p, "agentId"))
	if e != nil {
		return harness.Spec{}, harness.Content{}, e
	}
	profileContext := 0
	if v2 {
		var err error
		agent, profileContext, _, err = c.applyQualifiedProfile(workspaceID, agent)
		if err != nil {
			return harness.Spec{}, harness.Content{}, err
		}
		agent = clone(agent)
		for _, key := range []string{"model", "runtimeId", "thinking", "temperature", "maxOutputTokens", "maxSteps", "toolProtocol", "harnessMechanisms", "verificationFeedback"} {
			if value, ok := p[key]; ok {
				agent[key] = value
			}
		}
		if value, ok := agent["maxSteps"]; ok {
			n, valid := finiteNumber(value)
			if !valid || math.Trunc(n) != n || n < 1 || n > 200 {
				return harness.Spec{}, harness.Content{}, errors.New("maxSteps must be an integer from 1 to 200")
			}
		}
	}
	f, e := c.FreezeAgent(agent)
	if e != nil {
		return harness.Spec{}, harness.Content{}, e
	}
	authorityHash := ""
	if v2 {
		authorityHash = harnessAuthorityHash(f)
	}
	if f.Profile.ExecutionProfile != "docker" {
		return harness.Spec{}, harness.Content{}, errors.New("Harness coding fixtures require an explicit Docker capability profile")
	}
	base := harness.Content{Instructions: str(agent, "instructions"), DelegateAgentIDs: append([]string{}, f.Profile.DelegateAgentIDs...), Lessons: []string{}}
	capture := engine.Request{Instructions: agentInstructions(agent)}
	f.Configure(&capture)
	binding, e := c.applyHarnessBinding(workspaceID, agent, &capture, &f)
	if e != nil {
		return harness.Spec{}, harness.Content{}, e
	}
	if binding != nil {
		raw, _ := json.Marshal(binding["revision"])
		var r harness.Revision
		if e = json.Unmarshal(raw, &r); e != nil {
			return harness.Spec{}, harness.Content{}, e
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
		return harness.Spec{}, harness.Content{}, e
	}
	agent = clone(agent)
	agent["thinking"] = controls.thinking
	agent["temperature"] = controls.temperature
	agent["maxOutputTokens"] = controls.maxOutputTokens
	agent["toolProtocol"] = controls.toolProtocol
	settings := c.Settings()
	defaultContext := number(settings, "contextSize", 32768)
	if profileContext > 0 {
		defaultContext = profileContext
	}
	model := str(agent, "model")
	if model == "" {
		model = str(settings, "defaultModel")
	}
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var modelName, modelDigest, version string
	var runtimeIdentity Doc
	if v2 {
		runtimeID := str(agent, "runtimeId")
		if runtimeID == "" {
			runtimeID = str(settings, "defaultCompletionRuntimeId")
		}
		if runtimeID == "" {
			runtimeID = "ollama-default"
		}
		runtimeIdentity, e = c.completionIdentity(readCtx, runtimeID, model, number(p, "contextSize", defaultContext))
		if e != nil {
			return harness.Spec{}, harness.Content{}, e
		}
		agent["runtimeId"] = runtimeID
		modelName = model
		modelDigest = str(runtimeIdentity, "artifactDigest")
		version = str(runtimeIdentity, "revision")
	} else {
		identity, err := c.Client().ModelIdentity(readCtx, model, "completion")
		if err != nil {
			return harness.Spec{}, harness.Content{}, err
		}
		modelName = identity.Name
		modelDigest = identity.Digest
		version, e = c.Client().Version(readCtx)
		if e != nil {
			return harness.Spec{}, harness.Content{}, e
		}
	}
	b, _ := json.Marshal(p["taskIds"])
	var ids []string
	if e = json.Unmarshal(b, &ids); e != nil {
		return harness.Spec{}, harness.Content{}, e
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
	for _, n := range []string{"list_files", "read_file", "search_files", "write_file", "edit_file", "move_file", "make_directory", "run_command", "delegate_readonly", "verify_task"} {
		if contains(f.Profile.Tools, n) {
			effectiveTools = append(effectiveTools, n)
		}
	}
	var workflowIDs []string
	if p["workflowIds"] != nil {
		raw, _ := json.Marshal(p["workflowIds"])
		if e = json.Unmarshal(raw, &workflowIDs); e != nil {
			return harness.Spec{}, harness.Content{}, e
		}
	}
	approvedWorkflows, frozenAgents, e := c.freezeHarnessChoices(agent, workflowIDs, workspaceID)
	if e != nil {
		return harness.Spec{}, harness.Content{}, e
	}
	if base.WorkflowID != "" {
		frozenWorkflow, e := c.harnessSelectedWorkflow(workspaceID, str(agent, "id"))
		if e != nil {
			return harness.Spec{}, harness.Content{}, e
		}
		raw, _ := json.Marshal(frozenWorkflow)
		if approved := approvedWorkflows[base.WorkflowID]; len(approved) > 0 && harness.Hash(approved) != harness.Hash(json.RawMessage(raw)) {
			return harness.Spec{}, harness.Content{}, errors.New("selected workflow differs from active baseline snapshot")
		}
		approvedWorkflows[base.WorkflowID] = raw
		resolved, e := c.freezeRunAgents(agent, frozenWorkflow, workspaceID)
		if e != nil {
			return harness.Spec{}, harness.Content{}, e
		}
		for _, f := range resolved {
			b, _ := json.Marshal(f)
			frozenAgents[str(f.Agent, "id")] = b
		}
	}
	base.SkillIDs = selected
	spec := harness.Spec{ApprovedWorkflows: approvedWorkflows, FrozenAgents: frozenAgents, ApprovedDelegateIDs: append([]string{}, f.Profile.DelegateAgentIDs...), SolverTools: effectiveTools, Mode: str(p, "mode"), WorkspaceID: workspaceID, AgentID: str(agent, "id"), TaskIDs: ids, Model: modelName, ModelDigest: modelDigest, RuntimeVersion: version, RuntimeIdentity: runtimeIdentity, FixturePackID: str(p, "fixturePackId"), AuthorityHash: authorityHash, Agent: agent, ContextSize: number(p, "contextSize", defaultContext), FrozenCapability: frozen, ExecutorIdentity: f.ExecutionImage, ToolchainVersion: f.ExecutionImage, ApprovedSkills: skills, BudgetSeconds: number(p, "budgetSeconds", 1200), AppVersion: harnessBinaryIdentity()}
	if v2 {
		current, err := c.currentHarnessBinding(workspaceID, str(agent, "id"))
		if err != nil {
			return harness.Spec{}, harness.Content{}, err
		}
		if binding != nil && harness.Hash(binding["binding"]) != harness.Hash(current) {
			return harness.Spec{}, harness.Content{}, errors.New("active profile changed while freezing comparison")
		}
		if binding == nil && str(current, "revisionId") != "" {
			return harness.Spec{}, harness.Content{}, errors.New("active profile changed while freezing comparison")
		}
		spec.ProfileBindingHash = harness.Hash(bindingSelectionV2(current))
	}
	if spec.Mode == "upstream" {
		_, spec.PythonIdentity, e = harnessPythonIdentity()
		if e != nil {
			return harness.Spec{}, harness.Content{}, e
		}
		spec.UpstreamAdapterHash, e = harnessAdapterIdentity()
		if e != nil {
			return harness.Spec{}, harness.Content{}, e
		}
	}
	if spec.ContextSize < 1024 || spec.ContextSize > 131072 {
		return harness.Spec{}, harness.Content{}, errors.New("contextSize must be 1024–131072")
	}
	return spec, base, nil
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
	var e error
	if len(s.RuntimeIdentity) > 0 {
		identity, err := c.completionIdentity(ctx, str(s.RuntimeIdentity, "runtimeId"), s.Model, s.ContextSize)
		if err != nil {
			return err
		}
		if harness.Hash(obj(identity)) != harness.Hash(obj(s.RuntimeIdentity)) {
			return errors.New("completion runtime identity changed after admission")
		}
	} else {
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
	_, e := c.Store.Mutate("harness_binding", id, func(current Doc) (Doc, error) {
		if str(current, "armId") != "" {
			return nil, errors.New("active qualified profile requires complete-profile promotion or rollback")
		}
		activeID := str(current, "revisionId")
		if activeID == next {
			return nil, nil
		}
		if activeID != "" && activeID != previous {
			return nil, errors.New("target harness changed since promotion/rollback was requested")
		}
		binding := Doc{"id": id, "workspaceId": workspaceID, "agentId": agentID, "revisionId": next, "previousId": previous, "updatedAt": time.Now().UTC().Format(time.RFC3339Nano)}
		if history := current["promotionsV2"]; history != nil {
			binding["promotionsV2"] = history
		}
		return binding, nil
	})
	return e
}
