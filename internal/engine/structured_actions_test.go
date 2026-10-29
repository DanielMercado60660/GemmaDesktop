package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func actionFixtureTools() []inference.ToolDefinition {
	return []inference.ToolDefinition{
		{Type: "function", Function: inference.FunctionDefinition{Name: "read_file", Description: "Read file evidence", Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}}},
		{Type: "function", Function: inference.FunctionDefinition{Name: "edit_file", Description: "Propose a revision-checked edit", Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"path": map[string]any{"type": "string"}, "oldText": map[string]any{"type": "string"}, "newText": map[string]any{"type": "string"}, "expectedHash": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}}, "required": []string{"path", "oldText", "newText", "expectedHash"}}}},
		{Type: "function", Function: inference.FunctionDefinition{Name: "count_rows", Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"count": map[string]any{"type": "integer"}}, "required": []string{"count"}}}},
	}
}

func actionFixtureProtocol(t *testing.T) *StructuredActionProtocol {
	t.Helper()
	_, protocol, err := PrepareStructuredActions(inference.ChatRequest{Tools: actionFixtureTools()})
	if err != nil {
		t.Fatal(err)
	}
	return protocol
}

func decodeActionObservation(t *testing.T, content string) map[string]string {
	t.Helper()
	parts := strings.SplitN(content, "\n", 2)
	if len(parts) != 2 {
		t.Fatal("observation lacks its labeled JSON record")
	}
	var record map[string]string
	if err := json.Unmarshal([]byte(parts[1]), &record); err != nil {
		t.Fatalf("observation record is not reversible JSON: %v", err)
	}
	return record
}

func TestStructuredActionObservationsSurviveGemmaContentRendering(t *testing.T) {
	// Model the two relevant external transformations in Ollama v0.40.11:
	// model/renderers/gemma4.go stripThinking, then TrimSpace. This is a narrow
	// renderer-shaped regression, not a claim of a live Ollama integration test.
	renderAssistantContent := func(text string) string {
		var kept strings.Builder
		for {
			before, rest, found := strings.Cut(text, "<|channel>")
			kept.WriteString(before)
			if !found {
				break
			}
			_, text, found = strings.Cut(rest, "<channel|>")
			if !found {
				break
			}
		}
		return strings.TrimSpace(kept.String())
	}
	for _, source := range []string{
		"  token = '<|channel>thought\\nKEEP THIS<channel|>'\r\n# tail\t \n\n",
		"open_token = '<|channel>'\n# all subsequent source must survive \t\n",
		"literal = '\\n\\u003c'\n日本語 🧪  \t\r\n",
		" \t\r\n",
		"",
	} {
		prepared, _, err := PrepareStructuredActions(inference.ChatRequest{Messages: []inference.Message{{Role: "tool", ToolName: "read_file", ToolCallID: "read-channel-source", Content: source}}})
		if err != nil {
			t.Fatal(err)
		}
		// Pass through the API JSON layer before applying renderer behavior.
		wire, _ := json.Marshal(prepared)
		var received inference.ChatRequest
		if err := json.Unmarshal(wire, &received); err != nil {
			t.Fatal(err)
		}
		content := received.Messages[1].Content
		if strings.Contains(content, "<|channel>") || strings.Contains(content, "<channel|>") {
			t.Fatal("literal channel delimiter reached assistant Content")
		}
		rendered := renderAssistantContent(content)
		observation := decodeActionObservation(t, rendered)
		if observation["body"] != source || observation["tool_call_id"] != "read-channel-source" || observation["original_role"] != "tool" {
			t.Fatalf("renderer changed attributed source: got %q, want %q", observation["body"], source)
		}
		if source != "" && renderAssistantContent(source) == source {
			t.Fatal("fixture does not exercise the original renderer-loss failure")
		}
		if !strings.Contains(received.Messages[0].Content, "Decode that string exactly once") {
			t.Fatal("protocol omitted the reversible observation encoding instruction")
		}
	}
}

