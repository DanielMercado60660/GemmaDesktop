package workspace

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const CommandMaxFiles = 20000
const CommandMaxFileBytes = 16 << 20
const CommandMaxTreeBytes = 256 << 20
const CommandMaxChanges = 256
const CommandMaxChangeBytes = 32 << 20

// These are execution-only state, never automatically published as source.
var DefaultPrivatePaths = []string{"node_modules", ".venv", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".cache"}

type ManifestEntry struct {
	Hash      string `json:"hash"`
	Size      int64  `json:"size"`
	Mode      uint32 `json:"mode"`
	Directory bool   `json:"directory,omitempty"`
}
type CommandManifest struct {
	Files    map[string]ManifestEntry `json:"files"`
	Digest   string                   `json:"digest"`
	Excluded int                      `json:"excluded"`
}
type CommandChange struct {
	Path       string `json:"path"`
	BeforeHash string `json:"beforeHash,omitempty"`
	AfterHash  string `json:"afterHash,omitempty"`
	Size       int64  `json:"size"`
	Deleted    bool   `json:"deleted,omitempty"`
}

func CommandPathAllowed(name string, private []string) bool {
	if _, err := Clean(name, true); err != nil {
		return false
	}
	for _, p := range strings.Split(name, "/") {
		switch strings.ToLower(p) {
		case ".npmrc", ".pypirc", ".netrc", ".git-credentials", ".docker", ".kube", ".azure", ".config":
			return false
		}
		if strings.HasPrefix(p, ".gemma-write-") {
			return false
		}
	}
	for _, p := range private {
		if name == p || strings.HasPrefix(name, p+"/") {
			return false
		}
		// Single-component cache names are excluded at any project depth.
		if !strings.Contains(p, "/") {
			for _, segment := range strings.Split(name, "/") {
				if segment == p {
					return false
				}
			}
		}
	}
	return true
}

func ValidatePrivatePaths(paths []string) error {
	if len(paths) > 32 {
		return errors.New("at most 32 private execution paths")
	}
	for _, p := range paths {
		if strings.ContainsAny(p, ",\n\r\"") {
			return errors.New("private paths cannot contain mount separators, quotes, or newlines")
		}
		clean, e := Clean(p, true)
		if e != nil || clean != p || p == "." {
			return errors.New("private execution paths must be clean relative directories")
		}
	}
	return nil
}

