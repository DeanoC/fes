package targetimage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openRegularNoFollow(path string, requireSealed bool) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("not a regular non-symlink file: %s", path)
	}
	if requireSealed && info.Mode().Perm()&0o222 != 0 {
		return nil, nil, fmt.Errorf("file is writable: %s", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, nil, fmt.Errorf("open file: %s", path)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !openedInfo.Mode().IsRegular() || (requireSealed && openedInfo.Mode().Perm()&0o222 != 0) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("file changed to an invalid type or mode: %s", path)
	}
	return file, openedInfo, nil
}

func validateAbsoluteDestination(path, name string) error {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%s must be an absolute path", name)
	}
	return nil
}
