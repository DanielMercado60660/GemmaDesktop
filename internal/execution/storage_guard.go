package execution

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sync"
	"time"
)

const executionDiskBudget int64 = 2 << 30
const executionEntryBudget = 100000

type StorageUsage struct {
	ObservedBytes int64  `json:"observedBytes"`
	Entries       int    `json:"entries"`
	Complete      bool   `json:"complete"`
	Error         string `json:"error,omitempty"`
	ObservedAt    string `json:"observedAt"`
	Scope         string `json:"scope"`
	Semantics     string `json:"semantics"`
	ByteLimit     int64  `json:"monitorByteLimit"`
	EntryLimit    int    `json:"monitorEntryLimit"`
}

// This is a monitored aggregate bound, not a filesystem quota. RLIMIT_FSIZE
// additionally bounds individual files; small monitoring-interval overshoot is
// possible and is not described as a hard disk quota.
func boundedTreeUsage(ctx context.Context, roots []string) (int64, error) {
	usage, err := measureExecutionStorage(ctx, roots)
	return usage.ObservedBytes, err
}

// The same bounded walker supplies monitoring and read-only status. Sizes are
// logical file bytes, not allocated blocks or Docker Desktop's VM/image usage.
func measureExecutionStorage(ctx context.Context, roots []string) (usage StorageUsage, resultErr error) {
	usage = StorageUsage{ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Scope: "App execution snapshots, retained publication journals and private caches; excludes Docker images and VM allocation", Semantics: "Logical file bytes observed during a non-atomic scan. Soft 2 GiB / 100000-entry monitor, not a hard filesystem quota; interval overshoot is possible. No automatic pruning.", ByteLimit: executionDiskBudget, EntryLimit: executionEntryBudget}
	defer func() {
		usage.Complete = resultErr == nil
		if resultErr != nil {
			usage.Error = resultErr.Error()
		}
	}()
	for _, root := range roots {
		e := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			usage.Entries++
			if usage.Entries > executionEntryBudget {
				return errors.New("execution storage exceeds 100000 entries")
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			if !entry.IsDir() {
				info, e := entry.Info()
				if errors.Is(e, fs.ErrNotExist) {
					return nil
				}
				if e != nil {
					return e
				}
				usage.ObservedBytes += info.Size()
				if usage.ObservedBytes > executionDiskBudget {
					return errors.New("execution state storage exceeds monitored 2 GiB budget")
				}
			}
			return nil
		})
		if e != nil {
			return usage, e
		}
	}
	return usage, nil
}

func monitorExecutionStorage(parent context.Context, cancel context.CancelFunc, roots []string) func() error {
	ctx, stop := context.WithCancel(parent)
	done := make(chan struct{})
	var mu sync.Mutex
	var problem error
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, e := boundedTreeUsage(ctx, roots)
				if e != nil && ctx.Err() == nil {
					mu.Lock()
					problem = e
					mu.Unlock()
					cancel()
					return
				}
			}
		}
	}()
	return func() error { stop(); <-done; mu.Lock(); defer mu.Unlock(); return problem }
}
