//go:build linux

package targetcache

import (
	"os"

	"golang.org/x/sys/unix"
)

func exclusiveRename(root *os.Root, oldName, newName string) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	renameErr := unix.Renameat2(int(directory.Fd()), oldName, int(directory.Fd()), newName, unix.RENAME_NOREPLACE)
	closeErr := directory.Close()
	if renameErr != nil {
		return renameErr
	}
	return closeErr
}
