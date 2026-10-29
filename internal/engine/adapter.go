package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
	"unicode/utf8"

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

var ErrBudgetExceeded = errors.New("execution budget exceeded")
var ErrIncompleteResponse = errors.New("model response is incomplete")
var ErrEmptyResponse = errors.New("model stopped without an answer or tool call")
var ErrInvalidStructuredAction = errors.New("model returned an invalid structured action")

type incompleteResponseError struct {
	content string
	usage   inference.Metrics
	cause   error
}

func (e *incompleteResponseError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return "model response is incomplete: Ollama reached its generation length limit"
}
func (e *incompleteResponseError) Unwrap() error        { return ErrIncompleteResponse }
func (e *incompleteResponseError) Is(target error) bool { return errors.Is(e.cause, target) }

type ollamaModel struct {
	client  *inference.Client
	request Request
	budget  *Budget
	emit    func(Event)
	usage   inference.Metrics
	calls   int
	// A dedicated model bound is necessary for context summaries: ADK's
	// summarizer deliberately removes an inherited MaxOutputTokens setting.
	outputTokenLimit int32
	// A coarse summary-only size guard. Unlike ADK's transcript shrinking,
	// exceeding this bound declines compaction without silently dropping text.
	// Characters are not tokens; the inference client also enforces its byte cap.
	transcriptCharacterLimit int
}

func (m *ollamaModel) Name() string { return m.request.Model }

const emptyGenerationFeedback = "\n\nHost protocol feedback: The preceding generation returned no visible answer and no usable tool call. Produce either a complete answer or a valid tool call whose arguments satisfy the supplied JSON schema. This feedback does not authorize actions, expand scope, or approve execution. Preserve prior observations; do not repeat completed tool actions merely because of this retry."

func (m *ollamaModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		current := req
		var discarded *inference.Metrics
		for attempt := 0; attempt < 2; attempt++ {
			retry := false
			for response, err := range m.generateAttempt(ctx, current, stream, attempt+1) {
				if err != nil {
					var incomplete *incompleteResponseError
					if attempt == 0 && m.outputTokenLimit == 0 && errors.Is(err, ErrEmptyResponse) && errors.As(err, &incomplete) {
						usage := incomplete.usage
						discarded = &usage
						if err := ctx.Err(); err != nil {
							yield(nil, err)
							return
						}
						m.budget.mu.Lock()
						exhausted := m.budget.ModelCalls >= m.budget.MaxModelCalls || m.calls >= m.request.MaxSteps
						m.budget.mu.Unlock()
						if exhausted {
							yield(nil, fmt.Errorf("%w: no model call remains for an empty-response retry", ErrBudgetExceeded))
							return
						}
						if m.emit != nil {
							m.emit(Event{Type: "generation_retry", Title: "Retrying empty generation", NodeID: m.request.NodeID, Detail: "No answer or usable tool call was returned. One retry will use the same history and existing permissions.", Data: map[string]any{"model": m.Name(), "reason": "empty_response", "attempt": 2, "maxRetries": 1}})
						}
						current = requestWithEmptyFeedback(req)
						retry = true
						break
					}
					yield(nil, err)
					return
				}
				if response != nil && !response.Partial && discarded != nil {
					// Keep the successful response's actual prompt count intact for
					// ADK compaction. Discarded-attempt cost is separate audit metadata.
					if response.CustomMetadata == nil {
						response.CustomMetadata = map[string]any{}
					}
					response.CustomMetadata["discarded_generation_usage"] = *discarded
				}
				if !yield(response, nil) {
					return
				}
			}
			if !retry {
				return
			}
		}
	}
}

func requestWithEmptyFeedback(req *model.LLMRequest) *model.LLMRequest {
	copy := *req
	config := genai.GenerateContentConfig{}
	if req.Config != nil {
		config = *req.Config
	}
	instruction := genai.Content{}
	if config.SystemInstruction != nil {
		instruction = *config.SystemInstruction
		instruction.Parts = append([]*genai.Part(nil), instruction.Parts...)
	}
	instruction.Parts = append(instruction.Parts, &genai.Part{Text: emptyGenerationFeedback})
	config.SystemInstruction = &instruction
	copy.Config = &config
	return &copy
}

func addGenerationUsage(a, b inference.Metrics) inference.Metrics {
	a.TotalDuration += b.TotalDuration
	a.LoadDuration += b.LoadDuration
	a.PromptEvalCount += b.PromptEvalCount
	a.PromptEvalDuration += b.PromptEvalDuration
	a.EvalCount += b.EvalCount
	a.EvalDuration += b.EvalDuration
	return a
}

