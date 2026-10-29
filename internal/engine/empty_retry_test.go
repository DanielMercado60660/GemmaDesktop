package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func emptyRetryClient(t *testing.T, handle func(http.ResponseWriter, *http.Request, inference.ChatRequest)) *inference.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var request inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		handle(w, r, request)
	}))
	t.Cleanup(srv.Close)
	client, err := inference.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestEmptyRetryPreservesHistoryAndExecutesOnlyNewTool(t *testing.T) {
	var calls atomic.Int32
	requests := make(chan inference.ChatRequest, 4)
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		requests <- req
		switch calls.Add(1) {
		case 1:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"already-done","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":90,"eval_count":4,"total_duration":5}`)
		case 2:
			fmt.Fprintln(w, `{"message":{"thinking":"discarded empty reasoning"},"done":true,"done_reason":"stop","prompt_eval_count":100,"eval_count":7,"total_duration":10}`)
		case 3:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"fresh-call","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":110,"eval_count":5,"total_duration":20}`)
		default:
			fmt.Fprintln(w, `{"message":{"content":"Observed the new result."},"done":true,"prompt_eval_count":140,"eval_count":3,"total_duration":30}`)
		}
	})
	var events []Event
	executed := map[string]int{}
	req := Request{Model: "fixture", Instructions: "Keep the existing permissions.", Prompt: "Continue the review", MaxSteps: 4, MaxOutputTokens: 256, History: []Message{
		{Role: "user", Content: "Earlier request"},
		{Role: "assistant", Thinking: "historical completed reasoning", ToolCalls: []inference.ToolCall{{ID: "old-call", Function: inference.ToolFunction{Name: "inspect", Arguments: map[string]any{}}}}},
		{Role: "tool", ToolName: "inspect", ToolCallID: "old-call", Content: `{"content":"historical completed observation"}`},
	}, Tools: []ToolDefinition{{Name: "inspect"}}}
	result, err := New(client).Run(t.Context(), req, func(event Event) { events = append(events, event) }, func(_ context.Context, call ToolCall) (ToolResult, error) {
		executed[call.ID]++
		if call.ID != "fresh-call" && call.ID != "already-done" {
			t.Errorf("historical tool dispatched: %+v", call)
		}
		return ToolResult{Content: "fresh observation"}, nil
	})
	if err != nil || len(executed) != 2 || executed["already-done"] != 1 || executed["fresh-call"] != 1 || calls.Load() != 4 || result.Steps != 4 || result.Incomplete || result.Content != "Observed the new result." {
		t.Fatalf("retry result: err=%v executions=%v requests=%d result=%+v", err, executed, calls.Load(), result)
	}
	if result.Usage.PromptEvalCount != 440 || result.Usage.EvalCount != 19 || result.Usage.TotalDuration != 65 {
		t.Fatalf("discarded attempt usage missing or counted twice: %+v", result.Usage)
	}
	<-requests // The initial generation completed a real tool before the empty response.
	first, second, third := <-requests, <-requests, <-requests
	split := func(request inference.ChatRequest) (string, []inference.Message) {
		var system string
		var rest []inference.Message
		for _, message := range request.Messages {
			if message.Role == "system" {
				system += message.Content
			} else {
				rest = append(rest, message)
			}
		}
		return system, rest
	}
	system1, history1 := split(first)
	system2, history2 := split(second)
	system3, _ := split(third)
	if system2 != system1+emptyGenerationFeedback || system3 != system1 || !reflect.DeepEqual(history1, history2) || !reflect.DeepEqual(first.Options, second.Options) {
		t.Fatal("retry changed completed history/options, fabricated a turn, or persisted its feedback")
	}
	oldCall, oldResult, oldReasoning, completedCall, completedResult := false, false, false, false, false
	for _, message := range history2 {
		oldResult = oldResult || message.ToolCallID == "old-call"
		completedResult = completedResult || message.Role == "tool" && message.ToolCallID == "already-done"
		oldReasoning = oldReasoning || message.Thinking == "historical completed reasoning"
		for _, call := range message.ToolCalls {
			oldCall = oldCall || call.ID == "old-call"
			completedCall = completedCall || call.ID == "already-done"
		}
	}
	if !oldCall || !oldResult || !oldReasoning || !completedCall || !completedResult || second.Options["num_predict"] != float64(256) {
		t.Fatalf("completed history or output limit changed: call=%v result=%v reasoning=%v options=%v", oldCall, oldResult, oldReasoning, second.Options)
	}
	metrics, retries := 0, 0
	for _, event := range events {
		if event.Type == "generation_retry" {
			retries++
		}
		if event.Type == "generation_metrics" {
			metrics++
			data := event.Data.(map[string]any)
			if data["step"] != metrics || data["maxOutputTokens"] != 256 {
				t.Fatalf("attempt usage or limit ambiguous: %+v", data)
			}
		}
	}
	if retries != 1 || metrics != 4 {
		t.Fatalf("retry/attempt metrics count: %d/%d", retries, metrics)
	}
	if strings.Contains(string(result.Snapshot), "discarded empty reasoning") || strings.Contains(string(result.Snapshot), emptyGenerationFeedback) {
		t.Fatal("discarded reasoning or protocol feedback persisted as conversation")
	}
	var snapshot sessionSnapshot
	if err := json.Unmarshal(result.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range snapshot.Events {
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.ID == "fresh-call" {
				found = true
				if event.UsageMetadata.PromptTokenCount != 110 || event.UsageMetadata.CandidatesTokenCount != 5 || event.CustomMetadata["discarded_generation_usage"] == nil {
					t.Fatalf("successful ADK usage corrupted by discarded attempt: %+v", event.UsageMetadata)
				}
			}
		}
	}
	if !found {
		t.Fatal("successful tool turn missing from snapshot")
	}
}

