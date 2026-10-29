package execution

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func changesFixture(t *testing.T) (string, func(...string), func(string, string)) {
	t.Helper()
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("real sandbox qualification is opt-in")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		base := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}
		cmd := exec.Command(GitExecutable(), append(base, args...)...)
		cmd.Dir = root
		cmd.Env = []string{"PATH=" + SafePATH, "HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture Git %v: %s %v", args, out, err)
		}
	}
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	return root, git, write
}

func TestChangesCombineHEADAndNewTextSafely(t *testing.T) {
	root, git, write := changesFixture(t)
	for name, content := range map[string]string{"tracked.txt": "base\nkeep\n", "staged.txt": "stage base\n", "deleted.txt": "remove me\n", ".env": "CREDENTIAL=old-secret\n", ".gitignore": "ignored*\n", ".gitattributes": "*.txt diff=trap\n"} {
		write(name, content)
	}
	git("add", "-A")
	git("commit", "-qm", "initial")
	git("config", "diff.trap.command", "/bin/false")
	git("config", "diff.trap.textconv", "/bin/false")
	git("config", "core.fsmonitor", "/bin/false")
	write("tracked.txt", "staged version\nkeep\n")
	git("add", "--", "tracked.txt")
	write("tracked.txt", "working version\nkeep\n")
	write("staged.txt", "staged only\n")
	git("add", "--", "staged.txt")
	os.Remove(filepath.Join(root, "deleted.txt"))
	write(".env", "CREDENTIAL=must-not-leak\n")
	git("add", "--", ".env")
	write(".aws/credentials", "AWS_MUST_NOT_LEAK")
	write("ignored.txt", "IGNORED_MUST_NOT_APPEAR")
	write("new file.py", "print('new text')\n")
	write("no newline.txt", "last line")
	write("binary.db", string([]byte{0, 1, 2, 3}))
	outside := filepath.Join(t.TempDir(), "outside-secret")
	os.WriteFile(outside, []byte("SYMLINK_TARGET_MUST_NOT_APPEAR"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "linked.txt")); err != nil {
		t.Fatal(err)
	}
	r, err := WorkspaceChanges(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if r.Truncated || !strings.Contains(r.Scope, "HEAD") {
		t.Fatalf("scope %+v", r)
	}
	for _, want := range []string{"-base", "+working version", "+staged only", "-remove me", "+print('new text')", "\\ No newline at end of file", "binary content", "linked.txt"} {
		if !strings.Contains(r.Diff, want) {
			t.Errorf("missing %q in diff: %s", want, r.Diff)
		}
	}
	for _, forbidden := range []string{"+staged version", "CREDENTIAL=", "AWS_MUST_NOT_LEAK", "IGNORED_MUST_NOT_APPEAR", "SYMLINK_TARGET_MUST_NOT_APPEAR"} {
		if strings.Contains(r.Diff, forbidden) {
			t.Errorf("unexpected %q in diff", forbidden)
		}
	}
	states := map[string]GitChange{}
	for _, change := range r.Files {
		states[change.Path] = change
	}
	if states["tracked.txt"].Index != "M" || states["tracked.txt"].Worktree != "M" || states["staged.txt"].Index != "M" || states["staged.txt"].Worktree != " " || states["deleted.txt"].Worktree != "D" || !states["new file.py"].Untracked {
		t.Fatalf("index/worktree state lost: %+v", states)
	}
	for _, hidden := range []string{".env", ".aws/credentials", "ignored.txt"} {
		if _, ok := states[hidden]; ok {
			t.Fatalf("protected/ignored path in status: %s", hidden)
		}
	}
}

func TestChangesUnbornAndCleanRepositories(t *testing.T) {
	root, git, write := changesFixture(t)
	write("staged.py", "old staged\n")
	write("removed.py", "removed before first commit\n")
	git("add", "--", "staged.py", "removed.py")
	write("staged.py", "current staged file\n")
	os.Remove(filepath.Join(root, "removed.py"))
	write("untracked.py", "new untracked\n")
	r, err := WorkspaceChanges(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Scope, "No HEAD") || !strings.Contains(r.Diff, "+current staged file") || !strings.Contains(r.Diff, "+new untracked") || strings.Contains(r.Diff, "+old staged") || strings.Contains(r.Diff, "removed before first commit") {
		t.Fatalf("unborn result %+v", r)
	}
	git("add", "-A")
	git("commit", "-qm", "initial")
	r, err = WorkspaceChanges(context.Background(), root)
	if err != nil || r.Diff != "" || r.Truncated {
		t.Fatalf("clean result %+v %v", r, err)
	}
	if _, err = WorkspaceChanges(context.Background(), t.TempDir()); err == nil {
		t.Fatal("non-repository returned an empty success")
	}
}

func TestChangesBoundLargeAndNumerousNewFiles(t *testing.T) {
	root, _, write := changesFixture(t)
	write("000-large.txt", strings.Repeat("large new content\n", 5000))
	for i := 0; i < 105; i++ {
		write(fmt.Sprintf("file-%03d.txt", i), "")
	}
	r, err := WorkspaceChanges(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated || !strings.Contains(r.Diff, "larger than 64 KiB") || !strings.Contains(r.Diff, "100 files") || len(r.Diff) > changesMaxBytes {
		t.Fatalf("untracked limits %+v", r)
	}
}

func TestChangesBoundsTrackedOutput(t *testing.T) {
	root, git, write := changesFixture(t)
	write("tracked.txt", "before\n")
	git("add", "--", "tracked.txt")
	git("commit", "-qm", "initial")
	write("tracked.txt", strings.Repeat("tracked line 漢字\n", 25000))
	r, err := WorkspaceChanges(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated || len(r.Diff) > changesMaxBytes || !utf8.ValidString(r.Diff) || !strings.Contains(r.Diff, "Diff output truncated") {
		t.Fatalf("tracked output limits bytes=%d truncated=%v", len(r.Diff), r.Truncated)
	}
}

func TestChangesQuotedExecutableWithoutFinalNewline(t *testing.T) {
	root, _, write := changesFixture(t)
	name := "strange\nname-漢.py"
	write(name, "print('done')")
	os.Chmod(filepath.Join(root, name), 0755)
	r, err := WorkspaceChanges(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Diff, "new file mode 100755") || !strings.Contains(r.Diff, "\\012") || !strings.Contains(r.Diff, "\\ No newline at end of file") || strings.Contains(r.Diff, "strange\nname") {
		t.Fatalf("unsafe patch header %q", r.Diff)
	}
}
