package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"gemmadesktop.local/app/internal/inference"
)

func TestRejectedToolArgumentsEmitBoundedValidationActivityWithoutExecution(t *testing.T) {
	invalid := strings.Repeat("é", 3000) + " preserved-invalid-tail"
	modelCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var request inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		modelCalls++
		switch modelCalls {
		case 1:
			_ = json.NewEncoder(w).Encode(inference.ChatChunk{Done: true, Message: inference.Message{ToolCalls: []inference.ToolCall{{ID: "invalid-proposal", Function: inference.ToolFunction{Name: "run_command", Arguments: map[string]any{"args": invalid}}}}}})
		case 2:
			last := request.Messages[len(request.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "preserved-invalid-tail") || !strings.Contains(last.Content, `want \"array\"`) {
				t.Errorf("ADK model-facing validation diagnostic was changed: role=%s bytes=%d", last.Role, len(last.Content))
			}
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"valid-proposal","function":{"name":"run_command","arguments":{"args":["-m","unittest"]}}}]},"done":true}`)
		default:
			fmt.Fprintln(w, `{"message":{"content":"Reported the command failure."},"done":true}`)
		}
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	var events []Event
	hostCalls := 0
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Use array arguments", NodeID: "qa-node", Tools: []ToolDefinition{{Name: "run_command", Parameters: map[string]any{"type": "object", "properties": map[string]any{"args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"args"}}}}}, func(event Event) { events = append(events, event) }, func(context.Context, ToolCall) (ToolResult, error) {
		hostCalls++
		return ToolResult{Content: "command exited 1"}, errors.New("command failed")
	})
	if err != nil || hostCalls != 1 || result.Content != "Reported the command failure." {
		t.Fatalf("run err=%v hostCalls=%d content=%q", err, hostCalls, result.Content)
	}
	counts := map[string]int{}
	for _, event := range events {
		counts[event.Type]++
		if event.Type != "tool_validation_failed" {
			continue
		}
		data := event.Data.(map[string]any)
		if event.NodeID != "qa-node" || event.Tool != "run_command" || data["executed"] != false || data["callId"] != "invalid-proposal" || data["stage"] != "schema_validation" || data["truncated"] != true || data["error"] != event.Detail {
			t.Fatalf("wrong validation metadata: %+v", event)
		}
		if !utf8.ValidString(event.Detail) || utf8.RuneCountInString(event.Detail) > 2049 || strings.Contains(event.Detail, "preserved-invalid-tail") {
			t.Fatalf("unbounded/invalid public diagnostic: bytes=%d", len(event.Detail))
		}
		if _, exists := data["arguments"]; exists {
			t.Fatal("raw rejected arguments copied into public activity")
		}
	}
	if counts["tool_validation_failed"] != 1 || counts["tool_started"] != 1 || counts["tool_failed"] != 1 || counts["tool_completed"] != 0 {
		t.Fatalf("validation duplicated host failures or invented execution: %+v", counts)
	}
	var snapshot sessionSnapshot
	if err := json.Unmarshal(result.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	proposal, diagnostic := false, false
	for _, event := range snapshot.Events {
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if call := part.FunctionCall; call != nil && call.ID == "invalid-proposal" {
				proposal = call.Args["args"] == invalid
			}
			if response := part.FunctionResponse; response != nil && response.ID == "invalid-proposal" {
				detail, _ := response.Response["error"].(string)
				diagnostic = strings.Contains(detail, "preserved-invalid-tail") && strings.Contains(detail, `want "array"`)
			}
		}
	}
	if !proposal || !diagnostic {
		t.Fatalf("original ADK audit changed: proposal=%v diagnostic=%v", proposal, diagnostic)
	}
}
