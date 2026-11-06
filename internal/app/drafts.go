package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gemmadesktop.local/app/internal/store"
)

func agentInstructions(agent Doc) string {
	custom := str(agent, "instructions")
	if strings.TrimSpace(custom) == "" || custom == defaultInstructions || slices.Contains(legacyDefaultInstructions, custom) {
		return defaultInstructions
	}
	return defaultInstructions + "\n\n" + custom
}

func draftID(workspaceID, sessionID string) string {
	return contentDigest([]string{workspaceID, sessionID})
}
func emptyDraft(workspaceID, sessionID string) Doc {
	return Doc{"id": draftID(workspaceID, sessionID), "schemaVersion": 1, "workspaceId": workspaceID, "sessionId": sessionID, "text": "", "attachments": []string{}, "focusedRunId": "", "revision": 0}
}

func (c *Core) draftCall(method string, p Doc) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	wid, sid := str(p, "workspaceId"), str(p, "sessionId")
	if _, err := c.Store.Get("workspace", wid); err != nil {
		return nil, err
	}
	if sid != "" {
		s, err := c.Store.Get("session", sid)
		if err != nil {
			return nil, err
		}
		if str(s, "workspaceId") != wid {
			return nil, errors.New("draft session belongs to another workspace")
		}
	}
	id := draftID(wid, sid)
	if method == "draft.get" {
		d, err := c.Store.Get("draft", id)
		if errors.Is(err, store.ErrNotFound) {
			return emptyDraft(wid, sid), nil
		}
		return d, err
	}
	if method != "draft.save" && method != "draft.delete" {
		return nil, errors.New("unknown draft operation")
	}
	if _, ok := p["expectedRevision"]; !ok {
		return nil, errors.New("expectedRevision is required")
	}
	text := str(p, "text")
	focusedRunID := str(p, "focusedRunId")
	if value, ok := p["focusedRunId"]; ok {
		if _, valid := value.(string); !valid || len(focusedRunID) > 128 || strings.ContainsRune(focusedRunID, 0) {
			return nil, errors.New("invalid focused run reference")
		}
	}
	if len(text) > 100000 {
		return nil, errors.New("draft text exceeds 100000 bytes")
	}
	attachments := []string{}
	if method == "draft.save" && p["attachments"] != nil {
		var values []any
		switch a := p["attachments"].(type) {
		case []any:
			values = a
		case []string:
			for _, v := range a {
				values = append(values, v)
			}
		default:
			return nil, errors.New("draft attachments must be path references")
		}
		if len(values) > 16 {
			return nil, errors.New("at most 16 draft attachments")
		}
		for _, v := range values {
			path, ok := v.(string)
			if !ok || len(path) > 4096 || strings.ContainsRune(path, 0) {
				return nil, errors.New("invalid attachment path reference")
			}
			attachments = append(attachments, path)
		}
	}
	if method == "draft.delete" {
		text = ""
		focusedRunID = ""
	}
	return c.Store.Mutate("draft", id, func(old Doc) (Doc, error) {
		if old == nil {
			old = emptyDraft(wid, sid)
		}
		rev := number(old, "revision", 0)
		if number(p, "expectedRevision", -1) != rev {
			return nil, fmt.Errorf("draft revision conflict: expected %d; current %d", number(p, "expectedRevision", -1), rev)
		}
		return Doc{"id": id, "schemaVersion": 1, "workspaceId": wid, "sessionId": sid, "text": text, "attachments": attachments, "focusedRunId": focusedRunID, "revision": rev + 1, "updatedAt": store.Now()}, nil
	})
}

// Submission clears only the acknowledged source revision. The empty record is
// a tombstone: an old save cannot recreate text after an accepted submission.
func (c *Core) clearSubmittedDraft(workspaceID, sessionID string, expected int) (Doc, bool, error) {
	id := draftID(workspaceID, sessionID)
	cleared := false
	d, err := c.Store.Mutate("draft", id, func(old Doc) (Doc, error) {
		if old == nil {
			old = emptyDraft(workspaceID, sessionID)
		}
		if number(old, "revision", 0) != expected {
			return old, nil
		}
		d := emptyDraft(workspaceID, sessionID)
		d["revision"] = expected + 1
		d["updatedAt"] = store.Now()
		cleared = true
		return d, nil
	})
	return d, cleared, err
}

func (c *Core) removeDrafts(field, id string) error {
	rows, err := c.Store.ListScoped("draft", field, id)
	if err != nil {
		return err
	}
	for _, d := range rows {
		if err = c.Store.Delete("draft", str(d, "id")); err != nil {
			return err
		}
	}
	return nil
}
