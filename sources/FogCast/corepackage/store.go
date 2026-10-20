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
	"sync"
	"sync/atomic"
	"time"
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

	// publishMu covers link, the losing import's read, and unlinking the
	// temporary hard link. That unlink updates the archive ctime. A reader
	// that samples ctime across it treats the identical bytes as rewritten.
	publishMu sync.Mutex
	verifyMu  sync.Mutex
	verified  map[string]verifiedPackage
}

type verifiedPackage struct {
	key        string
	inspection Inspection
}

// fullVerifications counts archive decodes and identity hashes. A read whose
// stat fingerprint matches an earlier verification does not increment it.
var fullVerifications atomic.Int64

// racyWindow covers the filesystem's coarse ctime clock: a rewrite inside
// the same tick as the verified write leaves ctime unchanged, so a
// verification is remembered only once the archive's ctime is this old.
var racyWindow = 2 * time.Second

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
	manifest, payload, romMap, err := readArchive(data)
	if err != nil {
		return Inspection{}, false, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	descriptor, err := decode(manifest, payload, romMap)
	if err != nil {
		return Inspection{}, false, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	inspection := Inspection{PackageID: packageIdentity(manifest, payload, romMap), Descriptor: descriptor}
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

	published, created, err := s.publishImported(ctx, root, temporary, data, inspection)
	if err != nil {
		return Inspection{}, false, err
	}
	if created {
		removeTemporary = false
	}
	return published, created, nil
}

// publishImported links the temporary archive into its content-addressed name.
// The bool is true only when this call created that name. Callers hold no
// store locks; this method takes publishMu for the whole publish.
func (s *Store) publishImported(ctx context.Context, root *os.Root, temporary string, data []byte, inspection Inspection) (Inspection, bool, error) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
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
		inspection, err := s.inspectOpened(ctx, root, name[:64])
		if err != nil {
			return nil, err
		}
		result = append(result, inspection)
	}
	return result, nil
}

// Inspect returns one validated inspection. It reads and hashes the archive
// only when its stat fingerprint differs from the last verification.
func (s *Store) Inspect(ctx context.Context, packageID string) (Inspection, error) {
	if ctx == nil {
		return Inspection{}, errors.New("core package: missing inspect context")
	}
	if !hex64RE.MatchString(packageID) {
		return Inspection{}, ErrInvalidPackageID
	}
	root, err := s.open()
	if err != nil {
		return Inspection{}, err
	}
	defer root.Close()
	return s.inspectOpened(ctx, root, packageID)
}

func (s *Store) inspectOpened(ctx context.Context, root *os.Root, packageID string) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	if info, err := root.Lstat(packageID + ".fcore"); err == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o400 {
		key, _ := statKey(info)
		if inspection, ok := s.cachedVerified(packageID, key); ok {
			return inspection, nil
		}
	}
	inspection, _, err := s.readOpened(ctx, root, packageID)
	return inspection, err
}

func (s *Store) rememberVerified(packageID string, info os.FileInfo, inspection Inspection) {
	key, changed := statKey(info)
	if key == "" || time.Since(changed) < racyWindow {
		return
	}
	s.verifyMu.Lock()
	defer s.verifyMu.Unlock()
	if s.verified == nil {
		s.verified = make(map[string]verifiedPackage)
	}
	s.verified[packageID] = verifiedPackage{key: key, inspection: cloneInspection(inspection)}
}

func (s *Store) cachedVerified(packageID, key string) (Inspection, bool) {
	if key == "" {
		return Inspection{}, false
	}
	s.verifyMu.Lock()
	defer s.verifyMu.Unlock()
	hit, ok := s.verified[packageID]
	if !ok || hit.key != key {
		return Inspection{}, false
	}
	return cloneInspection(hit.inspection), true
}

// cloneInspection keeps the verification cache independent from both its
// callers and the inspections returned on cache hits. Descriptor's nested
// structs contain only values; copy each mutable member explicitly.
func cloneInspection(inspection Inspection) Inspection {
	descriptor := inspection.Descriptor
	if descriptor.ROM != nil {
		rom := *descriptor.ROM
		descriptor.ROM = &rom
	}
	if descriptor.ROMMap != nil {
		romMap := *descriptor.ROMMap
		descriptor.ROMMap = &romMap
	}
	if descriptor.ROMs != nil {
		roms := make([]ROMRequirement, len(descriptor.ROMs))
		copy(roms, descriptor.ROMs)
		descriptor.ROMs = roms
	}
	if descriptor.Interfaces != nil {
		interfaces := make([]Interface, len(descriptor.Interfaces))
		copy(interfaces, descriptor.Interfaces)
		descriptor.Interfaces = interfaces
	}
	inspection.Descriptor = descriptor
	return inspection
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
	// The archive is immutable (0400, published by link). An unchanged
	// fingerprint, which includes ctime, means these bytes were already
	// decoded and hashed.
	key, _ := statKey(after)
	if openedKey, _ := statKey(opened); key != openedKey {
		return Inspection{}, nil, errors.New("core package: installed archive changed while reading")
	}
	if inspection, ok := s.cachedVerified(packageID, key); ok {
		return inspection, data, nil
	}
	fullVerifications.Add(1)
	manifest, payload, romMap, err := readArchive(data)
	if err != nil {
		return Inspection{}, nil, err
	}
	descriptor, err := decode(manifest, payload, romMap)
	if err != nil {
		return Inspection{}, nil, err
	}
	inspection := Inspection{PackageID: packageIdentity(manifest, payload, romMap), Descriptor: descriptor}
	if inspection.PackageID != packageID {
		return Inspection{}, nil, errors.New("core package: installed archive identity does not match its filename")
	}
	s.rememberVerified(packageID, after, inspection)
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
