package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"os"
	"os/exec"
	"path/filepath"
)

func harnessAdapterPath() (string, error) {
	candidates := []string{}
	if exe, e := os.Executable(); e == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "..", "Resources", "scripts", "harness", "upstream_adapter.py"))
	}
	if cwd, e := os.Getwd(); e == nil {
		for n := 0; n < 4; n++ {
			candidates = append(candidates, filepath.Join(cwd, "scripts", "harness", "upstream_adapter.py"))
			cwd = filepath.Dir(cwd)
		}
	}
	for _, p := range candidates {
		if s, e := os.Stat(p); e == nil && s.Mode().IsRegular() {
			return filepath.Abs(p)
		}
	}
	return "", errors.New("packaged upstream adapter is missing")
}
func (c *Core) runHarnessUpstream(ctx context.Context, m *harness.Manager, x harness.Experiment) error {
	if x.Spec.UpstreamAdapterHash != "" {
		digest, e := harnessAdapterIdentity()
		if e != nil {
			return e
		}
		if digest != x.Spec.UpstreamAdapterHash {
			return errors.New("upstream adapter changed after admission")
		}
	}
	script, e := harnessAdapterPath()
	if e != nil {
		return e
	}
	python, pythonID, e := harnessPythonIdentity()
	if e != nil {
		return e
	}
	if x.Spec.PythonIdentity != "" && pythonID != x.Spec.PythonIdentity {
		return errors.New("upstream Python interpreter changed after admission")
	}
	tasks, e := harness.LoadFixtures()
	if e != nil {
		return e
	}
	selected := []harness.Task{}
	for _, id := range x.Spec.TaskIDs {
		for _, t := range tasks {
			if t.ID == id {
				selected = append(selected, t)
			}
		}
	}
	baseline, e := m.Get("revision", x.Spec.BaseRevisionID)
	if e != nil {
		return e
	}
	root := filepath.Join(c.DataDir, "harness", "upstream", x.ID)
	if e = os.MkdirAll(root, 0700); e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, python, "-I", "-B", script)
	cmd.Dir = root
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "PYTHONDONTWRITEBYTECODE=1", "PYTHONNOUSERSITE=1", "LANG=en_US.UTF-8"}
	stderr := &execution.BoundedBuffer{Limit: 32768}
	cmd.Stderr = stderr
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return e
	}
	done := false
	defer func() {
		stdin.Close()
		if !done {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	writer := json.NewEncoder(stdin)
	revisions := []Doc{baseline}
	trialRecords := []Doc{}
	seen := map[string]bool{str(baseline, "id"): true}
	for _, id := range x.PacketIDs {
		p, e := m.Get("packet", id)
		if e != nil {
			return e
		}
		rid := str(p, "revisionId")
		if rid != "" && !seen[rid] {
			r, e := m.Get("revision", rid)
			if e != nil {
				return e
			}
			revisions = append(revisions, r)
			seen[rid] = true
		}
	}
	for _, id := range x.TrialIDs {
		t, e := m.Get("trial", id)
		if e != nil {
			return e
		}
		trialRecords = append(trialRecords, t)
	}
	if e = writer.Encode(Doc{"revisions": revisions, "trials": trialRecords, "upstreamRevision": harness.UpstreamRevision, "experimentId": x.ID, "baseline": baseline, "tasks": selected}); e != nil {
		return e
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 65536), 8<<20)
	for scanner.Scan() {
		var request struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Method  string `json:"method"`
			Params  Doc    `json:"params"`
		}
		if e = json.Unmarshal(scanner.Bytes(), &request); e != nil {
			return fmt.Errorf("invalid upstream protocol: %w", e)
		}
		if request.JSONRPC != "2.0" || request.ID < 1 {
			return errors.New("invalid upstream request identity")
		}
		p := request.Params
		var result any
		var callErr error
		switch request.Method {
		case "solve":
			result, callErr = m.UpstreamSolve(ctx, x.ID, str(p, "taskId"), str(p, "label"), str(p, "revisionId"))
		case "packet":
			var evidence []string
			var files map[string]string
			b, _ := json.Marshal(p["evidence"])
			_ = json.Unmarshal(b, &evidence)
			b, _ = json.Marshal(p["context"])
			_ = json.Unmarshal(b, &files)
			result, callErr = m.UpstreamPacket(ctx, x.ID, str(p, "taskId"), str(p, "stage"), str(p, "baseRevisionId"), str(p, "mission"), evidence, files)
		case "finish":
			callErr = m.FinishUpstream(x.ID, str(p, "candidateId"), number(p, "rollouts", 0), number(p, "stages", 0))
			result = Doc{"finished": callErr == nil}
		default:
			callErr = errors.New("operation outside narrow upstream protocol")
		}
		response := Doc{"jsonrpc": "2.0", "id": request.ID, "result": result}
		if callErr != nil {
			delete(response, "result")
			response["error"] = Doc{"code": -32000, "message": callErr.Error()}
		}
		if e = writer.Encode(response); e != nil {
			return e
		}
	}
	if e = scanner.Err(); e != nil {
		return e
	}
	e = cmd.Wait()
	done = true
	if e != nil {
		tail, _ := stderr.Snapshot()
		return fmt.Errorf("upstream adapter: %w: %s", e, tail)
	}
	return nil
}
