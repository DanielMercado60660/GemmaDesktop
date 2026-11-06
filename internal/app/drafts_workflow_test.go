package app

import (
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/store"
)

func editorTestCore(t *testing.T) *Core {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "editors.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	c := &Core{Store: s, workspaceRuns: map[string]string{}}
	for kind, d := range map[string]Doc{"workspace": {"id": "w"}, "session": {"id": "s", "workspaceId": "w"}} {
		if err = s.Put(kind, d); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestDraftRevisionsProtectNewTypingAndSentTombstones(t *testing.T) {
	c := editorTestCore(t)
	save := func(text string, revision int) Doc {
		v, err := c.draftCall("draft.save", Doc{"workspaceId": "w", "sessionId": "s", "text": text, "attachments": []string{"main.py"}, "expectedRevision": revision})
		if err != nil {
			t.Fatal(err)
		}
		return obj(v)
	}
	save("submitted", 0)
	save("newer typing", 1)
	d, cleared, err := c.clearSubmittedDraft("w", "s", 1)
	if err != nil || cleared || str(d, "text") != "newer typing" {
		t.Fatalf("new typing lost: %v %v %v", d, cleared, err)
	}
	d, cleared, err = c.clearSubmittedDraft("w", "s", 2)
	if err != nil || !cleared || str(d, "text") != "" || number(d, "revision", 0) != 3 {
		t.Fatalf("clear: %v %v %v", d, cleared, err)
	}
	if _, err = c.draftCall("draft.save", Doc{"workspaceId": "w", "sessionId": "s", "text": "late stale save", "expectedRevision": 2}); err == nil {
		t.Fatal("stale save resurrected submitted draft")
	}
	save("next draft", 3)
	if _, err = c.deleteSession("s"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.draftCall("draft.save", Doc{"workspaceId": "w", "sessionId": "s", "text": "deleted session", "expectedRevision": 0}); err == nil {
		t.Fatal("deleted session accepted draft")
	}
	rows, err := c.Store.List("draft")
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted session drafts retained: %v %v", rows, err)
	}
}

func TestDraftOwnershipAndNewChatPersistence(t *testing.T) {
	c := editorTestCore(t)
	if err := c.Store.Put("workspace", Doc{"id": "other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.draftCall("draft.save", Doc{"workspaceId": "other", "sessionId": "s", "expectedRevision": 0}); err == nil {
		t.Fatal("cross-workspace draft admitted")
	}
	_, err := c.draftCall("draft.save", Doc{"workspaceId": "w", "sessionId": "", "text": "unsent", "expectedRevision": 0})
	if err != nil {
		t.Fatal(err)
	}
	// A fresh Core with the same private store restores the acknowledged record.
	reopened := &Core{Store: c.Store}
	v, err := reopened.draftCall("draft.get", Doc{"workspaceId": "w", "sessionId": ""})
	if err != nil || str(obj(v), "text") != "unsent" {
		t.Fatalf("restore: %v %v", v, err)
	}
}

func TestWorkflowSaveRejectsConcurrentEditsAndDeletedRevision(t *testing.T) {
	c := editorTestCore(t)
	p := Doc{"name": "Repair", "nodes": []Doc{{"id": "solve", "type": "agent", "agentId": "default"}}, "edges": []Doc{}}
	v, err := c.saveWorkflow(p)
	if err != nil {
		t.Fatal(err)
	}
	first := obj(v)
	if str(first, "revision") == "" {
		t.Fatal("missing workflow revision")
	}
	edit := clone(first)
	edit["name"] = "Reviewed repair"
	edit["expectedRevision"] = first["revision"]
	v, err = c.saveWorkflow(edit)
	if err != nil {
		t.Fatal(err)
	}
	second := obj(v)
	if str(first, "revision") == str(second, "revision") {
		t.Fatal("edit did not change revision")
	}
	if _, err = c.saveWorkflow(edit); err == nil {
		t.Fatal("stale editor overwrote newer workflow")
	}
	if err = c.Store.Delete("workflow", str(first, "id")); err != nil {
		t.Fatal(err)
	}
	edit["expectedRevision"] = second["revision"]
	if _, err = c.saveWorkflow(edit); err == nil {
		t.Fatal("deleted workflow silently resurrected")
	}
}

func TestDefaultInstructionsAreAppliedOnceWithoutChangingCustomText(t *testing.T) {
	for _, text := range []string{"", defaultInstructions} {
		if got := agentInstructions(Doc{"instructions": text}); got != defaultInstructions {
			t.Fatal("default instructions duplicated")
		}
	}
	custom := "Preserve exact project constraint."
	if got := agentInstructions(Doc{"instructions": custom}); !strings.HasSuffix(got, "\n\n"+custom) || strings.Count(got, defaultInstructions) != 1 {
		t.Fatal(got)
	}
}
