package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedHistoryAndEntityDeletionCursor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 1000; i++ {
		if err = s.Put("message", Doc{"id": fmt.Sprint(i), "sessionId": "unrelated", "content": "irrelevant history"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Put("message", Doc{"id": "selected", "sessionId": "selected", "content": "needed"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListScoped("message", "sessionId", "selected")
	if err != nil || len(rows) != 1 || String(rows[0], "content") != "needed" {
		t.Fatalf("scope: %v %v", rows, err)
	}
	if _, err = s.ListScoped("message", "sessionId') OR 1=1 --", "selected"); err == nil {
		t.Fatal("arbitrary field accepted")
	}
	before, err := s.EntityCursor()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put("session", Doc{"id": "gone"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("secret", Doc{"id": "private", "credential": "never include"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete("session", "gone"); err != nil {
		t.Fatal(err)
	}
	p, err := s.EntityChanges(before, 200)
	if err != nil || len(p.Changes) != 2 || !p.Changes[1].Deleted {
		t.Fatalf("changes: %+v %v", p, err)
	}
	for _, c := range p.Changes {
		if c.Kind == "secret" {
			t.Fatal("private entity in presentation changes")
		}
	}
	if p.Cursor <= before {
		t.Fatal("cursor did not advance through filtered records")
	}
}

func TestMutateConflictLeavesOriginalRecord(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "mutation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.Mutate("draft", "a", func(old Doc) (Doc, error) { return Doc{"id": "a", "text": "original"}, nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Mutate("draft", "a", func(old Doc) (Doc, error) { old["text"] = "bad"; return old, errors.New("conflict") })
	if err == nil {
		t.Fatal("mutation succeeded")
	}
	d, err := s.Get("draft", "a")
	if err != nil || String(d, "text") != "original" {
		t.Fatalf("rollback: %v %v", d, err)
	}
}

func BenchmarkSelectedSessionHistory(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "history.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	// 1000 conversations, ten 2-KiB messages each. Preparation is excluded.
	content := strings.Repeat("x", 2048)
	for i := 0; i < 10000; i++ {
		if err = s.Put("message", Doc{"id": fmt.Sprint(i), "sessionId": fmt.Sprint(i % 1000), "content": content}); err != nil {
			b.Fatal(err)
		}
	}
	b.Run("legacy_decode_all", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			all, err := s.List("message")
			if err != nil {
				b.Fatal(err)
			}
			n := 0
			for _, d := range all {
				if String(d, "sessionId") == "1" {
					n++
				}
			}
			if n != 10 {
				b.Fatal(n)
			}
		}
	})
	b.Run("indexed_selected", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rows, err := s.ListScoped("message", "sessionId", "1")
			if err != nil || len(rows) != 10 {
				b.Fatal(err, len(rows))
			}
		}
	})
	b.Run("bounded_page", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			page, err := s.MessagesBefore("1", "", 100)
			if err != nil || len(page.Messages) != 10 {
				b.Fatal(err, len(page.Messages))
			}
		}
	})
}
