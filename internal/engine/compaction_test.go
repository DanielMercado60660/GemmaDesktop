package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func compactionHistory() []Message {
	var history []Message
	for i := 0; i < 8; i++ {
		history = append(history, Message{Role: "user", Content: fmt.Sprintf("old-evidence-%d %s", i, strings.Repeat("historical detail ", 80))}, Message{Role: "assistant", Content: fmt.Sprintf("Reviewed old-evidence-%d", i)})
	}
	history = append(history, Message{Role: "assistant", ToolCalls: []inference.ToolCall{{ID: "recent_read", Function: inference.ToolFunction{Name: "read_file", Arguments: map[string]any{"path": "recent.txt"}}}}}, Message{Role: "tool", ToolCallID: "recent_read", ToolName: "read_file", Content: `{"content":"recent result"}`})
	return history
}

func isSummaryRequest(req inference.ChatRequest) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "Create a factual continuity summary for Gemma Desktop") {
			return true
		}
	}
	return false
}

func TestCompactionRetainsToolPairsAndRestoresDurableSummary(t *testing.T) {
	var mu sync.Mutex
	var modelRequests []inference.ChatRequest
	summaryCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var req inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if isSummaryRequest(req) {
			summaryCalls++
			if len(req.Tools) != 0 || req.Think != false {
				t.Errorf("summary has authority/thinking: %+v", req)
			}
			if req.Options["num_predict"] != float64(1024) || req.Options["temperature"] != float64(0) {
				t.Errorf("summary output cap or temperature changed: %+v", req.Options)
			}
			fmt.Fprintln(w, `{"message":{"content":"SUMMARY: durable work marker ORCHID-42; earlier evidence reviewed."},"done":true,"prompt_eval_count":300,"eval_count":12}`)
			return
		}
		modelRequests = append(modelRequests, req)
		fmt.Fprintln(w, `{"message":{"content":"Answer after compaction"},"done":true,"prompt_eval_count":50,"eval_count":3}`)
	}))
	defer srv.Close()
	c, _ := inference.NewClient(srv.URL)
	e := New(c)
	req := Request{Model: "fixture", Prompt: "Current user request stays exact", History: compactionHistory(), ContextSize: 4096, MaxSteps: 8, Temperature: 1, Tools: []ToolDefinition{{Name: "read_file"}}, ContextPolicy: &ContextPolicy{Enabled: true, ThresholdTokens: 100, RetainEvents: 1}}
	commits := 0
	result, err := e.Run(t.Context(), req, func(ev Event) {
		if ev.Type == "context_compacted" {
			commits++
		}
	}, func(context.Context, ToolCall) (ToolResult, error) {
		t.Error("unexpected tool action")
		return ToolResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TokenAccounting.Complete() || result.TokenAccounting.Attempts != 2 {
		t.Fatalf("summary accounting missing or double-counted: %+v", result.TokenAccounting)
	}
	if result.Steps != 2 || result.Usage.EvalCount != 15 || commits != 1 {
		t.Fatalf("summary budget/usage/events: steps=%d tokens=%d commits=%d", result.Steps, result.Usage.EvalCount, commits)
	}
	var snapshot sessionSnapshot
	if err = json.Unmarshal(result.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	old, summary := false, false
	for _, ev := range snapshot.Events {
		if ev.Actions.Compaction != nil {
			summary = true
		}
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				old = old || strings.Contains(p.Text, "old-evidence-0")
			}
		}
	}
	if !old || !summary {
		t.Fatalf("raw audit or compaction action lost: raw=%v summary=%v", old, summary)
	}
	req.Snapshot = result.Snapshot
	req.History = nil
	req.Prompt = "Continue after restoration"
	req.ContextPolicy.ThresholdTokens = 2000
	if _, err = e.Run(t.Context(), req, nil, func(context.Context, ToolCall) (ToolResult, error) { return ToolResult{}, nil }); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if summaryCalls != 1 || len(modelRequests) != 2 {
		t.Fatalf("summary repeated after restore: summaries=%d requests=%d", summaryCalls, len(modelRequests))
	}
	for _, request := range modelRequests {
		if request.Options["temperature"] != float64(1) {
			t.Fatalf("ordinary temperature changed during summary or restoration: %+v", request.Options)
		}
		call, result, hasSummary, rawOld, currentUser, hasUserArchive := false, false, false, false, false, false
		for _, m := range request.Messages {
			for _, c := range m.ToolCalls {
				call = call || c.ID == "recent_read"
			}
			result = result || (m.Role == "tool" && m.ToolCallID == "recent_read")
			hasSummary = hasSummary || strings.Contains(m.Content, "ORCHID-42")
			rawOld = rawOld || strings.Contains(m.Content, "old-evidence-0")
			hasUserArchive = hasUserArchive || (m.Role == "user" && strings.HasPrefix(m.Content, originalUserContextHeader))
			currentUser = currentUser || m.Content == "Current user request stays exact"
		}
		if !call || !result || !hasSummary || !rawOld || !hasUserArchive || !currentUser {
			t.Fatalf("bad compacted prompt: call=%v result=%v summary=%v rawOld=%v currentUser=%v", call, result, hasSummary, rawOld, currentUser)
		}
	}
}

