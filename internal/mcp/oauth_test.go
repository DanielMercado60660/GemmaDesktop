package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testSecrets struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *testSecrets) Get(_ context.Context, k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[k]
	if !ok {
		return nil, ErrNoCredential
	}
	return append([]byte{}, b...), nil
}
func (s *testSecrets) Put(ctx context.Context, k string, b []byte) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k] = append([]byte{}, b...)
	return nil
}
func (s *testSecrets) Delete(_ context.Context, k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, k)
	return nil
}
func TestOAuthPKCECallbackRefreshAndSignout(t *testing.T) {
	secrets := &testSecrets{data: map[string][]byte{}}
	var browserQuery url.Values
	var refresh atomic.Int32
	resource := ""
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" {
			t.Error("token request was not POST")
		}
		_ = r.ParseForm()
		if r.Form.Get("resource") != resource {
			t.Error("resource audience absent")
		}
		if r.Form.Get("grant_type") == "authorization_code" {
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != browserQuery.Get("code_challenge") || r.Form.Get("code") != "fixture-code" {
				t.Error("PKCE mismatch")
			}
			_, _ = io.WriteString(w, `{"access_token":"first","refresh_token":"rotate-me","token_type":"Bearer","expires_in":1,"scope":"read"}`)
		} else {
			refresh.Add(1)
			if r.Form.Get("refresh_token") != "rotate-me" {
				t.Error("refresh token not preserved")
			}
			_, _ = io.WriteString(w, `{"access_token":"second","refresh_token":"rotated","token_type":"Bearer","expires_in":3600}`)
		}
	}))
	defer provider.Close()
	resource = provider.URL + "/mcp"
	config := OAuthConfig{Issuer: provider.URL, AuthorizationEndpoint: provider.URL + "/authorize", TokenEndpoint: provider.URL + "/token", ClientID: "public-client", Scopes: []string{"read"}}
	o := NewOAuthManager(OAuthOptions{Secrets: secrets, AllowLoopback: true, OpenURL: func(raw string) error {
		u, e := url.Parse(raw)
		if e != nil {
			return e
		}
		browserQuery = u.Query()
		if browserQuery.Get("code_challenge_method") != "S256" || len(browserQuery.Get("state")) < 24 {
			return errors.New("missing PKCE/state")
		}
		cb, _ := url.Parse(browserQuery.Get("redirect_uri"))
		q := url.Values{"state": {"bad-state"}, "code": {"fixture-code"}}
		cb.RawQuery = q.Encode()
		res, e := http.Get(cb.String())
		if e != nil {
			return e
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			return errors.New("invalid state accepted")
		}
		q.Set("state", browserQuery.Get("state"))
		q.Set("iss", "https://wrong-issuer.example")
		cb.RawQuery = q.Encode()
		res, e = http.Get(cb.String())
		if e != nil {
			return e
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			return errors.New("wrong issuer accepted")
		}
		q.Set("iss", provider.URL)
		cb.RawQuery = q.Encode()
		res, e = http.Get(cb.String())
		if e != nil {
			return e
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			return errors.New("valid callback failed")
		}
		res, e = http.Get(cb.String())
		if e != nil {
			return e
		}
		res.Body.Close()
		if res.StatusCode != 409 {
			return errors.New("callback replay accepted")
		}
		return nil
	}})
	defer o.Close()
	if _, e := o.Start(t.Context(), "fixture", resource, config); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for o.Status(t.Context(), "fixture").Status == "pending" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond * 5)
	}
	if got := o.Status(t.Context(), "fixture"); got.Status != "connected" || got.Path != "explicit_endpoints" {
		t.Fatalf("flow: %+v", got)
	}
	token, e := o.Token(t.Context(), "fixture", resource, config)
	if e != nil || token != "second" || refresh.Load() != 1 {
		t.Fatalf("refresh %q %d %v", token, refresh.Load(), e)
	}
	config.ClientID = "changed"
	if _, e = o.Token(t.Context(), "fixture", resource, config); e == nil {
		t.Fatal("changed identity accepted")
	}
	if e = o.SignOut(t.Context(), "fixture"); e != nil {
		t.Fatal(e)
	}
	if o.Status(t.Context(), "fixture").Status != "signed_out" {
		t.Fatal("credential retained")
	}
}
func TestOAuthEndpointPolicyAndNoUncertainReplay(t *testing.T) {
	base := OAuthConfig{Issuer: "https://issuer.example", AuthorizationEndpoint: "https://issuer.example/auth", TokenEndpoint: "https://issuer.example/token", ClientID: "public"}
	for _, bad := range []string{"http://issuer.example/token", "https://other.example/token", "https://issuer.example/token?secret=x", "https://a:b@issuer.example/token"} {
		c := base
		c.TokenEndpoint = bad
		if ValidateOAuthConfig(c) == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	secret := &testSecrets{data: map[string][]byte{}}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing bearer")
		}
		w.WriteHeader(401)
	}))
	defer server.Close()
	b, _ := json.Marshal(tokenRecord{ConfigHash: digest(base), Resource: server.URL + "/mcp", Access: "fixture"})
	_ = secret.Put(t.Context(), "server", b)
	o := NewOAuthManager(OAuthOptions{Secrets: secret})
	defer o.Close()
	client := &http.Client{Transport: oauthTransport{base: http.DefaultTransport, manager: o, id: "server", resource: server.URL + "/mcp", config: base}}
	req, _ := http.NewRequestWithContext(t.Context(), "POST", server.URL+"/mcp", strings.NewReader(`{"method":"tools/call"}`))
	r, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if calls.Load() != 1 || r.StatusCode != 401 {
		t.Fatal("unauthorized tool request was replayed")
	}
	if _, e = client.Get(server.URL + "/other"); e == nil {
		t.Fatal("bearer sent outside approved resource")
	}
}
func TestFrozenMCPGrantRejectsInnerSelectorOrSchemaDrift(t *testing.T) {
	server := &connected{server: Server{Config: Config{ID: "s"}, ConfigHash: "config"}}
	ctx := WithGrant(t.Context(), Grant{ServerID: "s", Tool: "read", ConfigHash: "config", SchemaHash: "schema"})
	if e := checkGrant(ctx, server, "read", "schema"); e != nil {
		t.Fatal(e)
	}
	if checkGrant(ctx, server, "write", "schema") == nil || checkGrant(ctx, server, "read", "changed") == nil {
		t.Fatal("inner selector/schema bypass")
	}
}

