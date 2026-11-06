//go:build !darwin

package runtimes

import "errors"

func platformPreflight(Manifest) error {
	return errors.New("managed MLX runtimes require Apple Silicon macOS")
}
func availableDiskBytes(string) (int64, error) {
	return 0, errors.New("managed MLX runtime disk inspection requires macOS")
}
