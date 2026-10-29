package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivityByteCapKeepsRecentDiagnosticsAndPaginatedHistory(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Put("run", Doc{"id": "run", "sessionId": "chat"}); err != nil {
		t.Fatal(err)
	}
	// Simulate already journaled full legacy tool events. Direct SQL avoids the
	// newer Event display-payload truncation without changing production limits.
	payload := strings.Repeat("complete source line\n", 15000)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		body, _ := json.Marshal(Doc{"runId": "run", "type": "tool_completed", "title": fmt.Sprintf("tool-%d", i), "data": Doc{"payload": payload}})
		if _, err := tx.Exec(`INSERT INTO events(id,run_id,body) VALUES(?, 'run', ?)`, ID(), string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, event := range []Doc{{"type": "approval_requested", "title": "Recent approval"}, {"type": "tool_failed", "title": "Latest failure"}} {
		event["runId"] = "run"
		if _, err := s.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		if _, err := s.Event(Doc{"runId": "run", "type": "text", "detail": "newer token output"}); err != nil {
			t.Fatal(err)
		}
	}
	activity, err := s.Activity()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(activity)
	if len(activity) < 3 || len(activity) >= 42 || len(encoded) > maxActivityBytes {
		t.Fatalf("activity is not bounded: events=%d bytes=%d", len(activity), len(encoded))
	}
	if activity[len(activity)-2]["title"] != "Recent approval" || activity[len(activity)-1]["title"] != "Latest failure" {
		t.Fatal("latest approval/failure evicted by large older events")
	}
	var previous int64
	for _, event := range activity {
		if event["seq"].(int64) <= previous {
			t.Fatal("display order is not chronological")
		}
		previous = event["seq"].(int64)
		if event["type"] == "tool_completed" && event["data"].(map[string]any)["payload"] != payload {
			t.Fatal("individual tool event was fragmented")
		}
	}
	// The activity tail does not delete or hide events from session pagination.
	count, cursor := 0, int64(0)
	for {
		page, err := s.SessionEvents("chat", cursor, 1000)
		if err != nil {
			t.Fatal(err)
		}
		count += len(page.Events)
		if !page.HasMore {
			break
		}
		if page.Cursor <= cursor {
			t.Fatal("pagination stalled")
		}
		cursor = page.Cursor
	}
	if count != 62 {
		t.Fatalf("session history lost events: %d", count)
	}
}

func TestActivityKeepsOneOversizedNewestEventIntact(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Event(Doc{"runId": "run", "type": "approval_requested", "title": "Earlier approval"}); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("x", maxActivityBytes+1000)
	body, _ := json.Marshal(Doc{"runId": "run", "type": "tool_failed", "title": "Oversized latest failure", "detail": payload})
	if _, err := s.db.Exec(`INSERT INTO events(id,run_id,body) VALUES(?, 'run', ?)`, ID(), string(body)); err != nil {
		t.Fatal(err)
	}
	activity, err := s.Activity()
	if err != nil || len(activity) != 1 || activity[0]["detail"] != payload || activity[0]["title"] != "Oversized latest failure" {
		t.Fatalf("soft cap must preserve exactly the newest complete event: count=%d err=%v", len(activity), err)
	}
}

func TestSessionCursorRetainsHistoryPastRawEventWindow(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, r := range []Doc{{"id": "a", "sessionId": "chat"}, {"id": "b", "sessionId": "chat"}, {"id": "other", "sessionId": "elsewhere"}} {
		if err = s.Put("run", r); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.Event(Doc{"runId": "a", "type": "tool_failed", "title": "Original diagnostic"})
	if err != nil {
		t.Fatal(err)
	}
	// One transaction keeps the high-volume regression fixture inexpensive.
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3100; i++ {
		_, err = tx.Exec(`INSERT INTO events(id,run_id,body) VALUES(?, 'a', '{"runId":"a","type":"thinking","detail":"noise"}')`, ID())
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []Doc{{"runId": "a", "type": "text", "detail": "start"}, {"runId": "other", "type": "text", "detail": "private"}, {"runId": "b", "type": "tool_completed", "title": "Later result"}} {
		if _, err = s.Event(d); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.SessionEvents("chat", 0, 2)
	if err != nil || len(p.Events) != 2 || !p.HasMore || p.Events[0]["seq"] != first["seq"] {
		t.Fatal(p, err)
	}
	q, err := s.SessionEvents("chat", p.Cursor, 2)
	if err != nil || len(q.Events) != 1 || q.HasMore || q.Events[0]["title"] != "Later result" || q.Cursor <= p.Cursor {
		t.Fatal(q, err)
	}
	if _, err = s.Event(Doc{"runId": "b", "type": "text", "detail": "next"}); err != nil {
		t.Fatal(err)
	}
	next, err := s.SessionEvents("chat", q.Cursor, 2)
	if err != nil || len(next.Events) != 1 || next.Events[0]["detail"] != "next" {
		t.Fatal(next, err)
	}
	activity, err := s.Activity()
	if err != nil || len(activity) != 2 || activity[0]["title"] != "Original diagnostic" {
		t.Fatal(activity, err)
	}
	if _, err = s.SessionEvents("chat", -1, 2); err == nil {
		t.Fatal("negative cursor accepted")
	}
}

func TestGenerationMetricsRemainAuditOnlyAcrossPresentationPages(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Put("run", Doc{"id": "run", "sessionId": "chat"}); err != nil {
		t.Fatal(err)
	}
	appendVisible := func(title string) Doc {
		t.Helper()
		event, err := s.Event(Doc{"runId": "run", "type": "tool_failed", "title": title})
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	appendMetrics := func(count int) {
		t.Helper()
		tx, err := s.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for i := 0; i < count; i++ {
			kind := []string{"generation_metrics", "context_prepared", "token_accounting"}[i%3]
			if _, err := tx.Exec(`INSERT INTO events(id,run_id,body) VALUES(?, 'run', json_object('runId','run','type',?,'data',json_object('evalCount',8192)))`, ID(), kind); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	first := appendVisible("First diagnostic")
	// This exceeds both presentation query limits, so filtering after LIMIT
	// would starve the next visible event and the activity tail.
	appendMetrics(2100)
	second := appendVisible("Second diagnostic")
	appendMetrics(600)
	third := appendVisible("Latest diagnostic")
	appendMetrics(600)

	cursor := int64(0)
	for i, expected := range []Doc{first, second, third} {
		page, err := s.SessionEvents("chat", cursor, 1)
		if err != nil || len(page.Events) != 1 || page.Events[0]["seq"] != expected["seq"] || page.Cursor != expected["seq"] || page.HasMore != (i < 2) {
			t.Fatalf("visible page %d was displaced by metrics: page=%+v err=%v", i, page, err)
		}
		cursor = page.Cursor
	}
	empty, err := s.SessionEvents("chat", cursor, 1)
	if err != nil || len(empty.Events) != 0 || empty.HasMore || empty.Cursor != cursor {
		t.Fatalf("metrics-only tail changed the visible cursor: page=%+v err=%v", empty, err)
	}
	activity, err := s.Activity()
	if err != nil || len(activity) != 3 || activity[0]["seq"] != first["seq"] || activity[2]["seq"] != third["seq"] {
		t.Fatalf("metrics starved the activity tail: activity=%+v err=%v", activity, err)
	}
	// Presentation filtering must not delete diagnostics needed for audit.
	audit, err := s.Events("run", third["seq"].(int64))
	if err != nil || len(audit) != 600 {
		t.Fatalf("raw generation metrics were lost: count=%d err=%v", len(audit), err)
	}
	for _, event := range audit {
		if (event["type"] != "generation_metrics" && event["type"] != "context_prepared" && event["type"] != "token_accounting") || event["data"].(map[string]any)["evalCount"] != float64(8192) {
			t.Fatal("raw metric content changed")
		}
	}
	contextAudit, err := s.ContextAudit("run")
	if err != nil {
		t.Fatal(err)
	}
	accountingRetained := false
	for _, event := range contextAudit.Events {
		accountingRetained = accountingRetained || event["type"] == "token_accounting"
	}
	if !accountingRetained {
		t.Fatal("token accounting must remain inspectable in the bounded context audit")
	}
	newEvent := appendVisible("Newly appended diagnostic")
	page, err := s.SessionEvents("chat", empty.Cursor, 1)
	if err != nil || len(page.Events) != 1 || page.Events[0]["seq"] != newEvent["seq"] || page.HasMore {
		t.Fatalf("visible append after metrics-only tail was skipped: page=%+v err=%v", page, err)
	}
}
