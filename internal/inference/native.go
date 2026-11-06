package inference

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"
)

var ErrContextConfiguration = errors.New("requested context differs from the native runtime's configured context")
var ErrRepetitionLoop = errors.New("model generation stopped because of repetition")

// NativeConfig is an immutable binding to an app-verified local runtime and
// artifact catalog. Neither /v1/models nor a model name is artifact proof.
// Configure the server's context and disable its history/tool-result truncation
// before declaring a model ready here. Credentials never appear in Descriptor.
type NativeConfig struct {
	BaseURL    string
	APIKey     string
	Descriptor BackendDescriptor
	Models     []CompletionModel
	Gate       *AdmissionGate
	// Quiesce stops and reaps the exact owned runner after an interrupted
	// request. Managed runtimes must supply it; HTTP cancellation is not proof.
	Quiesce func(context.Context) error
}

type NativeBackend struct {
	base       string
	apiKey     string
	descriptor BackendDescriptor
	models     map[string]CompletionModel
	http       *http.Client
	gate       *AdmissionGate
	quiesce    func(context.Context) error
}

func NewOMLXBackend(config NativeConfig) (*NativeBackend, error) {
	return newNativeBackend(ProviderOMLX, config)
}

func NewMLXServeBackend(config NativeConfig) (*NativeBackend, error) {
	return newNativeBackend(ProviderMLXServe, config)
}

func newNativeBackend(provider ProviderKind, config NativeConfig) (*NativeBackend, error) {
	if config.BaseURL == "" {
		return nil, errors.New("native runtime requires an explicit loopback origin")
	}
	if strings.ContainsAny(config.APIKey, "\r\n\x00") {
		return nil, errors.New("invalid native runtime API key")
	}
	if config.Descriptor.Provider != "" && config.Descriptor.Provider != provider {
		return nil, errors.New("native runtime provider does not match adapter")
	}
	if config.Gate == nil {
		config.Gate = NewAdmissionGate()
	}
	// Reuse the same loopback, proxy and redirect policy as Ollama. This client
	// is transport configuration only; no Ollama protocol requests are made.
	local, err := NewClientWithGate(config.BaseURL, config.Gate)
	if err != nil {
		return nil, err
	}
	config.Descriptor.Provider = provider
	b := &NativeBackend{base: local.base, apiKey: config.APIKey, descriptor: config.Descriptor, http: local.inferenceHTTP, gate: config.Gate, quiesce: config.Quiesce, models: map[string]CompletionModel{}}
	for _, model := range config.Models {
		if err := validName(model.Name); err != nil {
			return nil, err
		}
		if IsRemoteName(model.Name) {
			return nil, ErrRemoteModel
		}
		if _, exists := b.models[model.Name]; exists {
			return nil, fmt.Errorf("duplicate native model %q", model.Name)
		}
		digest, err := normalizedDigest(model.ArtifactDigest)
		if err != nil {
			return nil, fmt.Errorf("native model %q artifact identity: %w", model.Name, err)
		}
		if model.ContextTokens < 1 || !model.Capabilities.Completion {
			return nil, fmt.Errorf("native model %q requires a verified context and completion capability", model.Name)
		}
		model.ArtifactDigest = digest
		b.models[model.Name] = model.Clone()
	}
	return b, nil
}

func (b *NativeBackend) Descriptor() BackendDescriptor { return b.descriptor }

func (b *NativeBackend) ResolveModel(ctx context.Context, name string) (CompletionModel, error) {
	if err := ctx.Err(); err != nil {
		return CompletionModel{}, err
	}
	model, exists := b.models[name]
	if !exists {
		return CompletionModel{}, fmt.Errorf("native model %q is not in the verified installed artifact catalog", name)
	}
	return model.Clone(), nil
}

