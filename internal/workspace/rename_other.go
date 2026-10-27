//go:build !darwin

package workspace

import (
	"errors"
	"os"
)

func renameExclusive(sourceDir *os.File, source string, targetDir *os.File, target string) error {
	return errors.New("exclusive atomic move is not qualified on this platform")
}
