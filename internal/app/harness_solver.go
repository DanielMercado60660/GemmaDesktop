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
	authorize := func(_ context.Context, a execution.Approval) error {
		if !allowed[a.Tool] {
			return errors.New("tool is outside the fixed experiment permissions")
		}
		return nil
	}
	mgr, e := tools.New(in.Root, tools.Options{Mode: "approval", Approve: authorize, Executor: runner, ArtifactDir: filepath.Join(c.DataDir, "harness", "trials", in.TrialID, "artifacts")})
	if e != nil {
		return harness.SolveResult{}, e
	}
	defer mgr.Close()
	req := engine.Request{Model: in.Spec.Model, Prompt: in.Prompt, Instructions: defaultInstructions + "\n" + in.Revision.Content.Instructions + "\n" + in.Revision.Content.ToolGuidance + "\n" + strings.Join(in.Revision.Content.Lessons, "\n") + "\nUse readOnly:true for the prescribed verification command. Add a new regression test file; preserve the original public tests and README.", MaxSteps: number(in.Spec.Agent, "maxSteps", 24), ContextSize: in.Spec.ContextSize, Timeout: time.Duration(in.Spec.BudgetSeconds) * time.Second, ContextPolicy: &engine.ContextPolicy{Enabled: true}, CapabilityID: f.Digest}
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
	emitEngine := func(event engine.Event) {
		if emit != nil && event.Type != "adk_event" && event.Type != "thinking" {
			emit(harness.Event{Type: event.Type, Title: event.Title, Detail: event.Detail, Data: event.Data})
		}
	}
	var execute engine.ToolExecutor
	execute = func(ctx context.Context, call engine.ToolCall) (engine.ToolResult, error) {
		if !allowed[call.Name] {
			return engine.ToolResult{IsError: true}, errors.New("tool outside experiment allowlist")
		}
		selected, ok := agents[call.CapabilityID]
		if !ok {
			return engine.ToolResult{IsError: true}, errors.New("unknown frozen capability")
		}
		if _, err := selected.Authorize(ctx, call); err != nil {
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
			value, runErr := mgr.Execute(ctx, call.Name, call.Arguments)
			for _, artifact := range value.Artifacts {
				captured = append(captured, harness.Doc{"path": artifact.Path, "mediaType": artifact.MediaType, "size": artifact.Size, "beforeHash": workspace.Hash([]byte(artifact.Before)), "afterHash": workspace.Hash([]byte(artifact.After))})
			}
			r = engine.ToolResult{Content: value.Content, Data: obj(value.Data), IsError: runErr != nil}
			err = runErr
		}
		mu.Lock()
		sequence++
		entry := harness.ToolObservation{CallID: call.ID, Tool: call.Name, Arguments: call.Arguments, Content: r.Content, Data: obj(r.Data), Sequence: sequence, StartSequence: startedSequence, Artifacts: captured}
		entry = boundHarnessObservation(entry)
		if err != nil {
			entry.Error = err.Error()
		}
		trace = append(trace, entry)
		mu.Unlock()
		return engine.ToolResult{Content: r.Content, Data: obj(r.Data), IsError: err != nil}, err
	}
	var result engine.Result
	eng := engine.New(c.Client())
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
