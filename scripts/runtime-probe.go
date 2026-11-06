//go:build ignore

// runtime-probe exercises the same installer/model acquisition/qualification
// manager as the app. It never grants model-generated tool calls host authority.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gemmadesktop.local/app/internal/runtimes"
)

func output(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (result error) {
	resources := flag.String("resources", "dist/runtime-resources", "trusted build resources")
	data := flag.String("data", ".runtime/managed-qualification", "isolated private app data")
	build := flag.String("build", "omlx-4213c328-gemma1", "exact catalog build")
	model := flag.String("model", "gemma4-e4b-4bit", "exact model artifact")
	contextTokens := flag.Int("context", 4096, "effective context tokens")
	batchStart := flag.String("batch-start", "", "original RFC3339 start when resuming this same bounded batch")
	flag.Parse()
	resourcePath, err := filepath.Abs(*resources)
	if err != nil {
		return err
	}
	dataPath, err := filepath.Abs(*data)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dataPath, 0700); err != nil {
		return err
	}
	catalog, models, err := runtimes.LoadCatalog(resourcePath)
	if err != nil {
		return err
	}
	var runtimeIdentity runtimes.Manifest
	var modelIdentity runtimes.ModelManifest
	for _, value := range catalog {
		if value.ID == *build {
			runtimeIdentity = value
		}
	}
	for _, value := range models {
		if value.ID == *model {
			modelIdentity = value
		}
	}
	if runtimeIdentity.ID == "" || modelIdentity.ID == "" {
		return errors.New("probe identity is not in trusted catalog")
	}
	digest := func(value any) string {
		data, _ := json.Marshal(value)
		return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	}
	started := time.Now().UTC()
	if *batchStart != "" {
		started, err = time.Parse(time.RFC3339, *batchStart)
		if err != nil {
			return err
		}
	}
	checkpointPath := filepath.Join(dataPath, "probe-checkpoint.json")
	if _, err := os.Stat(checkpointPath); err == nil {
		return errors.New("a checkpoint already exists; review it before starting another explicitly authorized batch")
	} else if !os.IsNotExist(err) {
		return err
	}
	checkpoint := map[string]any{"version": 1, "status": "running", "cursor": "install", "startedAt": started, "deadline": started.Add(2 * time.Hour), "schedule": []string{"install", "download", "activate", "protocol-smoke", "checkpoint"}, "scope": "4096-context compatibility smoke only; not coding or large-context qualification", "buildId": *build, "modelId": *model, "contextTokens": *contextTokens, "runtimeManifestDigest": digest(runtimeIdentity), "modelManifestDigest": digest(modelIdentity), "artifactDigest": modelIdentity.Model.ArtifactDigest, "templateDigest": modelIdentity.Model.TemplateDigest, "automaticNextBatch": false}
	if *contextTokens != 4096 {
		checkpoint["scope"] = "bounded protocol compatibility only; not coding qualification"
	}
	sourceFiles := map[string]string{}
	for _, pattern := range []string{"internal/inference/*.go", "internal/runtimes/*.go", "scripts/runtime-probe.go", "scripts/runtime-patch-omlx.py", "scripts/runtime-template-check.py"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sourceFiles[filepath.ToSlash(path)] = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		}
	}
	checkpoint["probeSourceFiles"] = sourceFiles
	persist := func() error {
		checkpoint["updatedAt"] = time.Now().UTC()
		data, err := json.MarshalIndent(checkpoint, "", "  ")
		if err != nil {
			return err
		}
		temp := checkpointPath + ".tmp"
		f, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err = f.Write(data); err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err = os.Rename(temp, checkpointPath); err != nil {
			return err
		}
		dir, err := os.Open(dataPath)
		if err != nil {
			return err
		}
		defer dir.Close()
		return dir.Sync()
	}
	if err = persist(); err != nil {
		return err
	}
	defer func() {
		checkpoint["cursor"] = "checkpoint"
		checkpoint["status"] = "checkpointed"
		checkpoint["outcome"] = map[string]bool{"protocolPassed": result == nil}
		if result != nil {
			checkpoint["status"] = "failed"
			checkpoint["error"] = result.Error()
		}
		result = errors.Join(result, persist())
	}()
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithDeadline(signalCtx, started.Add(2*time.Hour))
	defer cancel()
	if err = ctx.Err(); err != nil {
		return err
	}
	manager, err := runtimes.New(runtimes.Config{DataDir: dataPath, BundlesDir: resourcePath, Catalog: catalog, ModelCatalog: models})
	if err != nil {
		return err
	}
	defer func() { manager.Close(); checkpoint["processAfterCleanup"] = manager.Snapshot().Process }()
	wait := func(job runtimes.Job) error {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		lastStatus := ""
		var lastBytes int64
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
			for _, state := range manager.Snapshot().Jobs {
				if state.ID != job.ID {
					continue
				}
				if state.Status != lastStatus || state.CompletedBytes-lastBytes > 64<<20 {
					output(state)
					lastStatus = state.Status
					lastBytes = state.CompletedBytes
				}
				switch state.Status {
				case "complete":
					return nil
				case "failed", "cancelled", "interrupted":
					return errors.New(state.Error)
				}
			}
		}
	}
	installed := false
	for _, record := range manager.Snapshot().Installed {
		if record.ID == *build {
			installed = true
		}
	}
	if !installed {
		job, err := manager.Install(*build)
		if err != nil {
			return err
		}
		if err = wait(job); err != nil {
			return err
		}
	}
	installed = false
	checkpoint["cursor"] = "download"
	if err = persist(); err != nil {
		return err
	}
	for _, record := range manager.Snapshot().Models {
		if record.ID == *model {
			installed = true
		}
	}
	if !installed {
		job, err := manager.AcquireModel(*model)
		if err != nil {
			return err
		}
		if err = wait(job); err != nil {
			return err
		}
	}
	checkpoint["cursor"] = "activate"
	if err = persist(); err != nil {
		return err
	}
	if err = manager.Activate(*build); err != nil {
		return err
	}
	output(map[string]any{"stage": "live-probe", "buildId": *build, "modelId": *model, "contextTokens": *contextTokens, "deadline": time.Now().Add(10 * time.Minute).UTC()})
	checkpoint["cursor"] = "protocol-smoke"
	if err = persist(); err != nil {
		return err
	}
	receipt, err := manager.Qualify(ctx, runtimes.StartRequest{BuildID: *build, ModelID: *model, ContextTokens: *contextTokens})
	if err != nil {
		return fmt.Errorf("qualification failed: %w", err)
	}
	output(receipt)
	checkpoint["receipt"] = receipt
	return nil
}