func TestEmptyRetrySecondFailurePreservesAllObservedUsage(t *testing.T) {
	for _, kind := range []string{"empty", "length", "transport", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int32
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
				if calls.Add(1) == 1 {
					fmt.Fprintln(w, `{"done":true,"done_reason":"stop","prompt_eval_count":100,"eval_count":7}`)
					return
				}
				switch kind {
				case "empty":
					fmt.Fprintln(w, `{"message":{"thinking":"second discarded thought"},"done":true,"done_reason":"stop","prompt_eval_count":110,"eval_count":9}`)
				case "length":
					fmt.Fprintln(w, `{"message":{"content":"partial"},"done":true,"done_reason":"length","prompt_eval_count":110,"eval_count":9}`)
				case "transport":
					http.Error(w, "fixture transport failure", http.StatusServiceUnavailable)
				case "cancel":
					cancel()
				}
			})
			retries, metrics := 0, 0
			result, err := New(client).Run(ctx, Request{Model: "fixture", Prompt: "Respond", MaxSteps: 4}, func(event Event) {
				if event.Type == "generation_retry" {
					retries++
				}
				if event.Type == "generation_metrics" {
					metrics++
				}
			}, nil)
			wantPrompt, wantOutput, wantMetrics := 100, 7, 1
			if kind == "empty" || kind == "length" {
				wantPrompt, wantOutput, wantMetrics = 210, 16, 2
				if !errors.Is(err, ErrIncompleteResponse) || !result.Incomplete {
					t.Fatalf("incomplete error identity lost: %v", err)
				}
			} else if errors.Is(err, ErrIncompleteResponse) || result.Incomplete {
				t.Fatalf("transport/cancel error reclassified as incomplete: %v", err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) || kind == "empty" && !errors.Is(err, ErrEmptyResponse) {
				t.Fatalf("underlying error identity lost: %v", err)
			}
			if err == nil || calls.Load() != 2 || retries != 1 || metrics != wantMetrics || result.Usage.PromptEvalCount != wantPrompt || result.Usage.EvalCount != wantOutput {
				t.Fatalf("unbounded retry or discarded usage lost: err=%v calls=%d retries=%d metrics=%d usage=%+v", err, calls.Load(), retries, metrics, result.Usage)
			}
			if strings.Contains(string(result.Snapshot), "second discarded thought") {
				t.Fatal("discarded thinking persisted")
			}
		})
	}
}

