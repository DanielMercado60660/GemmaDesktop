package app

import (
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
	"strings"
)

func (c *Core) applyHarnessBinding(workspaceID string, agent Doc, req *engine.Request, frozen *FrozenAgent) (Doc, error) {
	id := harness.Hash([]string{workspaceID, str(agent, "id")})
	binding, e := c.Store.Get("harness_binding", id)
	if errors.Is(e, store.ErrNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if c.harness == nil {
		return nil, errors.New("active harness revision cannot be resolved")
	}
	d, e := c.harness.Get("revision", str(binding, "revisionId"))
	if e != nil {
		return nil, e
	}
	var revision harness.Revision
	b, _ := json.Marshal(d)
	if e = json.Unmarshal(b, &revision); e != nil {
		return nil, e
	}
	if revision.WorkspaceID != workspaceID || revision.AgentID != str(agent, "id") {
		return nil, errors.New("active harness revision targets another profile")
	}
	current := map[string]engine.SkillPackage{}
	for _, p := range frozen.Skills {
		current[p.ID] = p
	}
	selected := []engine.SkillPackage{}
	for _, id := range revision.Content.SkillIDs {
		p, ok := current[id]
		if !ok {
			return nil, errors.New("promoted harness references a skill no longer approved with the same digest")
		}
		selected = append(selected, p)
	}
	original := defaultInstructions + "\n\n" + str(agent, "instructions")
	replacement := defaultInstructions + "\n\n" + revision.Content.Instructions + "\n" + revision.Content.ToolGuidance + "\n" + strings.Join(revision.Content.Lessons, "\n")
	if strings.HasPrefix(req.Instructions, original) {
		req.Instructions = replacement + strings.TrimPrefix(req.Instructions, original)
	} else if req.Instructions == str(agent, "instructions") {
		req.Instructions = replacement
	} else {
		return nil, errors.New("cannot safely compose harness with the current instruction layout")
	}
	for _, id := range revision.Content.DelegateAgentIDs {
		if !contains(frozen.Profile.DelegateAgentIDs, id) {
			return nil, errors.New("promoted delegate is no longer authorized")
		}
	}
	frozen.Profile.DelegateAgentIDs = append([]string{}, revision.Content.DelegateAgentIDs...)
	req.SkillPackages = selected
	req.Skills = nil
	return Doc{"binding": binding, "revision": d, "selectedSkillDigests": revision.Content.SkillIDs}, nil
}

// An explicit user workflow is resolved by admission before consulting this
// default. The returned definition is immutable promoted evidence, not a lookup
// of a mutable graph with the same ID.
func (c *Core) harnessSelectedWorkflow(workspaceID, agentID string) (Doc, error) {
	binding, e := c.Store.Get("harness_binding", harness.Hash([]string{workspaceID, agentID}))
	if errors.Is(e, store.ErrNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	raw, e := c.harness.Get("revision", str(binding, "revisionId"))
	if e != nil {
		return nil, e
	}
	b, _ := json.Marshal(raw)
	var r harness.Revision
	if e = json.Unmarshal(b, &r); e != nil {
		return nil, e
	}
	if r.WorkspaceID != workspaceID || r.AgentID != agentID {
		return nil, errors.New("promoted workflow target mismatch")
	}
	if r.Content.WorkflowID == "" {
		return nil, nil
	}
	var d Doc
	if e = json.Unmarshal(r.ApprovedWorkflow, &d); e != nil {
		return nil, e
	}
	if str(d, "id") != r.Content.WorkflowID {
		return nil, errors.New("promoted workflow snapshot identity mismatch")
	}
	return d, nil
}