// ScanCommandTree optionally copies the exact bytes hashed to a private output
// directory. Every source open is root-anchored, no-follow and hardlink-checked.
// A second source scan is required after capture; this is not an OS snapshot.
func ScanCommandTree(ctx context.Context, root, copyTo string, private []string) (CommandManifest, error) {
	result := CommandManifest{Files: map[string]ManifestEntry{}}
	if err := ValidatePrivatePaths(private); err != nil {
		return result, err
	}
	w, err := Open(root)
	if err != nil {
		return result, err
	}
	defer w.Close()
	var total int64
	caseNames := map[string]string{}
	var walk func(string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var dr *os.Root
		if dir == "." {
			dr, err = w.root.OpenRoot(".")
		} else {
			pr, base, e := w.parent(dir, false)
			if e != nil {
				return e
			}
			before, e := pr.Lstat(base)
			if e != nil {
				pr.Close()
				return e
			}
			if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
				pr.Close()
				return ErrConflict
			}
			dr, e = pr.OpenRoot(base)
			pr.Close()
			if e != nil {
				return e
			}
			after, e := dr.Stat(".")
			if e != nil || !os.SameFile(before, after) {
				dr.Close()
				return ErrConflict
			}
		}
		if err != nil {
			return err
		}
		defer dr.Close()
		f, e := dr.Open(".")
		if e != nil {
			return e
		}
		entries, e := f.ReadDir(CommandMaxFiles + 1)
		f.Close()
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}
		if len(entries) > CommandMaxFiles {
			return errors.New("command snapshot directory exceeds entry limit")
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, ent := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := path.Join(dir, ent.Name())
			if !CommandPathAllowed(name, private) {
				result.Excluded++
				continue
			}
			if len(result.Files) >= CommandMaxFiles {
				return errors.New("command snapshot exceeds 20000 entries")
			}
			if len(name) > 1024 {
				return errors.New("command snapshot path exceeds 1024 bytes")
			}
			fold := strings.ToLower(name)
			if old, ok := caseNames[fold]; ok && old != name {
				return fmt.Errorf("case-colliding paths %q and %q", old, name)
			}
			caseNames[fold] = name
			info, e := dr.Lstat(ent.Name())
			if e != nil {
				return e
			}
			if info.IsDir() {
				result.Files[name] = ManifestEntry{Directory: true, Mode: uint32(info.Mode().Perm())}
				if copyTo != "" {
					if e = os.Mkdir(filepath.Join(copyTo, filepath.FromSlash(name)), 0755); e != nil {
						return e
					}
				}
				if e = walk(name); e != nil {
					return e
				}
				continue
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("command snapshot rejects non-regular path %q", name)
			}
			file, inf, e := regular(dr, ent.Name())
			if e != nil {
				return fmt.Errorf("%s: %w", name, e)
			}
			if inf.Size() > CommandMaxFileBytes {
				file.Close()
				return fmt.Errorf("%s exceeds 16 MiB command input limit", name)
			}
			b, e := io.ReadAll(io.LimitReader(file, CommandMaxFileBytes+1))
			file.Close()
			if e != nil {
				return e
			}
			total += int64(len(b))
			if len(b) > CommandMaxFileBytes || total > CommandMaxTreeBytes {
				return errors.New("command snapshot exceeds bounded input size")
			}
			mode := uint32(inf.Mode().Perm())
			result.Files[name] = ManifestEntry{Hash: Hash(b), Size: int64(len(b)), Mode: mode}
			if copyTo != "" {
				if e = os.WriteFile(filepath.Join(copyTo, filepath.FromSlash(name)), b, fs.FileMode(mode)); e != nil {
					return e
				}
				if e = os.Chmod(filepath.Join(copyTo, filepath.FromSlash(name)), fs.FileMode(mode)); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if err = walk("."); err != nil {
		return result, err
	}
	b, _ := json.Marshal(result.Files)
	result.Digest = Hash(b)
	return result, nil
}

func SameCommandManifest(a, b CommandManifest) bool { return a.Digest == b.Digest }

// commandReplace is used by journal publication/recovery, under the operation
// lease. Binary bytes are allowed, but every revision, path and type is checked.
func (w *Workspace) commandReplace(name string, data []byte, expected string, mode uint32, deleting bool) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	r, base, err := w.parent(name, true)
	if err != nil {
		return err
	}
	defer r.Close()
	check := func() error {
		f, _, e := regular(r, base)
		if errors.Is(e, os.ErrNotExist) && expected == "missing" {
			return nil
		}
		if e != nil {
			return fmt.Errorf("%q: %w", name, ErrConflict)
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, CommandMaxFileBytes+1))
		if e != nil {
			return e
		}
		if len(b) > CommandMaxFileBytes || Hash(b) != expected {
			return fmt.Errorf("%q: %w", name, ErrConflict)
		}
		return nil
	}
	if err = check(); err != nil {
		return err
	}
	if deleting {
		if err = r.Remove(base); err != nil {
			return err
		}
		dir, e := r.Open(".")
		if e != nil {
			return e
		}
		defer dir.Close()
		return dir.Sync()
	}
	if len(data) > CommandMaxFileBytes {
		return ErrTooLarge
	}
	// A unique sibling is created exclusively and renamed only after rechecking.
	tmpName := ".gemma-write-" + rand.Text()
	f, err := r.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fs.FileMode(mode)&0777)
	if err != nil {
		return err
	}
	defer r.Remove(tmpName)
	if err = f.Chmod(fs.FileMode(mode) & 0777); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	if err = check(); err != nil {
		return err
	}
	if expected == "missing" {
		err = r.Link(tmpName, base)
	} else {
		err = r.Rename(tmpName, base)
	}
	if err != nil {
		return err
	}
	d, e := r.Open(".")
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

func readCommandBytes(w *Workspace, name string) ([]byte, error) {
	r, base, e := w.parent(name, true)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	f, _, e := regular(r, base)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, CommandMaxFileBytes+1))
	if len(b) > CommandMaxFileBytes {
		return nil, ErrTooLarge
	}
	return b, e
}

func commandEnsureParents(w *Workspace, name string) error {
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(parent, "/") {
		current = path.Join(current, part)
		if e := w.Mkdir(current); e != nil {
			if !errors.Is(e, os.ErrExist) {
				return e
			}
		} else {
			r, _, e := w.parent(current, true)
			if e != nil {
				return e
			}
			dir, e := r.Open(".")
			r.Close()
			if e != nil {
				return e
			}
			e = dir.Sync()
			dir.Close()
			if e != nil {
				return e
			}
		}
	}
	return nil
}