func TestStructuredActionsPreserveMultipleADKToolCycles(t *testing.T) {
	const source = "value = '\\n'\r\nlabel = \"日本語 🧪\"\r\n# exact EOF"
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	request, err := toOllama(&model.LLMRequest{
		Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "User's protected-files constraint stays in force."}}}},
		Contents: []*genai.Content{
			{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "Inspect before changing source."}, {InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte("image-data")}}}},
			{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "First thought", Thought: true}, {Text: "I will inspect it."}, {FunctionCall: &genai.FunctionCall{ID: "read_1", Name: "read_file", Args: map[string]any{"path": "src/store.py"}}}}},
			{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "read_1", Name: "read_file", Response: map[string]any{"data": map[string]any{"content": source, "path": "src/store.py", "hash": hash}}}}}},
			{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "Second thought", Thought: true}, {FunctionCall: &genai.FunctionCall{ID: "edit_2", Name: "edit_file", Args: map[string]any{"path": "src/store.py", "oldText": "value = '\\n'", "newText": "value = '\\r\\n'", "expectedHash": hash}}}}},
			{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "edit_2", Name: "edit_file", Response: map[string]any{"isError": true, "error": "approval denied; no edit executed"}}}}},
			{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "Explain the failure."}}},
		},
	}, Request{Model: "fixture", Think: true, ContextSize: 32768})
	if err != nil {
		t.Fatal(err)
	}
	request.Tools = actionFixtureTools()
	before, _ := json.Marshal(request)
	prepared, protocol, err := PrepareStructuredActions(request)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(request)
	if string(before) != string(after) {
		t.Fatal("preparing experimental request changed native request bytes")
	}
	if len(prepared.Tools) != 0 || prepared.Format == nil || protocol == nil {
		t.Fatal("missing response schema or native tools remain")
	}
	if prepared.Think != true || prepared.Messages[0].Role != "system" || !strings.HasPrefix(prepared.Messages[0].Content, request.Messages[0].Content) {
		t.Fatal("changed thinking setting or original system instruction")
	}
	users, thoughts, prose, actions, observations := 0, []string{}, 0, 0, 0
	for _, m := range prepared.Messages {
		if len(m.ToolCalls) != 0 || m.Role == "tool" {
			t.Fatal("native tool protocol leaked into structured history")
		}
		if m.Role == "user" {
			users++
			if m.Content == "Inspect before changing source." && !reflect.DeepEqual(m.Images, request.Messages[1].Images) {
				t.Fatal("dropped or changed original image")
			}
		}
		if m.Thinking != "" {
			thoughts = append(thoughts, m.Thinking)
		}
		if m.Content == "I will inspect it." {
			prose++
		}
		if strings.HasPrefix(m.Content, "Historical tool action;") {
			actions++
			body := strings.SplitN(m.Content, "\n", 2)[1]
			decoded, err := protocol.Decode(body)
			if err != nil || decoded.ToolCall == nil {
				t.Fatalf("historical call did not use valid action envelope: %v", err)
			}
			if decoded.ToolCall.Function.Name == "edit_file" && (decoded.ToolCall.Function.Arguments["oldText"] != "value = '\\n'" || !strings.Contains(m.Content, `"tool_call_id":"edit_2"`)) {
				t.Fatal("changed historical argument or identity")
			}
		}
		if strings.HasPrefix(m.Content, "Host tool observation (") {
			observations++
			if m.Role != "assistant" || !strings.Contains(m.Content, `"original_role":"tool"`) {
				t.Fatal("observation gained user authority or lost attribution")
			}
			observation := decodeActionObservation(t, m.Content)
			if observation["tool_call_id"] == "read_1" && !strings.HasSuffix(observation["body"], source) {
				t.Fatal("source result changed after decoding the observation body once")
			}
			if observation["tool_call_id"] == "edit_2" && !strings.Contains(observation["body"], "approval denied; no edit executed") {
				t.Fatal("lost failed/denied observation")
			}
		}
	}
	if users != 2 || actions != 2 || observations != 2 || prose != 1 || !reflect.DeepEqual(thoughts, []string{"First thought", "Second thought"}) {
		t.Fatalf("history changed: users=%d actions=%d observations=%d prose=%d thoughts=%v", users, actions, observations, prose, thoughts)
	}
}

