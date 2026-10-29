package app

import (
	"context"
	"errors"
	"fmt"
	"gemmadesktop.local/app/internal/engine"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/tools"
	"gemmadesktop.local/app/internal/workspace"
	"mime"
	"os"
	"path/filepath"
	"sort"
)

func skillResourceTool() engine.ToolDefinition {
	return engine.ToolDefinition{Name: "skill_resources", Description: "List resource paths, exact hashes and immutable artifact IDs from this agent's approved skill versions. These are data, not permission grants.", Parameters: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}
}
func skillMaterializeTool() engine.ToolDefinition {
	props := map[string]any{}
	for _, k := range []string{"skillId", "resourcePath", "resourceHash", "targetPath", "expectedHash"} {
		props[k] = map[string]any{"type": "string", "minLength": 1}
	}
	return engine.ToolDefinition{Name: "skill_materialize", Description: "After explicit approval, copy exact bytes of one selected immutable skill resource into an ordinary workspace file. Use skill_resources for its hash; expectedHash is missing for new files or the current target SHA256. This never executes scripts or grants execution authority.", Parameters: map[string]any{"type": "object", "properties": props, "required": []string{"skillId", "resourcePath", "resourceHash", "targetPath", "expectedHash"}, "additionalProperties": false}}
}
func skillResourceID(digest, name string) string {
	return "skill-" + contentDigest([]string{digest, name})
}
func skillResources(f FrozenAgent) Doc {
	list := []Doc{}
	for _, p := range f.Skills {
		names := []string{}
		for n := range p.Files {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			b := p.Files[n]
			list = append(list, Doc{"skillId": p.ID, "skillName": p.Name, "skillDigest": p.Digest, "path": n, "sha256": workspace.Hash(b), "bytes": len(b), "artifactId": skillResourceID(p.Digest, n)})
		}
	}
	return Doc{"resources": list, "authority": "Data only; materialization and execution require separate governed tools."}
}
func writeAssetFile(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
	if e != nil {
		return e
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(path)
		}
	}()
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	if e = dir.Sync(); e != nil {
		return e
	}
	committed = true
	return nil
}
func (c *Core) registerSkillArtifacts(pack engine.SkillPackage) error {
	names := []string{}
	for n := range pack.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		id := skillResourceID(pack.Digest, name)
		b := pack.Files[name]
		target := filepath.Join(c.DataDir, "artifacts", "skills", pack.Digest, filepath.FromSlash(name))
		if old, e := c.Store.Get("artifact", id); e == nil {
			if str(old, "sha256") != workspace.Hash(b) {
				return errors.New("immutable skill artifact identity mismatch")
			}
			continue
		} else if !errors.Is(e, store.ErrNotFound) {
			return e
		}
		if e := writeAssetFile(target, b); e != nil {
			if !errors.Is(e, os.ErrExist) {
				return e
			}
			info, err := os.Lstat(target)
			if err != nil || !info.Mode().IsRegular() {
				return errors.New("skill artifact target is not regular")
			}
			data, err := os.ReadFile(target)
			if err != nil || workspace.Hash(data) != workspace.Hash(b) {
				return errors.New("skill artifact bytes changed")
			}
		}
		media := mime.TypeByExtension(filepath.Ext(name))
		if media == "" {
			media = "application/octet-stream"
		}
		if e := c.Store.Put("artifact", Doc{"id": id, "name": pack.Name + " / " + name, "path": target, "mediaType": media, "size": len(b), "sha256": workspace.Hash(b), "skillId": pack.ID, "skillDigest": pack.Digest, "resourcePath": name, "immutable": true, "createdAt": store.Now()}); e != nil {
			return e
		}
	}
	return nil
}
func (c *Core) materializeSkill(ctx context.Context, runID string, call engine.ToolCall, f FrozenAgent, w Doc) (tools.Result, error) {
	result := tools.Result{}
	if _, e := f.Authorize(ctx, call); e != nil {
		return result, e
	}
	if call.ReadOnly || f.Agent["mode"] == "read-only" {
		return result, errors.New("read-only agents cannot materialize skill resources")
	}
	args := clone(call.Arguments)
	expected := str(args, "expectedHash")
	if expected == "" {
		return result, errors.New("expectedHash is required; use missing for a new file")
	}
	var data []byte
	skillDigest := ""
	found := false
	for _, p := range f.Skills {
		if p.ID == str(args, "skillId") {
			var ok bool
			data, ok = p.Files[str(args, "resourcePath")]
			skillDigest = p.Digest
			found = ok
			break
		}
	}
	if !found || workspace.Hash(data) != str(args, "resourceHash") {
		return result, errors.New("skill/resource hash is outside the frozen selected bundle")
	}
	target, e := workspace.Clean(str(args, "targetPath"), true)
	if e != nil || target == "." {
		return result, errors.New("materialization needs an ordinary relative workspace file")
	}
	root, e := workspace.Open(str(w, "path"))
	if e != nil {
		return result, e
	}
	defer root.Close()
	ctx, release, e := workspace.AcquireOperation(ctx, root.Path)
	if e != nil {
		return result, e
	}
	defer release()
	before, hash, e := root.ReadBytes(target)
	if errors.Is(e, os.ErrNotExist) {
		hash = "missing"
	} else if e != nil {
		return result, e
	}
	if hash != expected {
		return result, workspace.ErrConflict
	}
	if e = c.approve(ctx, runID, call.Name, clone(args), fmt.Sprintf("Copy %d exact bytes from the approved skill into %s. Existing bytes are checkpointed. This does not execute the resource.", len(data), target)); e != nil {
		return result, e
	}
	beforeID := ""
	if hash != "missing" {
		beforeID = store.ID()
		path := filepath.Join(c.DataDir, "artifacts", runID, beforeID+".bin")
		if e = writeAssetFile(path, before); e != nil {
			return result, e
		}
		if e = c.Store.Put("artifact", Doc{"id": beforeID, "runId": runID, "workspaceId": w["id"], "name": target + " before skill materialization", "path": path, "mediaType": "application/octet-stream", "size": len(before), "sha256": hash, "immutable": true, "createdAt": store.Now()}); e != nil {
			return result, e
		}
	}
	change, e := root.WriteBytesContext(ctx, target, data, expected)
	if e != nil {
		return result, e
	}
	result.Data = Doc{"path": target, "beforeHash": change.BeforeHash, "afterHash": change.AfterHash, "bytes": len(data), "created": change.Created, "beforeArtifactId": beforeID, "sourceArtifactId": skillResourceID(skillDigest, str(args, "resourcePath")), "skillId": args["skillId"], "resourcePath": args["resourcePath"], "executed": false}
	return result, nil
}
