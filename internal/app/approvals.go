package app

import (
	"context"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/store"
)

func (c *Core) approve(ctx context.Context, runID, tool string, args any, reason string) error {
	id := store.ID()
	ch := make(chan bool, 1)
	d := Doc{"id": id, "runId": runID, "tool": tool, "arguments": args, "reason": reason, "status": "pending", "createdAt": store.Now()}
	c.mu.Lock()
	c.approvals[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.approvals, id); c.mu.Unlock() }()
	if e := c.Store.Put("approval", d); e != nil {
		return e
	}
	if runID != "" {
		if _, e := c.Store.Update("run", runID, func(r Doc) error { r["status"] = "waiting_approval"; return nil }); e != nil {
			return e
		}
	}
	if e := c.Emit(runID, "approval", "Approval required", reason, Doc{"approvalId": id, "tool": tool}); e != nil {
		return e
	}
	select {
	case <-ctx.Done():
		d["status"] = "expired"
		_ = c.Store.Put("approval", d)
		return ctx.Err()
	case allow := <-ch:
		if runID != "" {
			_, _ = c.Store.Update("run", runID, func(r Doc) error {
				if r["status"] == "waiting_approval" {
					r["status"] = "running"
				}
				return nil
			})
		}
		if !allow {
			return errors.New("The user denied this operation")
		}
		return nil
	}
}
func (c *Core) respondApproval(id string, allow bool) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := c.approvals[id]
	if ch == nil {
		return nil, errors.New("This approval is no longer pending")
	}
	d, e := c.Store.Get("approval", id)
	if e != nil {
		return nil, e
	}
	if d["status"] != "pending" {
		return nil, errors.New("This approval has already been answered")
	}
	if allow {
		d["status"] = "approved"
	} else {
		d["status"] = "denied"
	}
	d["resolvedAt"] = store.Now()
	if e = c.Store.Put("approval", d); e != nil {
		return nil, e
	}
	select {
	case ch <- allow:
		return d, nil
	default:
		return nil, fmt.Errorf("approval %s already delivered", id)
	}
}
