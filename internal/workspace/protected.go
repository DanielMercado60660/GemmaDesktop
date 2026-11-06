package workspace

import (
	"errors"
	"strings"
)

// Task protection is an explicit immutable task constraint, separate from the
// global credential/path policy. Comparison is conservative on case-folding
// filesystems, including changes to an ancestor of a protected path.
func ValidateProtectedPaths(paths []string) error {
	if len(paths) > 256 {
		return errors.New("at most 256 task-protected paths")
	}
	for _, p := range paths {
		clean, err := Clean(p, true)
		if err != nil || clean != p || p == "." || len(p) > 4096 {
			return errors.New("task-protected paths must be clean relative paths")
		}
	}
	return nil
}

func TaskProtected(name string, paths []string) bool {
	name = strings.ToLower(name)
	for _, p := range paths {
		p = strings.ToLower(p)
		if name == p || strings.HasPrefix(name, p+"/") || strings.HasPrefix(p, name+"/") {
			return true
		}
	}
	return false
}
