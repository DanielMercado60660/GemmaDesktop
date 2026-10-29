package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadBytes is a bounded regular-file snapshot for explicitly selected binary
// assets. It shares the same protected path, no-follow and hardlink guards.
func (w *Workspace) ReadBytes(name string) ([]byte, string, error) {
	r, base, e := w.parent(name, false)
	if e != nil {
		return nil, "", e
	}
	defer r.Close()
	f, before, e := regular(r, base)
	if e != nil {
		return nil, "", e
	}
	defer f.Close()
	if before.Size() > MaxFileBytes {
		return nil, "", ErrTooLarge
	}
	b, e := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if e != nil {
		return nil, "", e
	}
	if len(b) > MaxFileBytes {
		return nil, "", ErrTooLarge
	}
	after, e := f.Stat()
	if e != nil {
		return nil, "", e
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(b)) != after.Size() {
		return nil, "", ErrConflict
	}
	return b, Hash(b), nil
}

// WriteBytesContext never executes assets. The caller must persist any desired
// before-image and obtain authority; this primitive rechecks the exact revision
// while holding the canonical operation lease. Binary bytes stay out of Change's
// text-only fields, which must not be used as a binary checkpoint.
func (w *Workspace) WriteBytesContext(ctx context.Context, name string, data []byte, expected string) (Change, error) {
	var change Change
	if expected == "" {
		return change, errors.New("expectedHash is required; use missing for a new file")
	}
	if len(data) > MaxFileBytes {
		return change, ErrTooLarge
	}
	name, e := Clean(name, true)
	if e != nil {
		return change, e
	}
	_, release, e := AcquireOperation(ctx, w.Path)
	if e != nil {
		return change, e
	}
	defer release()
	mode := uint32(0644)
	beforeHash := "missing"
	r, base, e := w.parent(name, true)
	if e != nil {
		return change, e
	}
	f, info, e := regular(r, base)
	if e == nil {
		mode = uint32(info.Mode().Perm())
		f.Close()
	} else if !errors.Is(e, os.ErrNotExist) {
		r.Close()
		return change, e
	}
	r.Close()
	if e == nil {
		_, beforeHash, e = w.ReadBytes(name)
		if e != nil {
			return change, e
		}
	}
	if beforeHash != expected {
		return change, fmt.Errorf("%s: %w", name, ErrConflict)
	}
	if e = ctx.Err(); e != nil {
		return change, e
	}
	if e = w.commandReplace(name, data, expected, mode, false); e != nil {
		return change, e
	}
	return Change{Path: name, BeforeHash: beforeHash, AfterHash: Hash(data), Created: beforeHash == "missing"}, nil
}
