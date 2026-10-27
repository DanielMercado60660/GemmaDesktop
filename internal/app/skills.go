package app

import (
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/store"
	"google.golang.org/adk/v2/tool/skilltoolset/skill"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func (c *Core) importSkill(p Doc) (any, error) {
	path, e := required(p, "path")
	if e != nil {
		return nil, e
	}
	st, e := os.Stat(path)
	if e != nil {
		return nil, e
	}
	if st.IsDir() {
		path = filepath.Join(path, "SKILL.md")
	}
	st, e = os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("Skill must be a regular file, not a symlink or device")
	}
	if st.Size() > 256<<10 {
		return nil, errors.New("Skill exceeds 256 KiB")
	}
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || !os.SameFile(st, info) {
		return nil, errors.New("Skill changed while opening")
	}
	b, e := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 256<<10 {
		return nil, errors.New("Skill exceeds 256 KiB")
	}
	meta, _, e := skill.ParseBytes(b)
	if e != nil {
		return nil, fmt.Errorf("Invalid skill: %w", e)
	}
	name, desc := meta.Name, meta.Description
	list, e := c.Store.List("skill")
	if e != nil {
		return nil, e
	}
	for _, existing := range list {
		if existing["name"] == name {
			return nil, errors.New("A skill with this name is already imported")
		}
	}
	id := store.ID()
	dir := filepath.Join(c.DataDir, "skills", id)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(dir, "SKILL.md"), b, 0600); e != nil {
		return nil, e
	}
	d := Doc{"id": id, "name": name, "description": desc, "path": dir, "createdAt": store.Now()}
	return d, c.Store.Put("skill", d)
}
func (c *Core) openArtifact(id string, reveal bool) (any, error) {
	d, e := c.Store.Get("artifact", id)
	if e != nil {
		return nil, e
	}
	path := str(d, "path")
	if !filepath.IsAbs(path) {
		return nil, errors.New("Artifact has no absolute path")
	}
	root := c.DataDir
	if w, e := c.Store.Get("workspace", str(d, "workspaceId")); e == nil {
		root = str(w, "path")
	}
	real, e := filepath.EvalSymlinks(path)
	if e != nil {
		return nil, e
	}
	rel, e := filepath.Rel(root, real)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel, e = filepath.Rel(filepath.Join(c.DataDir, "artifacts"), real)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("Artifact moved outside its approved workspace or artifact store")
		}
	}
	if c.OpenPath == nil {
		return Doc{"path": real}, fmt.Errorf("Native file opening is unavailable; artifact: %s", real)
	}
	if !reveal {
		switch strings.ToLower(filepath.Ext(real)) {
		case ".app", ".command", ".sh", ".py", ".scpt", ".workflow", ".webloc":
			return nil, errors.New("Executable artifacts must be inspected using Locate; automatic execution is disabled")
		}
	}
	return Doc{"path": real}, c.OpenPath(real, reveal)
}
