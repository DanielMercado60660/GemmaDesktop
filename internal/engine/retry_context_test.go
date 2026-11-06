package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func assertRetryMessageSuffix(t *testing.T, original, retry inference.ChatRequest, feedback string) {
	t.Helper()
	if len(retry.Messages) != len(original.Messages)+1 {
		t.Fatalf("retry changed the existing message count: original=%d retry=%d", len(original.Messages), len(retry.Messages))
	}
	last := retry.Messages[len(original.Messages)]
	if last.Role != "user" || last.Content != feedback || len(last.ToolCalls) != 0 || last.ToolCallID != "" {
		t.Fatalf("retry feedback is not a separate trailing protocol message: %+v", last)
	}
	prefix := retry
	prefix.Messages = prefix.Messages[:len(original.Messages)]
	if !reflect.DeepEqual(original, prefix) {
		t.Fatal("retry changed the original system/message prefix, tool pairs, or generation controls")
	}
}

func TestRetryFeedbackLeavesSharedRequestAndEarlierRetryUnchanged(t *testing.T) {
	contents := make([]*genai.Content, 2, 4)
	contents[0] = genai.NewContentFromText("Do not create or edit files.", genai.RoleUser)
	contents[1] = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "observed", Name: "inspect", Response: map[string]any{"content": "Completed observation"}}}}}
	// A caller may reuse the spare backing-array slots for a subsequent turn.
	backing := contents[:cap(contents)]
	backing[2] = genai.NewContentFromText("future input", genai.RoleUser)
	req := &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText("Host permissions are fixed.", genai.RoleUser)}}
	before, _ := json.Marshal(req)
	first := requestWithRetryFeedback(req, emptyGenerationFeedback)
	second := requestWithRetryFeedback(req, lengthGenerationFeedback+secondRetryFeedback)
	after, _ := json.Marshal(req)
	if string(before) != string(after) || backing[2].Parts[0].Text != "future input" {
		t.Fatal("retry mutated the original request or its spare backing-array capacity")
	}
	if len(first.Contents) != 3 || first.Contents[2].Parts[0].Text != emptyGenerationFeedback || len(second.Contents) != 3 || second.Contents[2].Parts[0].Text != lengthGenerationFeedback+secondRetryFeedback {
		t.Fatal("retry feedback stacked or overwrote an earlier attempt")
	}
}

func TestRetryAfterCompactionRetainsExactOriginalUserConstraints(t *testing.T) {
	for _, kind := range []string{"empty", "length"} {
		t.Run(kind, func(t *testing.T) {
			var requests []inference.ChatRequest
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
				requests = append(requests, req)
				if len(requests) == 1 {
					if kind == "length" {
						fmt.Fprintln(w, `{"message":{"content":"discarded partial"},"done":true,"done_reason":"length","prompt_eval_count":100,"eval_count":9}`)
					} else {
						fmt.Fprintln(w, `{"message":{"thinking":"discarded thought"},"done":true,"prompt_eval_count":100,"eval_count":9}`)
					}
					return
				}
				fmt.Fprintln(w, `{"message":{"content":"Answer from preserved instructions."},"done":true,"prompt_eval_count":110,"eval_count":4}`)
			})
			const current = "Continue the earlier review; do not create or change any files."
			const hostInstruction = "The host permits inspection only. Existing capabilities remain frozen."
			result, err := New(client).Run(t.Context(), Request{Model: "fixture", Instructions: hostInstruction, Prompt: current, Snapshot: lossyUserSnapshot(t, exactUserCommand, false), MaxSteps: 2, ContextPolicy: &ContextPolicy{Enabled: true, ThresholdTokens: 32000}}, nil, nil)
			if err != nil || len(requests) != 2 || result.Content != "Answer from preserved instructions." || result.Steps != 2 || result.Usage.EvalCount != 13 {
				t.Fatalf("compacted retry failed or escaped the shared call budget: err=%v requests=%d result=%+v", err, len(requests), result)
			}
			assertRestoredUserWire(t, requests[0], current)
			feedback := emptyGenerationFeedback
			if kind == "length" {
				feedback = lengthGenerationFeedback
			}
			assertRetryMessageSuffix(t, requests[0], requests[1], feedback)
			if !strings.Contains(systemText(requests[1]), hostInstruction) {
				t.Fatal("frozen host instruction missing from retried compacted request")
			}
			for _, transient := range []string{originalUserContextHeader, "Host protocol feedback", "discarded partial", "discarded thought"} {
				if strings.Contains(string(result.Snapshot), transient) {
					t.Fatalf("request-only context or discarded output persisted: %q", transient)
				}
			}
		})
	}
}
