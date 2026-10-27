package execution

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EndpointClient binds requests and redirects to one explicitly approved origin.
// Only an explicitly selected numeric loopback endpoint may reach a private IP.
func EndpointClient(raw string) (*http.Client, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, nil, errors.New("valid endpoint without credentials or fragment required")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, nil, errors.New("use HTTPS, or an explicitly selected numeric loopback HTTP endpoint")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	approvedHost := u.Hostname()
	approvedPort := port
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 30 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, p, e := net.SplitHostPort(address)
		if e != nil || !strings.EqualFold(host, approvedHost) || p != approvedPort {
			return nil, errors.New("network destination differs from approved endpoint")
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, a := range ips {
			if !loopback && !PublicIP(a.IP) {
				return nil, errors.New("endpoint resolves to a prohibited private or special address")
			}
			if loopback && !a.IP.IsLoopback() {
				return nil, errors.New("loopback endpoint changed address")
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("endpoint has no address")
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), p))
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Scheme != u.Scheme || !strings.EqualFold(req.URL.Host, u.Host) || req.URL.User != nil {
			return errors.New("redirect outside approved endpoint is blocked")
		}
		return nil
	}}
	return client, u, nil
}

// PublicIP also rejects carrier NAT and documentation/reserved blocks that are
// not covered by net.IP.IsPrivate. DNS is resolved again at each new connection.
func PublicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96"} {
		_, n, _ := net.ParseCIDR(raw)
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// BoundJSONResponses limits non-streaming MCP responses before SDK decoding.
// SSE has its own per-event bound in the official SDK.
func BoundJSONResponses(client *http.Client, limit int64) {
	client.Transport = boundedJSONTransport{base: client.Transport, limit: limit}
}

type boundedJSONTransport struct {
	base  http.RoundTripper
	limit int64
}

func (t boundedJSONTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "application/json") {
		res.Body = &boundedBody{body: res.Body, remaining: t.limit}
	}
	return res, nil
}

type boundedBody struct {
	body      io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		var extra [1]byte
		n, err := b.body.Read(extra[:])
		if n > 0 {
			return 0, errors.New("response exceeded size limit")
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.body.Read(p)
	b.remaining -= int64(n)
	return n, err
}
func (b *boundedBody) Close() error { return b.body.Close() }
