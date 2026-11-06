package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"sync"
)

// AdmissionGate serializes inference across providers, including Ollama's
// embedding and residency operations. Owned native runners are reaped before
// cancellation releases admission. Failed cleanup blocks all future inference.
type AdmissionGate struct {
	slot    chan struct{}
	mu      sync.Mutex
	blocked error
}

func NewAdmissionGate() *AdmissionGate { return &AdmissionGate{slot: make(chan struct{}, 1)} }

// Acquire also covers owned-runtime startup/model loading. Release only after
// that operation is finished, or its owned process has been reaped.
func (g *AdmissionGate) Acquire(ctx context.Context) (func(), error) { return g.acquire(ctx) }

func (g *AdmissionGate) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case g.slot <- struct{}{}:
		g.mu.Lock()
		err := g.blocked
		g.mu.Unlock()
		if err != nil {
			<-g.slot
			return nil, err
		}
		return func() { <-g.slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Block fails closed after a runner could not be stopped. A new manager/session
// is required; callers cannot clear this condition while ownership is uncertain.
func (g *AdmissionGate) Block(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocked == nil {
		g.blocked = fmt.Errorf("inference blocked after runtime cleanup failed: %w", err)
	}
}

// BlockedReason exposes a read-only status for host readiness/UI. It never
// clears a cleanup failure or grants inference authority.
func (g *AdmissionGate) BlockedReason() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocked != nil {
		return g.blocked.Error()
	}
	return ""
}

type ProviderKind string

const (
	ProviderOllama   ProviderKind = "ollama"
	ProviderOMLX     ProviderKind = "omlx"
	ProviderMLXServe ProviderKind = "mlx_serve"
)

// BackendDescriptor identifies the selected runtime, not a model-name alias.
// Empty build/source fields mean unknown, never a qualified runtime.
type BackendDescriptor struct {
	Provider    ProviderKind `json:"provider"`
	RuntimeID   string       `json:"runtimeId,omitempty"`
	BuildID     string       `json:"buildId,omitempty"`
	SourceSHA   string       `json:"sourceSha,omitempty"`
	PatchDigest string       `json:"patchDigest,omitempty"`
}

type CompletionCapabilities struct {
	Completion       bool `json:"completion"`
	Tools            bool `json:"tools"`
	Thinking         bool `json:"thinking"`
	Vision           bool `json:"vision"`
	StructuredOutput bool `json:"structuredOutput"`
}

type CapabilityClaim struct {
	State          string `json:"state"` // supported, unsupported or unknown
	Advertised     bool   `json:"advertised"`
	Source         string `json:"source"`
	EvidenceDigest string `json:"evidenceDigest,omitempty"`
}

// CompletionModel is supplied by verified local metadata. Native adapters do
// not infer architecture, support or artifact identity from a display name.
type CompletionModel struct {
	Name             string                     `json:"name"`
	ArtifactDigest   string                     `json:"artifactDigest,omitempty"`
	TokenizerDigest  string                     `json:"tokenizerDigest,omitempty"`
	TemplateDigest   string                     `json:"templateDigest,omitempty"`
	Architecture     string                     `json:"architecture,omitempty"`
	ContextTokens    int                        `json:"contextTokens,omitempty"`
	Capabilities     CompletionCapabilities     `json:"capabilities"`
	CapabilityClaims map[string]CapabilityClaim `json:"capabilityClaims,omitempty"`
}

func (m CompletionModel) Clone() CompletionModel {
	m.CapabilityClaims = maps.Clone(m.CapabilityClaims)
	return m
}

type ThinkingMode string

const (
	ThinkingDefault ThinkingMode = ""
	ThinkingOn      ThinkingMode = "on"
	ThinkingOff     ThinkingMode = "off"
	ThinkingLow     ThinkingMode = "low"
	ThinkingMedium  ThinkingMode = "medium"
	ThinkingHigh    ThinkingMode = "high"
)

// GenerationControls contains only controls with an explicit provider mapping.
// In particular, a native server's configured context cannot be silently
// replaced by an ignored Ollama num_ctx option.
type GenerationControls struct {
	ContextTokens   int          `json:"contextTokens"`
	MaxOutputTokens int          `json:"maxOutputTokens"`
	Temperature     *float64     `json:"temperature,omitempty"`
	TopP            *float64     `json:"topP,omitempty"`
	TopK            *int         `json:"topK,omitempty"`
	Thinking        ThinkingMode `json:"thinking,omitempty"`
}

type OutputFormat struct {
	JSON   bool           `json:"json,omitempty"`
	Schema map[string]any `json:"schema,omitempty"`
}

type CompletionRequest struct {
	Model    string             `json:"model"`
	Messages []Message          `json:"messages"`
	Tools    []ToolDefinition   `json:"tools,omitempty"`
	Controls GenerationControls `json:"controls"`
	Format   OutputFormat       `json:"format,omitempty"`
}

// Generate emits content/thought deltas and complete validated tool calls only.
// Receipt observers are independent of tool authority and receive at most one
// terminal receipt, including on a later validation failure. Unknown counts
// remain TokenCountsPresent=false. No implementation downloads a model.
type CompletionBackend interface {
	Descriptor() BackendDescriptor
	ResolveModel(context.Context, string) (CompletionModel, error)
	Generate(context.Context, CompletionRequest, func(ChatChunk) error, ...func(ChatChunk)) error
}

