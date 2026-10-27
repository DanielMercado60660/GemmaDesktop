package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"gemmadesktop.local/app/internal/workspace"
)

func (m *Manager) snapshot(ctx context.Context, paths []string, label string) (Artifact, error) {
	if m.options.ArtifactDir == "" {
		return Artifact{}, errors.New("an app-owned checkpoint directory is required before modifying existing files")
	}
	if len(paths) == 0 || len(paths) > 20 || len(label) > 200 {
		return Artifact{}, errors.New("checkpoint path or label limit exceeded")
	}
	files := []workspace.File{}
	total := 0
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return Artifact{}, err
		}
		f, err := m.workspace.Read(path)
		if err != nil {
			return Artifact{}, err
		}
		total += len(f.Content)
		if total > 8<<20 {
			return Artifact{}, errors.New("checkpoint exceeds 8 MiB text limit")
		}
		files = append(files, f)
	}
	data, err := json.Marshal(map[string]any{"version": 1, "kind": "workspace-file-snapshot", "workspace": m.workspace.Path, "label": label, "createdAt": time.Now().UTC(), "files": files})
	if err != nil {
		return Artifact{}, err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Artifact{}, err
	}
	name := "checkpoint-" + hex.EncodeToString(nonce[:]) + ".json"
	r, err := os.OpenRoot(m.options.ArtifactDir)
	if err != nil {
		return Artifact{}, err
	}
	defer r.Close()
	f, err := r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Artifact{}, err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		r.Remove(name)
		return Artifact{}, err
	}
	if ce != nil {
		r.Remove(name)
		return Artifact{}, ce
	}
	dir, err := r.Open(".")
	if err != nil {
		return Artifact{}, err
	}
	err = dir.Sync()
	dir.Close()
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{Path: filepath.Join(m.options.ArtifactDir, name), MediaType: "application/json", Size: int64(len(data))}, nil
}