func TestOAuthPendingCancelAndRestartDoNotRecoverAuthority(t *testing.T) {
	for _, operation := range []string{"signout", "restart"} {
		t.Run(operation, func(t *testing.T) {
			secrets := &testSecrets{data: map[string][]byte{}}
			browser := make(chan string, 1)
			var exchanges atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/token" {
					http.NotFound(w, r)
					return
				}
				exchanges.Add(1)
				t.Error("cancelled pending flow exchanged a code")
			}))
			defer provider.Close()
			config := OAuthConfig{Issuer: provider.URL, AuthorizationEndpoint: provider.URL + "/authorize", TokenEndpoint: provider.URL + "/token", ClientID: "fixture"}
			o := NewOAuthManager(OAuthOptions{Secrets: secrets, AllowLoopback: true, OpenURL: func(raw string) error { browser <- raw; return nil }})
			if _, e := o.Start(t.Context(), "server", provider.URL+"/mcp", config); e != nil {
				t.Fatal(e)
			}
			var raw string
			select {
			case raw = <-browser:
			case <-time.After(time.Second):
				t.Fatal("browser was not opened")
			}
			if operation == "signout" {
				if e := o.SignOut(t.Context(), "server"); e != nil {
					t.Fatal(e)
				}
			}
			o.Close()
			fresh := NewOAuthManager(OAuthOptions{Secrets: secrets, AllowLoopback: true})
			defer fresh.Close()
			if fresh.Status(t.Context(), "server").Status != "signed_out" {
				t.Fatal("pending sign-in survived restart")
			}
			u, _ := url.Parse(raw)
			q := u.Query()
			callback, _ := url.Parse(q.Get("redirect_uri"))
			callback.RawQuery = url.Values{"state": {q.Get("state")}, "code": {"late"}, "iss": {provider.URL}}.Encode()
			client := &http.Client{Timeout: time.Second}
			if response, e := client.Get(callback.String()); e == nil {
				response.Body.Close()
				t.Fatal("cancelled callback listener remains active")
			}
			if exchanges.Load() != 0 {
				t.Fatal("cancelled flow acquired token")
			}
		})
	}
}

