//go:build linux

package romsource

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func exclusiveRenamePreparedAt(source *os.Root, oldBase string, destination *os.Root, newBase string) error {
	sourceDirectory, err := source.Open(".")
	if err != nil {
		return err
	}
	destinationDirectory, err := destination.Open(".")
	if err != nil {
		_ = sourceDirectory.Close()
		return err
	}
	renameErr := unix.Renameat2(int(sourceDirectory.Fd()), oldBase, int(destinationDirectory.Fd()), newBase, unix.RENAME_NOREPLACE)
	return errors.Join(renameErr, sourceDirectory.Close(), destinationDirectory.Close())
}
