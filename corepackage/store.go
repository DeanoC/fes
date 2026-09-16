package corepackage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
)

var (
	ErrInvalidPackage   = errors.New("core package: invalid package")
	ErrInvalidPackageID = errors.New("core package: invalid package ID")
	ErrPackageNotFound  = errors.New("core package: package not found")
	storeTemporaryRE    = regexp.MustCompile(`^\.corepackage-[0-9a-f]{32}\.tmp$`)
)

// Store owns a private, content-addressed collection of canonical .fcore
// archives. Each installed filename is the package identity derived from the
// archive's manifest and payload.
type Store struct {
	root     string
	rootInfo os.FileInfo
}

// NewStore opens or creates a private package store rooted at an absolute path.
func NewStore(path string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("core package: store root must be absolute")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("core package: create store root: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("core package: store root must be a real directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("core package: store root must not be accessible by group or other")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("core package: open store root: %w", err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("core package: store root changed while opening")
	}
	return &Store{root: path, rootInfo: opened}, nil
}

// Import validates and atomically publishes one canonical archive. The bool is
// true only when this call created the content-addressed entry.
func (s *Store) Import(ctx context.Context, length int64, reader io.Reader) (Inspection, bool, error) {
	if ctx == nil || reader == nil {
		return Inspection{}, false, fmt.Errorf("%w: missing import input", ErrInvalidPackage)
	}
	if length < 1 || length > MaxArchiveSize {
		return Inspection{}, false, fmt.Errorf("%w: archive size must be 1 through %d bytes", ErrInvalidPackage, MaxArchiveSize)
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, false, err
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, length+1))
	if err != nil {
		return Inspection{}, false, err
	}
	if int64(len(data)) != length {
		return Inspection{}, false, fmt.Errorf("%w: import length does not match the declared length", ErrInvalidPackage)
	}
	manifest, payload, err := readArchive(data)
	if err != nil {
		return Inspection{}, false, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	descriptor, err := decode(manifest, payload)
	if err != nil {
		return Inspection{}, false, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	inspection := Inspection{PackageID: packageIdentity(manifest, payload), Descriptor: descriptor}
	if err := ctx.Err(); err != nil {
		return Inspection{}, false, err
	}

	root, err := s.open()
	if err != nil {
		return Inspection{}, false, err
	}
	defer root.Close()
	token, err := randomToken()
	if err != nil {
		return Inspection{}, false, err
	}
	temporary := ".corepackage-" + token + ".tmp"
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return Inspection{}, false, fmt.Errorf("core package: create import temporary: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = root.Remove(temporary)
		}
	}()
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Inspection{}, false, fmt.Errorf("core package: write import temporary: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, false, err
	}

	installed := inspection.PackageID + ".fcore"
	if err := root.Link(temporary, installed); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return Inspection{}, false, fmt.Errorf("core package: publish archive: %w", err)
		}
		existingInspection, existingData, readErr := s.readOpened(ctx, root, inspection.PackageID)
		if readErr != nil || !bytes.Equal(existingData, data) || !reflect.DeepEqual(existingInspection, inspection) {
			return Inspection{}, false, errors.New("core package: existing content-addressed archive is invalid")
		}
		return existingInspection, false, nil
	}
	if err := root.Remove(temporary); err != nil {
		return Inspection{}, false, fmt.Errorf("core package: remove linked import temporary: %w", err)
	}
	removeTemporary = false
	if err := syncRoot(root); err != nil {
		return Inspection{}, false, err
	}
	return inspection, true, nil
}

// List validates and returns every installed archive sorted by package ID.
func (s *Store) List(ctx context.Context) ([]Inspection, error) {
	if ctx == nil {
		return nil, errors.New("core package: missing list context")
	}
	root, err := s.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("core package: read store root: %w", err)
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return nil, fmt.Errorf("core package: read store root: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("core package: close store root: %w", closeErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	result := make([]Inspection, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if storeTemporaryRE.MatchString(name) {
			info, err := root.Lstat(name)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
				return nil, fmt.Errorf("core package: invalid import temporary %q", name)
			}
			continue
		}
		if len(name) != 70 || name[64:] != ".fcore" || !hex64RE.MatchString(name[:64]) {
			return nil, fmt.Errorf("core package: unexpected store entry %q", name)
		}
		inspection, _, err := s.readOpened(ctx, root, name[:64])
		if err != nil {
			return nil, err
		}
		result = append(result, inspection)
	}
	return result, nil
}

// Read returns one validated inspection and the same exact canonical archive
// bytes from the content-addressed store.
func (s *Store) Read(ctx context.Context, packageID string) (Inspection, []byte, error) {
	if ctx == nil {
		return Inspection{}, nil, errors.New("core package: missing read context")
	}
	if !hex64RE.MatchString(packageID) {
		return Inspection{}, nil, ErrInvalidPackageID
	}
	root, err := s.open()
	if err != nil {
		return Inspection{}, nil, err
	}
	defer root.Close()
	return s.readOpened(ctx, root, packageID)
}

func (s *Store) open() (*os.Root, error) {
	info, err := os.Lstat(s.root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(s.rootInfo, info) {
		return nil, errors.New("core package: store root changed")
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, fmt.Errorf("core package: open store root: %w", err)
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(s.rootInfo, opened) {
		_ = root.Close()
		return nil, errors.New("core package: store root changed while opening")
	}
	return root, nil
}

func (s *Store) readOpened(ctx context.Context, root *os.Root, packageID string) (Inspection, []byte, error) {
	name := packageID + ".fcore"
	before, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return Inspection{}, nil, ErrPackageNotFound
	}
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Mode().Perm() != 0o400 {
		return Inspection{}, nil, errors.New("core package: installed archive must be an immutable regular file")
	}
	file, err := root.Open(name)
	if err != nil {
		return Inspection{}, nil, fmt.Errorf("core package: open installed archive: %w", err)
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		_ = file.Close()
		return Inspection{}, nil, errors.New("core package: installed archive changed while opening")
	}
	data, err := readOpenFileWithContext(ctx, file, opened.Size())
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Inspection{}, nil, err
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(opened, after) {
		return Inspection{}, nil, errors.New("core package: installed archive changed while reading")
	}
	manifest, payload, err := readArchive(data)
	if err != nil {
		return Inspection{}, nil, err
	}
	descriptor, err := decode(manifest, payload)
	if err != nil {
		return Inspection{}, nil, err
	}
	inspection := Inspection{PackageID: packageIdentity(manifest, payload), Descriptor: descriptor}
	if inspection.PackageID != packageID {
		return Inspection{}, nil, errors.New("core package: installed archive identity does not match its filename")
	}
	return inspection, data, nil
}

func readOpenFileWithContext(ctx context.Context, file *os.File, size int64) ([]byte, error) {
	if size < 1 || size > MaxArchiveSize {
		return nil, fmt.Errorf("core package: archive size must be 1 through %d bytes", MaxArchiveSize)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(&contextReader{ctx: ctx, reader: file}, data); err != nil {
		return nil, fmt.Errorf("core package: read installed archive: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var extra [1]byte
	if count, err := file.Read(extra[:]); count != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return nil, errors.New("core package: installed archive changed while reading")
	}
	return data, nil
}

func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("core package: open store root for sync: %w", err)
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("core package: sync store root: %w", err)
	}
	return nil
}
