package execution

import (
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gemmadesktop.local/app/internal/workspace"
)

type DependencyRequest struct {
	Workspace      string `json:"workspace"`
	PackageManager string `json:"packageManager"`
}
type DependencyResult struct {
	PackageManager  string `json:"packageManager"`
	PackageCount    int    `json:"packageCount"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	LockHash        string `json:"lockHash"`
	Command         Result `json:"command"`
}
type npmPackage struct {
	Resolved  string `json:"resolved"`
	Integrity string `json:"integrity"`
	Link      bool   `json:"link"`
}
type npmLock struct {
	LockfileVersion int                   `json:"lockfileVersion"`
	Packages        map[string]npmPackage `json:"packages"`
}

func parseNPMLock(b []byte) ([]npmPackage, error) {
	var lock npmLock
	if e := json.Unmarshal(b, &lock); e != nil {
		return nil, e
	}
	if lock.LockfileVersion != 2 && lock.LockfileVersion != 3 {
		return nil, errors.New("dependency setup requires package-lock v2 or v3")
	}
	if len(lock.Packages) > 201 {
		return nil, errors.New("dependency setup exceeds 200 locked package entries")
	}
	keys := []string{}
	for k := range lock.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []npmPackage{}
	seen := map[string]string{}
	for _, k := range keys {
		p := lock.Packages[k]
		if k == "" {
			continue
		}
		if p.Link {
			return nil, errors.New("linked/workspace dependencies require a later setup profile")
		}
		u, e := url.Parse(p.Resolved)
		if e != nil || u.Scheme != "https" || u.Host != "registry.npmjs.org" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, ".tgz") {
			return nil, fmt.Errorf("package %q is not an approved public npm tarball", k)
		}
		if !strings.HasPrefix(p.Integrity, "sha512-") {
			return nil, fmt.Errorf("package %q needs SHA-512 integrity", k)
		}
		sum, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(p.Integrity, "sha512-"))
		if e != nil || len(sum) != 64 {
			return nil, errors.New("invalid locked SHA-512 integrity")
		}
		if old, ok := seen[p.Resolved]; ok {
			if old != p.Integrity {
				return nil, errors.New("conflicting package integrity")
			}
			continue
		}
		seen[p.Resolved] = p.Integrity
		out = append(out, p)
	}
	return out, nil
}

// ProvisionDependencies grants no guest network. Only integrity-pinned public
// npm tarballs are fetched by the host; npm installation then runs offline with
// lifecycle scripts disabled and source genuinely read-only.
func (d *DockerExecutor) ProvisionDependencies(ctx context.Context, req DependencyRequest, approve Authorizer) (DependencyResult, error) {
	if req.PackageManager == "pip" {
		return d.provisionPip(ctx, req, approve)
	}
	if req.PackageManager == "go" {
		return d.provisionGo(ctx, req, approve)
	}
	out := DependencyResult{PackageManager: req.PackageManager, Command: Result{ExitCode: -1, Environment: "dependency-setup", Publication: "not_applied"}}
	if req.PackageManager != "npm" {
		return out, errors.New("only integrity-locked public npm setup is implemented; pip/Go/private registries require a separate supported provisioning profile")
	}
	w, e := workspace.Open(req.Workspace)
	if e != nil {
		return out, e
	}
	defer w.Close()
	req.Workspace = w.Path
	ctx, release, e := workspace.AcquireOperation(ctx, w.Path)
	if e != nil {
		return out, e
	}
	defer release()
	if e = d.dependencyAdmission(ctx, w.Path); e != nil {
		return out, e
	}
	lock, e := w.Read("package-lock.json")
	if e != nil {
		return out, e
	}
	manifest, e := w.Read("package.json")
	if e != nil {
		return out, e
	}
	packages, e := parseNPMLock([]byte(lock.Content))
	if e != nil {
		return out, e
	}
	out.LockHash = lock.Hash
	out.PackageCount = len(packages)
	if e = Require(ctx, approve, Approval{Tool: "setup_dependencies", Workspace: w.Path, Environment: "host public npm fetch + offline Docker install", Arguments: map[string]any{"packageManager": "npm", "lockHash": lock.Hash, "packageCount": len(packages), "registry": "https://registry.npmjs.org", "maxDownloadBytes": 256 << 20, "lifecycleScripts": false}, Reason: "Download these integrity-locked public packages, then install offline into private node_modules. Source remains read-only; no credentials, private registries, or package lifecycle scripts are allowed."}); e != nil {
		return out, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	client, _, e := EndpointClient("https://registry.npmjs.org")
	if e != nil {
		return out, e
	}
	defer client.CloseIdleConnections()
	client.Timeout = 45 * time.Second
	cache := filepath.Join(d.config.StateDir, "cache", workspace.Hash([]byte(w.Path + d.config.Image))[:32], "downloads")
	if e = ensureExecutionDir(cache); e != nil {
		return out, e
	}
	args := []string{"-c", "for f do npm cache add --offline --ignore-scripts --no-audit --no-fund \"$f\" || exit $?; done; npm ci --offline --ignore-scripts --no-audit --no-fund", "gemma-dependencies"}
	for _, p := range packages {
		file := workspace.Hash([]byte(p.Resolved+p.Integrity)) + ".tgz"
		destination := filepath.Join(cache, file)
		// Treat cached downloads as untrusted too; no symlinks, and verify integrity.
		var b []byte
		if inf, err := os.Lstat(destination); err == nil && inf.Mode().IsRegular() && inf.Size() <= 32<<20 {
			b, _ = os.ReadFile(destination)
		}
		if !validNPMIntegrity(b, p.Integrity) {
			hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Resolved, nil)
			if err != nil {
				return out, err
			}
			response, err := client.Do(hreq)
			if err != nil {
				return out, err
			}
			if response.StatusCode != 200 {
				response.Body.Close()
				return out, fmt.Errorf("npm tarball HTTP %d", response.StatusCode)
			}
			b, err = io.ReadAll(io.LimitReader(response.Body, (32<<20)+1))
			response.Body.Close()
			if err != nil {
				return out, err
			}
			if len(b) > 32<<20 {
				return out, errors.New("npm tarball exceeds 32 MiB")
			}
			out.DownloadedBytes += int64(len(b))
			if out.DownloadedBytes > 256<<20 {
				return out, errors.New("dependency download exceeds 256 MiB")
			}
			if !validNPMIntegrity(b, p.Integrity) {
				return out, errors.New("npm tarball integrity mismatch")
			}
			// Replace atomically so an interrupted download is never a valid cache entry.
			f, err := os.CreateTemp(cache, "download-")
			if err != nil {
				return out, err
			}
			tmp := f.Name()
			_, err = f.Write(b)
			if err == nil {
				err = f.Sync()
			}
			ce := f.Close()
			if err == nil {
				err = ce
			}
			if err == nil {
				err = os.Rename(tmp, destination)
			}
			os.Remove(tmp)
			if err != nil {
				return out, err
			}
		}
		args = append(args, "/cache/downloads/"+file)
	}
	// External editors are not held by the app's lease.
	now, e := w.Read("package-lock.json")
	if e != nil || now.Hash != lock.Hash {
		return out, workspace.ErrConflict
	}
	now, e = w.Read("package.json")
	if e != nil || now.Hash != manifest.Hash {
		return out, workspace.ErrConflict
	}
	out.Command, e = d.Run(ctx, Request{Workspace: w.Path, Executable: "/bin/sh", Args: args, ReadOnly: true, Timeout: 3 * time.Minute})
	if e == nil && out.Command.ExitCode != 0 {
		e = fmt.Errorf("offline npm installation exited with code %d", out.Command.ExitCode)
	}
	return out, e
}
func validNPMIntegrity(b []byte, integrity string) bool {
	want, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(integrity, "sha512-"))
	if e != nil || len(want) != 64 {
		return false
	}
	actual := sha512.Sum512(b)
	return subtle.ConstantTimeCompare(actual[:], want) == 1
}
