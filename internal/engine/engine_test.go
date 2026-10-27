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
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestADKToolRoundTripAndSnapshotContinuation(t *testing.T) {
	var mu sync.Mutex
	requests := []inference.ChatRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{"capabilities":["completion","tools","thinking"]}`)
			return
		}
		var request inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		requests = append(requests, request)
		turn := len(requests)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-ndjson")
		switch turn {
		case 1:
			fmt.Fprintln(w, `{"message":{"thinking":"I should inspect the file."},"done":false}`)
			fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"inspect_1","function":{"name":"read_file","arguments":{"path":"README.md"}}}]},"done":true,"eval_count":5}`)
		case 2:
			fmt.Fprintln(w, `{"message":{"content":"The file says hello."},"done":false}`)
			fmt.Fprintln(w, `{"done":true,"eval_count":4}`)
		default:
			fmt.Fprintln(w, `{"message":{"content":"I remember the previous result."},"done":true}`)
		}
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	e := New(client)
	calls := 0
	executor := func(ctx context.Context, call ToolCall) (ToolResult, error) {
		calls++
		if call.ID != "inspect_1" || call.Arguments["path"] != "README.md" {
			t.Errorf("unexpected call %+v", call)
		}
		return ToolResult{Content: "hello"}, nil
	}
	req := Request{Model: "local-model", Prompt: "Read README", Instructions: "Use tools for workspace facts.", Tools: []ToolDefinition{{Name: "read_file", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}}}}
	var events []Event
	result, err := e.Run(t.Context(), req, func(ev Event) { events = append(events, ev) }, executor)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result.Content != "The file says hello." || result.Steps != 2 {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	if len(result.Snapshot) == 0 {
		t.Fatal("missing snapshot")
	}
	mu.Lock()
	second := requests[1]
	mu.Unlock()
	hasThinking, hasTool := false, false
	for _, m := range second.Messages {
		hasThinking = hasThinking || m.Thinking == "I should inspect the file."
		hasTool = hasTool || (m.Role == "tool" && m.ToolCallID == "inspect_1" && strings.Contains(m.Content, "hello"))
	}
	if !hasThinking || !hasTool {
		t.Fatalf("continuation lost thinking/tool identity: %+v", second.Messages)
	}
	req.Prompt = "What did you find?"
	req.Snapshot = result.Snapshot
	result, err = e.Run(t.Context(), req, nil, executor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "I remember the previous result." {
		t.Fatal(result.Content)
	}
	mu.Lock()
	third := requests[2]
	mu.Unlock()
	history := false
	for _, m := range third.Messages {
		history = history || m.Content == "The file says hello."
	}
	if !history {
		t.Fatal("snapshot history not restored")
	}
}

func TestRunBudgetStopsToolLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"function":{"name":"again","arguments":{}}}]},"done":true}`)
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	e := New(client)
	calls := 0
	result, err := e.Run(t.Context(), Request{Model: "local-model", Prompt: "go", MaxSteps: 2, Tools: []ToolDefinition{{Name: "again"}}}, nil, func(context.Context, ToolCall) (ToolResult, error) { calls++; return ToolResult{Content: "again"}, nil })
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("%v", err)
	}
	if calls != 2 || len(result.Snapshot) == 0 {
		t.Fatalf("calls=%d snapshot=%d", calls, len(result.Snapshot))
	}
}

func TestMalformedToolArgumentsNeverReachExecutor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"function":{"name":"write","arguments":{"path":42}}}]},"done":true}`)
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	calls := 0
	_, err := New(client).Run(t.Context(), Request{Model: "test", Prompt: "go", MaxSteps: 1, Tools: []ToolDefinition{{Name: "write", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}}}}, nil, func(context.Context, ToolCall) (ToolResult, error) { calls++; return ToolResult{}, nil })
	if calls != 0 {
		t.Fatalf("invalid args reached executor %d times", calls)
	}
	if err == nil {
		t.Fatal("expected validation or budget error")
	}
}

func TestCancellationExportsNoPartialModelTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"content":"unfinished model text"},"done":false}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := New(client).Run(ctx, Request{Model: "test", Prompt: "User input"}, func(ev Event) {
		if ev.Type == "text" {
			cancel()
		}
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result: %v", err)
	}
	if len(result.Snapshot) == 0 || strings.Contains(string(result.Snapshot), "unfinished model text") || !strings.Contains(string(result.Snapshot), "User input") {
		t.Fatalf("partial output was persisted as a complete turn: %s", result.Snapshot)
	}
}

func TestFailedToolOutputIsPreservedForDiagnosis(t *testing.T) {
	var response map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var req inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		for _, msg := range req.Messages {
			if msg.Role == "tool" {
				if err := json.Unmarshal([]byte(msg.Content), &response); err != nil {
					t.Error(err)
				}
				fmt.Fprintln(w, `{"message":{"content":"The failure trace is available for diagnosis."},"done":true}`)
				return
			}
		}
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"test_1","function":{"name":"run_test","arguments":{}}}]},"done":true}`)
	}))
	defer srv.Close()
	c, _ := inference.NewClient(srv.URL)
	var failure Event
	_, err := New(c).Run(t.Context(), Request{Model: "fixture", Prompt: "Run tests", Tools: []ToolDefinition{{Name: "run_test"}}}, func(ev Event) {
		if ev.Type == "tool_failed" {
			failure = ev
		}
	}, func(context.Context, ToolCall) (ToolResult, error) {
		return ToolResult{Content: "FAILED test_answer: expected 42, got 0", Data: map[string]any{"stderr": "assertion failed at answer_test.go:12", "exitCode": 1}}, errors.New("process exited with code 1")
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := response["data"].(map[string]any)
	if response["isError"] != true || response["error"] != "process exited with code 1" || !strings.Contains(fmt.Sprint(response["content"]), "expected 42") || data["stderr"] != "assertion failed at answer_test.go:12" {
		t.Fatalf("model lost diagnostic output: %+v", response)
	}
	if !strings.Contains(failure.Detail, "expected 42") || !strings.Contains(failure.Detail, "process exited with code 1") || failure.Data == nil {
		t.Fatalf("failure event lost output: %+v", failure)
	}
}