func TestCompactionUsesTheRunModelBudget(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var req inference.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls++
		if !isSummaryRequest(req) {
			t.Error("main model call bypassed the exhausted shared budget")
		}
		fmt.Fprintln(w, `{"message":{"content":"Useful summary"},"done":true,"eval_count":5}`)
	}))
	defer srv.Close()
	c, _ := inference.NewClient(srv.URL)
	result, err := New(c).Run(t.Context(), Request{Model: "fixture", Prompt: "continue", MaxSteps: 1, History: compactionHistory(), ContextSize: 4096, ContextPolicy: &ContextPolicy{Enabled: true, ThresholdTokens: 100, RetainEvents: 1}}, nil, nil)
	if !errors.Is(err, ErrBudgetExceeded) || calls != 1 || result.Steps != 1 || result.Usage.EvalCount != 5 || len(result.Snapshot) == 0 {
		t.Fatalf("summary escaped budget or snapshot: err=%v calls=%d steps=%d tokens=%d", err, calls, result.Steps, result.Usage.EvalCount)
	}
}

func TestIncompleteCompactionPreservesRawHistoryAndReportsFailure(t *testing.T) {
	var sawRaw bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var req inference.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if isSummaryRequest(req) {
			fmt.Fprintln(w, `{"message":{"content":"incomplete"},"done":true,"done_reason":"length"}`)
			return
		}
		for _, m := range req.Messages {
			sawRaw = sawRaw || strings.Contains(m.Content, "old-evidence-0")
		}
		fmt.Fprintln(w, `{"message":{"content":"Answer from intact history"},"done":true}`)
	}))
	defer srv.Close()
	c, _ := inference.NewClient(srv.URL)
	failures := 0
	result, err := New(c).Run(t.Context(), Request{Model: "fixture", Prompt: "continue", History: compactionHistory(), ContextSize: 4096, MaxSteps: 4, ContextPolicy: &ContextPolicy{Enabled: true, ThresholdTokens: 100, RetainEvents: 2}}, func(ev Event) {
		if ev.Type == "context_compaction_failed" {
			failures++
		}
	}, nil)
	if err != nil || !sawRaw || failures != 1 || strings.Contains(string(result.Snapshot), `"compactedContent"`) {
		t.Fatalf("failed summary affected history: err=%v raw=%v failures=%d", err, sawRaw, failures)
	}
}

