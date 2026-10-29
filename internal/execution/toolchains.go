package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Toolchain struct {
	ID       string   `json:"id"`
	RepoTags []string `json:"repoTags"`
	Size     int64    `json:"size"`
}

// InstalledToolchains only reads the local Docker Desktop daemon. It never
// starts Docker, contacts a registry, pulls an image, or changes app settings.
func InstalledToolchains(ctx context.Context) ([]Toolchain, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	d := &DockerExecutor{config: DockerConfig{DockerPath: "/usr/local/bin/docker", StateDir: os.TempDir()}, host: "unix://" + filepath.Join(home, ".docker", "run", "docker.sock")}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ids, e := d.cli(ctx, 8192, "image", "ls", "--no-trunc", "--quiet", "--filter", "label=org.opencontainers.image.title=Gemma Desktop isolated coding toolchain")
	if e != nil {
		return nil, e
	}
	out := []Toolchain{}
	seen := map[string]bool{}
	for _, id := range strings.Fields(ids) {
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(out) >= 32 {
			return nil, errors.New("installed toolchain count exceeds limit")
		}
		if !immutableImage.MatchString(id) {
			return nil, errors.New("Docker returned an invalid image identity")
		}
		b, err := d.cli(ctx, 8192, "image", "inspect", id, "--format", "{\"id\":{{json .Id}},\"repoTags\":{{json .RepoTags}},\"size\":{{json .Size}}}")
		if err != nil {
			return nil, err
		}
		var t Toolchain
		if err = json.Unmarshal([]byte(b), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
