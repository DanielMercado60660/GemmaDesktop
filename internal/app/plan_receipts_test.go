package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/tools"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDurableToolReceiptsReachModelAndLinkPlanWithoutReplayingTools(t *testing.T) {
	var step atomic.Int32
	var h *integrationHarness
	var readReceipt, failedReceipt string
	findReceipt := func(content string) string {
		t.Helper()
		match := regexp.MustCompile(`"?receiptId"?\s*:\s*"([^"]+)"`).FindStringSubmatch(content)
		if len(match) != 2 {
			t.Errorf("model-visible receipt absent: %s", content)
			return "missing"
		}
		return match[1]
	}
	h = newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		call := func(id, name string, args Doc) {
			_ = json.NewEncoder(w).Encode(Doc{"message": Doc{"tool_calls": []Doc{{"id": id, "function": Doc{"name": name, "arguments": args}}}}, "done": true})
		}
		last := req.Messages[len(req.Messages)-1].Content
		switch step.Add(1) {
		case 1:
			call("read", "read_file", Doc{"path": "source.txt"})
		case 2:
			readReceipt = findReceipt(last)
			if !strings.HasSuffix(last, "original source\n") {
				t.Error("literal source payload changed", last)
			}
			_ = os.WriteFile(filepath.Join(h.workspace, "source.txt"), []byte("external edit\n"), 0600)
			call("read", "read_file", Doc{"path": "source.txt"})
		case 3:
			if findReceipt(last) != readReceipt || !strings.HasSuffix(last, "original source\n") {
				t.Error("cached read was replayed or lost receipt", last)
			}
			call("failed", "read_file", Doc{"path": "missing.txt"})
		case 4:
			failedReceipt = findReceipt(last)
			_ = os.WriteFile(filepath.Join(h.workspace, "missing.txt"), []byte("must not be read by replay"), 0600)
			call("failed", "read_file", Doc{"path": "missing.txt"})
		case 5:
			if findReceipt(last) != failedReceipt || !strings.Contains(last, "cannot be replayed") || strings.Contains(last, "must not be read by replay") {
				t.Error("failed cached outcome was replayed or lost receipt", last)
			}
			call("plan", "plan_update", Doc{"revision": 0, "items": []Doc{{"id": "inspect", "title": "Inspect source and missing path", "status": "completed", "receiptIds": []string{readReceipt, failedReceipt}}}})
		case 6:
			if strings.Contains(last, `"receiptId":`) {
				t.Error("unrecorded plan tool acquired a receipt", last)
			}
			fmt.Fprintln(w, `{"message":{"content":"Recorded the observations."},"done":true}`)
		default:
			t.Error("unexpected extra model call")
			fmt.Fprintln(w, `{"message":{"content":"Stop."},"done":true}`)
		}
	})
	_ = os.WriteFile(filepath.Join(h.workspace, "source.txt"), []byte("original source\n"), 0600)
	callIntegration(t, h.core, "agent.save", Doc{"id": "default", "name": "Fixture", "maxSteps": 8, "mode": "approval"})
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Inspect and link evidence"})
	finishedIntegration(t, h.core, str(run, "id"), "completed")
	plan := callIntegration(t, h.core, "plan.get", Doc{"runId": run["id"]})
	observations := obj(plan["observations"])
	if len(observations) != 2 || obj(observations[readReceipt])["status"] != "completed" || obj(observations[failedReceipt])["status"] != "failed" {
		t.Fatal("plan observations not linked", plan)
	}
	rows, _ := h.core.Store.List("toolcall")
	if len(rows) != 2 || step.Load() != 6 {
		t.Fatal("durable calls were duplicated", rows, step.Load())
	}
	for _, row := range rows {
		if str(row, "id") != readReceipt && str(row, "id") != failedReceipt {
			t.Fatal("receipt did not identify actual journal", row)
		}
		if _, exists := obj(obj(row["result"])["data"])["receiptId"]; exists {
			t.Fatal("model metadata changed original stored tool payload")
		}
	}
}

func TestPlanReceiptScopeAndPayloadPreservation(t *testing.T) {
	c := milestoneCore(t)
	_ = c.Store.Put("run", Doc{"id": "own"})
	_ = c.Store.Put("toolcall", Doc{"id": "foreign", "runId": "other", "tool": "secret-foreign-tool", "status": "completed"})
	for _, id := range []string{"foreign", "unknown"} {
		if _, err := c.updateTaskPlan("own", Doc{"revision": 0, "items": []PlanItem{{ID: "check", Title: "Check", Status: "completed", ReceiptIDs: []string{id}}}}); err == nil {
			t.Fatal("out-of-scope receipt linked", id)
		}
	}
	// Even a historical/corrupt link cannot disclose another run at read time.
	_ = c.Store.Put("taskplan", Doc{"id": "own", "items": []PlanItem{{ID: "old", Title: "Old", Status: "completed", ReceiptIDs: []string{"foreign"}}}})
	view, err := c.taskPlanView("own")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(view)
	if strings.Contains(string(b), "secret-foreign-tool") || obj(obj(view["observations"])["foreign"])["status"] != "unavailable" {
		t.Fatal("foreign observation disclosed", view)
	}
	original := Doc{"content": "literal\nsource\\path", "receiptId": "forged"}
	encoded, _ := json.Marshal(original)
	got := toolResultWithReceipt(tools.Result{Content: string(encoded), Data: original}, "host:node:call", errors.New("failed"))
	if !got.IsError || got.Data["receiptId"] != "host:node:call" || got.Data["content"] != original["content"] || original["receiptId"] != "forged" || got.Content != "" {
		t.Fatal("receipt augmentation changed payload or authority", got, original)
	}
	got = toolResultWithReceipt(tools.Result{Content: "distinct diagnostic", Data: original}, "host", nil)
	if got.Content != "distinct diagnostic" {
		t.Fatal("distinct content lost")
	}
	if toolResultWithReceipt(tools.Result{}, "empty", nil).Data["receiptId"] != "empty" {
		t.Fatal("empty result receipt missing")
	}
}
