package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/mcp"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/tools"
	"gemmadesktop.local/app/internal/workspace"
)

type MCPSelector struct {
	ServerID string   `json:"serverId"`
	Tools    []string `json:"tools"`
}
type CapabilityProfile struct {
	Version          int           `json:"version"`
	Revision         int           `json:"revision"`
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Tools            []string      `json:"tools"`
	SkillIDs         []string      `json:"skillIds"`
	MCP              []MCPSelector `json:"mcp"`
	DelegateAgentIDs []string      `json:"delegateAgentIds"`
	ExecutionProfile string        `json:"executionProfile"`
	PrivatePaths     []string      `json:"privatePaths,omitempty"`
	ProjectGuidance  bool          `json:"projectGuidance"`
}
type FrozenAgent struct {
	Agent                 Doc                   `json:"agent"`
	Profile               CapabilityProfile     `json:"profile"`
	Skills                []engine.SkillPackage `json:"skills"`
	MCPGrants             []mcp.Grant           `json:"mcpGrants"`
	Digest                string                `json:"digest"`
	ExecutionPrivatePaths []string              `json:"executionPrivatePaths,omitempty"`
	ExecutionImage        string                `json:"executionImage,omitempty"`
	UnavailableMCP        []string              `json:"unavailableMcp,omitempty"`
	HarnessBinding        Doc                   `json:"harnessBinding,omitempty"`
	HarnessInstructions   string                `json:"harnessInstructions,omitempty"`
}

func contentDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
func knownTools() map[string]bool {
	out := map[string]bool{"plan_update": true, "delegate_readonly": true, "skill_resources": true, "skill_materialize": true}
	for _, d := range tools.Catalogue() {
		out[d.Name] = true
	}
	return out
}
func validateProfile(p CapabilityProfile) error {
	if p.Version != 1 || p.Name == "" || len(p.Name) > 200 {
		return errors.New("capability version 1 and name required")
	}
	if p.ExecutionProfile != "restricted" && p.ExecutionProfile != "docker" {
		return errors.New("executionProfile must be restricted or docker")
	}
	if len(p.Tools) > 100 || len(p.SkillIDs) > 32 || len(p.MCP) > 32 || len(p.DelegateAgentIDs) > 16 {
		return errors.New("capability selection exceeds bounds")
	}
	if err := workspace.ValidatePrivatePaths(append(append([]string{}, workspace.DefaultPrivatePaths...), p.PrivatePaths...)); err != nil {
		return err
	}
	for _, selected := range [][]string{p.SkillIDs, p.DelegateAgentIDs} {
		seen := map[string]bool{}
		for _, id := range selected {
			if id == "" || seen[id] {
				return errors.New("selected IDs must be nonempty and unique")
			}
			seen[id] = true
		}
	}
	seen := map[string]bool{}
	for _, name := range p.Tools {
		if !knownTools()[name] || seen[name] {
			return fmt.Errorf("unknown or duplicate tool %q", name)
		}
		seen[name] = true
	}
	seen = map[string]bool{}
	for _, s := range p.MCP {
		if s.ServerID == "" || seen[s.ServerID] || len(s.Tools) == 0 || len(s.Tools) > 256 {
			return errors.New("MCP selectors require unique servers and 1–256 exact tool names")
		}
		seen[s.ServerID] = true
		for _, n := range s.Tools {
			if n == "" || strings.Contains(n, "*") {
				return errors.New("MCP selectors must name exact tools")
			}
		}
	}
	return nil
}

