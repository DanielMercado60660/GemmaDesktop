package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/execution"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type OAuthConfig struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorizationEndpoint"`
	TokenEndpoint         string   `json:"tokenEndpoint"`
	ClientID              string   `json:"clientId"`
	Scopes                []string `json:"scopes"`
}
type OAuthStatus struct {
	ServerID string `json:"serverId"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	Path     string `json:"path,omitempty"`
}
type SecretStore interface {
	Get(context.Context, string) ([]byte, error)
	Put(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

var ErrNoCredential = errors.New("no saved OAuth credential")

type OAuthOptions struct {
	Secrets       SecretStore
	OpenURL       func(string) error
	AllowLoopback bool
}
type tokenRecord struct {
	Path       string    `json:"path,omitempty"`
	ConfigHash string    `json:"configHash"`
	Resource   string    `json:"resource"`
	Access     string    `json:"access"`
	Refresh    string    `json:"refresh"`
	Expires    time.Time `json:"expires"`
}
type oauthPending struct {
	cancel context.CancelFunc
	status OAuthStatus
}
type OAuthManager struct {
	mu      sync.Mutex
	options OAuthOptions
	pending map[string]*oauthPending
	life    context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewOAuthManager(o OAuthOptions) *OAuthManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &OAuthManager{options: o, pending: map[string]*oauthPending{}, life: ctx, cancel: cancel}
}
func ValidateOAuthConfig(c OAuthConfig) error { return validateOAuth(c, false) }
func validateOAuth(c OAuthConfig, loopback bool) error {
	issuer, e := url.Parse(c.Issuer)
	if e != nil || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return errors.New("OAuth issuer must be an explicit HTTPS URL")
	}
	if c.ClientID == "" || len(c.ClientID) > 2048 || len(c.Scopes) > 32 {
		return errors.New("OAuth public client ID and at most 32 scopes required")
	}
	for _, scope := range c.Scopes {
		if scope == "" || len(scope) > 256 || strings.ContainsAny(scope, " \r\n\t") {
			return errors.New("OAuth scopes must be individual bounded tokens")
		}
	}
	for _, raw := range []string{c.Issuer, c.AuthorizationEndpoint, c.TokenEndpoint} {
		u, e := url.Parse(raw)
		if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Scheme != issuer.Scheme || !strings.EqualFold(u.Host, issuer.Host) {
			return errors.New("OAuth endpoints must be explicit URLs at the issuer origin without query, fragment or credentials")
		}
		if u.Scheme != "https" {
			ip := net.ParseIP(u.Hostname())
			if !loopback || u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
				return errors.New("OAuth endpoints require HTTPS")
			}
		}
		if _, _, e = execution.EndpointClient(raw); e != nil {
			return e
		}
	}
	return nil
}
func randomURLToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func (o *OAuthManager) Start(ctx context.Context, id, resource string, c OAuthConfig) (OAuthStatus, error) {
	if e := validateOAuth(c, o.options.AllowLoopback); e != nil {
		return OAuthStatus{}, e
	}
	if o.options.Secrets == nil || o.options.OpenURL == nil {
		return OAuthStatus{}, errors.New("secure credential storage and a system browser are required")
	}
	if _, _, e := execution.EndpointClient(resource); e != nil {
		return OAuthStatus{}, e
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.life.Err() != nil {
		return OAuthStatus{}, o.life.Err()
	}
	if p := o.pending[id]; p != nil && p.status.Status == "pending" {
		return p.status, errors.New("OAuth sign-in is already pending")
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return OAuthStatus{}, e
	}
	flow, cancel := context.WithTimeout(o.life, 3*time.Minute)
	p := &oauthPending{cancel: cancel, status: OAuthStatus{ServerID: id, Status: "pending"}}
	o.pending[id] = p
	state, verifier := randomURLToken(), randomURLToken()
	challenge := sha256.Sum256([]byte(verifier))
	callback := "http://" + listener.Addr().String() + "/oauth/callback"
	authURL, _ := url.Parse(c.AuthorizationEndpoint)
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", callback)
	q.Set("state", state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("scope", strings.Join(c.Scopes, " "))
	q.Set("resource", resource)
	authURL.RawQuery = q.Encode()
	codes := make(chan *sdkauth.AuthorizationResult, 1)
	var expectedState atomic.Value
	expectedState.Store(state)
	mux := http.NewServeMux()
	var once sync.Once
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		values, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || r.Method != "GET" || r.Host != listener.Addr().String() || len(values["state"]) != 1 || subtle.ConstantTimeCompare([]byte(values.Get("state")), []byte(expectedState.Load().(string))) != 1 {
			http.Error(w, "Invalid authorization callback", 400)
			return
		}
		if iss := values.Get("iss"); iss != "" && iss != c.Issuer {
			http.Error(w, "Issuer mismatch", 400)
			return
		}
		if values.Get("error") != "" {
			once.Do(func() { codes <- nil })
			http.Error(w, "Authorization declined", 400)
			return
		}
		code := values.Get("code")
		if len(values["code"]) != 1 || code == "" || len(code) > 8192 {
			http.Error(w, "Invalid authorization code", 400)
			return
		}
		accepted := false
		once.Do(func() {
			accepted = true
			codes <- &sdkauth.AuthorizationResult{Code: code, State: values.Get("state"), Iss: values.Get("iss")}
		})
		if !accepted {
			http.Error(w, "Callback already consumed", 409)
			return
		}
		_, _ = io.WriteString(w, "Authorization received. You can return to Gemma Desktop.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer cancel()
		defer server.Close()
		go func() { _ = server.Serve(listener) }()
		fetch := func(ctx context.Context, args *sdkauth.AuthorizationArgs) (*sdkauth.AuthorizationResult, error) {
			u, e := url.Parse(args.URL)
			if e != nil {
				return nil, e
			}
			if u.Scheme != authURL.Scheme || u.Host != authURL.Host || u.Path != authURL.Path {
				return nil, errors.New("authorization URL differs from pinned endpoint")
			}
			expectedState.Store(u.Query().Get("state"))
			if e = o.options.OpenURL(args.URL); e != nil {
				return nil, e
			}
			select {
			case result := <-codes:
				if result == nil {
					return nil, errors.New("authorization was declined")
				}
				return result, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		save := func(record tokenRecord) error {
			o.mu.Lock()
			defer o.mu.Unlock()
			if o.pending[id] != p || flow.Err() != nil {
				return context.Canceled
			}
			b, _ := json.Marshal(record)
			if e := o.options.Secrets.Put(flow, id, b); e != nil {
				return e
			}
			p.status.Path = record.Path
			return nil
		}
		usedSDK, err := o.authorizeSDK(flow, c, resource, callback, fetch, save)
		if !usedSDK && err == nil {
			var result *sdkauth.AuthorizationResult
			result, err = fetch(flow, &sdkauth.AuthorizationArgs{URL: authURL.String()})
			if err == nil {
				form := url.Values{"grant_type": {"authorization_code"}, "code": {result.Code}, "redirect_uri": {callback}, "client_id": {c.ClientID}, "code_verifier": {verifier}, "resource": {resource}}
				var record tokenRecord
				record, err = o.exchange(flow, c, resource, form, "")
				if err == nil {
					record.Path = "explicit_endpoints"
					err = save(record)
				}
			}
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.pending[id] != p {
			return
		}
		p.status.Status = "connected"
		if err != nil {
			p.status.Status = "failed"
			p.status.Error = "OAuth sign-in did not complete: " + safeOAuthError(err)
		}
	}()
	return p.status, nil
}
func safeOAuthError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return "provider, browser, endpoint policy, or secure storage rejected the operation"
}
func (o *OAuthManager) exchange(ctx context.Context, c OAuthConfig, resource string, form url.Values, oldRefresh string) (tokenRecord, error) {
	client, _, e := execution.EndpointClient(c.TokenEndpoint)
	if e != nil {
		return tokenRecord{}, e
	}
	client.Timeout = 20 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("token endpoint redirects are forbidden")
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.TokenEndpoint, strings.NewReader(form.Encode()))
	if e != nil {
		return tokenRecord{}, e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	response, e := client.Do(req)
	if e != nil {
		return tokenRecord{}, e
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if e != nil || len(b) > 256<<10 || response.StatusCode != 200 {
		return tokenRecord{}, errors.New("OAuth token exchange failed")
	}
	var v struct {
		Access  string      `json:"access_token"`
		Refresh string      `json:"refresh_token"`
		Type    string      `json:"token_type"`
		Expires json.Number `json:"expires_in"`
		Scope   string      `json:"scope"`
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return tokenRecord{}, errors.New("invalid OAuth token response")
	}
	if v.Access == "" || len(v.Access) > 64<<10 || strings.ContainsAny(v.Access, "\r\n") || !strings.EqualFold(v.Type, "Bearer") || len(v.Refresh) > 64<<10 {
		return tokenRecord{}, errors.New("unsupported OAuth credential response")
	}
	for _, scope := range strings.Fields(v.Scope) {
		if !containsScope(c.Scopes, scope) {
			return tokenRecord{}, errors.New("OAuth provider returned unrequested scope")
		}
	}
	record := tokenRecord{ConfigHash: digest(c), Resource: resource, Access: v.Access, Refresh: oldRefresh}
	if v.Refresh != "" {
		record.Refresh = v.Refresh
	}
	if v.Expires != "" {
		seconds, e := v.Expires.Int64()
		if e != nil || seconds <= 0 || seconds > 31536000 {
			return record, errors.New("invalid OAuth expiry")
		}
		record.Expires = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	return record, nil
}
func containsScope(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
func (o *OAuthManager) Token(ctx context.Context, id, resource string, c OAuthConfig) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.life.Err() != nil {
		return "", o.life.Err()
	}
	if e := validateOAuth(c, o.options.AllowLoopback); e != nil {
		return "", e
	}
	b, e := o.options.Secrets.Get(ctx, id)
	if e != nil {
		return "", errors.New("sign in to this MCP server first")
	}
	var record tokenRecord
	if e = json.Unmarshal(b, &record); e != nil || record.ConfigHash != digest(c) || record.Resource != resource {
		return "", errors.New("OAuth configuration changed; sign in again")
	}
	if !record.Expires.IsZero() && time.Until(record.Expires) < 30*time.Second {
		if record.Refresh == "" {
			return "", errors.New("OAuth credential expired; sign in again")
		}
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {record.Refresh}, "client_id": {c.ClientID}, "resource": {resource}}
		fresh, e := o.exchange(ctx, c, resource, form, record.Refresh)
		if e != nil {
			return "", errors.New("OAuth refresh failed; sign in again")
		}
		fresh.Path = record.Path
		data, _ := json.Marshal(fresh)
		if e = o.options.Secrets.Put(ctx, id, data); e != nil {
			return "", errors.New("OAuth credential could not be saved securely")
		}
		record = fresh
	}
	return record.Access, nil
}
func (o *OAuthManager) Status(ctx context.Context, id string) OAuthStatus {
	o.mu.Lock()
	defer o.mu.Unlock()
	if p := o.pending[id]; p != nil {
		return p.status
	}
	status := OAuthStatus{ServerID: id, Status: "signed_out"}
	if o.options.Secrets != nil {
		if b, e := o.options.Secrets.Get(ctx, id); e == nil {
			var record tokenRecord
			_ = json.Unmarshal(b, &record)
			status.Path = record.Path
			status.Status = "connected"
		}
	}
	return status
}
func (o *OAuthManager) SignOut(ctx context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if p := o.pending[id]; p != nil {
		p.cancel()
	}
	delete(o.pending, id)
	return o.options.Secrets.Delete(ctx, id)
}
func (o *OAuthManager) Close() { o.cancel(); o.wg.Wait() }

type oauthTransport struct {
	base         http.RoundTripper
	manager      *OAuthManager
	id, resource string
	config       OAuthConfig
}

func (t oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	endpoint, _ := url.Parse(t.resource)
	if req.URL.Scheme != endpoint.Scheme || !strings.EqualFold(req.URL.Host, endpoint.Host) || req.URL.Path != endpoint.Path {
		return nil, errors.New("OAuth bearer target differs from configured MCP endpoint")
	}
	token, e := t.manager.Token(req.Context(), t.id, t.resource, t.config)
	if e != nil {
		return nil, e
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+token)
	// Deliberately no 401/403 retry: a transport response cannot establish that
	// a tool operation had no effect. Re-authentication is explicit.
	return t.base.RoundTrip(clone)
}
func (m *Manager) ConfigureOAuth(o *OAuthManager) { m.mu.Lock(); defer m.mu.Unlock(); m.oauth = o }
func (m *Manager) OAuth() *OAuthManager           { m.mu.Lock(); defer m.mu.Unlock(); return m.oauth }
