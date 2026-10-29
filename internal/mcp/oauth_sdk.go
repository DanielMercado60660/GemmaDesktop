package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gemmadesktop.local/app/internal/execution"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

type sdkMetadata struct {
	body   []byte
	status int
}
type pinnedOAuthTransport struct {
	metadata    map[string]sdkMetadata
	tokenURL    string
	tokenClient *http.Client
}

func (t *pinnedOAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if cached, ok := t.metadata[r.URL.String()]; ok && r.Method == "GET" {
		return &http.Response{StatusCode: cached.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(cached.body)), Request: r}, nil
	}
	if r.URL.String() != t.tokenURL || r.Method != "POST" {
		return nil, errors.New("OAuth SDK request is outside pinned metadata/token policy")
	}
	res, e := t.tokenClient.Transport.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (256<<10)+1))
	res.Body.Close()
	if e != nil || len(b) > 256<<10 {
		return nil, errors.New("OAuth SDK token response exceeds limit")
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	return res, nil
}
func fetchOAuthMetadata(ctx context.Context, raw string) (sdkMetadata, error) {
	client, _, e := execution.EndpointClient(raw)
	if e != nil {
		return sdkMetadata{}, e
	}
	defer client.CloseIdleConnections()
	client.Timeout = 15 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("OAuth metadata redirects are forbidden")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return sdkMetadata{}, e
	}
	req.Header.Set("Accept", "application/json")
	res, e := client.Do(req)
	if e != nil {
		return sdkMetadata{}, e
	}
	defer res.Body.Close()
	if res.StatusCode == 404 || res.StatusCode == 405 || res.StatusCode == 501 {
		return sdkMetadata{status: 404}, nil
	}
	if res.StatusCode != 200 {
		return sdkMetadata{}, errors.New("OAuth metadata endpoint rejected discovery")
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (256<<10)+1))
	if e != nil || len(b) > 256<<10 {
		return sdkMetadata{}, errors.New("OAuth metadata exceeds limit")
	}
	if !json.Valid(b) {
		return sdkMetadata{}, errors.New("OAuth metadata is malformed")
	}
	return sdkMetadata{body: b, status: 200}, nil
}
func oauthMetadataURLs(resource, issuer string) ([]string, []string) {
	r, _ := url.Parse(resource)
	prm := *r
	prm.RawQuery = ""
	prm.Path = "/.well-known/oauth-protected-resource/" + strings.TrimLeft(r.Path, "/")
	first := prm.String()
	prm.Path = "/.well-known/oauth-protected-resource"
	a, _ := url.Parse(issuer)
	asm := *a
	paths := []string{}
	if a.Path == "" {
		asm.Path = "/.well-known/oauth-authorization-server"
		paths = append(paths, asm.String())
		asm.Path = "/.well-known/openid-configuration"
		paths = append(paths, asm.String())
	} else {
		for _, p := range []string{"/.well-known/oauth-authorization-server/" + strings.TrimLeft(a.Path, "/"), "/.well-known/openid-configuration/" + strings.TrimLeft(a.Path, "/"), "/" + strings.Trim(a.Path, "/") + "/.well-known/openid-configuration"} {
			asm.Path = p
			paths = append(paths, asm.String())
		}
	}
	return []string{first, prm.String()}, paths
}

