package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
)

type integrationHarness struct {
	core      *Core
	dir       string
	workspace string
	workID    string
	sessionID string
	close     func()
}

func newIntegrationHarness(t *testing.T, chat func(http.ResponseWriter, *http.Request, inference.ChatRequest)) *integrationHarness {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			fmt.Fprint(w, `{"version":"test-fixture"}`)
		case "/api/tags", "/api/ps":
			fmt.Fprint(w, `{"models":[]}`)
		case "/api/show":
			fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
		case "/api/chat":
			var req inference.ChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			chat(w, r, req)
		default:
			t.Errorf("unexpected fixture endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "gemma.db"))
	if err != nil {
		t.Fatal(err)
	}
	err = s.Put("settings", Doc{"id": "default", "ollamaUrl": srv.URL, "defaultModel": "fixture", "contextSize": 4096, "maxSteps": 4, "mode": "approval", "theme": "dark"})
	s.Close()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var closeOnce sync.Once
	h := &integrationHarness{core: c, dir: dir, workspace: t.TempDir()}
	h.close = func() {
		closeOnce.Do(func() {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(h.close)
	callIntegration(t, c, "agent.save", Doc{"id": "default", "name": "Fixture", "maxSteps": 4, "mode": "approval"})
	w := callIntegration(t, c, "workspace.add", Doc{"path": h.workspace})
	h.workID = str(w, "id")
	session := callIntegration(t, c, "session.create", Doc{"workspaceId": h.workID})
	h.sessionID = str(session, "id")
	return h
}

func callIntegration(t *testing.T, c *Core, method string, p Doc) Doc {
	t.Helper()
	v, err := c.Call(t.Context(), method, p)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return obj(v)
}

func waitIntegration(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for application state")
}

func pendingIntegration(t *testing.T, c *Core, runID string) Doc {
	t.Helper()
	var found Doc
	waitIntegration(t, func() bool {
		all, err := c.Store.List("approval")
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range all {
			if a["runId"] == runID && a["status"] == "pending" {
				found = a
				return true
			}
		}
		return false
	})
	return found
}

func finishedIntegration(t *testing.T, c *Core, id, status string) Doc {
	t.Helper()
	var run Doc
	waitIntegration(t, func() bool {
		var err error
		run, err = c.Store.Get("run", id)
		if err != nil {
			t.Fatal(err)
		}
		return !active(str(run, "status")) && !c.workspaceBusy(str(run, "workspaceId"))
	})
	if run["status"] != status {
		t.Fatalf("run status=%v expected=%s error=%v", run["status"], status, run["error"])
	}
	return run
}

func writeFixture(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
	for _, m := range req.Messages {
		if m.Role == "tool" {
			fmt.Fprintln(w, `{"message":{"content":"Written and verified."},"done":true}`)
			return
		}
	}
	fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"write_once","function":{"name":"write_file","arguments":{"path":"approved.txt","content":"approved content","expectedHash":"missing"}}}]},"done":true}`)
}

func TestIntegrationApprovedToolJournalAndConversationContinuation(t *testing.T) {
	var requests atomic.Int32
	var historyFound atomic.Bool
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		requests.Add(1)
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "user" && last.Content == "Recall the prior write" {
			for _, m := range req.Messages {
				if m.Role == "tool" && m.ToolCallID == "write_once" {
					historyFound.Store(true)
				}
			}
			fmt.Fprintln(w, `{"message":{"content":"The prior write was verified."},"done":true}`)
			return
		}
		writeFixture(w, r, req)
	})
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Create approved.txt"})
	id := str(run, "id")
	a := pendingIntegration(t, h.core, id)
	if _, err := os.Stat(filepath.Join(h.workspace, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("file changed before approval: %v", err)
	}
	if _, err := h.core.Call(t.Context(), "run.start", Doc{"sessionId": h.sessionID, "prompt": "overlap"}); err == nil {
		t.Fatal("overlapping workspace execution was accepted")
	}
	intent, err := h.core.Store.Get("toolcall", id+"::write_once")
	if err != nil || intent["status"] != "started" || intent["callId"] != "write_once" {
		t.Fatalf("missing pre-effect intent: %+v %v", intent, err)
	}
	callIntegration(t, h.core, "approval.respond", Doc{"id": a["id"], "allow": true})
	if _, err := h.core.Call(t.Context(), "approval.respond", Doc{"id": a["id"], "allow": true}); err == nil {
		t.Fatal("approval was replayed")
	}
	finishedIntegration(t, h.core, id, "completed")
	data, err := os.ReadFile(filepath.Join(h.workspace, "approved.txt"))
	if err != nil || string(data) != "approved content" {
		t.Fatalf("approved write failed: %q %v", data, err)
	}
	intent, err = h.core.Store.Get("toolcall", id+"::write_once")
	if err != nil || intent["status"] != "completed" {
		t.Fatalf("completion not journaled: %+v %v", intent, err)
	}
	second := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Recall the prior write"})
	finishedIntegration(t, h.core, str(second, "id"), "completed")
	if !historyFound.Load() || requests.Load() != 3 {
		t.Fatalf("structured history lost: history=%v requests=%d", historyFound.Load(), requests.Load())
	}
}

func TestIntegrationCancellationExpiresApprovalWithoutFileEffect(t *testing.T) {
	h := newIntegrationHarness(t, writeFixture)
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Request a write"})
	id := str(run, "id")
	a := pendingIntegration(t, h.core, id)
	callIntegration(t, h.core, "run.cancel", Doc{"id": id})
	finishedIntegration(t, h.core, id, "cancelled")
	if _, err := h.core.Call(t.Context(), "approval.respond", Doc{"id": a["id"], "allow": true}); err == nil {
		t.Fatal("cancelled approval was accepted")
	}
	if _, err := os.Stat(filepath.Join(h.workspace, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("cancelled write had an effect: %v", err)
	}
	a, err := h.core.Store.Get("approval", str(a, "id"))
	if err != nil || a["status"] != "expired" {
		t.Fatalf("cancelled approval not expired: %+v %v", a, err)
	}
}

func saveApprovalWorkflow(t *testing.T, h *integrationHarness, successor string) string {
	t.Helper()
	d := Doc{"name": "Approval checkpoint", "nodes": []Doc{{"id": "prepare", "type": "output", "prompt": "prepared"}, {"id": "approve", "type": "approval", "prompt": "Continue?"}, {"id": "after", "type": successor, "prompt": "accepted"}}, "edges": []Doc{{"source": "prepare", "target": "approve"}, {"source": "approve", "target": "after"}}}
	return str(callIntegration(t, h.core, "workflow.save", d), "id")
}

func reopenIntegrationBackup(t *testing.T, h *integrationHarness) *Core {
	t.Helper()
	dir := t.TempDir()
	if err := h.core.Store.Backup(filepath.Join(dir, "gemma.db")); err != nil {
		t.Fatal(err)
	}
	h.close()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func TestIntegrationDurableApprovalRecoveryDoesNotReplayCompletedNode(t *testing.T) {
	h := newIntegrationHarness(t, func(http.ResponseWriter, *http.Request, inference.ChatRequest) {
		t.Error("output-only workflow requested inference")
	})
	wf := saveApprovalWorkflow(t, h, "output")
	run := callIntegration(t, h.core, "workflow.run", Doc{"id": wf, "sessionId": h.sessionID, "prompt": "seed"})
	id := str(run, "id")
	oldApproval := pendingIntegration(t, h.core, id)
	recovered := reopenIntegrationBackup(t, h)
	run, err := recovered.Store.Get("run", id)
	if err != nil || run["status"] != "interrupted" || run["resumable"] != true {
		t.Fatalf("lost recoverable pause: %+v %v", run, err)
	}
	if _, err := recovered.Call(t.Context(), "approval.respond", Doc{"id": oldApproval["id"], "allow": true}); err == nil {
		t.Fatal("pre-crash approval ID retained authority")
	}
	callIntegration(t, recovered, "run.resume", Doc{"id": id})
	a := pendingIntegration(t, recovered, id)
	if a["id"] == oldApproval["id"] {
		t.Fatal("recovery reused approval authority")
	}
	callIntegration(t, recovered, "approval.respond", Doc{"id": a["id"], "allow": true})
	finishedIntegration(t, recovered, id, "completed")
	if _, err := recovered.Call(t.Context(), "run.resume", Doc{"id": id}); err == nil {
		t.Fatal("completed workflow resumed again")
	}
	events, err := recovered.Store.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	prepared, after := 0, 0
	for _, ev := range events {
		if ev["type"] == "node_started" && ev["nodeId"] == "prepare" {
			prepared++
		}
		if ev["type"] == "node_started" && ev["nodeId"] == "after" {
			after++
		}
	}
	if prepared != 1 || after != 1 {
		t.Fatalf("completed nodes replayed: prepare=%d after=%d", prepared, after)
	}
}

func TestIntegrationConsumedCheckpointCannotReplayAfterRestart(t *testing.T) {
	entered := make(chan struct{}, 1)
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		entered <- struct{}{}
		<-r.Context().Done()
	})
	wf := saveApprovalWorkflow(t, h, "agent")
	run := callIntegration(t, h.core, "workflow.run", Doc{"id": wf, "sessionId": h.sessionID, "prompt": "seed"})
	id := str(run, "id")
	a := pendingIntegration(t, h.core, id)
	callIntegration(t, h.core, "approval.respond", Doc{"id": a["id"], "allow": true})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("approved successor was not dispatched")
	}
	cp, err := h.core.Store.Get("checkpoint", id)
	if err != nil {
		t.Fatal(err)
	}
	pending, _ := cp["pending"].([]any)
	run, err = h.core.Store.Get("run", id)
	if err != nil || len(pending) != 0 || run["resumable"] == true {
		t.Fatalf("consumed authority still resumable: pending=%v run=%+v err=%v", pending, run, err)
	}
	recovered := reopenIntegrationBackup(t, h)
	if _, err := recovered.Call(t.Context(), "run.resume", Doc{"id": id}); err == nil {
		t.Fatal("uncertain continuation offered unsafe replay")
	}
}

func TestIntegrationReadOnlyAgentCannotGainWriteTool(t *testing.T) {
	var advertisedWrite atomic.Bool
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		for _, d := range req.Tools {
			if d.Function.Name == "write_file" {
				advertisedWrite.Store(true)
			}
		}
		writeFixture(w, r, req)
	})
	a := callIntegration(t, h.core, "agent.save", Doc{"name": "Read-only reviewer", "mode": "read-only", "maxSteps": 1})
	wf := callIntegration(t, h.core, "workflow.save", Doc{"name": "Review", "nodes": []Doc{{"id": "review", "type": "agent", "agentId": a["id"]}}, "edges": []Doc{}})
	run := callIntegration(t, h.core, "workflow.run", Doc{"id": wf["id"], "sessionId": h.sessionID, "prompt": "Try a write"})
	finishedIntegration(t, h.core, str(run, "id"), "failed")
	if advertisedWrite.Load() {
		t.Fatal("read-only agent advertised write authority")
	}
	approvals, err := h.core.Store.List("approval")
	if err != nil || len(approvals) != 0 {
		t.Fatalf("read-only agent escalated to approval: %v %v", approvals, err)
	}
	if _, err := os.Stat(filepath.Join(h.workspace, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("read-only agent wrote: %v", err)
	}
	callIntegration(t, h.core, "settings.save", Doc{"mode": "read-only"})
	if _, err := h.core.Call(t.Context(), "file.write", Doc{"workspaceId": h.workID, "path": "manual.txt", "content": "blocked", "expectedHash": "missing"}); err == nil {
		t.Fatal("read-only settings allowed file editor write")
	}
}

func TestIntegrationEvaluationFreezesInputAndRuntime(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var first sync.Once
	var writes atomic.Bool
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		first.Do(func() {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		for _, d := range req.Tools {
			if d.Function.Name == "write_file" || d.Function.Name == "run_command" {
				writes.Store(true)
			}
		}
		for _, m := range req.Messages {
			if m.Role == "tool" {
				b, _ := json.Marshal(inference.ChatChunk{Done: true, Message: inference.Message{Content: m.Content}})
				fmt.Fprintln(w, string(b))
				return
			}
		}
		fmt.Fprintln(w, `{"message":{"tool_calls":[{"id":"read_input","function":{"name":"read_file","arguments":{"path":"input.txt"}}}]},"done":true}`)
	})
	if err := os.WriteFile(filepath.Join(h.workspace, "input.txt"), []byte("frozen original"), 0600); err != nil {
		t.Fatal(err)
	}
	a := callIntegration(t, h.core, "agent.save", Doc{"name": "Candidate", "mode": "approval", "maxSteps": 3})
	experiment := callIntegration(t, h.core, "eval.start", Doc{"workspaceId": h.workID, "baselineAgentId": "default", "candidateAgentId": a["id"], "tasks": []string{"Read input.txt"}})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation did not start")
	}
	if _, err := h.core.Call(t.Context(), "settings.save", Doc{"ollamaUrl": "http://127.0.0.1:1"}); err == nil {
		t.Error("runtime settings changed during a frozen evaluation")
	}
	if err := os.WriteFile(filepath.Join(h.workspace, "input.txt"), []byte("new source content"), 0600); err != nil {
		t.Fatal(err)
	}
	close(release)
	var completed Doc
	waitIntegration(t, func() bool {
		var err error
		completed, err = h.core.Store.Get("experiment", str(experiment, "id"))
		if err != nil {
			t.Fatal(err)
		}
		return completed["status"] != "running"
	})
	if completed["status"] != "completed" || writes.Load() {
		t.Fatalf("evaluation status=%v writes=%v", completed["status"], writes.Load())
	}
	results, _ := completed["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results=%v", results)
	}
	for _, raw := range results {
		r := obj(raw)
		if r["status"] != "completed" || !strings.Contains(str(r, "content"), "frozen original") || strings.Contains(str(r, "content"), "new source content") {
			t.Fatalf("evaluation inputs drifted: %+v", r)
		}
	}
}

func TestIntegrationExclusiveOwnershipAndShutdownCancelActiveInference(t *testing.T) {
	entered := make(chan struct{}, 1)
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		fmt.Fprintln(w, `{"message":{"content":"unfinished output"},"done":false}`)
		w.(http.Flusher).Flush()
		entered <- struct{}{}
		<-r.Context().Done()
	})
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "A long request"})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("inference did not start")
	}
	if other, err := Open(h.dir); err == nil {
		other.Close()
		t.Fatal("second application instance acquired live data")
	}
	closed := make(chan struct{})
	go func() { h.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel inference")
	}
	if err := h.core.Close(); err != nil {
		t.Fatalf("idempotent shutdown: %v", err)
	}
	reopened, err := Open(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	run, err = reopened.Store.Get("run", str(run, "id"))
	if err != nil || run["status"] != "cancelled" {
		t.Fatalf("clean cancellation lost on reopen: %+v %v", run, err)
	}
	cp, err := reopened.Store.Get("checkpoint", str(run, "id"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cp["snapshot"])
	if strings.Contains(string(b), "unfinished output") {
		t.Fatal("partial generation rehydrated as a complete assistant turn")
	}
}
