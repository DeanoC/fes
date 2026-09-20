package fogcast

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast/catalog"
)

func materializeConfinedLibrary(ctx context.Context, rootPath, relativePath string, fingerprint catalog.Fingerprint) (launchPath string, cleanup func(), err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	relativePath, err = catalog.NormalizeRelativePath(relativePath)
	if err != nil {
		return "", nil, err
	}
	held, rootInfo, err := openPinnedLibraryRoot(rootPath)
	if err != nil {
		return "", nil, err
	}
	defer held.Close()

	destDir, err := os.MkdirTemp("", "fogcast-host-launch-*")
	if err != nil {
		return "", nil, err
	}
	var once sync.Once
	cleanup = func() {
		once.Do(func() { _ = os.RemoveAll(destDir) })
	}
	fail := func(cause error) (string, func(), error) {
		cleanup()
		return "", nil, cause
	}

	if err := copyConfinedFile(ctx, held, relativePath, destDir, &fingerprint); err != nil {
		return fail(err)
	}
	extension := strings.ToLower(path.Ext(relativePath))
	if extension == ".cue" || extension == ".gdi" {
		copied, err := os.Open(filepath.Join(destDir, filepath.FromSlash(relativePath)))
		if err != nil {
			return fail(err)
		}
		names, parseErr := catalog.ParseReferencedMedia(path.Base(relativePath), copied)
		_ = copied.Close()
		if parseErr != nil {
			return fail(parseErr)
		}
		directory := path.Dir(relativePath)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return fail(err)
			}
			companion := name
			if directory != "." {
				companion = path.Join(directory, name)
			}
			normalized, err := catalog.NormalizeRelativePath(companion)
			if err != nil {
				return fail(catalog.ErrEscapingMediaReference)
			}
			if err := copyConfinedFile(ctx, held, normalized, destDir, nil); err != nil {
				return fail(err)
			}
		}
	}
	if err := revalidatePinnedLibraryRoot(rootPath, rootInfo); err != nil {
		return fail(err)
	}
	return filepath.Join(destDir, filepath.FromSlash(relativePath)), cleanup, nil
}

func openPinnedLibraryRoot(rootPath string) (*os.Root, os.FileInfo, error) {
	entryInfo, err := os.Lstat(rootPath)
	if err != nil || entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.IsDir() {
		return nil, nil, errors.New("library source is unavailable")
	}
	directory, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, nil, err
	}
	openedInfo, err := directory.Stat(".")
	if err != nil || !openedInfo.IsDir() || !os.SameFile(entryInfo, openedInfo) {
		_ = directory.Close()
		return nil, nil, errors.New("library source is unavailable")
	}
	if err := revalidatePinnedLibraryRoot(rootPath, entryInfo); err != nil {
		_ = directory.Close()
		return nil, nil, err
	}
	return directory, openedInfo, nil
}

func revalidatePinnedLibraryRoot(rootPath string, expected os.FileInfo) error {
	current, err := os.Lstat(rootPath)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(expected, current) {
		return errors.New("library source is unavailable")
	}
	return nil
}

func copyConfinedFile(ctx context.Context, root *os.Root, relativePath, destDir string, fingerprint *catalog.Fingerprint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, info, err := openConfinedFile(root, relativePath)
	if err != nil {
		return err
	}
	defer source.Close()
	if fingerprint != nil && (info.Size() != fingerprint.SourceSize || info.ModTime().UnixNano() != fingerprint.ModifiedNS) {
		return errors.New("library source is unavailable")
	}
	destination := filepath.Join(destDir, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, contextReader{ctx: ctx, r: source})
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func openConfinedFile(root *os.Root, relativePath string) (*os.File, os.FileInfo, error) {
	info, err := root.Lstat(relativePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, errors.New("library source is unavailable")
	}
	source, err := root.Open(relativePath)
	if err != nil {
		return nil, nil, err
	}
	opened, err := source.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = source.Close()
		return nil, nil, errors.New("library source is unavailable")
	}
	rechecked, err := root.Lstat(relativePath)
	if err != nil || rechecked.Mode()&os.ModeSymlink != 0 || !rechecked.Mode().IsRegular() || !os.SameFile(opened, rechecked) {
		_ = source.Close()
		return nil, nil, errors.New("library source is unavailable")
	}
	return source, opened, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
