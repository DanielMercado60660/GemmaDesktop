package execution

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Cached contents are untrusted guest output. Never let a planted symlink
// redirect host-side package downloads or future bind mounts.
func ensureExecutionDir(abs string) error {
	if !filepath.IsAbs(abs) {
		return errors.New("private execution path must be absolute")
	}
	r, e := os.OpenRoot(string(filepath.Separator))
	if e != nil {
		return e
	}
	defer func() { r.Close() }()
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Clean(abs), string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		before, e := r.Lstat(component)
		if errors.Is(e, os.ErrNotExist) {
			if e = r.Mkdir(component, 0700); e != nil {
				return e
			}
			before, e = r.Lstat(component)
		}
		if e != nil {
			return e
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return errors.New("private execution directory contains a symlink or non-directory")
		}
		next, e := r.OpenRoot(component)
		if e != nil {
			return e
		}
		after, e := next.Stat(".")
		if e != nil || !os.SameFile(before, after) {
			next.Close()
			return errors.New("private execution directory changed")
		}
		r.Close()
		r = next
	}
	return nil
}
