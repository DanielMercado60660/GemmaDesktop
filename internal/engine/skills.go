package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"google.golang.org/adk/v2/tool/skilltoolset/skill"
)

type skillEntry struct {
	meta         *skill.Frontmatter
	instructions string
	resources    map[string][]byte
}
type skillSource struct{ entries map[string]skillEntry }

func openSkillRoot(p string) (*os.Root, error) { return os.OpenRoot(filepath.Dir(p)) }

// loadSkills snapshots approved packages, rejecting symlinks and special files.
// The resulting ADK source has no live access to the host filesystem.
func loadSkills(paths []string) (*skillSource, error) {
	s := &skillSource{entries: map[string]skillEntry{}}
	var total int64
	files := 0
	for _, p := range paths {
		if !filepath.IsAbs(p) || filepath.Base(p) != "SKILL.md" {
			return nil, fmt.Errorf("skill must be an absolute SKILL.md path: %q", p)
		}
		root, err := os.OpenRoot(filepath.Dir(p))
		if err != nil {
			return nil, err
		}
		entry, err := func() (skillEntry, error) {
			defer root.Close()
			info, err := root.Lstat("SKILL.md")
			if err != nil {
				return skillEntry{}, err
			}
			if !info.Mode().IsRegular() || info.Size() > 1<<20 {
				return skillEntry{}, fmt.Errorf("skill instructions must be a regular file below 1 MiB")
			}
			data, err := readSkillFile(root, "SKILL.md")
			if err != nil {
				return skillEntry{}, err
			}
			meta, body, err := skill.ParseBytes(data)
			if err != nil {
				return skillEntry{}, err
			}
			entry := skillEntry{meta: meta, instructions: body, resources: map[string][]byte{}}
			total += int64(len(data))
			files++
			if files > 512 || total > 8<<20 {
				return skillEntry{}, fmt.Errorf("skill packages exceed size/count limits")
			}
			for _, dir := range []string{"references", "assets", "scripts"} {
				if _, err := root.Lstat(dir); os.IsNotExist(err) {
					continue
				}
				err = fs.WalkDir(root.FS(), dir, func(name string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.Type()&fs.ModeSymlink != 0 {
						return fmt.Errorf("skill symlinks are not allowed: %s", name)
					}
					if d.IsDir() {
						return nil
					}
					info, err := d.Info()
					if err != nil {
						return err
					}
					if !info.Mode().IsRegular() {
						return fmt.Errorf("skill special files are not allowed: %s", name)
					}
					files++
					if files > 512 || info.Size() > 1<<20 || total+info.Size() > 8<<20 {
						return fmt.Errorf("skill resources exceed size/count limits")
					}
					b, err := readSkillFile(root, name)
					if err != nil {
						return err
					}
					total += int64(len(b))
					if total > 8<<20 {
						return fmt.Errorf("skill resources exceed size limit")
					}
					entry.resources[name] = b
					return nil
				})
				if err != nil {
					return skillEntry{}, err
				}
			}
			return entry, nil
		}()
		if err != nil {
			return nil, fmt.Errorf("load skill %s: %w", p, err)
		}
		if _, exists := s.entries[entry.meta.Name]; exists {
			return nil, fmt.Errorf("%w: %s", skill.ErrDuplicateSkill, entry.meta.Name)
		}
		s.entries[entry.meta.Name] = entry
	}
	return s, nil
}

func readSkillFile(root *os.Root, name string) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("skill file must be regular and not a symlink")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return nil, fmt.Errorf("skill file changed while opening")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		return nil, fmt.Errorf("skill hardlinks are not allowed")
	}
	if info.Size() > 1<<20 {
		return nil, fmt.Errorf("skill file exceeds 1 MiB")
	}
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("skill file exceeds 1 MiB")
	}
	after, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || int64(len(b)) != after.Size() {
		return nil, fmt.Errorf("skill file changed while reading")
	}
	return b, nil
}
func (s *skillSource) ListFrontmatters(ctx context.Context) ([]*skill.Frontmatter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(s.entries))
	for n := range s.entries {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*skill.Frontmatter, 0, len(names))
	for _, n := range names {
		copy := *s.entries[n].meta
		out = append(out, &copy)
	}
	return out, nil
}
func (s *skillSource) LoadFrontmatter(ctx context.Context, name string) (*skill.Frontmatter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, ok := s.entries[name]
	if !ok {
		return nil, skill.ErrSkillNotFound
	}
	copy := *e.meta
	return &copy, nil
}
func (s *skillSource) LoadInstructions(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	e, ok := s.entries[name]
	if !ok {
		return "", skill.ErrSkillNotFound
	}
	return e.instructions, nil
}
func (s *skillSource) LoadResource(ctx context.Context, name, p string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, ok := s.entries[name]
	if !ok {
		return nil, skill.ErrSkillNotFound
	}
	if !fs.ValidPath(p) || path.Clean(p) != p {
		return nil, skill.ErrInvalidResourcePath
	}
	b, ok := e.resources[p]
	if !ok {
		return nil, skill.ErrResourceNotFound
	}
	if !skillText(b) {
		return nil, fmt.Errorf("binary skill assets must be materialized through an explicitly authorized artifact operation")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (s *skillSource) ListResources(ctx context.Context, name, dir string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, ok := s.entries[name]
	if !ok {
		return nil, skill.ErrSkillNotFound
	}
	if dir != "" && dir != "." && !fs.ValidPath(dir) {
		return nil, skill.ErrInvalidResourcePath
	}
	out := []string{}
	for p := range e.resources {
		if dir == "" || dir == "." || p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}
