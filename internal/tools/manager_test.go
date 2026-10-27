package tools

import (
	"context"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovalDenialAndReadOnly(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("original"), 0600)
	m, err := New(root, Options{Approve: func(context.Context, Approval) error { return execution.ErrDenied }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	args := map[string]any{"path": "a", "content": "changed", "expectedHash": workspace.Hash([]byte("original"))}
	if _, err = m.Execute(context.Background(), "write_file", args); !errors.Is(err, execution.ErrDenied) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a"))
	if string(data) != "original" {
		t.Fatal("denied write happened")
	}
	ro, err := New(root, Options{Mode: "read-only"})
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for _, d := range ro.List() {
		if !readTool(d.Name) {
			t.Fatalf("mutator advertised %s", d.Name)
		}
	}
	if _, err = ro.Execute(context.Background(), "run_command", map[string]any{"executable": "echo"}); err == nil {
		t.Fatal("read-only shell accepted")
	}
}
func TestRevisionChangeDuringApprovalAndCheckpoint(t *testing.T) {
	root := t.TempDir()
	artifacts := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("original"), 0600)
	changeDuringApproval := true
	m, err := New(root, Options{ArtifactDir: artifacts, Approve: func(context.Context, Approval) error {
		if changeDuringApproval {
			os.WriteFile(filepath.Join(root, "a"), []byte("user edit"), 0600)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	args := map[string]any{"path": "a", "content": "changed", "expectedHash": workspace.Hash([]byte("original"))}
	if _, err = m.Execute(context.Background(), "write_file", args); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("stale approval accepted %v", err)
	}
	changeDuringApproval = false
	args["expectedHash"] = workspace.Hash([]byte("user edit"))
	r, err := m.Execute(context.Background(), "write_file", args)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Artifacts) != 2 {
		t.Fatalf("no durable before snapshot %+v", r)
	}
	b, err := os.ReadFile(r.Artifacts[1].Path)
	if err != nil || !strings.Contains(string(b), "user edit") {
		t.Fatalf("snapshot %s %v", b, err)
	}
	var saved map[string]any
	if err = json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
}
func TestValidationAndArtifactBoundary(t *testing.T) {
	root := t.TempDir()
	m, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Execute(context.Background(), "read_file", map[string]any{"path": "a", "unexpected": true}); err == nil {
		t.Fatal("unknown argument accepted")
	}
	if _, err = New(root, Options{ArtifactDir: filepath.Join(root, "artifacts")}); err == nil {
		t.Fatal("repository artifact directory accepted")
	}
	for _, d := range m.List() {
		if strings.Contains(d.Name, "terminal") {
			t.Fatal("model can access user terminal")
		}
	}
}
