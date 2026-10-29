package app

import (
	"context"
	"errors"
	"gemmadesktop.local/app/internal/tools"
)

func (c *Core) milestoneCall(ctx context.Context, method string, p Doc) (any, error) {
	switch method {
	case "mcp.oauth.start", "mcp.oauth.get", "mcp.oauth.signout":
		return c.oauthCall(ctx, method, p)
	case "capability.list":
		return c.listCapabilities()
	case "capability.catalogue":
		catalogue := []any{}
		for _, d := range tools.Catalogue() {
			catalogue = append(catalogue, d)
		}
		catalogue = append(catalogue, planTool(), delegateTool(), skillResourceTool(), skillMaterializeTool())
		return catalogue, nil
	case "capability.save":
		return c.saveCapability(p)
	case "plan.get":
		return c.taskPlanView(str(p, "runId"))
	case "plan.update":
		return c.updateTaskPlan(str(p, "runId"), p)
	case "context.get":
		return c.contextManifest(str(p, "runId"))
	case "delegation.get":
		return c.Store.Get("delegation", str(p, "id"))
	}
	return nil, errors.New("unknown milestone method")
}
func (c *Core) milestoneSnapshot(d Doc) error {
	for key, kind := range map[string]string{"capabilityProfiles": "capability", "taskPlans": "taskplan", "delegations": "delegation"} {
		var v []Doc
		var e error
		if kind == "capability" {
			v, e = c.listCapabilities()
		} else {
			v, e = c.Store.List(kind)
		}
		if e != nil {
			return e
		}
		if kind == "delegation" {
			for _, item := range v {
				delete(item, "result")
			}
		}
		d[key] = v
	}
	return nil
}
