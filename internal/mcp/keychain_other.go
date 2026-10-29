//go:build !darwin || !cgo

package mcp

import (
	"context"
	"errors"
)

type unavailableSecrets struct{}

func NewKeychainStore(string) SecretStore { return unavailableSecrets{} }
func (unavailableSecrets) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("native macOS Keychain requires a cgo-enabled build")
}
func (unavailableSecrets) Put(context.Context, string, []byte) error {
	return errors.New("native macOS Keychain requires a cgo-enabled build")
}
func (unavailableSecrets) Delete(context.Context, string) error {
	return errors.New("native macOS Keychain requires a cgo-enabled build")
}
