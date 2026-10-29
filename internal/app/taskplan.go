package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/store"
)

type PlanItem struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	ReceiptIDs []string `json:"receiptIds,omitempty"`
}

func planTool() engine.ToolDefinition {
	return engine.ToolDefinition{Name: "plan_update", Description: "Record the full task plan with optimistic revision. Start revision 0; use the returned revision on updates. To link evidence, copy the exact host receiptId returned by a recorded tool into receiptIds (at most 128 unique receipts across the plan). Do not invent IDs. Failed tool outcomes may be linked as observations. Plan status is a progress claim, not proof of correctness or authorization.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"revision": map[string]any{"type": "integer", "minimum": 0}, "items": map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "receiptIds": map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"type": "string", "maxLength": 512}}, "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "blocked", "cancelled"}}}, "required": []string{"id", "title", "status"}, "additionalProperties": false}}}, "required": []string{"revision", "items"}, "additionalProperties": false}}
}
func (c *Core) updateTaskPlan(runID string, p Doc) (Doc, error) {
	if _, e := c.Store.Get("run", runID); e != nil {
		return nil, e
	}
	var items []PlanItem
	b, e := json.Marshal(p["items"])
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, &items); e != nil {
		return nil, e
	}
	if len(items) > 32 {
		return nil, errors.New("at most 32 plan items")
	}
	seen := map[string]bool{}
	receipts := map[string]bool{}
	active := 0
	for _, i := range items {
		if i.ID == "" || len(i.ID) > 80 || seen[i.ID] || i.Title == "" || len(i.Title) > 1000 {
			return nil, errors.New("plan requires unique item ids and bounded titles")
		}
		seen[i.ID] = true
		switch i.Status {
		case "in_progress":
			active++
		case "pending", "completed", "blocked", "cancelled":
		default:
			return nil, errors.New("invalid plan status")
		}
		if len(i.ReceiptIDs) > 32 {
			return nil, errors.New("too many receipts")
		}
		for _, id := range i.ReceiptIDs {
			if id == "" || len(id) > 512 {
				return nil, errors.New("receipt identity must contain 1–512 bytes")
			}
			receipts[id] = true
			if len(receipts) > 128 {
				return nil, errors.New("at most 128 unique receipt links per plan")
			}
			_, e := c.Store.PlanReceipt(runID, id)
			if e != nil {
				return nil, fmt.Errorf("receipt %q does not belong to this run", id)
			}
		}
	}
	if active > 1 {
		return nil, errors.New("at most one plan item may be in progress")
	}
	rev, ok := finiteNumber(p["revision"])
	if !ok || rev < 0 || rev != float64(int(rev)) {
		return nil, errors.New("integer revision required")
	}
	d := Doc{"version": 1, "runId": runID, "items": items, "updatedAt": store.Now(), "qualification": "Agent/user progress claims; linked execution receipts are observations, not independent correctness grades."}
	return c.Store.Revise("taskplan", runID, int(rev), d)
}

func (c *Core) taskPlanView(runID string) (Doc, error) {
	d, err := c.Store.Get("taskplan", runID)
	if errors.Is(err, store.ErrNotFound) {
		return Doc{"id": runID, "revision": 0, "items": []any{}, "observations": Doc{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []PlanItem
	b, _ := json.Marshal(d["items"])
	if err = json.Unmarshal(b, &items); err != nil {
		return nil, err
	}
	observations, seen := Doc{}, map[string]bool{}
	omitted := 0
	for _, item := range items {
		for _, id := range item.ReceiptIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			if len(observations) >= 128 || len(id) > 512 {
				omitted++
				continue
			}
			row, e := c.Store.PlanReceipt(runID, id)
			if errors.Is(e, store.ErrNotFound) {
				row = Doc{"receiptId": id, "status": "unavailable", "note": "No linked observation is available in this run."}
			} else if e != nil {
				return nil, e
			}
			observations[id] = row
		}
	}
	d["observations"], d["observationsOmitted"] = observations, omitted
	return d, nil
}
