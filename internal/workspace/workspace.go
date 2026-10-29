// Package workspace provides bounded, traversal-resistant access to one selected directory.
package workspace

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const MaxFileBytes = 2 << 20
const MaxEntries = 2000

var ErrConflict = errors.New("file changed since it was read; read it again before editing")
var ErrProtected = errors.New("protected path is unavailable to workspace tools")
var ErrBinary = errors.New("file is binary; use an artifact viewer")
var ErrTooLarge = errors.New("file exceeds the text read limit")
var writers sync.Map

type Workspace struct {
	root    *os.Root
	Path    string
	writeMu *sync.Mutex
}
type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsDir      bool      `json:"isDir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	Symlink    bool      `json:"symlink,omitempty"`
}
type File struct {
	Path       string    `json:"path"`
	Content    string    `json:"content"`
	Hash       string    `json:"hash"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	Executable bool      `json:"executable,omitempty"`
}
type Change struct {
	Path       string `json:"path"`
	Before     string `json:"before"`
	After      string `json:"after"`
	BeforeHash string `json:"beforeHash"`
	AfterHash  string `json:"afterHash"`
	Created    bool   `json:"created"`
}
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}
type SearchResult struct {
	Matches      []Match `json:"matches"`
	Truncated    bool    `json:"truncated"`
	FilesScanned int     `json:"filesScanned"`
}

func Open(dir string) (*Workspace, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	mu, _ := writers.LoadOrStore(abs, &sync.Mutex{})
	return &Workspace{root: r, Path: abs, writeMu: mu.(*sync.Mutex)}, nil
}
func (w *Workspace) Close() error { return w.root.Close() }
func Hash(b []byte) string        { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Clean never accepts an absolute path or parent traversal, even if cleaning would keep it inside.
func Clean(name string, writing bool) (string, error) {
	if name == "" {
		name = "."
	}
	if strings.ContainsRune(name, 0) || filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return "", errors.New("use a relative workspace path")
	}
	for _, p := range strings.Split(name, "/") {
		if p == ".." {
			return "", errors.New("parent traversal is not allowed")
		}
		lower := strings.ToLower(p)
		switch lower {
		case ".npmrc", ".pypirc", ".netrc", ".git-credentials", ".docker", ".kube", ".azure":
			return "", ErrProtected
		}
		if lower == ".git" || lower == ".ssh" || lower == ".aws" || lower == ".gnupg" || lower == ".codex" || lower == ".gemmadesktop" || lower == ".gemma-desktop" || lower == ".env" || strings.HasPrefix(lower, ".env.") && !strings.HasSuffix(lower, ".example") && !strings.HasSuffix(lower, ".sample") {
			return "", ErrProtected
		}
		if writing && (lower == ".agents" || lower == "agents.md") {
			return "", ErrProtected
		}
	}
	return path.Clean(name), nil
}

// parent opens each directory separately and compares its inode before/after opening.
// This pins ancestors and prevents a checked component being replaced by a symlink.
func (w *Workspace) parent(name string, writing bool) (*os.Root, string, error) {
	name, err := Clean(name, writing)
	if err != nil {
		return nil, "", err
	}
	parts := strings.Split(name, "/")
	r, err := w.root.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	for _, p := range parts[:len(parts)-1] {
		if p == "." {
			continue
		}
		before, e := r.Lstat(p)
		if e != nil {
			r.Close()
			return nil, "", e
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			r.Close()
			return nil, "", errors.New("symlink or non-directory path component is not allowed")
		}
		next, e := r.OpenRoot(p)
		if e != nil {
			r.Close()
			return nil, "", e
		}
		after, e := next.Stat(".")
		if e != nil || !os.SameFile(before, after) {
			next.Close()
			r.Close()
			return nil, "", ErrConflict
		}
		r.Close()
		r = next
	}
	return r, parts[len(parts)-1], nil
}

func regular(r *os.Root, name string) (*os.File, os.FileInfo, error) {
	before, err := r.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, errors.New("only regular files may be read or edited; symlinks are not followed")
	}
	if stat, ok := before.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		return nil, nil, errors.New("hard-linked files are unavailable; use a private regular copy")
	}
	f, err := r.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		f.Close()
		return nil, nil, ErrConflict
	}
	return f, after, nil
}

