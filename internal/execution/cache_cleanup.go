package execution

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"gemmadesktop.local/app/internal/workspace"
)

// CacheRelease reports logical file bytes unlinked, not allocated disk blocks.
// A failed release may be partial and can be retried; source and journals are
// never removed. Removed means the complete cache tree existed and was removed.
type CacheRelease struct {
	CacheKey     string `json:"cacheKey"`
	Removed      bool   `json:"removed"`
	Entries      int    `json:"entries"`
	LogicalBytes int64  `json:"logicalBytes"`
}

// ReleaseWorkspaceCache is an app lifecycle operation, not a model tool or a
// general prune. Call only for a completed disposable fixture whose workspace
// still exists. It releases that canonical workspace's configured-image cache,
// including all lock-keyed environments, after app-owned work has stopped.
func (d *DockerExecutor) ReleaseWorkspaceCache(ctx context.Context, canonicalWorkspace string) (out CacheRelease, err error) {
	w, err := workspace.Open(canonicalWorkspace)
	if err != nil {
		return out, err
	}
	defer w.Close()
	ctx, release, err := workspace.AcquireOperation(ctx, w.Path)
	if err != nil {
		return out, err
	}
	defer release()
	v, _ := dockerSlots.LoadOrStore(d.config.StateDir, make(chan struct{}, 1))
	slot := v.(chan struct{})
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return out, ctx.Err()
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = d.pendingRecovery(); err != nil {
		return out, err
	}
	out.CacheKey = filepath.Base(d.dependencyCache(w.Path))
	state, err := os.OpenRoot(d.config.StateDir)
	if err != nil {
		return out, err
	}
	defer state.Close()
	cache, err := openCleanupDirectory(state, "cache")
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	defer cache.Close()
	if _, err = cache.Lstat(out.CacheKey); errors.Is(err, os.ErrNotExist) {
		return out, nil
	} else if err != nil {
		return out, err
	}
	// The key itself must be a genuine directory; descendant symlinks are
	// unlinked rather than followed, including links to other cache keys.
	if root, err := openCleanupDirectory(cache, out.CacheKey); err != nil {
		return out, err
	} else {
		root.Close()
	}
	if err = removeCacheEntry(ctx, cache, out.CacheKey, 0, &out); err != nil {
		return out, err
	}
	f, err := cache.Open(".")
	if err != nil {
		return out, err
	}
	err = f.Sync()
	f.Close()
	if err == nil {
		out.Removed = true
	}
	return out, err
}

func openCleanupDirectory(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("cache cleanup requires a real directory, not a symlink")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		child.Close()
		return nil, errors.New("cache cleanup directory changed")
	}
	return child, nil
}

func removeCacheEntry(ctx context.Context, parent *os.Root, name string, depth int, out *CacheRelease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 128 {
		return errors.New("cache directory nesting exceeds cleanup bound; explicit maintenance required")
	}
	if out.Entries >= executionEntryBudget {
		return errors.New("cache cleanup work limit reached; retry to continue")
	}
	info, err := parent.Lstat(name)
	if err != nil {
		return err
	}
	if info.IsDir() {
		dir, err := openCleanupDirectory(parent, name)
		if err != nil {
			return err
		}
		defer dir.Close()
		// Guest-owned directories may have removed owner write permission.
		// This is confined to the already-open cache directory.
		if err = dir.Chmod(".", 0700); err != nil {
			return err
		}
		f, err := dir.Open(".")
		if err != nil {
			return err
		}
		defer f.Close()
		for {
			entries, readErr := f.ReadDir(128)
			for _, entry := range entries {
				if err = removeCacheEntry(ctx, dir, entry.Name(), depth+1, out); err != nil {
					return err
				}
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	if err = parent.Remove(name); err != nil {
		return err
	}
	out.Entries++
	if info.Mode().IsRegular() {
		out.LogicalBytes += info.Size()
	}
	return nil
}
