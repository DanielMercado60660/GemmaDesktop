package browser

import (
	"context"
	"errors"
	"gemmadesktop.local/app/internal/execution"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProxyOriginBoundary(t *testing.T) {
	var first, second atomic.Int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { second.Add(1); w.Write([]byte("other")) }))
	defer b.Close()
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, b.URL, 302)
			return
		}
		w.Write([]byte("approved"))
	}))
	defer a.Close()
	p, err := newOriginProxy()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err = p.approve(a.URL); err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse(p.URL())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
	res, err := client.Get(a.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || first.Load() != 1 {
		t.Fatal("approved origin failed")
	}
	res, err = client.Get(b.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 || second.Load() != 0 {
		t.Fatal("unapproved origin reached")
	}
	res, err = client.Get(a.URL + "/redirect")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 || second.Load() != 0 {
		t.Fatal("redirect reached different origin")
	}
}
func TestBrowserFailsClosedWithoutApproval(t *testing.T) {
	m := NewManager(Options{})
	defer m.Close()
	if _, err := m.Navigate(context.Background(), "https://example.com"); !errors.Is(err, execution.ErrDenied) {
		t.Fatal(err)
	}
	if m.ctx != nil {
		t.Fatal("browser started before approval")
	}
}
func TestLiveChrome(t *testing.T) {
	if os.Getenv("GEMMA_TEST_BROWSER") != "1" {
		t.Skip("set GEMMA_TEST_BROWSER=1 for real isolated Chrome qualification")
	}
	var outsideCalls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { outsideCalls.Add(1); w.Write([]byte("outside")) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, 302)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Gemma browser fixture</title></head><body><h1>Gemma browser proof</h1><input id="value"><input id="secret" type="password"><button id="button" onclick="document.getElementById('result').textContent='clicked'">Change</button><p id="result">before</p></body></html>`))
	}))
	defer server.Close()
	approved := 0
	m := NewManager(Options{ArtifactDir: t.TempDir(), ChromePath: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", Approve: func(context.Context, execution.Approval) error { approved++; return nil }})
	defer m.Close()
	p, err := m.Navigate(context.Background(), server.URL)
	if err != nil || p.Title != "Gemma browser fixture" || !strings.Contains(p.Text, "Gemma browser proof") {
		t.Fatalf("navigate %+v %v", p, err)
	}
	if _, err = m.Type(context.Background(), "#value", "typed"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Type(context.Background(), "#secret", "no"); err == nil {
		t.Fatal("password input allowed")
	}
	p, err = m.Click(context.Background(), "#button")
	if err != nil || !strings.Contains(p.Text, "clicked") {
		t.Fatalf("click %+v %v", p, err)
	}
	shot, err := m.Screenshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(shot.Path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(file)
	file.Close()
	if err != nil || cfg.Width < 500 || shot.Size < 1000 {
		t.Fatalf("PNG %+v %+v %v", shot, cfg, err)
	}
	if _, err = m.Navigate(context.Background(), server.URL+"/redirect"); err == nil {
		t.Fatal("cross-origin redirect should fail")
	}
	if outsideCalls.Load() != 0 {
		t.Fatal("unapproved origin reached by Chrome")
	}
	if approved < 4 {
		t.Fatal("missing approvals")
	}
	profile := m.profile
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("browser profile leaked")
	}
}
