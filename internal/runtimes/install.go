package runtimes

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func (m *Manager) preflightDisk(required int64) error {
	free, err := availableDiskBytes(m.root)
	if err != nil {
		return err
	}
	if required < 0 || required > free-m.config.DiskReserveBytes {
		return fmt.Errorf("insufficient disk space: need %d bytes plus %d bytes reserve, have %d", required, m.config.DiskReserveBytes, free)
	}
	return nil
}

func (m *Manager) startJob(kind, target string, total int64, work func(context.Context, string) error) (Job, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Job{}, errors.New("runtime manager is closed")
	}
	if len(m.jobs) > 0 {
		m.mu.Unlock()
		return Job{}, errors.New("another artifact job is active")
	}
	job := Job{ID: randomID(), Kind: kind, Target: target, Status: "queued", TotalBytes: total, UpdatedAt: time.Now().UTC()}
	m.state.Jobs[job.ID] = job
	if err := m.persistLocked(); err != nil {
		delete(m.state.Jobs, job.ID)
		m.mu.Unlock()
		return Job{}, err
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.jobs[job.ID] = cancel
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer cancel()
		err := work(ctx, job.ID)
		m.mu.Lock()
		current := m.state.Jobs[job.ID]
		current.Status = "complete"
		current.UpdatedAt = time.Now().UTC()
		if err != nil {
			current.Status = "failed"
			current.Error = err.Error()
			if errors.Is(err, context.Canceled) {
				current.Status = "cancelled"
			}
		} else {
			current.CompletedBytes = current.TotalBytes
		}
		m.state.Jobs[job.ID] = current
		delete(m.jobs, job.ID)
		if persistErr := m.persistLocked(); persistErr != nil {
			current.Status = "failed"
			current.Error = "Could not persist final job state: " + persistErr.Error()
			m.state.Jobs[job.ID] = current
		}
		m.mu.Unlock()
		m.changed()
	}()
	m.changed()
	return job, nil
}

func (m *Manager) updateJob(id, status string, completed int64) error {
	m.mu.Lock()
	job := m.state.Jobs[id]
	if job.Status == status && time.Since(job.UpdatedAt) < 500*time.Millisecond {
		m.mu.Unlock()
		return nil
	}
	job.Status = status
	job.CompletedBytes = completed
	job.UpdatedAt = time.Now().UTC()
	m.state.Jobs[id] = job
	err := m.persistLocked()
	m.mu.Unlock()
	m.changed()
	return err
}

