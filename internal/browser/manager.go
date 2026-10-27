// Package browser owns a fresh Chromium profile, never the user's normal tabs.
package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gemmadesktop.local/app/internal/execution"
	"github.com/chromedp/chromedp"
)

type Options struct {
	ChromePath  string
	ArtifactDir string
	Approve     execution.Authorizer
}
type Page struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Links     []Link `json:"links"`
}
type Link struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}
type Screenshot struct {
	Path      string `json:"path"`
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	URL       string `json:"url"`
}
type Manager struct {
	mu              sync.Mutex
	options         Options
	ctx             context.Context
	cancel          context.CancelFunc
	allocatorCancel context.CancelFunc
	profile         string
	proxy           *originProxy
	approved        *url.URL
	closed          bool
}

func NewManager(options Options) *Manager { return &Manager{options: options} }

func (m *Manager) ensure(ctx context.Context) error {
	if m.closed {
		return errors.New("browser is closed")
	}
	if m.ctx != nil {
		return nil
	}
	profile, err := os.MkdirTemp("", "gemma-browser-")
	if err != nil {
		return err
	}
	m.profile = profile
	p, err := newOriginProxy()
	if err != nil {
		os.RemoveAll(profile)
		return err
	}
	m.proxy = p
	opts := []chromedp.ExecAllocatorOption{chromedp.Headless, chromedp.NoFirstRun, chromedp.NoDefaultBrowserCheck, chromedp.UserDataDir(profile), chromedp.NoInheritEnv, chromedp.Env("PATH="+execution.SafePATH, "HOME="+profile, "TMPDIR="+profile, "LANG=en_US.UTF-8"), chromedp.WindowSize(1280, 800), chromedp.ProxyServer(p.URL()), chromedp.Flag("proxy-bypass-list", "<-loopback>"), chromedp.Flag("disable-quic", true), chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"), chromedp.Flag("disable-background-networking", true), chromedp.Flag("disable-extensions", true), chromedp.Flag("disable-sync", true), chromedp.Flag("disable-default-apps", true), chromedp.Flag("use-mock-keychain", true), chromedp.Flag("password-store", "basic"), chromedp.Flag("disable-features", "AutofillServerCommunication,MediaRouter")}
	if m.options.ChromePath != "" {
		opts = append(opts, chromedp.ExecPath(m.options.ChromePath))
	}
	allocator, acancel := chromedp.NewExecAllocator(context.Background(), opts...)
	m.allocatorCancel = acancel
	m.ctx, m.cancel = chromedp.NewContext(allocator)
	// The initial allocator uses the context passed to Do as its process
	// lifetime. Initialize with our owned context, then use bounded children for
	// later actions; cancelling a completed startup action must not kill Chrome.
	stop := context.AfterFunc(ctx, m.cancel)
	timer := time.AfterFunc(40*time.Second, m.cancel)
	err = chromedp.Do(m.ctx)
	stop()
	timer.Stop()
	if err != nil {
		m.cancel()
		acancel()
		p.Close()
		os.RemoveAll(profile)
		m.ctx = nil
		return fmt.Errorf("isolated Chrome could not start: %w", err)
	}
	return nil
}
func (m *Manager) operationContext(user context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(m.ctx, 40*time.Second)
	stop := context.AfterFunc(user, cancel)
	return ctx, func() { stop(); cancel() }
}
func (m *Manager) require(ctx context.Context, tool string, args map[string]any, reason string) error {
	return execution.Require(ctx, m.options.Approve, execution.Approval{Tool: tool, Arguments: args, Reason: reason, Environment: "isolated Chrome profile; only the approved origin is proxied"})
}

func (m *Manager) Navigate(ctx context.Context, raw string) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(raw) > 8192 {
		return Page{}, errors.New("URL exceeds limit")
	}
	_, u, err := execution.EndpointClient(raw)
	if err != nil {
		return Page{}, err
	}
	if err = m.require(ctx, "browser.navigate", map[string]any{"url": raw}, "Allow navigation and page requests to this origin. Other origins, including cross-origin redirects, remain blocked."); err != nil {
		return Page{}, err
	}
	if err = m.ensure(ctx); err != nil {
		return Page{}, err
	}
	if err = m.proxy.approve(raw); err != nil {
		return Page{}, err
	}
	m.approved = u
	op, cancel := m.operationContext(ctx)
	defer cancel()
	if err = chromedp.Do(op, chromedp.Navigate(raw)); err != nil {
		return Page{}, err
	}
	return m.read(op)
}
func (m *Manager) read(ctx context.Context) (Page, error) {
	if m.ctx == nil || m.approved == nil {
		return Page{}, errors.New("navigate to an approved page first")
	}
	page, err := chromedp.Run(ctx, chromedp.Evaluate[Page](`(()=>{const t=document.body?.innerText||"";return {url:location.href,title:document.title.slice(0,1000),text:t.slice(0,65536),truncated:t.length>65536,links:Array.from(document.querySelectorAll('a[href]')).slice(0,100).map(a=>({text:(a.innerText||"").slice(0,200),url:a.href.slice(0,8192)}))}})()`))
	if err != nil {
		return Page{}, err
	}
	u, err := url.Parse(page.URL)
	if err != nil || u.Scheme != m.approved.Scheme || !strings.EqualFold(u.Host, m.approved.Host) {
		return Page{}, errors.New("page left the approved origin; its content is unavailable")
	}
	return page, nil
}
func (m *Manager) Read(ctx context.Context) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return Page{}, errors.New("navigate to an approved page first")
	}
	op, cancel := m.operationContext(ctx)
	defer cancel()
	return m.read(op)
}
func (m *Manager) Click(ctx context.Context, selector string) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil || m.approved == nil {
		return Page{}, errors.New("navigate first")
	}
	if selector == "" || len(selector) > 1000 {
		return Page{}, errors.New("valid bounded CSS selector required")
	}
	if err := m.require(ctx, "browser.click", map[string]any{"url": m.approved.String(), "selector": selector}, "Click this page element. This can submit a form or perform a remote write."); err != nil {
		return Page{}, err
	}
	op, cancel := m.operationContext(ctx)
	defer cancel()
	if _, err := m.read(op); err != nil {
		return Page{}, err
	}
	if err := chromedp.Do(op, chromedp.Click(chromedp.CSS(selector))); err != nil {
		return Page{}, err
	}
	return m.read(op)
}
func (m *Manager) Type(ctx context.Context, selector, text string) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil || m.approved == nil {
		return Page{}, errors.New("navigate first")
	}
	if selector == "" || len(selector) > 1000 || len(text) > 16<<10 {
		return Page{}, errors.New("selector or text exceeds limit")
	}
	if err := m.require(ctx, "browser.type", map[string]any{"url": m.approved.String(), "selector": selector, "text": text}, "Type this text into the page. Input events may send it to the approved origin."); err != nil {
		return Page{}, err
	}
	op, cancel := m.operationContext(ctx)
	defer cancel()
	if _, err := m.read(op); err != nil {
		return Page{}, err
	}
	encoded, _ := json.Marshal(selector)
	allowed, err := chromedp.Run(op, chromedp.Evaluate[bool](`(()=>{const e=document.querySelector(`+string(encoded)+`);return !!e&&!e.disabled&&!e.readOnly&&e.getClientRects().length>0&&getComputedStyle(e).visibility!=='hidden'&&(e.tagName==='TEXTAREA'||(e.tagName==='INPUT'&&['text','search','email','tel','url','number'].includes(e.type)))})()`))
	if err != nil {
		return Page{}, err
	}
	if !allowed {
		return Page{}, errors.New("only visible text input and textarea elements are supported; credential and upload fields are unavailable")
	}
	if err = chromedp.Do(op, chromedp.SetValue(chromedp.CSS(selector), text)); err != nil {
		return Page{}, err
	}
	return m.read(op)
}
func (m *Manager) Screenshot(ctx context.Context) (Screenshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil || m.options.ArtifactDir == "" {
		return Screenshot{}, errors.New("browser screenshot artifact directory is unavailable")
	}
	op, cancel := m.operationContext(ctx)
	defer cancel()
	page, err := m.read(op)
	if err != nil {
		return Screenshot{}, err
	}
	data, err := chromedp.Run(op, chromedp.CaptureScreenshot())
	if err != nil {
		return Screenshot{}, err
	}
	if len(data) > 10<<20 {
		return Screenshot{}, errors.New("screenshot exceeds size limit")
	}
	if err = os.MkdirAll(m.options.ArtifactDir, 0700); err != nil {
		return Screenshot{}, err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Screenshot{}, err
	}
	path := filepath.Join(m.options.ArtifactDir, "browser-"+hex.EncodeToString(nonce[:])+".png")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Screenshot{}, err
	}
	_, err = f.Write(data)
	ce := f.Close()
	if err != nil {
		os.Remove(path)
		return Screenshot{}, err
	}
	if ce != nil {
		os.Remove(path)
		return Screenshot{}, ce
	}
	return Screenshot{Path: path, MediaType: "image/png", Size: len(data), URL: page.URL}, nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	if m.allocatorCancel != nil {
		m.allocatorCancel()
	}
	if m.proxy != nil {
		_ = m.proxy.Close()
	}
	if m.profile != "" {
		return os.RemoveAll(m.profile)
	}
	return nil
}