func TestStructuredActionArgumentsRemainExact(t *testing.T) {
	protocol := actionFixtureProtocol(t)
	args := map[string]any{
		"path": "src/日本語.py", "expectedHash": strings.Repeat("a1", 32),
		"oldText": "value = '\\n'\r\n# quote \" and backslash \\",
		"newText": "value = '\\r\\n'\nlabel = \"🧪\"\n# literal \\ud800, tab\tand nul\x00",
	}
	wire, _ := json.Marshal(map[string]any{"action": "tool_call", "name": "edit_file", "arguments": args})
	decoded, err := protocol.Decode(string(wire))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Answer != "" || decoded.ToolCall == nil || decoded.ToolCall.ID != "" || !reflect.DeepEqual(decoded.ToolCall.Function.Arguments, args) {
		t.Fatalf("changed code/hash arguments or assigned execution identity: %#v", decoded)
	}
	// Reject before ADK's downstream float64 decode can lose an integer bit.
	large, err := protocol.Decode(`{"action":"tool_call","name":"count_rows","arguments":{"count":9007199254740993}}`)
	if err == nil || large.ToolCall != nil {
		t.Fatalf("unsafe integer reached the downstream path: %#v, %v", large, err)
	}
	final, err := protocol.Decode(`{"action":"final","answer":"Line 1\nLine 2: \ud83e\uddea"}`)
	if err != nil || final.ToolCall != nil || final.Answer != "Line 1\nLine 2: 🧪" {
		t.Fatalf("final answer did not decode exactly: %#v, %v", final, err)
	}
}

func TestStructuredActionRejectsInvalidProposalsWithoutAResult(t *testing.T) {
	protocol := actionFixtureProtocol(t)
	cases := map[string]string{
		"unknown tool":       `{"action":"tool_call","name":"delete_everything","arguments":{}}`,
		"unknown action":     `{"action":"execute","name":"read_file","arguments":{"path":"a"}}`,
		"missing field":      `{"action":"tool_call","name":"read_file","arguments":{}}`,
		"wrong type":         `{"action":"tool_call","name":"read_file","arguments":{"path":12}}`,
		"null arguments":     `{"action":"tool_call","name":"read_file","arguments":null}`,
		"extra argument":     `{"action":"tool_call","name":"read_file","arguments":{"path":"a","approve":true}}`,
		"extra envelope":     `{"action":"tool_call","name":"read_file","arguments":{"path":"a"},"approved":true}`,
		"two variants":       `{"action":"tool_call","name":"read_file","arguments":{"path":"a"},"answer":"done"}`,
		"two values":         `{"action":"final","answer":"a"} {"action":"final","answer":"b"}`,
		"array actions":      `[{"action":"final","answer":"a"}]`,
		"trailing garbage":   `{"action":"final","answer":"a"} x`,
		"duplicate action":   `{"action":"final","action":"final","answer":"a"}`,
		"duplicate arg":      `{"action":"tool_call","name":"read_file","arguments":{"path":"safe","path":"unsafe"}}`,
		"escaped duplicate":  `{"action":"tool_call","name":"read_file","arguments":{"path":"safe","pa\u0074h":"unsafe"}}`,
		"wrong case":         `{"Action":"final","answer":"a"}`,
		"fractional integer": `{"action":"tool_call","name":"count_rows","arguments":{"count":1.25}}`,
		"fence":              "```json\n{\"action\":\"final\",\"answer\":\"a\"}\n```",
		"truncated":          `{"action":"final","answer":"a`,
		"blank final":        `{"action":"final","answer":" \n\t"}`,
		"unpaired high":      `{"action":"final","answer":"\ud800"}`,
		"unpaired low":       `{"action":"final","answer":"\udc00"}`,
		"invalid UTF8":       "{\"action\":\"final\",\"answer\":\"\xff\"}",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := protocol.Decode(text)
			if err == nil || result.ToolCall != nil || result.Answer != "" {
				t.Fatalf("invalid proposal produced a usable result: %#v, %v", result, err)
			}
		})
	}
}