func (m *Manager) Install(buildID string) (Job, error) {
	m.mu.Lock()
	manifest, ok := m.catalog[buildID]
	m.mu.Unlock()
	if !ok {
		return Job{}, errors.New("unknown trusted runtime build")
	}
	if err := platformPreflight(manifest); err != nil {
		return Job{}, err
	}
	requirements, err := runtimeRequirements(manifest)
	if err != nil {
		return Job{}, err
	}
	required, err := requirements.required()
	if err != nil {
		return Job{}, err
	}
	if err := m.preflightDisk(required); err != nil {
		return Job{}, err
	}
	return m.startJob("runtime_install", buildID, manifest.Bundle.Size, func(ctx context.Context, jobID string) error {
		if err := m.updateJob(jobID, "downloading", 0); err != nil {
			return err
		}
		var progressErr error
		archive, err := m.download(ctx, manifest.Bundle, func(n int64) {
			if progressErr == nil {
				progressErr = m.updateJob(jobID, "downloading", n)
			}
		})
		if progressErr != nil {
			return progressErr
		}
		if err != nil {
			return err
		}
		if err = m.updateJob(jobID, "extracting", manifest.Bundle.Size); err != nil {
			return err
		}
		destination := filepath.Join(m.root, "runtimes", buildID)
		staging := destination + ".stage-" + jobID
		if err = os.Mkdir(staging, 0700); err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		if err = extractBundle(ctx, archive, staging, manifest.Files); err != nil {
			return err
		}
		if err = m.updateJob(jobID, "verifying", manifest.Bundle.Size); err != nil {
			return err
		}
		if err = verifyInventory(ctx, staging, manifest.Files); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		m.opMu.Lock()
		defer m.opMu.Unlock()
		if _, err = os.Lstat(destination); err == nil {
			// An interrupted state write can leave a complete unregistered tree.
			// Adopt it only after its entire inventory independently verifies.
			if err = verifyInventory(ctx, destination, manifest.Files); err != nil {
				return errors.New("existing runtime tree differs from its trusted manifest")
			}
		} else if !os.IsNotExist(err) {
			return err
		} else if err = os.Rename(staging, destination); err != nil {
			return err
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		previous, had := m.state.Installed[buildID]
		m.state.Installed[buildID] = Installed{ID: buildID, ManifestDigest: digestJSON(manifest), InstalledAt: time.Now().UTC()}
		if err = m.persistLocked(); err != nil {
			if had {
				m.state.Installed[buildID] = previous
			} else {
				delete(m.state.Installed, buildID)
			}
			return err
		}
		_ = os.Remove(archive)
		return nil
	})
}

func extractBundle(ctx context.Context, archive, destination string, files []File) error {
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	gz, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(contextReader{ctx, gz})
	expected := map[string]File{}
	for _, file := range files {
		expected[file.Path] = file
	}
	seen := map[string]bool{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if !safeRelative(name) {
			return fmt.Errorf("unsafe archive entry %q", header.Name)
		}
		if header.Typeflag == tar.TypeDir {
			// Directories may only be ancestors of declared regular files.
			allowed := false
			for candidate := range expected {
				if strings.HasPrefix(candidate, name+"/") {
					allowed = true
					break
				}
			}
			if !allowed {
				return fmt.Errorf("undeclared archive directory %q", name)
			}
			if err = os.MkdirAll(filepath.Join(destination, filepath.FromSlash(name)), 0700); err != nil {
				return err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return errors.New("runtime bundle may contain only regular files and directories; links/devices are forbidden")
		}
		file, ok := expected[name]
		if !ok || seen[name] || file.Size != header.Size {
			return fmt.Errorf("archive entry %q differs from trusted inventory", name)
		}
		seen[name] = true
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(f, reader, file.Size)
		syncErr := f.Sync()
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		mode := os.FileMode(0400)
		if file.Executable {
			mode = 0500
		}
		if err = os.Chmod(target, mode); err != nil {
			return err
		}
	}
	if len(seen) != len(expected) {
		return errors.New("runtime archive omitted declared files")
	}
	return nil
}

func verifyInventory(ctx context.Context, directory string, files []File) error {
	expected := map[string]File{}
	for _, file := range files {
		expected[file.Path] = file
	}
	seen := 0
	err := filepath.WalkDir(directory, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("artifact inventory contains a symbolic link")
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(directory, filename)
		if err != nil {
			return err
		}
		file, ok := expected[filepath.ToSlash(rel)]
		if !ok {
			return errors.New("artifact contains an undeclared file")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if (info.Mode()&0111 != 0) != file.Executable {
			return errors.New("artifact executable mode differs from manifest")
		}
		if err = verifyFile(ctx, filename, file.Size, file.SHA256); err != nil {
			return fmt.Errorf("verify %s: %w", rel, err)
		}
		seen++
		return nil
	})
	if err != nil {
		return err
	}
	if seen != len(files) {
		return errors.New("installed artifact is missing files")
	}
	return nil
}

func (m *Manager) AcquireModel(modelID string) (Job, error) {
	m.mu.Lock()
	model, ok := m.models[modelID]
	m.mu.Unlock()
	if !ok {
		return Job{}, errors.New("unknown trusted model artifact")
	}
	requirements, err := modelRequirements(model)
	if err != nil {
		return Job{}, err
	}
	required, err := requirements.required()
	if err != nil {
		return Job{}, err
	}
	total := requirements.installedBytes
	if err := m.preflightDisk(required); err != nil {
		return Job{}, err
	}
	return m.startJob("model_download", modelID, total, func(ctx context.Context, jobID string) error {
		root := filepath.Join(m.root, "models", modelID)
		staging := root + ".stage-" + jobID
		destination := filepath.Join(staging, model.ServeName)
		if err := os.MkdirAll(destination, 0700); err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		var complete int64
		files := make([]File, 0, len(model.Files))
		for _, file := range model.Files {
			if err := m.updateJob(jobID, "downloading", complete); err != nil {
				return err
			}
			var progressErr error
			source, err := m.download(ctx, file.Source, func(n int64) {
				if progressErr == nil {
					progressErr = m.updateJob(jobID, "downloading", complete+n)
				}
			})
			if progressErr != nil {
				return progressErr
			}
			if err != nil {
				return err
			}
			target := filepath.Join(destination, filepath.FromSlash(file.Path))
			if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			in, err := os.Open(source)
			if err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
			if err != nil {
				in.Close()
				return err
			}
			_, err = io.Copy(out, contextReader{ctx, in})
			syncErr := out.Sync()
			out.Close()
			in.Close()
			if err != nil {
				return err
			}
			if syncErr != nil {
				return syncErr
			}
			files = append(files, file.File)
			complete += file.Size
			_ = os.Remove(source)
		}
		if err := m.updateJob(jobID, "verifying", complete); err != nil {
			return err
		}
		if err := verifyInventory(ctx, destination, files); err != nil {
			return err
		}
		m.opMu.Lock()
		defer m.opMu.Unlock()
		if _, err := os.Lstat(root); err == nil {
			if err = verifyInventory(ctx, filepath.Join(root, model.ServeName), files); err != nil {
				return errors.New("existing model differs from trusted artifact")
			}
		} else if !os.IsNotExist(err) {
			return err
		} else if err = os.Rename(staging, root); err != nil {
			return err
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		old, had := m.state.Models[modelID]
		m.state.Models[modelID] = Installed{ID: modelID, ManifestDigest: digestJSON(model), InstalledAt: time.Now().UTC()}
		if err := m.persistLocked(); err != nil {
			if had {
				m.state.Models[modelID] = old
			} else {
				delete(m.state.Models, modelID)
			}
			return err
		}
		return nil
	})
}

// ModelArtifactDigest hashes the ordered complete file inventory. A model's
// config/tokenizer/template and weights all contribute, not merely its alias.
func ModelArtifactDigest(files []ModelFile) string {
	inventory := make([]File, len(files))
	for i, file := range files {
		inventory[i] = file.File
		inventory[i].Path = path.Clean(inventory[i].Path)
	}
	return "sha256:" + digestJSON(inventory)
}
