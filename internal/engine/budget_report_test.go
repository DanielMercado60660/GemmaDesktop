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

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

const reportText = "STATUS-REPORT: inspected twice; the failing test remains unfixed."

func systemText(request inference.ChatRequest) string {
	var system strings.Builder
	for _, message := range request.Messages {
		if message.Role == "system" {
			system.WriteString(message.Content)
		}
	}
	return system.String()
}

type budgetFixture struct {
	mu       sync.Mutex
	requests []inference.ChatRequest
	events   []Event
	executed []ToolCall
}

func (f *budgetFixture) eventsOf(kind string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, event := range f.events {
		if event.Type == kind {
			data, _ := event.Data.(map[string]any)
			out = append(out, data)
		}
	}
	return out
}

// run answers each ordinary turn with an inspect call and the budget notice
// with report, so only the host's reporting generation can produce prose.
func (f *budgetFixture) run(t *testing.T, ctx context.Context, req Request, report string) (Result, error) {
	t.Helper()
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, chat inference.ChatRequest) {
		f.mu.Lock()
		f.requests = append(f.requests, chat)
		n := len(f.requests)
		f.mu.Unlock()
		if strings.Contains(systemText(chat), "Host budget notice") {
			fmt.Fprintln(w, report)
			return
		}
		fmt.Fprintf(w, `{"message":{"tool_calls":[{"id":"inspect-%d","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":100,"eval_count":5}`+"\n", n)
	})
	return New(client).Run(ctx, req, func(event Event) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.events = append(f.events, event)
	}, func(_ context.Context, call ToolCall) (ToolResult, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.executed = append(f.executed, call)
		return ToolResult{Content: "observed " + call.ID}, nil
	})
}

func TestBudgetExhaustionEndsWithOneToolsFreeStatusReport(t *testing.T) {
	var f budgetFixture
	// The report also proposes a tool call: it is dropped, never executed.
	reply := fmt.Sprintf(`{"message":{"content":%q,"tool_calls":[{"id":"report-call","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":300,"eval_count":40}`, reportText)
	result, err := f.run(t, t.Context(), Request{Model: "fixture", Instructions: "Keep the existing permissions.", Prompt: "Fix the failing test", MaxSteps: 2, Tools: []ToolDefinition{{Name: "inspect"}}}, reply)
	if !errors.Is(err, ErrBudgetExceeded) || errors.Is(err, ErrIncompleteResponse) || !strings.Contains(err.Error(), "final step was a status report") {
		t.Fatalf("report did not end as a budget failure: %v", err)
	}
	if result.Content != reportText || result.Incomplete || result.Steps != 3 || len(f.requests) != 3 || len(f.executed) != 2 {
		t.Fatalf("report missing or tools ran: requests=%d executed=%v result=%+v", len(f.requests), f.executed, result)
	}
	// Accounting includes the reporting call like any generation.
	if result.Usage.PromptEvalCount != 500 || result.Usage.EvalCount != 50 || result.TokenAccounting == nil || result.TokenAccounting.Attempts != 3 || result.TokenAccounting.Status != "complete" {
		t.Fatalf("report usage missing: %+v %+v", result.Usage, result.TokenAccounting)
	}
	before, report := f.requests[1], f.requests[2]
	if len(before.Tools) != 1 || strings.Contains(systemText(before), "Host budget notice") {
		t.Fatal("ordinary generation lost its tools or received the notice")
	}
	if len(report.Tools) != 0 || report.Format != nil || systemText(report) != systemText(before)+budgetReportFeedback {
		t.Fatalf("report request still declares tools or lacks the notice: tools=%d system=%q", len(report.Tools), systemText(report))
	}
	observed := false
	for _, message := range report.Messages {
		observed = observed || message.Role == "tool" && message.ToolCallID == "inspect-2"
	}
	if !observed {
		t.Fatal("report did not see the run's latest observation")
	}
	metrics := f.eventsOf("generation_metrics")
	if notices := f.eventsOf("budget_report"); len(notices) != 1 || len(metrics) != 3 || metrics[2]["budgetReport"] != true || metrics[2]["toolCalls"] != 0 || metrics[2]["droppedToolCalls"] != 1 || metrics[1]["budgetReport"] != false {
		t.Fatalf("report not marked in events: notices=%v metrics=%v", notices, metrics)
	}
	streamed := false
	for _, event := range f.events {
		streamed = streamed || event.Type == "text" && strings.Contains(event.Detail, "STATUS-REPORT")
	}
	if !streamed {
		t.Fatal("report text was not presented")
	}
	// The report is a message for the user, not a replayable model turn.
	for _, leaked := range []string{"STATUS-REPORT", "report-call", "Host budget notice"} {
		if strings.Contains(string(result.Snapshot), leaked) {
			t.Fatalf("report entered the session snapshot: %q", leaked)
		}
	}
}

