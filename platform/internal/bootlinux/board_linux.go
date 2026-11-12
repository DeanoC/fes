//go:build linux

package bootlinux

import (
	"errors"
	"fmt"
	"io"
	"os"

	release "github.com/DeanoC/FogCast/appliance"
	"golang.org/x/sys/unix"
)

// ReadBootBoard reads the bootstrap's immutable image selection. Neither a
// release manifest nor a writable volume can select the bootstrap platform.
func ReadBootBoard(path, architecture string) (string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("read bootstrap board: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("bootstrap board must be a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 65))
	if err != nil {
		return "", err
	}
	if architecture == "arm" && string(raw) == release.Board+"\n" {
		return release.Board, nil
	}
	if architecture == "arm64" && string(raw) == release.BoardDE25+"\n" {
		return release.BoardDE25, nil
	}
	return "", errors.New("bootstrap board does not match the supported ARM CPU")
}
