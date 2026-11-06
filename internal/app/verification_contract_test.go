package app

import (
	"fmt"
	"net/http"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestIntegrationOptionalVerificationContractFrozenAndExposedExactlyOnce(t *testing.T) {
	got := make(chan inference.ChatRequest, 1)
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		select {
		case got <- req:
		default:
		}
		fmt.Fprintln(w, `{"done":true,"message":{"content":"Agent finished without verification."}}`)
	})
	contract := Doc{"command": []string{"/usr/bin/true"}, "protectedPaths": []string{"README.md"}, "requireRegressionTest": true}
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Inspect the task", "verificationContract": contract})
	contract["command"] = []string{"changed-after-admission"}
	// The agent changed nothing, so the host runs no completion check and asks
	// for no approval; finishing still never counts as verification.
	finished := finishedIntegration(t, h.core, str(run, "id"), "completed")
	if finished["verification"] != "unverified" {
		t.Fatal("agent completion counted as verification", finished)
	}
	approvals, err := h.core.Store.List("approval")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range approvals {
		if a["runId"] == run["id"] {
			t.Fatal("unchanged run requested a host verification approval", a)
		}
	}
	req := <-got
	count := 0
	for _, tool := range req.Tools {
		if tool.Function.Name == "verify_task" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("verification tool declared %d times", count)
	}
	cp, err := h.core.Store.Get("checkpoint", str(run, "id"))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeVerificationContract(cp["verificationContract"])
	if err != nil || frozen.Command[0] != "/usr/bin/true" || !frozen.RequireRegressionTest {
		t.Fatal("contract changed", frozen, err)
	}
}
func TestFocusedDraftReferenceUsesRevisionAndClearsWithSubmission(t *testing.T) {
	c := editorTestCore(t)
	v, err := c.draftCall("draft.save", Doc{"workspaceId": "w", "sessionId": "s", "text": "Continue narrowly", "focusedRunId": "prior-run", "expectedRevision": 0})
	if err != nil {
		t.Fatal(err)
	}
	if str(obj(v), "focusedRunId") != "prior-run" {
		t.Fatal(v)
	}
	d, cleared, err := c.clearSubmittedDraft("w", "s", 1)
	if err != nil || !cleared || str(d, "focusedRunId") != "" {
		t.Fatal(d, cleared, err)
	}
}
