package store

import (
	"path/filepath"
	"testing"
)

func TestVulnerableWALEngineIsRejected(t *testing.T) {
	for _, v := range []string{"3.51.2", "3.50.6", "invalid"} {
		if walVersionSupported(v) {
			t.Fatal("unqualified engine accepted", v)
		}
	}
	for _, v := range []string{"3.51.3", "3.54.1"} {
		if !walVersionSupported(v) {
			t.Fatal("fixed engine rejected", v)
		}
	}
}

func TestRecoveryPreservesEvidenceAndExpiresApproval(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	s, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	t.Log("SQLite", s.Version)
	if e = s.Put("run", Doc{"id": "r", "status": "running"}); e != nil {
		t.Fatal(e)
	}
	s.Put("approval", Doc{"id": "a", "runId": "r", "status": "pending"})
	first, e := s.Event(Doc{"runId": "r", "type": "tool_started", "title": "Write intended"})
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Recover(); e != nil {
		t.Fatal(e)
	}
	r, _ := s.Get("run", "r")
	a, _ := s.Get("approval", "a")
	if r["status"] != "interrupted" || a["status"] != "expired" {
		t.Fatal(r, a)
	}
	events, e := s.Events("r", 0)
	if e != nil || len(events) != 2 || events[0]["seq"] != first["seq"] {
		t.Fatal(events, e)
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if e = s.Backup(backup); e != nil {
		t.Fatal(e)
	}
	b, e := Open(backup)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	r, _ = b.Get("run", "r")
	if r["status"] != "interrupted" {
		t.Fatal(r)
	}
}
