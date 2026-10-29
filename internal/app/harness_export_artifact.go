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
	value, err := c.harnessCall(ctx, "harness.experiments.export", p)
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
