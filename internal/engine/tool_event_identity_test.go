package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestToolCompletionEventIdentityDoesNotChangeModelResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%v", failed), func(t *testing.T) {
			var requests atomic.Int32
			responses := make(chan map[string]any, 1)
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
				if requests.Add(1) == 1 {
					fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"host-call","function":{"name":"inspect","arguments":{}}}]},"done":true}`)
					return
				}
				for _, message := range req.Messages {
					if message.Role == "tool" {
						var response map[string]any
						if err := json.Unmarshal([]byte(message.Content), &response); err != nil {
							t.Error(err)
						}
						responses <- response
					}
				}
				fmt.Fprintln(w, `{"message":{"content":"Observed the result."},"done":true}`)
			})
			payload := map[string]any{"callId": "untrusted-result-field", "value": "evidence"}
			original, _ := json.Marshal(payload)
			var startID, executedID, terminalID, terminalType string
			result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Inspect", Tools: []ToolDefinition{{Name: "inspect"}}}, func(event Event) {
				if event.Type == "tool_started" {
					startID = event.Data.(ToolCall).ID
				}
				if event.Type == "tool_completed" || event.Type == "tool_failed" {
					data := event.Data.(map[string]any)
					terminalID, _ = data["callId"].(string)
					terminalType = event.Type
					// An event consumer owns its top-level metadata. It cannot
					// inject graph state into the separate ADK response map.
					data["callId"] = "event-consumer-id"
					data["content"] = "event-consumer-prose"
				}
			}, func(_ context.Context, call ToolCall) (ToolResult, error) {
				executedID = call.ID
				var toolErr error
				if failed {
					toolErr = errors.New("fixture failure")
				}
				return ToolResult{Content: "tool evidence", Data: payload}, toolErr
			})
			if err != nil || result.Content != "Observed the result." || startID != "host-call" || terminalID != startID || executedID != startID {
				t.Fatalf("tool identity mismatch: start=%q execute=%q terminal=%q err=%v", startID, executedID, terminalID, err)
			}
			wantType := "tool_completed"
			want := map[string]any{"isError": failed, "content": "tool evidence", "data": payload}
			if failed {
				wantType = "tool_failed"
				want["error"] = "fixture failure"
			}
			if terminalType != wantType {
				t.Fatalf("terminal type=%q want=%q", terminalType, wantType)
			}
			select {
			case got := <-responses:
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("event metadata changed model-facing result: got=%+v want=%+v", got, want)
				}
			default:
				t.Fatal("no model-facing tool response")
			}
			after, _ := json.Marshal(payload)
			if string(after) != string(original) {
				t.Fatal("executor result data was mutated")
			}
		})
	}
}
