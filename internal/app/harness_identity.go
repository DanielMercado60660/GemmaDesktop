package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var harnessBinaryOnce sync.Once
var harnessBinaryHash string

func harnessBinaryIdentity() string {
	harnessBinaryOnce.Do(func() {
		path, e := os.Executable()
		if e != nil {
			return
		}
		f, e := os.Open(path)
		if e != nil {
			return
		}
		defer f.Close()
		h := sha256.New()
		if _, e = io.Copy(h, f); e == nil {
			harnessBinaryHash = "sha256:" + hex.EncodeToString(h.Sum(nil))
		}
	})
	return harnessBinaryHash
}

func harnessAdapterIdentity() (string, error) {
	script, e := harnessAdapterPath()
	if e != nil {
		return "", e
	}
	body, e := os.ReadFile(script)
	if e != nil {
		return "", e
	}
	manifest, e := os.ReadFile(filepath.Join(filepath.Dir(script), "..", "..", "third_party", "harness2", "UPSTREAM.json"))
	if e != nil {
		return "", e
	}
	digest := sha256.Sum256(append(body, manifest...))
	return hex.EncodeToString(digest[:]), nil
}

func harnessPythonIdentity() (string, string, error) {
	candidates := []string{}
	for _, name := range []string{"python3.12", "python3.13"} {
		if p, e := exec.LookPath(name); e == nil {
			candidates = append(candidates, p)
		}
	}
	for _, root := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/Library/Frameworks/Python.framework/Versions/3.12/bin", "/Library/Frameworks/Python.framework/Versions/3.13/bin"} {
		for _, name := range []string{"python3.12", "python3.13"} {
			candidates = append(candidates, filepath.Join(root, name))
		}
	}
	for _, path := range candidates {
		real, e := filepath.EvalSymlinks(path)
		if e != nil {
			continue
		}
		info, e := os.Stat(real)
		if e != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, e := exec.CommandContext(ctx, real, "-I", "-B", "-c", "import sys; assert sys.version_info[:2] in [(3,12),(3,13)]; print(sys.version)").Output()
		cancel()
		if e != nil {
			continue
		}
		f, e := os.Open(real)
		if e != nil {
			continue
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			continue
		}
		return real, fmt.Sprintf("sha256:%x version:%s", h.Sum(nil), out), nil
	}
	return "", "", fmt.Errorf("upstream comparison requires an existing Python 3.12 or 3.13 interpreter")
}
