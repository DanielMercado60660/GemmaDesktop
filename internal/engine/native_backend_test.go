package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestNativeEngineToolRoundTripAndTerminalUsage(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid=%t", invalid), func(t *testing.T) {
			var requests, executions atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if requests.Add(1) == 1 {
					arguments := `{}`
					if invalid {
						arguments = `{`
					}
					argJSON, _ := json.Marshal(arguments)
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"remember this plan\",\"tool_calls\":[{\"index\":0,\"id\":\"original-id\",\"function\":{\"name\":\"inspect\",\"arguments\":"+string(argJSON)+"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
				} else {
					foundThought, foundID := false, false
					for _, raw := range body["messages"].([]any) {
						message := raw.(map[string]any)
						if message["role"] == "assistant" && message["reasoning_content"] == "remember this plan" {
							foundThought = true
						}
						if message["role"] == "tool" && message["tool_call_id"] == "original-id" {
							foundID = true
						}
					}
					if !foundThought || !foundID {
						t.Errorf("native tool cycle lost history: %+v", body["messages"])
					}
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			backend, err := inference.NewOMLXBackend(inference.NativeConfig{BaseURL: server.URL, Models: []inference.CompletionModel{{Name: "fixture", ArtifactDigest: strings.Repeat("a", 64), ContextTokens: 32768, Capabilities: inference.CompletionCapabilities{Completion: true, Tools: true, Thinking: true}}}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := New(backend).Run(t.Context(), Request{Model: "fixture", Prompt: "inspect", MaxSteps: 2, Tools: []ToolDefinition{{Name: "inspect"}}}, nil, func(_ context.Context, call ToolCall) (ToolResult, error) {
				executions.Add(1)
				if call.ID != "original-id" {
					t.Errorf("tool ID changed: %s", call.ID)
				}
				return ToolResult{Content: "observed"}, nil
			})
			wantCalls := int32(2)
			if invalid {
				wantCalls = 1
			}
			if (err != nil) != invalid || requests.Load() != wantCalls || result.Usage.PromptEvalCount != int(wantCalls)*11 || result.Usage.EvalCount != int(wantCalls)*7 || !result.TokenAccounting.Complete() {
				t.Fatalf("requests=%d err=%v result=%+v", requests.Load(), err, result)
			}
			if invalid && executions.Load() != 0 || !invalid && executions.Load() != 1 {
				t.Fatalf("invalid tool authority: %d", executions.Load())
			}
		})
	}
}