func TestStructuredActionPreparationRejectsUnsupportedSchemasAndConflicts(t *testing.T) {
	cases := map[string]map[string]any{
		"reference":          {"type": "object", "$ref": "#/$defs/input", "$defs": map[string]any{}},
		"remote reference":   {"type": "object", "$ref": "https://example.invalid/input.json"},
		"schema dialect":     {"type": "object", "$schema": "https://json-schema.org/draft/2020-12/schema"},
		"unknown constraint": {"type": "object", "mustBeAuthorized": true},
		"nested unsupported": {"type": "object", "properties": map[string]any{"email": map[string]any{"type": "string", "format": "email"}}},
		"nonobject root":     {"type": "array", "items": map[string]any{"type": "string"}},
		"invalid type":       {"type": "object", "properties": map[string]any{"value": map[string]any{"type": "not-a-json-type"}}},
	}
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			request := inference.ChatRequest{Tools: []inference.ToolDefinition{{Type: "function", Function: inference.FunctionDefinition{Name: "fixture", Parameters: schema}}}}
			before, _ := json.Marshal(request)
			out, protocol, err := PrepareStructuredActions(request)
			after, _ := json.Marshal(request)
			if err == nil || protocol != nil || out.Format != nil || string(before) != string(after) {
				t.Fatalf("unsupported schema was accepted or mutated: %v", err)
			}
		})
	}
	for _, request := range []inference.ChatRequest{
		{Format: "json", Tools: actionFixtureTools()},
		{Tools: []inference.ToolDefinition{actionFixtureTools()[0], actionFixtureTools()[0]}},
		{Messages: []inference.Message{{Role: "user", ToolCalls: []inference.ToolCall{{Function: inference.ToolFunction{Name: "read_file"}}}}}},
		{Messages: []inference.Message{{Role: "assistant", Thinking: "invalid \xff"}}},
	} {
		if _, _, err := PrepareStructuredActions(request); err == nil {
			t.Fatal("conflicting format, duplicate tool, or invalid source role was accepted")
		}
	}
}

func TestStructuredActionHistoryKeepsParallelCallsAndRetiredTools(t *testing.T) {
	request := inference.ChatRequest{Tools: actionFixtureTools(), Messages: []inference.Message{
		{Role: "user", Content: "Original request"},
		{Role: "assistant", Thinking: "Original thought", Images: []string{"assistant-image"}, ToolCalls: []inference.ToolCall{
			{ID: "first", Type: "function", Function: inference.ToolFunction{Index: 0, Name: "retired_tool", Arguments: map[string]any{"exact": "\n\\n"}}},
			{ID: "second", Type: "function", Function: inference.ToolFunction{Index: 1, Name: "read_file", Arguments: map[string]any{"path": "b"}}},
		}},
		{Role: "tool", ToolName: "retired_tool", ToolCallID: "first", Content: "First observation\n", Thinking: "Observation metadata thought", Images: []string{"tool-image"}},
		{Role: "tool", ToolName: "read_file", ToolCallID: "second", Content: "Second observation"},
	}}
	prepared, protocol, err := PrepareStructuredActions(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Messages) != 7 || prepared.Messages[2].Thinking != "Original thought" || prepared.Messages[2].Images[0] != "assistant-image" {
		t.Fatal("lost the original assistant message while splitting parallel calls")
	}
	for index, identity := range []string{"first", "second"} {
		if !strings.Contains(prepared.Messages[3+index].Content, `"tool_call_id":"`+identity+`"`) || !strings.Contains(prepared.Messages[5+index].Content, `"tool_call_id":"`+identity+`"`) {
			t.Fatal("parallel action/observation order or identity changed")
		}
	}
	observation := prepared.Messages[5]
	if observation.Thinking != "Observation metadata thought" || observation.Images[0] != "tool-image" || decodeActionObservation(t, observation.Content)["body"] != "First observation\n" {
		t.Fatal("lost observation payload while adapting its role")
	}
	retiredJSON := strings.SplitN(prepared.Messages[3].Content, "\n", 2)[1]
	if !strings.Contains(retiredJSON, `"name":"retired_tool"`) {
		t.Fatal("erased a completed historical call absent from today's catalog")
	}
	if _, err := protocol.Decode(retiredJSON); err == nil {
		t.Fatal("historical existence authorized a retired tool for a new action")
	}
}

