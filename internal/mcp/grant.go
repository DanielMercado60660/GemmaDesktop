package mcp

import (
	"context"
	"errors"
)

type Grant struct {
	ServerID   string `json:"serverId"`
	Tool       string `json:"tool"`
	ConfigHash string `json:"configHash"`
	SchemaHash string `json:"schemaHash"`
}
type grantKey struct{}

func WithGrant(ctx context.Context, g Grant) context.Context {
	return context.WithValue(ctx, grantKey{}, g)
}
func checkGrant(ctx context.Context, server *connected, name, schema string) error {
	if g, ok := ctx.Value(grantKey{}).(Grant); ok {
		if g.ServerID != server.server.ID || g.Tool != name || g.ConfigHash != server.server.ConfigHash || (schema != "" && g.SchemaHash != schema) {
			return errors.New("MCP capability identity/schema changed since run admission")
		}
	}
	return nil
}
