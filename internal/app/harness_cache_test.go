package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"gemmadesktop.local/app/internal/store"
	"gemmadesktop.local/app/internal/workspace"
)

const harnessCleanupTestImage = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func harnessCacheTestCore(t *testing.T) *Core {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = os.MkdirAll(filepath.Join(root, "harness"), 0700); err != nil {
		t.Fatal(err)
	}
	return &Core{DataDir: root, Store: db}
}
func seedHarnessCache(t *testing.T, c *Core, root string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(c.DataDir, "execution", "cache", workspace.Hash([]byte(canonical + harnessCleanupTestImage))[:32])
	if err = os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(cache, "go-cache"), []byte(strings.Repeat("disposable", 1024)), 0600); err != nil {
		t.Fatal(err)
	}
	return cache
}
func ownedHarnessRoot(t *testing.T, c *Core, id string) string {
	t.Helper()
	root := filepath.Join(c.DataDir, "harness", "trials", id, "workspace")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("immutable final source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestHarnessDisposableCacheReleasePreservesEvidenceAndOrdinaryCaches(t *testing.T) {
	c := harnessCacheTestCore(t)
	root := ownedHarnessRoot(t, c, "trial-a")
	original, _ := os.ReadFile(filepath.Join(root, "source.go"))
	sourceHash := workspace.Hash(original)
	journal := filepath.Join(c.DataDir, "harness", "trials", "trial-a", "journal.json")
	if err := os.WriteFile(journal, []byte("durable evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	trialCache := seedHarnessCache(t, c, root)
	ordinary := t.TempDir()
	ordinaryCache := seedHarnessCache(t, c, ordinary)
	row, err := c.beginHarnessCache("trial", "trial-a", root, harnessCleanupTestImage)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := c.executorForConfig("docker", harnessCleanupTestImage)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.finishHarnessCache(t.Context(), runner, row); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(trialCache); !os.IsNotExist(err) {
		t.Fatal("trial cache retained", err)
	}
	after, err := os.ReadFile(filepath.Join(root, "source.go"))
	if err != nil || workspace.Hash(after) != sourceHash {
		t.Fatal("final source changed", err)
	}
	if data, err := os.ReadFile(journal); err != nil || string(data) != "durable evidence" {
		t.Fatal("journal changed", err)
	}
	if _, err = os.Stat(filepath.Join(ordinaryCache, "go-cache")); err != nil {
		t.Fatal("ordinary cache changed", err)
	}
	saved, err := c.Store.Get("harness_cache_cleanup", str(row, "id"))
	if err != nil || saved["status"] != "completed" {
		t.Fatal(saved, err)
	}
	// A later grader preserves the completed solver cleanup receipt.
	again, err := c.beginHarnessCache("trial", "trial-a", root, harnessCleanupTestImage)
	if err != nil || again["status"] != "completed" || obj(again["release"])["removed"] != true {
		t.Fatal("release evidence overwritten", again, err)
	}
	if _, err = c.beginHarnessCache("trial", "trial-a", ordinary, harnessCleanupTestImage); err == nil {
		t.Fatal("ordinary source accepted for disposal")
	}

	grader, err := os.MkdirTemp(filepath.Join(c.DataDir, "harness"), "grading-")
	if err != nil {
		t.Fatal(err)
	}
	gradeCache := seedHarnessCache(t, c, grader)
	row, err = c.beginHarnessCache("grader", "trial-a", grader, harnessCleanupTestImage)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.finishHarnessCache(t.Context(), runner, row); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{grader, gradeCache} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("temporary grader retained", path, err)
		}
	}
}

type failedHarnessCacheExecutor struct{}

func (failedHarnessCacheExecutor) Run(context.Context, execution.Request) (execution.Result, error) {
	return execution.Result{}, errors.New("unused")
}
func (failedHarnessCacheExecutor) Description() string { return "controlled cleanup failure" }
func (failedHarnessCacheExecutor) ReleaseWorkspaceCache(context.Context, string) (execution.CacheRelease, error) {
	return execution.CacheRelease{}, errors.New("controlled cleanup failure")
}

func TestHarnessCacheFailureIsDurableAndCannotQualify(t *testing.T) {
	c := harnessCacheTestCore(t)
	root := ownedHarnessRoot(t, c, "failed-trial")
	row, err := c.beginHarnessCache("trial", "failed-trial", root, harnessCleanupTestImage)
	if err != nil {
		t.Fatal(err)
	}
	err = c.finishHarnessCache(t.Context(), failedHarnessCacheExecutor{}, row)
	if err == nil {
		t.Fatal("cleanup failure hidden")
	}
	saved, e := c.Store.Get("harness_cache_cleanup", str(row, "id"))
	if e != nil || saved["status"] != "failed" || !strings.Contains(str(saved, "error"), "controlled cleanup") {
		t.Fatal(saved, e)
	}
	grade, e := c.gradeHarnessTrial(t.Context(), harness.Task{}, harness.Trial{ID: "failed-trial", Error: err.Error()})
	if e == nil || grade.Passed || len(grade.Checks) != 1 || !grade.Checks[0].Critical {
		t.Fatal("cleanup failure did not invalidate infrastructure qualification", grade, e)
	}
	if c.harnessCacheSummary()["failed"] != 1 {
		t.Fatal("cleanup failure is absent from status")
	}
}

func TestHarnessCacheRecoveryRetriesRecordedRootsOnlyAndSkipsActiveTrials(t *testing.T) {
	c := harnessCacheTestCore(t)
	root := ownedHarnessRoot(t, c, "interrupted-trial")
	cache := seedHarnessCache(t, c, root)
	row, err := c.beginHarnessCache("trial", "interrupted-trial", root, harnessCleanupTestImage)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Store.Put("harness_trial", Doc{"id": "interrupted-trial", "status": "running"}); err != nil {
		t.Fatal(err)
	}
	if err = c.reconcileHarnessCaches(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(cache); err != nil {
		t.Fatal("active trial cache removed", err)
	}
	if err = c.Store.Put("harness_trial", Doc{"id": "interrupted-trial", "status": "interrupted"}); err != nil {
		t.Fatal(err)
	}
	ordinaryCache := seedHarnessCache(t, c, t.TempDir())
	if err = c.reconcileHarnessCaches(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("interrupted recorded cache retained", err)
	}
	if _, err = os.Stat(filepath.Join(root, "source.go")); err != nil {
		t.Fatal("interrupted output removed", err)
	}
	if _, err = os.Stat(ordinaryCache); err != nil {
		t.Fatal("unrecorded cache removed", err)
	}
	if err = c.reconcileHarnessCaches(t.Context()); err != nil {
		t.Fatal("repeated recovery is not idempotent", err)
	}
	saved, _ := c.Store.Get("harness_cache_cleanup", str(row, "id"))
	if saved["status"] != "completed" {
		t.Fatal(saved)
	}

	// Crash after cache release and temporary source removal, before final row write.
	grader, err := os.MkdirTemp(filepath.Join(c.DataDir, "harness"), "grading-")
	if err != nil {
		t.Fatal(err)
	}
	row, err = c.beginHarnessCache("grader", "interrupted-trial", grader, harnessCleanupTestImage)
	if err != nil {
		t.Fatal(err)
	}
	row["cacheReleased"], row["status"] = true, "released"
	if err = c.Store.Put("harness_cache_cleanup", row); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(grader); err != nil {
		t.Fatal(err)
	}
	if err = c.reconcileHarnessCaches(t.Context()); err != nil {
		t.Fatal("released grader recovery failed", err)
	}
	saved, _ = c.Store.Get("harness_cache_cleanup", str(row, "id"))
	if saved["status"] != "completed" {
		t.Fatal(saved)
	}
}