func (m *ollamaModel) generateAttempt(ctx context.Context, req *model.LLMRequest, stream bool, attempt int) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		m.budget.mu.Lock()
		if m.budget.ModelCalls >= m.budget.MaxModelCalls || m.calls >= m.request.MaxSteps {
			m.budget.mu.Unlock()
			yield(nil, fmt.Errorf("%w: maximum model calls reached (agent %d, run %d)", ErrBudgetExceeded, m.request.MaxSteps, m.budget.MaxModelCalls))
			return
		}
		m.budget.ModelCalls++
		m.calls++
		step := m.budget.ModelCalls
		m.budget.mu.Unlock()
		if m.emit != nil {
			m.emit(Event{Type: "status", Title: "Generating", NodeID: m.request.NodeID, Data: map[string]any{"step": step, "model": m.Name()}})
		}
		chat, err := toOllama(req, m.request)
		if err != nil {
			yield(nil, err)
			return
		}
		if m.outputTokenLimit > 0 {
			chat.Options["num_predict"] = m.outputTokenLimit
		}
		var protocol *StructuredActionProtocol
		if m.outputTokenLimit == 0 && m.request.ToolProtocol == ToolProtocolStructuredJSON {
			chat, protocol, err = PrepareStructuredActions(chat)
			if err != nil {
				yield(nil, err)
				return
			}
		}
		if m.transcriptCharacterLimit > 0 {
			characters := 0
			for _, message := range chat.Messages {
				characters += utf8.RuneCountInString(message.Content)
			}
			if characters > m.transcriptCharacterLimit {
				yield(nil, fmt.Errorf("summary input is %d characters, above the %d-character guard; original history preserved", characters, m.transcriptCharacterLimit))
				return
			}
		}
		if m.emit != nil {
			m.emit(Event{Type: "context_prepared", Title: "Generation context prepared", NodeID: m.request.NodeID, Data: generationContextAudit(chat, step, attempt, m.outputTokenLimit > 0)})
		}
		var text, thinking strings.Builder
		var calls []inference.ToolCall
		var metric inference.Metrics
		var reason string
		stopped := errors.New("stream consumer stopped")
		terminal, measured := false, false
		recordReceipt := beginProviderUsage(ctx)
		// Account at the provider boundary, before downstream validation or ADK
		// cancellation can replace a response/error and hide consumed tokens.
		defer func() {
			t, n := 0, 0
			if terminal {
				t = 1
			}
			if measured {
				n = 1
			}
			recordReceipt(metric, terminal, measured)
			m.budget.mu.Lock()
			m.usage = addGenerationUsage(m.usage, metric)
			m.budget.mu.Unlock()
			if m.emit != nil {
				m.emit(Event{Type: "token_accounting", Title: "Token measurement", NodeID: m.request.NodeID, Data: map[string]any{"step": step, "attempt": attempt, "summary": m.outputTokenLimit > 0, "tokenAccounting": inference.NewTokenAccounting(1, t, n)}})
			}
		}()
		err = m.client.Chat(ctx, chat, func(chunk inference.ChatChunk) error {
			m.budget.mu.Lock()
			m.budget.OutputBytes += len(chunk.Message.Content) + len(chunk.Message.Thinking)
			tooLarge := m.budget.OutputBytes > m.budget.MaxOutputBytes
			m.budget.mu.Unlock()
			if tooLarge {
				return fmt.Errorf("%w: model output limit", ErrBudgetExceeded)
			}
			if chunk.Message.Content != "" {
				text.WriteString(chunk.Message.Content)
				if protocol == nil && stream && !yield(&model.LLMResponse{Partial: true, Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: chunk.Message.Content}}}}, nil) {
					return stopped
				}
			}
			if chunk.Message.Thinking != "" {
				thinking.WriteString(chunk.Message.Thinking)
				if stream && !yield(&model.LLMResponse{Partial: true, Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: chunk.Message.Thinking, Thought: true}}}}, nil) {
					return stopped
				}
			}
			for _, call := range chunk.Message.ToolCalls {
				if len(calls) >= 64 {
					return fmt.Errorf("%w: maximum tool calls in one response", ErrBudgetExceeded)
				}
				if call.Function.Name == "" {
					return errors.New("Ollama returned a tool call without a name")
				}
				if call.Function.Arguments == nil {
					call.Function.Arguments = map[string]any{}
				}
				encoded, err := json.Marshal(call)
				if err != nil {
					return err
				}
				m.budget.mu.Lock()
				m.budget.OutputBytes += len(encoded)
				tooLarge := m.budget.OutputBytes > m.budget.MaxOutputBytes
				m.budget.mu.Unlock()
				if tooLarge {
					return fmt.Errorf("%w: model output limit", ErrBudgetExceeded)
				}
				calls = append(calls, call)
			}
			return nil
		}, func(receipt inference.ChatChunk) {
			metric, reason = receipt.Metrics, receipt.DoneReason
			terminal, measured = true, receipt.TokenCountsPresent
			recordReceipt(metric, terminal, measured)
		})
		if errors.Is(err, stopped) {
			return
		}
		if err != nil {
			yield(nil, err)
			return
		}
		rawCharacters := utf8.RuneCountInString(text.String())
		var protocolErr error
		if protocol != nil {
			// Raw action JSON is transport data, not user-facing prose. Decode
			// only a complete response, and never dispatch a native-call fallback.
			raw := text.String()
			text.Reset()
			if reason != "length" && (strings.TrimSpace(raw) != "" || len(calls) > 0) {
				if len(calls) > 0 {
					protocolErr = errors.New("unexpected native tool calls in structured response")
				} else {
					var action StructuredAction
					action, protocolErr = protocol.Decode(raw)
					if protocolErr == nil {
						text.WriteString(action.Answer)
						if action.ToolCall != nil {
							calls = append(calls, *action.ToolCall)
						}
					}
				}
			}
		}
		if m.emit != nil {
			m.emit(Event{Type: "generation_metrics", Title: "Generation usage", NodeID: m.request.NodeID, Data: map[string]any{
				"model": m.Name(), "contextSize": m.request.ContextSize, "maxOutputTokens": chat.Options["num_predict"],
				"step": step, "attempt": attempt,
				"promptTokens": metric.PromptEvalCount, "outputTokens": metric.EvalCount, "doneReason": reason,
				"visibleCharacters": utf8.RuneCountInString(text.String()), "thinkingCharacters": utf8.RuneCountInString(thinking.String()), "toolCalls": len(calls),
				"summary":      m.outputTokenLimit > 0,
				"toolProtocol": m.request.ToolProtocol, "rawResponseCharacters": rawCharacters,
			}})
		}
		// Do not publish a complete ADK turn or execute tool calls from a
		// truncated ordinary generation. Keep its streamed text as a labelled
		// incomplete result. The summary adapter instead returns MAX_TOKENS to
		// ADK's summarizer, which rejects the summary without replacing history.
		if reason == "length" && m.outputTokenLimit == 0 {
			yield(nil, &incompleteResponseError{content: text.String(), usage: metric})
			return
		}
		if protocolErr != nil {
			yield(nil, &incompleteResponseError{usage: metric, cause: fmt.Errorf("%w: %v", ErrInvalidStructuredAction, protocolErr)})
			return
		}
		// Some runtimes can stop after hidden/filtered thinking without any
		// actionable output. That is not a completed assistant turn.
		if strings.TrimSpace(text.String()) == "" && len(calls) == 0 {
			yield(nil, &incompleteResponseError{usage: metric, cause: ErrEmptyResponse})
			return
		}
		parts := []*genai.Part{}
		if thinking.Len() > 0 {
			parts = append(parts, &genai.Part{Text: thinking.String(), Thought: true})
		}
		if text.Len() > 0 {
			parts = append(parts, &genai.Part{Text: text.String()})
		}
		seen := map[string]bool{}
		for _, call := range calls {
			if call.ID == "" {
				call.ID = newID("call")
			}
			if seen[call.ID] {
				yield(nil, fmt.Errorf("duplicate tool call ID %q", call.ID))
				return
			}
			seen[call.ID] = true
			parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: call.ID, Name: call.Function.Name, Args: call.Function.Arguments}})
		}
		finish := genai.FinishReasonStop
		if reason == "length" {
			finish = genai.FinishReasonMaxTokens
		}
		yield(&model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: parts}, TurnComplete: true, FinishReason: finish, UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(metric.PromptEvalCount), CandidatesTokenCount: int32(metric.EvalCount), TotalTokenCount: int32(metric.PromptEvalCount + metric.EvalCount)}, CustomMetadata: map[string]any{"ollama_metrics": metric, "done_reason": reason}}, nil)
	}
}

