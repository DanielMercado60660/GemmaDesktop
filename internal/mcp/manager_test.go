package mcp

import (
	"context"
	"errors"
	"gemmadesktop.local/app/internal/execution"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRealSDKDiscoverySchemaApprovalAndWorkspaceBoundary(t *testing.T) {
	s := sdk.NewServer(&sdk.Implementation{Name: "Gemma fixture", Version: "1"}, nil)
	var calls atomic.Int32
	add := func(extra bool) {
		properties := map[string]any{"message": map[string]any{"type": "string"}}
		if extra {
			properties["changed"] = map[string]any{"type": "boolean"}
		}
		s.AddTool(&sdk.Tool{Name: "echo", Description: "Fixture echo", InputSchema: map[string]any{"type": "object", "properties": properties, "required": []string{"message"}, "additionalProperties": false}}, func(ctx context.Context, r *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "fixture reply"}}}, nil
		})
	}
	add(false)
	server := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, nil))
	defer server.Close()
	m := NewManager()
	defer m.Close()
	root := t.TempDir()
	config := Config{ID: "fixture", Name: "Fixture", Transport: "streamable-http", URL: server.URL, Enabled: true}
	approvals := 0
	approve := func(ctx context.Context, a execution.Approval) error {
		approvals++
		if a.Workspace == "" {
			t.Error("missing workspace binding")
		}
		return nil
	}
	if _, err := m.Connect(context.Background(), config, root, nil); !errors.Is(err, execution.ErrDenied) {
		t.Fatalf("unapproved connect %v", err)
	}
	connected, err := m.Connect(context.Background(), config, root, approve)
	if err != nil {
		t.Fatal(err)
	}
	if len(connected.Tools) != 1 || connected.Tools[0].SchemaHash == "" {
		t.Fatalf("discovery %+v", connected)
	}
	if _, err = m.CallInWorkspace(context.Background(), "fixture", "echo", map[string]any{"message": "hi"}, t.TempDir(), approve); err == nil {
		t.Fatal("cross-workspace call accepted")
	}
	if calls.Load() != 0 || approvals != 1 {
		t.Fatal("cross-workspace call reached approval/server")
	}
	if _, err = m.CallInWorkspace(context.Background(), "fixture", "echo", map[string]any{"message": 1}, root, approve); err == nil {
		t.Fatal("invalid schema arguments accepted")
	}
	if calls.Load() != 0 || approvals != 1 {
		t.Fatal("invalid arguments reached server")
	}
	if _, err = m.CallInWorkspace(context.Background(), "fixture", "echo", map[string]any{"message": "hi"}, root, nil); !errors.Is(err, execution.ErrDenied) {
		t.Fatalf("unapproved call %v", err)
	}
	r, err := m.CallInWorkspace(context.Background(), "fixture", "echo", map[string]any{"message": "hi"}, root, approve)
	if err != nil || !strings.Contains(r.Content, "fixture reply") || calls.Load() != 1 {
		t.Fatalf("call %+v %v", r, err)
	}
	_, err = m.CallInWorkspace(context.Background(), "fixture", "echo", map[string]any{"message": "hi"}, root, func(context.Context, execution.Approval) error { add(true); return nil })
	if err == nil || !strings.Contains(err.Error(), "schema changed") || calls.Load() != 1 {
		t.Fatalf("schema changed after approval: %v calls=%d", err, calls.Load())
	}
	if len(m.ToolsForWorkspace(t.TempDir())) != 0 {
		t.Fatal("tools leaked to other workspace")
	}
}
