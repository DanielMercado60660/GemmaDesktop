package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestApprovalDiscoveryRejectsUnnotifiedTTLSchemaDrift(t *testing.T) {
	for _, outputDrift := range []bool{false, true} {
		name := "input"
		if outputDrift {
			name = "output"
		}
		t.Run(name, func(t *testing.T) {
			var lists, calls atomic.Int32
			var changed atomic.Bool
			s := sdk.NewServer(&sdk.Implementation{Name: "TTL fixture", Version: "1"}, nil)
			s.AddTool(&sdk.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				calls.Add(1)
				return &sdk.CallToolResult{StructuredContent: map[string]any{"value": 1}}, nil
			})
			// Change only future advertisements, without AddTool/list-change
			// notifications. A positive TTL must not hide this pending-approval drift.
			s.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
				return func(ctx context.Context, method string, request sdk.Request) (sdk.Result, error) {
					result, err := next(ctx, method, request)
					if list, ok := result.(*sdk.ListToolsResult); err == nil && ok {
						lists.Add(1)
						copy := *list
						copy.TTLMs = 60_000
						tool := *list.Tools[0]
						tool.InputSchema = map[string]any{"type": "object"}
						tool.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "integer"}}}
						if changed.Load() {
							if outputDrift {
								tool.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}}
							} else {
								tool.InputSchema = map[string]any{"type": "object", "required": []string{"newField"}}
							}
						}
						copy.Tools = []*sdk.Tool{&tool}
						return &copy, nil
					}
					return result, err
				}
			})
			server := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, &sdk.StreamableHTTPOptions{Stateless: true}))
			defer server.Close()
			// Control: the same pinned SDK without the host middleware caches it.
			plain := sdk.NewClient(&sdk.Implementation{Name: "cache control", Version: "1"}, nil)
			session, err := plain.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: server.URL}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if list, err := session.ListTools(t.Context(), &sdk.ListToolsParams{}); err != nil {
					t.Fatal(err)
				} else if list.TTLMs <= 0 || session.InitializeResult().ProtocolVersion != "2026-07-28" {
					t.Fatalf("fixture missing new protocol positive TTL: protocol=%s ttl=%d", session.InitializeResult().ProtocolVersion, list.TTLMs)
				}
			}
			_ = session.Close()
			if lists.Load() != 1 {
				t.Fatalf("fixture did not exercise SDK TTL caching: %d requests", lists.Load())
			}
			m := NewManager()
			defer m.Close()
			root := t.TempDir()
			approve := func(context.Context, execution.Approval) error { return nil }
			if _, err := m.Connect(t.Context(), Config{ID: "fixture", Name: "TTL fixture", Transport: "http", URL: server.URL, Enabled: true}, root, approve); err != nil {
				t.Fatal(err)
			}
			_, err = m.CallInWorkspace(t.Context(), "fixture", "echo", map[string]any{}, root, func(context.Context, execution.Approval) error {
				changed.Store(true)
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "schema changed") || calls.Load() != 0 || lists.Load() != 4 {
				t.Fatalf("drift error=%v calls=%d server lists=%d", err, calls.Load(), lists.Load())
			}
		})
	}
}

func TestDeclaredStructuredOutputValidation(t *testing.T) {
	for _, test := range []struct {
		name                              string
		value                             any
		toolError, wantError, unsupported bool
	}{
		{name: "valid", value: map[string]any{"value": 7}},
		{name: "wrong type", value: map[string]any{"value": "seven"}, wantError: true},
		{name: "missing structured output", wantError: true},
		{name: "tool error may omit success shape", toolError: true},
		{name: "remote schema is not fetched", wantError: true, unsupported: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			s := sdk.NewServer(&sdk.Implementation{Name: "output fixture", Version: "1"}, nil)
			s.AddTool(&sdk.Tool{Name: "fixture", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				calls.Add(1)
				return &sdk.CallToolResult{StructuredContent: test.value, IsError: test.toolError, Content: []sdk.Content{&sdk.TextContent{Text: "fixture result"}}}, nil
			})
			s.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
				return func(ctx context.Context, method string, request sdk.Request) (sdk.Result, error) {
					result, err := next(ctx, method, request)
					if list, ok := result.(*sdk.ListToolsResult); err == nil && ok {
						copy, tool := *list, *list.Tools[0]
						tool.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "integer"}}, "required": []string{"value"}, "additionalProperties": false}
						if test.unsupported {
							tool.OutputSchema = map[string]any{"$ref": "https://example.invalid/never-fetch-schema"}
						}
						copy.Tools = []*sdk.Tool{&tool}
						return &copy, nil
					}
					return result, err
				}
			})
			server := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, nil))
			defer server.Close()
			m := NewManager()
			defer m.Close()
			root := t.TempDir()
			approvals := 0
			approve := func(context.Context, execution.Approval) error { approvals++; return nil }
			if _, err := m.Connect(t.Context(), Config{ID: "fixture", Name: "output fixture", Transport: "http", URL: server.URL, Enabled: true}, root, approve); err != nil {
				t.Fatal(err)
			}
			result, err := m.CallInWorkspace(t.Context(), "fixture", "fixture", map[string]any{}, root, approve)
			if (err != nil) != test.wantError {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if test.unsupported {
				if calls.Load() != 0 || approvals != 1 || !strings.Contains(err.Error(), "no loader") {
					t.Fatalf("unsupported schema reached approval/execution: calls=%d approvals=%d err=%v", calls.Load(), approvals, err)
				}
			} else if calls.Load() != 1 || approvals != 2 {
				t.Fatalf("calls=%d approvals=%d", calls.Load(), approvals)
			} else if test.wantError && (!result.OutcomeUnknown || result.Data != nil || !strings.Contains(err.Error(), "do not automatically retry")) {
				t.Fatalf("invalid output lost outcome caution: %+v %v", result, err)
			} else if !test.wantError && result.IsError != test.toolError {
				t.Fatalf("lost tool error: %+v", result)
			}
		})
	}
}
