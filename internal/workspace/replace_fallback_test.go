package workspace

import (
	"strings"
	"testing"
)

func TestReplaceFallbackNormalizesOnlyLineEnds(t *testing.T) {
	for _, c := range []struct {
		name, content, old, new, want, err string
		normalized                         bool
	}{
		{name: "exact match unchanged", content: "a = 1\nb = 2\n", old: "b = 2", new: "b = 3", want: "a = 1\nb = 3\n"},
		{name: "trailing whitespace in file", content: "def f():   \n    return 1  \n\nx = 2\n", old: "def f():\n    return 1\n", new: "def f():\n    return 2\n", want: "def f():\n    return 2\n\nx = 2\n", normalized: true},
		{name: "trailing whitespace in oldText", content: "start\nvalue = 1\nend\n", old: "value = 1  \t", new: "value = 2", want: "start\nvalue = 2\nend\n", normalized: true},
		{name: "crlf file lf oldText", content: "keep\r\na\r\nb\r\nc\r\n", old: "a\nb\n", new: "A\nB\n", want: "keep\r\nA\nB\nc\r\n", normalized: true},
		{name: "lf file crlf oldText", content: "a\nb\nc\n", old: "a\r\nb", new: "X", want: "X\nc\n", normalized: true},
		{name: "replaces the matching middle span", content: "start\nfoo  \nbar\nend\n", old: "foo\nbar", new: "X", want: "start\nX\nend\n", normalized: true},
		{name: "two normalized matches", content: "foo \nbar\nfoo\t\nbar\n", old: "foo\nbar", err: "oldText matches 2 places in f.py; include more surrounding lines"},
		{name: "two exact matches", content: "x\nx\n", old: "x", err: "oldText matches 2 places in f.py"},
		{name: "indentation differs", content: "if x:\n\treturn 1\n", old: "if x:\n    return 1", err: "oldText was not found in f.py. Re-read the exact lines with read_file (startLine/lineCount)"},
		{name: "deeper indentation differs", content: "if x:\n  return 1  \n", old: "if x:\n    return 1\n", err: "was not found"},
		{name: "interior whitespace differs", content: "a  = 1 \n", old: "a = 1\n", err: "was not found"},
		{name: "dropped tail must end a line", content: "x = 10\n", old: "x = 1 ", err: "was not found"},
		{name: "whitespace only oldText", content: "a\n\nb\n", old: " \n \n", err: "was not found"},
		{name: "json escapes hint", content: "first\nsecond\n", old: `first\nsecond`, err: "JSON escape"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := openTest(t)
			f, err := w.Write("f.py", c.content, "missing")
			if err != nil {
				t.Fatal(err)
			}
			plan, err := w.LocateReplacement("f.py", c.old, f.AfterHash)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("want error containing %q, got %v", c.err, err)
				}
				if _, err = w.Replace("f.py", c.old, "changed", f.AfterHash); err == nil {
					t.Fatal("Replace accepted what validation rejected")
				}
				if got, _ := w.Read("f.py"); got.Content != c.content {
					t.Fatalf("rejected edit changed file: %q", got.Content)
				}
				return
			}
			if err != nil || plan.Normalized != c.normalized {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
			change, err := w.Replace("f.py", c.old, c.new, f.AfterHash)
			if err != nil || change.After != c.want || change.Before != c.content {
				t.Fatalf("got %q, want %q (err %v)", change.After, c.want, err)
			}
			if c.content[:plan.Start]+c.new+c.content[plan.End:] != c.want {
				t.Fatal("validated span differs from the applied span")
			}
		})
	}
}

func TestReplaceFallbackKeepsRevisionCheck(t *testing.T) {
	w, _ := openTest(t)
	f, err := w.Write("f.py", "a  \nb\n", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Replace("f.py", "a\nb", "c", Hash([]byte("other"))); err != ErrConflict {
		t.Fatalf("stale fallback edit accepted: %v", err)
	}
	if got, _ := w.Read("f.py"); got.Hash != f.AfterHash {
		t.Fatal("stale edit changed file")
	}
}