func TestStructuredActionNumbersRejectLossInsteadOfCoercing(t *testing.T) {
	request := inference.ChatRequest{Tools: []inference.ToolDefinition{{Type: "function", Function: inference.FunctionDefinition{Name: "numeric", Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"value": map[string]any{"type": "number"}}, "required": []string{"value"}}}}}}
	_, protocol, err := PrepareStructuredActions(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"0.1", "1.0", "1e3", "256", "8192", "9007199254740991", "-9007199254740991", "9.007199254740991e15"} {
		if _, err := protocol.Decode(`{"action":"tool_call","name":"numeric","arguments":{"value":` + value + `}}`); err != nil {
			t.Fatalf("valid numeric value %s: %v", value, err)
		}
	}
	for _, value := range []string{"9007199254740992", "9007199254740993", "-9007199254740992", "9007199254740992.0", "9.007199254740992e15", "-1e16", "18446744073709551615", "18446744073709551616", "1.234567890123456789", "1e99999999", "1e-99999999", "1e-400"} {
		result, err := protocol.Decode(`{"action":"tool_call","name":"numeric","arguments":{"value":` + value + `}}`)
		if err == nil || result.ToolCall != nil {
			t.Fatalf("unrepresentable numeric value %s yielded an action", value)
		}
	}
}

func TestStructuredActionProtocolIsDetachedAndFinalOnlyWithoutTools(t *testing.T) {
	request := inference.ChatRequest{Tools: actionFixtureTools(), Options: map[string]any{"num_predict": 8192, "nested": map[string]any{"key": "original"}}, Messages: []inference.Message{{Role: "user", Content: "Original", Images: []string{"image"}}}}
	prepared, protocol, err := PrepareStructuredActions(request)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Messages[1].Images[0] = "changed"
	prepared.Options["nested"].(map[string]any)["key"] = "changed"
	prepared.Format.(json.RawMessage)[0] = '['
	if request.Messages[0].Images[0] != "image" || request.Options["nested"].(map[string]any)["key"] != "original" {
		t.Fatal("prepared nested values alias the input")
	}
	// Neither caller mutation of Format nor of its original tool schema can
	// weaken the independently resolved validation schema.
	request.Tools[0].Function.Parameters["additionalProperties"] = true
	if _, err := protocol.Decode(`{"action":"tool_call","name":"read_file","arguments":{"path":"a","unexpected":1}}`); err == nil {
		t.Fatal("external mutation weakened action validation")
	}
	_, finalOnly, err := PrepareStructuredActions(inference.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalOnly.Decode(`{"action":"final","answer":"Complete"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := finalOnly.Decode(`{"action":"tool_call","name":"read_file","arguments":{"path":"a"}}`); err == nil {
		t.Fatal("tool-free protocol allowed a tool")
	}
}

func TestStructuredActionLimitsAndSchemaDiscrimination(t *testing.T) {
	protocol := actionFixtureProtocol(t)
	if _, err := protocol.Decode(strings.Repeat(" ", maxStructuredActionBytes+1)); err == nil {
		t.Fatal("response byte limit not enforced")
	}
	if _, err := protocol.Decode(strings.Repeat("[", maxStructuredActionDepth+2) + "0" + strings.Repeat("]", maxStructuredActionDepth+2)); err == nil {
		t.Fatal("response nesting limit not enforced")
	}
	// The name discriminator must select this tool's schema, not a broad union
	// that validates arguments merely because another tool accepts them.
	if _, err := protocol.Decode(`{"action":"tool_call","name":"edit_file","arguments":{"path":"a"}}`); err == nil {
		t.Fatal("edit used read_file's weaker argument schema")
	}
	if _, err := protocol.Decode(`{"action":"tool_call","name":"edit_file","arguments":{"path":"a","oldText":"a","newText":"b","expectedHash":"stale"}}`); err == nil {
		t.Fatal("argument pattern was not enforced")
	}
}
