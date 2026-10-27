package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/tools"
	"gemmadesktop.local/app/internal/workflow"
	"gemmadesktop.local/app/internal/workspace"
)

func (c *Core) startRun(p Doc, workflowID string) (any, error) {
	session, e := c.Store.Get("session", str(p, "sessionId"))
	if e != nil {
		return nil, e
	}
	w, e := c.Store.Get("workspace", str(session, "workspaceId"))
	if e != nil {
		return nil, e
	}
	prompt, e := required(p, "prompt")
	if e != nil {
		return nil, e
	}
	if len(prompt) > 100000 {
		return nil, errors.New("Prompt is too long")
	}
	settings := c.Settings()
	agentID := str(p, "agentId")
	if agentID == "" {
		agentID = "default"
	}
	agent, e := c.Store.Get("agent", agentID)
	if e != nil {
		return nil, e
	}
	model := str(p, "model")
	if model == "" {
		model = str(agent, "model")
	}
	if model == "" {
		model = str(settings, "defaultModel")
	}
	req := engine.Request{Model: model, Instructions: defaultInstructions + "\n\n" + str(agent, "instructions"), Prompt: prompt, MaxSteps: number(agent, "maxSteps", number(settings, "maxSteps", 24)), ContextSize: number(settings, "contextSize", 32768), Temperature: 0.2, Timeout: 20 * time.Minute}
	msgs, e := c.Store.List("message")
	if e != nil {
		return nil, e
	}
	for _, m := range msgs {
		if m["sessionId"] == session["id"] {
			req.History = append(req.History, engine.Message{Role: str(m, "role"), Content: str(m, "content")})
		}
	}
	if workflowID == "" {
		previous, e := c.Store.List("run")
		if e != nil {
			return nil, e
		}
		for i := len(previous) - 1; i >= 0; i-- {
			r := previous[i]
			if r["sessionId"] == session["id"] {
				if cp, e := c.Store.Get("checkpoint", str(r, "id")); str(r, "workflowId") == "" && r["status"] == "completed" && e == nil && cp["snapshot"] != nil {
					req.Snapshot, _ = json.Marshal(cp["snapshot"])
				}
				break
			}
		}
	}
	// Attachment paths have the same protection as model file tools.
	if a, ok := p["attachments"].([]any); ok {
		root, e := workspace.Open(str(w, "path"))
		if e != nil {
			return nil, e
		}
		defer root.Close()
		for _, v := range a {
			path, ok := v.(string)
			if !ok {
				return nil, errors.New("Attachment path must be a string")
			}
			f, e := root.Read(path)
			if e != nil {
				return nil, e
			}
			if len(f.Content) > 128<<10 {
				return nil, errors.New("Text attachments are limited to 128 KiB each")
			}
			req.Prompt += "\n\nUntrusted attachment: " + path + "\n" + f.Content
		}
	}
	skills, e := c.Store.List("skill")
	if e != nil {
		return nil, e
	}
	for _, s := range skills {
		req.Skills = append(req.Skills, filepath.Join(str(s, "path"), "SKILL.md"))
	}
	mode := str(agent, "mode")
	if settings["mode"] == "read-only" {
		mode = "read-only"
	}
	req.ReadOnly = mode == "read-only"
	snapshot := Doc{"id": "", "request": req, "workspace": w, "mode": mode, "agents": []Doc{}}
	if workflowID != "" {
		wf, e := c.Store.Get("workflow", workflowID)
		if e != nil {
			return nil, e
		}
		snapshot["workflow"] = wf
		all, e := c.Store.List("agent")
		if e != nil {
			return nil, e
		}
		snapshot["agents"] = all
	}
	id := store.ID()
	snapshot["id"] = id
	c.mu.Lock()
	if previous := c.workspaceRuns[str(w, "id")]; previous != "" {
		c.mu.Unlock()
		return nil, fmt.Errorf("This workspace already has active run %s", previous)
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.workspaceRuns[str(w, "id")] = id
	c.runs[id] = cancel
	c.mu.Unlock()
	cleanup := func() {
		cancel()
		c.mu.Lock()
		delete(c.runs, id)
		delete(c.workspaceRuns, str(w, "id"))
		c.mu.Unlock()
	}
	run := Doc{"id": id, "sessionId": session["id"], "workspaceId": w["id"], "status": "queued", "model": model, "prompt": prompt, "agentId": agentID, "workflowId": workflowID, "startedAt": store.Now()}
	if e = c.Store.Put("checkpoint", snapshot); e != nil {
		cleanup()
		return nil, e
	}
	if e = c.Store.Put("run", run); e != nil {
		cleanup()
		return nil, e
	}
	if e = c.Store.Put("message", Doc{"id": store.ID(), "sessionId": session["id"], "runId": id, "role": "user", "content": prompt, "createdAt": store.Now()}); e != nil {
		cleanup()
		return nil, e
	}
	session["updatedAt"] = store.Now()
	if str(session, "title") == "New conversation" {
		title := []rune(prompt)
		if len(title) > 60 {
			title = append(title[:60], '…')
		}
		session["title"] = string(title)
	}
	if e = c.Store.Put("session", session); e != nil {
		cleanup()
		return nil, e
	}
	c.wg.Add(1)
	go func() { defer c.wg.Done(); defer cleanup(); c.executeRun(ctx, run, snapshot, req) }()
	return run, nil
}
func (c *Core) cancelRun(id string) (any, error) {
	c.mu.Lock()
	cancel := c.runs[id]
	c.mu.Unlock()
	if cancel == nil {
		return nil, errors.New("Run is no longer active")
	}
	r, e := c.Store.Update("run", id, func(r Doc) error { r["status"] = "cancelling"; return nil })
	if e != nil {
		return nil, e
	}
	cancel()
	return r, nil
}
func (c *Core) executeRun(ctx context.Context, run, snapshot Doc, req engine.Request) {
	id := str(run, "id")
	w := obj(snapshot["workspace"])
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	var fatal error
	var fatalMu sync.Mutex
	emit := func(ev engine.Event) {
		d := obj(ev)
		d["runId"] = id
		if _, e := c.Store.Event(d); e != nil {
			fatalMu.Lock()
			fatal = e
			fatalMu.Unlock()
			cancel()
		}
	}
	if _, e := c.Store.Update("run", id, func(r Doc) error { r["status"] = "running"; return nil }); e != nil {
		return
	}
	mgr, e := c.runTools(ctx, id, w, str(snapshot, "mode"))
	if e != nil {
		c.finishRun(run, engine.Result{}, e)
		return
	}
	defer mgr.Close()
	readOnlyMgr, e := c.runTools(ctx, id, w, "read-only")
	if e != nil {
		c.finishRun(run, engine.Result{}, e)
		return
	}
	defer readOnlyMgr.Close()
	req.Tools = nil
	for _, d := range mgr.List() {
		req.Tools = append(req.Tools, engine.ToolDefinition{Name: d.Name, Description: d.Description, Parameters: d.Parameters})
	}
	var executionMu sync.Mutex
	executor := func(toolCtx context.Context, call engine.ToolCall) (engine.ToolResult, error) {
		executionMu.Lock()
		defer executionMu.Unlock()
		// The intent is durable before any external effect is possible.
		intentID := id + ":" + call.NodeID + ":" + call.ID
		if old, err := c.Store.Get("toolcall", intentID); err == nil {
			a, _ := json.Marshal(old["arguments"])
			b, _ := json.Marshal(call.Arguments)
			if string(a) != string(b) || old["tool"] != call.Name {
				return engine.ToolResult{}, errors.New("Duplicate tool call ID has different arguments")
			}
			if old["status"] != "completed" {
				return engine.ToolResult{}, errors.New("This tool call has an uncertain or failed previous outcome; it cannot be replayed")
			}
			var cached tools.Result
			raw, _ := json.Marshal(old["result"])
			if e := json.Unmarshal(raw, &cached); e != nil {
				return engine.ToolResult{}, e
			}
			return engine.ToolResult{Content: cached.Content, Data: obj(cached.Data)}, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return engine.ToolResult{}, err
		}
		intent := Doc{"id": intentID, "callId": call.ID, "nodeId": call.NodeID, "runId": id, "tool": call.Name, "arguments": call.Arguments, "status": "started", "createdAt": store.Now()}
		if e := c.Store.Put("toolcall", intent); e != nil {
			return engine.ToolResult{}, e
		}
		selected := mgr
		if call.ReadOnly {
			selected = readOnlyMgr
		}
		result, e := selected.Execute(toolCtx, call.Name, call.Arguments)
		intent["finishedAt"] = store.Now()
		intent["status"] = "completed"
		if e != nil {
			intent["status"] = "failed"
			intent["error"] = e.Error()
		}
		intent["result"] = result
		if dbErr := c.Store.Put("toolcall", intent); dbErr != nil {
			cancel()
			return engine.ToolResult{}, dbErr
		}
		for _, a := range result.Artifacts {
			path := a.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(str(w, "path"), path)
			}
			mediaType := mime.TypeByExtension(filepath.Ext(path))
			if mediaType == "" {
				mediaType = "application/octet-stream"
			}
			size := int64(len(a.After))
			if stat, e := os.Stat(path); e == nil {
				size = stat.Size()
			}
			d := Doc{"id": store.ID(), "runId": id, "workspaceId": w["id"], "name": filepath.Base(path), "path": path, "mediaType": mediaType, "createdAt": store.Now(), "before": a.Before, "after": a.After, "size": size}
			if e := c.Store.Put("artifact", d); e != nil {
				return engine.ToolResult{}, e
			}
		}
		data := obj(result.Data)
		return engine.ToolResult{Content: result.Content, Data: data, IsError: e != nil}, e
	}
	eng := engine.New(c.Client())
	var result engine.Result
	if raw, ok := snapshot["workflow"]; ok {
		var def workflow.Definition
		b, _ := json.Marshal(raw)
		if e = json.Unmarshal(b, &def); e != nil {
			c.finishRun(run, result, e)
			return
		}
		resolver := func(id string) (engine.Request, error) {
			agents := []Doc{}
			b, _ := json.Marshal(snapshot["agents"])
			_ = json.Unmarshal(b, &agents)
			for _, a := range agents {
				if a["id"] == id {
					ar := req
					ar.Instructions = defaultInstructions + "\n" + str(a, "instructions")
					if str(a, "model") != "" {
						ar.Model = str(a, "model")
					}
					ar.MaxSteps = number(a, "maxSteps", req.MaxSteps)
					ar.ReadOnly = req.ReadOnly || a["mode"] == "read-only"
					if ar.ReadOnly {
						ar.Tools = []engine.ToolDefinition{}
						for _, d := range readOnlyMgr.List() {
							ar.Tools = append(ar.Tools, engine.ToolDefinition{Name: d.Name, Description: d.Description, Parameters: d.Parameters})
						}
					}
					return ar, nil
				}
			}
			return engine.Request{}, errors.New("Workflow agent is missing from the frozen run configuration")
		}
		runner := workflow.New(eng)
		for {
			// Once an approval is consumed, its old checkpoint is no longer a
			// safe restart boundary. Clear it durably before dispatching successors.
			if len(req.Resume) > 0 {
				snapshot["pending"] = []engine.PendingInput{}
				if e = c.Store.Put("checkpoint", snapshot); e != nil {
					break
				}
				if _, e = c.Store.Update("run", id, func(r Doc) error { r["resumable"] = false; return nil }); e != nil {
					break
				}
			}
			result, e = runner.Run(ctx, def, req, emit, executor, resolver)
			if e != nil || len(result.Pending) == 0 {
				break
			}
			snapshot["snapshot"] = result.Snapshot
			snapshot["pending"] = result.Pending
			if e = c.Store.Put("checkpoint", snapshot); e != nil {
				break
			}
			_, e = c.Store.Update("run", id, func(r Doc) error { r["resumable"] = true; return nil })
			if e != nil {
				break
			}
			req.Snapshot = result.Snapshot
			req.Resume = map[string]any{}
			for _, pending := range result.Pending {
				allow := c.approve(ctx, id, "workflow.approval", pending.Payload, pending.Message)
				if ctx.Err() != nil {
					e = ctx.Err()
					break
				}
				req.Resume[pending.ID] = allow == nil
			}
			if e != nil {
				break
			}
		}
	} else {
		result, e = eng.Run(ctx, req, emit, executor)
	}
	fatalMu.Lock()
	if fatal != nil {
		e = fatal
	}
	fatalMu.Unlock()
	if len(result.Snapshot) > 0 {
		snapshot["snapshot"] = result.Snapshot
	}
	snapshot["pending"] = result.Pending
	snapshot["result"] = result
	if dbErr := c.Store.Put("checkpoint", snapshot); dbErr != nil {
		e = errors.Join(e, dbErr)
	}
	c.finishRun(run, result, e)
}
func (c *Core) finishRun(run Doc, result engine.Result, err error) {
	id := str(run, "id")
	if strings.TrimSpace(result.Content) != "" {
		if e := c.Store.Put("message", Doc{"id": store.ID(), "sessionId": run["sessionId"], "runId": id, "role": "assistant", "content": result.Content, "createdAt": store.Now()}); e != nil {
			err = errors.Join(err, e)
		}
	}
	status := "completed"
	detail := "Run completed"
	if err != nil {
		status = "failed"
		detail = err.Error()
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
	}
	_, e := c.Store.Update("run", id, func(r Doc) error {
		r["status"] = status
		r["finishedAt"] = store.Now()
		r["summary"] = result.Content
		r["usage"] = result.Usage
		r["resumable"] = false
		if err != nil {
			r["error"] = err.Error()
		}
		return nil
	})
	if e == nil {
		_ = c.Emit(id, "run_completed", detail, "", Doc{"status": status, "steps": result.Steps, "usage": result.Usage})
	}
}
func (c *Core) saveWorkflow(p Doc) (any, error) {
	if _, e := required(p, "name"); e != nil {
		return nil, e
	}
	if str(p, "id") == "" {
		p["id"] = store.ID()
	}
	var def workflow.Definition
	b, _ := json.Marshal(p)
	if e := json.Unmarshal(b, &def); e != nil {
		return nil, e
	}
	if e := workflow.Validate(def); e != nil {
		return nil, e
	}
	return p, c.Store.Put("workflow", p)
}
func (c *Core) runTools(ctx context.Context, id string, w Doc, mode string) (*tools.Manager, error) {
	return tools.New(str(w, "path"), tools.Options{Mode: mode, Approve: func(ctx context.Context, a tools.Approval) error {
		return c.approve(ctx, id, a.Tool, a.Arguments, a.Reason)
	}, MCP: c.extension.mcp, ArtifactDir: filepath.Join(c.DataDir, "artifacts", id)})
}

