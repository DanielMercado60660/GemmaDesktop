package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gemmadesktop.local/app/internal/workspace"
)

type lockedWheel struct {
	Name, Version string
	Hashes        map[string]bool
}

var wheelRequirement = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)==([A-Za-z0-9][A-Za-z0-9.!+_-]*)$`)
var sha256Hex = regexp.MustCompile(`^[a-f0-9]{64}$`)

func parseWheelLock(text string) ([]lockedWheel, error) {
	text = strings.ReplaceAll(text, "\\\r\n", " ")
	text = strings.ReplaceAll(text, "\\\n", " ")
	out := []lockedWheel{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		m := wheelRequirement.FindStringSubmatch(parts[0])
		if m == nil {
			return nil, errors.New("requirements.lock accepts only exact name==version and --hash=sha256:… entries; no URLs, options, markers, editable installs, or source builds")
		}
		w := lockedWheel{Name: m[1], Version: m[2], Hashes: map[string]bool{}}
		for _, h := range parts[1:] {
			if strings.HasPrefix(h, "#") {
				break
			}
			value := strings.TrimPrefix(h, "--hash=sha256:")
			if value == h || !sha256Hex.MatchString(value) {
				return nil, errors.New("each requirement needs a valid SHA-256 hash")
			}
			w.Hashes[value] = true
		}
		if len(w.Hashes) == 0 {
			return nil, errors.New("each requirement needs at least one locked SHA-256 wheel hash")
		}
		out = append(out, w)
		if len(out) > 100 {
			return nil, errors.New("wheel setup exceeds 100 requirements")
		}
	}
	return out, nil
}
func boundedPublicFetch(ctx context.Context, raw, origin string, limit int64) ([]byte, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host != origin || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("dependency URL is outside the public registry policy")
	}
	client, _, e := EndpointClient("https://" + origin)
	if e != nil {
		return nil, e
	}
	defer client.CloseIdleConnections()
	client.Timeout = 45 * time.Second
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return nil, e
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("dependency registry returned HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, errors.New("dependency response exceeded size limit")
	}
	return b, nil
}
func privateDownloadFile(dir, name string, b []byte) error {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return errors.New("invalid download filename")
	}
	if e := ensureExecutionDir(dir); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, "download-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}
func (d *DockerExecutor) dependencyCache(root string) string {
	return filepath.Join(d.config.StateDir, "cache", workspace.Hash([]byte(root + d.config.Image))[:32])
}
func (d *DockerExecutor) dependencyAdmission(ctx context.Context, root string) error {
	rel, e := filepath.Rel(root, d.config.StateDir)
	if e != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("execution state must be outside the selected workspace")
	}
	if e = d.pendingRecovery(); e != nil {
		return e
	}
	_, e = boundedTreeUsage(ctx, []string{d.config.StateDir})
	return e
}

func (d *DockerExecutor) provisionPip(ctx context.Context, req DependencyRequest, approve Authorizer) (DependencyResult, error) {
	out := DependencyResult{PackageManager: "pip", Command: Result{ExitCode: -1, Environment: "dependency-setup", Publication: "not_applied"}}
	w, e := workspace.Open(req.Workspace)
	if e != nil {
		return out, e
	}
	defer w.Close()
	ctx, release, e := workspace.AcquireOperation(ctx, w.Path)
	if e != nil {
		return out, e
	}
	defer release()
	if e = d.dependencyAdmission(ctx, w.Path); e != nil {
		return out, e
	}
	lock, e := w.Read("requirements.lock")
	if e != nil {
		return out, e
	}
	packages, e := parseWheelLock(lock.Content)
	if e != nil {
		return out, e
	}
	out.PackageCount = len(packages)
	out.LockHash = lock.Hash
	if e = Require(ctx, approve, Approval{Tool: "setup_dependencies", Workspace: w.Path, Environment: "public PyPI wheel fetch + offline Docker install", Arguments: map[string]any{"packageManager": "pip", "lockHash": lock.Hash, "packageCount": len(packages), "registries": []string{"pypi.org", "files.pythonhosted.org"}, "maxDownloadBytes": 256 << 20, "sourceBuilds": false}, Reason: "Fetch only SHA-256-locked public wheels; install offline into private .venv. No source distributions, setup scripts, credentials, or source writes."}); e != nil {
		return out, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	dir := filepath.Join(d.dependencyCache(w.Path), "wheels", lock.Hash)
	for _, p := range packages {
		b, err := boundedPublicFetch(ctx, "https://pypi.org/pypi/"+url.PathEscape(p.Name)+"/"+url.PathEscape(p.Version)+"/json", "pypi.org", 2<<20)
		if err != nil {
			return out, err
		}
		var metadata struct {
			URLs []struct {
				Filename    string            `json:"filename"`
				URL         string            `json:"url"`
				PackageType string            `json:"packagetype"`
				Digests     map[string]string `json:"digests"`
			} `json:"urls"`
		}
		if err = json.Unmarshal(b, &metadata); err != nil {
			return out, err
		}
		matched := 0
		for _, f := range metadata.URLs {
			if f.PackageType != "bdist_wheel" || !strings.HasSuffix(f.Filename, ".whl") || !p.Hashes[f.Digests["sha256"]] {
				continue
			}
			b, err = boundedPublicFetch(ctx, f.URL, "files.pythonhosted.org", 32<<20)
			if err != nil {
				return out, err
			}
			sum := sha256.Sum256(b)
			if hex.EncodeToString(sum[:]) != f.Digests["sha256"] {
				return out, errors.New("wheel integrity mismatch")
			}
			out.DownloadedBytes += int64(len(b))
			if out.DownloadedBytes > 256<<20 {
				return out, errors.New("wheel downloads exceed 256 MiB")
			}
			if err = privateDownloadFile(dir, f.Filename, b); err != nil {
				return out, err
			}
			matched++
		}
		if matched == 0 {
			return out, fmt.Errorf("no hash-matching public wheel for %s==%s; source builds are unavailable", p.Name, p.Version)
		}
	}
	now, e := w.Read("requirements.lock")
	if e != nil || now.Hash != lock.Hash {
		return out, workspace.ErrConflict
	}
	script := "python3 -m venv /workspace/.venv && /workspace/.venv/bin/python -m pip install --no-index --only-binary=:all: --require-hashes --no-deps --find-links=\"$1\" -r /workspace/requirements.lock"
	out.Command, e = d.Run(ctx, Request{Workspace: w.Path, Executable: "/bin/sh", Args: []string{"-c", script, "gemma-wheels", "/cache/wheels/" + lock.Hash}, ReadOnly: true, Timeout: 3 * time.Minute})
	if e == nil && out.Command.ExitCode != 0 {
		e = fmt.Errorf("offline wheel install exited with code %d", out.Command.ExitCode)
	}
	return out, e
}

type goLockedModule struct {
	Path, Version string
	Zip           bool
	Mod           bool
}

var modulePath = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)
var moduleVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+[A-Za-z0-9.+-]*$`)

