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

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestGenerationControlsReachOllamaWire(t *testing.T) {
	for _, tc := range []struct {
		name      string
		think     any
		limit     int
		wantLimit float64
	}{
		{name: "automatic default", wantLimit: 8192},
		{name: "thinking on", think: true, limit: 256, wantLimit: 256},
		{name: "thinking off", think: false, limit: 32768, wantLimit: 32768},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
				}
				fmt.Fprintln(w, `{"message":{"content":"complete"},"done":true}`)
			}))
			defer srv.Close()
			client, _ := inference.NewClient(srv.URL)
			result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Test explicit controls", Think: tc.think, MaxOutputTokens: tc.limit}, nil, nil)
			if err != nil || result.Content != "complete" {
				t.Fatalf("generation failed: %+v %v", result, err)
			}
			gotThink, hasThink := received["think"]
			if gotThink != tc.think || hasThink != (tc.think != nil) {
				t.Fatalf("thinking override changed: has=%v value=%v expected=%v", hasThink, gotThink, tc.think)
			}
			if got := received["options"].(map[string]any)["num_predict"]; got != tc.wantLimit {
				t.Fatalf("num_predict=%v expected=%v", got, tc.wantLimit)
			}
		})
	}
}

func TestADKOutputLimitCanTightenButNotRaiseAgentLimit(t *testing.T) {
	for _, tc := range []struct{ agent, adk, want int }{
		{0, 0, 8192}, {1024, 256, 256}, {1024, 32768, 1024}, {0, 128, 128},
	} {
		req, err := toOllama(&model.LLMRequest{Config: &genai.GenerateContentConfig{MaxOutputTokens: int32(tc.adk)}}, Request{Model: "fixture", MaxOutputTokens: tc.agent})
		if err != nil || req.Options["num_predict"] != tc.want {
			t.Fatalf("agent=%d ADK=%d result=%+v error=%v", tc.agent, tc.adk, req.Options, err)
		}
	}
	for _, bad := range []int{-1, 255, 32769} {
		if _, err := toOllama(&model.LLMRequest{}, Request{Model: "fixture", MaxOutputTokens: bad}); err == nil {
			t.Errorf("invalid ordinary generation limit accepted: %d", bad)
		}
	}
}

func TestThinkingAtOutputLimitCannotExecuteTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var request inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Think != true || request.Options["num_predict"] != float64(256) {
			t.Errorf("thinking has no generation bound: %+v", request)
		}
		fmt.Fprintln(w, `{"message":{"thinking":"Unfinished private reasoning"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"partial_call","function":{"name":"edit","arguments":{}}}]},"done":true,"done_reason":"length","eval_count":256}`)
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	calls := 0
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "A bounded thinking run", Think: true, MaxOutputTokens: 256, Tools: []ToolDefinition{{Name: "edit"}}}, nil, func(context.Context, ToolCall) (ToolResult, error) { calls++; return ToolResult{}, nil })
	if !errors.Is(err, ErrIncompleteResponse) || !result.Incomplete || calls != 0 || result.Usage.EvalCount != 256 || result.Content != "" {
		t.Fatalf("thinking-only limit became successful or executed a tool: %+v %v calls=%d", result, err, calls)
	}
	if strings.Contains(string(result.Snapshot), "Unfinished private reasoning") || strings.Contains(string(result.Snapshot), "partial_call") {
		t.Fatal("incomplete thinking/tool proposal persisted as a complete turn")
	}
}
