package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestMessagePagesBoundedOrderedWithoutDuplicates(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "page.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 251; i++ {
		if err = s.Put("message", Doc{"id": fmt.Sprintf("m%03d", i), "sessionId": "selected", "content": fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Put("message", Doc{"id": "other", "sessionId": "other"}); err != nil {
		t.Fatal(err)
	}
	before := ""
	seen := map[string]bool{}
	sizes := []int{}
	for {
		p, err := s.MessagesBefore("selected", before, 100)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(p.Messages))
		for _, d := range p.Messages {
			id := String(d, "id")
			if seen[id] || id == "other" {
				t.Fatal("duplicate/unscoped", id)
			}
			seen[id] = true
		}
		if !p.HasMore {
			break
		}
		if p.Before == before {
			t.Fatal("stuck cursor")
		}
		before = p.Before
	}
	if fmt.Sprint(sizes) != "[100 100 51]" || len(seen) != 251 {
		t.Fatalf("pages %v total %d", sizes, len(seen))
	}
	if _, err = s.MessagesBefore("selected", "malformed", 100); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}
