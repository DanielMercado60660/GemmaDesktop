package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/genai"
)

type telemetrySummarizer struct {
	result compaction.SummarizeResult
	err    error
	input  []*session.Event
}

func (s *telemetrySummarizer) SummarizeEvents(_ context.Context, events []*session.Event) (compaction.SummarizeResult, error) {
	s.input = events
	return s.result, s.err
}

func TestCompactionTelemetryPreservesResultsAndPrivateHistory(t *testing.T) {
	input := []*session.Event{
		{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
			{Text: "秘密", Thought: true},
			{FunctionCall: &genai.FunctionCall{Name: "read_file", Args: map[string]any{"path": "private.txt"}}},
		}}}},
		{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{Name: "read_file", Response: map[string]any{"content": "private result"}}},
		}}}},
	}
	before, _ := json.Marshal(input)
	usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 73, CandidatesTokenCount: 9}
	for _, status := range []string{"generated", "declined", "failed"} {
		t.Run(status, func(t *testing.T) {
			inner := &telemetrySummarizer{result: compaction.SummarizeResult{Usage: usage}}
			if status == "generated" {
				inner.result.Content = genai.NewContentFromText("保留", genai.RoleModel)
			}
			if status == "failed" {
				inner.err = context.Canceled
			}
			var events []Event
			observer := &observedSummarizer{inner: inner, emit: func(e Event) { events = append(events, e) }, nodeID: "child"}
			result, err := observer.SummarizeEvents(t.Context(), input)
			if result.Content != inner.result.Content || result.Usage != usage || !errors.Is(err, inner.err) || !reflect.DeepEqual(inner.input, input) {
				t.Fatal("observation changed summarizer input, result, error or usage")
			}
			last := events[len(events)-1]
			if last.Type != "context_compaction_metrics" || last.NodeID != "child" {
				t.Fatalf("missing scoped metrics: %+v", last)
			}
			data := last.Data.(map[string]any)
			if data["status"] != status || data["elapsedMilliseconds"].(int64) < 0 || data["usage"] != usage {
				t.Fatalf("ambiguous attempt result: %+v", data)
			}
			metrics := data["input"].(compactionInputMetrics)
			if metrics.Events != 2 || metrics.ThinkingCharacters != 2 || metrics.TextCharacters != 0 || metrics.ToolCalls != 1 || metrics.ToolResponses != 1 || metrics.ToolArgumentJSONBytes == 0 || metrics.ToolResponseJSONBytes == 0 {
				t.Fatalf("input observations: %+v", metrics)
			}
			for _, e := range events {
				if e.Type == "context_compacted" {
					t.Fatal("a returned summary was falsely reported as committed")
				}
			}
			encoded, _ := json.Marshal(events)
			for _, secret := range []string{"秘密", "private.txt", "private result", "保留"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatal("metrics copied private content")
				}
			}
			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("metrics mutated original history")
			}
			observer.emit = nil
			if result, err := observer.SummarizeEvents(t.Context(), input); result.Usage != usage || !errors.Is(err, inner.err) {
				t.Fatal("no-listener observation changed result")
			}
		})
	}
}

func TestCompactionTelemetryUsesSelectedContentWithoutPriorSummaryThoughts(t *testing.T) {
	content := &genai.Content{Parts: []*genai.Part{{Text: "三字文"}, {Text: "excluded thought", Thought: true}}}
	events := []*session.Event{nil, {LLMResponse: model.LLMResponse{Content: content}, Actions: session.EventActions{Compaction: &session.EventCompaction{CompactedContent: genai.NewContentFromText("annotation is not the selected event content", genai.RoleModel)}}}}
	metrics := observeCompactionInput(events)
	if metrics.Events != 2 || metrics.PriorSummaries != 1 || metrics.TextCharacters != 3 || metrics.ThinkingCharacters != 0 || summaryTextCharacters(content) != 3 {
		t.Fatalf("prior summary accounting: %+v", metrics)
	}
}

func TestContextAuditSeparatesWireThinkingFromProviderTokens(t *testing.T) {
	chat := inference.ChatRequest{Messages: []inference.Message{{Role: "assistant", Content: "答え", Thinking: "private reasoning"}, {Role: "user", Content: "continue"}}, Options: map[string]any{"num_ctx": 32768, "num_predict": 8192}}
	before, _ := json.Marshal(chat)
	data := generationContextAudit(chat, 4, 2, false)
	if data["wireContentCharacters"] != 10 || data["wireThinkingCharacters"] != 17 || !strings.Contains(data["estimateMethod"].(string), "Provider templates may omit thinking") {
		t.Fatalf("wire estimates presented as provider usage: %+v", data)
	}
	encoded, _ := json.Marshal(data)
	if strings.Contains(string(encoded), "private reasoning") {
		t.Fatal("wire audit copied reasoning")
	}
	after, _ := json.Marshal(chat)
	if string(before) != string(after) {
		t.Fatal("audit mutated provider request")
	}
}
