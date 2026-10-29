package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageUsageReportsScopeAndDoesNotFollowLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "owned.bin"), []byte("observed"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, []byte(strings.Repeat("excluded", 100)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside-link")); err != nil {
		t.Fatal(err)
	}
	usage, err := measureExecutionStorage(t.Context(), []string{root})
	if err != nil || !usage.Complete || usage.ObservedBytes != int64(len("observed")) || usage.Entries != 3 {
		t.Fatal("measurement followed a link or lost owned bytes", usage, err)
	}
	if usage.ByteLimit != 2<<30 || usage.EntryLimit != 100000 || !strings.Contains(usage.Semantics, "not a hard") || !strings.Contains(usage.Scope, "excludes Docker") || usage.ObservedAt == "" {
		t.Fatal("measurement overclaimed enforcement or scope", usage)
	}
}

func TestStorageUsageReportsIncompleteLimitAndCancellation(t *testing.T) {
	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "sparse-limit"))
	if err != nil {
		t.Fatal(err)
	}
	// Sparse length exercises the logical-byte monitor without allocating 2 GiB.
	if err = f.Truncate(executionDiskBudget + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	usage, err := measureExecutionStorage(t.Context(), []string{root})
	if err == nil || usage.Complete || usage.ObservedBytes != executionDiskBudget+1 || !strings.Contains(usage.Error, "monitored") {
		t.Fatal("limit omitted or reported as complete", usage, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	usage, err = measureExecutionStorage(ctx, []string{root})
	if !errors.Is(err, context.Canceled) || usage.Complete || usage.Error == "" {
		t.Fatal("cancelled scan claimed complete measurement", usage, err)
	}
}
