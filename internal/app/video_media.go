package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gemmadesktop.local/app/internal/media"
)

type videoMediaBinding struct {
	workspaceID, clipID, kind, frameID, digest string
	expires                                    time.Time
}

type videoMediaAccess struct {
	mu       sync.Mutex
	bindings map[string]videoMediaBinding
	streams  chan struct{}
	now      func() time.Time
}

func newVideoMediaAccess() *videoMediaAccess {
	return &videoMediaAccess{bindings: map[string]videoMediaBinding{}, streams: make(chan struct{}, 4), now: time.Now}
}

func (c *Core) issueVideoMedia(workspaceID, clipID, kind, frameID string) (any, error) {
	asset, err := c.videos.OpenMedia(workspaceID, clipID, kind, frameID)
	if err != nil {
		return nil, err
	}
	defer asset.File.Close()
	return c.videoAccess.issue(videoMediaBinding{workspaceID: workspaceID, clipID: clipID, kind: kind, frameID: frameID, digest: asset.SHA256}, asset.ContentType)
}

func (a *videoMediaAccess) issue(binding videoMediaBinding, contentType string) (Doc, error) {
	if contentType != "video/mp4" && contentType != "image/jpeg" {
		return nil, errors.New("Unsupported media type")
	}
	if raw, err := hex.DecodeString(binding.digest); err != nil || len(raw) != 32 {
		return nil, errors.New("Invalid media identity")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(random[:])
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for key, item := range a.bindings {
		if !now.Before(item.expires) {
			delete(a.bindings, key)
		}
	}
	if len(a.bindings) >= 128 {
		var oldest string
		for key, item := range a.bindings {
			if oldest == "" || item.expires.Before(a.bindings[oldest].expires) {
				oldest = key
			}
		}
		delete(a.bindings, oldest)
	}
	binding.expires = now.Add(30 * time.Minute)
	a.bindings[token] = binding
	return Doc{"url": "/api/media/" + token, "mediaType": contentType, "expiresAt": binding.expires.UTC().Format(time.RFC3339)}, nil
}

// VideoMediaHandler exposes only previously authorized imported assets. URLs
// contain opaque short-lived capabilities, never workspace or filesystem paths.
func (c *Core) VideoMediaHandler() http.Handler {
	return c.videoAccess.handler(func(binding videoMediaBinding) (media.Asset, error) {
		if _, err := c.Store.Get("workspace", binding.workspaceID); err != nil {
			return media.Asset{}, err
		}
		return c.videos.OpenMedia(binding.workspaceID, binding.clipID, binding.kind, binding.frameID)
	})
}

func (a *videoMediaAccess) handler(open func(videoMediaBinding) (media.Asset, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !localMediaRequest(r) {
			http.Error(w, "Origin rejected", http.StatusForbidden)
			return
		}
		if r.URL.RawQuery != "" || !strings.HasPrefix(r.URL.Path, "/api/media/") {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimPrefix(r.URL.Path, "/api/media/")
		decoded, err := hex.DecodeString(token)
		if err != nil || len(decoded) != 32 || len(token) != 64 {
			http.NotFound(w, r)
			return
		}
		a.mu.Lock()
		binding, found := a.bindings[token]
		if found && !a.now().Before(binding.expires) {
			delete(a.bindings, token)
			found = false
		}
		a.mu.Unlock()
		if !found {
			http.NotFound(w, r)
			return
		}
		select {
		case a.streams <- struct{}{}:
			defer func() { <-a.streams }()
		case <-r.Context().Done():
			return
		default:
			http.Error(w, "Too many active media streams", http.StatusTooManyRequests)
			return
		}
		asset, err := open(binding)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if asset.File == nil {
			http.NotFound(w, r)
			return
		}
		defer asset.File.Close()
		info, err := asset.File.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != asset.Size || asset.Size < 1 || asset.Size > 256<<20 || asset.SHA256 != binding.digest {
			http.NotFound(w, r)
			return
		}
		if asset.ContentType != "video/mp4" && asset.ContentType != "image/jpeg" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", asset.ContentType)
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("ETag", `"`+binding.digest+`"`)
		http.ServeContent(w, r, asset.Name, time.Time{}, &contextMediaReader{ctx: r.Context(), ReadSeeker: asset.File})
	})
}

func localMediaRequest(r *http.Request) bool {
	host := r.Host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if host != "localhost" && host != "wails.localhost" && (ip == nil || !ip.IsLoopback()) {
		return false
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "wails") || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
	}
	return true
}

type contextMediaReader struct {
	ctx context.Context
	io.ReadSeeker
}

func (reader *contextMediaReader) Read(p []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.ReadSeeker.Read(p)
}
