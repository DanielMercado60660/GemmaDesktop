package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nativeFixture(t *testing.T, provider ProviderKind, endpoint string, gate *AdmissionGate) *NativeBackend {
	t.Helper()
	config := NativeConfig{BaseURL: endpoint, APIKey: "fixture-secret", Gate: gate, Descriptor: BackendDescriptor{RuntimeID: "fixture", BuildID: "test-build"}, Models: []CompletionModel{{Name: "fixture", ArtifactDigest: strings.Repeat("a", 64), ContextTokens: 32768, Capabilities: CompletionCapabilities{Completion: true, Tools: true, Thinking: true, StructuredOutput: true}}}}
	var backend *NativeBackend
	var err error
	if provider == ProviderOMLX {
		backend, err = NewOMLXBackend(config)
	} else {
		backend, err = NewMLXServeBackend(config)
	}
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func nativeRequest() CompletionRequest {
	return CompletionRequest{Model: "fixture", Messages: []Message{{Role: "user", Content: "hello"}}, Controls: GenerationControls{ContextTokens: 32768, MaxOutputTokens: 256, Thinking: ThinkingOff}}
}

func sseEvent(data string) string { return "data: " + data + "\n\n" }
func nativeFinish(reason string) string {
	return sseEvent(`{"choices":[{"index":0,"delta":{},"finish_reason":"` + reason + `"}]}`)
}

func TestNativeFragmentedStreamPreservesToolsThinkingAndUsage(t *testing.T) {
	for _, provider := range []ProviderKind{ProviderOMLX, ProviderMLXServe} {
		t.Run(string(provider), func(t *testing.T) {
			var wire map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-secret" {
					t.Error("wrong native endpoint or authentication")
				}
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				body := ": heartbeat\r\n\r\n" + sseEvent(`{"choices":[{"index":0,"delta":{"reasoning_content":"check this"}}]}`) + sseEvent(`{"choices":[{"index":0,"delta":{"content":"こんにちは"}}]}`) + sseEvent(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-one","type":"function","function":{"name":"read","arguments":"{\"pa"}}]}}]}`) + sseEvent(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a\"}"}}]}}]}`) + nativeFinish("tool_calls") + sseEvent(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":6},"prompt_eval_duration":0.2,"generation_duration":0.3}}`) + sseEvent("[DONE]")
				for i := 0; i < len(body); i += 7 {
					_, _ = w.Write([]byte(body[i:min(i+7, len(body))]))
					w.(http.Flusher).Flush()
				}
			}))
			defer server.Close()
			backend := nativeFixture(t, provider, server.URL, nil)
			req := nativeRequest()
			req.Messages = append(req.Messages, Message{Role: "assistant", Thinking: "retain current cycle", ToolCalls: []ToolCall{{ID: "prior", Function: ToolFunction{Name: "read", Arguments: map[string]any{"path": "old"}}}}}, Message{Role: "tool", ToolCallID: "prior", ToolName: "read", Content: "old result"})
			var text, thinking string
			var calls []ToolCall
			var receipt ChatChunk
			receipts := 0
			err := backend.Generate(t.Context(), req, func(chunk ChatChunk) error {
				text += chunk.Message.Content
				thinking += chunk.Message.Thinking
				if len(chunk.Message.ToolCalls) > 0 && !chunk.Done {
					t.Fatal("partial tool call escaped")
				}
				calls = append(calls, chunk.Message.ToolCalls...)
				return nil
			}, func(c ChatChunk) { receipts++; receipt = c })
			if err != nil {
				t.Fatal(err)
			}
			if text != "こんにちは" || thinking != "check this" || len(calls) != 1 || calls[0].ID != "call-one" || calls[0].Function.Arguments["path"] != "a" {
				t.Fatalf("lost stream data: %q %q %+v", text, thinking, calls)
			}
			if receipts != 1 || !receipt.TokenCountsPresent || receipt.PromptEvalCount != 10 || receipt.EvalCount != 4 || receipt.UsageDetails.CachedPromptTokens == nil || *receipt.UsageDetails.CachedPromptTokens != 6 {
				t.Fatalf("incorrect terminal usage: count=%d %+v", receipts, receipt)
			}
			if provider == ProviderOMLX && receipt.PromptEvalDuration != int64(200*time.Millisecond) || provider == ProviderMLXServe && receipt.PromptEvalDuration != 0 {
				t.Fatalf("provider timing mapping incorrect: %+v", receipt.Metrics)
			}
			messages := wire["messages"].([]any)
			assistant := messages[1].(map[string]any)
			if assistant["reasoning_content"] != "retain current cycle" || messages[2].(map[string]any)["tool_call_id"] != "prior" {
				t.Fatalf("tool-cycle history was altered: %+v", messages)
			}
			if assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"] != `{"path":"old"}` {
				t.Fatalf("tool history arguments are not native JSON strings: %+v", assistant)
			}
			if wire["max_tokens"] != float64(256) || wire["options"] != nil || wire["num_ctx"] != nil {
				t.Fatalf("wrong native controls: %+v", wire)
			}
			if provider == ProviderOMLX && wire["chat_template_kwargs"].(map[string]any)["enable_thinking"] != false || provider == ProviderMLXServe && wire["enable_thinking"] != false {
				t.Fatalf("thinking-off override lost: %+v", wire)
			}
		})
	}
}

