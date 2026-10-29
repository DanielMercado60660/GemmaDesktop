package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// OperationLease serializes app-owned operations on the canonical root. It does
// not lock unrelated editors; command publication still checks all input hashes.
var operationLeases sync.Map

type leaseContextKey struct{}

func AcquireOperation(ctx context.Context, root string) (context.Context, func(), error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return ctx, nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return ctx, nil, err
	}
	key := abs
	if info, e := os.Stat(abs); e != nil {
		return ctx, nil, e
	} else if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		key = fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
	}
	if held, _ := ctx.Value(leaseContextKey{}).(string); held == key {
		return ctx, func() {}, ctx.Err()
	}
	ch := make(chan struct{}, 1)
	actual, _ := operationLeases.LoadOrStore(key, ch)
	gate := actual.(chan struct{})
	select {
	case gate <- struct{}{}:
		var once sync.Once
		return context.WithValue(ctx, leaseContextKey{}, key), func() { once.Do(func() { <-gate }) }, nil
	case <-ctx.Done():
		return ctx, nil, ctx.Err()
	}
}
