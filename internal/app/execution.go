package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/store"
)

// executorForConfig uses the image recorded at admission. A resumed run must
// not silently acquire the user's newer execution configuration.
func (c *Core) executorForConfig(profile, image string, privatePaths ...[]string) (execution.Executor, error) {
	switch profile {
	case "", "restricted":
		return execution.NewRunner(), nil
	case "docker":
		if recovery, err := c.Store.Get("execution_state", "recovery"); err == nil && recovery["status"] == "blocked" {
			return nil, errors.New("Docker execution awaits recovery: " + str(recovery, "error"))
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if image == "" {
			return nil, errors.New("Configure an installed, pinned Docker toolchain image in Settings before using this capability profile")
		}
		config := execution.DockerConfig{StateDir: filepath.Join(c.DataDir, "execution"), Image: image}
		if len(privatePaths) > 0 {
			config.PrivatePaths = privatePaths[0]
		}
		return execution.NewDockerExecutor(config)
	default:
		return nil, errors.New("Unknown execution profile")
	}
}

func (c *Core) reconcileExecution(ctx context.Context) error {
	image := str(c.Settings(), "dockerImage")
	if image == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	x, err := execution.NewDockerExecutor(execution.DockerConfig{StateDir: filepath.Join(c.DataDir, "execution"), Image: image})
	d := Doc{"id": "recovery", "status": "ready", "checkedAt": store.Now()}
	if err == nil {
		var results any
		results, err = x.Reconcile(ctx)
		d["publications"] = results
	}
	if err != nil {
		d["status"] = "blocked"
		d["error"] = err.Error()
	}
	return errors.Join(err, c.Store.Put("execution_state", d))
}

func (c *Core) executorForProfile(profile string) (execution.Executor, error) {
	return c.executorForConfig(profile, str(c.Settings(), "dockerImage"))
}

func (c *Core) executionCall(ctx context.Context, method string, p Doc) (any, error) {
	switch method {
	case "execution.images":
		images, err := execution.InstalledToolchains(ctx)
		return Doc{"items": images}, err
	case "execution.status":
		image := str(c.Settings(), "dockerImage")
		d := Doc{"image": image, "configured": image != "", "defaults": Doc{"cpus": 4, "memoryGiB": 8, "pids": 256, "network": "none", "concurrency": 1}, "note": "Commands use private snapshots. Permitted source changes synchronize automatically; dependencies and configured private output directories stay in the command cache."}
		if recovery, err := c.Store.Get("execution_state", "recovery"); err == nil {
			d["recovery"] = recovery
			if recovery["status"] == "blocked" {
				d["error"] = recovery["error"]
			}
		}
		if image == "" {
			return d, nil
		}
		x, err := execution.NewDockerExecutor(execution.DockerConfig{StateDir: filepath.Join(c.DataDir, "execution"), Image: image})
		if err != nil {
			d["error"] = err.Error()
			return d, nil
		}
		liveRecovery, recoveryErr := x.RecoveryStatus()
		if liveRecovery != "ready" {
			recovery := Doc{"status": liveRecovery, "checkedAt": store.Now(), "source": "current command ownership journals"}
			if recoveryErr != nil {
				recovery["error"] = recoveryErr.Error()
				d["error"] = recoveryErr.Error()
			}
			d["recovery"] = recovery
		}
		status, err := x.Status(ctx)
		d["runtime"] = status
		if err != nil {
			d["error"] = err.Error()
		}
		return d, nil
	case "execution.configure":
		image := strings.TrimSpace(str(p, "image"))
		if image != "" {
			if _, err := execution.NewDockerExecutor(execution.DockerConfig{StateDir: filepath.Join(c.DataDir, "execution"), Image: image}); err != nil {
				return nil, err
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.runs) > 0 || len(c.evals) > 0 || c.harnessBusy() {
			return nil, errors.New("Wait for active runs and evaluations before changing the toolchain image")
		}
		settings, err := c.Store.Update("settings", "default", func(d Doc) error { d["dockerImage"] = image; return nil })
		if err != nil {
			return nil, err
		}
		if err = c.reconcileExecution(ctx); err != nil {
			return nil, err
		}
		return Doc{"image": settings["dockerImage"]}, nil
	case "execution.reconcile":
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.runs) > 0 || len(c.evals) > 0 || c.harnessBusy() {
			return nil, errors.New("Wait for active work before recovering command execution")
		}
		if err := c.reconcileExecution(ctx); err != nil {
			return nil, err
		}
		return c.Store.Get("execution_state", "recovery")
	default:
		return nil, errors.New("Unknown execution operation")
	}
}
