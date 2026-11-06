package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/tools"
	"gemmadesktop.local/app/internal/workflow"
	"gemmadesktop.local/app/internal/workspace"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (c *Core) executeHarnessSolver(ctx context.Context, in harness.SolveRequest, emit func(harness.Event)) (out harness.SolveResult, retErr error) {
	if len(in.Spec.RuntimeIdentity) > 0 {
		backend, identity, err := c.completionBackend(ctx, str(in.Spec.RuntimeIdentity, "runtimeId"), in.Spec.Model, in.Spec.ContextSize)
		if err != nil {
			return out, err
		}
		if contentDigest(obj(identity)) != contentDigest(obj(in.Spec.RuntimeIdentity)) {
			return out, errors.New("runtime identity changed before solver admission")
		}
		ctx = context.WithValue(ctx, completionKey{}, &frozenCompletion{CompletionBackend: backend, core: c, bindings: Doc{in.Spec.Model: in.Spec.RuntimeIdentity}})
	}
	var f FrozenAgent
	if e := json.Unmarshal(in.Spec.FrozenCapability, &f); e != nil {
		return harness.SolveResult{}, e
	}
	digest := f.Digest
	f.Digest = ""
	if contentDigest(f) != digest {
		return harness.SolveResult{}, errors.New("frozen capability digest mismatch")
	}
	f.Digest = digest
	if f.Profile.ExecutionProfile != "docker" || f.ExecutionImage != in.Spec.ExecutorIdentity {
		return harness.SolveResult{}, errors.New("experiment executor differs from frozen capability")
	}
	f, agents, e := harnessSelectedAgents(in.Spec, in.Revision.Content, f)
	if e != nil {
		return harness.SolveResult{}, e
	}
	runner, e := c.executorForConfig("docker", in.Spec.ExecutorIdentity, f.ExecutionPrivatePaths)
	if e != nil {
		return harness.SolveResult{}, e
	}
	cleanup, e := c.beginHarnessCache("trial", in.TrialID, in.Root, in.Spec.ExecutorIdentity)
	if e != nil {
		return harness.SolveResult{}, e
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := c.finishHarnessCache(cleanupCtx, runner, cleanup); err != nil {
			out.Incomplete = true
			retErr = errors.Join(retErr, fmt.Errorf("disposable Harness cache cleanup failed: %w", err))
		}
	}()
	allowed := map[string]bool{}
	for _, n := range []string{"list_files", "read_file", "search_files", "write_file", "edit_file", "move_file", "make_directory", "run_command", "delegate_readonly"} {
		if contains(f.Profile.Tools, n) && contains(in.Spec.SolverTools, n) {
			allowed[n] = true
		}
	}
	if len(in.Revision.Content.DelegateAgentIDs) == 0 {
		delete(allowed, "delegate_readonly")
	}
	var contract *execution.VerificationContract
	if in.Task.Contract == "coding" || in.Task.Contract == "read_only" {
		contract = &execution.VerificationContract{Command: append([]string{}, in.Task.Command...), ProtectedPaths: append([]string{}, in.Task.ProtectedFiles...), RequireRegressionTest: in.Task.Contract == "coding"}
		if len(contract.Command) == 0 {
			return harness.SolveResult{}, errors.New("versioned task is missing its declared verification command")
		}
		if allowed["run_command"] {
			allowed["verify_task"] = true
		}
		if in.Task.Contract == "read_only" {
			for _, name := range []string{"write_file", "edit_file", "move_file", "make_directory", "run_command", "delegate_readonly"} {
				delete(allowed, name)
			}
		}
	}
	authorize := func(_ context.Context, a execution.Approval) error {
		if !allowed[a.Tool] {
			return errors.New("tool is outside the fixed experiment permissions")
		}
		return nil
	}
	mode := "approval"
	if in.Task.Contract == "read_only" {
		mode = "read-only"
	}
	guidance := c.newGuidanceGuard(in.TrialID, in.Root, f.Profile.ProjectGuidance, nil, f.ExecutionPrivatePaths)
	guidanceRegistry := &guidanceRegistry{}
	guidanceRegistry.add(guidance)
	mgr, e := tools.New(in.Root, tools.Options{Mode: mode, Verification: contract, BeforeAccess: guidance, Approve: authorize, Executor: runner, ArtifactDir: filepath.Join(c.DataDir, "harness", "trials", in.TrialID, "artifacts")})
	if e != nil {
		return harness.SolveResult{}, e
	}
	defer mgr.Close()
	req := engine.Request{Model: in.Spec.Model, Prompt: in.Prompt, Instructions: defaultInstructions + "\n" + in.Revision.Content.Instructions + "\n" + in.Revision.Content.ToolGuidance + "\n" + strings.Join(in.Revision.Content.Lessons, "\n") + "\nUse readOnly:true for the prescribed verification command. Add a new regression test file; preserve the original public tests and README.", MaxSteps: number(in.Spec.Agent, "maxSteps", 24), ContextSize: in.Spec.ContextSize, Timeout: time.Duration(in.Spec.BudgetSeconds) * time.Second, ContextPolicy: &engine.ContextPolicy{Enabled: true}, CapabilityID: f.Digest}
	if in.Task.Contract != "" {
		req.Instructions = defaultInstructions + "\n" + in.Revision.Content.Instructions + "\n" + in.Revision.Content.ToolGuidance + "\n" + strings.Join(in.Revision.Content.Lessons, "\n")
		req.ReadOnly = in.Task.Contract == "read_only"
		req.VerificationRequired = in.Task.Contract == "coding"
		req.VerificationFeedback = in.Spec.Agent["verificationFeedback"] == true
		if req.VerificationRequired {
			req.Instructions += "\nUse verify_task after your last edit. Preserve the declared protected paths and add a discoverable regression test that fails on the original defect and passes after repair."
		}
	}
	controls, e := agentGenerationControls(in.Spec.Agent)
	if e != nil {
		return harness.SolveResult{}, e
	}
	controls.apply(&req)
	for _, id := range in.Revision.Content.SkillIDs {
		raw, ok := in.Spec.ApprovedSkills[id]
		if !ok {
			return harness.SolveResult{}, errors.New("unapproved skill revision")
		}
		var p engine.SkillPackage
		if e = json.Unmarshal(raw, &p); e != nil {
			return harness.SolveResult{}, e
		}
		req.SkillPackages = append(req.SkillPackages, p)
	}
	if e = engine.ValidateSkillPackages(req.SkillPackages); e != nil {
		return harness.SolveResult{}, e
	}
	for _, d := range mgr.List() {
		if allowed[d.Name] {
			req.Tools = append(req.Tools, engine.ToolDefinition{Name: d.Name, Description: d.Description, Parameters: d.Parameters})
		}
	}
	if allowed["delegate_readonly"] {
		req.Tools = append(req.Tools, delegateTool())
	}
	// Every workflow/delegate receives only skills selected by this revision.
	selectedAgents := map[string]FrozenAgent{}
	for _, agent := range agents {
		skills := []engine.SkillPackage{}
		for _, selected := range req.SkillPackages {
			for _, approved := range agent.Skills {
				if selected.Digest == approved.Digest {
					skills = append(skills, selected)
					break
				}
			}
		}
		agent.Skills = skills
		agent.Digest = ""
		agent.Digest = contentDigest(agent)
		selectedAgents[agent.Digest] = agent
		if str(agent.Agent, "id") == in.Spec.AgentID {
			f = agent
		}
	}
	agents = selectedAgents
	req.CapabilityID = f.Digest
	ctx = engine.WithBudget(ctx, &engine.Budget{MaxModelCalls: req.MaxSteps, MaxToolCalls: 100, MaxOutputBytes: 8 << 20})
	var mu sync.Mutex
	trace := []harness.ToolObservation{}
	sequence := 0
	hostChecks := &harnessHostChecks{}
	emitEngine := func(event engine.Event) {
		guidanceRegistry.observe(event)
		hostChecks.observe(event)
		if emit != nil && event.Type != "adk_event" && event.Type != "thinking" {
			emit(harness.Event{Type: event.Type, Title: event.Title, Detail: event.Detail, Data: event.Data})
		}
	}
	var execute engine.ToolExecutor
	execute = func(ctx context.Context, call engine.ToolCall) (engine.ToolResult, error) {
		hostInitiated := hostChecks.take(call)
		if !allowed[call.Name] {
			return engine.ToolResult{IsError: true}, errors.New("tool outside experiment allowlist")
		}
		selected, ok := agents[call.CapabilityID]
		if !ok {
			return engine.ToolResult{IsError: true}, errors.New("unknown frozen capability")
		}
		authorityCall := call
		if call.Name == "verify_task" && contract != nil {
			authorityCall.Name = "run_command"
		}
		if _, err := selected.Authorize(ctx, authorityCall); err != nil {
			return engine.ToolResult{IsError: true}, err
		}
		mu.Lock()
		sequence++
		startedSequence := sequence
		mu.Unlock()
		var r engine.ToolResult
		var captured []harness.Doc
		var err error
		if call.Name == "delegate_readonly" {
			r, err = c.delegateReadonly(ctx, in.TrialID, call, selected, agents, req, emitEngine, execute)
		} else {
			value, runErr := mgr.Execute(conversationScoped(ctx, call), call.Name, call.Arguments)
			for _, artifact := range value.Artifacts {
				captured = append(captured, harness.Doc{"path": artifact.Path, "mediaType": artifact.MediaType, "size": artifact.Size, "beforeHash": workspace.Hash([]byte(artifact.Before)), "afterHash": workspace.Hash([]byte(artifact.After))})
			}
			r = engine.ToolResult{Content: value.Content, Data: obj(value.Data), IsError: runErr != nil}
			err = runErr
		}
		mu.Lock()
		sequence++
		entry := harness.ToolObservation{CallID: call.ID, Tool: call.Name, Arguments: call.Arguments, Content: r.Content, Data: obj(r.Data), Sequence: sequence, StartSequence: startedSequence, Artifacts: captured}
		if hostInitiated {
			// Trace copy only: the result returned below stays as the tool made
			// it. obj(nil) is nil when the check failed before producing data.
			if entry.Data == nil {
				entry.Data = harness.Doc{}
			}
			entry.Data["hostInitiated"] = true
		}
		entry = boundHarnessObservation(entry)
		if err != nil {
			entry.Error = err.Error()
		}
		trace = append(trace, entry)
		mu.Unlock()
		return engine.ToolResult{Content: r.Content, Data: obj(r.Data), IsError: err != nil}, err
	}
	var result engine.Result
	eng := engine.New(c.completionFromContext(ctx))
	if id := in.Revision.Content.WorkflowID; id != "" {
		var def workflow.Definition
		if e = json.Unmarshal(in.Spec.ApprovedWorkflows[id], &def); e != nil {
			return harness.SolveResult{}, e
		}
		for _, node := range def.Nodes {
			if node.Type == "approval" {
				return harness.SolveResult{}, errors.New("interactive approval workflow is not eligible for automated trials")
			}
		}
		result, e = workflow.New(eng).Run(ctx, def, req, emitEngine, execute, harnessWorkflowResolver(req, f, agents))
	} else {
		result, e = eng.Run(ctx, req, emitEngine, execute)
	}
	return harness.SolveResult{Content: result.Content, Incomplete: result.Incomplete, Usage: obj(result.Usage), TokenAccounting: result.TokenAccounting, ToolCalls: trace}, e
}
