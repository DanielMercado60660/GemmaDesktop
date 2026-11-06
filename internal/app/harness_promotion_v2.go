package app

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
	"sort"
	"time"
)

// This fingerprint excludes personal presentation/model preferences. All
// authority-bearing selections and their exact imported bytes remain bound.
func harnessAuthorityHash(f FrozenAgent) string {
	sorted := func(in []string) []string { out := append([]string{}, in...); sort.Strings(out); return out }
	skills := Doc{}
	for _, skill := range f.Skills {
		skills[skill.ID] = harness.Hash(skill)
	}
	grants := []string{}
	for _, grant := range f.MCPGrants {
		grants = append(grants, harness.Hash(grant))
	}
	sort.Strings(grants)
	return harness.Hash(Doc{"profileId": f.Profile.ID, "version": f.Profile.Version, "tools": sorted(f.Profile.Tools), "skillIds": sorted(f.Profile.SkillIDs), "skills": skills, "mcp": f.Profile.MCP, "mcpGrants": grants, "unavailableMcp": sorted(f.UnavailableMCP), "delegates": sorted(f.Profile.DelegateAgentIDs), "executionProfile": f.Profile.ExecutionProfile, "executionImage": f.ExecutionImage, "privatePaths": sorted(f.ExecutionPrivatePaths), "projectGuidance": f.Profile.ProjectGuidance})
}

func bindingSelectionV2(binding Doc) Doc {
	out := clone(binding)
	if out == nil {
		out = Doc{}
	}
	delete(out, "promotionsV2")
	return out
}
func (c *Core) currentHarnessBinding(workspaceID, agentID string) (Doc, error) {
	b, err := c.Store.Get("harness_binding", harness.Hash([]string{workspaceID, agentID}))
	if errors.Is(err, store.ErrNotFound) {
		return Doc{}, nil
	}
	return b, err
}
func bindingHistoryV2(binding Doc) map[string]Doc {
	out := map[string]Doc{}
	b, _ := json.Marshal(binding["promotionsV2"])
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]Doc{}
	}
	return out
}

func (c *Core) validateQualifiedAuthority(arm harness.ArmV2, agent Doc) error {
	if arm.Spec.WorkspaceID == "" || arm.Spec.AgentID != str(agent, "id") || arm.Spec.AuthorityHash == "" {
		return errors.New("qualified profile has no matching authority snapshot")
	}
	current, err := c.FreezeAgent(agent)
	if err != nil {
		return err
	}
	if harnessAuthorityHash(current) != arm.Spec.AuthorityHash {
		return errors.New("permissions, skills, MCP grants or executor changed since qualification")
	}
	// The qualification froze delegates/workflow agents as well as the primary.
	for id, raw := range arm.Spec.FrozenAgents {
		if id == arm.Spec.AgentID {
			continue
		}
		var expected FrozenAgent
		if err = json.Unmarshal(raw, &expected); err != nil {
			return err
		}
		child, err := c.Store.Get("agent", id)
		if err != nil {
			return err
		}
		frozen, err := c.FreezeAgent(child)
		if err != nil {
			return err
		}
		if harnessAuthorityHash(expected) != harnessAuthorityHash(frozen) {
			return errors.New("a frozen delegate or workflow agent changed authority")
		}
	}
	return nil
}

// Called before ordinary model selection and capability freezing. Only the
// admitted execution fields overlay the personal agent document in memory.
func (c *Core) applyQualifiedProfile(workspaceID string, agent Doc) (Doc, int, Doc, error) {
	binding, err := c.currentHarnessBinding(workspaceID, str(agent, "id"))
	if err != nil {
		return nil, 0, nil, err
	}
	armID := str(binding, "armId")
	if armID == "" {
		return agent, 0, nil, nil
	}
	if c.harness == nil {
		return nil, 0, nil, errors.New("qualified profile service unavailable")
	}
	arm, err := c.harness.GetArmV2(armID)
	if err != nil {
		return nil, 0, nil, err
	}
	if arm.Spec.WorkspaceID != workspaceID || arm.Revision.ID != str(binding, "revisionId") {
		return nil, 0, nil, errors.New("qualified profile binding identity mismatch")
	}
	receipt := bindingHistoryV2(binding)[str(binding, "promotionId")]
	if receipt == nil || str(receipt, "armId") != arm.ID || str(receipt, "status") == "rolled_back" {
		return nil, 0, nil, errors.New("qualified profile lacks an active promotion receipt")
	}
	if err = c.validateQualifiedAuthority(arm, agent); err != nil {
		return nil, 0, nil, err
	}
	effective := clone(agent)
	effective["qualifiedProfileBindingHash"] = harness.Hash(bindingSelectionV2(binding))
	effective["model"] = arm.Spec.Model
	effective["runtimeId"] = str(arm.Spec.RuntimeIdentity, "runtimeId")
	for _, key := range []string{"thinking", "temperature", "maxOutputTokens", "maxSteps", "toolProtocol", "harnessMechanisms", "verificationFeedback"} {
		if value, ok := arm.Spec.Agent[key]; ok {
			effective[key] = value
		} else {
			delete(effective, key)
		}
	}
	return effective, arm.Spec.ContextSize, Doc{"binding": binding, "arm": obj(arm)}, nil
}

