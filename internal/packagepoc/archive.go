// Package packagepoc creates the deliberately small POC 1A deployment archive.
package packagepoc

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var archiveModes = map[string]int64{
	"MiSTer.ini.fragment": 0o600,
	"agent.toml":          0o600,
	"mister-agent":        0o755,
	"start-agent.sh":      0o755,
}

// Create writes a reproducible gzip-compressed USTAR archive. The source
// directory must contain exactly the four files understood by the POC 1A
// installer; refusing additional files prevents accidental ROM distribution.
func Create(sourceDirectory, outputPath string, fixedTime time.Time) (err error) {
	names, err := validateSource(sourceDirectory)
	if err != nil {
		return err
	}

	outputDirectory := filepath.Dir(outputPath)
	temporary, err := os.CreateTemp(outputDirectory, ".mister-remote-poc1a-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create temporary archive: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary archive mode: %w", err)
	}

	archiveTime := fixedTime.UTC().Truncate(time.Second)
	gzipWriter := gzip.NewWriter(temporary)
	gzipWriter.Header.ModTime = archiveTime
	gzipWriter.Header.OS = 255
	gzipWriter.Header.Name = ""
	gzipWriter.Header.Comment = ""
	tarWriter := tar.NewWriter(gzipWriter)

	for _, name := range names {
		if err = appendFile(tarWriter, sourceDirectory, name, archiveTime); err != nil {
			return err
		}
	}
	if err = tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar stream: %w", err)
	}
	if err = gzipWriter.Close(); err != nil {
		return fmt.Errorf("close gzip stream: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary archive: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close temporary archive: %w", err)
	}
	if err = os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish archive: %w", err)
	}
	return nil
}

func validateSource(sourceDirectory string) ([]string, error) {
	entries, err := os.ReadDir(sourceDirectory)
	if err != nil {
		return nil, fmt.Errorf("read source directory: %w", err)
	}
	if len(entries) != len(archiveModes) {
		return nil, fmt.Errorf("source directory must contain exactly %d deployment files", len(archiveModes))
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, permitted := archiveModes[entry.Name()]; !permitted {
			return nil, fmt.Errorf("unexpected deployment file %q", entry.Name())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("deployment file %q is a symbolic link", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect deployment file %q: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("deployment file %q is not regular", entry.Name())
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func appendFile(writer *tar.Writer, sourceDirectory, name string, fixedTime time.Time) error {
	path := filepath.Join(sourceDirectory, name)
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open deployment file %q: %w", name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect deployment file %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("deployment file %q changed while packaging", name)
	}

	header := &tar.Header{
		Name:     filepath.ToSlash(filepath.Join("mister-remote", name)),
		Mode:     archiveModes[name],
		Size:     info.Size(),
		ModTime:  fixedTime,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatUSTAR,
	}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write header for %q: %w", name, err)
	}
	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf("write content for %q: %w", name, err)
	}
	return nil
}
