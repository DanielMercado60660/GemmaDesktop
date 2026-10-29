package app

import (
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/workflow"
)

// Only host-selected saved definitions and already-authorized delegates become
// candidate choices. Packets cannot create agents, graph code or permissions.
func (c *Core) freezeHarnessChoices(primary Doc, ids []string, workspaceID string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	if len(ids) > 8 {
		return nil, nil, errors.New("choose at most eight approved workflows")
	}
	workflows := map[string]json.RawMessage{}
	frozen := map[string]json.RawMessage{}
	all, e := c.freezeRunAgents(primary, nil, workspaceID)
	if e != nil {
		return nil, nil, e
	}
	for _, id := range ids {
		if _, ok := workflows[id]; ok {
			return nil, nil, errors.New("duplicate approved workflow")
		}
		d, e := c.Store.Get("workflow", id)
		if e != nil {
			return nil, nil, e
		}
		b, _ := json.Marshal(d)
		var def workflow.Definition
		if e = json.Unmarshal(b, &def); e != nil {
			return nil, nil, e
		}
		if e = workflow.Validate(def); e != nil {
			return nil, nil, e
		}
		for _, node := range def.Nodes {
			if node.Type == "approval" {
				return nil, nil, errors.New("automated harness workflows cannot contain approval nodes")
			}
		}
		agents, e := c.freezeRunAgents(primary, d, workspaceID)
		if e != nil {
			return nil, nil, e
		}
		for key, f := range agents {
			all[key] = f
		}
		workflows[id] = b
	}
	for _, f := range all {
		b, _ := json.Marshal(f)
		frozen[str(f.Agent, "id")] = b
	}
	return workflows, frozen, nil
}

func harnessSelectedAgents(spec harness.Spec, content harness.Content, parent FrozenAgent) (FrozenAgent, map[string]FrozenAgent, error) {
	if e := harness.ValidateContent(content, spec); e != nil {
		return parent, nil, e
	}
	agents := map[string]FrozenAgent{}
	for id, raw := range spec.FrozenAgents {
		if id == str(parent.Agent, "id") {
			continue
		}
		var f FrozenAgent
		if e := json.Unmarshal(raw, &f); e != nil {
			return parent, nil, e
		}
		digest := f.Digest
		f.Digest = ""
		if contentDigest(f) != digest || str(f.Agent, "id") != id {
			return parent, nil, errors.New("frozen agent digest mismatch")
		}
		// The campaign's model and generation controls are fixed across all arms.
		f.Agent = clone(f.Agent)
		for _, key := range []string{"thinking", "temperature", "maxOutputTokens", "toolProtocol"} {
			f.Agent[key] = spec.Agent[key]
		}
		f.Agent["model"] = spec.Model
		originalDelegates := f.Profile.DelegateAgentIDs
		f.Profile.DelegateAgentIDs = nil
		for _, child := range content.DelegateAgentIDs {
			if contains(parent.Profile.DelegateAgentIDs, child) && contains(originalDelegates, child) {
				f.Profile.DelegateAgentIDs = append(f.Profile.DelegateAgentIDs, child)
			}
		}
		f.Digest = contentDigest(f)
		agents[f.Digest] = f
	}
	parent.Profile.DelegateAgentIDs = append([]string{}, content.DelegateAgentIDs...)
	parent.Digest = ""
	parent.Digest = contentDigest(parent)
	agents[parent.Digest] = parent
	return parent, agents, nil
}

func harnessWorkflowResolver(req engine.Request, parent FrozenAgent, agents map[string]FrozenAgent) workflow.AgentResolver {
	return func(id string) (engine.Request, error) {
		for _, f := range agents {
			if str(f.Agent, "id") == id {
				r := req
				childInstructions := defaultInstructions + "\n\n" + str(f.Agent, "instructions")
				if f.HarnessInstructions != "" {
					childInstructions = f.HarnessInstructions
				}
				r.Instructions = childInstructions + "\n" + req.Instructions
				r.CapabilityID = f.Digest
				r.Tools = f.FilterTools(parent.FilterTools(req.Tools))
				r.SkillPackages = nil
				for _, skill := range req.SkillPackages {
					for _, approved := range f.Skills {
						if skill.Digest == approved.Digest {
							r.SkillPackages = append(r.SkillPackages, skill)
							break
						}
					}
				}
				return r, nil
			}
		}
		return engine.Request{}, errors.New("workflow agent is absent from frozen campaign")
	}
}