func (w *Workspace) Read(name string) (File, error) {
	return w.ReadLimit(name, MaxFileBytes)
}

// ReadLimit applies the same path and inode checks while enforcing a smaller
// caller budget before allocating the file's content.
func (w *Workspace) ReadLimit(name string, limit int) (File, error) {
	if limit < 1 || limit > MaxFileBytes {
		return File{}, errors.New("invalid text read limit")
	}
	r, base, err := w.parent(name, false)
	if err != nil {
		return File{}, err
	}
	defer r.Close()
	f, info, err := regular(r, base)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	if info.Size() > int64(limit) {
		return File{}, fmt.Errorf("%w (%d bytes)", ErrTooLarge, limit)
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return File{}, err
	}
	if len(b) > limit {
		return File{}, ErrTooLarge
	}
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return File{}, ErrBinary
	}
	clean, _ := Clean(name, false)
	return File{Path: clean, Content: string(b), Hash: Hash(b), Size: int64(len(b)), ModifiedAt: info.ModTime(), Executable: info.Mode()&0111 != 0}, nil
}

func (w *Workspace) List(name string) ([]Entry, error) {
	r, base, err := w.parent(name, false)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	info, err := r.Lstat(base)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("directory required; symlinks are not followed")
	}
	d, err := r.OpenFile(base, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	if now, e := d.Stat(); e != nil || !os.SameFile(info, now) {
		return nil, ErrConflict
	}
	entries, err := d.ReadDir(MaxEntries + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > MaxEntries {
		return nil, fmt.Errorf("directory exceeds %d entries; select a smaller directory", MaxEntries)
	}
	out := []Entry{}
	for _, entry := range entries {
		p := path.Join(name, entry.Name())
		if _, err := Clean(p, false); err != nil {
			continue
		}
		inf, e := entry.Info()
		if e != nil {
			continue
		}
		out = append(out, Entry{Name: entry.Name(), Path: p, IsDir: inf.IsDir(), Size: inf.Size(), ModifiedAt: inf.ModTime(), Symlink: inf.Mode()&os.ModeSymlink != 0})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// DirectoryPath validates a command working directory using the same protected
// paths and no-symlink rules as file tools. Process confinement still applies
// independently if a directory is renamed after validation.
func (w *Workspace) DirectoryPath(name string) (string, error) {
	clean, err := Clean(name, false)
	if err != nil {
		return "", err
	}
	r, base, err := w.parent(clean, false)
	if err != nil {
		return "", err
	}
	defer r.Close()
	before, err := r.Lstat(base)
	if err != nil {
		return "", err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("working directory must be a directory without symlinks")
	}
	next, err := r.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer next.Close()
	after, err := next.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		return "", ErrConflict
	}
	return filepath.Join(w.Path, filepath.FromSlash(clean)), nil
}

// Write requires the exact hash from Read; a new file uses expectedHash="missing".
// Atomic replacement preserves hard-linked files outside this workspace. External
// editors are not controlled by our lock; hashes are checked again immediately before rename.
func (w *Workspace) Write(name, content, expectedHash string) (Change, error) {
	if len(content) > MaxFileBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return Change{}, errors.New("content must be bounded UTF-8 text")
	}
	if expectedHash == "" {
		return Change{}, errors.New("expectedHash is required (use missing for a new file)")
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	r, base, err := w.parent(name, true)
	if err != nil {
		return Change{}, err
	}
	defer r.Close()
	var before []byte
	mode := fs.FileMode(0644)
	created := false
	f, inf, err := regular(r, base)
	if errors.Is(err, os.ErrNotExist) {
		if expectedHash != "missing" {
			return Change{}, ErrConflict
		}
		created = true
	} else if err != nil {
		return Change{}, err
	} else {
		defer f.Close()
		mode = inf.Mode().Perm()
		before, err = io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
		if err != nil {
			return Change{}, err
		}
		if len(before) > MaxFileBytes || Hash(before) != expectedHash {
			return Change{}, ErrConflict
		}
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Change{}, err
	}
	tmp := ".gemma-write-" + hex.EncodeToString(nonce[:])
	out, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return Change{}, err
	}
	defer r.Remove(tmp)
	if _, err = io.WriteString(out, content); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return Change{}, err
	}
	if closeErr != nil {
		return Change{}, closeErr
	}
	if created {
		if _, err = r.Lstat(base); !errors.Is(err, os.ErrNotExist) {
			return Change{}, ErrConflict
		}
	} else {
		check, _, e := regular(r, base)
		if e != nil {
			return Change{}, ErrConflict
		}
		b, e := io.ReadAll(io.LimitReader(check, MaxFileBytes+1))
		check.Close()
		if e != nil || Hash(b) != expectedHash {
			return Change{}, ErrConflict
		}
	}
	if created {
		err = r.Link(tmp, base)
	} else {
		err = r.Rename(tmp, base)
	}
	if err != nil {
		return Change{}, err
	}
	clean, _ := Clean(name, true)
	return Change{Path: clean, Before: string(before), After: content, BeforeHash: Hash(before), AfterHash: Hash([]byte(content)), Created: created}, nil
}

