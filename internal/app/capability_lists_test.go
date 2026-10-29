package app

import (
	"encoding/json"
	"testing"
)

func TestCapabilityPublicListsNormalizeOmittedAndHistoricalNullArrays(t *testing.T) {
	c := milestoneCore(t)
	check := func(v any) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var row map[string]json.RawMessage
		if err = json.Unmarshal(b, &row); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"tools", "skillIds", "mcp", "delegateAgentIds"} {
			if string(row[key]) != "[]" {
				t.Errorf("%s is not an empty JSON array: %s", key, row[key])
			}
		}
	}
	for _, input := range []Doc{{"name": "Omitted"}, {"name": "Null", "tools": nil, "skillIds": nil, "mcp": nil, "delegateAgentIds": nil}} {
		out, err := c.saveCapability(input)
		if err != nil {
			t.Fatal(err)
		}
		check(out)
		saved, err := c.Store.Get("capability", str(obj(out), "id"))
		if err != nil {
			t.Fatal(err)
		}
		check(saved)
	}
	legacy := Doc{"id": "historical-null", "version": 1, "revision": 3, "name": "Historical", "executionProfile": "restricted", "tools": nil, "skillIds": nil, "mcp": nil, "delegateAgentIds": nil}
	if err := c.Store.Put("capability", legacy); err != nil {
		t.Fatal(err)
	}
	before, _ := c.Store.Get("capability", "historical-null")
	beforeDigest := contentDigest(before)
	listed, err := c.milestoneCall(t.Context(), "capability.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range listed.([]Doc) {
		if row["id"] == "historical-null" {
			check(row)
			found = true
		}
	}
	if !found {
		t.Fatal("historical profile absent")
	}
	snapshot := Doc{}
	if err = c.milestoneSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	for _, row := range snapshot["capabilityProfiles"].([]Doc) {
		if row["id"] == "historical-null" {
			check(row)
		}
	}
	after, _ := c.Store.Get("capability", "historical-null")
	if contentDigest(after) != beforeDigest {
		t.Fatal("public read rewrote historical identity")
	}
}