func (c *Core) resumeRun(id string) (any, error) {
	run, e := c.Store.Get("run", id)
	if e != nil {
		return nil, e
	}
	if run["status"] != "interrupted" || run["resumable"] != true {
		return nil, errors.New("Only interrupted workflow approval checkpoints can be resumed")
	}
	cp, e := c.Store.Get("checkpoint", id)
	if e != nil {
		return nil, e
	}
	var pending []engine.PendingInput
	b, _ := json.Marshal(cp["pending"])
	if e = json.Unmarshal(b, &pending); e != nil || len(pending) == 0 {
		return nil, errors.New("No durable approval checkpoint exists")
	}
	var req engine.Request
	b, _ = json.Marshal(cp["request"])
	if e = json.Unmarshal(b, &req); e != nil {
		return nil, e
	}
	req.Snapshot, _ = json.Marshal(cp["snapshot"])
	req.Resume = map[string]any{}
	w := obj(cp["workspace"])
	c.mu.Lock()
	if c.workspaceRuns[str(w, "id")] != "" {
		c.mu.Unlock()
		return nil, errors.New("Workspace already has active work")
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.runs[id] = cancel
	c.workspaceRuns[str(w, "id")] = id
	c.mu.Unlock()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()
		defer func() { c.mu.Lock(); delete(c.runs, id); delete(c.workspaceRuns, str(w, "id")); c.mu.Unlock() }()
		for _, p := range pending {
			e := c.approve(ctx, id, "workflow.approval", p.Payload, p.Message)
			if ctx.Err() != nil {
				c.finishRun(run, engine.Result{}, ctx.Err())
				return
			}
			req.Resume[p.ID] = e == nil
		}
		c.executeRun(ctx, run, cp, req)
	}()
	return run, nil
}
