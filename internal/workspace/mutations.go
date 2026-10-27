package workspace

import (
	"errors"
	"io"
	"os"
)

// Delete removes one regular text file at the reviewed revision. The caller
// persists a before-image checkpoint before invoking destructive operations.
func (w *Workspace) Delete(name, expectedHash string) (Change, error) {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	r, base, err := w.parent(name, true)
	if err != nil {
		return Change{}, err
	}
	defer r.Close()
	f, _, err := regular(r, base)
	if err != nil {
		return Change{}, err
	}
	defer f.Close()
	before, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return Change{}, err
	}
	if len(before) > MaxFileBytes || Hash(before) != expectedHash {
		return Change{}, ErrConflict
	}
	check, _, err := regular(r, base)
	if err != nil {
		return Change{}, ErrConflict
	}
	current, err := io.ReadAll(io.LimitReader(check, MaxFileBytes+1))
	check.Close()
	if err != nil || Hash(current) != expectedHash {
		return Change{}, ErrConflict
	}
	if err = r.Remove(base); err != nil {
		return Change{}, err
	}
	clean, _ := Clean(name, true)
	return Change{Path: clean, Before: string(before), BeforeHash: expectedHash}, nil
}

func (w *Workspace) Move(source, destination, expectedHash string) (Change, error) {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	sr, sb, err := w.parent(source, true)
	if err != nil {
		return Change{}, err
	}
	defer sr.Close()
	dr, db, err := w.parent(destination, true)
	if err != nil {
		return Change{}, err
	}
	defer dr.Close()
	if _, err = dr.Lstat(db); !errors.Is(err, os.ErrNotExist) {
		return Change{}, errors.New("move destination must not exist")
	}
	f, _, err := regular(sr, sb)
	if err != nil {
		return Change{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return Change{}, err
	}
	if len(b) > MaxFileBytes || Hash(b) != expectedHash {
		return Change{}, ErrConflict
	}
	sd, err := sr.Open(".")
	if err != nil {
		return Change{}, err
	}
	defer sd.Close()
	dd, err := dr.Open(".")
	if err != nil {
		return Change{}, err
	}
	defer dd.Close()
	check, _, err := regular(sr, sb)
	if err != nil {
		return Change{}, ErrConflict
	}
	current, err := io.ReadAll(io.LimitReader(check, MaxFileBytes+1))
	check.Close()
	if err != nil || Hash(current) != expectedHash {
		return Change{}, ErrConflict
	}
	if err = renameExclusive(sd, sb, dd, db); err != nil {
		return Change{}, err
	}
	clean, _ := Clean(destination, true)
	return Change{Path: clean, Before: string(b), After: string(b), BeforeHash: expectedHash, AfterHash: expectedHash, Created: true}, nil
}
