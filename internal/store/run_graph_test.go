package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGraphUsesFrozenDefinitionAndProjectsOnlyBoundedMetadata(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	put := func(kind string, d Doc) {
		t.Helper()
		if err := s.Put(kind, d); err != nil {
			t.Fatal(err)
		}
	}
	frozen := Doc{"id": "flow", "name": "Executed version", "nodes": []Doc{{"id": "read", "type": "agent", "label": "Read source", "prompt": strings.Repeat("PRIVATE-PROMPT", 100000), "position": Doc{"x": 12, "y": 34}}}, "edges": []Doc{}}
	put("run", Doc{"id": "r", "workflowId": "flow"})
	put("checkpoint", Doc{"id": "r", "workflow": frozen, "snapshot": strings.Repeat("ADK-RAW-BODY", 100000), "request": Doc{"instructions": "PRIVATE-INSTRUCTIONS"}})
	put("workflow", Doc{"id": "flow", "name": "Edited version", "nodes": []Doc{{"id": "different"}}})
	first, err := s.RunGraph("r")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(first)
	if first["source"] != "checkpoint" || !strings.Contains(string(encoded), "Executed version") || !strings.Contains(string(encoded), "Read source") || len(encoded) > 2000 || strings.Contains(string(encoded), "PRIVATE") || strings.Contains(string(encoded), "ADK-RAW") || strings.Contains(string(encoded), "different") {
		t.Fatalf("incorrect projection: %s", encoded)
	}
	if err := s.Delete("workflow", "flow"); err != nil {
		t.Fatal(err)
	}
	after, err := s.RunGraph("r")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := json.Marshal(after)
	if string(encoded) != string(other) {
		t.Fatal("mutable workflow deletion changed executed graph")
	}
	full, err := s.Get("checkpoint", "r")
	if err != nil || !strings.Contains(full["snapshot"].(string), "ADK-RAW") {
		t.Fatal("projection changed checkpoint")
	}
}

func TestRunGraphMissingAndOversizedDefinitionsNeverUseMutableFallback(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RunGraph("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
	for _, id := range []string{"agent", "legacy", "large"} {
		run := Doc{"id": id}
		if id != "agent" {
			run["workflowId"] = "flow"
		}
		if err := s.Put("run", run); err != nil {
			t.Fatal(err)
		}
	}
	nodes := make([]Doc, 65)
	for i := range nodes {
		nodes[i] = Doc{"id": "node"}
	}
	if err := s.Put("checkpoint", Doc{"id": "large", "workflow": Doc{"nodes": nodes, "edges": []Doc{}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"agent", "legacy", "large"} {
		got, err := s.RunGraph(id)
		if err != nil || got["source"] != "events" || got["workflow"] != nil || got["reason"] == "" {
			t.Fatalf("%s: %v %v", id, got, err)
		}
	}
}
