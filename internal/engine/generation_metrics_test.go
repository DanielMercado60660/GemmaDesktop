package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestIncompleteGenerationRecordsLimitsAndObservedUsageWithoutContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprintln(w, `{"message":{"thinking":"private thought","content":"unfinished"},"done":true,"done_reason":"length","prompt_eval_count":21217,"eval_count":8192}`)
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	var metrics []map[string]any
	result, err := New(client).Run(t.Context(), Request{Model: "fixture", Prompt: "Finish", ContextSize: 65536, MaxOutputTokens: 8192}, func(ev Event) {
		if ev.Type == "generation_metrics" {
			metrics = append(metrics, ev.Data.(map[string]any))
		}
	}, nil)
	if !errors.Is(err, ErrIncompleteResponse) || !result.Incomplete || len(metrics) != 1 {
		t.Fatalf("incomplete result or final metrics lost: result=%+v err=%v metrics=%v", result, err, metrics)
	}
	m := metrics[0]
	if m["contextSize"] != 65536 || m["maxOutputTokens"] != 8192 || m["promptTokens"] != 21217 || m["outputTokens"] != 8192 || m["doneReason"] != "length" || m["thinkingCharacters"] != 15 {
		t.Fatalf("generation limits/usage ambiguous: %+v", m)
	}
	encoded, _ := json.Marshal(m)
	if strings.Contains(string(encoded), "private thought") || strings.Contains(string(encoded), "unfinished") {
		t.Fatal("metrics captured model content")
	}
}

func TestOversizeSummaryDeclinesBeforeInference(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	client, _ := inference.NewClient(srv.URL)
	m := &ollamaModel{client: client, request: defaults(Request{Model: "fixture"}), budget: &Budget{MaxModelCalls: 2}, transcriptCharacterLimit: 10, outputTokenLimit: 256}
	var got error
	request := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText(strings.Repeat("文", 11), genai.RoleUser)}}
	for _, err := range m.GenerateContent(t.Context(), request, false) {
		got = err
	}
	if got == nil || !strings.Contains(got.Error(), "original history preserved") || calls != 0 {
		t.Fatalf("oversize transcript reached inference: calls=%d err=%v", calls, got)
	}
}
