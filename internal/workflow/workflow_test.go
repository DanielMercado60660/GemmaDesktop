package workflow

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

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
)

func actionRuntime(t *testing.T) *Runtime {
	t.Helper()
	c, err := inference.NewClient("http://127.0.0.1:11434")
	if err != nil {
		t.Fatal(err)
	}
	return New(engine.New(c))
}

func TestApprovalSnapshotResumeAndStaleReply(t *testing.T) {
	r := actionRuntime(t)
	d := Definition{ID: "review", Nodes: []Node{{ID: "prepare", Type: "output", Prompt: "draft"}, {ID: "approve", Type: "approval", Prompt: "Accept draft?"}, {ID: "finish", Type: "output", Prompt: "accepted"}}, Edges: []Edge{{Source: "prepare", Target: "approve"}, {Source: "approve", Target: "finish"}}}
	req := engine.Request{Model: "not-used", Prompt: "input"}
	first, err := r.Run(t.Context(), d, req, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Pending) != 1 {
		t.Fatalf("pending=%+v result=%s", first.Pending, first.Content)
	}
	req.Snapshot = first.Snapshot
	req.Resume = map[string]any{first.Pending[0].ID: true}
	second, err := r.Run(t.Context(), d, req, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Pending) != 0 || !strings.Contains(second.Content, "accepted") || !strings.Contains(second.Content, "draft") {
		t.Fatalf("result=%+v", second)
	}
	req.Snapshot = second.Snapshot
	if _, err := r.Run(t.Context(), d, req, nil, nil, nil); err == nil {
		t.Fatal("duplicate reply was accepted")
	}
	req.Snapshot = first.Snapshot
	d.Nodes[2].Prompt = "different"
	if _, err := r.Run(t.Context(), d, req, nil, nil, nil); err == nil {
		t.Fatal("changed graph resumed")
	}
}

func TestJoinResumeRestoresCompletedPredecessors(t *testing.T) {
	r := actionRuntime(t)
	d := Definition{ID: "join", Nodes: []Node{{ID: "start", Type: "parallel"}, {ID: "left", Type: "output", Prompt: "left-result"}, {ID: "approve", Type: "approval"}, {ID: "join", Type: "join"}, {ID: "finish", Type: "output"}}, Edges: []Edge{{Source: "start", Target: "left"}, {Source: "start", Target: "approve"}, {Source: "left", Target: "join"}, {Source: "approve", Target: "join"}, {Source: "join", Target: "finish"}}}
	started := map[string]int{}
	var mu sync.Mutex
	emit := func(ev engine.Event) {
		if ev.Type == "node_started" {
			mu.Lock()
			started[ev.NodeID]++
			mu.Unlock()
		}
	}
	req := engine.Request{Prompt: "seed"}
	first, err := r.Run(t.Context(), d, req, emit, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Pending) != 1 {
		t.Fatal("missing approval")
	}
	req.Snapshot = first.Snapshot
	req.Resume = map[string]any{first.Pending[0].ID: true}
	second, err := r.Run(t.Context(), d, req, emit, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.Content, "left-result") || !strings.Contains(second.Content, "approve") {
		t.Fatalf("join did not complete: %s", second.Content)
	}
	if started["left"] != 1 || started["finish"] != 1 {
		t.Fatalf("replayed or missing nodes: %+v", started)
	}
}

func TestApprovalRejectsWithoutExecutingSuccessor(t *testing.T) {
	r := actionRuntime(t)
	d := Definition{Nodes: []Node{{ID: "ask", Type: "approval"}, {ID: "after", Type: "output"}}, Edges: []Edge{{Source: "ask", Target: "after"}}}
	req := engine.Request{Prompt: "test"}
	first, err := r.Run(t.Context(), d, req, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Snapshot = first.Snapshot
	req.Resume = map[string]any{first.Pending[0].ID: false}
	after := false
	_, err = r.Run(t.Context(), d, req, func(ev engine.Event) { after = after || ev.NodeID == "after" }, nil, nil)
	if !errors.Is(err, ErrApprovalRejected) || after {
		t.Fatalf("err=%v successor=%v", err, after)
	}
}

func TestConditionalRoutingAndJoinRejection(t *testing.T) {
	d := Definition{Nodes: []Node{{ID: "check", Type: "condition", Condition: "contains: pass"}, {ID: "yes", Type: "output", Prompt: "success-branch"}, {ID: "no", Type: "output", Prompt: "failure-branch"}}, Edges: []Edge{{Source: "check", Target: "yes", Condition: "true"}, {Source: "check", Target: "no", Condition: "false"}}}
	r := actionRuntime(t)
	result, err := r.Run(t.Context(), d, engine.Request{Prompt: "pass"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "success-branch") || strings.Contains(result.Content, "failure-branch") {
		t.Fatal(result.Content)
	}
	d.Nodes = append(d.Nodes, Node{ID: "join", Type: "join"})
	d.Edges = append(d.Edges, Edge{Source: "yes", Target: "join"}, Edge{Source: "no", Target: "join"})
	if err := Validate(d); err == nil {
		t.Fatal("conditional join accepted")
	}
}

func TestDynamicFanoutAndSharedBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var req inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		last := req.Messages[len(req.Messages)-1].Content
		b, _ := json.Marshal(inference.ChatChunk{Message: inference.Message{Content: "processed:" + last}, Done: true, Metrics: inference.Metrics{EvalCount: 1}})
		fmt.Fprintln(w, string(b))
	}))
	defer srv.Close()
	c, _ := inference.NewClient(srv.URL)
	r := New(engine.New(c))
	d := Definition{Nodes: []Node{{ID: "fanout", Type: "fanout", Items: []string{"a", "b", "c"}}, {ID: "done", Type: "output"}}, Edges: []Edge{{Source: "fanout", Target: "done"}}}
	result, err := r.Run(t.Context(), d, engine.Request{Model: "local", Prompt: "go", MaxSteps: 4}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Steps != 3 || result.Usage.EvalCount != 3 || !strings.Contains(result.Content, "processed:a") || !strings.Contains(result.Content, "processed:c") {
		t.Fatalf("result=%+v", result)
	}
	_, err = r.Run(context.Background(), d, engine.Request{Model: "local", Prompt: "go", MaxSteps: 2}, nil, nil, nil)
	if !errors.Is(err, engine.ErrBudgetExceeded) {
		t.Fatalf("shared budget failed: %v", err)
	}
}

