package inference

import "encoding/json"

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Thinking   string     `json:"thinking,omitempty"`
	Images     []string   `json:"images,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Index     int            `json:"index,omitempty"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type ChatRequest struct {
	Model     string           `json:"model"`
	Messages  []Message        `json:"messages"`
	Tools     []ToolDefinition `json:"tools,omitempty"`
	Stream    bool             `json:"stream"`
	Think     any              `json:"think,omitempty"`
	Format    any              `json:"format,omitempty"`
	Options   map[string]any   `json:"options,omitempty"`
	KeepAlive any              `json:"keep_alive,omitempty"`
	// Explicit false values preserve the host's history. Chat enforces false
	// even when a caller sets these fields; do not add omitempty.
	Truncate bool `json:"truncate"`
	Shift    bool `json:"shift"`
}

type Metrics struct {
	TotalDuration      int64 `json:"total_duration,omitempty"`
	LoadDuration       int64 `json:"load_duration,omitempty"`
	PromptEvalCount    int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"`
	EvalCount          int   `json:"eval_count,omitempty"`
	EvalDuration       int64 `json:"eval_duration,omitempty"`
}

type ChatChunk struct {
	// Set by the decoder from explicit JSON member presence, not nonzero values.
	TokenCountsPresent bool    `json:"-"`
	Model              string  `json:"model"`
	Message            Message `json:"message"`
	Done               bool    `json:"done"`
	DoneReason         string  `json:"done_reason,omitempty"`
	Error              string  `json:"error,omitempty"`
	Metrics
}

type Model struct {
	Name          string       `json:"name"`
	Model         string       `json:"model,omitempty"`
	Size          int64        `json:"size"`
	Digest        string       `json:"digest"`
	ModifiedAt    string       `json:"modified_at,omitempty"`
	Details       ModelDetails `json:"details"`
	RemoteModel   string       `json:"remote_model,omitempty"`
	RemoteHost    string       `json:"remote_host,omitempty"`
	SizeVRAM      int64        `json:"size_vram,omitempty"`
	ContextLength int          `json:"context_length,omitempty"`
	ExpiresAt     string       `json:"expires_at,omitempty"`
}

type ModelDetails struct {
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families,omitempty"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

type ModelInfo struct {
	Capabilities []string                   `json:"capabilities"`
	Details      ModelDetails               `json:"details"`
	ModelInfo    map[string]any             `json:"model_info"`
	Parameters   string                     `json:"parameters,omitempty"`
	RemoteModel  string                     `json:"remote_model,omitempty"`
	RemoteHost   string                     `json:"remote_host,omitempty"`
	Raw          map[string]json.RawMessage `json:"-"`
}

type PullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
	Error     string `json:"error,omitempty"`
}

// EmbedInput represents one independent embedding. Image contains encoded image
// bytes, not a URL or filename. Text is passed exactly as supplied.
type EmbedInput struct {
	Text  string
	Image []byte
}

type EmbedRequest struct {
	Model          string
	ExpectedDigest string
	Input          []EmbedInput
}

type EmbedResponse struct {
	Model           string      `json:"model"`
	Digest          string      `json:"-"`
	Embeddings      [][]float32 `json:"embeddings"`
	TotalDuration   int64       `json:"total_duration"`
	LoadDuration    int64       `json:"load_duration"`
	PromptEvalCount int         `json:"prompt_eval_count"`
}

// ModelIdentity is local metadata verified against both tags and show. Digests
// are normalized to sha256:<hex> for stable index provenance.
type ModelIdentity struct {
	Name         string
	Digest       string
	Capabilities []string
}
