package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryAssetWriteChecksRevisionAndNeverSetsExecute(t *testing.T) {
	root := t.TempDir()
	w, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	data := []byte{0, 255, 1, 2}
	c, e := w.WriteBytesContext(t.Context(), "asset.bin", data, "missing")
	if e != nil || c.AfterHash != Hash(data) || !c.Created {
		t.Fatal(c, e)
	}
	b, hash, e := w.ReadBytes("asset.bin")
	if e != nil || !bytes.Equal(b, data) {
		t.Fatal(e)
	}
	info, _ := os.Stat(filepath.Join(root, "asset.bin"))
	if info.Mode()&0111 != 0 {
		t.Fatal("materialized bytes became executable")
	}
	if _, e = w.WriteBytesContext(t.Context(), "asset.bin", []byte{3}, "missing"); !errors.Is(e, ErrConflict) {
		t.Fatal("stale revision accepted", e)
	}
	if _, e = w.WriteBytesContext(t.Context(), "asset.bin", []byte{3}, hash); e != nil {
		t.Fatal(e)
	}
	if _, e = w.WriteBytesContext(t.Context(), "AGENTS.md", data, "missing"); !errors.Is(e, ErrProtected) {
		t.Fatal("protected binary write", e)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	_ = os.WriteFile(outside, data, 0600)
	_ = os.Symlink(outside, filepath.Join(root, "link"))
	if _, e = w.WriteBytesContext(t.Context(), "link", data, "missing"); e == nil {
		t.Fatal("link write accepted")
	}
}
