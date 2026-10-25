package rooms

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxArchiveBytes = maxInstallBytes + (8 << 20)

// Export validates and snapshots a directory pack before atomically publishing
// its portable archive. An invalid pack leaves an existing output unchanged.
func Export(source, output string) error {
	temporary, err := os.MkdirTemp("", "fogcast-room-export-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	pack, err := Install(temporary, source)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(output), ".room-export-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	archive := zip.NewWriter(file)
	err = fs.WalkDir(pack.FS, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !archivePath(name) {
			return fmt.Errorf("invalid room archive path: %s", name)
		}
		return addArchiveFile(archive, pack.FS, name)
	})
	closeErr := archive.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxArchiveBytes {
		return errors.New("room archive exceeds size limit")
	}
	if err = file.Chmod(0644); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), output)
}

func addArchiveFile(archive *zip.Writer, source fs.FS, name string) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0644)
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	input, err := source.Open(name)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, input)
	closeErr := input.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func archivePath(name string) bool {
	return fs.ValidPath(name) && name != "." && path.Clean(name) == name &&
		!strings.Contains(name, "\\") && !strings.Contains(strings.Split(name, "/")[0], ":")
}

// Extract privately and completely before invoking the ordinary installer.
// No selected room is changed by a truncated, corrupt or oversized archive.
func installArchive(root, source string) (Pack, error) {
	input, err := os.Open(source)
	if err != nil {
		return Pack{}, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return Pack{}, err
	}
	if info.Size() > maxArchiveBytes {
		return Pack{}, errors.New("room archive exceeds size limit")
	}
	archive, err := zip.NewReader(input, info.Size())
	if err != nil {
		return Pack{}, err
	}
	if len(archive.File) > maxInstallEntries {
		return Pack{}, errors.New("room archive has too many entries")
	}
	temporary, err := os.MkdirTemp("", "fogcast-room-import-")
	if err != nil {
		return Pack{}, err
	}
	defer os.RemoveAll(temporary)
	seen := make(map[string]bool)
	var total uint64
	for _, member := range archive.File {
		name := member.Name
		if member.FileInfo().IsDir() {
			name = strings.TrimSuffix(name, "/")
		}
		if !archivePath(name) || seen[name] {
			return Pack{}, fmt.Errorf("invalid or duplicate room archive path: %s", member.Name)
		}
		seen[name] = true
		destination := filepath.Join(temporary, filepath.FromSlash(name))
		if member.FileInfo().IsDir() {
			if member.UncompressedSize64 != 0 {
				return Pack{}, errors.New("room archive directory contains data")
			}
			if err := os.MkdirAll(destination, 0700); err != nil {
				return Pack{}, err
			}
			continue
		}
		if !member.Mode().IsRegular() {
			return Pack{}, errors.New("room archive member must be a regular file")
		}
		if member.UncompressedSize64 > maxAssetBytes {
			return Pack{}, errors.New("room archive member exceeds size limit")
		}
		total += member.UncompressedSize64
		if total > maxInstallBytes {
			return Pack{}, errors.New("room archive exceeds expanded size limit")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return Pack{}, err
		}
		reader, err := member.Open()
		if err != nil {
			return Pack{}, err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			reader.Close()
			return Pack{}, err
		}
		count, copyErr := io.Copy(output, io.LimitReader(reader, maxAssetBytes+1))
		reader.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return Pack{}, copyErr
		}
		if closeErr != nil {
			return Pack{}, closeErr
		}
		if count != int64(member.UncompressedSize64) {
			return Pack{}, errors.New("room archive member size mismatch")
		}
	}
	return Install(root, temporary)
}
