package browser

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gemmadesktop.local/app/internal/execution"
)

// originProxy permits only the current approved origin. Resolution and dialing
// are pinned by EndpointClient; inherited system proxies are never consulted.
type originProxy struct {
	mu          sync.Mutex
	origin      *url.URL
	transport   *http.Transport
	connections map[net.Conn]bool
	server      *http.Server
	listener    net.Listener
	generation  context.Context
	revoke      context.CancelFunc
}

func newOriginProxy() (*originProxy, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &originProxy{connections: map[net.Conn]bool{}, listener: l}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10}
	go func() { _ = p.server.Serve(l) }()
	return p, nil
}
func (p *originProxy) URL() string { return "http://" + p.listener.Addr().String() }
func (p *originProxy) approve(raw string) error {
	c, u, err := execution.EndpointClient(raw)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.revoke != nil {
		p.revoke()
	}
	p.generation, p.revoke = context.WithCancel(context.Background())
	for c := range p.connections {
		_ = c.Close()
		delete(p.connections, c)
	}
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
	p.origin = u
	p.transport = c.Transport.(*http.Transport)
	return nil
}
func endpointPort(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
func (p *originProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	u, tr, generation := p.origin, p.transport, p.generation
	p.mu.Unlock()
	if u == nil {
		http.Error(w, "No browser origin has been approved", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		host, port, err := net.SplitHostPort(r.Host)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(host, u.Hostname()) || port != endpointPort(u) {
			http.Error(w, "Origin is not approved", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		up, err := tr.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
		if err != nil {
			http.Error(w, "Destination was blocked or unavailable", http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			up.Close()
			http.Error(w, "Tunnel unavailable", http.StatusInternalServerError)
			return
		}
		down, buffer, err := hj.Hijack()
		if err != nil {
			up.Close()
			return
		}
		p.mu.Lock()
		if p.origin != u {
			p.mu.Unlock()
			up.Close()
			down.Close()
			return
		}
		p.connections[up] = true
		p.connections[down] = true
		p.mu.Unlock()
		defer func() {
			up.Close()
			down.Close()
			p.mu.Lock()
			delete(p.connections, up)
			delete(p.connections, down)
			p.mu.Unlock()
		}()
		_ = up.SetDeadline(time.Now().Add(3 * time.Minute))
		_ = down.SetDeadline(time.Now().Add(3 * time.Minute))
		if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err = buffer.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { _, _ = io.Copy(up, buffer); _ = up.Close(); close(done) }()
		_, _ = io.Copy(down, up)
		_ = down.Close()
		<-done
		return
	}
	if r.URL.Scheme != u.Scheme || !strings.EqualFold(r.URL.Host, u.Host) || !strings.EqualFold(r.Host, u.Host) || r.URL.User != nil {
		http.Error(w, "Origin is not approved", http.StatusForbidden)
		return
	}
	requestCtx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(generation, cancel)
	defer func() { stop(); cancel() }()
	request := r.Clone(requestCtx)
	request.RequestURI = ""
	request.Header.Del("Proxy-Authorization")
	request.Header.Del("Proxy-Connection")
	res, err := tr.RoundTrip(request)
	if err != nil {
		http.Error(w, "Destination was blocked or unavailable", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	// Cross-origin redirects never reach a second destination; its proxy request
	// will be denied. Browser code also verifies the resulting displayed origin.
	for k, values := range res.Header {
		if strings.EqualFold(k, "Connection") || strings.EqualFold(k, "Transfer-Encoding") {
			continue
		}
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}
func (p *originProxy) Close() error {
	p.mu.Lock()
	if p.revoke != nil {
		p.revoke()
	}
	for c := range p.connections {
		_ = c.Close()
	}
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
	p.mu.Unlock()
	err := p.server.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