type nativeMessage struct {
	Role             string           `json:"role"`
	Content          any              `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []nativeToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
	Name             string           `json:"name,omitempty"`
}

type nativeToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (b *NativeBackend) payload(req CompletionRequest) ([]byte, error) {
	model, err := b.ResolveModel(context.Background(), req.Model)
	if err != nil {
		return nil, err
	}
	if err := req.Controls.validate(); err != nil {
		return nil, err
	}
	if req.Controls.ContextTokens != model.ContextTokens {
		return nil, fmt.Errorf("%w (requested %d, configured %d)", ErrContextConfiguration, req.Controls.ContextTokens, model.ContextTokens)
	}
	if req.Controls.MaxOutputTokens < 1 || req.Controls.MaxOutputTokens > model.ContextTokens {
		return nil, errors.New("native output limit must be positive and fit the configured context")
	}
	if len(req.Tools) > 0 && !model.Capabilities.Tools {
		return nil, errors.New("native model tool capability has not been verified")
	}
	if req.Controls.Thinking != ThinkingDefault && !model.Capabilities.Thinking {
		return nil, errors.New("native model thinking capability has not been verified")
	}
	if (req.Format.JSON || req.Format.Schema != nil) && !model.Capabilities.StructuredOutput {
		return nil, errors.New("native model structured output capability has not been verified")
	}
	messages := make([]nativeMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		switch message.Role {
		case "system", "user", "assistant", "tool":
		default:
			return nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
		m := nativeMessage{Role: message.Role, Content: message.Content, ReasoningContent: message.Thinking, ToolCallID: message.ToolCallID, Name: message.ToolName}
		if m.Role == "tool" && m.ToolCallID == "" {
			return nil, errors.New("native tool result requires its original tool_call_id")
		}
		for _, call := range message.ToolCalls {
			if m.Role != "assistant" || call.ID == "" || call.Function.Name == "" || call.Function.Arguments == nil {
				return nil, errors.New("native assistant tool history requires an ID, name and argument object")
			}
			arguments, err := json.Marshal(call.Function.Arguments)
			if err != nil {
				return nil, err
			}
			t := nativeToolCall{ID: call.ID, Type: "function"}
			t.Function.Name, t.Function.Arguments = call.Function.Name, string(arguments)
			m.ToolCalls = append(m.ToolCalls, t)
		}
		messages = append(messages, m)
	}
	payload := map[string]any{"model": req.Model, "messages": messages, "stream": true, "stream_options": map[string]bool{"include_usage": true}, "max_tokens": req.Controls.MaxOutputTokens}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	if req.Controls.Temperature != nil {
		payload["temperature"] = *req.Controls.Temperature
	}
	if req.Controls.TopP != nil {
		payload["top_p"] = *req.Controls.TopP
	}
	if req.Controls.TopK != nil {
		payload["top_k"] = *req.Controls.TopK
	}
	switch req.Controls.Thinking {
	case ThinkingOn, ThinkingOff:
		enabled := req.Controls.Thinking == ThinkingOn
		if b.descriptor.Provider == ProviderOMLX {
			payload["chat_template_kwargs"] = map[string]bool{"enable_thinking": enabled}
		} else {
			payload["enable_thinking"] = enabled
		}
	case ThinkingLow, ThinkingMedium, ThinkingHigh:
		payload["reasoning_effort"] = string(req.Controls.Thinking)
	}
	if req.Format.Schema != nil {
		payload["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "response", "schema": req.Format.Schema, "strict": true}}
	} else if req.Format.JSON {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	// Measure the actual native serialization before adding base64 images. Tool
	// arguments are JSON strings on this wire, so escaped bytes count too.
	text, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(text) > MaxTextRequestBytes {
		return nil, ErrTextRequestTooLarge
	}
	for i, message := range req.Messages {
		if len(message.Images) == 0 {
			continue
		}
		if !model.Capabilities.Vision {
			return nil, errors.New("native model vision capability has not been verified")
		}
		if len(message.Images) > 32 {
			return nil, errors.New("too many native image attachments")
		}
		parts := []any{map[string]string{"type": "text", "text": message.Content}}
		for _, encoded := range message.Images {
			if len(encoded) > 16<<20 {
				return nil, errors.New("native image attachment exceeds size limit")
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, errors.New("invalid native image encoding")
			}
			kind := http.DetectContentType(decoded)
			if !strings.HasPrefix(kind, "image/") {
				return nil, errors.New("native image attachment has no supported image signature")
			}
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + kind + ";base64," + encoded}})
		}
		messages[i].Content = parts
	}
	encoded, err := json.Marshal(payload)
	if len(encoded) > 64<<20 {
		return nil, errors.New("native inference request exceeds size limit")
	}
	return encoded, err
}

func (b *NativeBackend) Generate(ctx context.Context, req CompletionRequest, emit func(ChatChunk) error, observe ...func(ChatChunk)) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := b.payload(req)
	if err != nil {
		return err
	}
	release, err := b.gate.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if b.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	}
	complete := false
	defer func() {
		if complete || b.quiesce == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := b.quiesce(cleanupCtx); err != nil {
			b.gate.Block(err)
			result = errors.Join(result, fmt.Errorf("native runtime cleanup: %w", err))
		}
	}()
	resp, err := b.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%s request: %w", b.descriptor.Provider, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("%s HTTP %d: %s", b.descriptor.Provider, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || contentType != "text/event-stream" {
		return errors.New("native streaming response must use text/event-stream")
	}
	state := nativeStream{provider: b.descriptor.Provider, model: req.Model, calls: map[int]*toolFragments{}}
	// The usage-only trailer can follow finish_reason. Delay the one receipt
	// until decoding ends, so the ledger does not commit an earlier unknown count.
	defer func() {
		if state.finished {
			receipt := ChatChunk{Done: true, DoneReason: state.reason, Metrics: state.metrics, TokenCountsPresent: state.countsPresent, UsageDetails: state.usageDetails}
			for _, callback := range observe {
				if callback != nil {
					callback(receipt)
				}
			}
		}
	}()
	if err := scanSSE(ctx, resp.Body, func(data []byte) error { return state.accept(data, emit) }); err != nil {
		return err
	}
	if !state.done || !state.finished {
		return ErrIncompleteStream
	}
	complete = true
	if state.repetition {
		return ErrRepetitionLoop
	}
	calls, err := state.completeCalls()
	if err != nil {
		return err
	}
	if emit != nil {
		return emit(ChatChunk{Model: req.Model, Done: true, DoneReason: state.reason, Message: Message{Role: "assistant", ToolCalls: calls}, Metrics: state.metrics, TokenCountsPresent: state.countsPresent, UsageDetails: state.usageDetails})
	}
	return nil
}

// UsageDetails carries optional native observations separately from the legacy
// nanosecond counters. Nil means unavailable, including legitimate zero counts.
type UsageDetails struct {
	CachedPromptTokens        *int     `json:"cachedPromptTokens,omitempty"`
	TimeToFirstTokenSeconds   *float64 `json:"timeToFirstTokenSeconds,omitempty"`
	LoadDurationSeconds       *float64 `json:"loadDurationSeconds,omitempty"`
	PromptEvalDurationSeconds *float64 `json:"promptEvalDurationSeconds,omitempty"`
	GenerationDurationSeconds *float64 `json:"generationDurationSeconds,omitempty"`
	TotalDurationSeconds      *float64 `json:"totalDurationSeconds,omitempty"`
}

type nativeUsage struct {
	PromptTokens     *int `json:"prompt_tokens"`
	CompletionTokens *int `json:"completion_tokens"`
	PromptDetails    *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	LoadDuration       *float64 `json:"model_load_duration"`
	PromptEvalDuration *float64 `json:"prompt_eval_duration"`
	GenerationDuration *float64 `json:"generation_duration"`
	TotalTime          *float64 `json:"total_time"`
	TimeToFirstToken   *float64 `json:"time_to_first_token"`
}

func nativeDuration(seconds *float64) (int64, error) {
	if seconds == nil {
		return 0, nil
	}
	if math.IsNaN(*seconds) || math.IsInf(*seconds, 0) || *seconds < 0 || *seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return 0, errors.New("invalid native usage duration")
	}
	return int64(*seconds * float64(time.Second)), nil
}

func (s *nativeStream) usage(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var u nativeUsage
	if err := json.Unmarshal(raw, &u); err != nil {
		return fmt.Errorf("decode native usage: %w", err)
	}
	if u.PromptTokens != nil && *u.PromptTokens >= 0 {
		s.metrics.PromptEvalCount = *u.PromptTokens
	}
	if u.CompletionTokens != nil && *u.CompletionTokens >= 0 {
		s.metrics.EvalCount = *u.CompletionTokens
	}
	if u.PromptTokens != nil && *u.PromptTokens < 0 || u.CompletionTokens != nil && *u.CompletionTokens < 0 {
		return errors.New("negative native token count")
	}
	if s.countsPresent {
		return errors.New("duplicate native usage receipt")
	}
	if u.PromptTokens != nil && u.CompletionTokens != nil {
		s.metrics.PromptEvalCount, s.metrics.EvalCount = *u.PromptTokens, *u.CompletionTokens
		s.countsPresent = true
	}
	if u.PromptDetails != nil && u.PromptDetails.CachedTokens != nil {
		cached := *u.PromptDetails.CachedTokens
		if cached < 0 || u.PromptTokens != nil && cached > *u.PromptTokens {
			return errors.New("invalid native cached-token count")
		}
		s.usageDetails.CachedPromptTokens = &cached
	}
	// These field names/units are oMLX extensions. mlx-serve does not get
	// fabricated Ollama timing values or inferred prefill/decode splits.
	if s.provider == ProviderOMLX {
		for destination, seconds := range map[*int64]*float64{&s.metrics.TotalDuration: u.TotalTime, &s.metrics.LoadDuration: u.LoadDuration, &s.metrics.PromptEvalDuration: u.PromptEvalDuration, &s.metrics.EvalDuration: u.GenerationDuration} {
			value, err := nativeDuration(seconds)
			if err != nil {
				return err
			}
			*destination = value
		}
		if _, err := nativeDuration(u.TimeToFirstToken); err != nil {
			return err
		}
		s.usageDetails.TimeToFirstTokenSeconds = u.TimeToFirstToken
		s.usageDetails.LoadDurationSeconds = u.LoadDuration
		s.usageDetails.PromptEvalDurationSeconds = u.PromptEvalDuration
		s.usageDetails.GenerationDurationSeconds = u.GenerationDuration
		s.usageDetails.TotalDurationSeconds = u.TotalTime
	}
	return nil
}

var _ CompletionBackend = (*NativeBackend)(nil)
