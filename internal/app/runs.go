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
	var promotedWorkflow Doc
	if workflowID == "" {
		promotedWorkflow, e = c.harnessSelectedWorkflow(str(w, "id"), agentID)
		if e != nil {
			return nil, e
		}
		if promotedWorkflow != nil {
			workflowID = str(promotedWorkflow, "id")
		}
	}
	model := str(p, "model")
	if model == "" {
		model = str(agent, "model")
	}
	if model == "" {
		model = str(settings, "defaultModel")
	}
	if e = c.checkCompletionSelection(model); e != nil {
		return nil, e
	}
	req := engine.Request{Model: model, Instructions: defaultInstructions + "\n\n" + str(agent, "instructions"), Prompt: prompt, MaxSteps: number(agent, "maxSteps", number(settings, "maxSteps", 24)), ContextSize: number(settings, "contextSize", 32768), Timeout: 20 * time.Minute}
	controls, e := agentGenerationControls(agent)
	if e != nil {
		return nil, e
	}
	controls.apply(&req)
	req.ContextPolicy = &engine.ContextPolicy{Enabled: true}
	msgs, e := c.Store.List("message")
	if e != nil {
		return nil, e
	}
	previous, e := c.Store.List("run")
	if e != nil {
		return nil, e
	}
	incompleteRuns := map[string]bool{}
	for _, r := range previous {
		if r["sessionId"] != session["id"] {
			continue
		}
		id := str(r, "id")
		incompleteRuns[id] = r["incomplete"] == true
		if _, known := r["incomplete"]; !known && r["status"] != "completed" {
			// Older binaries recorded this flag only in the checkpoint result.
			// Read it without rewriting the original message or run evidence.
			cp, err := c.Store.Get("checkpoint", id)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			if err == nil {
				incompleteRuns[id] = obj(cp["result"])["incomplete"] == true
			}
		}
	}
	for _, m := range msgs {
		// Partial replies remain visible in the audit/UI, but are not complete
		// assistant turns and must not become normal conversation history when
		// a failed run falls back from its snapshot to persisted messages.
		if m["sessionId"] == session["id"] && !(m["role"] == "assistant" && (m["incomplete"] == true || incompleteRuns[str(m, "runId")])) {
			req.History = append(req.History, engine.Message{Role: str(m, "role"), Content: str(m, "content")})
		}
	}
	if workflowID == "" {
		for i := len(previous) - 1; i >= 0; i-- {
			r := previous[i]
			if r["sessionId"] == session["id"] {
				cp, err := c.Store.Get("checkpoint", str(r, "id"))
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return nil, err
				}
				if err == nil {
					req.Snapshot = conversationSnapshot(r, cp)
				}
				if r["status"] != "completed" {
					recovery, err := c.recoveryContext(r)
					if err != nil {
						return nil, err
					}
					req.Instructions += recovery
				}
				break
			}
		}
	}
	mode := str(agent, "mode")
	if settings["mode"] == "read-only" {
		mode = "read-only"
	}
	req.ReadOnly = mode == "read-only"
	req.Deadline = time.Now().Add(20 * time.Minute)
	snapshot := Doc{"id": "", "workspace": w, "mode": mode, "agents": []Doc{}}
	var frozenWorkflow Doc

	if workflowID != "" {
		wf := promotedWorkflow
		if wf == nil {
			wf, e = c.Store.Get("workflow", workflowID)
			if e != nil {
				return nil, e
			}
		}
		snapshot["workflow"] = wf
		frozenWorkflow = wf
		all, e := c.Store.List("agent")
		if e != nil {
			return nil, e
		}
		snapshot["agents"] = all
	}
	frozen, e := c.freezeRunAgents(agent, frozenWorkflow, str(w, "id"))
	if e != nil {
		return nil, e
	}
	var primary FrozenAgent
	for _, f := range frozen {
		if str(f.Agent, "id") == agentID {
			primary = f
			break
		}
	}
	if primary.Digest == "" {
		return nil, errors.New("primary capability is unavailable")
	}
	primary.Configure(&req)
	manifest, e := c.freezeContext(w, primary.Profile, p["attachments"])
	if e != nil {
		return nil, e
	}
	req.ContextSources = manifest.Sources
	snapshot["capabilities"] = frozen
	snapshot["contextManifest"] = frozenContextAudit(manifest, req, primary)
	snapshot["request"] = req
	id := store.ID()
	snapshot["id"] = id
	c.mu.Lock()
	if c.harnessBusy() {
		c.mu.Unlock()
		return nil, errors.New("Wait for active Harness work before starting a chat")
	}
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

