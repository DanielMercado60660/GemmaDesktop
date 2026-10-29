package harness

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Export contains application-owned manifests, public fixture inputs, observed
// traces, output files and results. It never loads credentials or grading cases.
// Evaluation diagnostics are deliberately reduced to names/pass flags: trusted
// probe source, assertion inputs/expected values and hidden stdout stay private.
func (m *Manager) Export(id string) (Doc, error) {
	var x Experiment
	if e := m.get("experiment", id, &x); e != nil {
		return nil, e
	}
	out := Doc{"format": "gemma-harness-export-v1", "createdAt": m.now(), "experiment": asDoc(x), "upstreamRevision": UpstreamRevision, "limitations": []string{"Hidden grader sources and assertions are intentionally excluded; grader hashes identify the trusted policy.", "Credential records and raw thinking are never loaded. Known private metadata keys and the app data root are redacted from metadata/traces; candidate artifact bytes remain exact and should be reviewed before sharing."}}
	taskIDs := map[string]bool{}
	for _, id := range x.Spec.TaskIDs {
		taskIDs[id] = true
	}
	trialIDs := map[string]bool{}
	allTrials, e := m.config.Store.List("harness_trial")
	if e != nil {
		return nil, e
	}
	for _, d := range allTrials {
		if d["experimentId"] == id {
			if d["status"] == "running" {
				return nil, errors.New("wait for the active trial before exporting its artifacts")
			}
			trialIDs[stringValue(d["id"])] = true
		}
	}
	for _, id := range x.TrialIDs {
		trialIDs[id] = true
	}
	revisions := map[string]bool{x.Spec.BaseRevisionID: true, x.CandidateID: true}
	for _, kind := range []string{"packet", "evaluation", "promotion", "lesson"} {
		rows, e := m.config.Store.List("harness_" + kind)
		if e != nil {
			return nil, e
		}
		selected := []Doc{}
		for _, d := range rows {
			belongs := d["experimentId"] == id
			if kind == "promotion" {
				var v Evaluation
				if m.get("evaluation", stringValue(d["evaluationId"]), &v) == nil {
					belongs = v.ExperimentID == id
				}
			}
			if !belongs {
				continue
			}
			if kind == "evaluation" {
				var v Evaluation
				v, _ = decode[Evaluation](d)
				for i := range v.Results {
					r := &v.Results[i]
					trialIDs[r.TrialID] = true
					taskIDs[r.TaskID] = true
					r.Grade.Error = ""
					for j := range r.Grade.Checks {
						r.Grade.Checks[j].Error = ""
					}
				}
				d = asDoc(v)
			}
			if kind == "packet" {
				delete(d, "context")
				if rid := stringValue(d["revisionId"]); rid != "" {
					revisions[rid] = true
				}
			}
			selected = append(selected, d)
		}
		sort.Slice(selected, func(i, j int) bool { return stringValue(selected[i]["id"]) < stringValue(selected[j]["id"]) })
		out[kind+"s"] = selected
	}
	versions := []Lesson{}
	if heads, ok := out["lessons"].([]Doc); ok {
		for _, head := range heads {
			history, e := m.LessonHistory(stringValue(head["id"]))
			if e != nil {
				return nil, e
			}
			versions = append(versions, history...)
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].ID == versions[j].ID {
			return versions[i].Version < versions[j].Version
		}
		return versions[i].ID < versions[j].ID
	})
	out["lessonVersions"] = versions
	total := 0
	trials := []Doc{}
	for tid := range trialIDs {
		t, e := m.trial(tid)
		if e != nil {
			return nil, e
		}
		if t.OutputHash != "" {
			h, e := treeHash(t.Root)
			if e != nil || h != t.OutputHash {
				return nil, errors.New("export refused: trial output changed")
			}
		}
		files := map[string]Doc{}
		e = filepath.WalkDir(t.Root, func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("symlink in trial export")
			}
			if d.IsDir() {
				if path != t.Root && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "__pycache__") {
					return filepath.SkipDir
				}
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return errors.New("special file in trial export")
			}
			if info.Size() > 8<<20 {
				return errors.New("file exceeds export bound")
			}
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			total += len(b)
			if total > 64<<20 {
				return errors.New("export exceeds 64 MiB; export trials separately")
			}
			rel, _ := filepath.Rel(t.Root, path)
			digest := sha256.Sum256(b)
			files[rel] = Doc{"encoding": "base64", "sha256": fmt.Sprintf("%x", digest), "size": len(b), "content": base64.StdEncoding.EncodeToString(b)}
			return nil
		})
		if e != nil {
			return nil, e
		}
		d := asDoc(t)
		d["files"] = files
		trials = append(trials, d)
		revisions[t.RevisionID] = true
	}
	sort.Slice(trials, func(i, j int) bool { return stringValue(trials[i]["id"]) < stringValue(trials[j]["id"]) })
	out["trials"] = trials
	rs := []Doc{}
	for rid := range revisions {
		if rid == "" {
			continue
		}
		r, e := m.revision(rid)
		if e != nil {
			return nil, e
		}
		rs = append(rs, asDoc(r))
	}
	sort.Slice(rs, func(i, j int) bool { return stringValue(rs[i]["id"]) < stringValue(rs[j]["id"]) })
	out["revisions"] = rs
	ts := []Task{}
	for tid := range taskIDs {
		t := m.tasks[tid]
		t.Cases = nil
		t.Probe = nil
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
	out["publicFixtures"] = ts
	b, _ := json.Marshal(out)
	var safe Doc
	_ = json.Unmarshal(b, &safe)
	redactExport(map[string]any(safe), filepath.Dir(m.config.Root))
	exportedAt := safe["createdAt"]
	delete(safe, "createdAt")
	safe["payloadHash"] = Hash(safe)
	safe["createdAt"] = exportedAt
	return safe, nil
}
func stringValue(v any) string { s, _ := v.(string); return s }
func redactExport(v any, appRoot string) {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			switch strings.ToLower(k) {
			case "workingdirectory", "sourcepath", "cwd", "workspaceroot", "authorization", "accesstoken", "refreshtoken", "clientsecret", "password", "apikey", "cookie", "journalpath", "root", "datadir", "thinkingcontent", "rawthinking":
				delete(x, k)
			default:
				if text, ok := value.(string); ok && appRoot != "" && filepath.IsAbs(appRoot) && appRoot != "/" {
					x[k] = strings.ReplaceAll(text, appRoot, "[APP_DATA]")
				} else {
					redactExport(value, appRoot)
				}
			}
		}
	case []any:
		for _, value := range x {
			redactExport(value, appRoot)
		}
	}
}
