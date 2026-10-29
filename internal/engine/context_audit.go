package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"gemmadesktop.local/app/internal/inference"
	"unicode/utf8"
)

// Audit metadata describes the actual renderer output without copying source,
// reasoning or credentials into another journal. Hashes are evidence identities,
// not permission grants or a claim that the runtime processed every byte.
func generationContextAudit(chat inference.ChatRequest, step, attempt int, summary bool) map[string]any {
	chat.Stream = true
	chat.Truncate = false
	chat.Shift = false // Client.Chat enforces these.
	raw, _ := json.Marshal(chat)
	digest := sha256.Sum256(raw)
	roles := map[string]int{}
	characters, bytes, images, calls := 0, 0, 0, 0
	for _, m := range chat.Messages {
		roles[m.Role]++
		characters += utf8.RuneCountInString(m.Content) + utf8.RuneCountInString(m.Thinking)
		bytes += len(m.Content) + len(m.Thinking)
		images += len(m.Images)
		calls += len(m.ToolCalls)
	}
	declarations, _ := json.Marshal(chat.Tools)
	schema := sha256.Sum256(declarations)
	names := []string{}
	for _, t := range chat.Tools {
		names = append(names, t.Function.Name)
	}
	out := map[string]any{"phase": "runtime_generation", "step": step, "attempt": attempt, "summary": summary, "model": chat.Model, "requestSHA256": hex.EncodeToString(digest[:]), "requestBytes": len(raw), "messages": len(chat.Messages), "roles": roles, "messageTextBytes": bytes, "estimatedTextTokens": (characters + 3) / 4, "estimateMethod": "Unicode text characters / 4; excludes image tokens and tool-schema tokenization, not measured usage", "images": images, "historicalToolCalls": calls, "toolNames": names, "toolDeclarationsSHA256": hex.EncodeToString(schema[:]), "toolDeclarationBytes": len(declarations), "contextSize": chat.Options["num_ctx"], "maxOutputTokens": chat.Options["num_predict"], "truncate": false, "shift": false}
	format, _ := json.Marshal(chat.Format)
	formatHash := sha256.Sum256(format)
	out["formatSHA256"] = hex.EncodeToString(formatHash[:])
	out["formatBytes"] = len(format)
	if len(declarations) <= 12<<10 {
		out["toolDeclarations"] = chat.Tools
	} else {
		out["toolDeclarationOmission"] = "Inspector omits declarations above 12 KiB; request digest and byte count retained. Model input was not truncated."
	}
	if chat.Format != nil && len(format) <= 12<<10 {
		out["format"] = chat.Format
	} else if len(format) > 12<<10 {
		out["formatOmission"] = "Inspector omits format schema above 12 KiB; model input was not truncated."
	}
	return out
}
