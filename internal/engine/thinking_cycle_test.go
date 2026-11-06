package engine

import (
	"fmt"
	"strings"
	"testing"
)

func cycleFixture() string {
	return strings.Repeat(strings.Repeat("a", 127)+"\n"+strings.Repeat("b", 127)+"\n", 8)
}

func TestThinkingCycleFrozenThresholdsAndFragmentation(t *testing.T) {
	for _, chunk := range []int{1, 7, 128, 4096} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			text := cycleFixture()
			g := thinkingCycleGuard{}
			var match *thinkingCycleMatch
			for i := 0; i < len(text) && match == nil; i += chunk {
				match = g.Add(text[i:min(i+chunk, len(text))])
			}
			if match == nil || match.PatternLines != 2 || match.Repetitions != 8 || match.RepeatedBytes != 2048 || match.BytesSeen != 2048 || match.Version != thinkingCycleVersion {
				t.Fatalf("frozen threshold changed: %+v", match)
			}
		})
	}
	for name, text := range map[string]string{
		"seven repeats":         strings.Repeat(strings.Repeat("a", 127)+"\n"+strings.Repeat("b", 127)+"\n", 7),
		"less than 2048 bytes":  strings.Repeat(strings.Repeat("a", 126)+"\n"+strings.Repeat("b", 126)+"\n", 8),
		"incomplete final line": strings.TrimSuffix(cycleFixture(), "\n"),
		"fenced code":           "```go\n" + cycleFixture() + "```\n",
		"tilde fenced code":     "~~~\n" + cycleFixture() + "~~~\n",
	} {
		t.Run(name, func(t *testing.T) {
			if match := (&thinkingCycleGuard{}).Add(text); match != nil {
				t.Fatalf("false trigger: %+v", match)
			}
		})
	}
	var enumeration strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&enumeration, "Distinct request row %d: %s\n", i, strings.Repeat("ordinary detail ", 10))
	}
	if match := (&thinkingCycleGuard{}).Add(enumeration.String()); match != nil {
		t.Fatal("nonrepeating enumeration is not a cycle", match)
	}
}

func TestThinkingCycleBlockWidthWindowAndWhitespace(t *testing.T) {
	for _, width := range []int{2, 9, 12, 13} {
		var block strings.Builder
		for i := 0; i < width; i++ {
			fmt.Fprintf(&block, " line %d %s \r\n", i, strings.Repeat("x", 130))
		}
		g := thinkingCycleGuard{}
		match := g.Add(strings.Repeat(block.String(), 8))
		if width <= 12 && (match == nil || match.PatternLines != width) {
			t.Fatalf("width %d: %+v", width, match)
		}
		if width > 12 && match != nil {
			t.Fatal("out-of-window cycle matched", match)
		}
		if g.lineCount > 96 {
			t.Fatal("window exceeded")
		}
	}
	// Blank lines do not advance or reset the frozen nonempty-line window.
	text := strings.ReplaceAll(cycleFixture(), "\n", "\n\n")
	if match := (&thinkingCycleGuard{}).Add(text); match == nil || match.RepeatedBytes != 2048 {
		t.Fatal("blank-line semantics changed", match)
	}
}

func TestThinkingCycleFenceDelimitersIgnoreEmbeddedAndOppositeMarkers(t *testing.T) {
	for name, fence := range map[string]struct{ open, inside, close string }{
		"embedded marker":      {"```python\n", "marker = \"```\"\n", "```\n"},
		"opposite marker":      {"```python\n", "~~~\n", "```\n"},
		"short marker":         {"````python\n", "```\n", "````\n"},
		"closing text":         {"```python\n", "``` extra\n", "```\n"},
		"tilde nested markers": {"~~~~\n", "```\n~~~\nmarker = \"~~~~\"\n", "~~~~~\n"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, chunk := range []int{1, 17, 10000} {
				g := thinkingCycleGuard{}
				text := fence.open + fence.inside + cycleFixture() + fence.close
				for i := 0; i < len(text); i += chunk {
					if match := g.Add(text[i:min(i+chunk, len(text))]); match != nil {
						t.Fatalf("chunk %d: fenced code triggered: %+v", chunk, match)
					}
				}
				if match := g.Add(cycleFixture()); match == nil {
					t.Fatalf("chunk %d: matching close did not restore prose detection", chunk)
				}
			}
		})
	}
	g := thinkingCycleGuard{}
	if match := g.Add("An inline ``` marker is not a fence.\n" + cycleFixture()); match == nil {
		t.Fatal("inline marker incorrectly opened a code block")
	}
}

func TestThinkingCycleOversizedLinesCannotForgeMatchesOrLoseFences(t *testing.T) {
	g := thinkingCycleGuard{}
	if g.Add(strings.Repeat("x", thinkingCycleMaxLineBytes+1)) != nil || len(g.tail) > thinkingCycleMaxLineBytes || len(g.fenceTail) > 2 || !g.oversized {
		t.Fatal("partial-line storage unbounded")
	}
	if match := g.Add("```\n" + cycleFixture() + "```\n"); match != nil {
		t.Fatal("oversized fence lost", match)
	}
	if !g.suppressed || g.Add(cycleFixture()) != nil {
		t.Fatal("ambiguous oversized fence did not suppress this attempt's detector")
	}
	g = thinkingCycleGuard{}
	for i := 0; i < 100; i++ {
		if match := g.Add(strings.Repeat("x", thinkingCycleMaxLineBytes+1) + "\n"); match != nil {
			t.Fatal("oversized line became synthetic repetition", match)
		}
	}
	if g.lineCount != 0 || len(g.tail) != 0 {
		t.Fatal("oversized lines retained")
	}
	if match := g.Add(cycleFixture()); match == nil {
		t.Fatal("oversized non-fence line suppressed later ordinary cycle")
	}
}
