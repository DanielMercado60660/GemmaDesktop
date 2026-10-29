package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/inference"
)

func TestProviderTerminalAccountingSurvivesRejectedPayloads(t *testing.T) {
	for _, tc := range []struct {
		name, body          string
		complete, wantError bool
		prompt, output      int
	}{
		{"normal", `{"message":{"content":"answer"},"done":true,"prompt_eval_count":11,"eval_count":7}`, true, false, 11, 7},
		{"explicit_zero", `{"message":{"content":"answer"},"done":true,"prompt_eval_count":0,"eval_count":0}`, true, false, 0, 0},
		{"missing_prompt", `{"message":{"content":"answer"},"done":true,"eval_count":7}`, false, false, 0, 7},
		{"missing_output", `{"message":{"content":"answer"},"done":true,"prompt_eval_count":11}`, false, false, 11, 0},
		{"negative_prompt", `{"message":{"content":"answer"},"done":true,"prompt_eval_count":-1,"eval_count":7}`, false, false, 0, 7},
		{"length", `{"message":{"content":"unfinished"},"done":true,"done_reason":"length","prompt_eval_count":11,"eval_count":7}`, true, true, 11, 7},
		{"duplicate_id", `{"message":{"tool_calls":[{"id":"same","function":{"name":"inspect","arguments":{}}},{"id":"same","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":11,"eval_count":7}`, true, true, 11, 7},
		{"missing_tool_name", `{"message":{"tool_calls":[{"id":"bad","function":{"arguments":{}}}]},"done":true,"prompt_eval_count":11,"eval_count":7}`, true, true, 11, 7},
		{"malformed_tool_arguments", `{"message":{"tool_calls":[{"id":"bad","function":{"name":"inspect","arguments":"not an object"}}]},"done":true,"prompt_eval_count":11,"eval_count":7}`, true, true, 11, 7},
		{"provider_terminal_error", `{"error":"fixture generation error","done":true,"prompt_eval_count":11,"eval_count":7}`, true, true, 11, 7},
		{"data_after_terminal", `{"message":{"content":"answer"},"done":true,"prompt_eval_count":11,"eval_count":7}` + "\n" + `{"done":false}`, true, true, 11, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) { fmt.Fprintln(w, tc.body) })
			result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "go", MaxSteps: 1, Tools: []ToolDefinition{{Name: "inspect"}}}, nil, func(context.Context, ToolCall) (ToolResult, error) {
				t.Error("rejected payload executed a tool")
				return ToolResult{}, nil
			})
			if (err != nil) != tc.wantError || result.Usage.PromptEvalCount != tc.prompt || result.Usage.EvalCount != tc.output || result.TokenAccounting == nil || result.TokenAccounting.Complete() != tc.complete || result.TokenAccounting.Attempts != 1 || result.TokenAccounting.TerminalResponses != 1 {
				t.Fatalf("err=%v result=%+v accounting=%+v", err, result, result.TokenAccounting)
			}
		})
	}
}
func TestAbortedGenerationKeepsObservedLowerBoundNotComplete(t *testing.T) {
	for _, mode := range []string{"eof", "cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 2*time.Second)
				defer deadlineCancel()
			}
			client := emptyRetryClient(t, func(w http.ResponseWriter, r *http.Request, _ inference.ChatRequest) {
				if calls.Add(1) == 1 {
					fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"observed","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":100,"eval_count":5}`)
					return
				}
				fmt.Fprintln(w, `{"message":{"content":"partial second response"},"done":false}`)
				w.(http.Flusher).Flush()
				if mode == "cancel" || mode == "deadline" {
					<-r.Context().Done()
				}
			})
			result, err := New(client).Run(ctx, Request{Model: "fixture", Prompt: "go", MaxSteps: 2, Tools: []ToolDefinition{{Name: "inspect"}}}, func(e Event) {
				if mode == "cancel" && e.Type == "text" {
					cancel()
				}
			}, func(context.Context, ToolCall) (ToolResult, error) { return ToolResult{Content: "observed"}, nil })
			if err == nil || result.Usage.PromptEvalCount != 100 || result.Usage.EvalCount != 5 || result.TokenAccounting == nil || result.TokenAccounting.Complete() || result.TokenAccounting.Status != "partial" || result.TokenAccounting.Attempts != 2 || result.TokenAccounting.MeasuredResponses != 1 {
				t.Fatalf("aborted cost falsely complete: %v %+v accounting=%+v", err, result, result.TokenAccounting)
			}
			if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected real context deadline, got %v", err)
			}
		})
	}
}
func TestTerminalReceiptRetainedWhenConsumerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		fmt.Fprintln(w, `{"message":{"content":"terminal answer"},"done":true,"prompt_eval_count":12,"eval_count":8}`)
	})
	result, err := New(client).Run(ctx, Request{Model: "fixture", Prompt: "go", MaxSteps: 1}, func(e Event) {
		if e.Type == "text" {
			cancel()
		}
	}, nil)
	if !errors.Is(err, context.Canceled) || result.Usage.PromptEvalCount != 12 || result.Usage.EvalCount != 8 || !result.TokenAccounting.Complete() {
		t.Fatalf("terminal receipt lost: %v %+v accounting=%+v", err, result, result.TokenAccounting)
	}
}
func TestEmptyRetryReceiptsAreCountedOnce(t *testing.T) {
	var calls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		body := `{"message":{},"done":true,"prompt_eval_count":100,"eval_count":7}`
		if calls.Add(1) == 2 {
			body = `{"message":{"content":"answer"},"done":true,"prompt_eval_count":110,"eval_count":5}`
		}
		fmt.Fprintln(w, body)
	})
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "go", MaxSteps: 2}, nil, nil)
	if err != nil || calls.Load() != 2 || result.Usage.PromptEvalCount != 210 || result.Usage.EvalCount != 12 || !result.TokenAccounting.Complete() || result.TokenAccounting.Attempts != 2 {
		t.Fatalf("retry accounting: %v %+v accounting=%+v", err, result, result.TokenAccounting)
	}
	if strings.Contains(string(result.Snapshot), `"PromptTokenCount":210`) {
		t.Fatal("aggregate cost polluted compaction prompt measurement")
	}
}

func TestCancelledParentCannotClaimCompleteBeforeNestedUsageReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"child","function":{"name":"delegate","arguments":{}}}]},"done":true,"prompt_eval_count":10,"eval_count":2}`)
	})
	entered, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	releaseChild := func() { released.Do(func() { close(release) }) }
	defer releaseChild()
	resultCh := make(chan Result, 1)
	go func() {
		result, _ := New(client).Run(ctx, Request{Model: "fixture", Prompt: "go", MaxSteps: 2, Tools: []ToolDefinition{{Name: "delegate"}}}, nil, func(childCtx context.Context, _ ToolCall) (ToolResult, error) {
			close(entered)
			<-release // Simulates a child unwinding after the parent's deadline.
			AccountNestedUsage(childCtx, inference.Metrics{PromptEvalCount: 20, EvalCount: 3}, inference.NewTokenAccounting(2, 1, 1))
			return ToolResult{}, context.Canceled
		})
		resultCh <- result
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("delegate did not start")
	}
	cancel()
	var result Result
	select {
	case result = <-resultCh:
		if result.TokenAccounting.Complete() {
			t.Fatalf("parent returned complete before child's cost was known: %+v", result.TokenAccounting)
		}
	case <-time.After(100 * time.Millisecond):
		// If the runner waits for its tool, it cannot publish premature costs.
		releaseChild()
		select {
		case result = <-resultCh:
		case <-time.After(2 * time.Second):
			t.Fatal("parent did not return after child released")
		}
	}
	if result.TokenAccounting.Complete() || result.TokenAccounting.Status != "partial" || result.Usage.PromptEvalCount != 30 || result.Usage.EvalCount != 5 {
		t.Fatalf("nested partial receipt lost: usage=%+v accounting=%+v", result.Usage, result.TokenAccounting)
	}
}
