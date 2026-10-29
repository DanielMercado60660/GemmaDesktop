package engine

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"
)

// ADK validates function arguments before invoking our host callback. Observe
// those rejected proposals without creating a host execution event or receipt.
// Returning nil leaves ADK's original response and full audit event untouched.
func observeToolValidation(schemas map[string]*jsonschema.Resolved, nodeID string, emit func(Event)) llmagent.OnToolErrorCallback {
	return func(ctx agent.Context, t tool.Tool, args map[string]any, runErr error) (map[string]any, error) {
		if emit == nil || runErr == nil || ctx.Err() != nil {
			return nil, nil
		}
		schema := schemas[t.Name()]
		if schema == nil {
			return nil, nil
		}
		// Match ADK's JSON-decoded validation input. Do not classify normal
		// callback/transport/budget failures as argument validation failures.
		encoded, err := json.Marshal(args)
		if err != nil {
			return nil, nil
		}
		var decoded any
		if json.Unmarshal(encoded, &decoded) != nil {
			return nil, nil
		}
		if decoded == nil {
			decoded = map[string]any{}
		}
		if schema.Validate(decoded) == nil {
			return nil, nil
		}
		detail, truncated := boundedValidationText(runErr.Error(), 2048)
		callID, _ := boundedValidationText(ctx.FunctionCallID(), 128)
		emit(Event{Type: "tool_validation_failed", Title: "Invalid arguments: " + t.Name(), Tool: t.Name(), NodeID: nodeID, Detail: detail, Data: map[string]any{"callId": callID, "stage": "schema_validation", "executed": false, "error": detail, "truncated": truncated}})
		return nil, nil
	}
}

func boundedValidationText(value string, limit int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false
	}
	return string(runes[:limit]) + "…", true
}