func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func toOllama(req *model.LLMRequest, settings Request) (inference.ChatRequest, error) {
	maxTokens := settings.MaxOutputTokens
	if maxTokens == 0 {
		maxTokens = DefaultMaxOutputTokens
	}
	if maxTokens < MinMaxOutputTokens || maxTokens > MaxMaxOutputTokens {
		return inference.ChatRequest{}, fmt.Errorf("maxOutputTokens must be %d–%d", MinMaxOutputTokens, MaxMaxOutputTokens)
	}
	if req.Config != nil && req.Config.MaxOutputTokens > 0 {
		maxTokens = min(maxTokens, int(req.Config.MaxOutputTokens))
	}
	r := inference.ChatRequest{Model: settings.Model, Stream: true, Think: settings.Think, Options: map[string]any{"num_ctx": settings.ContextSize, "temperature": settings.Temperature, "num_predict": maxTokens}}
	if req.Config != nil && req.Config.SystemInstruction != nil {
		var text strings.Builder
		for _, p := range req.Config.SystemInstruction.Parts {
			if p != nil {
				text.WriteString(p.Text)
			}
		}
		if text.Len() > 0 {
			r.Messages = append(r.Messages, inference.Message{Role: "system", Content: text.String()})
		}
	}
	for _, c := range req.Contents {
		if c == nil {
			continue
		}
		role := c.Role
		if role == genai.RoleModel {
			role = "assistant"
		}
		if role == "" {
			role = "user"
		}
		message := inference.Message{Role: role}
		var toolMessages []inference.Message
		for _, p := range c.Parts {
			if p == nil {
				continue
			}
			switch {
			case p.FunctionResponse != nil:
				content, err := toolResponseText(p.FunctionResponse.Name, p.FunctionResponse.Response)
				if err != nil {
					return r, err
				}
				toolMessages = append(toolMessages, inference.Message{Role: "tool", Content: content, ToolName: p.FunctionResponse.Name, ToolCallID: p.FunctionResponse.ID})
			case p.FunctionCall != nil:
				message.ToolCalls = append(message.ToolCalls, inference.ToolCall{ID: p.FunctionCall.ID, Type: "function", Function: inference.ToolFunction{Name: p.FunctionCall.Name, Arguments: p.FunctionCall.Args}})
			case p.InlineData != nil:
				if !strings.HasPrefix(p.InlineData.MIMEType, "image/") {
					return r, fmt.Errorf("unsupported attachment MIME type %q", p.InlineData.MIMEType)
				}
				message.Images = append(message.Images, encodeImage(p.InlineData.Data))
			case p.FileData != nil:
				return r, errors.New("remote/file URI inputs are not allowed; attach approved image bytes")
			case p.Thought:
				message.Thinking += p.Text
			default:
				message.Content += p.Text
			}
		}
		if message.Content != "" || message.Thinking != "" || len(message.ToolCalls) > 0 || len(message.Images) > 0 {
			r.Messages = append(r.Messages, message)
		}
		r.Messages = append(r.Messages, toolMessages...)
	}
	if req.Config != nil {
		for _, t := range req.Config.Tools {
			if t == nil {
				continue
			}
			for _, f := range t.FunctionDeclarations {
				if f == nil {
					continue
				}
				parameters := map[string]any{"type": "object", "properties": map[string]any{}}
				if f.ParametersJsonSchema != nil {
					b, err := json.Marshal(f.ParametersJsonSchema)
					if err != nil {
						return r, err
					}
					if err = json.Unmarshal(b, &parameters); err != nil {
						return r, err
					}
				} else if f.Parameters != nil {
					b, err := json.Marshal(f.Parameters)
					if err != nil {
						return r, err
					}
					if err = json.Unmarshal(b, &parameters); err != nil {
						return r, err
					}
					normalizeSchema(parameters)
				}
				r.Tools = append(r.Tools, inference.ToolDefinition{Type: "function", Function: inference.FunctionDefinition{Name: f.Name, Description: f.Description, Parameters: parameters}})
			}
		}
		if req.Config.Temperature != nil {
			r.Options["temperature"] = *req.Config.Temperature
		}
		if req.Config.TopP != nil {
			r.Options["top_p"] = *req.Config.TopP
		}
		if req.Config.TopK != nil {
			r.Options["top_k"] = *req.Config.TopK
		}
	}
	return r, nil
}

func normalizeSchema(m map[string]any) {
	for k, v := range m {
		if k == "type" {
			if s, ok := v.(string); ok {
				m[k] = strings.ToLower(s)
			}
		}
		switch x := v.(type) {
		case map[string]any:
			normalizeSchema(x)
		case []any:
			for _, a := range x {
				if mm, ok := a.(map[string]any); ok {
					normalizeSchema(mm)
				}
			}
		}
	}
}
