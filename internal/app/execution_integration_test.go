package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/inference"
)

func TestGeneralSettingsKeepNewerToolchainConfiguration(t *testing.T) {
	c := milestoneCore(t)
	client, err := inference.NewClient("http://127.0.0.1:11434")
	if err != nil {
		t.Fatal(err)
	}
	c.client = client
	validated := Doc{"id": "default", "ollamaUrl": client.BaseURL(), "defaultModel": "fixture", "contextSize": 4096, "maxSteps": 4, "mode": "approval", "theme": "light", "dockerImage": "old-image"}
	if err = c.Store.Put("settings", validated); err != nil {
		t.Fatal(err)
	}
	// Model the exact interleaving: general settings captured/validated their
	// input, then execution.configure published a newer unrelated selection.
	if _, err = c.Store.Update("settings", "default", func(d Doc) error {
		d["dockerImage"] = "new-image"
		d["unrelatedSetting"] = "retained"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := c.saveValidatedSettings(validated, client)
	if err != nil || obj(saved)["dockerImage"] != "new-image" || obj(saved)["unrelatedSetting"] != "retained" || obj(saved)["theme"] != "light" {
		t.Fatal("general save replaced a concurrent toolchain selection", saved, err)
	}
	c.runs = map[string]context.CancelFunc{"active": func() {}}
	validated["theme"] = "dark"
	if _, err = c.saveValidatedSettings(validated, client); err == nil {
		t.Fatal("active-run admission guard was bypassed")
	}
	if current := c.Settings(); current["theme"] != "light" || current["dockerImage"] != "new-image" {
		t.Fatal("rejected save changed settings", current)
	}
}

func TestExecutionStatusShowsNewRecoveryRequirement(t *testing.T) {
	c := milestoneCore(t)
	if err := c.Store.Put("settings", Doc{"id": "default", "dockerImage": "sha256:" + strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Store.Put("execution_state", Doc{"id": "recovery", "status": "ready"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(c.DataDir, "execution", "commands", "cmd-owned-command")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lease.json"), []byte(`{"status":"creating"}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Cancel the runtime inspection before dispatch: this test does not contact
	// Docker. Local recovery metadata must still supersede stale startup state.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	value, err := c.executionCall(ctx, "execution.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	recovery := obj(obj(value)["recovery"])
	if recovery["status"] != "blocked" || !strings.Contains(str(recovery, "error"), "reconciliation") {
		t.Fatal("pending recovery hidden behind earlier ready state", value)
	}
	stored, _ := c.Store.Get("execution_state", "recovery")
	if stored["status"] != "ready" {
		t.Fatal("read-only inspection mutated recovery record")
	}
}

func TestDockerRecoveryReceiptRetainsUncertaintyAndProvenance(t *testing.T) {
	c := milestoneCore(t)
	data := Doc{"exitCode": 0, "publication": "recovery_required", "commandId": "owned-command", "image": "sha256:" + strings.Repeat("b", 64), "snapshotDigest": "input-hash", "dependencyDigest": "dependency-hash", "workspaceChanged": true}
	if err := c.Store.Put("toolcall", Doc{"id": "command", "runId": "run", "callId": "call", "status": "failed", "tool": "run_command", "arguments": Doc{"executable": "python3"}, "result": Doc{"data": data}, "error": "source publication needs recovery"}); err != nil {
		t.Fatal(err)
	}
	receipt, err := c.runReceipt("run")
	if err != nil || number(receipt, "unresolvedTools", 0) != 1 {
		t.Fatal("Docker uncertainty was treated as settled failure", receipt, err)
	}
	text, err := c.recoveryContext(Doc{"id": "run", "status": "failed"})
	if err != nil || len(text) > 16<<10 {
		t.Fatal("invalid recovery context", len(text), err)
	}
	var body Doc
	if err = json.Unmarshal([]byte(text[strings.Index(text, "{"):]), &body); err != nil {
		t.Fatal(err)
	}
	actions := body["observedActions"].([]any)
	if len(actions) != 1 || number(body, "unresolvedTools", 0) != 1 {
		t.Fatal("uncertain operation omitted", body)
	}
	for _, key := range []string{"publication", "commandId", "image", "snapshotDigest", "dependencyDigest", "workspaceChanged"} {
		if obj(actions[0])[key] != data[key] {
			t.Fatalf("recovery lost %s: %v", key, actions[0])
		}
	}
}