func (c *Core) promoteHarnessV2(ctx context.Context, id string) (Doc, error) {
	release, guardErr := c.guardProfileActivationV2()
	if guardErr != nil {
		return nil, guardErr
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	arm, err := c.harness.QualifiedArmV2(id)
	if err != nil {
		return nil, err
	}
	run, err := c.harness.GetRunV2(id)
	if err != nil {
		return nil, err
	}
	baseline, err := c.harness.GetArmV2(run.ArmIDs[0])
	if err != nil {
		return nil, err
	}
	if baseline.Spec.ProfileBindingHash == "" || baseline.Spec.ProfileBindingHash != arm.Spec.ProfileBindingHash {
		return nil, errors.New("qualification arms do not bind the same frozen current product")
	}
	agent, err := c.Store.Get("agent", arm.Spec.AgentID)
	if err != nil {
		return nil, err
	}
	if _, err = c.Store.Get("workspace", arm.Spec.WorkspaceID); err != nil {
		return nil, err
	}
	if err = c.validateQualifiedAuthority(arm, agent); err != nil {
		return nil, err
	}
	key := harness.Hash([]string{arm.Spec.WorkspaceID, arm.Spec.AgentID})
	promotionID := harness.Hash([]string{"profile-promotion-v2", id, arm.ID})[:32]
	now := time.Now().UTC().Format(time.RFC3339Nano)
	binding, err := c.Store.Mutate("harness_binding", key, func(old Doc) (Doc, error) {
		history := bindingHistoryV2(old)
		if history[promotionID] != nil {
			return nil, nil
		}
		if len(history) >= 256 {
			return nil, errors.New("profile promotion history reached its bound")
		}
		previous := bindingSelectionV2(old)
		if harness.Hash(previous) != baseline.Spec.ProfileBindingHash {
			return nil, errors.New("active profile changed after qualification admission")
		}
		next := Doc{"id": key, "workspaceId": arm.Spec.WorkspaceID, "agentId": arm.Spec.AgentID, "revisionId": arm.Revision.ID, "armId": arm.ID, "promotionId": promotionID, "updatedAt": now}
		receipt := Doc{"id": promotionID, "evaluationId": id, "workspaceId": arm.Spec.WorkspaceID, "agentId": arm.Spec.AgentID, "armId": arm.ID, "revisionId": arm.Revision.ID, "previousBinding": previous, "activeBindingHash": harness.Hash(next), "status": "applied", "createdAt": now}
		history[promotionID] = receipt
		next["promotionsV2"] = history
		return next, nil
	})
	if err != nil {
		return nil, err
	}
	return bindingHistoryV2(binding)[promotionID], nil
}

func (c *Core) listHarnessPromotionsV2() ([]Doc, error) {
	bindings, err := c.Store.List("harness_binding")
	if err != nil {
		return nil, err
	}
	out := []Doc{}
	for _, binding := range bindings {
		for _, p := range bindingHistoryV2(binding) {
			p["active"] = str(binding, "promotionId") == str(p, "id") && str(binding, "armId") == str(p, "armId")
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return str(out[i], "createdAt") < str(out[j], "createdAt") })
	return out, nil
}
func (c *Core) getHarnessPromotionV2(id string) (Doc, error) {
	all, err := c.listHarnessPromotionsV2()
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if str(p, "id") == id {
			return p, nil
		}
	}
	return nil, store.ErrNotFound
}
func (c *Core) rollbackHarnessV2(ctx context.Context, id string) (Doc, error) {
	release, guardErr := c.guardProfileActivationV2()
	if guardErr != nil {
		return nil, guardErr
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	receipt, err := c.getHarnessPromotionV2(id)
	if err != nil {
		return nil, err
	}
	key := harness.Hash([]string{str(receipt, "workspaceId"), str(receipt, "agentId")})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := c.Store.Mutate("harness_binding", key, func(old Doc) (Doc, error) {
		history := bindingHistoryV2(old)
		current := history[id]
		if current == nil {
			return nil, errors.New("promotion receipt changed")
		}
		if str(current, "status") == "rolled_back" {
			return nil, nil
		}
		if str(old, "promotionId") != id || harness.Hash(bindingSelectionV2(old)) != str(current, "activeBindingHash") {
			return nil, errors.New("another profile change superseded this promotion")
		}
		previous := Doc{}
		b, _ := json.Marshal(current["previousBinding"])
		if err := json.Unmarshal(b, &previous); err != nil {
			return nil, err
		}
		if len(previous) == 0 {
			previous = Doc{"id": key, "workspaceId": current["workspaceId"], "agentId": current["agentId"], "revisionId": ""}
		}
		current["status"] = "rolled_back"
		current["rolledBackAt"] = now
		history[id] = current
		previous["promotionsV2"] = history
		return previous, nil
	})
	if err != nil {
		return nil, err
	}
	return bindingHistoryV2(result)[id], nil
}

func (c *Core) guardProfileActivationV2() (func(), error) {
	c.runtimeGuard.Lock()
	c.videoGuard.Lock()
	release := func() { c.videoGuard.Unlock(); c.runtimeGuard.Unlock() }
	if c.videoBusy() {
		release()
		return nil, errors.New("wait for active video work before changing the qualified profile")
	}
	c.mu.Lock()
	busy := len(c.runs) > 0 || len(c.evals) > 0 || len(c.pulls) > 0 || c.harnessBusy()
	c.mu.Unlock()
	if busy {
		release()
		return nil, errors.New("wait for active runs, comparisons, downloads and Harness work before changing the qualified profile")
	}
	return release, nil
}

func (c *Core) qualifiedRunAgentsV2(workspaceID string, agent Doc, arm harness.ArmV2) (map[string]FrozenAgent, error) {
	primary, err := c.FreezeAgent(agent)
	if err != nil {
		return nil, err
	}
	req := engine.Request{Instructions: agentInstructions(agent)}
	primary.Configure(&req)
	binding, err := c.applyHarnessBinding(workspaceID, agent, &req, &primary)
	if err != nil {
		return nil, err
	}
	primary.HarnessBinding = binding
	primary.HarnessInstructions = req.Instructions
	primary.Skills = req.SkillPackages
	_, agents, err := harnessSelectedAgents(arm.Spec, arm.Revision.Content, primary)
	if err != nil {
		return nil, err
	}
	// The shared qualification solver dispatches every selected child through
	// the arm's backend. Preserve that exact selection in the normal-run snapshot.
	resolved := map[string]FrozenAgent{}
	for _, f := range agents {
		f.Agent = clone(f.Agent)
		f.Agent["runtimeId"] = str(arm.Spec.RuntimeIdentity, "runtimeId")
		f.Digest = ""
		f.Digest = contentDigest(f)
		resolved[f.Digest] = f
	}
	return resolved, nil
}

func (c *Core) qualifiedProfileSummariesV2() ([]Doc, error) {
	bindings, err := c.Store.List("harness_binding")
	if err != nil {
		return nil, err
	}
	out := []Doc{}
	for _, binding := range bindings {
		if str(binding, "armId") == "" {
			continue
		}
		row := Doc{"workspaceId": binding["workspaceId"], "agentId": binding["agentId"], "armId": binding["armId"], "revisionId": binding["revisionId"], "promotionId": binding["promotionId"]}
		if c.harness == nil {
			row["error"] = "profile service unavailable"
			out = append(out, row)
			continue
		}
		arm, err := c.harness.GetArmV2(str(binding, "armId"))
		if err != nil {
			row["error"] = err.Error()
			out = append(out, row)
			continue
		}
		row["name"] = arm.Name
		row["model"] = arm.Spec.Model
		row["runtimeId"] = arm.Spec.RuntimeIdentity["runtimeId"]
		row["contextSize"] = arm.Spec.ContextSize
		row["generation"] = Doc{"thinking": arm.Spec.Agent["thinking"], "temperature": arm.Spec.Agent["temperature"], "maxOutputTokens": arm.Spec.Agent["maxOutputTokens"], "maxSteps": arm.Spec.Agent["maxSteps"], "toolProtocol": arm.Spec.Agent["toolProtocol"], "harnessMechanisms": arm.Spec.Agent["harnessMechanisms"], "verificationFeedback": arm.Spec.Agent["verificationFeedback"]}
		row["status"] = "configured" // Authority/runtime are revalidated at admission.
		out = append(out, row)
	}
	return out, nil
}
