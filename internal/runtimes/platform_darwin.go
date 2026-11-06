//go:build darwin

package runtimes

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func platformPreflight(manifest Manifest) error {
	if runtime.GOARCH != manifest.Architecture {
		return fmt.Errorf("requires macOS %s", manifest.Architecture)
	}
	current, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return errors.New("could not verify macOS version")
	}
	if compareVersion(current, manifest.MinimumOS) < 0 {
		return fmt.Errorf("requires macOS %s or newer", manifest.MinimumOS)
	}
	return nil
}

func compareVersion(a, b string) int {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, y := 0, 0
		if i < len(aa) {
			x, _ = strconv.Atoi(aa[i])
		}
		if i < len(bb) {
			y, _ = strconv.Atoi(bb[i])
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func availableDiskBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}