func TestWithinBudgetRunMakesNoReportCall(t *testing.T) {
	var calls atomic.Int32
	var reports atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, chat inference.ChatRequest) {
		if strings.Contains(systemText(chat), "Host budget notice") {
			reports.Add(1)
		}
		if calls.Add(1) == 1 {
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"inspect","function":{"name":"inspect","arguments":{}}}]},"done":true}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"content":"Done within budget."},"done":true}`)
	})
	notices := 0
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Inspect", MaxSteps: 2, Tools: []ToolDefinition{{Name: "inspect"}}}, func(event Event) {
		if event.Type == "budget_report" {
			notices++
		}
	}, func(context.Context, ToolCall) (ToolResult, error) { return ToolResult{Content: "observed"}, nil })
	if err != nil || result.Content != "Done within budget." || calls.Load() != 2 || reports.Load() != 0 || notices != 0 || result.Steps != 2 {
		t.Fatalf("run that used exactly its budget changed: err=%v calls=%d reports=%d notices=%d result=%+v", err, calls.Load(), reports.Load(), notices, result)
	}
}

// A failed report ends the run exactly as the budget check did before: a
// budget failure without content, no retry, no tool, and its cost counted.
func TestFailedStatusReportFailsAsBudgetExhaustion(t *testing.T) {
	for _, tc := range []struct{ name, reply string }{
		{"empty", `{"message":{"thinking":"REPORT-THOUGHT"},"done":true,"done_reason":"stop","prompt_eval_count":300,"eval_count":40}`},
		{"tool_only", `{"message":{"tool_calls":[{"id":"report-call","function":{"name":"inspect","arguments":{}}}]},"done":true,"prompt_eval_count":300,"eval_count":40}`},
		{"length", `{"message":{"content":"REPORT-PARTIAL"},"done":true,"done_reason":"length","prompt_eval_count":300,"eval_count":40}`},
		{"transport", `not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f budgetFixture
			result, err := f.run(t, t.Context(), Request{Model: "fixture", Prompt: "Fix the failing test", MaxSteps: 2, Tools: []ToolDefinition{{Name: "inspect"}}}, tc.reply)
			if !errors.Is(err, ErrBudgetExceeded) || errors.Is(err, ErrIncompleteResponse) || strings.Contains(err.Error(), "final step was a status report") {
				t.Fatalf("failed report changed the failure: %v", err)
			}
			if result.Content != "" || result.Incomplete || len(f.requests) != 3 || len(f.executed) != 2 || len(f.eventsOf("generation_retry")) != 0 {
				t.Fatalf("failed report retried, ran a tool or left content: requests=%d executed=%v result=%+v", len(f.requests), f.executed, result)
			}
			if tc.name != "transport" && (result.Usage.PromptEvalCount != 500 || result.Usage.EvalCount != 50) {
				t.Fatalf("failed report usage lost: %+v", result.Usage)
			}
			for _, leaked := range []string{"REPORT-THOUGHT", "REPORT-PARTIAL", "report-call"} {
				if strings.Contains(string(result.Snapshot), leaked) {
					t.Fatalf("failed report persisted: %q", leaked)
				}
			}
		})
	}
}