func TestNodeBudgetCannotBeResetByLargerWorkflowBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"function":{"name":"read","arguments":{}}}]},"done":true}`)
	}))
	defer srv.Close()
	c, _ := inference.NewClient(srv.URL)
	r := New(engine.New(c))
	d := Definition{Nodes: []Node{{ID: "worker", Type: "agent", AgentID: "limited"}}}
	result, err := r.Run(t.Context(), d, engine.Request{Model: "test", Prompt: "go", MaxSteps: 10, Tools: []engine.ToolDefinition{{Name: "read"}}}, nil, func(context.Context, engine.ToolCall) (engine.ToolResult, error) {
		return engine.ToolResult{Content: "read result"}, nil
	}, func(string) (engine.Request, error) {
		return engine.Request{MaxSteps: 1}, nil
	})
	if !errors.Is(err, engine.ErrBudgetExceeded) || result.Steps != 1 {
		t.Fatalf("node budget lost: steps=%d error=%v", result.Steps, err)
	}
}

func TestParallelTerminalsRejectedBeforeExecution(t *testing.T) {
	d := Definition{Nodes: []Node{{ID: "start", Type: "parallel"}, {ID: "left", Type: "agent"}, {ID: "right", Type: "agent"}}, Edges: []Edge{{Source: "start", Target: "left"}, {Source: "start", Target: "right"}}}
	if err := Validate(d); err == nil {
		t.Fatal("parallel branches without a join accepted")
	}
}

func TestAgentConfigurationRetainsReadOnlyToolAndSkillPolicy(t *testing.T) {
	base := engine.Request{Tools: []engine.ToolDefinition{{Name: "write_file"}}, Skills: []string{"base"}}
	req, err := nodeRequest(base, Node{ID: "reviewer", AgentID: "reviewer"}, func(string) (engine.Request, error) {
		return engine.Request{ReadOnly: true, Tools: []engine.ToolDefinition{}, Skills: []string{"reviewer"}}, nil
	})
	if err != nil || !req.ReadOnly || len(req.Tools) != 0 || req.Skills[0] != "reviewer" || req.NodeID != "reviewer" {
		t.Fatalf("lost node policy: %+v %v", req, err)
	}
}

func TestSeparateRepliesToParallelApprovalsResumeJoin(t *testing.T) {
	r := actionRuntime(t)
	d := Definition{Nodes: []Node{{ID: "start", Type: "parallel"}, {ID: "first", Type: "approval"}, {ID: "second", Type: "approval"}, {ID: "join", Type: "join"}}, Edges: []Edge{{Source: "start", Target: "first"}, {Source: "start", Target: "second"}, {Source: "first", Target: "join"}, {Source: "second", Target: "join"}}}
	req := engine.Request{Prompt: "seed"}
	first, err := r.Run(t.Context(), d, req, nil, nil, nil)
	if err != nil || len(first.Pending) != 2 {
		t.Fatalf("parallel pause: pending=%d err=%v", len(first.Pending), err)
	}
	req.Snapshot = first.Snapshot
	req.Resume = map[string]any{first.Pending[0].ID: true}
	second, err := r.Run(t.Context(), d, req, nil, nil, nil)
	if err != nil || len(second.Pending) != 1 || second.Pending[0].ID != first.Pending[1].ID {
		t.Fatalf("partial response: pending=%+v err=%v", second.Pending, err)
	}
	req.Snapshot = second.Snapshot
	req.Resume = map[string]any{second.Pending[0].ID: true}
	third, err := r.Run(t.Context(), d, req, nil, nil, nil)
	if err != nil || len(third.Pending) != 0 || !strings.Contains(third.Content, `"first":"seed"`) || !strings.Contains(third.Content, `"second":"seed"`) {
		t.Fatalf("join completion: content=%q pending=%d err=%v", third.Content, len(third.Pending), err)
	}
}
