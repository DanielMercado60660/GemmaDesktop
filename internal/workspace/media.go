package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

type MediaSource struct {
	Path   string
	SHA256 string
	Bytes  int64
}
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// CopyMedia streams one protected, pinned regular file to a caller-owned private
// destination. The second hash detects an in-place edit during import; the copy's
// own hash is always authoritative, never a later pathname read.
func (w *Workspace) CopyMedia(ctx context.Context, name string, dst io.Writer, limit int64) (MediaSource, error) {
	if limit < 1 || limit > 256<<20 {
		return MediaSource{}, errors.New("invalid media import size limit")
	}
	r, base, err := w.parent(name, false)
	if err != nil {
		return MediaSource{}, err
	}
	defer r.Close()
	f, info, err := regular(r, base)
	if err != nil {
		return MediaSource{}, err
	}
	defer f.Close()
	if info.Size() < 1 || info.Size() > limit {
		return MediaSource{}, errors.New("video must contain 1 byte to 256 MiB")
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(contextReader{ctx, f}, limit+1))
	if err != nil {
		return MediaSource{}, err
	}
	if n > limit || n != info.Size() {
		return MediaSource{}, ErrConflict
	}
	first := hex.EncodeToString(hash.Sum(nil))
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return MediaSource{}, err
	}
	hash.Reset()
	verified, err := io.Copy(hash, io.LimitReader(contextReader{ctx, f}, limit+1))
	if err != nil {
		return MediaSource{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return MediaSource{}, err
	}
	current, err := r.Lstat(base)
	if err != nil {
		return MediaSource{}, err
	}
	if verified != n || first != hex.EncodeToString(hash.Sum(nil)) || !os.SameFile(info, after) || !os.SameFile(info, current) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return MediaSource{}, ErrConflict
	}
	clean, _ := Clean(name, false)
	return MediaSource{Path: clean, SHA256: first, Bytes: n}, nil
}