// initCapabilities is idempotent. Legacy agents snapshot the existing selection;
// subsequently importing a package never silently grants it to every profile.
func (c *Core) initCapabilities() error {
	if _, err := c.Store.Get("capability", "legacy"); errors.Is(err, store.ErrNotFound) {
		p := CapabilityProfile{Version: 1, Revision: 1, ID: "legacy", Name: "Existing workspace capabilities", ExecutionProfile: "restricted", Tools: []string{}, SkillIDs: []string{}, MCP: []MCPSelector{}, DelegateAgentIDs: []string{}}
		for _, d := range tools.Catalogue() {
			p.Tools = append(p.Tools, d.Name)
		}
		p.Tools = append(p.Tools, "plan_update")
		skills, e := c.Store.List("skill")
		if e != nil {
			return e
		}
		for _, s := range skills {
			if s["archived"] != true {
				p.SkillIDs = append(p.SkillIDs, str(s, "id"))
			}
		}
		servers, e := c.Store.List("mcp")
		if e != nil {
			return e
		}
		for _, server := range servers {
			raw, _ := json.Marshal(server["tools"])
			var ts []mcp.Tool
			_ = json.Unmarshal(raw, &ts)
			names := []string{}
			for _, t := range ts {
				names = append(names, t.Name)
			}
			if len(names) > 0 {
				p.MCP = append(p.MCP, MCPSelector{ServerID: str(server, "id"), Tools: names})
			}
		}
		if _, e = c.Store.PublishVersion("capability", p.ID, 0, obj(p)); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	delegations, err := c.Store.List("delegation")
	if err != nil {
		return err
	}
	for _, d := range delegations {
		if d["status"] == "running" {
			d["status"] = "interrupted"
			d["error"] = "Application stopped; delegate observations are retained without execution replay"
			if err = c.Store.Put("delegation", d); err != nil {
				return err
			}
		}
	}
	agents, e := c.Store.List("agent")
	if e != nil {
		return e
	}
	for _, a := range agents {
		if str(a, "capabilityProfileId") == "" {
			a["capabilityProfileId"] = "legacy"
			if e = c.Store.Put("agent", a); e != nil {
				return e
			}
		}
	}
	return nil
}
func (c *Core) prepareAgentCapability(p Doc) error {
	id := str(p, "capabilityProfileId")
	if id == "" {
		id = "legacy"
		p["capabilityProfileId"] = id
	}
	_, e := c.Store.Get("capability", id)
	if id == "legacy" && errors.Is(e, store.ErrNotFound) {
		return c.initCapabilities()
	}
	return e
}
func (c *Core) FreezeAgent(agent Doc) (FrozenAgent, error) {
	var f FrozenAgent
	f.Agent = clone(agent)
	if _, err := agentGenerationControls(f.Agent); err != nil {
		return f, err
	}
	id := str(agent, "capabilityProfileId")
	if id == "" {
		id = "legacy"
	}
	d, e := c.Store.Get("capability", id)
	if e != nil {
		return f, e
	}
	b, _ := json.Marshal(d)
	if e = json.Unmarshal(b, &f.Profile); e != nil {
		return f, e
	}
	if e = validateProfile(f.Profile); e != nil {
		return f, e
	}
	for _, id := range f.Profile.SkillIDs {
		s, e := c.Store.Get("skill", id)
		if e != nil {
			return f, e
		}
		if s["archived"] == true {
			return f, errors.New("selected skill is archived; update the capability profile")
		}
		p, e := readImportedSkill(s)
		if e != nil {
			return f, e
		}
		if str(s, "digest") == "" {
			if _, e = c.Store.Update("skill", id, func(current Doc) error {
				if existing := str(current, "digest"); existing != "" && existing != p.Digest {
					return errors.New("legacy skill changed during immutable migration")
				}
				current["digest"] = p.Digest
				current["version"] = 1
				return nil
			}); e != nil {
				return f, e
			}
		}
		if e = c.registerSkillArtifacts(p); e != nil {
			return f, e
		}
		f.Skills = append(f.Skills, p)
	}
	if e = engine.ValidateSkillPackages(f.Skills); e != nil {
		return f, e
	}
	f.ExecutionPrivatePaths = append(append([]string{}, workspace.DefaultPrivatePaths...), f.Profile.PrivatePaths...)
	for _, selector := range f.Profile.MCP {
		found := false
		var servers []mcp.Server
		if c.extension != nil {
			servers = c.extension.mcp.List()
		}
		for _, server := range servers {
			if server.ID != selector.ServerID {
				continue
			}
			found = true
			for _, name := range selector.Tools {
				matched := false
				for _, t := range server.Tools {
					if t.Name == name {
						f.MCPGrants = append(f.MCPGrants, mcp.Grant{ServerID: server.ID, Tool: name, ConfigHash: server.ConfigHash, SchemaHash: t.SchemaHash})
						matched = true
						break
					}
				}
				if !matched {
					f.UnavailableMCP = append(f.UnavailableMCP, selector.ServerID+":"+name)
				}
			}
		}
		if !found {
			f.UnavailableMCP = append(f.UnavailableMCP, selector.ServerID+": disconnected")
		}
	}
	if f.Profile.ExecutionProfile == "docker" {
		f.ExecutionImage = str(c.Settings(), "dockerImage")
		if f.ExecutionImage == "" {
			return f, errors.New("configure a pinned Docker image before using this profile")
		}
	}
	f.Digest = contentDigest(f)
	return f, nil
}
func readImportedSkill(s Doc) (engine.SkillPackage, error) {
	p, e := engine.SnapshotSkill(filepath.Join(str(s, "path"), "SKILL.md"))
	if e != nil {
		return p, e
	}
	if d := str(s, "digest"); d != "" && d != p.Digest {
		return p, errors.New("imported skill content changed; re-import explicitly")
	}
	if id := str(s, "id"); id != "" {
		p.ID = id
	}
	return p, nil
}
func (f FrozenAgent) Configure(req *engine.Request) {
	req.CapabilityID = f.Digest
	req.Skills = nil
	req.SkillPackages = f.Skills
	if f.HarnessInstructions != "" {
		original := defaultInstructions + "\n\n" + str(f.Agent, "instructions")
		if strings.HasPrefix(req.Instructions, original) {
			req.Instructions = f.HarnessInstructions + strings.TrimPrefix(req.Instructions, original)
		}
	}
}
func (f FrozenAgent) FilterTools(defs []engine.ToolDefinition) []engine.ToolDefinition {
	out := []engine.ToolDefinition{}
	for _, d := range defs {
		if contains(f.Profile.Tools, d.Name) {
			out = append(out, d)
		}
	}
	return out
}
func (f FrozenAgent) Authorize(ctx context.Context, call engine.ToolCall) (context.Context, error) {
	if call.CapabilityID != f.Digest || !contains(f.Profile.Tools, call.Name) {
		return ctx, errors.New("tool is outside the frozen agent capability profile")
	}
	if call.Name == "mcp_call" {
		server, _ := call.Arguments["serverId"].(string)
		name, _ := call.Arguments["tool"].(string)
		for _, g := range f.MCPGrants {
			if g.ServerID == server && g.Tool == name {
				return mcp.WithGrant(ctx, g), nil
			}
		}
		return ctx, errors.New("MCP server/tool is outside the frozen selectors")
	}
	return ctx, nil
}
func (c *Core) saveCapability(p Doc) (any, error) {
	var profile CapabilityProfile
	b, _ := json.Marshal(p)
	if e := json.Unmarshal(b, &profile); e != nil {
		return nil, e
	}
	if profile.Version == 0 {
		profile.Version = 1
	}
	if profile.ExecutionProfile == "" {
		profile.ExecutionProfile = "restricted"
	}
	if e := validateProfile(profile); e != nil {
		return nil, e
	}
	normalizeCapabilityLists(&profile)
	if profile.ID == "" {
		profile.ID = store.ID()
	}
	for _, id := range profile.SkillIDs {
		if _, e := c.Store.Get("skill", id); e != nil {
			return nil, e
		}
	}
	for _, id := range profile.DelegateAgentIDs {
		if _, e := c.Store.Get("agent", id); e != nil {
			return nil, e
		}
	}
	sort.Strings(profile.Tools)
	return c.Store.PublishVersion("capability", profile.ID, profile.Revision, obj(profile))
}

func normalizeCapabilityLists(p *CapabilityProfile) {
	if p.Tools == nil {
		p.Tools = []string{}
	}
	if p.SkillIDs == nil {
		p.SkillIDs = []string{}
	}
	if p.MCP == nil {
		p.MCP = []MCPSelector{}
	}
	if p.DelegateAgentIDs == nil {
		p.DelegateAgentIDs = []string{}
	}
}

// Normalize the public representation without rewriting immutable historical
// revisions or changing a frozen run's capability identity.
func (c *Core) listCapabilities() ([]Doc, error) {
	rows, err := c.Store.List("capability")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var p CapabilityProfile
		b, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		normalizeCapabilityLists(&p)
		row["tools"], row["skillIds"], row["mcp"], row["delegateAgentIds"] = p.Tools, p.SkillIDs, p.MCP, p.DelegateAgentIDs
	}
	return rows, nil
}
