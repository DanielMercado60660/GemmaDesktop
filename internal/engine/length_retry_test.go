package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestLengthStopRetriesOnceWithFeedback(t *testing.T) {
	var calls atomic.Int32
	requests := make(chan inference.ChatRequest, 3)
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		requests <- req
		switch calls.Add(1) {
		case 1:
			fmt.Fprintln(w, `{"message":{"thinking":"DISCARDED-LONG-REASONING","content":"DISCARDED-PARTIAL","tool_calls":[{"id":"truncated-call","function":{"name":"inspect","arguments":{}}}]},"done":true,"done_reason":"length","prompt_eval_count":100,"eval_count":256}`)
		case 2:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"fresh-call","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":110,"eval_count":5}`)
		default:
			fmt.Fprintln(w, `{"message":{"content":"Short final answer."},"done":true,"prompt_eval_count":120,"eval_count":3}`)
		}
	})
	var retries []map[string]any
	executed := map[string]int{}
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Instructions: "Keep the existing permissions.", Prompt: "Answer", MaxSteps: 4, MaxOutputTokens: 256, Tools: []ToolDefinition{{Name: "inspect"}}}, func(event Event) {
		if event.Type == "generation_retry" {
			retries = append(retries, event.Data.(map[string]any))
		}
	}, func(_ context.Context, call ToolCall) (ToolResult, error) {
		executed[call.ID]++
		return ToolResult{Content: "observed"}, nil
	})
	if err != nil || result.Incomplete || result.Content != "Short final answer." || calls.Load() != 3 || result.Steps != 3 {
		t.Fatalf("length retry did not recover: err=%v calls=%d result=%+v", err, calls.Load(), result)
	}
	if len(executed) != 1 || executed["fresh-call"] != 1 {
		t.Fatalf("truncated tool proposal executed or retry call lost: %v", executed)
	}
	if len(retries) != 1 || retries[0]["reason"] != "length" || retries[0]["attempt"] != 2 || retries[0]["maxRetries"] != 2 {
		t.Fatalf("retry events: %+v", retries)
	}
	if result.Usage.PromptEvalCount != 330 || result.Usage.EvalCount != 264 {
		t.Fatalf("discarded attempt usage missing or counted twice: %+v", result.Usage)
	}
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
	system1, history1 := split(<-requests)
	retry := <-requests
	system2, history2 := split(retry)
	system3, _ := split(<-requests)
	if system2 != system1 || system3 != system1 || len(history2) != len(history1)+1 || !reflect.DeepEqual(history1, history2[:len(history1)]) || history2[len(history1)].Content != lengthGenerationFeedback || history2[len(history1)].Role != "user" || retry.Options["num_predict"] != float64(256) {
		t.Fatal("retry changed history/limits, or its feedback persisted beyond the retry")
	}
	for _, leaked := range []string{"DISCARDED-LONG-REASONING", "DISCARDED-PARTIAL", "truncated-call", lengthGenerationFeedback} {
		if strings.Contains(string(result.Snapshot), leaked) {
			t.Fatalf("discarded generation or protocol feedback persisted: %q", leaked)
		}
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
				found = event.UsageMetadata.PromptTokenCount == 110 && event.CustomMetadata["discarded_generation_usage"] != nil
			}
		}
	}
	if !found {
		t.Fatal("successful retry lost its own usage or the discarded-attempt audit")
	}
}

func TestLengthStopThreeTimesStaysIncomplete(t *testing.T) {
	var calls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		fmt.Fprintf(w, `{"message":{"content":"partial-%d"},"done":true,"done_reason":"length","prompt_eval_count":100,"eval_count":9}`+"\n", calls.Add(1))
	})
	retries := 0
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Answer", MaxSteps: 4}, func(event Event) {
		if event.Type == "generation_retry" {
			retries++
		}
	}, nil)
	if !errors.Is(err, ErrIncompleteResponse) || errors.Is(err, ErrBudgetExceeded) || !result.Incomplete || result.Content != "partial-3" || calls.Load() != 3 || retries != 2 || result.Usage.EvalCount != 27 {
		t.Fatalf("third length stop not reported as incomplete: err=%v calls=%d retries=%d result=%+v", err, calls.Load(), retries, result)
	}
}

func TestLengthRetryHonorsModelBudget(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared=%v", shared), func(t *testing.T) {
			var calls atomic.Int32
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
				calls.Add(1)
				fmt.Fprintln(w, `{"message":{"content":"partial"},"done":true,"done_reason":"length","prompt_eval_count":100,"eval_count":9}`)
			})
			ctx, req := t.Context(), Request{Model: "fixture", Prompt: "Answer", MaxSteps: 1}
			if shared {
				req.MaxSteps = 4
				ctx = WithBudget(ctx, &Budget{MaxModelCalls: 1, MaxToolCalls: 4, MaxOutputBytes: 1 << 20})
			}
			retries := 0
			result, err := New(client).Run(ctx, req, func(event Event) {
				if event.Type == "generation_retry" {
					retries++
				}
			}, nil)
			// Without a remaining call the stop fails exactly as before retries.
			if !errors.Is(err, ErrIncompleteResponse) || errors.Is(err, ErrBudgetExceeded) || !result.Incomplete || result.Content != "partial" || calls.Load() != 1 || retries != 0 || result.Usage.EvalCount != 9 {
				t.Fatalf("budget not honored: err=%v calls=%d retries=%d result=%+v", err, calls.Load(), retries, result)
			}
		})
	}
}

// Gemma 4 through Ollama can return two empty turns or two length stops in a
// row. The second retry escalates its feedback and still never persists it.
func TestSecondRetryRecoversConsecutiveEmptyOrLengthStops(t *testing.T) {
	stops := map[string]string{
		"empty":  `{"message":{"thinking":"DISCARDED-THOUGHT"},"done":true,"done_reason":"stop","prompt_eval_count":100,"eval_count":7}`,
		"length": `{"message":{"content":"DISCARDED-PARTIAL"},"done":true,"done_reason":"length","prompt_eval_count":100,"eval_count":256}`,
	}
	feedback := map[string]string{"empty": emptyGenerationFeedback, "length": lengthGenerationFeedback}
	for _, sequence := range [][2]string{{"empty", "empty"}, {"length", "length"}, {"empty", "length"}} {
		t.Run(sequence[0]+"_"+sequence[1], func(t *testing.T) {
			var requests []inference.ChatRequest
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
				requests = append(requests, req)
				switch n := len(requests); {
				case n <= 2:
					fmt.Fprintln(w, stops[sequence[n-1]])
				case n == 3:
					fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"fresh-call","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":110,"eval_count":5}`)
				default:
					fmt.Fprintln(w, `{"message":{"content":"Recovered answer."},"done":true,"prompt_eval_count":120,"eval_count":3}`)
				}
			})
			var retries []map[string]any
			executed := 0
			result, err := New(client).Run(t.Context(), Request{Model: "fixture", Instructions: "Keep the existing permissions.", Prompt: "Answer", MaxSteps: 4, MaxOutputTokens: 256, Tools: []ToolDefinition{{Name: "inspect"}}}, func(event Event) {
				if event.Type == "generation_retry" {
					retries = append(retries, event.Data.(map[string]any))
				}
			}, func(context.Context, ToolCall) (ToolResult, error) {
				executed++
				return ToolResult{Content: "observed"}, nil
			})
			if err != nil || result.Incomplete || result.Content != "Recovered answer." || len(requests) != 4 || executed != 1 || result.Steps != 4 {
				t.Fatalf("second retry did not recover: err=%v requests=%d executed=%d result=%+v", err, len(requests), executed, result)
			}
			if len(retries) != 2 || retries[0]["attempt"] != 2 || retries[1]["attempt"] != 3 || retries[1]["maxRetries"] != 2 || retries[1]["reason"] != map[string]string{"empty": "empty_response", "length": "length"}[sequence[1]] {
				t.Fatalf("retry events: %+v", retries)
			}
			discardedOutput := map[string]int{"empty": 7, "length": 256}
			if result.Usage.PromptEvalCount != 430 || result.Usage.EvalCount != discardedOutput[sequence[0]]+discardedOutput[sequence[1]]+8 {
				t.Fatalf("discarded attempt usage missing or counted twice: %+v", result.Usage)
			}
			assertRetryMessageSuffix(t, requests[0], requests[1], feedback[sequence[0]])
			assertRetryMessageSuffix(t, requests[0], requests[2], feedback[sequence[1]]+secondRetryFeedback)
			if systemText(requests[3]) != systemText(requests[0]) {
				t.Fatal("retry feedback persisted in the system instruction")
			}
			for _, leaked := range []string{"DISCARDED-THOUGHT", "DISCARDED-PARTIAL", "Host protocol feedback", "Second retry"} {
				if strings.Contains(string(result.Snapshot), leaked) {
					t.Fatalf("discarded generation or feedback persisted: %q", leaked)
				}
			}
			var snapshot sessionSnapshot
			if err := json.Unmarshal(result.Snapshot, &snapshot); err != nil {
				t.Fatal(err)
			}
			audited := false
			for _, event := range snapshot.Events {
				if event.Content == nil || len(event.Content.Parts) == 0 || event.Content.Parts[0].FunctionCall == nil || event.Content.Parts[0].FunctionCall.ID != "fresh-call" {
					continue
				}
				encoded, _ := json.Marshal(event.CustomMetadata["discarded_generation_usage"])
				var discarded inference.Metrics
				audited = json.Unmarshal(encoded, &discarded) == nil && discarded.PromptEvalCount == 200 && event.UsageMetadata.PromptTokenCount == 110
			}
			if !audited {
				t.Fatal("both discarded attempts missing from the successful turn's audit")
			}
		})
	}
}

