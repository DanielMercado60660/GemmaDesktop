package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHarnessMetadataProjectsLargePayloadsAndPreservesDetailOrder(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "harness.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	large := strings.Repeat("complete immutable evidence\n", 80000)
	payloadKeys := []string{"context", "result", "response", "content", "results", "library", "approvedWorkflow"}
	nestedKeys := []string{"agent", "frozenCapability", "frozenCapabilities", "approvedSkills", "skills", "approvedWorkflows", "workflows", "frozenAgents", "agents"}
	for _, kind := range []string{"experiment", "revision", "packet", "trial", "lesson", "evaluation", "promotion"} {
		t.Run(kind, func(t *testing.T) {
			body := Doc{"id": "large", "status": "completed", "progress": Doc{"type": "tool_completed", "toolCount": 8}, "gate": Doc{"eligible": false}, "spec": Doc{"model": "fixture", "contextSize": 65536, "taskIds": []string{"public-task"}, "hash": "manifest"}}
			// Multi-megabyte real detail plus one marker per removable branch.
			for _, key := range payloadKeys {
				body[key] = Doc{"marker": "PAYLOAD-" + key}
			}
			body["result"] = Doc{"toolCalls": []any{Doc{"content": large}}}
			for _, key := range nestedKeys {
				body["spec"].(Doc)[key] = Doc{"marker": "NESTED-" + key}
			}
			if e = s.Put("harness_"+kind, body); e != nil {
				t.Fatal(e)
			}
			if e = s.Put("harness_"+kind, Doc{"id": "small", "status": "ready"}); e != nil {
				t.Fatal(e)
			}
			// Make update ordering explicit and different from ID lexical ordering.
			if _, e = s.db.Exec(`UPDATE entities SET updated=CASE id WHEN 'large' THEN '2026-10-27T01:00:00Z' ELSE '2026-10-27T02:00:00Z' END WHERE kind=?`, "harness_"+kind); e != nil {
				t.Fatal(e)
			}
			full, e := s.List("harness_" + kind)
			if e != nil {
				t.Fatal(e)
			}
			rows, e := s.ListHarnessMetadata(kind)
			if e != nil {
				t.Fatal(e)
			}
			if len(rows) != 2 || rows[0]["id"] != full[0]["id"] || rows[1]["id"] != full[1]["id"] {
				t.Fatal("metadata changed List ordering", rows)
			}
			projected := rows[0]
			for _, key := range payloadKeys {
				if _, ok := projected[key]; ok {
					t.Fatal("payload returned", key)
				}
			}
			spec := projected["spec"].(map[string]any)
			for _, key := range nestedKeys {
				if _, ok := spec[key]; ok {
					t.Fatal("nested payload returned", key)
				}
			}
			if spec["model"] != "fixture" || spec["hash"] != "manifest" || projected["status"] != "completed" || !reflect.DeepEqual(projected["progress"], full[0]["progress"]) {
				t.Fatal("metadata changed", projected)
			}
			encoded, _ := json.Marshal(rows)
			if len(encoded) > 1024 || strings.Contains(string(encoded), "PAYLOAD-") || strings.Contains(string(encoded), "NESTED-") {
				t.Fatal("metadata response still contains payload bytes", len(encoded))
			}
			detail, e := s.Get("harness_"+kind, "large")
			if e != nil {
				t.Fatal(e)
			}
			got := detail["result"].(map[string]any)["toolCalls"].([]any)[0].(map[string]any)["content"]
			if got != large || !reflect.DeepEqual(detail, full[0]) {
				t.Fatal("Get lost or modified complete immutable detail")
			}
		})
	}
	empty, e := s.ListHarnessMetadata("trial")
	if e != nil || empty == nil {
		t.Fatal("list contract changed", e)
	}
	for _, bad := range []string{"message", "harness_trial", "trial' OR 1=1 --"} {
		if _, e = s.ListHarnessMetadata(bad); e == nil {
			t.Fatal("uncontrolled collection accepted", bad)
		}
	}
}

func TestHarnessMetadataRemovesPayloadBeforeGoDecode(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "projection.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	// SQLite accepts this valid JSON number, but encoding/json cannot decode it
	// into float64. Metadata must succeed because result is removed in SQL. A
	// read-complete-then-strip implementation fails this regression.
	body := `{"id":"proof","status":"failed","result":{"unrepresentable":1e1000},"spec":{"agent":{"unrepresentable":1e1000},"model":"fixture"}}`
	if _, e = s.db.Exec(`INSERT INTO entities(kind,id,body,updated) VALUES(?,?,?,?)`, "harness_trial", "proof", body, "fixed"); e != nil {
		t.Fatal(e)
	}
	rows, e := s.ListHarnessMetadata("trial")
	if e != nil || len(rows) != 1 || rows[0]["status"] != "failed" {
		t.Fatal("projection happened after Go decode", rows, e)
	}
	if _, e = s.Get("harness_trial", "proof"); e == nil {
		t.Fatal("full Get decoding contract unexpectedly changed")
	}
}

func TestHarnessEvaluationProgressIsProjectedWithoutResultPayloads(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "progress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// The unrepresentable number proves no full result is decoded in Go to count
	// these entries. Both failed and successful attempt entries count as recorded.
	body := `{"id":"progress","status":"running","expectedResults":72,"results":[{"grade":{"passed":false},"payload":1e1000},{"grade":{"passed":true},"payload":1e1000}]}`
	if _, err = s.db.Exec(`INSERT INTO entities(kind,id,body,updated) VALUES(?,?,?,?)`, "harness_evaluation", "progress", body, "2026-10-27"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListHarnessMetadata("evaluation")
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	row := rows[0]
	if row["recordedResults"] != int64(2) || row["expectedResults"] != float64(72) {
		t.Fatal("incorrect progress counts", row)
	}
	if _, ok := row["results"]; ok {
		t.Fatal("result payload retained")
	}
	if _, err = s.Get("harness_evaluation", "progress"); err == nil {
		t.Fatal("full Get decoding changed")
	}
	if err = s.Put("harness_evaluation", Doc{"id": "empty", "status": "running", "results": []any{}}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListHarnessMetadata("evaluation")
	if err != nil {
		t.Fatal(err)
	}
	for _, row = range rows {
		if row["id"] == "empty" && row["recordedResults"] != int64(0) {
			t.Fatal("empty progress count incorrect", row)
		}
	}
}