func TestCompactionCancellationClosesItsModelStream(t *testing.T) {
	entered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var req inference.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		fmt.Fprintln(w, `{"message":{"content":"pending summary"},"done":false}`)
		w.(http.Flusher).Flush()
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := inference.NewClient(srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := New(c).Run(ctx, Request{Model: "fixture", Prompt: "continue", History: compactionHistory(), ContextSize: 4096, ContextPolicy: &ContextPolicy{Enabled: true, ThresholdTokens: 100, RetainEvents: 2}}, nil, nil)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("summary did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("summary ignored cancellation")
	}
}

func TestCompactionPreservesLongConstraintsAndPriorSummary(t *testing.T) {
	requests := make(chan inference.ChatRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var req inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		requests <- req
		fmt.Fprintln(w, `{"message":{"content":"The original constraints and handoff remain active."},"done":true,"prompt_eval_count":12000,"eval_count":20}`)
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	budget := &Budget{MaxModelCalls: 8, MaxOutputBytes: 1 << 20}
	metrics := 0
	cfg, _, err := New(client).compactionConfig(Request{Model: "fixture", ContextSize: 32768, MaxSteps: 8, ContextPolicy: &ContextPolicy{Enabled: true}}, budget, func(ev Event) {
		if ev.Type == "generation_metrics" {
			metrics++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both important values are past the former per-part cap, and together
	// these parts exceed the former whole-transcript cap. The real ADK
	// summarizer must render all of them before the HTTP request is sent.
	constraint := strings.Repeat("earlier user context ", 1100) + "REQUIRED: preserve tests/test_existing.py exactly; never change its assertions."
	priorSummary := strings.Repeat("earlier recorded decision ", 900) + "DURABLE_HANDOFF_ORCHID_42_UNCHANGED"
	summaryContent := genai.NewContentFromText(priorSummary, genai.RoleModel)
	events := []*session.Event{
		{Author: "user", LLMResponse: model.LLMResponse{Content: genai.NewContentFromText(constraint, genai.RoleUser)}},
		{Author: "summary", LLMResponse: model.LLMResponse{Content: summaryContent}, Actions: session.EventActions{Compaction: &session.EventCompaction{CompactedContent: summaryContent}}},
	}
	result, err := cfg.Summarizer.SummarizeEvents(t.Context(), events)
	if err != nil || result.Content == nil {
		t.Fatalf("summary failed: result=%+v err=%v", result, err)
	}
	request := <-requests
	var input strings.Builder
	for _, message := range request.Messages {
		input.WriteString(message.Content)
	}
	if !strings.Contains(input.String(), constraint) || !strings.Contains(input.String(), priorSummary) || strings.Contains(input.String(), "[truncated") {
		t.Fatal("summary request silently shortened a user constraint or prior summary")
	}
	if request.Options["num_predict"] != float64(3072) || request.Think != false || len(request.Tools) != 0 {
		t.Fatalf("unexpected summary controls: %+v", request)
	}
	if metrics != 1 {
		t.Fatalf("summary metrics were not forwarded: %d", metrics)
	}
}

func TestCompactionBeyondFourSummariesStillSharesRunBudget(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		calls.Add(1)
		fmt.Fprintln(w, `{"message":{"content":"Retain the user goal and observed evidence."},"done":true,"eval_count":5}`)
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	// An ordinary call has already spent one slot. Six summaries should be
	// allowed by the run's remaining budget, despite the old private cap of
	// four; the next summary must not reach the model endpoint.
	budget := &Budget{ModelCalls: 1, MaxModelCalls: 7, MaxOutputBytes: 1 << 20}
	cfg, _, err := New(client).compactionConfig(Request{Model: "fixture", ContextSize: 32768, MaxSteps: 12, ContextPolicy: &ContextPolicy{Enabled: true}}, budget, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		events := []*session.Event{{Author: "user", LLMResponse: model.LLMResponse{Content: genai.NewContentFromText(fmt.Sprintf("Completed batch %d; retain the same user constraints.", i), genai.RoleUser)}}}
		if _, err := cfg.Summarizer.SummarizeEvents(t.Context(), events); err != nil {
			t.Fatalf("summary %d rejected before the run budget was exhausted: %v", i+1, err)
		}
	}
	events := []*session.Event{{Author: "user", LLMResponse: model.LLMResponse{Content: genai.NewContentFromText("One more batch", genai.RoleUser)}}}
	if _, err := cfg.Summarizer.SummarizeEvents(t.Context(), events); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("summary bypassed exhausted run budget: %v", err)
	}
	if calls.Load() != 6 || budget.ModelCalls != 7 {
		t.Fatalf("wrong budget accounting: requests=%d modelCalls=%d", calls.Load(), budget.ModelCalls)
	}
}
