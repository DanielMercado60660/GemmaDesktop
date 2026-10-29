package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExplicitLoadUsesRequestedContext(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests = append(requests, request)
		fmt.Fprint(w, `{"done":true}`)
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	if err := c.Load(t.Context(), "gemma4:12b", 32768); err != nil {
		t.Fatal(err)
	}
	if err := c.Unload(t.Context(), "gemma4:12b"); err != nil {
		t.Fatal(err)
	}
	if err := c.Load(t.Context(), "gemma4:12b", 0); err == nil {
		t.Fatal("invalid context accepted")
	}
	if len(requests) != 2 {
		t.Fatalf("requests: %v", requests)
	}
	if requests[0]["options"].(map[string]any)["num_ctx"] != float64(32768) {
		t.Fatalf("load omitted requested context: %v", requests[0])
	}
	if requests[1]["keep_alive"] != float64(0) || requests[1]["options"] != nil {
		t.Fatalf("unload changed: %v", requests[1])
	}
}

func TestRejectRemoteOrigins(t *testing.T) {
	for _, u := range []string{"https://example.com", "http://127.0.0.1.evil.test", "http://user:secret@127.0.0.1", "http://127.0.0.1/api", "file:///tmp/server", "http://192.168.1.10:11434"} {
		if _, err := NewClient(u); err == nil {
			t.Errorf("accepted %q", u)
		}
	}
}

func TestFragmentedStreamRequiresTerminalMarker(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       error
	}{{"complete", "{\"message\":{\"content\":\"hello\"},\"done\":false}\n{\"done\":true,\"eval_count\":1}\n", nil}, {"truncated", "{\"message\":{\"content\":\"hello\"},\"done\":false}\n", ErrIncompleteStream}, {"error", "{\"error\":\"model failed\"}\n", errors.New("model failed")}} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					fmt.Fprint(w, `{"capabilities":["completion"]}`)
					return
				}
				for _, b := range []byte(test.body) {
					_, _ = w.Write([]byte{b})
					w.(http.Flusher).Flush()
				}
			}))
			defer srv.Close()
			c, _ := NewClient(srv.URL)
			var text strings.Builder
			err := c.Chat(t.Context(), ChatRequest{Model: "gemma4:12b"}, func(ch ChatChunk) error { text.WriteString(ch.Message.Content); return nil })
			if test.want == nil && err != nil {
				t.Fatal(err)
			}
			if test.want != nil && (err == nil || !strings.Contains(err.Error(), test.want.Error())) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
			if test.name != "error" && text.String() != "hello" {
				t.Fatal(text.String())
			}
		})
	}
}

func TestCloudAliasIsBlockedBeforeChat(t *testing.T) {
	chat := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{"remote_model":"gemma4:31b","remote_host":"https://ollama.com"}`)
			return
		}
		chat = true
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	err := c.Chat(t.Context(), ChatRequest{Model: "innocent-alias"}, nil)
	if !errors.Is(err, ErrRemoteModel) || chat {
		t.Fatalf("err=%v dispatched=%v", err, chat)
	}
}

func TestCancellationClosesStreamAndReleasesAdmission(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"one"}}`)
		w.(http.Flusher).Flush()
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- c.Chat(ctx, ChatRequest{Model: "test"}, nil) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop stream")
	}
	if len(c.admission) != 0 {
		t.Fatal("inference admission leaked")
	}
}

func TestPullMustFinishSuccessfully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"status":"downloading","total":10,"completed":5}`)
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	if err := c.Pull(t.Context(), "test", nil); !errors.Is(err, ErrIncompleteStream) {
		t.Fatalf("%v", err)
	}
}
