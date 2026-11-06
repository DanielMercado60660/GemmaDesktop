package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"gemmadesktop.local/app/internal/store"
)

// Export stays in the application artifact directory until the user chooses to
// share it. No model provider or connected service receives the experiment.
func (c *Core) exportHarnessArtifact(ctx context.Context, p Doc) (any, error) {
	method := "harness.experiments.export"
	if p["version"] == "v2" {
		method = "harness.diagnostics.export"
		if p["kind"] == "qualification" {
			method = "harness.evaluationsV2.export"
		}
	}
	value, err := c.harnessCall(ctx, method, p)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	blob, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(blob) > 100<<20 {
		return nil, errors.New("experiment export exceeds 100 MiB")
	}
	blob = append(blob, '\n')
	digest := sha256.Sum256(blob)
	id := hex.EncodeToString(digest[:])
	dir := filepath.Join(c.DataDir, "artifacts", "harness")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, id+".json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
	if errors.Is(err, os.ErrExist) {
		prior, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if sha256.Sum256(prior) != digest {
			return nil, errors.New("existing export artifact changed")
		}
		if artifact, getErr := c.Store.Get("artifact", id); getErr == nil {
			return artifact, nil
		}
		return nil, errors.New("export file exists without its artifact record; retain it for recovery")
	}
	if err != nil {
		return nil, err
	}
	_, err = file.Write(blob)
	err = errors.Join(err, file.Sync(), file.Close())
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	d := Doc{"id": id, "name": "Harness experiment " + str(p, "id") + ".json", "path": path, "size": len(blob), "mediaType": "application/json", "sha256": id, "createdAt": store.Now(), "source": "harness_export", "experimentId": str(p, "id")}
	if experiment, e := c.harness.Get("experiment", str(p, "id")); e == nil {
		d["workspaceId"] = str(obj(experiment["spec"]), "workspaceId")
	}
	if err = c.Store.Put("artifact", d); err != nil {
		return nil, err
	}
	return d, nil
}
