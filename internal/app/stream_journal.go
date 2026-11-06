package app

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gemmadesktop.local/app/internal/engine"
)

// Only presentation deltas are coalesced. Every authority/checkpoint event
// flushes the pending display text first and is written synchronously.
type displayJournal struct {
	mu         sync.Mutex
	closeOnce  sync.Once
	write      func(engine.Event)
	pending    *engine.Event
	text       strings.Builder
	stop, done chan struct{}
	closed     bool
}

func newDisplayJournal(write func(engine.Event)) *displayJournal {
	j := &displayJournal{write: write, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(j.done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-j.stop:
				return
			case <-ticker.C:
				j.mu.Lock()
				j.flush()
				j.mu.Unlock()
			}
		}
	}()
	return j
}
func (j *displayJournal) flush() {
	if j.pending != nil {
		j.pending.Detail = j.text.String()
		j.write(*j.pending)
		j.pending = nil
		j.text.Reset()
	}
}
func (j *displayJournal) Emit(event engine.Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	if (event.Type != "text" && event.Type != "thinking") || event.Data != nil {
		j.flush()
		j.write(event)
		return
	}
	if j.pending != nil && (j.pending.Type != event.Type || j.pending.NodeID != event.NodeID || j.pending.Title != event.Title || j.pending.Tool != event.Tool) {
		j.flush()
	}
	text := strings.ToValidUTF8(event.Detail, "�")
	for len(text) > 0 {
		if j.pending == nil {
			e := event
			e.Detail = ""
			j.pending = &e
			j.text.Grow(8192)
		}
		n := min(len(text), 8192-j.text.Len())
		for n > 0 && !utf8.ValidString(text[:n]) {
			n--
		}
		if n == 0 {
			j.flush()
			continue
		}
		j.text.WriteString(text[:n])
		text = text[n:]
		if j.text.Len() >= 8192 || len(text) > 0 {
			j.flush()
		}
	}
}
func (j *displayJournal) Close() {
	j.closeOnce.Do(func() {
		close(j.stop)
		<-j.done
		j.mu.Lock()
		defer j.mu.Unlock()
		j.flush()
		j.closed = true
	})
}
