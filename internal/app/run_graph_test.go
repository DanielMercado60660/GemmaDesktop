package app

import (
	"errors"
	"path/filepath"
	"testing"

	"gemmadesktop.local/app/internal/store"
)

func TestRunGraphFacadeIsReadOnlyAndRequiresAnExistingRun(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := &Core{Store: db}
	if _, err := c.Call(t.Context(), "run.graph", Doc{"id": "unknown"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
	if err := db.Put("run", Doc{"id": "r", "status": "completed"}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Call(t.Context(), "run.graph", Doc{"id": "r"})
	if err != nil {
		t.Fatal(err)
	}
	doc := obj(got)
	if doc["runId"] != "r" || doc["source"] != "events" || doc["workflow"] != nil {
		t.Fatalf("unexpected API: %v", doc)
	}
	saved, err := db.Get("run", "r")
	if err != nil || saved["status"] != "completed" {
		t.Fatal("graph read changed run")
	}
}
