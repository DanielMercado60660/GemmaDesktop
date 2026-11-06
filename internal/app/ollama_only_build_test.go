package app

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

// An Ollama-only bundle deliberately lacks managed resources. Exercise the
// ordinary app admission/delivery path with a local protocol fixture, never a model.
func TestOllamaOnlyBuildOrdinaryRunWithoutManagedResources(t *testing.T) {
	t.Setenv("GEMMA_RUNTIME_BUNDLES", filepath.Join(t.TempDir(), "absent-resources"))
	var requests atomic.Int32
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		requests.Add(1)
		if req.Model != "fixture" {
			t.Errorf("ordinary run selected unexpected model %q", req.Model)
		}
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Mock final delivered."},"done":true,"prompt_eval_count":8,"eval_count":4}`)
	})
	snapshot := h.core.managed.Snapshot()
	if len(snapshot.Catalog) != 0 || len(snapshot.ModelCatalog) != 0 || len(snapshot.Installed) != 0 || len(snapshot.Models) != 0 {
		t.Fatal("missing bundled catalog unexpectedly supplied managed artifacts")
	}
	if _, err := h.core.Call(t.Context(), "runtime.managed.install", Doc{"buildId": "absent-build"}); err == nil {
		t.Fatal("managed installation succeeded without a trusted bundled build")
	}
	run := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "prompt": "Return the fixture final."})
	finishedIntegration(t, h.core, str(run, "id"), "completed")
	if requests.Load() != 1 {
		t.Fatalf("ordinary run generated %d times", requests.Load())
	}
	messages, err := h.core.Store.ListScoped("message", "sessionId", h.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	delivered := false
	for _, message := range messages {
		if message["role"] == "assistant" && message["content"] == "Mock final delivered." {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("ordinary app did not persist the final answer")
	}
}
