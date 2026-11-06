package engine

import (
	"crypto/sha256"
	"errors"
	"strings"
)

const thinkingCycleVersion = "thinking_cycle_v1"
const thinkingCycleMaxLineBytes = 64 << 10
const thinkingCycleWindowLines = 96
const thinkingCycleMinBytes = 2048
const thinkingCycleRepeats = 8

var ErrThinkingCycle = errors.New("experimental thinking-cycle guard stopped a repeated thinking block")

type thinkingCycleMatch struct {
	Version       string `json:"version"`
	PatternLines  int    `json:"patternLines"`
	Repetitions   int    `json:"repetitions"`
	RepeatedBytes int    `json:"repeatedBytes"`
	BytesSeen     int    `json:"bytesSeen"`
}

// The callback owns this exact pointer. Only its unmodified return from the
// backend proves clean interruption; a joined cleanup error is not retryable.
type thinkingCycleStop struct{ match thinkingCycleMatch }

func (*thinkingCycleStop) Error() string { return ErrThinkingCycle.Error() }
func (*thinkingCycleStop) Unwrap() error { return ErrThinkingCycle }

type thinkingCycleLine struct {
	digest [32]byte
	bytes  int
}

// This experiment exactly matches the frozen offline line/block thresholds.
// It inspects only the thinking stream supplied by the adapter. Keeping line
// identities and lengths bounds the window independently of source line size.
type thinkingCycleGuard struct {
	tail       string
	fenceTail  string
	lineFence  bool
	oversized  bool
	fence      byte
	fenceSize  int
	suppressed bool
	bytesSeen  int
	lines      [thinkingCycleWindowLines]thinkingCycleLine
	lineCount  int
}

func (g *thinkingCycleGuard) Add(delta string) *thinkingCycleMatch {
	if g.suppressed {
		return nil
	}
	for len(delta) > 0 {
		newline := strings.IndexByte(delta, '\n')
		part, complete := delta, newline >= 0
		if complete {
			part = delta[:newline]
		}
		g.bytesSeen += len(part)
		// Track fence markers even in a line too large to retain, so dropping
		// that line cannot accidentally turn fenced source into ordinary text.
		fences := g.fenceTail + part
		g.lineFence = g.lineFence || strings.Contains(fences, "```") || strings.Contains(fences, "~~~")
		g.fenceTail = strings.Clone(fences[max(0, len(fences)-2):])
		if !g.oversized {
			if len(g.tail)+len(part) > thinkingCycleMaxLineBytes {
				g.tail, g.oversized, g.lineCount = "", true, 0
			} else {
				g.tail += part
			}
		}
		if !complete {
			return nil
		}
		g.bytesSeen++ // completed newline belongs to the observed stream
		line, fence, oversized := strings.TrimSpace(g.tail), g.lineFence, g.oversized
		g.tail, g.fenceTail, g.lineFence, g.oversized = "", "", false, false
		delta = delta[newline+1:]
		if oversized {
			g.lineCount = 0
			if fence {
				// The dropped line may contain a delimiter, but its role cannot
				// be established safely. Disable this optional detector for the
				// attempt rather than accidentally inspecting fenced source.
				g.suppressed = true
				return nil
			}
			continue
		}
		marker, size, rest := thinkingFenceDelimiter(line)
		if g.fence != 0 {
			// Embedded markers, opposite-type fences and shorter delimiters
			// cannot close the current code block.
			if marker == g.fence && size >= g.fenceSize && rest == "" {
				g.fence, g.fenceSize = 0, 0
			}
			continue
		}
		if marker != 0 && (marker != '`' || !strings.ContainsRune(rest, '`')) {
			g.fence, g.fenceSize, g.lineCount = marker, size, 0
			continue
		}
		if fence {
			// Inline fence-like text is not a delimiter. Exclude the ambiguous
			// line without changing code state or joining patterns across it.
			g.lineCount = 0
			continue
		}
		if line == "" {
			continue
		}
		if g.lineCount == len(g.lines) {
			copy(g.lines[:], g.lines[1:])
			g.lineCount--
		}
		g.lines[g.lineCount] = thinkingCycleLine{sha256.Sum256([]byte(line)), len(line) + 1}
		g.lineCount++
		for width := 2; width <= 12; width++ {
			n := width * thinkingCycleRepeats
			if g.lineCount < n {
				continue
			}
			segment := g.lines[g.lineCount-n : g.lineCount]
			bytes, same := 0, true
			for i, record := range segment {
				bytes += record.bytes
				if record != segment[len(segment)-width+i%width] {
					same = false
					break
				}
			}
			if same && bytes >= thinkingCycleMinBytes {
				return &thinkingCycleMatch{thinkingCycleVersion, width, thinkingCycleRepeats, bytes, g.bytesSeen}
			}
		}
	}
	return nil
}

// The exploratory prototype toggled on embedded markers. Production v1 uses
// delimiter lines and matching fence lengths to keep code out of prose checks;
// this parser safety correction does not change the frozen cycle thresholds.
func thinkingFenceDelimiter(line string) (marker byte, size int, rest string) {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, 0, ""
	}
	marker = line[0]
	for size < len(line) && line[size] == marker {
		size++
	}
	if size < 3 {
		return 0, 0, ""
	}
	return marker, size, strings.TrimSpace(line[size:])
}
