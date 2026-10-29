package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/store"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (c *Core) importSkill(p Doc) (any, error) {
	source, err := required(p, "path")
	if err != nil {
		return nil, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		source = filepath.Join(source, "SKILL.md")
	}
	pack, err := engine.SnapshotSkill(source)
	if err != nil {
		return nil, err
	}
	id := pack.Digest
	if old, e := c.Store.Get("skill", id); e == nil {
		if old["archived"] != true {
			return nil, errors.New("this skill version is already imported")
		}
		stored, err := readImportedSkill(old)
		if err != nil || stored.Digest != pack.Digest {
			return nil, errors.New("archived skill bytes changed; cannot reactivate this version")
		}
		if err = c.registerSkillArtifacts(stored); err != nil {
			return nil, err
		}
		old["archived"] = false
		return old, c.Store.Put("skill", old)
	} else if !errors.Is(e, store.ErrNotFound) {
		return nil, e
	}
	parent := filepath.Join(c.DataDir, "skills")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	temp, err := os.MkdirTemp(parent, ".import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)
	names := make([]string, 0, len(pack.Files))
	for name := range pack.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	files := []Doc{}
	bytes := 0
	for _, name := range names {
		b := pack.Files[name]
		path := filepath.Join(temp, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		if err = os.WriteFile(path, b, 0400); err != nil {
			return nil, err
		}
		h := sha256.Sum256(b)
		files = append(files, Doc{"path": name, "bytes": len(b), "sha256": hex.EncodeToString(h[:])})
		bytes += len(b)
	}
	dir := filepath.Join(parent, id)
	// A crash between directory publication and the database write can leave
	// this exact version on disk. Reuse only verified bytes; never overwrite a
	// conflicting or incomplete directory under the content-addressed name.
	if _, err = os.Lstat(dir); err == nil {
		if err = verifySkillDirectory(dir, pack.Digest); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err = os.Rename(temp, dir); err != nil {
		// Another import may have published the same version after our check.
		if verifyErr := verifySkillDirectory(dir, pack.Digest); verifyErr != nil {
			return nil, err
		}
	}
	d := Doc{"id": id, "version": 1, "name": pack.Name, "description": pack.Description, "digest": pack.Digest, "path": dir, "sourcePath": source, "files": files, "bytes": bytes, "createdAt": store.Now(), "archived": false, "authority": "Instruction and resource data only; scripts require governed execution"}
	if err = c.registerSkillArtifacts(pack); err != nil {
		return nil, err
	}
	return d, c.Store.Put("skill", d)
}
func verifySkillDirectory(dir, digest string) error {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("existing skill version is not a regular directory")
	}
	p, err := engine.SnapshotSkill(filepath.Join(dir, "SKILL.md"))
	if err != nil || p.Digest != digest {
		return errors.New("existing skill version bytes do not match its immutable digest")
	}
	return nil
}
func (c *Core) archiveSkill(id string) (any, error) {
	return c.Store.Update("skill", id, func(d Doc) error { d["archived"] = true; return nil })
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
