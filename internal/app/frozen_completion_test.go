package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestFrozenCompletionRejectsTemplateRuntimeAndAliasDriftBeforeGeneration(t *testing.T) {
	var generation, drift atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			fmt.Fprintf(w, `{"version":"%d"}`, drift.Load()/2)
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"fixture","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)
		case "/api/show":
			fmt.Fprintf(w, `{"capabilities":["completion","tools"],"template":"template-%d","details":{"quantization_level":"4bit"}}`, drift.Load()%2)
		case "/api/chat":
			generation.Add(1)
			fmt.Fprintln(w, `{"done":true,"message":{"role":"assistant","content":"ok"},"prompt_eval_count":2,"eval_count":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := inference.NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{client: client}
	backend, identity, err := c.completionBackend(t.Context(), ollamaRuntimeID, "fixture", 4096)
	if err != nil {
		t.Fatal(err)
	}
	frozen := &frozenCompletion{CompletionBackend: backend, core: c, bindings: Doc{"fixture": identity}}
	req := inference.CompletionRequest{Model: "fixture", Messages: []inference.Message{{Role: "user", Content: "test"}}, Controls: inference.GenerationControls{ContextTokens: 4096, MaxOutputTokens: 256}}
	if err = frozen.Generate(t.Context(), req, func(inference.ChatChunk) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, value := range []int32{1, 2} {
		drift.Store(value)
		if err = frozen.Generate(t.Context(), req, func(inference.ChatChunk) error { return nil }); err == nil {
			t.Fatal("changed identity generated", value)
		}
	}
	if generation.Load() != 1 {
		t.Fatal("generation executed after identity drift", generation.Load())
	}
	req.Model = "unfrozen"
	if err = frozen.Generate(t.Context(), req, nil); err == nil {
		t.Fatal("unfrozen model admitted")
	}
}