func parseGoSum(text string) ([]goLockedModule, error) {
	mods := map[string]goLockedModule{}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p := strings.Fields(line)
		if len(p) != 3 || !modulePath.MatchString(p[0]) || strings.Contains(p[0], "..") || strings.HasPrefix(p[0], "/") || !strings.HasPrefix(p[2], "h1:") {
			return nil, errors.New("unsupported go.sum entry")
		}
		version := strings.TrimSuffix(p[1], "/go.mod")
		if !moduleVersion.MatchString(version) {
			return nil, errors.New("unsupported Go module version")
		}
		key := p[0] + "@" + version
		m := mods[key]
		m.Path = p[0]
		m.Version = version
		if p[1] == version {
			m.Zip = true
		} else {
			m.Mod = true
		}
		mods[key] = m
		if len(mods) > 100 {
			return nil, errors.New("Go setup exceeds 100 locked module versions")
		}
	}
	keys := []string{}
	for k := range mods {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []goLockedModule{}
	for _, k := range keys {
		m := mods[k]
		if m.Zip && !m.Mod {
			return nil, errors.New("Go zip entry also requires its go.mod checksum")
		}
		out = append(out, m)
	}
	return out, nil
}
func escapeGoModule(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func (d *DockerExecutor) provisionGo(ctx context.Context, req DependencyRequest, approve Authorizer) (DependencyResult, error) {
	out := DependencyResult{PackageManager: "go", Command: Result{ExitCode: -1, Environment: "dependency-setup", Publication: "not_applied"}}
	w, e := workspace.Open(req.Workspace)
	if e != nil {
		return out, e
	}
	defer w.Close()
	ctx, release, e := workspace.AcquireOperation(ctx, w.Path)
	if e != nil {
		return out, e
	}
	defer release()
	if e = d.dependencyAdmission(ctx, w.Path); e != nil {
		return out, e
	}
	lock, e := w.Read("go.sum")
	if e != nil {
		return out, e
	}
	manifest, e := w.Read("go.mod")
	if e != nil {
		return out, e
	}
	for _, line := range strings.Split(manifest.Content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "replace") {
			return out, errors.New("Go replacement modules require a separate provisioning profile")
		}
	}
	mods, e := parseGoSum(lock.Content)
	if e != nil {
		return out, e
	}
	out.PackageCount = len(mods)
	out.LockHash = lock.Hash
	if e = Require(ctx, approve, Approval{Tool: "setup_dependencies", Workspace: w.Path, Environment: "public Go proxy fetch + offline checksum verification", Arguments: map[string]any{"packageManager": "go", "lockHash": lock.Hash, "moduleVersions": len(mods), "registry": "https://proxy.golang.org", "maxDownloadBytes": 256 << 20}, Reason: "Fetch only public module files listed in go.sum, then let the installed Go tool verify checksums offline. No VCS subprocess fetches, replacement modules, source execution, or toolchain downloads."}); e != nil {
		return out, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	proxy := filepath.Join(d.dependencyCache(w.Path), "go-proxy")
	for _, m := range mods {
		base := escapeGoModule(m.Path) + "/@v/" + escapeGoModule(m.Version)
		extensions := []string{".mod"}
		if m.Zip {
			extensions = append(extensions, ".info", ".zip")
		}
		for _, ext := range extensions {
			b, err := boundedPublicFetch(ctx, "https://proxy.golang.org/"+base+ext, "proxy.golang.org", 32<<20)
			if err != nil {
				return out, err
			}
			out.DownloadedBytes += int64(len(b))
			if out.DownloadedBytes > 256<<20 {
				return out, errors.New("Go downloads exceed 256 MiB")
			}
			if err = privateDownloadFile(filepath.Join(proxy, filepath.FromSlash(escapeGoModule(m.Path)), "@v"), escapeGoModule(m.Version)+ext, b); err != nil {
				return out, err
			}
		}
	}
	now, e := w.Read("go.sum")
	if e != nil || now.Hash != lock.Hash {
		return out, workspace.ErrConflict
	}
	now, e = w.Read("go.mod")
	if e != nil || now.Hash != manifest.Hash {
		return out, workspace.ErrConflict
	}
	out.Command, e = d.Run(ctx, Request{Workspace: w.Path, Executable: "/usr/bin/env", Args: []string{"GOPROXY=file:///cache/go-proxy", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly", "/bin/sh", "-c", "go mod download all && go mod verify"}, ReadOnly: true, Timeout: 3 * time.Minute})
	if e == nil && out.Command.ExitCode != 0 {
		e = fmt.Errorf("offline Go checksum verification exited with code %d", out.Command.ExitCode)
	}
	return out, e
}