func (w *Workspace) Mkdir(name string) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	r, base, err := w.parent(name, true)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Mkdir(base, 0755)
}

// ValidateReplacement checks the exact edit before asking for approval. Replace
// performs this check again, followed by Write's final revision check.
func (w *Workspace) ValidateReplacement(name, old, expectedHash string) (File, error) {
	if old == "" {
		return File{}, errors.New("oldText cannot be empty")
	}
	f, err := w.Read(name)
	if err != nil {
		return File{}, err
	}
	if f.Hash != expectedHash {
		return File{}, ErrConflict
	}
	matches := strings.Count(f.Content, old)
	if matches == 0 {
		return File{}, errors.New("oldText was not found. Re-read the file and use a shorter unique span with actual newlines; do not copy JSON escape sequences. You may instead use write_file with the complete content and a fresh hash")
	}
	if matches > 1 {
		return File{}, fmt.Errorf("oldText matched %d times; include more surrounding text to identify exactly one occurrence", matches)
	}
	return f, nil
}

// Replace applies one unambiguous text edit to a file at its expected revision.
func (w *Workspace) Replace(name, old, new, expectedHash string) (Change, error) {
	f, err := w.ValidateReplacement(name, old, expectedHash)
	if err != nil {
		return Change{}, err
	}
	return w.Write(name, strings.Replace(f.Content, old, new, 1), expectedHash)
}

func (w *Workspace) Search(ctx context.Context, dir, query string, useRegex bool, limit int) (SearchResult, error) {
	if query == "" {
		return SearchResult{}, errors.New("query is required")
	}
	if len(query) > 4096 {
		return SearchResult{}, errors.New("query too long")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var re *regexp.Regexp
	var err error
	if useRegex {
		re, err = regexp.Compile(query)
		if err != nil {
			return SearchResult{}, err
		}
	}
	result := SearchResult{Matches: []Match{}}
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 || result.FilesScanned >= 5000 {
			result.Truncated = true
			return nil
		}
		entries, err := w.List(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(result.Matches) >= limit || result.FilesScanned >= 5000 {
				result.Truncated = true
				return nil
			}
			if e.Symlink {
				continue
			}
			if e.IsDir {
				if e.Name == "node_modules" || e.Name == "vendor" || e.Name == ".cache" || e.Name == "dist" || e.Name == "build" || e.Name == ".venv" || e.Name == "venv" || e.Name == "__pycache__" {
					continue
				}
				if err := walk(e.Path, depth+1); err != nil {
					return err
				}
				continue
			}
			if e.Size > MaxFileBytes {
				continue
			}
			f, err := w.Read(e.Path)
			if err != nil {
				continue
			}
			result.FilesScanned++
			s := bufio.NewScanner(strings.NewReader(f.Content))
			s.Buffer(make([]byte, 4096), MaxFileBytes)
			line := 0
			for s.Scan() {
				line++
				v := s.Text()
				matched := strings.Contains(v, query)
				if re != nil {
					matched = re.MatchString(v)
				}
				if matched {
					if len(v) > 1000 {
						v = string([]rune(v)[:min(len([]rune(v)), 1000)])
					}
					result.Matches = append(result.Matches, Match{Path: e.Path, Line: line, Text: v})
					if len(result.Matches) >= limit {
						result.Truncated = true
						break
					}
				}
			}
		}
		return nil
	}
	err = walk(dir, 0)
	return result, err
}
