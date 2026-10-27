package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/inference"
)

// This opt-in test uses the installed model, never a substitute endpoint.
// GEMMA_LIVE_MODEL=gemma4:12b-mlx go test ./internal/engine -run TestLiveOllama
func TestLiveOllamaToolAndSnapshot(t *testing.T) {
	model := os.Getenv("GEMMA_LIVE_MODEL")
	if model == "" {
		t.Skip("set GEMMA_LIVE_MODEL to test the installed Ollama model")
	}
	client, err := inference.NewClient("http://127.0.0.1:11434")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	version, err := client.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Show(ctx, model)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Ollama %s; model %s; capabilities %v", version, model, info.Capabilities)
	e := New(client)
	calls := 0
	const secret = "EMBER-7429"
	execute := func(_ context.Context, call ToolCall) (ToolResult, error) {
		calls++
		if call.Name != "read_verification_code" || call.ID == "" {
			t.Errorf("unexpected live tool call: %+v", call)
		}
		return ToolResult{Content: secret}, nil
	}
	req := Request{Model: model, Instructions: "Use the provided tool to obtain unknown facts. Return a concise answer. Never invent a verification code.", Prompt: "Call read_verification_code exactly once, then reply with its returned code only.", Temperature: 0, Think: false, MaxSteps: 4, ContextSize: 4096, Timeout: 5 * time.Minute, Tools: []ToolDefinition{{Name: "read_verification_code", Description: "Return the local test verification code. Takes no arguments."}}}
	var textEvents int
	first, err := e.Run(ctx, req, func(ev Event) {
		if ev.Type == "text" {
			textEvents++
		}
	}, execute)
	if err != nil {
		t.Fatalf("live first turn: %v", err)
	}
	if calls != 1 || !strings.Contains(first.Content, secret) || len(first.Snapshot) == 0 || textEvents == 0 {
		t.Fatalf("tool calls=%d answer=%q snapshot bytes=%d streamed chunks=%d", calls, first.Content, len(first.Snapshot), textEvents)
	}
	req.Snapshot = first.Snapshot
	req.Tools = nil
	req.Prompt = "What exact verification code did the tool return in our previous turn? Reply only with that code."
	second, err := e.Run(ctx, req, nil, nil)
	if err != nil {
		t.Fatalf("live snapshot continuation: %v", err)
	}
	if !strings.Contains(second.Content, secret) || calls != 1 {
		t.Fatalf("snapshot continuation answer=%q tool calls=%d", second.Content, calls)
	}
	t.Logf("verified tool calls=%d; first model calls=%d; first answer=%q; continuation=%q; snapshot bytes=%d; generated tokens=%d", calls, first.Steps, first.Content, second.Content, len(second.Snapshot), first.Usage.EvalCount+second.Usage.EvalCount)
}
