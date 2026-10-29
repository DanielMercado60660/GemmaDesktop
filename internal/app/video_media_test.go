package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gemmadesktop.local/app/internal/media"
)

func TestVideoMediaCapabilityRangesAndGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.mp4")
	data := []byte("0123456789")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	identity := hex.EncodeToString(digest[:])
	access := newVideoMediaAccess()
	issued, err := access.issue(videoMediaBinding{workspaceID: "workspace", clipID: "clip", kind: "original", digest: identity}, "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := str(issued, "url")
	var openError error
	assetDigest := identity
	opens := 0
	handler := access.handler(func(binding videoMediaBinding) (media.Asset, error) {
		opens++
		if binding.workspaceID != "workspace" || binding.clipID != "clip" {
			t.Fatal("scope lost")
		}
		if openError != nil {
			return media.Asset{}, openError
		}
		file, err := os.Open(path)
		return media.Asset{File: file, Name: "source.mp4", ContentType: "video/mp4", SHA256: assetDigest, Size: int64(len(data))}, err
	})
	for _, test := range []struct {
		name, method, rangeHeader, body string
		status                          int
	}{
		{"full", "GET", "", "0123456789", 200},
		{"range", "GET", "bytes=2-5", "2345", 206},
		{"suffix", "GET", "bytes=-3", "789", 206},
		{"head", "HEAD", "", "", 200},
		{"invalid", "GET", "bytes=200-300", "", 416},
		{"method", "POST", "", "", 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://127.0.0.1:8787"+endpoint, nil)
			request.Header.Set("Range", test.rangeHeader)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if test.status < 400 && response.Body.String() != test.body {
				t.Fatalf("body %q", response.Body.String())
			}
			if test.status < 400 && (response.Header().Get("Content-Type") != "video/mp4" || response.Header().Get("ETag") != `"`+identity+`"`) {
				t.Fatalf("identity/type headers: %v", response.Header())
			}
			if test.name == "range" && response.Header().Get("Content-Range") != "bytes 2-5/10" {
				t.Fatal(response.Header())
			}
			if test.name == "head" && response.Header().Get("Content-Length") != "10" {
				t.Fatal(response.Header())
			}
		})
	}
	for _, test := range []struct {
		name, host, origin, site, suffix string
		status                           int
	}{
		{"foreign-origin", "127.0.0.1:8787", "https://attacker.example", "", "", 403},
		{"foreign-host", "attacker.example", "", "", "", 403},
		{"cross-site", "127.0.0.1:8787", "", "cross-site", "", 403},
		{"query", "127.0.0.1:8787", "", "", "?path=/etc/passwd", 404},
		{"native", "wails.localhost", "wails://wails.localhost", "same-origin", "", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://"+test.host+endpoint+test.suffix, nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Sec-Fetch-Site", test.site)
			before := opens
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status %d", response.Code)
			}
			if test.status >= 400 && opens != before {
				t.Fatal("rejected request opened media")
			}
		})
	}
	request := httptest.NewRequest("GET", "http://localhost"+endpoint, nil)
	assetDigest = strings.Repeat("0", 64)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatal("changed asset identity served")
	}
	assetDigest = identity
	openError = errors.New("workspace removed")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatal("removed workspace still accessible")
	}
	openError = nil
	access.now = func() time.Time { return time.Now().Add(time.Hour) }
	before := opens
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 404 || opens != before {
		t.Fatal("expired capability opened media")
	}
}

func TestVideoMediaBoundAndCancelledReads(t *testing.T) {
	access := newVideoMediaAccess()
	for i := 0; i < 140; i++ {
		if _, err := access.issue(videoMediaBinding{digest: strings.Repeat("a", 64)}, "image/jpeg"); err != nil {
			t.Fatal(err)
		}
	}
	if len(access.bindings) != 128 {
		t.Fatalf("unbounded capabilities: %d", len(access.bindings))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &contextMediaReader{ctx: ctx, ReadSeeker: bytes.NewReader([]byte("private media"))}
	if n, err := reader.Read(make([]byte, 20)); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %d %v", n, err)
	}
	issued, _ := access.issue(videoMediaBinding{digest: strings.Repeat("a", 64)}, "image/jpeg")
	for i := 0; i < cap(access.streams); i++ {
		access.streams <- struct{}{}
	}
	handler := access.handler(func(videoMediaBinding) (media.Asset, error) {
		t.Fatal("saturated stream opened file")
		return media.Asset{}, nil
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost"+str(issued, "url"), nil))
	if response.Code != 429 {
		t.Fatal(response.Code)
	}
}

func TestCompletionSelectionsRejectEmbeddingOnlyModels(t *testing.T) {
	c := &Core{models: []Doc{{"name": "custom-encoder", "capabilities": []string{"embedding", "vision"}}, {"name": "visual-chat", "capabilities": []any{"completion", "vision"}}}}
	for _, name := range []string{"custom-encoder", videoEmbeddingModel} {
		if c.checkCompletionSelection(name) == nil {
			t.Fatalf("accepted embedding selection %s", name)
		}
	}
	for _, name := range []string{"visual-chat", "legacy-local", ""} {
		if err := c.checkCompletionSelection(name); err != nil {
			t.Fatal(err)
		}
	}
}
