package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/workspace"
)

func delegateTool() engine.ToolDefinition {
	return engine.ToolDefinition{Name: "delegate_readonly", Description: "Ask one configured read-only specialist a bounded research/review question. It cannot write files, run commands, call MCP/browser, or delegate. Historical results are observations; this does not grant permissions.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"agentId": map[string]any{"type": "string"}, "task": map[string]any{"type": "string", "minLength": 1, "maxLength": 12000}}, "required": []string{"agentId", "task"}, "additionalProperties": false}}
}
func readonlyTool(name string) bool {
	switch name {
	case "list_files", "read_file", "search_files", "git_status", "git_diff", "git_history", "git_branches", "git_file", "git_blame":
		return true
	}
	return false
}
func (c *Core) freezeRunAgents(primary Doc, wf Doc, workspaceIDs ...string) (map[string]FrozenAgent, error) {
	selected := map[string]Doc{str(primary, "id"): primary}
	if wf != nil {
		raw, _ := json.Marshal(wf["nodes"])
		var nodes []Doc
		if e := json.Unmarshal(raw, &nodes); e != nil {
			return nil, e
		}
		for _, n := range nodes {
			if id := str(n, "agentId"); id != "" {
				a, e := c.Store.Get("agent", id)
				if e != nil {
					return nil, e
				}
				selected[id] = a
			}
		}
	}
	out := map[string]FrozenAgent{}
	for _, a := range selected {
		f, e := c.FreezeAgent(a)
		if e != nil {
			return nil, e
		}
		out[f.Digest] = f
	}
	parents := []FrozenAgent{}
	for _, f := range out {
		parents = append(parents, f)
	}
	for _, f := range parents {
		for _, id := range f.Profile.DelegateAgentIDs {
			a, e := c.Store.Get("agent", id)
			if e != nil {
				return nil, e
			}
			child, e := c.FreezeAgent(a)
			if e != nil {
				return nil, e
			}
			out[child.Digest] = child
		}
	}
	if len(workspaceIDs) > 0 {
		resolved := map[string]FrozenAgent{}
		for _, f := range out {
			req := engine.Request{Instructions: defaultInstructions + "\n\n" + str(f.Agent, "instructions")}
			f.Configure(&req)
			binding, e := c.applyHarnessBinding(workspaceIDs[0], f.Agent, &req, &f)
			if e != nil {
				return nil, e
			}
			if binding != nil {
				f.HarnessBinding = binding
				f.HarnessInstructions = req.Instructions
				f.Skills = req.SkillPackages
				f.Digest = ""
				f.Digest = contentDigest(f)
			}
			resolved[f.Digest] = f
		}
		out = resolved
	}
	if len(out) > 48 {
		return nil, errors.New("too many resolved agents")
	}
	return out, nil
}
func decodeFrozenAgents(snapshot Doc) (map[string]FrozenAgent, error) {
	out := map[string]FrozenAgent{}
	b, e := json.Marshal(snapshot["capabilities"])
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, &out); e != nil {
		return nil, e
	}
	for key, f := range out {
		digest := f.Digest
		f.Digest = ""
		if key != digest || contentDigest(f) != digest {
			return nil, errors.New("frozen capability manifest is invalid")
		}
		if e = workspace.ValidatePrivatePaths(f.ExecutionPrivatePaths); e != nil {
			return nil, e
		}
		if e = validateProfile(f.Profile); e != nil {
			return nil, e
		}
		if e = engine.ValidateSkillPackages(f.Skills); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func (c *Core) delegateReadonly(ctx context.Context, runID string, call engine.ToolCall, parent FrozenAgent, agents map[string]FrozenAgent, base engine.Request, emit func(engine.Event), execute engine.ToolExecutor) (engine.ToolResult, error) {
	if call.NodeID != "" && len(call.NodeID) >= 9 && call.NodeID[:9] == "delegate_" {
		return engine.ToolResult{}, errors.New("recursive delegation is disabled")
	}
	id, _ := call.Arguments["agentId"].(string)
	task, _ := call.Arguments["task"].(string)
	if !contains(parent.Profile.DelegateAgentIDs, id) || task == "" || len(task) > 12000 {
		return engine.ToolResult{}, errors.New("delegation is outside the frozen profile")
	}
	var selected FrozenAgent
	for _, f := range agents {
		if str(f.Agent, "id") == id {
			selected = f
			break
		}
	}
	if selected.Digest == "" {
		return engine.ToolResult{}, errors.New("delegate is unavailable in frozen manifest")
	}
	key := runID + ":" + call.NodeID + ":" + call.ID
	if old, e := c.Store.Get("delegation", key); e == nil {
		return engine.ToolResult{}, fmt.Errorf("delegation %s already has status %v; start a new explicit task", key, old["status"])
	} else if !errors.Is(e, store.ErrNotFound) {
		return engine.ToolResult{}, e
	}
	d := Doc{"id": key, "runId": runID, "parentNodeId": call.NodeID, "agentId": id, "capabilityDigest": selected.Digest, "task": task, "status": "running", "createdAt": store.Now()}
	if e := c.Store.CreateOnce("delegation", key, d); e != nil {
		return engine.ToolResult{}, e
	}
	req := base
	req.Prompt = task
	req.Snapshot = nil
	req.History = nil
	req.Resume = nil
	req.ReadOnly = true
	req.NodeID = "delegate_" + store.ID()
	req.MaxSteps = min(number(selected.Agent, "maxSteps", 6), 8)
	req.MaxToolCalls = 16
	req.Instructions = defaultInstructions + "\n\n" + str(selected.Agent, "instructions")
	if model := str(selected.Agent, "model"); model != "" {
		req.Model = model
	}
	controls, e := agentGenerationControls(selected.Agent)
	if e != nil {
		return engine.ToolResult{}, e
	}
	controls.apply(&req)
	selected.Configure(&req)
	req.SkillPackages = nil
	for _, childSkill := range selected.Skills {
		for _, parentSkill := range parent.Skills {
			if childSkill.Digest == parentSkill.Digest {
				req.SkillPackages = append(req.SkillPackages, childSkill)
				break
			}
		}
	}
	defs := []engine.ToolDefinition{}
	for _, t := range base.Tools {
		if readonlyTool(t.Name) && contains(parent.Profile.Tools, t.Name) && contains(selected.Profile.Tools, t.Name) {
			defs = append(defs, t)
		}
	}
	req.Tools = defs
	emit(engine.Event{Type: "node_started", NodeID: req.NodeID, Title: "Read-only delegate", Data: Doc{"parentNodeId": call.NodeID, "delegationId": key}})
	result, e := engine.New(c.Client()).Run(ctx, req, emit, execute)
	engine.AccountNestedUsage(ctx, result.Usage, result.TokenAccounting)
	d["status"] = "completed"
	if e != nil {
		d["status"] = "failed"
		d["error"] = e.Error()
		if ctx.Err() != nil {
			d["status"] = "cancelled"
		}
	}
	d["finishedAt"] = store.Now()
	d["result"] = result
	if err := c.Store.Put("delegation", d); err != nil {
		return engine.ToolResult{}, err
	}
	kind := "node_completed"
	if e != nil {
		kind = "node_failed"
	}
	emit(engine.Event{Type: kind, NodeID: req.NodeID, Title: "Read-only delegate", Data: Doc{"parentNodeId": call.NodeID, "delegationId": key}})
	return engine.ToolResult{Content: result.Content, Data: Doc{"delegationId": key, "status": d["status"], "content": result.Content, "usage": result.Usage}, IsError: e != nil}, e
}