func TestEmptyRetryHonorsSharedModelBudget(t *testing.T) {
	var calls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		calls.Add(1)
		fmt.Fprintln(w, `{"done":true,"done_reason":"stop","prompt_eval_count":100,"eval_count":7}`)
	})
	budget := &Budget{MaxModelCalls: 1, MaxToolCalls: 4, MaxOutputBytes: 1 << 20}
	retries := 0
	result, err := New(client).Run(WithBudget(t.Context(), budget), Request{Model: "fixture", Prompt: "Respond", MaxSteps: 4}, func(event Event) {
		if event.Type == "generation_retry" {
			retries++
		}
	}, nil)
	if !errors.Is(err, ErrBudgetExceeded) || errors.Is(err, ErrIncompleteResponse) || calls.Load() != 1 || retries != 0 || result.Steps != 1 || result.Usage.PromptEvalCount != 100 || result.Usage.EvalCount != 7 {
		t.Fatalf("shared budget or empty usage lost: err=%v calls=%d retries=%d result=%+v", err, calls.Load(), retries, result)
	}
}

func TestEmptyRetryHonorsSharedOutputBudget(t *testing.T) {
	var calls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		if calls.Add(1) == 1 {
			fmt.Fprintln(w, `{"message":{"thinking":"12345"},"done":true,"done_reason":"stop","prompt_eval_count":100,"eval_count":7}`)
			return
		}
		// This would fit a fresh budget, but not the remaining three bytes.
		fmt.Fprintln(w, `{"message":{"content":"four"},"done":false}`)
	})
	budget := &Budget{MaxModelCalls: 4, MaxToolCalls: 4, MaxOutputBytes: 8}
	result, err := New(client).Run(WithBudget(t.Context(), budget), Request{Model: "fixture", Prompt: "Respond", MaxSteps: 4}, nil, nil)
	if !errors.Is(err, ErrBudgetExceeded) || calls.Load() != 2 || result.Usage.PromptEvalCount != 100 || result.Usage.EvalCount != 7 || result.Content != "" {
		t.Fatalf("retry reset output budget or lost observed cost: calls=%d result=%+v error=%v", calls.Load(), result, err)
	}
}

func TestNonemptyFailureAndSummaryNeverRetry(t *testing.T) {
	for _, kind := range []string{"length", "transport", "cancel", "summary"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int32
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
				calls.Add(1)
				switch kind {
				case "length":
					fmt.Fprintln(w, `{"message":{"content":"partial"},"done":true,"done_reason":"length","eval_count":9}`)
				case "transport":
					http.Error(w, "fixture failure", http.StatusServiceUnavailable)
				case "cancel":
					cancel()
				case "summary":
					fmt.Fprintln(w, `{"done":true,"done_reason":"stop","eval_count":9}`)
				}
			})
			retries := 0
			m := &ollamaModel{client: client, request: defaults(Request{Model: "fixture", MaxSteps: 4}), budget: &Budget{MaxModelCalls: 4, MaxOutputBytes: 1 << 20}, emit: func(event Event) {
				if event.Type == "generation_retry" {
					retries++
				}
			}}
			if kind == "summary" {
				m.outputTokenLimit = 256
			}
			var failure error
			for _, err := range m.GenerateContent(ctx, &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Respond", genai.RoleUser)}}, false) {
				failure = err
			}
			if failure == nil || calls.Load() != 1 || retries != 0 {
				t.Fatalf("noneligible generation retried: err=%v calls=%d retries=%d", failure, calls.Load(), retries)
			}
			if kind == "summary" && m.usage.EvalCount != 9 {
				t.Fatalf("summary usage changed: %+v", m.usage)
			}
		})
	}
}