// A second retry needs its own model call. Without one a truncated turn keeps
// its partial text as incomplete, while an empty turn, which would leave the
// user nothing, spends the run's one tools-free status report instead.
func TestSecondRetryHonorsModelBudget(t *testing.T) {
	for _, kind := range []string{"empty", "length"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, chat inference.ChatRequest) {
				calls.Add(1)
				if strings.Contains(systemText(chat), "Host budget notice") {
					if len(chat.Tools) != 0 || strings.Contains(systemText(chat), "Host protocol feedback") {
						t.Errorf("report kept tools or retry feedback: %q", systemText(chat))
					}
					fmt.Fprintf(w, `{"message":{"content":%q},"done":true}`+"\n", reportText)
					return
				}
				if kind == "empty" {
					fmt.Fprintln(w, `{"done":true,"done_reason":"stop","prompt_eval_count":100,"eval_count":7}`)
					return
				}
				fmt.Fprintln(w, `{"message":{"content":"partial"},"done":true,"done_reason":"length","prompt_eval_count":100,"eval_count":9}`)
			})
			retries, notices := 0, 0
			result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Answer", MaxSteps: 2, Tools: []ToolDefinition{{Name: "inspect"}}}, func(event Event) {
				switch event.Type {
				case "generation_retry":
					retries++
				case "budget_report":
					notices++
				}
			}, func(context.Context, ToolCall) (ToolResult, error) {
				t.Error("no generation here proposes a tool")
				return ToolResult{}, nil
			})
			if kind == "empty" && (calls.Load() != 3 || retries != 1 || notices != 1 || result.Steps != 3) {
				t.Fatalf("empty turn retried past the budget or skipped the report: calls=%d retries=%d notices=%d result=%+v", calls.Load(), retries, notices, result)
			}
			if kind == "empty" && (!errors.Is(err, ErrBudgetExceeded) || errors.Is(err, ErrIncompleteResponse) || !strings.Contains(err.Error(), "final step was a status report") || result.Content != reportText || result.Incomplete) {
				t.Fatalf("empty without a retry call: err=%v result=%+v", err, result)
			}
			if kind == "length" && (calls.Load() != 2 || retries != 1 || notices != 0 || result.Steps != 2) {
				t.Fatalf("second retry exceeded budget: calls=%d retries=%d notices=%d result=%+v", calls.Load(), retries, notices, result)
			}
			if kind == "length" && (!errors.Is(err, ErrIncompleteResponse) || errors.Is(err, ErrBudgetExceeded) || !result.Incomplete || result.Content != "partial") {
				t.Fatalf("truncation without a retry call: err=%v result=%+v", err, result)
			}
		})
	}
}
