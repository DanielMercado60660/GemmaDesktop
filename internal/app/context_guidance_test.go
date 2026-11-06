package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/tools"
	"gemmadesktop.local/app/internal/workspace"
)

func TestFocusedContinuationUsesFreshRealRunAdmission(t *testing.T) {
	requests := make(chan inference.ChatRequest, 4)
	h := newIntegrationHarness(t, func(w http.ResponseWriter, _ *http.Request, req inference.ChatRequest) {
		requests <- req
		fmt.Fprintln(w, `{"message":{"content":"OLD_ASSISTANT_NARRATION"},"done":true,"prompt_eval_count":5,"eval_count":3}`)
	})
	first := callIntegration(t, h.core, "run.start", Doc{"sessionId": h.sessionID, "agentId": "default", "prompt": "Preserve EXACT_CONSTRAINT and do not replay effects."})
	finishedIntegration(t, h.core, str(first, "id"), "completed")
	<-requests
	second := callIntegration(t, h.core, "context.focus", Doc{"runId": str(first, "id"), "prompt": "Continue by inspecting the remaining issue."})
	finishedIntegration(t, h.core, str(second, "id"), "completed")
	dispatched := <-requests
	b, _ := json.Marshal(dispatched.Messages)
	if !strings.Contains(string(b), "EXACT_CONSTRAINT") || strings.Contains(string(b), "OLD_ASSISTANT_NARRATION") {
		t.Fatal("focused dispatch lost exact constraint or restored discarded assistant history", string(b))
	}
	checkpoint, err := h.core.Store.Get("checkpoint", str(second, "id"))
	if err != nil {
		t.Fatal(err)
	}
	var req engine.Request
	b, _ = json.Marshal(checkpoint["request"])
	if err = json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.History) != 0 || len(req.Snapshot) != 0 || str(obj(checkpoint["focusedContinuation"]), "sourceRunId") != str(first, "id") {
		t.Fatal("focus replayed execution history", checkpoint)
	}
	if _, err = h.core.Call(t.Context(), "context.focus", Doc{"runId": str(first, "id"), "prompt": "Stale source"}); err == nil {
		t.Fatal("stale focus admitted")
	}
}

