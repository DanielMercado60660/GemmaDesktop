//go:build darwin

package workspace

import (
	"golang.org/x/sys/unix"
	"os"
)

func renameExclusive(sourceDir *os.File, source string, targetDir *os.File, target string) error {
	return unix.RenameatxNp(int(sourceDir.Fd()), source, int(targetDir.Fd()), target, unix.RENAME_EXCL)
}
