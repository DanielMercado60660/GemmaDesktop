package store

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPublishVersionConcurrentCASAndRollback(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "revision.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.PublishVersion("capability", "p", 0, Doc{"name": "first"}); e != nil {
		t.Fatal(e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.PublishVersion("capability", "p", 1, Doc{"name": "candidate", "candidate": i}); err == nil {
				wins.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("concurrent stale updates won %d times", wins.Load())
	}
	first, _ := s.Get("capability_revision", "p:1")
	head, _ := s.Get("capability", "p")
	second, _ := s.Get("capability_revision", "p:2")
	if first["name"] != "first" || head["candidate"] != second["candidate"] {
		t.Fatal(first, head, second)
	}
	_, e = s.db.Exec(`CREATE TRIGGER reject_capability_head BEFORE UPDATE ON entities WHEN NEW.kind='capability' BEGIN SELECT RAISE(ABORT,'fixture reject'); END`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishVersion("capability", "p", 2, Doc{"name": "must rollback"}); e == nil {
		t.Fatal("failed head publication accepted")
	}
	if _, e = s.Get("capability_revision", "p:3"); e == nil {
		t.Fatal("orphan immutable revision committed despite head failure")
	}
	after, _ := s.Get("capability", "p")
	if after["revision"] != head["revision"] {
		t.Fatal("head advanced after rollback")
	}
}

func TestDurableIdentityCanOnlyBeClaimedOnce(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "claim.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.CreateOnce("delegation", "call", Doc{"status": "running"}) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("duplicate durable identity admitted", wins.Load())
	}
	_ = s.Put("delegation", Doc{"id": "call", "status": "completed"})
	if s.CreateOnce("delegation", "call", Doc{"status": "running"}) == nil {
		t.Fatal("completed operation was restarted")
	}
}
