package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestIntegrationIncompleteReplyIsAuditedButNeverReplayed(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			testIncompleteReplyReplay(t, legacy)
		})
	}
}

func testIncompleteReplyReplay(t *testing.T, legacy bool) {
	t.Helper()
	const partial = "PARTIAL-ANSWER-MUST-NOT-BECOME-COMPLETE-HISTORY"
	var calls atomic.Int32
	received := make(chan inference.ChatRequest, 2)
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		received <- req
		if calls.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(inference.ChatChunk{Message: inference.Message{Content: partial}})
			fmt.Fprintln(w, `{"done":true,"done_reason":"length","eval_count":256}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"content":"A complete follow-up answer."},"done":true}`)
	})
	callIntegration(t, h.core, "agent.save", Doc{"id": "default", "name": "Bounded", "mode": "approval", "maxSteps": 4, "thinking": "off", "maxOutputTokens": 256})
	first := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Original exact user request"})
	failed := finishedIntegration(t, h.core, str(first, "id"), "failed")
	if failed["incomplete"] != true || failed["summary"] != partial || !strings.Contains(str(failed, "error"), "generation length limit") {
		t.Fatalf("partial reply lost its failed/incomplete status: %+v", failed)
	}
	page := callIntegration(t, h.core, "session.get", Doc{"id": h.sessionID})
	messages, _ := page["messages"].([]any)
	audited := false
	for _, raw := range messages {
		message := obj(raw)
		if message["runId"] == first["id"] && message["role"] == "assistant" {
			audited = message["content"] == partial && message["incomplete"] == true
		}
	}
	if !audited {
		t.Fatalf("partial text and metadata unavailable to UI/audit: %+v", messages)
	}
	if legacy {
		// An already running old binary may finish before deployment. Its
		// checkpoint contains incomplete=true, but its message/run do not.
		if _, err := h.core.Store.Update("run", str(first, "id"), func(r Doc) error { delete(r, "incomplete"); return nil }); err != nil {
			t.Fatal(err)
		}
		for _, raw := range messages {
			message := obj(raw)
			if message["runId"] == first["id"] && message["role"] == "assistant" {
				if _, err := h.core.Store.Update("message", str(message, "id"), func(m Doc) error { delete(m, "incomplete"); return nil }); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	second := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Continue with a fresh complete answer"})
	completed := finishedIntegration(t, h.core, str(second, "id"), "completed")
	if completed["incomplete"] != false {
		t.Fatalf("complete follow-up inherited incomplete flag: %+v", completed)
	}
	<-received
	followup := <-received
	priorUser, currentUser, recovery := false, false, false
	for _, message := range followup.Messages {
		if strings.Contains(message.Content, partial) {
			t.Fatalf("partial reply replayed as model context: %+v", message)
		}
		priorUser = priorUser || (message.Role == "user" && message.Content == "Original exact user request")
		currentUser = currentUser || (message.Role == "user" && message.Content == "Continue with a fresh complete answer")
		recovery = recovery || (message.Role == "system" && strings.Contains(message.Content, "Host recovery context") && strings.Contains(message.Content, "generation length limit"))
	}
	if !priorUser || !currentUser || !recovery {
		t.Fatalf("lost user requests or failure context: prior=%v current=%v recovery=%v", priorUser, currentUser, recovery)
	}
	cp, err := h.core.Store.Get("checkpoint", str(second, "id"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(cp["snapshot"])
	if strings.Contains(string(snapshot), partial) {
		t.Fatal("follow-up snapshot reintroduced incomplete content")
	}
}