func TestOAuthSDKPreferredAndMaliciousMetadataNeverDowngrades(t *testing.T) {
	for _, variant := range []string{"sdk", "wrong_issuer", "wrong_endpoint", "malformed", "http_error"} {
		t.Run(variant, func(t *testing.T) {
			secrets := &testSecrets{data: map[string][]byte{}}
			var browserCalls, tokens atomic.Int32
			var base string
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/oauth-protected-resource/mcp":
					if variant == "malformed" {
						fmt.Fprint(w, "not json")
						return
					}
					if variant == "http_error" {
						w.WriteHeader(403)
						return
					}
					issuer := base
					if variant == "wrong_issuer" {
						issuer = "https://malicious.example"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"resource": base + "/mcp", "authorization_servers": []string{issuer}, "scopes_supported": []string{"read"}})
				case "/.well-known/oauth-authorization-server":
					token := base + "/token"
					if variant == "wrong_endpoint" {
						token = base + "/unexpected"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": token, "response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}})
				case "/token":
					tokens.Add(1)
					_ = r.ParseForm()
					if r.Form.Get("resource") != base+"/mcp" || r.Form.Get("code_verifier") == "" {
						t.Error("SDK exchange omitted PKCE/resource")
					}
					fmt.Fprint(w, `{"access_token":"sdk-canary","refresh_token":"sdk-refresh","token_type":"Bearer","expires_in":3600,"scope":"read"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer provider.Close()
			base = provider.URL
			config := OAuthConfig{Issuer: base, AuthorizationEndpoint: base + "/authorize", TokenEndpoint: base + "/token", ClientID: "public", Scopes: []string{"read"}}
			o := NewOAuthManager(OAuthOptions{Secrets: secrets, AllowLoopback: true, OpenURL: func(raw string) error {
				browserCalls.Add(1)
				u, _ := url.Parse(raw)
				q := u.Query()
				if q.Get("code_challenge_method") != "S256" || q.Get("resource") != base+"/mcp" {
					return errors.New("SDK authorization URL lost PKCE/resource")
				}
				cb, _ := url.Parse(q.Get("redirect_uri"))
				cb.RawQuery = url.Values{"state": {q.Get("state")}, "code": {"sdk-code"}, "iss": {base}}.Encode()
				r, e := http.Get(cb.String())
				if e != nil {
					return e
				}
				r.Body.Close()
				if r.StatusCode != 200 {
					return errors.New("SDK callback failed")
				}
				return nil
			}})
			defer o.Close()
			if _, e := o.Start(t.Context(), "server", base+"/mcp", config); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(3 * time.Second)
			for o.Status(t.Context(), "server").Status == "pending" && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			status := o.Status(t.Context(), "server")
			if variant == "sdk" {
				if status.Status != "connected" || status.Path != "sdk" || tokens.Load() != 1 {
					t.Fatal("SDK flow did not qualify", status, tokens.Load())
				}
				o.Close()
				restarted := NewOAuthManager(OAuthOptions{Secrets: secrets, AllowLoopback: true})
				defer restarted.Close()
				token, e := restarted.Token(t.Context(), "server", base+"/mcp", config)
				if e != nil || token != "sdk-canary" || restarted.Status(t.Context(), "server").Path != "sdk" {
					t.Fatal("SDK persistence hook failed", e)
				}
			} else {
				if status.Status != "failed" || browserCalls.Load() != 0 || tokens.Load() != 0 {
					t.Fatal("malicious/failed metadata silently downgraded", status, browserCalls.Load(), tokens.Load())
				}
				if _, e := secrets.Get(t.Context(), "server"); !errors.Is(e, ErrNoCredential) {
					t.Fatal("invalid metadata installed credential")
				}
			}
		})
	}
}