// A new turn restores observations from an ordinary run, including a failed
// one, without resuming its execution or approval authority. Engine snapshots
// exclude incomplete terminal generations and preserve completed tool pairs
// and summaries. A workflow checkpoint must only enter the explicit resume path.
func conversationSnapshot(run, checkpoint Doc) json.RawMessage {
	if str(run, "workflowId") != "" || checkpoint["workflow"] != nil {
		return nil
	}
	switch str(run, "status") {
	case "completed", "failed", "cancelled", "interrupted":
	default:
		return nil
	}
	if pending, ok := checkpoint["pending"]; ok {
		encoded, err := json.Marshal(pending)
		var inputs []engine.PendingInput
		if err != nil || json.Unmarshal(encoded, &inputs) != nil || len(inputs) != 0 {
			return nil
		}
	}
	encoded, err := json.Marshal(checkpoint["snapshot"])
	if err != nil || len(encoded) > 64<<20 {
		return nil
	}
	var snapshot struct {
		Version int               `json:"version"`
		Events  []json.RawMessage `json:"events"`
		State   map[string]any    `json:"state"`
	}
	if json.Unmarshal(encoded, &snapshot) != nil || snapshot.Version != 1 || len(snapshot.Events) == 0 {
		return nil
	}
	return encoded
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
	deadline := req.Deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(20 * time.Minute)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
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
	frozen, e := decodeFrozenAgents(snapshot)
	if e != nil {
		c.finishRun(run, engine.Result{}, e)
		return
	}
	primary, hasFrozen := frozen[req.CapabilityID]
	if _, present := snapshot["capabilities"]; present && !hasFrozen {
		c.finishRun(run, engine.Result{}, errors.New("checkpoint has no matching frozen primary capability"))
		return
	}
	// Legacy checkpoints retain their original restrictive tool mode. New runs
	// always carry a complete, content-verified capability manifest.
	profile, image := "restricted", ""
	if hasFrozen {
		profile = primary.Profile.ExecutionProfile
		image = primary.ExecutionImage
	}
	config := runToolConfig{Profile: profile, Image: image}
	if hasFrozen {
		config.PrivatePaths = primary.ExecutionPrivatePaths
	}
	mgr, e := c.runTools(ctx, id, w, str(snapshot, "mode"), config)
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
	if hasFrozen {
		req.Tools = primary.FilterTools(req.Tools)
		for _, d := range []engine.ToolDefinition{planTool(), delegateTool(), skillResourceTool(), skillMaterializeTool()} {
			if contains(primary.Profile.Tools, d.Name) && !(req.ReadOnly && d.Name == "skill_materialize") {
				req.Tools = append(req.Tools, d)
			}
		}
	}
	snapshot["contextToolDeclarations"] = req.Tools
	if e = c.Store.Put("checkpoint", snapshot); e != nil {
		c.finishRun(run, engine.Result{}, e)
		return
	}
	managers := map[string]*tools.Manager{}
	if hasFrozen {
		managers[primary.Digest] = mgr
	}
	defer func() {
		for _, m := range managers {
			if m != mgr {
				m.Close()
			}
		}
	}()
	var executionMu sync.Mutex
	var executor engine.ToolExecutor
	executor = func(toolCtx context.Context, call engine.ToolCall) (engine.ToolResult, error) {
		selectedProfile, scoped := frozen[call.CapabilityID]
		if strings.HasPrefix(call.NodeID, "delegate_") && !readonlyTool(call.Name) {
			return engine.ToolResult{}, errors.New("delegates have read-only tool authority")
		}
		if hasFrozen {
			if !scoped {
				return engine.ToolResult{}, errors.New("unknown frozen capability identity")
			}
			var authErr error
			toolCtx, authErr = selectedProfile.Authorize(toolCtx, call)
			if authErr != nil {
				return engine.ToolResult{}, authErr
			}
			if call.CapabilityID != primary.Digest {
				parentCall := call
				parentCall.CapabilityID = primary.Digest
				if _, authErr = primary.Authorize(toolCtx, parentCall); authErr != nil {
					return engine.ToolResult{}, authErr
				}
			}

			if call.Name == "delegate_readonly" {
				return c.delegateReadonly(toolCtx, id, call, selectedProfile, frozen, req, emit, executor)
			}
			if call.Name == "plan_update" {
				d, err := c.updateTaskPlan(id, call.Arguments)
				return engine.ToolResult{Data: d, IsError: err != nil}, err
			}
			if call.Name == "skill_resources" {
				return engine.ToolResult{Data: skillResources(selectedProfile)}, nil
			}
			if call.Name == "mcp_list" {
				visible := []any{}
				for _, t := range c.extension.mcp.ToolsForWorkspace(str(w, "path")) {
					for _, g := range selectedProfile.MCPGrants {
						if t.ServerID == g.ServerID && t.Name == g.Tool && t.SchemaHash == g.SchemaHash {
							visible = append(visible, t)
						}
					}
				}
				return engine.ToolResult{Data: Doc{"tools": visible, "unavailable": selectedProfile.UnavailableMCP}}, nil
			}
		}
		executionMu.Lock()
		defer executionMu.Unlock()
		selected := mgr
		if call.ReadOnly {
			selected = readOnlyMgr
		} else if scoped {
			selected = managers[selectedProfile.Digest]
			if selected == nil {
				var err error
				selected, err = c.runTools(toolCtx, id, w, str(snapshot, "mode"), runToolConfig{Profile: selectedProfile.Profile.ExecutionProfile, Image: selectedProfile.ExecutionImage, PrivatePaths: selectedProfile.ExecutionPrivatePaths})
				if err != nil {
					return engine.ToolResult{}, err
				}
				managers[selectedProfile.Digest] = selected
			}
		}
		// The intent is durable before any external effect is possible.
		intentID := id + ":" + call.NodeID + ":" + call.ID
		if old, err := c.Store.Get("toolcall", intentID); err == nil {
			a, _ := json.Marshal(old["arguments"])
			b, _ := json.Marshal(call.Arguments)
			if string(a) != string(b) || old["tool"] != call.Name {
				return engine.ToolResult{}, errors.New("Duplicate tool call ID has different arguments")
			}
			var cached tools.Result
			raw, _ := json.Marshal(old["result"])
			if e := json.Unmarshal(raw, &cached); e != nil {
				return engine.ToolResult{}, e
			}
			if old["status"] != "completed" {
				err := errors.New("This tool call has an uncertain or failed previous outcome; it cannot be replayed")
				if prior := str(old, "error"); prior != "" {
					err = fmt.Errorf("%w: %s", err, prior)
				}
				return toolResultWithReceipt(cached, intentID, err), err
			}
			return toolResultWithReceipt(cached, intentID, nil), nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return engine.ToolResult{}, err
		}
		intent := Doc{"id": intentID, "callId": call.ID, "nodeId": call.NodeID, "runId": id, "tool": call.Name, "arguments": call.Arguments, "status": "started", "createdAt": store.Now()}
		if e := c.Store.Put("toolcall", intent); e != nil {
			return engine.ToolResult{}, e
		}
		var result tools.Result
		var e error
		if call.Name == "skill_materialize" {
			result, e = c.materializeSkill(toolCtx, id, call, selectedProfile, w)
		} else {
			result, e = selected.Execute(toolCtx, call.Name, call.Arguments)
		}
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
				return toolResultWithReceipt(result, intentID, e), e
			}
		}
		return toolResultWithReceipt(result, intentID, e), e
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
					controls, err := agentGenerationControls(a)
					if err != nil {
						return engine.Request{}, err
					}
					controls.apply(&ar)
					ar.Instructions = defaultInstructions + "\n\n" + str(a, "instructions")
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
					if hasFrozen {
						found := false
						for _, f := range frozen {
							if str(f.Agent, "id") == id {
								f.Configure(&ar)
								ar.Tools = f.FilterTools(ar.Tools)
								// Workflow profiles cannot expand the parent run's
								// tool authority, including read-only catalogue paths.
								ar.Tools = primary.FilterTools(ar.Tools)
								for _, d := range []engine.ToolDefinition{planTool(), delegateTool(), skillResourceTool(), skillMaterializeTool()} {
									if contains(f.Profile.Tools, d.Name) && contains(primary.Profile.Tools, d.Name) && !(ar.ReadOnly && d.Name == "skill_materialize") {
										exists := false
										for _, t := range ar.Tools {
											if t.Name == d.Name {
												exists = true
											}
										}
										if !exists {
											ar.Tools = append(ar.Tools, d)
										}
									}
								}
								found = true
								break
							}
						}
						if !found {
							return engine.Request{}, errors.New("workflow capability is missing from frozen manifest")
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

// Add a host-owned journal identity to a copy, preserving the durable tool
// payload and any distinct prose. Identical JSON Content is redundant once its
// complete structured data is present; do not reintroduce escaped file output.
func toolResultWithReceipt(result tools.Result, receiptID string, err error) engine.ToolResult {
	data := obj(result.Data)
	if data == nil {
		data = Doc{}
	}
	content := result.Content
	if content != "" {
		var decoded any
		if json.Unmarshal([]byte(content), &decoded) == nil {
			a, _ := json.Marshal(decoded)
			b, _ := json.Marshal(data)
			if string(a) == string(b) {
				content = ""
			}
		}
	}
	data["receiptId"] = receiptID
	return engine.ToolResult{Content: content, Data: data, IsError: err != nil}
}
func (c *Core) finishRun(run Doc, result engine.Result, err error) {
	id := str(run, "id")
	if result.Incomplete && err == nil {
		err = engine.ErrIncompleteResponse
	}
	receipt, receiptErr := c.runReceipt(id)
	if receiptErr != nil {
		err = errors.Join(err, receiptErr)
	} else if number(receipt, "unresolvedTools", 0) > 0 {
		err = errors.Join(err, errors.New("One or more tool outcomes remain uncertain; inspect the observed actions before continuing"))
	}
	if strings.TrimSpace(result.Content) != "" {
		if e := c.Store.Put("message", Doc{"id": store.ID(), "sessionId": run["sessionId"], "runId": id, "role": "assistant", "content": result.Content, "incomplete": result.Incomplete, "createdAt": store.Now()}); e != nil {
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
		r["incomplete"] = result.Incomplete
		r["usage"] = result.Usage
		r["receipt"] = receipt
		r["resumable"] = false
		if err != nil {
			r["error"] = err.Error()
		}
		return nil
	})
	if e == nil {
		_ = c.Emit(id, "run_completed", detail, "", Doc{"status": status, "steps": result.Steps, "usage": result.Usage, "incomplete": result.Incomplete})
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

type runToolConfig struct {
	Profile, Image string
	PrivatePaths   []string
}

func (c *Core) runTools(ctx context.Context, id string, w Doc, mode string, configs ...runToolConfig) (*tools.Manager, error) {
	config := runToolConfig{Profile: "restricted"}
	if len(configs) > 0 {
		config = configs[0]
	}
	executor, e := c.executorForConfig(config.Profile, config.Image, config.PrivatePaths)
	if e != nil {
		return nil, e
	}
	return tools.New(str(w, "path"), tools.Options{Executor: executor, Mode: mode, Approve: func(ctx context.Context, a tools.Approval) error {
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
	if !req.Deadline.IsZero() && !time.Now().Before(req.Deadline) {
		return nil, errors.New("workflow checkpoint deadline has expired; start a new run")
	}
	w := obj(cp["workspace"])
	c.mu.Lock()
	if c.harnessBusy() {
		c.mu.Unlock()
		return nil, errors.New("Harness work is active")
	}
	if c.workspaceRuns[str(w, "id")] != "" {
		c.mu.Unlock()
		return nil, errors.New("Workspace already has active work")
	}
	ctx, cancel := context.WithCancel(c.ctx)
	if !req.Deadline.IsZero() {
		cancel()
		ctx, cancel = context.WithDeadline(c.ctx, req.Deadline)
	}
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
