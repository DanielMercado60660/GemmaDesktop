package app

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/mcp"
	"strings"
)

func (c *Core) initOAuth() error {
	c.extension.mcp.ConfigureOAuth(mcp.NewOAuthManager(mcp.OAuthOptions{Secrets: mcp.NewKeychainStore("GemmaDesktop.MCPOAuth." + contentDigest(c.DataDir)), OpenURL: func(url string) error {
		if c.OpenURL == nil {
			return errors.New("system browser is unavailable")
		}
		return c.OpenURL(url)
	}}))
	return nil
}
func (c *Core) closeOAuth() {
	if c.extension != nil {
		if o := c.extension.mcp.OAuth(); o != nil {
			o.Close()
		}
	}
}

// Call before replacing/deleting a saved MCP configuration. An in-flight login
// for the old issuer/client must not install a credential after that change.
func (c *Core) invalidateOAuthOnConfigChange(ctx context.Context, id string, next Doc) error {
	if id == "" {
		return nil
	}
	old, e := c.Store.Get("mcp", id)
	if e != nil {
		return e
	}
	if old["oauth"] == nil {
		return nil
	}
	if next != nil && contentDigest([]any{old["url"], old["oauth"]}) == contentDigest([]any{next["url"], next["oauth"]}) {
		return nil
	}
	if o := c.extension.mcp.OAuth(); o != nil {
		return o.SignOut(ctx, id)
	}
	return nil
}

// Keep only declarative public configuration. Secrets never enter SQLite, logs,
// frontend state or the MCP launch environment.
func sanitizeMCPConfig(p Doc) (Doc, error) {
	for key := range p {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || lower == "headers" || lower == "env" {
			return nil, errors.New("MCP credentials must use OAuth secure storage")
		}
	}
	d := Doc{}
	for _, key := range []string{"id", "name", "transport", "command", "args", "url", "enabled"} {
		if v, ok := p[key]; ok {
			d[key] = v
		}
	}
	if raw, ok := p["oauth"]; ok && raw != nil {
		b, e := json.Marshal(raw)
		if e != nil {
			return nil, e
		}
		var fields map[string]any
		if e = json.Unmarshal(b, &fields); e != nil {
			return nil, e
		}
		allowed := map[string]bool{"issuer": true, "authorizationEndpoint": true, "tokenEndpoint": true, "clientId": true, "scopes": true}
		for key := range fields {
			if !allowed[key] {
				return nil, errors.New("unsupported OAuth field; client secrets and tokens are not accepted")
			}
		}
		var config mcp.OAuthConfig
		if e = json.Unmarshal(b, &config); e != nil {
			return nil, e
		}
		if e = mcp.ValidateOAuthConfig(config); e != nil {
			return nil, e
		}
		if d["transport"] == "stdio" {
			return nil, errors.New("OAuth is supported only for HTTP MCP servers")
		}
		d["oauth"] = config
	}
	return d, nil
}
func (c *Core) oauthCall(ctx context.Context, method string, p Doc) (any, error) {
	id := str(p, "id")
	d, e := c.Store.Get("mcp", id)
	if e != nil {
		return nil, e
	}
	o := c.extension.mcp.OAuth()
	if o == nil {
		return nil, errors.New("OAuth is unavailable")
	}
	switch method {
	case "mcp.oauth.get":
		return o.Status(ctx, id), nil
	case "mcp.oauth.signout":
		_ = c.extension.mcp.Disconnect(id)
		if e = o.SignOut(ctx, id); e != nil {
			return nil, e
		}
		d["status"] = "disconnected"
		_ = c.Store.Put("mcp", d)
		return o.Status(ctx, id), nil
	case "mcp.oauth.start":
		var config mcp.OAuthConfig
		b, _ := json.Marshal(d["oauth"])
		if e = json.Unmarshal(b, &config); e != nil {
			return nil, e
		}
		return o.Start(c.ctx, id, str(d, "url"), config)
	}
	return nil, errors.New("unknown OAuth method")
}
