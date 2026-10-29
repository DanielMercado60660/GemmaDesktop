package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/adk/v2/tool/skilltoolset/skill"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"
)

// SkillPackage is a content-addressed, immutable snapshot. Scripts are data;
// importing or loading one never authorizes execution.
type SkillPackage struct {
	Version     int               `json:"version"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Digest      string            `json:"digest"`
	Files       map[string][]byte `json:"files"`
}

func packageDigest(files map[string][]byte) string {
	b, _ := json.Marshal(files)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func SnapshotSkill(path string) (SkillPackage, error) {
	source, err := loadSkills([]string{path})
	if err != nil {
		return SkillPackage{}, err
	}
	for _, entry := range source.entries {
		// Preserve the exact frontmatter bytes, not a normalized reconstruction.
		root, err := openSkillRoot(path)
		if err != nil {
			return SkillPackage{}, err
		}
		defer root.Close()
		b, err := readSkillFile(root, "SKILL.md")
		if err != nil {
			return SkillPackage{}, err
		}
		files := entry.resources
		files["SKILL.md"] = b
		p := SkillPackage{Version: 1, Name: entry.meta.Name, Description: entry.meta.Description, Files: files}
		p.Digest = packageDigest(files)
		p.ID = p.Digest
		return p, ValidateSkillPackages([]SkillPackage{p})
	}
	return SkillPackage{}, errors.New("skill is empty")
}
func ValidateSkillPackages(packages []SkillPackage) error {
	total, count := 0, 0
	names := map[string]bool{}
	for _, p := range packages {
		if p.Version != 1 || p.Digest != packageDigest(p.Files) {
			return errors.New("skill package digest/version mismatch")
		}
		meta, _, err := skill.ParseBytes(p.Files["SKILL.md"])
		if err != nil {
			return err
		}
		if meta.Name != p.Name || meta.Description != p.Description || names[p.Name] {
			return errors.New("skill package metadata mismatch or duplicate name")
		}
		names[p.Name] = true
		for name, b := range p.Files {
			if !fs.ValidPath(name) || (name != "SKILL.md" && !strings.HasPrefix(name, "references/") && !strings.HasPrefix(name, "scripts/") && !strings.HasPrefix(name, "assets/")) {
				return fmt.Errorf("unsupported skill resource %q", name)
			}
			total += len(b)
			count++
			if len(b) > 1<<20 || total > 8<<20 || count > 512 {
				return errors.New("skill packages exceed 512 files, 1 MiB per file or 8 MiB total")
			}
		}
	}
	return nil
}
func (s *skillSource) addPackages(packages []SkillPackage) error {
	if err := ValidateSkillPackages(packages); err != nil {
		return err
	}
	for _, p := range packages {
		if _, ok := s.entries[p.Name]; ok {
			return skill.ErrDuplicateSkill
		}
		meta, body, _ := skill.ParseBytes(p.Files["SKILL.md"])
		e := skillEntry{meta: meta, instructions: body, resources: map[string][]byte{}}
		keys := make([]string, 0, len(p.Files))
		for k := range p.Files {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k != "SKILL.md" {
				e.resources[k] = append([]byte{}, p.Files[k]...)
			}
		}
		s.entries[p.Name] = e
	}
	return nil
}
func skillText(b []byte) bool { return utf8.Valid(b) && !strings.ContainsRune(string(b), '\x00') }
