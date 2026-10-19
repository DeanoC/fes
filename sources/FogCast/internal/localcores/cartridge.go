package localcores

import (
	"archive/zip"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

// maxCartridgeBytes bounds a ROM the kit-local socket will read. The bytes
// stay on this machine.
const maxCartridgeBytes = 8 << 20

// readCartridgeFile reads one regular file under libraryRoot.
// libraryRoot is the configured library directory. Symlinks above that
// directory, such as a mount point, are followed when the root is pinned.
// Every component of relativePath is opened from that pinned directory and
// a symlink there is refused, so a parent swapped for a symlink cannot name
// a file outside the library. A relative root, a directory, or an oversized
// file is unavailable.
func readCartridgeFile(libraryRoot, relativePath string) ([]byte, error) {
	file, info, relative, err := openConfinedCartridge(libraryRoot, relativePath)
	if err != nil {
		return nil, errUnavailable
	}
	defer file.Close()
	if strings.EqualFold(filepath.Ext(relative), ".zip") {
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

// openConfinedCartridge pins libraryRoot, then resolves relativePath one
// component at a time. os.Root refuses a symlink that would leave the root.
// Lstat on each component also refuses a symlink that stays inside it.
func openConfinedCartridge(libraryRoot, relativePath string) (*os.File, os.FileInfo, string, error) {
	rootPath, parts, err := cartridgeLocation(libraryRoot, relativePath)
	if err != nil {
		return nil, nil, "", err
	}
	relative := pathpkg.Join(parts...)
	entry, err := os.Lstat(rootPath)
	if err != nil || entry.Mode()&os.ModeSymlink != 0 || !entry.IsDir() {
		return nil, nil, "", errUnavailable
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, nil, "", errUnavailable
	}
	defer root.Close()
	openedRoot, err := root.Stat(".")
	if err != nil || !openedRoot.IsDir() || !os.SameFile(entry, openedRoot) {
		return nil, nil, "", errUnavailable
	}
	if err := recheckCartridgeRoot(rootPath, entry); err != nil {
		return nil, nil, "", err
	}

	var parent os.FileInfo
	current := ""
	for _, part := range parts[:len(parts)-1] {
		current = pathpkg.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, nil, "", errUnavailable
		}
		parent = info
	}
	info, err := root.Lstat(relative)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCartridgeBytes {
		return nil, nil, "", errUnavailable
	}

	openRoot := root
	base := parts[len(parts)-1]
	if len(parts) > 1 {
		directory, err := root.OpenRoot(pathpkg.Join(parts[:len(parts)-1]...))
		if err != nil {
			return nil, nil, "", errUnavailable
		}
		defer directory.Close()
		openedDir, err := directory.Stat(".")
		if err != nil || !openedDir.IsDir() || !os.SameFile(parent, openedDir) {
			return nil, nil, "", errUnavailable
		}
		openRoot = directory
	}
	file, err := openRoot.Open(base)
	if err != nil {
		return nil, nil, "", errUnavailable
	}
	fail := func() (*os.File, os.FileInfo, string, error) {
		_ = file.Close()
		return nil, nil, "", errUnavailable
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() != info.Size() || !os.SameFile(info, opened) {
		return fail()
	}
	current = ""
	for _, part := range parts[:len(parts)-1] {
		current = pathpkg.Join(current, part)
		again, err := root.Lstat(current)
		if err != nil || again.Mode()&os.ModeSymlink != 0 || !again.IsDir() {
			return fail()
		}
	}
	again, err := root.Lstat(relative)
	if err != nil || again.Mode()&os.ModeSymlink != 0 || !again.Mode().IsRegular() || !os.SameFile(opened, again) {
		return fail()
	}
	if err := recheckCartridgeRoot(rootPath, entry); err != nil {
		return fail()
	}
	still, err := root.Stat(".")
	if err != nil || !still.IsDir() || !os.SameFile(entry, still) {
		return fail()
	}
	return file, opened, relative, nil
}

func cartridgeLocation(libraryRoot, relativePath string) (string, []string, error) {
	libraryRoot = strings.TrimSpace(libraryRoot)
	if libraryRoot == "" || strings.ContainsRune(libraryRoot, 0) || !filepath.IsAbs(libraryRoot) {
		return "", nil, errUnavailable
	}
	relativePath = strings.TrimSpace(relativePath)
	if relativePath == "" || strings.ContainsRune(relativePath, 0) {
		return "", nil, errUnavailable
	}
	slash := strings.ReplaceAll(relativePath, `\`, "/")
	if pathpkg.IsAbs(slash) {
		return "", nil, errUnavailable
	}
	cleaned := pathpkg.Clean(slash)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", nil, errUnavailable
	}
	parts := strings.Split(cleaned, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", nil, errUnavailable
		}
	}
	return filepath.Clean(libraryRoot), parts, nil
}

func recheckCartridgeRoot(rootPath string, expected os.FileInfo) error {
	current, err := os.Lstat(rootPath)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(expected, current) {
		return errUnavailable
	}
	return nil
}
