package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
)

func TestCallerBudgetOnlyNarrowsSelectedLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params Doc
		limit  time.Duration
		want   time.Duration
	}{
		{"legacy", Doc{}, 20 * time.Minute, 20 * time.Minute},
		{"explicit", Doc{"budgetSeconds": 600}, 20 * time.Minute, 10 * time.Minute},
		{"qualified_equal", Doc{"budgetSeconds": 90}, 90 * time.Second, 90 * time.Second},
		{"qualified_widen", Doc{"budgetSeconds": 91}, 90 * time.Second, 0},
		{"null", Doc{"budgetSeconds": nil}, 20 * time.Minute, 0},
		{"string", Doc{"budgetSeconds": "600"}, 20 * time.Minute, 0},
		{"fraction", Doc{"budgetSeconds": 600.5}, 20 * time.Minute, 0},
		{"nan", Doc{"budgetSeconds": math.NaN()}, 20 * time.Minute, 0},
		{"infinite", Doc{"budgetSeconds": math.Inf(1)}, 20 * time.Minute, 0},
		{"zero", Doc{"budgetSeconds": 0}, 20 * time.Minute, 0},
		{"negative", Doc{"budgetSeconds": -1}, 20 * time.Minute, 0},
		{"too_small", Doc{"budgetSeconds": 29}, 20 * time.Minute, 0},
		{"too_large", Doc{"budgetSeconds": 1201}, 20 * time.Minute, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := narrowedRunTimeout(tc.params, tc.limit)
			if tc.want == 0 {
				if err == nil {
					t.Fatalf("invalid/widening budget admitted: %v", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("budget=%v err=%v", got, err)
			}
		})
	}
}

func TestFinishReserveControlsAreExplicitAndBounded(t *testing.T) {
	for _, value := range []any{-1, 121, 0.5, "60", nil, math.NaN(), math.Inf(1)} {
		if _, err := agentGenerationControls(Doc{"finishReserveSeconds": value}); err == nil {
			t.Fatalf("invalid reserve accepted: %v", value)
		}
	}
	legacy, err := agentGenerationControls(Doc{})
	if err != nil || legacy.finishReserveSeconds != 0 {
		t.Fatal("legacy reserve changed", legacy, err)
	}
	for _, seconds := range []int{0, 60, 120} {
		controls, err := agentGenerationControls(Doc{"finishReserveSeconds": seconds})
		if err != nil {
			t.Fatal(err)
		}
		var request engine.Request
		controls.apply(&request)
		if request.FinishReserveSeconds != seconds {
			t.Fatal("reserve did not reach request", request)
		}
	}
}

func TestIntegrationCallerBudgetAndReserveFreezeAtAdmission(t *testing.T) {
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, _ inference.ChatRequest) {
		fmt.Fprintln(w, `{"message":{"content":"Observed answer."},"done":true}`)
	})
	callIntegration(t, h.core, "agent.save", Doc{"id": "default", "name": "Bounded", "mode": "approval", "maxSteps": 4, "finishReserveSeconds": 60})
	saved, err := h.core.Store.Get("agent", "default")
	if err != nil || number(saved, "finishReserveSeconds", -1) != 60 {
		t.Fatal("reserve not saved", saved, err)
	}
	before := time.Now()
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Inspect only.", "budgetSeconds": 600})
	finishedIntegration(t, h.core, str(run, "id"), "completed")
	cp, err := h.core.Store.Get("checkpoint", str(run, "id"))
	if err != nil {
		t.Fatal(err)
	}
	var request engine.Request
	b, _ := json.Marshal(cp["request"])
	if err = json.Unmarshal(b, &request); err != nil {
		t.Fatal(err)
	}
	if request.FinishReserveSeconds != 60 || request.Deadline.Before(before.Add(600*time.Second)) || request.Deadline.After(time.Now().Add(600*time.Second)) || number(run, "budgetSeconds", 0) != 600 {
		t.Fatalf("admitted bound not frozen: run=%+v request=%+v", run, request)
	}
	if _, err = h.core.Call(t.Context(), "run.start", Doc{"sessionId": h.sessionID, "prompt": "No widening", "budgetSeconds": "600"}); err == nil {
		t.Fatal("string budget admitted")
	}
	if _, err = h.core.Call(t.Context(), "run.start", Doc{"sessionId": h.sessionID, "prompt": "Reserve cannot consume entire task", "budgetSeconds": 30}); err == nil {
		t.Fatal("reserve larger than budget admitted")
	}
	// Old checkpoints have no reserve field. Deserialization leaves it disabled;
	// the persisted absolute deadline continues to be the limit after restart.
	legacy := engine.Request{}
	if err = json.Unmarshal([]byte(`{"deadline":"2020-01-01T00:00:00Z"}`), &legacy); err != nil || legacy.FinishReserveSeconds != 0 {
		t.Fatal("legacy request changed", legacy, err)
	}
	if err = h.core.Store.Put("run", Doc{"id": "expired-budget", "status": "interrupted", "resumable": true}); err != nil {
		t.Fatal(err)
	}
	if err = h.core.Store.Put("checkpoint", Doc{"id": "expired-budget", "request": legacy, "pending": []engine.PendingInput{{ID: "approval", Message: "Continue?"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.core.resumeRun("expired-budget"); err == nil || !strings.Contains(err.Error(), "deadline has expired") {
		t.Fatal("resume reset an expired persisted deadline", err)
	}
}
