package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(b)
}

func verifyFile(ctx context.Context, path string, size int64, digest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("artifact size/type differs from manifest")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, contextReader{ctx, f})
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("artifact SHA-256 differs from manifest")
	}
	return nil
}

func (m *Manager) sourceReader(ctx context.Context, source Source, offset int64) (io.ReadCloser, bool, error) {
	if source.BundledPath != "" {
		if m.config.BundlesDir == "" {
			return nil, false, errors.New("runtime bundle resources are unavailable")
		}
		root, err := filepath.EvalSymlinks(m.config.BundlesDir)
		if err != nil {
			return nil, false, err
		}
		candidate, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(source.BundledPath)))
		if err != nil {
			return nil, false, err
		}
		rel, err := filepath.Rel(root, candidate)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, false, errors.New("bundled artifact escaped Resources")
		}
		f, err := os.Open(candidate)
		if err != nil {
			return nil, false, err
		}
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return nil, false, err
		}
		return f, true, nil
	}
	u, _ := url.Parse(source.URL)
	allowed := map[string]bool{strings.ToLower(u.Hostname()): true}
	for _, host := range source.RedirectHosts {
		allowed[host] = true
	}
	client := http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 30 * time.Second}}
	if m.config.HTTPClient != nil {
		client = *m.config.HTTPClient
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 8 || req.URL.Scheme != "https" || req.URL.User != nil || !allowed[strings.ToLower(req.URL.Hostname())] {
			return errors.New("artifact redirect is outside the trusted manifest allowlist")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, false, fmt.Errorf("artifact download HTTP %d", resp.StatusCode)
	}
	resumed := resp.StatusCode == http.StatusPartialContent
	if resumed {
		var begin, end, total int64
		if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &begin, &end, &total); err != nil || begin != offset || total != source.Size || end != source.Size-1 {
			resp.Body.Close()
			return nil, false, errors.New("artifact resume range differs from manifest")
		}
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		resp.Body.Close()
		return nil, false, errors.New("artifact response must not transform encoded bytes")
	}
	return resp.Body, resumed, nil
}

func (m *Manager) download(ctx context.Context, source Source, progress func(int64)) (string, error) {
	path := filepath.Join(m.root, "downloads", source.SHA256)
	if verifyFile(ctx, path, source.Size, source.SHA256) == nil {
		if progress != nil {
			progress(source.Size)
		}
		return path, nil
	}
	partial := path + ".part"
	var offset int64
	if info, err := os.Lstat(partial); err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("download staging path is not a regular file")
		}
		offset = info.Size()
		if offset >= source.Size {
			_ = os.Remove(partial)
			offset = 0
		}
	}
	reader, resumed, err := m.sourceReader(ctx, source, offset)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	if !resumed {
		offset = 0
	}
	flags := os.O_WRONLY | os.O_CREATE
	if offset == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(partial, flags, 0600)
	if err != nil {
		return "", err
	}
	complete := offset
	buffer := make([]byte, 256<<10)
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = reader.Read(buffer)
		if n > 0 {
			if complete+int64(n) > source.Size {
				err = errors.New("download exceeds manifest size")
				break
			}
			var written int
			written, err = f.Write(buffer[:n])
			complete += int64(written)
			if progress != nil {
				progress(complete)
			}
			if err != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	if err == io.EOF {
		err = nil
	}
	if syncErr := f.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if complete != source.Size {
		return "", fmt.Errorf("artifact ended at %s of %s bytes", strconv.FormatInt(complete, 10), strconv.FormatInt(source.Size, 10))
	}
	if err = verifyFile(ctx, partial, source.Size, source.SHA256); err != nil {
		_ = os.Remove(partial)
		return "", err
	}
	if err = os.Rename(partial, path); err != nil {
		return "", err
	}
	return path, nil
}