func (c *Client) Descriptor() BackendDescriptor {
	return BackendDescriptor{Provider: ProviderOllama}
}

func (c *Client) ResolveModel(ctx context.Context, name string) (CompletionModel, error) {
	identity, err := c.ModelIdentity(ctx, name, "completion")
	if err != nil {
		return CompletionModel{}, err
	}
	model := CompletionModel{Name: identity.Name, ArtifactDigest: identity.Digest}
	model.CapabilityClaims = map[string]CapabilityClaim{}
	for _, capability := range []string{"completion", "tools", "thinking", "vision"} {
		model.CapabilityClaims[capability] = CapabilityClaim{State: "unsupported", Source: "ollama_model_metadata"}
	}
	for _, capability := range identity.Capabilities {
		model.CapabilityClaims[capability] = CapabilityClaim{State: "supported", Advertised: true, Source: "ollama_model_metadata"}
		switch capability {
		case "completion":
			model.Capabilities.Completion = true
		case "tools":
			model.Capabilities.Tools = true
		case "thinking":
			model.Capabilities.Thinking = true
		case "vision":
			model.Capabilities.Vision = true
		}
	}
	return model, nil
}

func (c *Client) Generate(ctx context.Context, req CompletionRequest, emit func(ChatChunk) error, observe ...func(ChatChunk)) error {
	chat, err := req.ollamaRequest()
	if err != nil {
		return err
	}
	return c.Chat(ctx, chat, emit, observe...)
}

func (c GenerationControls) validate() error {
	if c.ContextTokens < 0 || c.MaxOutputTokens < 0 {
		return errors.New("context and output token limits must not be negative")
	}
	for name, v := range map[string]*float64{"temperature": c.Temperature, "top_p": c.TopP} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if c.TopP != nil && *c.TopP > 1 || c.TopK != nil && *c.TopK < 0 {
		return errors.New("invalid top_p or top_k")
	}
	switch c.Thinking {
	case ThinkingDefault, ThinkingOn, ThinkingOff, ThinkingLow, ThinkingMedium, ThinkingHigh:
		return nil
	default:
		return errors.New("unsupported thinking mode")
	}
}

func (r CompletionRequest) ollamaRequest() (ChatRequest, error) {
	if err := r.Controls.validate(); err != nil {
		return ChatRequest{}, err
	}
	q := ChatRequest{Model: r.Model, Messages: r.Messages, Tools: r.Tools, Stream: true, Options: map[string]any{}}
	if r.Controls.ContextTokens > 0 {
		q.Options["num_ctx"] = r.Controls.ContextTokens
	}
	if r.Controls.MaxOutputTokens > 0 {
		q.Options["num_predict"] = r.Controls.MaxOutputTokens
	}
	if r.Controls.Temperature != nil {
		q.Options["temperature"] = *r.Controls.Temperature
	}
	if r.Controls.TopP != nil {
		q.Options["top_p"] = *r.Controls.TopP
	}
	if r.Controls.TopK != nil {
		q.Options["top_k"] = *r.Controls.TopK
	}
	switch r.Controls.Thinking {
	case ThinkingOn:
		q.Think = true
	case ThinkingOff:
		q.Think = false
	case ThinkingLow, ThinkingMedium, ThinkingHigh:
		q.Think = string(r.Controls.Thinking)
	}
	if r.Format.Schema != nil {
		q.Format = r.Format.Schema
	} else if r.Format.JSON {
		q.Format = "json"
	}
	return q, nil
}

// CompletionFromChat is the temporary engine migration seam. Reject unknown
// options instead of dropping a caller's requested generation control.
func CompletionFromChat(q ChatRequest) (CompletionRequest, error) {
	r := CompletionRequest{Model: q.Model, Messages: q.Messages, Tools: q.Tools}
	for key, value := range q.Options {
		b, err := json.Marshal(value)
		if err != nil {
			return r, err
		}
		switch key {
		case "num_ctx":
			err = json.Unmarshal(b, &r.Controls.ContextTokens)
		case "num_predict":
			err = json.Unmarshal(b, &r.Controls.MaxOutputTokens)
		case "temperature", "top_p":
			var f float64
			err = json.Unmarshal(b, &f)
			if key == "temperature" {
				r.Controls.Temperature = &f
			} else {
				r.Controls.TopP = &f
			}
		case "top_k":
			var n int
			err = json.Unmarshal(b, &n)
			r.Controls.TopK = &n
		default:
			return r, fmt.Errorf("unmapped generation option %q", key)
		}
		if err != nil {
			return r, fmt.Errorf("invalid %s: %w", key, err)
		}
	}
	switch think := q.Think.(type) {
	case nil:
	case bool:
		if think {
			r.Controls.Thinking = ThinkingOn
		} else {
			r.Controls.Thinking = ThinkingOff
		}
	case string:
		r.Controls.Thinking = ThinkingMode(think)
	default:
		return r, errors.New("invalid thinking control")
	}
	if q.Format != nil {
		if text, ok := q.Format.(string); ok && text == "json" {
			r.Format.JSON = true
		} else {
			b, err := json.Marshal(q.Format)
			if err != nil {
				return r, err
			}
			if err := json.Unmarshal(b, &r.Format.Schema); err != nil || r.Format.Schema == nil {
				return r, errors.New("structured output format must be a JSON schema object")
			}
		}
	}
	return r, r.Controls.validate()
}

var _ CompletionBackend = (*Client)(nil)
