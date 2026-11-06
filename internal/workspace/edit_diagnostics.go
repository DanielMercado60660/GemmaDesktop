package workspace

import (
	"fmt"
	"strings"
)

// Diagnostics never relax matching. A unique complete first line gives us an
// anchor for a useful mismatch location; otherwise do not guess which region
// the caller intended. Bound both the scan's comparisons and returned excerpts.
func replacementMismatchHint(file File, old string) string {
	hint := " Current hash: " + file.Hash + "."
	want := strings.SplitN(old, "\n", 257)
	if len(want) < 2 || len(want) > 256 || strings.TrimSpace(want[0]) == "" {
		return hint
	}
	normalize := func(s string) string { return strings.TrimRight(s, " \t\r") }
	have := strings.Split(file.Content, "\n")
	firstLine := normalize(want[0])
	anchor := -1
	for i, line := range have {
		if normalize(line) == firstLine {
			if anchor >= 0 {
				return hint // identical anchors would make the suggested line ambiguous
			}
			anchor = i
		}
	}
	if anchor < 0 {
		return hint
	}
	for offset, expected := range want {
		line := anchor + offset
		if line >= len(have) {
			return hint + fmt.Sprintf(" After the unique first-line anchor at line %d, the file ends before oldText line %d (%q).", anchor+1, offset+1, diagnosticExcerpt(expected))
		}
		if normalize(have[line]) != normalize(expected) {
			return hint + fmt.Sprintf(" First differing line after the unique anchor: file line %d (oldText line %d); expected %q, current %q.", line+1, offset+1, diagnosticExcerpt(expected), diagnosticExcerpt(have[line]))
		}
	}
	return hint
}

func diagnosticExcerpt(s string) string {
	runes := []rune(s)
	if len(runes) > 160 {
		return string(runes[:160]) + "…"
	}
	return s
}
