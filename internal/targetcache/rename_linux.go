//go:build linux

package targetcache

import (
	"errors"
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
		// exFAT (the MiSTer SD filesystem) does not implement renameat2.
		// Uploads are serialized by Manager.uploadGate, so an existence check
		// immediately followed by Root.Rename preserves no-overwrite behavior
		// on filesystems that lack RENAME_NOREPLACE.
		if errors.Is(renameErr, unix.EINVAL) || errors.Is(renameErr, unix.ENOSYS) || errors.Is(renameErr, unix.ENOTSUP) {
			if _, statErr := root.Lstat(newName); statErr == nil {
				return os.ErrExist
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
			return root.Rename(oldName, newName)
		}
		return renameErr
	}
	return closeErr
}
