package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestFinishReserveIsBoundedToolsFreeAndNotReplayed(t *testing.T) {
	const status = "FINISH-STATUS: observed the calculation output; the requested cross-check remains unfinished."
	var calls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, request inference.ChatRequest) {
		calls.Add(1)
		if len(request.Tools) != 0 || request.Options["num_predict"] != float64(1024) || request.Options["temperature"] != float64(0.2) || request.Think != true || !strings.Contains(systemText(request), deadlineReportFeedback) {
			t.Errorf("reserve report changed controls or retained tools: %+v", request)
		}
		paired := false
		for _, message := range request.Messages {
			paired = paired || message.Role == "tool" && message.ToolCallID == "observed" && strings.Contains(message.Content, "measured-output")
		}
		if !paired {
			t.Error("complete observed tool pair missing from report")
		}
		fmt.Fprintf(w, `{"message":{"content":%q,"tool_calls":[{"id":"discard","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":80,"eval_count":21}`+"\n", status)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	budget := &Budget{MaxModelCalls: 2, MaxToolCalls: 2, MaxOutputBytes: 1 << 20}
	req := Request{Model: "fixture", Instructions: "Keep original permissions.", Prompt: "Finish the requested task.", Think: true, Temperature: 0.2, MaxOutputTokens: 8192, MaxSteps: 2, FinishReserveSeconds: 1, Timeout: time.Minute, Tools: []ToolDefinition{{Name: "inspect"}}, History: []Message{{Role: "assistant", ToolCalls: []inference.ToolCall{{ID: "observed", Function: inference.ToolFunction{Name: "inspect", Arguments: map[string]any{}}}}}, {Role: "tool", ToolCallID: "observed", ToolName: "inspect", Content: "measured-output"}}}
	var notices []Event
	result, err := New(client).Run(WithBudget(ctx, budget), req, func(event Event) {
		if event.Type == "budget_report" {
			notices = append(notices, event)
		}
	}, func(context.Context, ToolCall) (ToolResult, error) {
		t.Error("report executed a proposed tool")
		return ToolResult{}, nil
	})
	if !errors.Is(err, ErrBudgetExceeded) || !errors.Is(err, errFinishReserve) || result.Content != status || !result.Incomplete || calls.Load() != 1 || budget.ModelCalls != 1 || budget.ToolCalls != 0 || len(notices) != 1 {
		t.Fatalf("closing result/status/accounting: result=%+v err=%v calls=%d budget=%+v notices=%v", result, err, calls.Load(), budget, notices)
	}
	if result.TokenAccounting == nil || !result.TokenAccounting.Complete() || result.TokenAccounting.Attempts != 1 || result.Usage.EvalCount != 21 {
		t.Fatal("closing usage lost or counted twice", result)
	}
	for _, forbidden := range []string{status, "FINISH-STATUS", "discard", "Host deadline notice"} {
		if strings.Contains(string(result.Snapshot), forbidden) {
			t.Fatalf("closing response/proposal entered snapshot: %q", forbidden)
		}
	}
	if notices[0].Data.(map[string]any)["reason"] != "deadline_reserve" {
		t.Fatal("ambiguous closing reason", notices)
	}
}

func TestFinishReserveDoesNotCapAnOrdinaryGeneration(t *testing.T) {
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, request inference.ChatRequest) {
		if request.Options["num_predict"] != float64(8192) || len(request.Tools) != 1 || strings.Contains(systemText(request), "Host deadline notice") {
			t.Error("ordinary generation changed", request)
		}
		fmt.Fprintln(w, `{"message":{"content":"Complete normal answer."},"done":true}`)
	})
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Inspect.", MaxOutputTokens: 8192, Timeout: time.Minute, FinishReserveSeconds: 1, Tools: []ToolDefinition{{Name: "inspect"}}}, nil, func(context.Context, ToolCall) (ToolResult, error) {
		t.Error("unrequested tool execution")
		return ToolResult{}, nil
	})
	if err != nil || result.Incomplete || result.Content != "Complete normal answer." {
		t.Fatal("ordinary completion changed", result, err)
	}
}

