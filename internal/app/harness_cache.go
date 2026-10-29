package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
)

type harnessCacheReleaser interface {
	ReleaseWorkspaceCache(context.Context, string) (execution.CacheRelease, error)
}

// Only explicitly recorded disposable Harness roots are eligible. This is not
// a general cache collector and never traverses ordinary project workspaces.
func (c *Core) harnessCachePath(kind, trialID, root string) error {
	if trialID == "" || filepath.Base(trialID) != trialID || trialID == "." || trialID == ".." {
		return errors.New("invalid disposable trial identity")
	}
	base := filepath.Join(c.DataDir, "harness")
	switch kind {
	case "trial":
		if root != filepath.Join(base, "trials", trialID, "workspace") {
			return errors.New("trial cache root is outside its owned fixture")
		}
	case "grader":
		if filepath.Dir(root) != base || !strings.HasPrefix(filepath.Base(root), "grading-") {
			return errors.New("grader cache root is outside its owned temporary fixture")
		}
	default:
		return errors.New("unknown disposable Harness cache kind")
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, root)
	if err != nil || resolved != filepath.Join(resolvedBase, rel) {
		return errors.New("disposable Harness root contains an unexpected symlink")
	}
	return nil
}

func (c *Core) beginHarnessCache(kind, trialID, root, image string) (Doc, error) {
	if err := c.harnessCachePath(kind, trialID, root); err != nil {
		return nil, err
	}
	row := Doc{"id": harness.Hash([]string{kind, trialID, root, image})[:32], "kind": kind, "trialId": trialID, "workspace": root, "image": image, "status": "pending", "updatedAt": store.Now()}
	// A later independent grade must retain the solver's actual release receipt,
	// not replace it with an absent-cache receipt for the same immutable trial.
	if saved, err := c.Store.Get("harness_cache_cleanup", str(row, "id")); err == nil {
		return saved, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return row, c.Store.Put("harness_cache_cleanup", row)
}

func (c *Core) finishHarnessCache(ctx context.Context, runner execution.Executor, row Doc) error {
	if row["status"] == "completed" {
		return nil
	}
	root, kind, trialID := str(row, "workspace"), str(row, "kind"), str(row, "trialId")
	if err := c.harnessCachePath(kind, trialID, root); err != nil {
		// A crash after recording a released grader cache and removing its temporary
		// source can leave only the final metadata write outstanding.
		if !(kind == "grader" && row["cacheReleased"] == true && errors.Is(err, os.ErrNotExist)) {
			return c.harnessCacheFailure(row, err)
		}
	} else if row["cacheReleased"] != true {
		releaser, ok := runner.(harnessCacheReleaser)
		if !ok {
			return c.harnessCacheFailure(row, errors.New("executor does not support guarded disposable cache release"))
		}
		released, err := releaser.ReleaseWorkspaceCache(ctx, root)
		row["release"] = released
		if err != nil {
			return c.harnessCacheFailure(row, err)
		}
		row["cacheReleased"] = true
		row["status"] = "released"
		row["updatedAt"] = store.Now()
		if err = c.Store.Put("harness_cache_cleanup", row); err != nil {
			return err
		}
	}
	if kind == "grader" {
		if err := os.RemoveAll(root); err != nil {
			return c.harnessCacheFailure(row, err)
		}
	}
	row["status"] = "completed"
	row["updatedAt"] = store.Now()
	delete(row, "error")
	return c.Store.Put("harness_cache_cleanup", row)
}
func (c *Core) harnessCacheFailure(row Doc, err error) error {
	row["status"], row["error"], row["updatedAt"] = "failed", err.Error(), store.Now()
	return errors.Join(fmt.Errorf("disposable Harness cache cleanup failed: %w", err), c.Store.Put("harness_cache_cleanup", row))
}

// Retry only durable cleanup requests after command recovery has reconciled
// guests. A running trial is never cleaned by this restart/admission path.
func (c *Core) reconcileHarnessCaches(ctx context.Context) error {
	rows, err := c.Store.List("harness_cache_cleanup")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row["status"] == "completed" {
			continue
		}
		trial, e := c.Store.Get("harness_trial", str(row, "trialId"))
		if e == nil && trial["status"] == "running" {
			continue
		}
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return e
		}
		runner, e := c.executorForConfig("docker", str(row, "image"))
		if e != nil {
			return c.harnessCacheFailure(row, e)
		}
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		e = c.finishHarnessCache(bounded, runner, row)
		cancel()
		if e != nil {
			return e
		}
	}
	return nil
}

func (c *Core) harnessCacheSummary() Doc {
	out := Doc{"pending": 0, "failed": 0}
	rows, err := c.Store.List("harness_cache_cleanup")
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	for _, row := range rows {
		switch row["status"] {
		case "pending", "released":
			out["pending"] = out["pending"].(int) + 1
		case "failed":
			out["failed"] = out["failed"].(int) + 1
			out["error"] = row["error"]
		}
	}
	return out
}
