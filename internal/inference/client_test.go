package inference

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
