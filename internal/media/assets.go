package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func (m *Manager) openVerified(relative, expected string, limit int64) (Asset, error) {
	if expected == "" || filepath.IsAbs(relative) || strings.Contains(relative, "..") || strings.Contains(relative, "\\") {
		return Asset{}, errors.New("invalid private media reference")
	}
	r, e := os.OpenRoot(m.root)
	if e != nil {
		return Asset{}, e
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts[:len(parts)-1] {
		before, e := r.Lstat(part)
		if e != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			r.Close()
			return Asset{}, errors.New("private media ancestor is not a regular directory")
		}
		next, e := r.OpenRoot(part)
		if e != nil {
			r.Close()
			return Asset{}, e
		}
		after, e := next.Stat(".")
		r.Close()
		if e != nil || !os.SameFile(before, after) {
			next.Close()
			return Asset{}, errors.New("private media ancestor changed")
		}
		r = next
	}
	defer r.Close()
	name := parts[len(parts)-1]
	before, e := r.Lstat(name)
	if e != nil || !before.Mode().IsRegular() {
		return Asset{}, errors.New("private media file is missing or not regular")
	}
	if stat, ok := before.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		return Asset{}, errors.New("private media hard links are forbidden")
	}
	if before.Size() < 1 || before.Size() > limit {
		return Asset{}, errors.New("private media size exceeds its bound")
	}
	f, e := r.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return Asset{}, e
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		f.Close()
		return Asset{}, errors.New("private media file changed")
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, limit+1))
	if e != nil || n != before.Size() || hex.EncodeToString(h.Sum(nil)) != expected {
		f.Close()
		return Asset{}, errors.New("private media hash no longer matches its immutable record")
	}
	after, e = f.Stat()
	if e != nil || !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		f.Close()
		return Asset{}, errors.New("private media changed while reading")
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		f.Close()
		return Asset{}, e
	}
	return Asset{File: f, Name: name, SHA256: expected, Size: n, ModTime: before.ModTime()}, nil
}
func (m *Manager) OpenMedia(workspaceID, clipID, kind, frameID string) (Asset, error) {
	if e := m.scope(workspaceID); e != nil {
		return Asset{}, e
	}
	clip, e := getDocument[Clip](m.db, "clips", workspaceID, clipID)
	if e != nil {
		return Asset{}, e
	}
	switch kind {
	case "original":
		if !safeID(clip.ID) || clip.SHA256 == "" || clip.DurationSeconds <= 0 {
			return Asset{}, errors.New("video import is not validated")
		}
		asset, e := m.openVerified(filepath.Join("assets", clip.ID, "source.mp4"), clip.SHA256, MaxSourceBytes)
		asset.Name = clip.Name
		asset.ContentType = "video/mp4"
		return asset, e
	case "frame":
		if !safeID(frameID) {
			return Asset{}, errors.New("invalid frame ID")
		}
		var path, hash string
		e = m.db.QueryRow(`SELECT frames.path,frames.sha256 FROM frames JOIN indexes ON indexes.id=frames.index_id WHERE frames.id=? AND indexes.clip_id=? AND indexes.status='ready'`, frameID, clip.ID).Scan(&path, &hash)
		if e != nil {
			return Asset{}, errors.New("indexed frame is not available for this clip")
		}
		asset, e := m.openVerified(path, hash, 1<<20)
		asset.ContentType = "image/jpeg"
		return asset, e
	default:
		return Asset{}, errors.New("media kind must be original or frame")
	}
}
