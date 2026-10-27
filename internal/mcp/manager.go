// Package mcp connects external tools through the official MCP Go SDK.
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"gemmadesktop.local/app/internal/execution"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	URL       string   `json:"url,omitempty"`
	Enabled   bool     `json:"enabled"`
}
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
	SchemaHash  string `json:"schemaHash"`
	ServerID    string `json:"serverId"`
}
type Server struct {
	Config
	Workspace  string `json:"workspace"`
	ConfigHash string `json:"configHash"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Tools      []Tool `json:"tools"`
}
type CallResult struct {
	Content        string `json:"content"`
	Data           any    `json:"data,omitempty"`
	IsError        bool   `json:"isError"`
	OutcomeUnknown bool   `json:"outcomeUnknown,omitempty"`
	NeedsInput     bool   `json:"needsInput,omitempty"`
}
type connected struct {
	server  Server
	session *sdk.ClientSession
	cancel  context.CancelFunc
	cleanup func()
	command *exec.Cmd
}
type Manager struct {
	mu        sync.Mutex
	connectMu sync.Mutex
	servers   map[string]*connected
	life      context.Context
	cancel    context.CancelFunc
}

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{servers: map[string]*connected{}, life: ctx, cancel: cancel}
}

func canonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("MCP workspace must be a directory")
	}
	return resolved, nil
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func validateConfig(c Config) error {
	if c.ID == "" || c.Name == "" || len(c.ID) > 128 || len(c.Name) > 200 {
		return errors.New("MCP server id and name are required")
	}
	if !c.Enabled {
		return errors.New("MCP server is disabled")
	}
	switch c.Transport {
	case "stdio":
		if c.Command == "" {
			return errors.New("stdio command is required")
		}
	case "http", "streamable-http":
		if c.URL == "" {
			return errors.New("HTTP endpoint is required")
		}
	default:
		return errors.New("supported MCP transports are stdio and streamable-http")
	}
	return nil
}

func (m *Manager) Connect(ctx context.Context, cfg Config, workspace string, authorize execution.Authorizer) (Server, error) {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()
	cfg.Args = append([]string{}, cfg.Args...)
	if err := validateConfig(cfg); err != nil {
		return Server{}, err
	}
	root, err := canonicalRoot(workspace)
	if err != nil {
		return Server{}, err
	}
	if err = execution.Require(ctx, authorize, execution.Approval{Tool: "mcp.connect", Arguments: map[string]any{"server": cfg, "workspace": root}, Workspace: root, Environment: cfg.Transport, Reason: "Connect this explicitly configured MCP server. Its tool descriptions are untrusted; tool calls require separate approval."}); err != nil {
		return Server{}, err
	}
	if err = m.Disconnect(cfg.ID); err != nil {
		return Server{}, err
	}
	life, cancel := context.WithCancel(m.life)
	cleanup := func() {}
	var transport sdk.Transport
	var cmd *exec.Cmd
	if cfg.Transport == "stdio" {
		cmd, cleanup, err = execution.NewRunner().Prepare(life, execution.Request{Workspace: root, Executable: cfg.Command, Args: cfg.Args})
		if err != nil {
			cancel()
			return Server{}, err
		}
		cmd.Stderr = &execution.BoundedBuffer{Limit: 32 << 10}
		transport = &sdk.CommandTransport{Command: cmd, TerminateDuration: 500 * time.Millisecond}
	} else {
		client, _, e := execution.EndpointClient(cfg.URL)
		if e != nil {
			cancel()
			return Server{}, e
		}
		execution.BoundJSONResponses(client, 2<<20)
		transport = &sdk.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: client, MaxRetries: -1, MaxEventSize: 1 << 20}
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "Gemma Desktop", Version: "0.1.0"}, &sdk.ClientOptions{Capabilities: &sdk.ClientCapabilities{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	connectCtx, ccancel := context.WithTimeout(ctx, 20*time.Second)
	defer ccancel()
	cs, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		cancel()
		cleanup()
		return Server{}, fmt.Errorf("MCP connection failed: %w", err)
	}
	conn := &connected{server: Server{Config: cfg, Workspace: root, ConfigHash: digest(cfg), Status: "connected", Tools: []Tool{}}, session: cs, cancel: cancel, cleanup: cleanup, command: cmd}
	listCtx, lcancel := context.WithTimeout(ctx, 15*time.Second)
	defer lcancel()
	conn.server.Tools, err = discover(listCtx, cs, cfg.ID)
	if err != nil {
		_ = cs.Close()
		cancel()
		cleanup()
		return Server{}, err
	}
	m.mu.Lock()
	if m.life.Err() != nil {
		m.mu.Unlock()
		_ = cs.Close()
		cancel()
		cleanup()
		return Server{}, errors.New("MCP manager closed")
	}
	m.servers[cfg.ID] = conn
	result := conn.server
	result.Tools = append([]Tool{}, result.Tools...)
	m.mu.Unlock()
	return result, nil
}
func discover(ctx context.Context, cs *sdk.ClientSession, id string) ([]Tool, error) {
	out := []Tool{}
	cursor := ""
	seen := map[string]bool{}
	names := map[string]bool{}
	for page := 0; page < 20; page++ {
		res, err := cs.ListTools(ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, t := range res.Tools {
			if t == nil || names[t.Name] {
				return nil, errors.New("MCP tool names must be present and unique")
			}
			names[t.Name] = true
			if len(out) >= 200 {
				return nil, errors.New("MCP server advertises more than 200 tools")
			}
			schema, err := json.Marshal(t.InputSchema)
			if err != nil || len(schema) > 128<<10 {
				return nil, errors.New("invalid or oversized tool schema")
			}
			if t.Name == "" || len(t.Name) > 200 || len(t.Description) > 16<<10 {
				return nil, errors.New("invalid or oversized tool description")
			}
			out = append(out, Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, SchemaHash: digest(t.InputSchema), ServerID: id})
		}
		if res.NextCursor == "" {
			return out, nil
		}
		if seen[res.NextCursor] {
			return nil, errors.New("MCP repeated pagination cursor")
		}
		seen[res.NextCursor] = true
		cursor = res.NextCursor
	}
	return nil, errors.New("MCP tool pagination limit exceeded")
}

func (m *Manager) List() []Server {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Server, 0, len(m.servers))
	for _, c := range m.servers {
		s := c.server
		s.Tools = append([]Tool{}, s.Tools...)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (m *Manager) ToolsForWorkspace(root string) []Tool {
	root, err := canonicalRoot(root)
	if err != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Tool{}
	for _, c := range m.servers {
		if c.server.Workspace == root && c.server.Status == "connected" {
			out = append(out, c.server.Tools...)
		}
	}
	return out
}
func (m *Manager) CallInWorkspace(ctx context.Context, id, name string, args map[string]any, workspace string, authorize execution.Authorizer) (CallResult, error) {
	root, err := canonicalRoot(workspace)
	if err != nil {
		return CallResult{}, err
	}
	m.mu.Lock()
	c := m.servers[id]
	if c == nil || c.server.Workspace != root {
		m.mu.Unlock()
		return CallResult{}, errors.New("MCP server is not connected to this workspace")
	}
	m.mu.Unlock()
	return m.call(ctx, c, id, name, args, authorize)
}
func (m *Manager) call(ctx context.Context, c *connected, id, name string, args map[string]any, authorize execution.Authorizer) (CallResult, error) {
	b, err := json.Marshal(args)
	if err != nil || len(b) > 256<<10 {
		return CallResult{}, errors.New("tool arguments exceed limit")
	}
	var snapshot map[string]any
	if err = json.Unmarshal(b, &snapshot); err != nil {
		return CallResult{}, err
	}
	args = snapshot
	// Refresh before every call so changed schemas cannot reuse an earlier approval.
	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	list, err := discover(refreshCtx, c.session, id)
	cancel()
	if err != nil {
		return CallResult{}, err
	}
	var selected *Tool
	for i := range list {
		if list[i].Name == name {
			selected = &list[i]
			break
		}
	}
	if selected == nil {
		return CallResult{}, errors.New("MCP tool is no longer advertised")
	}
	schemaBytes, _ := json.Marshal(selected.InputSchema)
	var schema jsonschema.Schema
	if err = json.Unmarshal(schemaBytes, &schema); err != nil {
		return CallResult{}, fmt.Errorf("invalid tool schema: %w", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return CallResult{}, fmt.Errorf("unsupported tool schema: %w", err)
	}
	if err = resolved.Validate(args); err != nil {
		return CallResult{}, fmt.Errorf("tool arguments: %w", err)
	}
	if err = execution.Require(ctx, authorize, execution.Approval{Tool: "mcp.call", Arguments: map[string]any{"serverId": id, "tool": name, "arguments": args, "schemaHash": selected.SchemaHash, "configHash": c.server.ConfigHash}, Workspace: c.server.Workspace, Environment: c.server.Transport, Reason: "Allow this MCP tool invocation. Server annotations cannot establish that it is read-only."}); err != nil {
		return CallResult{}, err
	}
	refreshCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
	fresh, err := discover(refreshCtx, c.session, id)
	cancel()
	if err != nil {
		return CallResult{}, err
	}
	unchanged := false
	for _, t := range fresh {
		if t.Name == name && t.SchemaHash == selected.SchemaHash {
			unchanged = true
		}
	}
	if !unchanged {
		return CallResult{}, errors.New("MCP tool schema changed while approval was pending; review again")
	}
	m.mu.Lock()
	if m.servers[id] != c {
		m.mu.Unlock()
		return CallResult{}, errors.New("MCP connection changed while approval was pending")
	}
	c.server.Tools = list
	m.mu.Unlock()
	callCtx, callCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer callCancel()
	res, err := c.session.CallTool(callCtx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return CallResult{OutcomeUnknown: true}, fmt.Errorf("MCP call outcome is unknown; do not automatically retry a write: %w", err)
	}
	if res.NeedsInput() {
		return CallResult{NeedsInput: true, Data: res}, errors.New("MCP server requested user input; resume requires an explicit answer")
	}
	b, err = json.Marshal(res)
	if err != nil {
		return CallResult{}, err
	}
	if len(b) > 2<<20 {
		return CallResult{OutcomeUnknown: true}, errors.New("MCP result exceeds 2 MiB limit; do not automatically retry the operation")
	}
	var text strings.Builder
	for _, item := range res.Content {
		if t, ok := item.(*sdk.TextContent); ok {
			text.WriteString(t.Text)
			text.WriteByte('\n')
		}
	}
	return CallResult{Content: text.String(), Data: res, IsError: res.IsError}, nil
}
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	c := m.servers[id]
	delete(m.servers, id)
	m.mu.Unlock()
	if c == nil {
		return nil
	}
	c.cancel()
	if c.command != nil && c.command.Process != nil {
		_ = syscall.Kill(-c.command.Process.Pid, syscall.SIGKILL)
	}
	err := c.session.Close()
	c.cleanup()
	return err
}
func (m *Manager) Close() error {
	m.cancel()
	m.mu.Lock()
	ids := make([]string, 0, len(m.servers))
	for id := range m.servers {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if err := m.Disconnect(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
