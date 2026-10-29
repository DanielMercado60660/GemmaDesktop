//go:build darwin && cgo

package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"
)

// This opt-in qualification uses a new service and fake canary only. It does
// not enumerate, read, update or remove any user or application credential.
func TestNativeKeychainCanaryLifecycle(t *testing.T) {
	if os.Getenv("GEMMA_TEST_KEYCHAIN") != "1" {
		t.Skip("opt-in native Keychain canary: GEMMA_TEST_KEYCHAIN=1")
	}
	service := "GemmaDesktop.test." + rand.Text()
	account := "canary"
	first := NewKeychainStore(service)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := first.Delete(ctx, account); err != nil {
			t.Errorf("canary cleanup failed: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	original := []byte(`{"access":"fake-canary-first","refresh":"fake-canary-refresh"}`)
	updated := []byte(`{"access":"fake-canary-updated"}`)
	if e := first.Put(ctx, account, original); e != nil {
		t.Fatal(e)
	}
	restarted := NewKeychainStore(service)
	b, e := restarted.Get(ctx, account)
	if e != nil || !bytes.Equal(b, original) {
		t.Fatal("native credential did not survive store re-instantiation", e)
	}
	if e = restarted.Put(ctx, account, updated); e != nil {
		t.Fatal(e)
	}
	b, e = first.Get(ctx, account)
	if e != nil || !bytes.Equal(b, updated) {
		t.Fatal("native update not visible", e)
	}
	if e = restarted.Delete(ctx, account); e != nil {
		t.Fatal(e)
	}
	if _, e = first.Get(ctx, account); !errors.Is(e, ErrNoCredential) {
		t.Fatal("deleted canary remains", e)
	}
}
