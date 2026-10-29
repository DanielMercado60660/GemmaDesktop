package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gemmadesktop.local/app/internal/execution"
	mcpclient "gemmadesktop.local/app/internal/mcp"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/workspace"
)

type extensions struct {
	core      *Core
	mcp       *mcpclient.Manager
	terminals *execution.TerminalManager
}

func newExtensions(c *Core) (*extensions, error) {
	x := &extensions{core: c, mcp: mcpclient.NewManager()}
	x.terminals = execution.NewTerminalManager(func(ctx context.Context, a execution.Approval) error {
		return c.approve(ctx, "", a.Tool, a.Arguments, a.Reason)
	})
	configs, e := c.Store.List("mcp")
	if e != nil {
		return nil, e
	}
	for _, d := range configs {
		d["status"] = "disconnected"
		if e = c.Store.Put("mcp", d); e != nil {
			return nil, e
		}
	}
	return x, nil
}
func (x *extensions) Close() { x.mcp.Close(); x.terminals.Close() }
func (x *extensions) workspace(p Doc) (*workspace.Workspace, error) {
	w, e := x.core.Store.Get("workspace", str(p, "workspaceId"))
	if e != nil {
		return nil, e
	}
	return workspace.Open(str(w, "path"))
}
func (x *extensions) Call(ctx context.Context, method string, p Doc) (any, error) {
	c := x.core
	switch method {
	case "file.list", "file.read", "file.write", "git.diff":
		w, e := x.workspace(p)
		if e != nil {
			return nil, e
		}
		defer w.Close()
		switch method {
		case "file.list":
			entries, e := w.List(str(p, "path"))
			return Doc{"entries": entries}, e
		case "file.read":
			return w.Read(str(p, "path"))
		case "file.write":
			if c.workspaceBusy(str(p, "workspaceId")) {
				return nil, errors.New("Wait for the active workspace run before saving edits")
			}
			if c.Settings()["mode"] == "read-only" {
				return nil, errors.New("Workspace is in read-only mode")
			}
			_, release, err := workspace.AcquireOperation(ctx, w.Path)
			if err != nil {
				return nil, err
			}
			defer release()
			change, e := w.Write(str(p, "path"), str(p, "content"), str(p, "expectedHash"))
			if e != nil {
				return nil, e
			}
			return change, nil
		case "git.diff":
			return execution.WorkspaceChanges(ctx, w.Path)
		}
	case "terminal.start":
		w, e := x.workspace(p)
		if e != nil {
			return nil, e
		}
		defer w.Close()
		return x.terminals.Start(c.ctx, w.Path)
	case "terminal.write":
		return Doc{}, x.terminals.Write(str(p, "id"), str(p, "data"))
	case "terminal.resize":
		return Doc{}, x.terminals.Resize(str(p, "id"), uint16(number(p, "cols", 100)), uint16(number(p, "rows", 30)))
	case "terminal.read":
		r, e := x.terminals.Read(str(p, "id"), int64(number(p, "offset", 0)))
		d := obj(r)
		d["exited"] = !r.Running
		return d, e
	case "terminal.stop":
		return Doc{}, x.terminals.Stop(str(p, "id"))
	case "mcp.save":
		var err error
		p, err = sanitizeMCPConfig(p)
		if err != nil {
			return nil, err
		}
		if _, e := required(p, "name"); e != nil {
			return nil, e
		}
		if p["transport"] != "stdio" && p["transport"] != "http" && p["transport"] != "streamable-http" {
			return nil, errors.New("Choose stdio or http transport")
		}
		if strings.Contains(str(p, "url"), "@") || strings.Contains(str(p, "url"), "?") {
			return nil, errors.New("Do not store credentials in the MCP URL")
		}
		if str(p, "id") == "" {
			p["id"] = store.ID()
		} else {
			if err := c.invalidateOAuthOnConfigChange(ctx, str(p, "id"), p); err != nil {
				return nil, err
			}
			x.mcp.Disconnect(str(p, "id"))
		}
		p["status"] = "disconnected"
		return p, c.Store.Put("mcp", p)
	case "mcp.connect":
		d, e := c.Store.Get("mcp", str(p, "id"))
		if e != nil {
			return nil, e
		}
		w, e := x.workspace(p)
		if e != nil {
			return nil, fmt.Errorf("Select a workspace before connecting this server: %w", e)
		}
		defer w.Close()
		var config mcpclient.Config
		b, _ := json.Marshal(d)
		if e = json.Unmarshal(b, &config); e != nil {
			return nil, e
		}
		server, e := x.mcp.Connect(c.ctx, config, w.Path, func(ctx context.Context, a execution.Approval) error {
			return c.approve(ctx, "", a.Tool, a.Arguments, a.Reason)
		})
		if e != nil {
			d["status"] = "error"
			d["error"] = e.Error()
			_ = c.Store.Put("mcp", d)
			return nil, e
		}
		actual := obj(server)
		for k, v := range actual {
			d[k] = v
		}
		d["workspaceId"] = p["workspaceId"]
		d["status"] = "connected"
		delete(d, "error")
		return d, c.Store.Put("mcp", d)
	case "mcp.delete":
		if err := c.invalidateOAuthOnConfigChange(ctx, str(p, "id"), nil); err != nil {
			return nil, err
		}
		x.mcp.Disconnect(str(p, "id"))
		return Doc{}, c.Store.Delete("mcp", str(p, "id"))
	}
	return nil, fmt.Errorf("Unknown application method %q", method)
}
