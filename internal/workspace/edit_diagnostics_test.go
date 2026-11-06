package workspace

import (
	"strings"
	"testing"
)

func TestReplacementMismatchIdentifiesUniqueAnchorWithoutEditing(t *testing.T) {
	w, _ := openTest(t)
	before := "header\ndef solve():\n    return 7\n"
	f, err := w.Write("source.py", before, "missing")
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Replace("source.py", "def solve():\n    return 3", "replacement", f.AfterHash)
	for _, want := range []string{f.AfterHash, "file line 3 (oldText line 2)", `expected "    return 3", current "    return 7"`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %q in %v", want, err)
		}
	}
	got, _ := w.Read("source.py")
	if got.Content != before {
		t.Fatal("diagnostic changed the file")
	}
}

func TestReplacementMismatchDoesNotGuessAmbiguousAnchor(t *testing.T) {
	w, _ := openTest(t)
	f, _ := w.Write("source.py", "block\na\nblock\nb\n", "missing")
	_, err := w.Replace("source.py", "block\nc", "replacement", f.AfterHash)
	if err == nil || !strings.Contains(err.Error(), f.AfterHash) || strings.Contains(err.Error(), "differing line") {
		t.Fatalf("ambiguous diagnostic: %v", err)
	}
}

func TestReplacementMismatchBoundsExcerpts(t *testing.T) {
	w, _ := openTest(t)
	f, _ := w.Write("source.py", "anchor\n"+strings.Repeat("😀", 10000), "missing")
	_, err := w.Replace("source.py", "anchor\n"+strings.Repeat("x", 10000), "replacement", f.AfterHash)
	if err == nil || len(err.Error()) > 1500 || !strings.Contains(err.Error(), "…") {
		t.Fatalf("unbounded diagnostic length: %v", err)
	}
}
