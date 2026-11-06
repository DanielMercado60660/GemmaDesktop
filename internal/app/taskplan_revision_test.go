package app

import (
	"strings"
	"testing"
)

func TestTaskPlanAcceptsNextRevisionNumber(t *testing.T) {
	c := milestoneCore(t)
	if err := c.Store.Put("run", Doc{"id": "r", "status": "running"}); err != nil {
		t.Fatal(err)
	}
	plan := func(rev int, status string) (Doc, error) {
		return c.updateTaskPlan("r", Doc{"revision": float64(rev), "items": []PlanItem{{ID: "a", Title: "A", Status: status}}})
	}
	// A first plan sent as revision 1 and an update sent as "next" (2) are both
	// read as replacing the current revision.
	d, err := plan(1, "pending")
	if err != nil || number(d, "revision", 0) != 1 {
		t.Fatalf("first plan: %v %v", d, err)
	}
	if d, err = plan(2, "in_progress"); err != nil || number(d, "revision", 0) != 2 {
		t.Fatalf("next-number update: %v %v", d, err)
	}
	if d, err = plan(2, "completed"); err != nil || number(d, "revision", 0) != 3 {
		t.Fatalf("current-number update: %v %v", d, err)
	}
	// Stale or skipped revisions still conflict.
	for _, rev := range []int{1, 5} {
		if _, err = plan(rev, "blocked"); err == nil || !strings.Contains(err.Error(), "revision conflict") {
			t.Fatalf("revision %d accepted: %v", rev, err)
		}
	}
}