func waitInsideReserve(ctx context.Context, reserve time.Duration) error {
	deadline, _ := ctx.Deadline()
	delay := time.Until(deadline) - reserve
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestFinishReserveAfterToolSuppressesNestedReports(t *testing.T) {
	var calls, reports atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, request inference.ChatRequest) {
		calls.Add(1)
		if strings.Contains(systemText(request), "Host deadline notice") {
			reports.Add(1)
			fmt.Fprintln(w, `{"message":{"content":"The delegated work remains unfinished."},"done":true}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"delegate","function":{"name":"delegate","arguments":{}}}]},"done":true}`)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	budget := &Budget{MaxModelCalls: 4, MaxToolCalls: 2, MaxOutputBytes: 1 << 20}
	var nestedErr error
	executed := 0
	result, err := New(client).Run(WithBudget(ctx, budget), Request{Model: "fixture", Prompt: "Outer task.", Timeout: time.Minute, FinishReserveSeconds: 2, MaxSteps: 4, Tools: []ToolDefinition{{Name: "delegate"}}}, nil, func(ctx context.Context, _ ToolCall) (ToolResult, error) {
		executed++
		if err := waitInsideReserve(ctx, 1500*time.Millisecond); err != nil {
			return ToolResult{}, err
		}
		_, nestedErr = New(client).Run(ctx, Request{Model: "fixture", Prompt: "Nested task.", Timeout: time.Minute, FinishReserveSeconds: 2, MaxSteps: 4}, nil, nil)
		return ToolResult{Content: "Delegated work did not finish."}, nestedErr
	})
	if !errors.Is(nestedErr, errFinishReserve) || !errors.Is(err, errFinishReserve) || !result.Incomplete || executed != 1 || calls.Load() != 2 || reports.Load() != 1 || budget.ModelCalls != 2 {
		t.Fatalf("nested reserve spent extra calls/replayed effects: nested=%v err=%v result=%+v executed=%d calls=%d reports=%d budget=%+v", nestedErr, err, result, executed, calls.Load(), reports.Load(), budget)
	}
}

func TestFinishReserveHonorsCancellationAndOutputBudget(t *testing.T) {
	for _, kind := range []string{"cancelled", "expired", "output-exhausted", "allowance-used"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
				calls.Add(1)
				fmt.Fprintln(w, finishedReply)
			})
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			if kind == "expired" {
				var expired context.CancelFunc
				ctx, expired = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer expired()
			}
			budget := &Budget{MaxModelCalls: 2, MaxToolCalls: 2, MaxOutputBytes: 10}
			if kind == "output-exhausted" {
				budget.OutputBytes = 10
			}
			if kind == "allowance-used" {
				report := &budgetReport{}
				report.used.Store(true)
				ctx = context.WithValue(ctx, budgetReportKey{}, report)
			}
			result, err := New(client).Run(WithBudget(ctx, budget), Request{Model: "fixture", Prompt: "Finish.", Timeout: time.Minute, FinishReserveSeconds: 1}, nil, nil)
			if err == nil || calls.Load() != 0 || budget.ModelCalls != 0 || result.Content != "" {
				t.Fatalf("closing call escaped cancellation/budget: kind=%s result=%+v err=%v calls=%d", kind, result, err, calls.Load())
			}
		})
	}
}

func TestSummaryModelNeverSpendsClosingAllowance(t *testing.T) {
	var calls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, request inference.ChatRequest) {
		calls.Add(1)
		if strings.Contains(systemText(request), "Host deadline notice") {
			t.Error("summary became a status report")
		}
		fmt.Fprintln(w, `{"message":{"content":"Continuity summary."},"done":true}`)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	ctx = context.WithValue(ctx, budgetReportKey{}, &budgetReport{})
	m := &ollamaModel{client: client, request: defaults(Request{Model: "fixture", FinishReserveSeconds: 1}), budget: &Budget{MaxModelCalls: 2, MaxOutputBytes: 1 << 20}, outputTokenLimit: 256}
	for _, err := range m.GenerateContent(ctx, &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Summarize.", genai.RoleUser)}}, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("summary call count changed", calls.Load())
	}
}