// Runs nested in a tool (delegates, workflow nodes) share the budget but not
// the allowance, so only the outermost run spends one extra call.
func TestOnlyTheOutermostRunReports(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, chat inference.ChatRequest) {
		nested := false
		for _, message := range chat.Messages {
			nested = nested || message.Role == "user" && message.Content == "nested task"
		}
		kind := "outer"
		switch {
		case strings.Contains(systemText(chat), "Host budget notice"):
			kind = "report"
		case nested:
			kind = "nested"
		}
		mu.Lock()
		requests = append(requests, kind)
		mu.Unlock()
		switch kind {
		case "report":
			fmt.Fprintf(w, `{"message":{"content":%q},"done":true}`+"\n", reportText)
		case "nested":
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"nested-inspect","function":{"name":"inspect","arguments":{}}}]},"done":true}`)
		default:
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"delegate","function":{"name":"delegate","arguments":{}}}]},"done":true}`)
		}
	})
	budget := &Budget{MaxModelCalls: 2, MaxToolCalls: 8, MaxOutputBytes: 1 << 20}
	var nestedErr error
	var nestedContent string
	var execute ToolExecutor
	execute = func(ctx context.Context, call ToolCall) (ToolResult, error) {
		if call.Name != "delegate" {
			return ToolResult{Content: "observed"}, nil
		}
		out, err := New(client).Run(ctx, Request{Model: "fixture", Prompt: "nested task", MaxSteps: 4, NodeID: "delegate_1", Tools: []ToolDefinition{{Name: "inspect"}}}, nil, execute)
		nestedErr, nestedContent = err, out.Content
		return ToolResult{Content: out.Content}, err
	}
	result, err := New(client).Run(WithBudget(t.Context(), budget), Request{Model: "fixture", Prompt: "outer task", MaxSteps: 4, Tools: []ToolDefinition{{Name: "delegate"}}}, nil, execute)
	if !errors.Is(nestedErr, ErrBudgetExceeded) || strings.Contains(nestedErr.Error(), "status report") || nestedContent != "" {
		t.Fatalf("nested run reported: err=%v content=%q", nestedErr, nestedContent)
	}
	if !errors.Is(err, ErrBudgetExceeded) || result.Content != reportText || strings.Join(requests, ",") != "outer,nested,report" || budget.ModelCalls != 3 {
		t.Fatalf("outer report missing or extra calls: err=%v requests=%v calls=%d content=%q", err, requests, budget.ModelCalls, result.Content)
	}
}

func TestStructuredRunReportsInProseWithoutTheActionProtocol(t *testing.T) {
	var requests []inference.ChatRequest
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, chat inference.ChatRequest) {
		requests = append(requests, chat)
		if len(requests) == 1 {
			fmt.Fprintln(w, `{"message":{"content":"{\"action\":\"tool_call\",\"name\":\"inspect\",\"arguments\":{}}"},"done":true}`)
			return
		}
		fmt.Fprintf(w, `{"message":{"content":%q},"done":true}`+"\n", reportText)
	})
	executed := 0
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Inspect", MaxSteps: 1, ToolProtocol: ToolProtocolStructuredJSON, Tools: []ToolDefinition{{Name: "inspect", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}}, nil, func(context.Context, ToolCall) (ToolResult, error) {
		executed++
		return ToolResult{Content: "observed"}, nil
	})
	if !errors.Is(err, ErrBudgetExceeded) || result.Content != reportText || executed != 1 || len(requests) != 2 {
		t.Fatalf("structured report: err=%v executed=%d requests=%d result=%+v", err, executed, len(requests), result)
	}
	report := requests[1]
	if requests[0].Format == nil || report.Format != nil || len(report.Tools) != 0 || strings.Contains(systemText(report), "JSON action protocol") || !strings.Contains(systemText(report), "Host budget notice") {
		t.Fatalf("report kept the action protocol: format=%s system=%q", report.Format, systemText(report))
	}
}

func TestSummaryGenerationNeverReports(t *testing.T) {
	var calls atomic.Int32
	client := emptyRetryClient(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		calls.Add(1)
		fmt.Fprintln(w, `{"message":{"content":"summary"},"done":true}`)
	})
	m := &ollamaModel{client: client, request: defaults(Request{Model: "fixture", MaxSteps: 4}), budget: &Budget{ModelCalls: 1, MaxModelCalls: 1, MaxOutputBytes: 1 << 20}, outputTokenLimit: 256}
	ctx := context.WithValue(t.Context(), budgetReportKey{}, &budgetReport{})
	var failure error
	for _, err := range m.GenerateContent(ctx, &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("Summarize", genai.RoleUser)}}, false) {
		failure = err
	}
	var report *budgetReportError
	if !errors.Is(failure, ErrBudgetExceeded) || errors.As(failure, &report) || calls.Load() != 0 {
		t.Fatalf("summary spent the report allowance: err=%v calls=%d", failure, calls.Load())
	}
}
