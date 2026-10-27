package engine

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"gemmadesktop.local/app/internal/inference"
)

type Message = inference.Message
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	NodeID    string         `json:"nodeId,omitempty"`
	ReadOnly  bool           `json:"readOnly,omitempty"`
}
type ToolResult struct {
	Content string         `json:"content,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	IsError bool           `json:"isError,omitempty"`
}
type ToolExecutor func(context.Context, ToolCall) (ToolResult, error)
type Event struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	NodeID string `json:"nodeId,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Data   any    `json:"data,omitempty"`
}

type Request struct {
	Model        string           `json:"model"`
	Instructions string           `json:"instructions"`
	ReadOnly     bool             `json:"readOnly,omitempty"`
	History      []Message        `json:"history,omitempty"`
	Prompt       string           `json:"prompt"`
	Images       []string         `json:"images,omitempty"`
	Tools        []ToolDefinition `json:"tools,omitempty"`
	Skills       []string         `json:"skills,omitempty"`
	MaxSteps     int              `json:"maxSteps,omitempty"`
	MaxToolCalls int              `json:"maxToolCalls,omitempty"`
	ContextSize  int              `json:"contextSize,omitempty"`
	Temperature  float64          `json:"temperature,omitempty"`
	Think        any              `json:"think,omitempty"`
	Timeout      time.Duration    `json:"-"`
	NodeID       string           `json:"nodeId,omitempty"`
	Snapshot     json.RawMessage  `json:"snapshot,omitempty"`
	Resume       map[string]any   `json:"resume,omitempty"`
}
type Result struct {
	Content  string            `json:"content"`
	Steps    int               `json:"steps"`
	Usage    inference.Metrics `json:"usage"`
	Snapshot json.RawMessage   `json:"snapshot,omitempty"`
	Pending  []PendingInput    `json:"pending,omitempty"`
}
type PendingInput struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Payload any    `json:"payload,omitempty"`
}

// Budget is shared by nested runs via WithBudget. It is independent of ADK's
// per-run limits so nested agents cannot reset the application's allowance.
type Budget struct {
	mu             sync.Mutex
	ModelCalls     int
	ToolCalls      int
	MaxModelCalls  int
	MaxToolCalls   int
	OutputBytes    int
	MaxOutputBytes int
}
type budgetKey struct{}

func WithBudget(ctx context.Context, b *Budget) context.Context {
	return context.WithValue(ctx, budgetKey{}, b)
}
func BudgetFromContext(ctx context.Context) *Budget {
	b, _ := ctx.Value(budgetKey{}).(*Budget)
	return b
}
