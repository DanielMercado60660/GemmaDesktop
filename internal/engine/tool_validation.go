package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"
)

// ADK validates function arguments before invoking our host callback. Observe
// those rejected proposals without creating a host execution event or receipt.
// Keep its original error and arguments; recognized mistakes receive additional
// request-shape guidance in the same paired function response, never coercion.
func observeToolValidation(schemas map[string]*jsonschema.Resolved, nodeID string, emit func(Event)) llmagent.OnToolErrorCallback {
	return func(ctx agent.Context, t tool.Tool, args map[string]any, runErr error) (map[string]any, error) {
		if runErr == nil || ctx.Err() != nil {
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
		if emit != nil {
			detail, truncated := boundedValidationText(runErr.Error(), 2048)
			callID, _ := boundedValidationText(ctx.FunctionCallID(), 128)
			emit(Event{Type: "tool_validation_failed", Title: "Invalid arguments: " + t.Name(), Tool: t.Name(), NodeID: nodeID, Detail: detail, Data: map[string]any{"callId": callID, "stage": "schema_validation", "executed": false, "error": detail, "truncated": truncated}})
		}
		object, _ := decoded.(map[string]any)
		if guidance := toolArgumentGuidance(t.Name(), schema.Schema(), object); guidance != "" {
			return map[string]any{"error": runErr.Error(), "isError": true, "executed": false, "stage": "schema_validation", "guidance": guidance}, nil
		}
		return nil, nil
	}
}

// Only direct, declared constraints are described. Do not infer a correction
// from validator prose, schema descriptions, references, or malformed values.
// In particular, a string that resembles JSON or shell syntax stays a string.
func toolArgumentGuidance(name string, schema *jsonschema.Schema, args map[string]any) string {
	if schema == nil || schema.Ref != "" || schema.DynamicRef != "" {
		return ""
	}
	var hints []string
	switch name {
	case "run_command":
		field := schema.Properties["args"]
		_, isString := args["args"].(string)
		if isString && field != nil && field.Ref == "" && field.DynamicRef == "" && field.Type == "array" && field.Items != nil && field.Items.Ref == "" && field.Items.DynamicRef == "" && field.Items.Type == "string" {
			hints = append(hints, `args must be a JSON array of strings, for example "args":["-m","unittest","-v"], not a string containing JSON or a shell command. Resubmit the intended arguments as separate array items; do not change their values or repeat the executable in args.`)
		}
	case "read_file":
		for _, key := range []string{"startLine", "lineCount"} {
			field := schema.Properties[key]
			value, numeric := args[key].(float64)
			if !numeric || field == nil || field.Ref != "" || field.DynamicRef != "" || field.Type != "integer" || field.Minimum == nil || field.Maximum == nil || field.ExclusiveMinimum != nil || field.ExclusiveMaximum != nil {
				continue
			}
			if math.Trunc(value) != value || value < *field.Minimum || value > *field.Maximum {
				hints = append(hints, fmt.Sprintf("%s must be an integer from %g to %g (inclusive). Request a range within those bounds; use additional reads if needed.", key, *field.Minimum, *field.Maximum))
			}
		}
	}
	if len(hints) == 0 {
		return ""
	}
	guidance, _ := boundedValidationText("Nothing executed. "+strings.Join(hints, " ")+" Preserve the task's constraints and existing permissions; this feedback grants no execution approval.", 1024)
	return guidance
}

func boundedValidationText(value string, limit int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false
	}
	return string(runes[:limit]) + "…", true
}
