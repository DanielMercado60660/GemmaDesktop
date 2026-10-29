package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaCopyRetainsBinaryAndRejectsUnsafeInputs(t *testing.T) {
	root := t.TempDir()
	data := []byte{0, 1, 2, 255, 0}
	if e := os.WriteFile(filepath.Join(root, "video.mp4"), data, 0600); e != nil {
		t.Fatal(e)
	}
	w, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	var out bytes.Buffer
	result, e := w.CopyMedia(t.Context(), "video.mp4", &out, 1024)
	if e != nil || !bytes.Equal(out.Bytes(), data) || result.SHA256 != Hash(data) {
		t.Fatal(result, e)
	}
	os.Symlink(filepath.Join(root, "video.mp4"), filepath.Join(root, "link.mp4"))
	for _, path := range []string{"../video.mp4", "link.mp4", ".env"} {
		if _, e = w.CopyMedia(t.Context(), path, &out, 1024); e == nil {
			t.Fatalf("unsafe import accepted: %s", path)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e = w.CopyMedia(ctx, "video.mp4", &out, 1024); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e = w.CopyMedia(t.Context(), "video.mp4", &out, 4); e == nil {
		t.Fatal("oversized source accepted")
	}
}
