package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type deadlineProbeTransport func(*http.Request) (*http.Response, error)

func (probe deadlineProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return probe(request)
}

func TestBackgroundModelResidencyHasFiniteDeadline(t *testing.T) {
	for _, load := range []bool{false, true} {
		t.Run(fmt.Sprintf("load=%t", load), func(t *testing.T) {
			c, _ := NewClient("")
			calls := 0
			probe := deadlineProbeTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := request.Context().Deadline()
				remaining := time.Until(deadline)
				want := 5 * time.Minute
				body := `{"done":true}`
				if request.URL.Path == "/api/show" {
					want = 30 * time.Second
					body = `{}`
				}
				if !ok || remaining > want || remaining < want-time.Second {
					t.Errorf("%s deadline: present=%t remaining=%s want<=%s", request.URL.Path, ok, remaining, want)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			c.http.Transport, c.inferenceHTTP.Transport = probe, probe
			var err error
			if load {
				err = c.Load(context.Background(), "fixture", 4096)
			} else {
				err = c.Unload(context.Background(), "fixture")
			}
			if err != nil || calls != 2 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestContextOverflowIsNotSilentlyTruncatedOrRetried(t *testing.T) {
	for _, streamedError := range []bool{false, true} {
		t.Run(fmt.Sprintf("streamed=%t", streamedError), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					fmt.Fprint(w, `{}`)
					return
				}
				calls.Add(1)
				var wire map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
				}
				for _, key := range []string{"truncate", "shift"} {
					if string(wire[key]) != "false" {
						t.Errorf("%s must be explicitly false, got %s", key, wire[key])
					}
				}
				if calls.Load() > 1 {
					fmt.Fprintln(w, `{"done":true}`)
					return
				}
				var messages []Message
				if err := json.Unmarshal(wire["messages"], &messages); err != nil || len(messages) != 3 || messages[0].Content != "earlier evidence must remain" {
					t.Errorf("history was changed: %s, %v", wire["messages"], err)
				}
				if !streamedError {
					w.WriteHeader(http.StatusBadRequest)
				}
				fmt.Fprintln(w, `{"error":"input length exceeds the context length limit"}`)
			}))
			defer srv.Close()
			c, _ := NewClient(srv.URL)
			emitted := 0
			err := c.Chat(t.Context(), ChatRequest{Model: "fixture", Truncate: true, Shift: true, Messages: []Message{{Role: "user", Content: "earlier evidence must remain"}, {Role: "assistant", Content: "recorded"}, {Role: "user", Content: "continue"}}}, func(ChatChunk) error { emitted++; return nil })
			if err == nil || !strings.Contains(err.Error(), "context length") || calls.Load() != 1 || emitted != 0 {
				t.Fatalf("err=%v requests=%d emitted=%d", err, calls.Load(), emitted)
			}
			// An overflow must release admission without a hidden truncation retry.
			if err := c.Chat(t.Context(), ChatRequest{Model: "fixture"}, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTextRequestLimitRejectsBeforeDispatchAndExcludesImages(t *testing.T) {
	var calls atomic.Int32
	const imageBytes = 2 * MaxTextRequestBytes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if len(req.Messages) != 1 || len(req.Messages[0].Images) != 1 || len(req.Messages[0].Images[0]) != imageBytes {
			t.Error("text measurement changed image payload")
		}
		fmt.Fprintln(w, `{"done":true}`)
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	oversized := strings.Repeat("界", MaxTextRequestBytes/3+1)
	requests := []ChatRequest{
		{Model: "fixture", Messages: []Message{{Role: "user", Content: oversized}}},
		{Model: "fixture", Messages: []Message{{Role: "assistant", Thinking: oversized}}},
		{Model: "fixture", Tools: []ToolDefinition{{Type: "function", Function: FunctionDefinition{Name: "fixture", Description: oversized}}}},
	}
	for _, req := range requests {
		if err := c.Chat(t.Context(), req, nil); !errors.Is(err, ErrTextRequestTooLarge) {
			t.Fatalf("oversized request: %v", err)
		}
	}
	if calls.Load() != 0 || len(c.admission) != 0 {
		t.Fatalf("oversized text reached dispatch or admission: requests=%d admission=%d", calls.Load(), len(c.admission))
	}
	req := ChatRequest{Model: "fixture", Messages: []Message{{Role: "user", Content: "small text", Images: []string{strings.Repeat("A", imageBytes)}}}}
	if err := c.Chat(t.Context(), req, nil); err != nil {
		t.Fatal(err)
	}
	if len(req.Messages[0].Images[0]) != imageBytes || calls.Load() != 2 {
		t.Fatal("image exclusion mutated caller or skipped normal dispatch")
	}
}

func TestInferenceHeadersUseCallerDeadline(t *testing.T) {
	operations := map[string]func(context.Context, *Client) error{
		"chat": func(ctx context.Context, c *Client) error { return c.Chat(ctx, ChatRequest{Model: "fixture"}, nil) },
		"load": func(ctx context.Context, c *Client) error { return c.Load(ctx, "fixture", 4096) },
		"pull": func(ctx context.Context, c *Client) error { return c.Pull(ctx, "fixture", nil) },
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					fmt.Fprint(w, `{}`)
					return
				}
				select {
				case <-time.After(100 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
				fmt.Fprintln(w, `{"done":true,"status":"success","version":"fixture"}`)
			}))
			defer srv.Close()
			c, _ := NewClient(srv.URL)
			// Shorten only metadata's production 30s deadline to make this fixture fast.
			metadata := c.http.Transport.(*http.Transport)
			if metadata.ResponseHeaderTimeout != 30*time.Second {
				t.Fatalf("metadata deadline changed: %s", metadata.ResponseHeaderTimeout)
			}
			metadata.ResponseHeaderTimeout = 20 * time.Millisecond
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if err := operation(ctx, c); err != nil {
				t.Fatalf("valid cold operation inherited metadata timeout: %v", err)
			}
			if _, err := c.Version(ctx); err == nil {
				t.Fatal("metadata header deadline was removed")
			}
		})
	}
}

