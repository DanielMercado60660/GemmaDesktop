package app

import (
	"context"
	"path/filepath"
	"testing"

	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
)

func TestOllamaDownloadRecoveryDoesNotReplayExternalWork(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client, err := inference.NewClient("http://127.0.0.1:11434")
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{Store: db, client: client, pullState: map[string]Doc{}, pulls: map[string]context.CancelFunc{}}
	if err = db.Put("ollama_download", Doc{"id": "job", "name": "fixture", "state": "running", "endpointDigest": contentDigest(client.BaseURL())}); err != nil {
		t.Fatal(err)
	}
	if err = c.recoverOllamaDownloads(); err != nil {
		t.Fatal(err)
	}
	if c.pullState["fixture"]["state"] != "interrupted" || len(c.pulls) != 0 {
		t.Fatal("interrupted download replayed", c.pullState)
	}
	job, err := db.Get("ollama_download", "job")
	if err != nil || job["state"] != "interrupted" {
		t.Fatal(job, err)
	}
}