func TestScopedGuidanceFollowsReadsAndBlocksFirstMutation(t *testing.T) {
	c := milestoneCore(t)
	root := t.TempDir()
	put := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("AGENTS.md", "root scope")
	put("src/AGENTS.md", "source scope")
	put("src/data.txt", "original")
	put("other/AGENTS.md", "unrelated scope")
	manifest, err := c.freezeContext(Doc{"path": root}, CapabilityProfile{ProjectGuidance: true}, []any{"src/data.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range manifest.Sources {
		if source.Path == "other/AGENTS.md" {
			t.Fatal("unrelated guidance loaded at admission")
		}
	}
	guard := c.newGuidanceGuard("run", root, true, []engine.ContextSource{manifest.Sources[0]}, workspace.DefaultPrivatePaths)
	m, err := tools.New(root, tools.Options{Mode: "auto", ArtifactDir: t.TempDir(), BeforeAccess: guard, Approve: func(context.Context, tools.Approval) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	read, err := m.Execute(t.Context(), "read_file", Doc{"path": "src/data.txt"})
	if err != nil || str(obj(read.Data), "guidanceReceiptId") == "" {
		t.Fatal("read omitted scoped guidance", read, err)
	}
	if !strings.Contains(read.Content, "original") {
		t.Fatal("guidance replaced read content")
	}
	put("src/AGENTS.md", "changed source scope")
	args := Doc{"path": "src/data.txt", "content": "updated", "expectedHash": workspace.Hash([]byte("original"))}
	blocked, err := m.Execute(t.Context(), "write_file", args)
	if err == nil || obj(blocked.Data)["mutationExecuted"] != false {
		t.Fatal("new guidance did not stop mutation", blocked, err)
	}
	current, _ := os.ReadFile(filepath.Join(root, "src/data.txt"))
	if string(current) != "original" {
		t.Fatal("blocked mutation changed source")
	}
	if _, err = m.Execute(t.Context(), "write_file", args); err != nil {
		t.Fatal("reviewed retry failed", err)
	}
	registry := &guidanceRegistry{}
	registry.add(guard)
	registry.observe(engine.Event{Type: "context_compacted"})
	args["expectedHash"] = workspace.Hash([]byte("updated"))
	args["content"] = "post compaction"
	if _, err = m.Execute(t.Context(), "write_file", args); err == nil {
		t.Fatal("compaction dropped guidance without requiring it to be surfaced again")
	}
	current, _ = os.ReadFile(filepath.Join(root, "src/data.txt"))
	if string(current) != "updated" {
		t.Fatal("post compaction mutation ran before guidance")
	}
	receipts, err := c.guidanceReceipts("run")
	if err != nil || len(receipts) != 2 {
		t.Fatal("immutable old and new guidance receipts missing", receipts, err)
	}
	b, _ := json.Marshal(receipts)
	if !strings.Contains(string(b), "source scope") || !strings.Contains(string(b), "changed source scope") || strings.Contains(string(b), "unrelated scope") {
		t.Fatal("receipt provenance incorrect", string(b))
	}
	if _, err = guard(t.Context(), "read_file", Doc{"path": "../outside"}); err == nil {
		t.Fatal("path escape admitted")
	}
	if _, err = guard(t.Context(), "read_file", Doc{"path": ".git/config"}); err == nil {
		t.Fatal("private scope admitted")
	}
}

func TestWritableCommandGuidanceFailsClosedAtHostScopeBound(t *testing.T) {
	c := milestoneCore(t)
	root := t.TempDir()
	for i := 0; i < 257; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("dir-%03d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	guard := c.newGuidanceGuard("run", root, true, nil, workspace.DefaultPrivatePaths)
	if _, err := guard(t.Context(), "run_command", Doc{"workingDirectory": "dir-000", "readOnly": false}); err == nil || !strings.Contains(err.Error(), "workspace-wide") {
		t.Fatal("cwd hid wider writable host authority", err)
	}
	if _, err := guard(t.Context(), "run_command", Doc{"workingDirectory": "dir-000", "readOnly": true}); err != nil {
		t.Fatal("read-only command unnecessarily scanned unrelated scopes", err)
	}
}

func TestFocusedContinuationPreservesConstraintsAndRejectsStaleSource(t *testing.T) {
	c := milestoneCore(t)
	agent := Doc{"id": "agent", "capabilityProfileId": "legacy"}
	frozen, err := c.FreezeAgent(agent)
	if err != nil {
		t.Fatal(err)
	}
	session := Doc{"id": "session", "workspaceId": "workspace"}
	run := Doc{"id": "source", "sessionId": "session", "workspaceId": "workspace", "agentId": "agent", "status": "completed", "createdAt": "2026-11-04T00:00:00Z"}
	contract := Doc{"command": []string{"go", "test", "./..."}}
	for kind, doc := range map[string]Doc{"run": run, "checkpoint": {"id": "source", "capabilities": map[string]FrozenAgent{frozen.Digest: frozen}, "request": engine.Request{CapabilityID: frozen.Digest, VerificationRequired: true}, "verificationContract": contract}, "message": {"id": "user", "sessionId": "session", "role": "user", "content": "Do not change the public API. Preserve Unicode: é.", "createdAt": "2026-11-04T00:00:00Z"}, "toolcall": {"id": "effect", "runId": "source", "callId": "write", "tool": "write_file", "status": "started", "arguments": Doc{"path": "code.go"}}} {
		if err = c.Store.Put(kind, doc); err != nil {
			t.Fatal(err)
		}
	}
	focus, err := c.prepareFocusedContinuation(Doc{"focusedFromRunId": "source"}, session, "agent", "")
	if err != nil {
		t.Fatal(err)
	}
	source, err := focusedContextSource(focus)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source.Content, "Do not change the public API. Preserve Unicode: é.") || !strings.Contains(source.Content, `"unresolvedTools":1`) || source.TrustClass != "historical_evidence" {
		t.Fatal("focus lost original constraints or uncertain receipt", source)
	}
	if contentDigest(obj(focus["verificationContract"])) != contentDigest(contract) {
		t.Fatal("contract changed")
	}
	if err = engine.ValidateContextSources([]engine.ContextSource{source}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.prepareFocusedContinuation(Doc{"focusedFromRunId": "source", "verificationContract": Doc{"command": []string{"true"}}}, session, "agent", ""); err == nil {
		t.Fatal("verification contract overridden")
	}
	if _, err = c.prepareFocusedContinuation(Doc{"focusedFromRunId": "source"}, session, "agent", "workflow"); err == nil {
		t.Fatal("workflow accepted")
	}
	if err = c.Store.Put("run", Doc{"id": "later", "sessionId": "session", "createdAt": "2026-11-04T00:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.prepareFocusedContinuation(Doc{"focusedFromRunId": "source"}, session, "agent", ""); err == nil {
		t.Fatal("older source omitted later constraints")
	}
	if focus, err = c.prepareFocusedContinuation(Doc{}, session, "agent", ""); err != nil || focus != nil {
		t.Fatal("implicit focus created")
	}
}
