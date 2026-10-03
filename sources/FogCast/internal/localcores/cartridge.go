package localcores

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// maxCartridgeBytes bounds a ROM the kit-local socket will read. The bytes
// stay on this machine.
const maxCartridgeBytes = 8 << 20

// readCartridgeFile reads one regular file. Symlinks are not followed.
// A relative path, a directory, or an oversized file is unavailable.
func readCartridgeFile(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
		return nil, errUnavailable
	}
	cleaned := filepath.Clean(path)
	file, err := os.OpenFile(cleaned, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCartridgeBytes {
		return nil, errUnavailable
	}
	if strings.EqualFold(filepath.Ext(cleaned), ".zip") {
		archive, err := zip.NewReader(file, info.Size())
		if err != nil {
			return nil, errUnavailable
		}
		var selected *zip.File
		for _, member := range archive.File {
			if strings.EqualFold(filepath.Ext(member.Name), ".sms") {
				if selected != nil || member.Flags&1 != 0 || member.UncompressedSize64 < 1 || member.UncompressedSize64 > maxCartridgeBytes {
					return nil, errUnavailable
				}
				selected = member
			}
		}
		if selected == nil {
			return nil, errUnavailable
		}
		reader, err := selected.Open()
		if err != nil {
			return nil, errUnavailable
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxCartridgeBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(data) == 0 || int64(len(data)) != int64(selected.UncompressedSize64) {
			return nil, errUnavailable
		}
		return data, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, info.Size()))
	if err != nil || int64(len(data)) != info.Size() {
		return nil, errUnavailable
	}
	return data, nil
}