func TestNativeInvalidStreamsNeverEmitToolAuthority(t *testing.T) {
	tool := sseEvent(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{}"}}]}}]}`)
	usage := sseEvent(`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2}}`)
	for _, test := range []struct {
		name, body  string
		wantReceipt bool
	}{
		{"missing finish", tool + sseEvent("[DONE]"), false},
		{"missing done", tool + nativeFinish("tool_calls") + usage, true},
		{"partial SSE event", tool + nativeFinish("tool_calls") + usage + "data: [DONE]", true},
		{"malformed arguments", strings.Replace(tool, `"arguments":"{}"`, `"arguments":"{"`, 1) + nativeFinish("tool_calls") + usage + sseEvent("[DONE]"), true},
		{"argument array", strings.Replace(tool, `"arguments":"{}"`, `"arguments":"[]"`, 1) + nativeFinish("tool_calls") + usage + sseEvent("[DONE]"), true},
		{"missing ID", strings.Replace(tool, `"id":"c1",`, "", 1) + nativeFinish("tool_calls") + usage + sseEvent("[DONE]"), true},
		{"changing ID", tool + sseEvent(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c2"}]}}]}`), false},
		{"duplicate ID", tool + sseEvent(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"c1","function":{"name":"read","arguments":"{}"}}]}}]}`) + nativeFinish("tool_calls") + usage + sseEvent("[DONE]"), true},
		{"wrong finish", tool + nativeFinish("stop") + usage + sseEvent("[DONE]"), true},
		{"post done data", tool + nativeFinish("tool_calls") + usage + sseEvent("[DONE]") + tool, true},
		{"repetition", tool + sseEvent(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls","finish_details":{"type":"repetition_loop"}}]}`) + usage + sseEvent("[DONE]"), true},
		{"provider error", tool + sseEvent(`{"error":{"code":"invalid_tool_call","message":"bad call"}}`), false},
		{"terminal error receipt", sseEvent(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2},"error":{"message":"generation failed"}}`), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			backend := nativeFixture(t, ProviderOMLX, server.URL, nil)
			toolCalls, receipts := 0, 0
			err := backend.Generate(t.Context(), nativeRequest(), func(chunk ChatChunk) error { toolCalls += len(chunk.Message.ToolCalls); return nil }, func(receipt ChatChunk) {
				receipts++
				if !receipt.TokenCountsPresent || receipt.PromptEvalCount != 5 || receipt.EvalCount != 2 {
					t.Errorf("valid usage lost on invalid stream: %+v", receipt)
				}
			})
			if err == nil || toolCalls != 0 || (receipts == 1) != test.wantReceipt {
				t.Fatalf("err=%v calls=%d receipts=%d", err, toolCalls, receipts)
			}
		})
	}
}

func TestNativeCountsPresenceAndLength(t *testing.T) {
	for _, usage := range []struct {
		name, body string
		known      bool
	}{{"zero", `{"prompt_tokens":0,"completion_tokens":0}`, true}, {"missing", `{}`, false}, {"partial", `{"prompt_tokens":3}`, false}} {
		t.Run(usage.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, sseEvent(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"unfinished","function":{"name":"edit","arguments":"{"}}]}}]}`)+nativeFinish("length")+sseEvent(`{"choices":[],"usage":`+usage.body+`}`)+sseEvent("[DONE]"))
			}))
			defer server.Close()
			backend := nativeFixture(t, ProviderMLXServe, server.URL, nil)
			var receipt ChatChunk
			err := backend.Generate(t.Context(), nativeRequest(), func(chunk ChatChunk) error {
				if len(chunk.Message.ToolCalls) != 0 {
					t.Fatal("length-limited tool call escaped")
				}
				return nil
			}, func(c ChatChunk) { receipt = c })
			if err != nil || !receipt.Done || receipt.DoneReason != "length" || receipt.TokenCountsPresent != usage.known {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
		})
	}
}

