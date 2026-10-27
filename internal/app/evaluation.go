package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/tools"
	"gemmadesktop.local/app/internal/workspace"
)

// Evaluation freezes bounded text inputs and both configurations. It deliberately
// grants no write, shell, MCP or browser capability to either candidate.
func (c *Core) startEval(p Doc) (any, error) {
	w, e := c.Store.Get("workspace", str(p, "workspaceId"))
	if e != nil {
		return nil, e
	}
	baseline, e := c.Store.Get("agent", str(p, "baselineAgentId"))
	if e != nil {
		return nil, e
	}
	candidate, e := c.Store.Get("agent", str(p, "candidateAgentId"))
	if e != nil {
		return nil, e
	}
	if baseline["id"] == candidate["id"] {
		return nil, errors.New("Choose two different agent configurations")
	}
	var tasks []string
	b, _ := json.Marshal(p["tasks"])
	_ = json.Unmarshal(b, &tasks)
	if len(tasks) == 0 {
		return nil, errors.New("Provide repeatable evaluation prompts")
	}
	if len(tasks) > 12 {
		return nil, errors.New("At most 12 tasks per comparison")
	}
	for _, t := range tasks {
		if t == "" || len(t) > 10000 {
			return nil, errors.New("Each task must contain 1–10000 characters")
		}
	}
	id := store.ID()
	root := filepath.Join(c.DataDir, "evaluations", id, "input")
	digest, count, e := freezeWorkspace(str(w, "path"), root)
	if e != nil {
		return nil, e
	}
	experiment := Doc{"id": id, "workspaceId": w["id"], "baselineAgentId": baseline["id"], "candidateAgentId": candidate["id"], "baseline": baseline, "candidate": candidate, "tasks": tasks, "status": "running", "createdAt": store.Now(), "inputDigest": digest, "inputFiles": count, "inputPath": root, "results": []Doc{}, "qualification": "Read-only frozen-text task comparison. Execution success is not a correctness grade; compare the recorded answers and evidence."}
	if e = c.Store.Put("experiment", experiment); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(c.ctx, 45*time.Minute)
	c.mu.Lock()
	c.evals[id] = cancel
	c.mu.Unlock()
	settings := c.Settings()
	client := c.Client()
	response := clone(experiment)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()
		defer func() { c.mu.Lock(); delete(c.evals, id); c.mu.Unlock() }()
		results := []Doc{}
		final := "completed"
		for _, task := range tasks {
			for _, config := range []Doc{baseline, candidate} {
				if ctx.Err() != nil {
					final = "cancelled"
					break
				}
				started := time.Now()
				model := str(config, "model")
				if model == "" {
					model = str(settings, "defaultModel")
				}
				req := engine.Request{Model: model, Instructions: defaultInstructions + "\n" + str(config, "instructions"), Prompt: task, MaxSteps: number(config, "maxSteps", 24), ContextSize: number(settings, "contextSize", 32768), Temperature: 0.2}
				mgr, err := tools.New(root, tools.Options{Mode: "read-only"})
				calls := 0
				trace := []Doc{}
				var traceMu sync.Mutex
				var result engine.Result
				if err == nil {
					for _, d := range mgr.List() {
						req.Tools = append(req.Tools, engine.ToolDefinition{Name: d.Name, Description: d.Description, Parameters: d.Parameters})
					}
					result, err = engine.New(client).Run(ctx, req, nil, func(ctx context.Context, call engine.ToolCall) (engine.ToolResult, error) {
						traceMu.Lock()
						calls++
						traceMu.Unlock()
						r, e := mgr.Execute(ctx, call.Name, call.Arguments)
						entry := Doc{"tool": call.Name, "arguments": call.Arguments, "content": r.Content, "data": r.Data}
						if e != nil {
							entry["error"] = e.Error()
						}
						if len(r.Content) > 32768 {
							entry["content"] = r.Content[:32768]
							entry["data"] = nil
						}
						traceMu.Lock()
						trace = append(trace, entry)
						traceMu.Unlock()
						return engine.ToolResult{Content: r.Content, Data: obj(r.Data), IsError: e != nil}, e
					})
					mgr.Close()
				}
				r := Doc{"agentId": config["id"], "agentName": config["name"], "task": task, "status": "completed", "content": result.Content, "durationMs": time.Since(started).Milliseconds(), "usage": result.Usage, "toolCalls": calls}
				r["toolTrace"] = trace
				if err != nil {
					r["status"] = "failed"
					r["error"] = err.Error()
				}
				results = append(results, r)
				experiment["results"] = results
				_ = c.Store.Put("experiment", experiment)
			}
			if ctx.Err() != nil {
				final = "cancelled"
				break
			}
		}
		experiment["status"] = final
		experiment["finishedAt"] = store.Now()
		_ = c.Store.Put("experiment", experiment)
	}()
	return response, nil
}
func (c *Core) cancelEval(id string) (any, error) {
	c.mu.Lock()
	f := c.evals[id]
	c.mu.Unlock()
	if f == nil {
		return nil, errors.New("Evaluation is no longer running")
	}
	f()
	return Doc{}, nil
}
func freezeWorkspace(source, destination string) (string, int, error) {
	w, e := workspace.Open(source)
	if e != nil {
		return "", 0, e
	}
	defer w.Close()
	if e = os.MkdirAll(destination, 0700); e != nil {
		return "", 0, e
	}
	digest := sha256.New()
	count, total := 0, 0
	var walk func(string) error
	walk = func(path string) error {
		entries, e := w.List(path)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if entry.Symlink {
				continue
			}
			if entry.IsDir {
				switch entry.Name {
				case "node_modules", ".cache", ".venv", "vendor", "build", "dist", "artifacts":
					continue
				}
				if e = walk(entry.Path); e != nil {
					return e
				}
				continue
			}
			if entry.Size > 128<<10 {
				continue
			}
			f, e := w.Read(entry.Path)
			if e != nil {
				continue
			}
			count++
			total += len(f.Content)
			if count > 500 || total > 20<<20 {
				return errors.New("Evaluation input exceeds 500 text files or 20 MiB; choose a narrower workspace")
			}
			path := filepath.Join(destination, entry.Path)
			if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				return e
			}
			if e = os.WriteFile(path, []byte(f.Content), 0600); e != nil {
				return e
			}
			digest.Write([]byte(entry.Path))
			digest.Write([]byte{0})
			digest.Write([]byte(f.Hash))
		}
		return nil
	}
	if e = walk("."); e != nil {
		return "", count, e
	}
	return hex.EncodeToString(digest.Sum(nil)), count, nil
}
