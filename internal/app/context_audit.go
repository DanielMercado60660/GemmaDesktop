package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"unicode/utf8"
)

func contextDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func auditSource(kind, path, scope, trust string, raw []byte, show bool) engine.ContextSource {
	content := ""
	if show {
		content = string(raw)
	} else {
		content = fmt.Sprintf("Metadata only: %d bytes; full input is retained in the frozen request/checkpoint. SHA256 identifies those complete bytes.", len(raw))
	}
	return engine.ContextSource{Kind: kind, Path: path, SHA256: contextDigest(raw), Content: content, Scope: scope, TrustClass: trust}
}
func frozenContextAudit(manifest ContextManifest, req engine.Request, primary FrozenAgent) ContextManifest {
	manifest.Version = 2
	manifest.Notice = "Admission sources are immutable input evidence. Runtime generation records separately describe the rendered request after history filtering, compaction, skill loading and tool results; hashes do not grant authority. Runtime token counts are observations, not tokenizer estimates. The manifest digest identifies admission rows only."
	manifest.Sources = append(append([]engine.ContextSource{}, manifest.Sources...), auditSource("host_instructions", "host/default", "admission; included in effective instructions", "host_policy", []byte(defaultInstructions), true))
	manifest.Sources = append(manifest.Sources, auditSource("agent_instructions", str(primary.Agent, "id"), "admission; stored profile instructions before an optional Harness override", "agent_configuration", []byte(str(primary.Agent, "instructions")), true), auditSource("effective_instructions", "request/instructions", "admission; complete effective system instructions including host, Harness and recovery guidance", "host_and_agent_configuration", []byte(req.Instructions), true), auditSource("current_user", "request/prompt", "admission; native current user request", "user_input", []byte(req.Prompt), true))
	var history []byte
	if len(req.Snapshot) > 0 {
		history = req.Snapshot
		manifest.Sources = append(manifest.Sources, auditSource("restored_history", "request/snapshot", "admission; complete ADK observations and summaries; runtime filtering determines visible context", "mixed_historical_evidence", history, false))
	} else {
		history, _ = json.Marshal(req.History)
		manifest.Sources = append(manifest.Sources, auditSource("message_history", "request/history", "admission; fallback transcript; incomplete assistant messages were excluded", "mixed_historical_evidence", history, false))
	}
	skillMeta := []Doc{}
	for _, p := range req.SkillPackages {
		names := []string{}
		for n := range p.Files {
			names = append(names, n)
		}
		skillMeta = append(skillMeta, Doc{"id": p.ID, "name": p.Name, "description": p.Description, "digest": p.Digest, "files": len(names), "authority": "instruction/resource data only; on-demand loading is recorded separately"})
	}
	b, _ := json.Marshal(skillMeta)
	manifest.Sources = append(manifest.Sources, auditSource("selected_skills", "request/skillPackages", "admission; metadata only; complete immutable package bytes retained in frozen capability manifest", "untrusted_instruction_resources", b, true))
	policy, _ := json.Marshal(Doc{"contextSize": req.ContextSize, "contextPolicy": req.ContextPolicy, "deadline": req.Deadline, "maxSteps": req.MaxSteps, "maxOutputTokens": req.MaxOutputTokens, "toolProtocol": req.ToolProtocol, "runtimeTruncate": false, "runtimeShift": false, "originalUserArchiveByteLimit": 65536, "archiveLimitBehavior": "fail explicitly; never silently truncate"})
	manifest.Sources = append(manifest.Sources, auditSource("context_policy", "request/policy", "admission; limits are frozen, summaries/actual usage are runtime observations", "host_policy", policy, true))
	capMeta, _ := json.Marshal(Doc{"digest": primary.Digest, "profile": primary.Profile, "executionImage": primary.ExecutionImage, "executionPrivatePaths": primary.ExecutionPrivatePaths, "mcpGrants": primary.MCPGrants, "unavailableMcp": primary.UnavailableMCP})
	manifest.Sources = append(manifest.Sources, auditSource("capability_selection", "request/capabilities", "admission; credential-free authority manifest; actual final tool declarations are hashed per generation", "host_policy", capMeta, true))
	manifest.Bytes += len(req.Instructions) + len(req.Prompt) + len(history) + len(b)
	manifest.EstimatedTokens += (utf8.RuneCountInString(req.Instructions) + utf8.RuneCountInString(req.Prompt) + utf8.RuneCount(history) + utf8.RuneCount(b) + 3) / 4
	manifest.EstimateMethod = "heuristic admitted text/history characters / 4; restored history may be compacted; excludes binary image tokens and full on-demand skill resources; runtime generations provide separate counts"
	manifest.Digest = contentDigest(manifest.Sources)
	return manifest
}
func (c *Core) contextAuditView(runID string, d Doc) (any, error) {
	raw, _ := json.Marshal(d["contextManifest"])
	var manifest ContextManifest
	if e := json.Unmarshal(raw, &manifest); e != nil {
		return nil, e
	}
	audit, e := c.Store.ContextAudit(runID)
	if e != nil {
		return nil, e
	}
	out := obj(manifest)
	out["admissionDigest"] = manifest.Digest
	out["runtimeAudit"] = audit
	sources := append([]engine.ContextSource{}, manifest.Sources...)
	if declarations, ok := d["contextToolDeclarations"]; ok {
		b, _ := json.Marshal(declarations)
		sources = append(sources, auditSource("runtime_host_tool_catalogue", "checkpoint/contextToolDeclarations", "Prepared before dispatch; workflow profiles further narrow this catalogue. Per-generation hashes include final ADK skill/tool schemas.", "host_declarations", b, true))
	}
	for _, ev := range audit.Events {
		data := obj(ev["data"])
		b, _ := json.Marshal(data)
		sources = append(sources, auditSource("runtime_"+str(ev, "type"), fmt.Sprintf("event/%v node/%s", ev["seq"], str(ev, "nodeId")), "runtime observation; distinct from the immutable admission manifest", "runtime_metadata", b, true))
	}
	out["sources"] = sources
	omissions := append([]string{}, manifest.Omissions...)
	if audit.Truncated {
		omissions = append(omissions, "Runtime inspector shows a newest bounded tail of context/metric events (1024 events / 2 MiB); complete audit remains in the event journal.")
	}
	if audit.TruncatedToolResults > 0 {
		omissions = append(omissions, fmt.Sprintf("%d tool results explicitly reported bounded/truncated evidence; inspect the original tool activities for exact scope.", audit.TruncatedToolResults))
	}
	out["omissions"] = omissions
	return out, nil
}
