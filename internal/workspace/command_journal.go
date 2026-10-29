package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type journalChange struct {
	CommandChange
	BeforeMode uint32 `json:"beforeMode"`
	AfterMode  uint32 `json:"afterMode"`
	BeforeFile string `json:"beforeFile,omitempty"`
	AfterFile  string `json:"afterFile,omitempty"`
}
type commandJournal struct {
	Version            int             `json:"version"`
	Workspace          string          `json:"workspace"`
	Status             string          `json:"status"`
	Changes            []journalChange `json:"changes"`
	Applied            int             `json:"applied"`
	Error              string          `json:"error,omitempty"`
	CreatedDirectories []string        `json:"createdDirectories,omitempty"`
}
type Publication struct {
	Status      string          `json:"status"`
	Changes     []CommandChange `json:"changes"`
	JournalPath string          `json:"journalPath,omitempty"`
}

func saveCommandJournal(dir string, j commandJournal) error {
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(dir, "journal.tmp"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(filepath.Join(dir, "journal.tmp"), filepath.Join(dir, "journal.json")); e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

// PublishCommand checks the entire source manifest before applying any delta.
// Each before/after image is durable before the journal enters applying state.
// External editors cannot be globally locked; changes are optimistic and each
// destination is checked again immediately before its individual mutation.
func PublishCommand(ctx context.Context, root, snapshot, journalDir string, base CommandManifest, private []string) (Publication, error) {
	out := Publication{Status: "not_applied", Changes: []CommandChange{}, JournalPath: filepath.Join(journalDir, "journal.json")}
	ctx, release, e := AcquireOperation(ctx, root)
	if e != nil {
		return out, e
	}
	defer release()
	current, e := ScanCommandTree(ctx, root, "", private)
	if e != nil {
		return out, e
	}
	if !SameCommandManifest(base, current) {
		out.Status = "conflict"
		return out, fmt.Errorf("workspace changed while command ran: %w", ErrConflict)
	}
	after, e := ScanCommandTree(ctx, snapshot, "", private)
	if e != nil {
		return out, e
	}
	w, e := Open(root)
	if e != nil {
		return out, e
	}
	defer w.Close()
	sw, e := Open(snapshot)
	if e != nil {
		return out, e
	}
	defer sw.Close()
	j := commandJournal{Version: 1, Workspace: w.Path, Status: "prepared", Changes: []journalChange{}}
	names := map[string]bool{}
	for n := range base.Files {
		names[n] = true
	}
	for n := range after.Files {
		names[n] = true
	}
	keys := []string{}
	for n := range names {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	if e = os.MkdirAll(journalDir, 0700); e != nil {
		return out, e
	}
	total := int64(0)
	for _, n := range keys {
		b, bok := base.Files[n]
		a, aok := after.Files[n]
		if b == a && bok == aok {
			continue
		}
		// Empty directories are not source changes; file parents are created below.
		if b.Directory || a.Directory {
			if bok && aok && b.Directory != a.Directory {
				return out, fmt.Errorf("file/directory replacement unsupported: %s", n)
			}
			continue
		}
		if len(j.Changes) >= CommandMaxChanges {
			return out, errors.New("command exceeds 256 changed source files")
		}
		c := journalChange{CommandChange: CommandChange{Path: n, BeforeHash: b.Hash, AfterHash: a.Hash, Size: a.Size, Deleted: !aok}, BeforeMode: b.Mode, AfterMode: a.Mode}
		for _, version := range []string{"before", "after"} {
			if version == "before" && !bok || version == "after" && !aok {
				continue
			}
			reader := w
			expected := b.Hash
			if version == "after" {
				reader = sw
				expected = a.Hash
			}
			data, err := readCommandBytes(reader, n)
			if err != nil {
				return out, err
			}
			if Hash(data) != expected {
				return out, ErrConflict
			}
			total += int64(len(data))
			if total > CommandMaxChangeBytes {
				return out, errors.New("command source changes exceed 32 MiB journal budget")
			}
			fn := fmt.Sprintf("%s-%04d", version, len(j.Changes))
			if err = writeJournalImage(journalDir, fn, data); err != nil {
				return out, err
			}
			if version == "before" {
				c.BeforeFile = fn
			} else {
				c.AfterFile = fn
			}
		}
		j.Changes = append(j.Changes, c)
		out.Changes = append(out.Changes, c.CommandChange)
	}
	directories := map[string]bool{}
	for _, c := range j.Changes {
		if c.Deleted {
			continue
		}
		for p := path.Dir(c.Path); p != "."; p = path.Dir(p) {
			if _, ok := base.Files[p]; !ok {
				directories[p] = true
			}
		}
	}
	for p := range directories {
		j.CreatedDirectories = append(j.CreatedDirectories, p)
	}
	sort.Strings(j.CreatedDirectories)
	if e = ctx.Err(); e != nil {
		return out, e
	}
	// Revalidate all input bytes after journal preparation, before any source write.
	current, e = ScanCommandTree(ctx, root, "", private)
	if e != nil {
		return out, e
	}
	if !SameCommandManifest(base, current) {
		out.Status = "conflict"
		return out, ErrConflict
	}
	j.Status = "applying"
	if e = saveCommandJournal(journalDir, j); e != nil {
		return out, e
	}
	// Publication is bounded and deliberately finishes/rolls back after it starts,
	// even if the run context is subsequently cancelled.
	for i, c := range j.Changes {
		if e = applyJournalChange(w, journalDir, c, false); e != nil {
			j.Error = e.Error()
			_ = saveCommandJournal(journalDir, j)
			if re := rollbackCommand(w, journalDir, &j); re != nil {
				out.Status = "recovery_required"
				return out, fmt.Errorf("publication failed: %v; rollback blocked: %w", e, re)
			}
			out.Status = "rolled_back"
			return out, e
		}
		j.Applied = i + 1
		if e = saveCommandJournal(journalDir, j); e != nil {
			out.Status = "recovery_required"
			return out, e
		}
	}
	j.Status = "committed"
	if e = saveCommandJournal(journalDir, j); e != nil {
		out.Status = "recovery_required"
		return out, e
	}
	out.Status = "applied"
	return out, nil
}

func writeJournalImage(dir, name string, b []byte) error {
	f, e := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func applyJournalChange(w *Workspace, dir string, c journalChange, reverse bool) error {
	expected := c.BeforeHash
	image := c.AfterFile
	mode := c.AfterMode
	deleting := c.Deleted
	if reverse {
		expected = c.AfterHash
		image = c.BeforeFile
		mode = c.BeforeMode
		deleting = c.BeforeHash == ""
	}
	if expected == "" {
		expected = "missing"
	}
	var b []byte
	var e error
	if image != "" {
		b, e = os.ReadFile(filepath.Join(dir, image))
		if e != nil {
			return e
		}
		target := c.AfterHash
		if reverse {
			target = c.BeforeHash
		}
		if Hash(b) != target {
			return errors.New("journal image integrity check failed")
		}
	}
	if !deleting {
		if e = commandEnsureParents(w, c.Path); e != nil {
			return e
		}
	}
	return w.commandReplace(c.Path, b, expected, mode, deleting)
}

func rollbackCommand(w *Workspace, dir string, j *commandJournal) error {
	for i := len(j.Changes) - 1; i >= 0; i-- {
		c := j.Changes[i]
		data, e := readCommandBytes(w, c.Path)
		hash := ""
		mode := uint32(0)
		if e == nil {
			hash = Hash(data)
			r, base, err := w.parent(c.Path, true)
			if err != nil {
				return err
			}
			inf, err := r.Lstat(base)
			r.Close()
			if err != nil {
				return err
			}
			mode = uint32(inf.Mode().Perm())
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if hash == c.BeforeHash && mode == c.BeforeMode {
			continue
		}
		if hash != c.AfterHash || mode != c.AfterMode {
			return fmt.Errorf("recovery conflicts with external edit to %q", c.Path)
		}
		if e = applyJournalChange(w, dir, c, true); e != nil {
			return e
		}
	}
	// Only remove directories this publication could have created, and only if
	// still empty. Preserve unrelated editor-created contents.
	for i := len(j.CreatedDirectories) - 1; i >= 0; i-- {
		n := j.CreatedDirectories[i]
		r, base, e := w.parent(n, true)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		info, e := r.Lstat(base)
		if errors.Is(e, os.ErrNotExist) {
			r.Close()
			continue
		}
		if e != nil || !info.IsDir() {
			r.Close()
			return fmt.Errorf("recovery directory changed: %q", n)
		}
		e = r.Remove(base)
		r.Close()
		if e != nil {
			return fmt.Errorf("recovery retained non-empty directory %q: %w", n, e)
		}
	}
	j.Status = "rolled_back"
	return saveCommandJournal(dir, *j)
}

// RecoverCommandJournal rolls back an interrupted publication, only when every
// current file still matches a journal before/after image. It never re-runs code.
func RecoverCommandJournal(ctx context.Context, journalPath string) (Publication, error) {
	out := Publication{JournalPath: journalPath}
	f, e := os.Open(journalPath)
	if e != nil {
		return out, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return out, e
	}
	if info.Size() > 256<<10 {
		return out, errors.New("journal metadata too large")
	}
	var j commandJournal
	if e = json.NewDecoder(f).Decode(&j); e != nil {
		return out, e
	}
	if j.Version != 1 || len(j.Changes) > CommandMaxChanges {
		return out, errors.New("unsupported journal")
	}
	if len(j.CreatedDirectories) > CommandMaxFiles {
		return out, errors.New("journal directory count exceeds limit")
	}
	for _, n := range j.CreatedDirectories {
		if !CommandPathAllowed(n, nil) || strings.Contains(n, "\\") {
			return out, ErrProtected
		}
	}
	if j.Status == "committed" || j.Status == "rolled_back" {
		out.Status = j.Status
		return out, nil
	}
	for _, c := range j.Changes {
		if !CommandPathAllowed(c.Path, nil) {
			return out, ErrProtected
		}
		for _, n := range []string{c.BeforeFile, c.AfterFile} {
			if n != "" && (filepath.Base(n) != n || n == "." || n == "..") {
				return out, errors.New("invalid journal image")
			}
		}
	}
	_, release, e := AcquireOperation(ctx, j.Workspace)
	if e != nil {
		return out, e
	}
	defer release()
	w, e := Open(j.Workspace)
	if e != nil {
		return out, e
	}
	defer w.Close()
	if e = rollbackCommand(w, filepath.Dir(journalPath), &j); e != nil {
		out.Status = "recovery_required"
		return out, e
	}
	out.Status = j.Status
	return out, nil
}