func TestNativeCancellationAndSharedOllamaAdmission(t *testing.T) {
	started := make(chan struct{})
	closed := make(chan struct{})
	var ollamaRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sseEvent(`{"choices":[{"index":0,"delta":{"content":"one"}}]}`))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			close(closed)
			return
		}
		ollamaRequests.Add(1)
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprintln(w, `{"done":true}`)
	}))
	defer server.Close()
	gate := NewAdmissionGate()
	backend := nativeFixture(t, ProviderOMLX, server.URL, gate)
	ollama, _ := NewClientWithGate(server.URL, gate)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- backend.Generate(ctx, nativeRequest(), nil) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("native stream did not start")
	}
	queuedCtx, queuedCancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer queuedCancel()
	if err := ollama.Chat(queuedCtx, ChatRequest{Model: "fixture"}, nil); !errors.Is(err, context.DeadlineExceeded) || ollamaRequests.Load() != 0 {
		t.Fatalf("shared admission failed: requests=%d err=%v", ollamaRequests.Load(), err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native request ignored cancellation")
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("native HTTP body remained open")
	}
	if err := ollama.Chat(t.Context(), ChatRequest{Model: "fixture"}, nil); err != nil {
		t.Fatalf("shared admission leaked: %v", err)
	}
}

func TestNativePreflightAndOriginPolicy(t *testing.T) {
	for _, origin := range []string{"", "https://example.com", "http://localhost.evil", "http://127.0.0.1/v1", "http://user:secret@127.0.0.1", "http://192.168.1.1"} {
		if _, err := NewOMLXBackend(NativeConfig{BaseURL: origin}); err == nil {
			t.Errorf("accepted origin %q", origin)
		}
	}
	backend := nativeFixture(t, ProviderOMLX, "http://127.0.0.1:1", nil)
	req := nativeRequest()
	req.Controls.ContextTokens = 8192
	if err := backend.Generate(t.Context(), req, nil); !errors.Is(err, ErrContextConfiguration) {
		t.Fatal(err)
	}
	req = nativeRequest()
	req.Model = "not-installed"
	if err := backend.Generate(t.Context(), req, nil); err == nil || !strings.Contains(err.Error(), "artifact catalog") {
		t.Fatal(err)
	}
	req = nativeRequest()
	req.Messages[0].Content = strings.Repeat("x", MaxTextRequestBytes)
	if err := backend.Generate(t.Context(), req, nil); !errors.Is(err, ErrTextRequestTooLarge) {
		t.Fatal(err)
	}
}

func TestSSEMultilineAndBoundedEvents(t *testing.T) {
	input := "event: message\r\ndata: {\r\ndata: \"ok\": true}\r\n\r\n"
	var got string
	if err := scanSSE(t.Context(), strings.NewReader(input), func(data []byte) error { got = string(data); return nil }); err != nil || got != "{\n\"ok\": true}" {
		t.Fatalf("%q %v", got, err)
	}
	oversized := "data: " + strings.Repeat("a", maxStreamLine)
	if err := scanSSE(t.Context(), strings.NewReader(oversized), func([]byte) error { t.Fatal("oversized event delivered"); return nil }); err == nil {
		t.Fatal("unbounded SSE event accepted")
	}
}

func TestNativeToolNumbersNeverSilentlyRound(t *testing.T) {
	for _, tc := range []struct {
		number string
		valid  bool
	}{
		{"9007199254740993", false}, {"9007199254740992", true},
		{"-9007199254740993", false}, {"0.1", true},
		{"0.123456789012345678901", false}, {"1e-9999999999999", false},
		{"1e20", true}, {"0e9999999999999", true},
	} {
		t.Run(tc.number, func(t *testing.T) {
			arguments := `{"nested":[{"value":` + tc.number + `}]}`
			fragment := &toolFragments{id: "call-1"}
			fragment.name.WriteString("write")
			fragment.arguments.WriteString(arguments)
			state := nativeStream{reason: "tool_calls", calls: map[int]*toolFragments{0: fragment}}
			calls, err := state.completeCalls()
			if tc.valid && (err != nil || len(calls) != 1) {
				t.Fatalf("safe number rejected: %v", err)
			}
			if !tc.valid && (err == nil || len(calls) != 0) {
				t.Fatalf("unsafe argument gained tool authority: %+v %v", calls, err)
			}
		})
	}
}

func TestNativeCancellationQuiescesBeforeAdmissionAndBlocksOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(`{"choices":[{"index":0,"delta":{"content":"started"}}]}`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	gate := NewAdmissionGate()
	backend := nativeFixture(t, ProviderOMLX, server.URL, gate)
	quiescing, finishCleanup := make(chan struct{}), make(chan struct{})
	backend.quiesce = func(context.Context) error {
		close(quiescing)
		<-finishCleanup
		return errors.New("fixture cleanup failure")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- backend.Generate(ctx, nativeRequest(), func(ChatChunk) error { cancel(); return context.Canceled })
	}()
	<-quiescing
	waitCtx, waitCancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer waitCancel()
	if release, err := gate.acquire(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("admission released during cleanup: %v", err)
	}
	close(finishCleanup)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "cleanup failure") {
		t.Fatalf("cleanup failure missing: %v", err)
	}
	client, err := NewClientWithGate(server.URL, gate)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Chat(t.Context(), ChatRequest{Model: "fixture"}, nil); err == nil || !strings.Contains(err.Error(), "inference blocked") {
		t.Fatalf("Ollama bypassed failed cleanup: %v", err)
	}
}
