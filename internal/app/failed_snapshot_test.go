package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
)

func TestIntegrationTwoFailedTurnsRetainSummaryAndCompletedToolPairs(t *testing.T) {
	const summary = "DURABLE-SUMMARY-ORCHID: earlier constraints remain active."
	const partial = "INCOMPLETE-TERMINAL-ANSWER-MUST-NOT-REPLAY"
	var ordinaryCalls, summaryCalls atomic.Int32
	seen := make(chan inference.ChatRequest, 4)
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		for _, message := range req.Messages {
			if strings.Contains(message.Content, "Create a factual continuity summary for Gemma Desktop") {
				summaryCalls.Add(1)
				_ = json.NewEncoder(w).Encode(inference.ChatChunk{Message: inference.Message{Content: summary}, Done: true, Metrics: inference.Metrics{PromptEvalCount: 500, EvalCount: 20}})
				return
			}
		}
		seen <- req
		switch ordinaryCalls.Add(1) {
		case 1:
			writeFixture(w, r, req)
		case 2:
			http.Error(w, "interrupted after committed write", http.StatusServiceUnavailable)
		case 3:
			_ = json.NewEncoder(w).Encode(inference.ChatChunk{Message: inference.Message{Content: partial}, Done: true, DoneReason: "length", Metrics: inference.Metrics{PromptEvalCount: 1000, EvalCount: 256}})
		default:
			fmt.Fprintln(w, `{"message":{"content":"Earlier observed work retained without repeating it."},"done":true}`)
		}
	})
	for i := 0; i < 8; i++ {
		for _, role := range []string{"user", "assistant"} {
			text := fmt.Sprintf("Prior %s turn %d: ", role, i) + strings.Repeat("historical evidence ", 50)
			if err := h.core.Store.Put("message", Doc{"id": store.ID(), "sessionId": h.sessionID, "runId": "historical", "role": role, "content": text, "createdAt": store.Now()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	first := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Create approved.txt"})
	approval := pendingIntegration(t, h.core, str(first, "id"))
	callIntegration(t, h.core, "approval.respond", Doc{"id": approval["id"], "allow": true})
	finishedIntegration(t, h.core, str(first, "id"), "failed")
	second := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Review the committed evidence without repeating it"})
	finishedIntegration(t, h.core, str(second, "id"), "failed")
	third := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Try the review again with a complete answer"})
	finishedIntegration(t, h.core, str(third, "id"), "completed")
	if ordinaryCalls.Load() != 4 || summaryCalls.Load() != 1 {
		t.Fatalf("unexpected model/summary calls: %d/%d", ordinaryCalls.Load(), summaryCalls.Load())
	}
	<-seen
	<-seen
	for i, prompt := range []string{"Review the committed evidence without repeating it", "Try the review again with a complete answer"} {
		req := <-seen
		call, response, hasSummary, currentUser, recovery := false, false, false, false, false
		for _, message := range req.Messages {
			for _, proposal := range message.ToolCalls {
				call = call || proposal.ID == "write_once"
			}
			response = response || message.Role == "tool" && message.ToolCallID == "write_once"
			hasSummary = hasSummary || strings.Contains(message.Content, summary)
			currentUser = currentUser || message.Role == "user" && message.Content == prompt
			recovery = recovery || message.Role == "system" && strings.Contains(message.Content, "Host recovery context")
			if strings.Contains(message.Content, partial) {
				t.Fatal("incomplete generation re-entered model context")
			}
		}
		if !call || !response || !hasSummary || !currentUser || !recovery {
			t.Fatalf("retry%d lost observations: call=%v response=%v summary=%v current=%v recovery=%v", i+1, call, response, hasSummary, currentUser, recovery)
		}
	}
	for _, run := range []Doc{second, third} {
		calls, err := h.core.Store.ForRun("toolcall", str(run, "id"))
		if err != nil || len(calls) != 0 {
			t.Fatalf("historical tool executed again: calls=%v err=%v", calls, err)
		}
		cp, err := h.core.Store.Get("checkpoint", str(run, "id"))
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _ := json.Marshal(cp["snapshot"])
		if !strings.Contains(string(snapshot), summary) || !strings.Contains(string(snapshot), `"compaction"`) || strings.Contains(string(snapshot), partial) {
			t.Fatal("retry snapshot lost its summary or included incomplete terminal output")
		}
	}
	content, err := os.ReadFile(filepath.Join(h.workspace, "approved.txt"))
	if err != nil || string(content) != "approved content" {
		t.Fatalf("committed file changed during observation-only retries: %q %v", content, err)
	}
}

func TestIntegrationCancelledProposalIsNotExecutedOnNewTurn(t *testing.T) {
	var calls atomic.Int32
	var unpairedCallReplayed atomic.Bool
	h := newIntegrationHarness(t, func(w http.ResponseWriter, r *http.Request, req inference.ChatRequest) {
		if calls.Add(1) == 1 {
			writeFixture(w, r, req)
			return
		}
		for _, message := range req.Messages {
			for _, call := range message.ToolCalls {
				if call.ID == "write_once" {
					unpairedCallReplayed.Store(true)
				}
			}
		}
		fmt.Fprintln(w, `{"message":{"content":"The cancelled proposal has no approval authority."},"done":true}`)
	})
	first := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Request a write"})
	approval := pendingIntegration(t, h.core, str(first, "id"))
	callIntegration(t, h.core, "run.cancel", Doc{"id": first["id"]})
	finishedIntegration(t, h.core, str(first, "id"), "cancelled")
	next := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Explain the cancellation without writing anything"})
	finishedIntegration(t, h.core, str(next, "id"), "completed")
	if unpairedCallReplayed.Load() || calls.Load() != 2 {
		t.Fatalf("unanswered proposal replayed=%v modelCalls=%d", unpairedCallReplayed.Load(), calls.Load())
	}
	if _, err := os.Stat(filepath.Join(h.workspace, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("cancelled proposal had a file effect: %v", err)
	}
	if _, err := h.core.Call(t.Context(), "approval.respond", Doc{"id": approval["id"], "allow": true}); err == nil {
		t.Fatal("old approval regained authority")
	}
	journal, err := h.core.Store.ForRun("toolcall", str(next, "id"))
	if err != nil || len(journal) != 0 {
		t.Fatalf("new turn executed old proposal: %+v %v", journal, err)
	}
}

func TestConversationSnapshotEligibility(t *testing.T) {
	valid := Doc{"version": 1, "events": []Doc{{"author": "user", "content": Doc{"role": "user", "parts": []Doc{{"text": "Preserved request"}}}}}, "state": Doc{}}
	for _, status := range []string{"completed", "failed", "cancelled", "interrupted"} {
		if len(conversationSnapshot(Doc{"status": status}, Doc{"snapshot": valid})) == 0 {
			t.Fatalf("ordinary terminal status %s rejected", status)
		}
	}
	for _, tc := range []struct {
		name string
		run  Doc
		cp   Doc
	}{
		{"active", Doc{"status": "running"}, Doc{"snapshot": valid}},
		{"workflow run", Doc{"status": "failed", "workflowId": "workflow"}, Doc{"snapshot": valid}},
		{"workflow checkpoint", Doc{"status": "failed"}, Doc{"snapshot": valid, "workflow": Doc{}}},
		{"pending approval", Doc{"status": "failed"}, Doc{"snapshot": valid, "pending": []Doc{{"id": "authority"}}}},
		{"invalid pending", Doc{"status": "failed"}, Doc{"snapshot": valid, "pending": "not-a-list"}},
		{"missing snapshot", Doc{"status": "failed"}, Doc{}},
		{"unsupported version", Doc{"status": "failed"}, Doc{"snapshot": Doc{"version": 2, "events": []Doc{{}}}}},
		{"empty snapshot", Doc{"status": "failed"}, Doc{"snapshot": Doc{"version": 1, "events": []Doc{}}}},
		{"invalid events", Doc{"status": "failed"}, Doc{"snapshot": Doc{"version": 1, "events": "invalid"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(conversationSnapshot(tc.run, tc.cp)) != 0 {
				t.Fatal("ineligible execution checkpoint entered conversation history")
			}
		})
	}
}
