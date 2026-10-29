package engine

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestEmptyOrThinkingOnlyStopIsNotCompleted(t *testing.T) {
	for _, body := range []string{
		`{"message":{},"done":true,"done_reason":"stop","eval_count":1146}`,
		`{"message":{"content":"  \n\t"},"done":true,"done_reason":"stop","eval_count":1146}`,
		`{"message":{"thinking":"private reasoning without an answer"},"done":true,"done_reason":"stop","eval_count":1146}`,
	} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/chat" {
					calls.Add(1)
				}
				fmt.Fprintln(w, body)
			}))
			defer srv.Close()
			client, _ := inference.NewClient(srv.URL)
			result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Finish and report the observed result"}, nil, nil)
			if !errors.Is(err, ErrEmptyResponse) || !result.Incomplete || result.Usage.EvalCount != 2292 || calls.Load() != 2 {
				t.Fatalf("empty stop treated as complete or lost usage: %+v %v", result, err)
			}
			if strings.Contains(string(result.Snapshot), "private reasoning without an answer") {
				t.Fatal("thinking-only stop persisted as a completed turn")
			}
		})
	}
}