// SDK authorization is preferred only with authentic, bounded metadata matching
// the user-pinned endpoints. Cached validated responses freeze discovery through
// the exchange. A missing/unsupported metadata endpoint permits explicit mode;
// malformed, mismatched, network and authentication failures never downgrade.
func (o *OAuthManager) authorizeSDK(ctx context.Context, c OAuthConfig, resource, callback string, fetch sdkauth.AuthorizationCodeFetcher, save func(tokenRecord) error) (bool, error) {
	prmURLs, asmURLs := oauthMetadataURLs(resource, c.Issuer)
	cache := map[string]sdkMetadata{}
	found := false
	for _, raw := range prmURLs {
		m, e := fetchOAuthMetadata(ctx, raw)
		if e != nil {
			return true, e
		}
		cache[raw] = m
		if m.status == 404 {
			continue
		}
		var p oauthex.ProtectedResourceMetadata
		if e = json.Unmarshal(m.body, &p); e != nil {
			return true, e
		}
		if p.Resource != resource || len(p.AuthorizationServers) != 1 || p.AuthorizationServers[0] != c.Issuer {
			return true, errors.New("protected-resource metadata differs from pinned resource/issuer")
		}
		found = true
		break
	}
	if !found {
		return false, nil
	}
	found = false
	for _, raw := range asmURLs {
		m, e := fetchOAuthMetadata(ctx, raw)
		if e != nil {
			return true, e
		}
		cache[raw] = m
		if m.status == 404 {
			continue
		}
		var a oauthex.AuthServerMeta
		if e = json.Unmarshal(m.body, &a); e != nil {
			return true, e
		}
		if a.Issuer != c.Issuer || a.AuthorizationEndpoint != c.AuthorizationEndpoint || a.TokenEndpoint != c.TokenEndpoint {
			return true, errors.New("authorization metadata differs from pinned issuer/endpoints")
		}
		found = true
		break
	}
	if !found {
		return false, nil
	}
	tokenClient, _, e := execution.EndpointClient(c.TokenEndpoint)
	if e != nil {
		return true, e
	}
	defer tokenClient.CloseIdleConnections()
	client := &http.Client{Timeout: 20 * time.Second, Transport: &pinnedOAuthTransport{metadata: cache, tokenURL: c.TokenEndpoint, tokenClient: tokenClient}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OAuth SDK redirects are forbidden") }}
	handler, e := sdkauth.NewAuthorizationCodeHandler(&sdkauth.AuthorizationCodeHandlerConfig{PreregisteredClient: &oauthex.ClientCredentials{ClientID: c.ClientID, Issuer: c.Issuer}, RedirectURL: callback, AuthorizationCodeFetcher: fetch, ScopeFilter: func([]string) []string { return append([]string{}, c.Scopes...) }, AcceptUnadvertisedIss: true, Client: client, NewTokenSource: func(_ context.Context, _ *oauth2.Config, t *oauth2.Token) (oauth2.TokenSource, error) {
		if t.AccessToken == "" || !strings.EqualFold(t.TokenType, "Bearer") || len(t.AccessToken) > 64<<10 || len(t.RefreshToken) > 64<<10 || strings.ContainsAny(t.AccessToken, "\r\n") {
			return nil, errors.New("unsupported SDK token response")
		}
		if value := t.Extra("scope"); value != nil {
			scope, ok := value.(string)
			if !ok {
				return nil, errors.New("invalid SDK scope response")
			}

			for _, s := range strings.Fields(scope) {
				if !containsScope(c.Scopes, s) {
					return nil, errors.New("OAuth SDK returned unrequested scope")
				}
			}
		}
		if !t.Expiry.IsZero() && (time.Until(t.Expiry) <= 0 || time.Until(t.Expiry) > 366*24*time.Hour) {
			return nil, errors.New("invalid SDK token expiry")
		}
		record := tokenRecord{ConfigHash: digest(c), Resource: resource, Access: t.AccessToken, Refresh: t.RefreshToken, Expires: t.Expiry, Path: "sdk"}
		if e := save(record); e != nil {
			return nil, e
		}
		// Resource-scoped refresh/persistence stays in our request-bound Token path;
		// never install an SDK transport that can replay an uncertain MCP action.
		return oauth2.StaticTokenSource(t), nil
	}})
	if e != nil {
		return true, e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", resource, nil)
	if e != nil {
		return true, e
	}
	// Explicit sign-in bootstrap only: no protected tool request is sent/replayed.
	response := &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	return true, handler.Authorize(ctx, req, response)
}
