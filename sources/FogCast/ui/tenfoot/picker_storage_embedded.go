//go:build wasip1 || tamago

package tenfoot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Embedded pickers only traverse mounted virtual filesystems. Device nodes
// are not media files, and Linux sysfs probing has no meaning on this target.
// The caller still requires directories or regular files of the right size.
func pickerSkipsPath(path string) bool {
	path = filepath.Clean(path)
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	return path == "/dev" || strings.HasPrefix(path, "/dev/")
}

// The mounted VFS supplies the writable permission hint. Actual state writes
// remain authoritative and report mount or I/O failures to the application.
func directoryWritable(_ string, info os.FileInfo) bool { return info.Mode().Perm()&0222 != 0 }

func openLocalPickerFile(path string) (*os.File, error) {
	if pickerSkipsPath(path) {
		return nil, fmt.Errorf("device paths are not media files")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("choose a regular media file")
	}
	return os.Open(path)
}
