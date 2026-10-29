package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestStructuredAdapterPreservesGovernedToolCycle(t *testing.T) {
	var generations atomic.Int32
	var effects atomic.Int32
	var presented strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var request inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Format == nil || len(request.Tools) != 0 {
			t.Error("structured transport not selected")
		}
		n := generations.Add(1)
		action := `{"action":"tool_call","name":"read_file","arguments":{"path":"note.txt"}}`
		if n == 2 {
			encoded, _ := json.Marshal(request.Messages)
			if !strings.Contains(string(encoded), "Host tool observation") || !strings.Contains(string(encoded), "OBSERVED-72") {
				t.Error("tool evidence lost")
			}
			action = `{"action":"final","answer":"Observed OBSERVED-72."}`
		}
		// Deliberately split the envelope: no fragment is a user-visible answer.
		for _, part := range []string{action[:10], action[10:]} {
			json.NewEncoder(w).Encode(inference.ChatChunk{Message: inference.Message{Content: part}})
		}
		fmt.Fprintln(w, `{"done":true,"done_reason":"stop","prompt_eval_count":20,"eval_count":10}`)
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "read note.txt", ToolProtocol: ToolProtocolStructuredJSON, Tools: []ToolDefinition{{Name: "read_file", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}}}}, func(ev Event) {
		if ev.Type == "text" {
			presented.WriteString(ev.Detail)
		}
	}, func(_ context.Context, call ToolCall) (ToolResult, error) {
		effects.Add(1)
		if call.ID == "" || call.Name != "read_file" || call.Arguments["path"] != "note.txt" {
			t.Errorf("bad call: %+v", call)
		}
		return ToolResult{Content: "OBSERVED-72"}, nil
	})
	if err != nil || result.Content != "Observed OBSERVED-72." || effects.Load() != 1 || generations.Load() != 2 || result.Usage.EvalCount != 20 {
		t.Fatalf("result=%+v err=%v effects=%d", result, err, effects.Load())
	}
	if strings.Contains(presented.String(), `"action"`) {
		t.Fatal("raw envelope streamed to presentation")
	}
	if !strings.Contains(string(result.Snapshot), "functionResponse") {
		t.Fatal("ADK tool observation not persisted")
	}
}

func TestStructuredAdapterNeverDispatchesInvalidOrTruncatedActions(t *testing.T) {
	for _, tc := range []struct {
		name, content, reason string
		native                bool
		want                  error
	}{
		{"truncated", `{"action":"tool_call","name":"effect","arguments":{}}`, "length", false, ErrIncompleteResponse},
		{"badJSON", `{"action":`, "stop", false, ErrInvalidStructuredAction},
		{"unknown", `{"action":"tool_call","name":"unknown","arguments":{}}`, "stop", false, ErrInvalidStructuredAction},
		{"mixed", `{"action":"final","answer":"done"}`, "stop", true, ErrInvalidStructuredAction},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var effects, requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					fmt.Fprint(w, `{}`)
					return
				}
				requests.Add(1)
				message := inference.Message{Content: tc.content}
				if tc.native {
					message.ToolCalls = []inference.ToolCall{{Function: inference.ToolFunction{Name: "effect", Arguments: map[string]any{}}}}
				}
				json.NewEncoder(w).Encode(inference.ChatChunk{Message: message, Done: true, DoneReason: tc.reason, Metrics: inference.Metrics{PromptEvalCount: 12, EvalCount: 7}})
			}))
			defer srv.Close()
			client, _ := inference.NewClient(srv.URL)
			result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "do task", ToolProtocol: ToolProtocolStructuredJSON, Tools: []ToolDefinition{{Name: "effect", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}}, nil, func(context.Context, ToolCall) (ToolResult, error) { effects.Add(1); return ToolResult{}, nil })
			if !errors.Is(err, tc.want) || !result.Incomplete || effects.Load() != 0 || requests.Load() != 1 || result.Content != "" || result.Usage.EvalCount != 7 {
				t.Fatalf("result=%+v err=%v effects=%d requests=%d", result, err, effects.Load(), requests.Load())
			}
			if strings.Contains(string(result.Snapshot), tc.content) {
				t.Fatal("invalid action persisted")
			}
		})
	}
}

func TestStructuredAdapterCountsRawEnvelopeBudgetAndLeavesSummaryProse(t *testing.T) {
	for _, summary := range []bool{false, true} {
		t.Run(fmt.Sprint(summary), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					fmt.Fprint(w, `{}`)
					return
				}
				var request inference.ChatRequest
				json.NewDecoder(r.Body).Decode(&request)
				if summary && request.Format != nil {
					t.Error("summary incorrectly constrained as an action")
				}
				content := `{"action":"final","answer":"` + strings.Repeat("x", 300) + `"}`
				if summary {
					content = "A prose summary."
				}
				json.NewEncoder(w).Encode(inference.ChatChunk{Message: inference.Message{Content: content}, Done: true})
			}))
			defer srv.Close()
			client, _ := inference.NewClient(srv.URL)
			budget := &Budget{MaxModelCalls: 2, MaxOutputBytes: 256}
			m := &ollamaModel{client: client, request: defaults(Request{Model: "fixture", ToolProtocol: ToolProtocolStructuredJSON}), budget: budget}
			if summary {
				m.outputTokenLimit = 256
			}
			req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("summarize", genai.RoleUser)}}
			var got error
			var response *model.LLMResponse
			for r, err := range m.GenerateContent(t.Context(), req, false) {
				response, got = r, err
			}
			if summary {
				if got != nil || response == nil {
					t.Fatalf("summary failed: %v", got)
				}
			} else if !errors.Is(got, ErrBudgetExceeded) {
				t.Fatalf("raw output escaped byte cap: %v", got)
			}
		})
	}
}

func TestUnknownToolProtocolRejectsBeforeInference(t *testing.T) {
	client, _ := inference.NewClient("http://127.0.0.1:1")
	_, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "task", ToolProtocol: "unknown"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "toolProtocol") {
		t.Fatalf("unknown protocol not rejected: %v", err)
	}
}
