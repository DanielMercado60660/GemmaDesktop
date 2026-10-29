package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gemmadesktop.local/app/internal/workspace"
)

const changesMaxBytes = 256 << 10
const changesMaxNewFiles = 100
const changesMaxNewFileBytes = 64 << 10

type Changes struct {
	Diff      string      `json:"diff"`
	Scope     string      `json:"scope"`
	Truncated bool        `json:"truncated,omitempty"`
	Files     []GitChange `json:"files"`
}

// Keep index and working-tree states separate: a file may have both a staged
// change and a later unstaged edit, even when the combined HEAD diff is empty.
type GitChange struct {
	Path      string `json:"path"`
	Index     string `json:"index"`
	Worktree  string `json:"worktree"`
	Untracked bool   `json:"untracked"`
}

// WorkspaceChanges is a read-only view, not a patch application API. HEAD is
// the baseline for tracked files, so staged and unstaged edits are combined.
// New text files are read through the guarded workspace API, never git --no-index.
func WorkspaceChanges(ctx context.Context, root string) (Changes, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	w, err := workspace.Open(root)
	if err != nil {
		return Changes{}, err
	}
	defer w.Close()
	base := []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.untrackedCache=false", "-c", "core.quotePath=true", "-c", "submodule.recurse=false"}
	git := func(args ...string) (Result, error) {
		argv := append(append([]string{}, base...), args...)
		return NewRunner().Run(ctx, Request{Workspace: w.Path, Executable: GitExecutable(), Args: argv, ReadOnly: true, AllowGitMetadata: true, MaxOutput: 1 << 20, Timeout: 15 * time.Second})
	}
	head, err := git("rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return Changes{}, err
	}
	if head.ExitCode != 0 && head.ExitCode != 1 {
		return Changes{}, gitChangesError(head)
	}
	unborn := head.ExitCode == 1
	result := Changes{Scope: "File labels distinguish staged and unstaged state. The patch combines tracked changes against HEAD and untracked text files. Ignored/protected paths are excluded; binary content and symlinks are not expanded.", Files: []GitChange{}}
	statusArgs := []string{"-c", "status.renames=false", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=all", "--", "."}
	statusArgs = append(statusArgs, GitProtectedExclusions()...)
	status, err := git(statusArgs...)
	if err != nil {
		return Changes{}, err
	}
	if status.ExitCode != 0 {
		return Changes{}, gitChangesError(status)
	}
	if status.Truncated {
		// First/last output is unsuitable for parsing NUL-delimited records.
		result.Truncated = true
	} else {
		for _, record := range strings.Split(status.Stdout, "\x00") {
			if len(record) < 4 || record[2] != ' ' {
				continue
			}
			path := record[3:]
			if _, err := workspace.Clean(path, false); err != nil {
				continue
			}
			if len(result.Files) == 1000 {
				result.Truncated = true
				break
			}
			result.Files = append(result.Files, GitChange{Path: path, Index: string(record[0]), Worktree: string(record[1]), Untracked: record[:2] == "??"})
		}
	}
	var out strings.Builder
	appendBounded := func(text string) bool {
		remaining := changesMaxBytes - 256 - out.Len()
		if len(text) <= remaining {
			out.WriteString(text)
			return true
		}
		if remaining > 0 {
			out.WriteString(outputPrefix(text, remaining))
		}
		out.WriteString("\n# Diff output truncated at 256 KiB; remaining content omitted.\n")
		result.Truncated = true
		return false
	}
	pathspec := append([]string{"--", "."}, GitProtectedExclusions()...)
	if !unborn {
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--no-renames", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "HEAD"}
		args = append(args, pathspec...)
		tracked, e := git(args...)
		if e != nil {
			return Changes{}, e
		}
		if tracked.ExitCode != 0 {
			return Changes{}, gitChangesError(tracked)
		}
		if !appendBounded(tracked.Stdout) {
			result.Diff = out.String()
			return result, nil
		}
		if tracked.Truncated {
			result.Truncated = true
			if !appendBounded("\n# Tracked diff exceeded the process output budget; only its beginning and end are shown.\n") {
				result.Diff = out.String()
				return result, nil
			}
		}
	} else {
		result.Scope = "No HEAD commit yet: current staged and untracked text files against an empty baseline. Ignored/protected paths are excluded; binary content and symlinks are not expanded."
	}
	args := []string{"ls-files", "-z", "--others", "--exclude-standard"}
	if unborn {
		args = append(args, "--cached")
	}
	args = append(args, pathspec...)
	listing, err := git(args...)
	if err != nil {
		return Changes{}, err
	}
	if listing.ExitCode != 0 {
		return Changes{}, gitChangesError(listing)
	}
	if listing.Truncated {
		result.Truncated = true
		appendBounded("\n# File listing exceeded its budget; new-file patches are omitted.\n")
		result.Diff = out.String()
		return result, nil
	}
	seen := map[string]bool{}
	count := 0
	for _, name := range strings.Split(listing.Stdout, "\x00") {
		if err = ctx.Err(); err != nil {
			return Changes{}, err
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if _, err = workspace.Clean(name, false); err != nil {
			continue
		}
		if count >= changesMaxNewFiles {
			result.Truncated = true
			appendBounded("\n# New-file patch limit reached (100 files); remaining files omitted.\n")
			break
		}
		count++
		file, readErr := w.ReadLimit(name, changesMaxNewFileBytes)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		} // A staged file can already have been removed again.
		if readErr != nil {
			reason := "symlink, non-regular, or unreadable file"
			if errors.Is(readErr, workspace.ErrBinary) {
				reason = "binary content"
			}
			if errors.Is(readErr, workspace.ErrTooLarge) {
				reason = "text larger than 64 KiB"
				result.Truncated = true
			}
			if !appendBounded(fmt.Sprintf("# New-file content omitted (%s): %s\n", reason, quoteGitPath(name))) {
				break
			}
			continue
		}
		if !appendBounded(newFilePatch(file)) {
			break
		}
	}
	result.Diff = out.String()
	return result, nil
}

func gitChangesError(r Result) error {
	detail := strings.TrimSpace(r.Stderr)
	if detail == "" {
		detail = fmt.Sprintf("Git exited with code %d", r.ExitCode)
	}
	return errors.New(detail)
}

// Git's quoted paths use bytewise C escapes, not JSON's Unicode escapes.
func quoteGitPath(path string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, v := range []byte(path) {
		if v >= 32 && v <= 126 && v != '"' && v != '\\' {
			b.WriteByte(v)
		} else if v == '"' || v == '\\' {
			b.WriteByte('\\')
			b.WriteByte(v)
		} else {
			fmt.Fprintf(&b, "\\%03o", v)
		}
	}
	b.WriteByte('"')
	return b.String()
}
func newFilePatch(file workspace.File) string {
	var b strings.Builder
	mode := "100644"
	if file.Executable {
		mode = "100755"
	}
	fmt.Fprintf(&b, "diff --git %s %s\nnew file mode %s\n", quoteGitPath("a/"+file.Path), quoteGitPath("b/"+file.Path), mode)
	if file.Content == "" {
		return b.String()
	}
	lines := strings.Split(file.Content, "\n")
	finalNewline := strings.HasSuffix(file.Content, "\n")
	if finalNewline {
		lines = lines[:len(lines)-1]
	}
	fmt.Fprintf(&b, "--- /dev/null\n+++ %s\n@@ -0,0 +1,%d @@\n", quoteGitPath("b/"+file.Path), len(lines))
	for _, line := range lines {
		b.WriteByte('+')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if !finalNewline {
		b.WriteString("\\ No newline at end of file\n")
	}
	return b.String()
}
