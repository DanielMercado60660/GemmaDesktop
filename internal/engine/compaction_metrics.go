package engine

import (
	"encoding/json"
	"unicode/utf8"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// These are observations of the selected event content, not a duplicate of
// ADK's transcript formatter or an estimate of provider-token savings.
type compactionInputMetrics struct {
	Events                int `json:"events"`
	TextCharacters        int `json:"textCharacters"`
	ThinkingCharacters    int `json:"thinkingCharacters"`
	ToolCalls             int `json:"toolCalls"`
	ToolArgumentJSONBytes int `json:"toolArgumentJSONBytes"`
	ToolResponses         int `json:"toolResponses"`
	ToolResponseJSONBytes int `json:"toolResponseJSONBytes"`
	PriorSummaries        int `json:"priorSummaries"`
	UnmeasurablePayloads  int `json:"unmeasurablePayloads"`
}

func observeCompactionInput(events []*session.Event) compactionInputMetrics {
	metrics := compactionInputMetrics{Events: len(events)}
	for _, event := range events {
		if event == nil {
			continue
		}
		content := event.Content
		if event.Actions.Compaction != nil {
			metrics.PriorSummaries++
		}
		if content == nil {
			continue
		}
		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Thought {
				// ADK excludes thoughts on prior summaries from its transcript.
				if event.Actions.Compaction == nil {
					metrics.ThinkingCharacters += utf8.RuneCountInString(part.Text)
				}
			} else {
				metrics.TextCharacters += utf8.RuneCountInString(part.Text)
			}
			if part.FunctionCall != nil {
				metrics.ToolCalls++
				if data, err := json.Marshal(part.FunctionCall.Args); err == nil {
					metrics.ToolArgumentJSONBytes += len(data)
				} else {
					metrics.UnmeasurablePayloads++
				}
			}
			if part.FunctionResponse != nil {
				metrics.ToolResponses++
				if data, err := json.Marshal(part.FunctionResponse.Response); err == nil {
					metrics.ToolResponseJSONBytes += len(data)
				} else {
					metrics.UnmeasurablePayloads++
				}
			}
		}
	}
	return metrics
}

func summaryTextCharacters(content *genai.Content) int {
	characters := 0
	if content != nil {
		for _, part := range content.Parts {
			if part != nil && !part.Thought {
				characters += utf8.RuneCountInString(part.Text)
			}
		}
	}
	return characters
}
