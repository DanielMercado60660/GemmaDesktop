package app

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

// A budget-exhausted run shows the model's status report as a complete
// message and failed-run summary, while the next turn restores only the
// observed work from its snapshot plus recovery context, as any failed run.
func TestIntegrationBudgetReportIsShownButNotReplayed(t *testing.T) {
	const report = "BUDGET-STATUS: listed the workspace twice; the failing test is still unfixed."
	var mu sync.Mutex
	var requests []inference.ChatRequest
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		mu.Lock()
		requests = append(requests, req)
		n := len(requests)
		mu.Unlock()
		notice, followup := false, false
		for _, message := range req.Messages {
			notice = notice || message.Role == "system" && strings.Contains(message.Content, "Host budget notice")
			followup = followup || message.Role == "user" && message.Content == "Continue"
		}
		switch {
		case notice:
			fmt.Fprintf(w, `{"message":{"content":%q},"done":true}`+"\n", report)
		case followup:
			fmt.Fprintln(w, `{"message":{"content":"Continuing from the observed listing."},"done":true}`)
		default:
			fmt.Fprintf(w, `{"message":{"tool_calls":[{"id":"list-%d","function":{"name":"list_files","arguments":{}}}]},"done":true}`+"\n", n)
		}
	})
	callIntegration(t, h.core, "agent.save", Doc{"id": "default", "name": "Bounded", "mode": "approval", "maxSteps": 2, "thinking": "off"})
	first := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Fix the failing test"})
	failed := finishedIntegration(t, h.core, str(first, "id"), "failed")
	if failed["summary"] != report || failed["incomplete"] != false || !strings.Contains(str(failed, "error"), "execution budget exceeded") || !strings.Contains(str(failed, "error"), "final step was a status report") {
		t.Fatalf("budget report not kept as the failed run's complete summary: %+v", failed)
	}
	page := callIntegration(t, h.core, "session.get", Doc{"id": h.sessionID})
	messages, _ := page["messages"].([]any)
	shown := false
	for _, raw := range messages {
		message := obj(raw)
		if message["runId"] == first["id"] && message["role"] == "assistant" {
			shown = message["content"] == report && message["incomplete"] == false
		}
	}
	if !shown {
		t.Fatalf("budget report missing from the conversation: %+v", messages)
	}
	second := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Continue"})
	finishedIntegration(t, h.core, str(second, "id"), "completed")
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("expected two ordinary calls, one report and one follow-up: %d", len(requests))
	}
	observed, recovery := false, false
	for _, message := range requests[3].Messages {
		if strings.Contains(message.Content, "BUDGET-STATUS") || strings.Contains(message.Content, "Host budget notice") {
			t.Fatalf("budget report replayed as model context: %+v", message)
		}
		observed = observed || message.Role == "tool" && message.ToolCallID == "list-2"
		recovery = recovery || message.Role == "system" && strings.Contains(message.Content, "Host recovery context") && strings.Contains(message.Content, "execution budget exceeded")
	}
	if !observed || !recovery {
		t.Fatalf("follow-up lost observed work or failure context: observed=%v recovery=%v", observed, recovery)
	}
}
