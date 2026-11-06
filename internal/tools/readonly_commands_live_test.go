package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyCommandOptInRealSandbox(t *testing.T) {
	if os.Getenv("GEMMA_TEST_SANDBOX") != "1" {
		t.Skip("real sandbox qualification is opt-in")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, []byte("preserved"), 0600); err != nil {
		t.Fatal(err)
	}
	approved := false
	m, err := New(root, Options{Mode: "read-only", AllowReadOnlyCommands: true, Approve: func(_ context.Context, a Approval) error {
		approved = true
		if a.Arguments["readOnly"] != true {
			t.Fatal("write authority reached approval", a)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	code := `from pathlib import Path
import tempfile
assert Path('source.txt').read_text() == 'preserved'
for name in ('source.txt', 'helper.py'):
    try:
        Path(name).write_text('unexpected')
    except PermissionError:
        pass
    else:
        raise AssertionError('workspace write was allowed')
with tempfile.TemporaryFile(mode='w+') as temporary:
    temporary.write('private scratch')
print(6 * 7)
`
	result, err := m.Execute(t.Context(), "run_command", map[string]any{"executable": "/opt/homebrew/bin/python3", "args": []string{"-c", code}, "readOnly": false})
	if err != nil {
		t.Fatalf("real read-only command: %v\n%s", err, result.Content)
	}
	data := result.Data.(map[string]any)
	if !approved || data["readOnly"] != true || strings.TrimSpace(data["stdout"].(string)) != "42" {
		t.Fatalf("unexpected command result: %+v", result)
	}
	got, err := os.ReadFile(source)
	if err != nil || string(got) != "preserved" {
		t.Fatal("source changed", err)
	}
	if _, err = os.Stat(filepath.Join(root, "helper.py")); !os.IsNotExist(err) {
		t.Fatal("helper file was created", err)
	}
}
