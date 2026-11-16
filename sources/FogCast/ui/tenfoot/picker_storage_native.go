//go:build !wasip1 && !tamago

package tenfoot

import (
	"github.com/DeanoC/FogCast/appliance/blockprobe"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func pickerSkipsPath(path string) bool { return blockprobe.Skips(path) }

func directoryWritable(path string, _ os.FileInfo) bool { return unix.Access(path, unix.W_OK) == nil }

func openLocalPickerFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
