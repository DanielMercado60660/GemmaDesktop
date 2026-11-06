package tools

import (
	"fmt"
	"strings"

	"gemmadesktop.local/app/internal/workspace"
)

// Only a successful edit from this conversation at this exact revision can
// be replayed as already applied. Finding newText somewhere in a file is not
// evidence that an earlier edit succeeded.
type completedEdit struct {
	oldHash, newHash, afterHash string
}

func (m *Manager) existingDirectoryResult(path string) (Result, bool) {
	clean, err := workspace.Clean(path, true)
	if err != nil {
		return Result{}, false
	}
	if _, err = m.workspace.DirectoryPath(clean); err != nil {
		return Result{}, false
	}
	return encode(map[string]any{"path": clean, "created": false, "exists": true, "message": "Directory already exists; no change was made."}), true
}

func (m *Manager) rememberEdit(scope, path, old, new, afterHash string) {
	m.revisionMu.Lock()
	defer m.revisionMu.Unlock()
	if m.completedEdits == nil {
		m.completedEdits = map[revisionKey]completedEdit{}
	}
	m.completedEdits[revisionKey{scope: scope, path: path}] = completedEdit{workspace.Hash([]byte(old)), workspace.Hash([]byte(new)), afterHash}
}

func (m *Manager) completedEditResult(scope, path, old, new, expectedHash string) (Result, bool) {
	clean, err := workspace.Clean(path, true)
	if err != nil {
		return Result{}, false
	}
	m.revisionMu.Lock()
	prior, ok := m.completedEdits[revisionKey{scope: scope, path: clean}]
	m.revisionMu.Unlock()
	if !ok || prior.oldHash != workspace.Hash([]byte(old)) || prior.newHash != workspace.Hash([]byte(new)) || prior.afterHash != expectedHash {
		return Result{}, false
	}
	current, err := m.workspace.Read(clean)
	if err != nil || current.Hash != prior.afterHash {
		return Result{}, false
	}
	return encode(map[string]any{"path": clean, "applied": true, "alreadyApplied": true, "replacements": 0, "beforeHash": current.Hash, "afterHash": current.Hash, "message": "Edit already applied successfully at this revision; no additional change was made."}), true
}

func appliedChangeData(name string, change workspace.Change, revisionNote string) map[string]any {
	message := "File written successfully."
	if name == "edit_file" {
		message = "Edit applied successfully."
	}
	data := map[string]any{"path": change.Path, "beforeHash": change.BeforeHash, "afterHash": change.AfterHash, "created": change.Created, "applied": true, "message": message}
	if name == "edit_file" {
		data["replacements"] = 1
		data["diffPreview"] = editDiffPreview(change.Before, change.After)
	}
	if revisionNote != "" {
		data["revisionNote"] = message + " " + revisionNote
	}
	return data
}

// A bounded preview, not a complete patch: show the changed region after
// trimming shared prefix/suffix lines. The artifact keeps the full before/after.
func editDiffPreview(before, after string) string {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	if start == endA && start == endB {
		return "No text difference."
	}
	lines := []string{fmt.Sprintf("@@ -%d,%d +%d,%d @@ (preview)", start+1, endA-start, start+1, endB-start)}
	appendLines := func(prefix string, changed []string) {
		for _, line := range changed[:min(len(changed), 8)] {
			lines = append(lines, prefix+clip(line, 160))
		}
		if len(changed) > 8 {
			lines = append(lines, fmt.Sprintf("… %d more %s lines; see the full change artifact", len(changed)-8, prefix))
		}
	}
	appendLines("-", a[start:endA])
	appendLines("+", b[start:endB])
	return strings.Join(lines, "\n")
}