func TestCancellationBeforeInferenceHeadersReleasesAdmission(t *testing.T) {
	for _, load := range []bool{false, true} {
		t.Run(fmt.Sprintf("load=%t", load), func(t *testing.T) {
			started := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					fmt.Fprint(w, `{}`)
					return
				}
				// Consume the body so net/http can detect a disconnected client.
				var request any
				_ = json.NewDecoder(r.Body).Decode(&request)
				close(started)
				<-r.Context().Done()
			}))
			defer srv.Close()
			c, _ := NewClient(srv.URL)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				if load {
					result <- c.Load(ctx, "fixture", 4096)
				} else {
					result <- c.Chat(ctx, ChatRequest{Model: "fixture"}, nil)
				}
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not reach cold-load fixture")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel error: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("waiting for headers ignored cancellation")
			}
			if len(c.admission) != 0 {
				t.Fatal("inference admission leaked")
			}
		})
	}
}

// Explicitly opt in separately from the ordinary live-model test: this may
// change the loaded model's context allocation. Never run while another app
// task owns the model slot. It sends no tools and requests at most one token.
func TestLiveOllamaRejectsContextOverflow(t *testing.T) {
	model := os.Getenv("GEMMA_LIVE_CONTEXT_OVERFLOW_MODEL")
	if model == "" {
		t.Skip("set GEMMA_LIVE_CONTEXT_OVERFLOW_MODEL for exclusive overflow qualification")
	}
	c, err := NewClient("http://127.0.0.1:11434")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	version, err := c.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Show(ctx, model)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(info.Details.Format, "safetensors") || strings.Contains(strings.ToLower(model), "-mlx") {
		t.Skipf("Ollama=%s model=%s: MLX num_ctx is a soft recommendation, not an enforced input limit; its architectural maximum is separate. This test does not qualify that hard limit and will not send an oversized prompt", version, model)
	}
	t.Logf("qualifying requested-context rejection for non-MLX backend: Ollama=%s model=%s requestedContext=1024", version, model)
	emitted := false
	err = c.Chat(ctx, ChatRequest{Model: model, Think: false, Options: map[string]any{"num_ctx": 1024, "num_predict": 1, "temperature": 0}, Messages: []Message{
		{Role: "user", Content: strings.Repeat("Earlier evidence must remain: sentinel 1234567890.\n", 4096)},
		{Role: "assistant", Content: "Recorded."},
		{Role: "user", Content: "Reply OK."},
	}}, func(chunk ChatChunk) error {
		emitted = emitted || chunk.Message.Content != "" || chunk.Message.Thinking != "" || len(chunk.Message.ToolCalls) != 0
		return nil
	})
	if err == nil {
		t.Fatal("overfull history accepted; runner context or no-truncation semantics need qualification")
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "context") || (!strings.Contains(message, "exceed") && !strings.Contains(message, "limit")) || emitted {
		t.Fatalf("overflow must reject before generation, emitted=%t: %v", emitted, err)
	}
	t.Logf("runner rejected overflow: %v", err)
}
