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

	"gemmadesktop.local/app/internal/inference"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

var ErrBudgetExceeded = errors.New("execution budget exceeded")

type ollamaModel struct {
	client  *inference.Client
	request Request
	budget  *Budget
	emit    func(Event)
	usage   inference.Metrics
	calls   int
}

func (m *ollamaModel) Name() string { return m.request.Model }

func (m *ollamaModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
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
		var text, thinking strings.Builder
		var calls []inference.ToolCall
		var metric inference.Metrics
		var reason string
		stopped := errors.New("stream consumer stopped")
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
				if stream && !yield(&model.LLMResponse{Partial: true, Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: chunk.Message.Content}}}}, nil) {
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
			if chunk.Done {
				metric = chunk.Metrics
				reason = chunk.DoneReason
			}
			return nil
		})
		if errors.Is(err, stopped) {
			return
		}
		if err != nil {
			yield(nil, err)
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
		m.usage.TotalDuration += metric.TotalDuration
		m.usage.LoadDuration += metric.LoadDuration
		m.usage.PromptEvalCount += metric.PromptEvalCount
		m.usage.PromptEvalDuration += metric.PromptEvalDuration
		m.usage.EvalCount += metric.EvalCount
		m.usage.EvalDuration += metric.EvalDuration
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
	r := inference.ChatRequest{Model: settings.Model, Stream: true, Think: settings.Think, Options: map[string]any{"num_ctx": settings.ContextSize, "temperature": settings.Temperature}}
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
				b, err := json.Marshal(p.FunctionResponse.Response)
				if err != nil {
					return r, err
				}
				toolMessages = append(toolMessages, inference.Message{Role: "tool", Content: string(b), ToolName: p.FunctionResponse.Name, ToolCallID: p.FunctionResponse.ID})
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
		if req.Config.MaxOutputTokens > 0 {
			r.Options["num_predict"] = req.Config.MaxOutputTokens
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
