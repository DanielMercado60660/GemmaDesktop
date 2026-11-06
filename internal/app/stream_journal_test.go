package app

import (
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/store"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestDisplayJournalFlushesBeforeAuthorityAndPreservesExactUnicode(t *testing.T) {
	var events []engine.Event
	j := newDisplayJournal(func(e engine.Event) { events = append(events, e) })
	text := strings.Repeat("café🙂", 2000)
	j.Emit(engine.Event{Type: "text", Detail: text[:5]})
	j.Emit(engine.Event{Type: "text", Detail: text[5:]})
	j.Emit(engine.Event{Type: "tool_started", Tool: "write_file"})
	j.Emit(engine.Event{Type: "thinking", Detail: "retained on close"})
	j.Close()
	j.Close()
	var actual strings.Builder
	for _, e := range events {
		if e.Type == "text" {
			if len(e.Detail) > 8192 || !utf8.ValidString(e.Detail) {
				t.Fatal("invalid bounded display event")
			}
			actual.WriteString(e.Detail)
		}
	}
	if actual.String() != text {
		t.Fatal("text changed during coalescing")
	}
	if events[len(events)-2].Type != "tool_started" || events[len(events)-1].Type != "thinking" {
		t.Fatal("authority ordering changed", events)
	}
}

func BenchmarkDurableDisplayStream(b *testing.B) {
	for _, coalesced := range []bool{false, true} {
		name := "legacy_each_delta"
		if coalesced {
			name = "bounded_coalescing"
		}
		b.Run(name, func(b *testing.B) {
			db, err := store.Open(filepath.Join(b.TempDir(), "stream.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			writes := 0
			write := func(e engine.Event) {
				if _, err := db.Event(store.Doc{"type": e.Type, "detail": e.Detail, "runId": "benchmark"}); err != nil {
					panic(err)
				}
				writes++
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var j *displayJournal
				emit := write
				if coalesced {
					j = newDisplayJournal(write)
					emit = j.Emit
				}
				for i := 0; i < 2048; i++ {
					emit(engine.Event{Type: "text", Detail: "generated token "})
				}
				emit(engine.Event{Type: "tool_started", Tool: "read_file"})
				if j != nil {
					j.Close()
				}
			}
			b.ReportMetric(float64(writes)/float64(b.N), "writes/op")
		})
	}
}

func TestDisplayJournalFlushesWhileModelIsIdle(t *testing.T) {
	got := make(chan engine.Event, 1)
	j := newDisplayJournal(func(e engine.Event) { got <- e })
	defer j.Close()
	j.Emit(engine.Event{Type: "text", Detail: "first token"})
	select {
	case e := <-got:
		if e.Detail != "first token" {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("idle stream retained text indefinitely")
	}
}

func TestDisplayJournalConcurrentClose(t *testing.T) {
	j := newDisplayJournal(func(engine.Event) {})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				j.Emit(engine.Event{Type: "text", Detail: "x"})
			}
		}()
	}
	wg.Wait()
	j.Close()
}
