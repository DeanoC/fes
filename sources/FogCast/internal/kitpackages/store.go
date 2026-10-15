// Package kitpackages retains installed target core packages independently of
// the appliance image. Installing and selecting never activate FPGA hardware.
package kitpackages

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DeanoC/FogCast/corepackage"
)

var (
	ErrSelectionConflict = errors.New("kit packages: selection changed")
	ErrCoreMismatch      = errors.New("kit packages: package belongs to another core")
	ErrInvalidPackageID  = corepackage.ErrInvalidPackageID
	ErrPackageNotFound   = corepackage.ErrPackageNotFound
	ErrInvalidPackage    = corepackage.ErrInvalidPackage
	packageIDRE          = regexp.MustCompile(`^[0-9a-f]{64}$`)
	coreIDRE             = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
	stageRE              = regexp.MustCompile(`^(?:\.corepackage-|[0-9a-f]{64}-)[0-9a-f]{32}$`)
	selectionTemporaryRE = regexp.MustCompile(`^\.selections-[0-9a-f]{32}\.tmp$`)
	archiveTemporaryRE   = regexp.MustCompile(`^\.corepackage-[0-9a-f]{32}\.tmp$`)
)

const DefaultRoot = "/run/fogcast/fesdata3/core-library"

type Selection struct {
	CoreID    string `json:"core_id"`
	PackageID string `json:"package_id"`
}

// SelectedPackage is a validated local launch selection. Path is never a wire field.
type SelectedPackage struct {
	Selection
	Inspection corepackage.Inspection
	Path       string `json:"-"`
}

type selectionFile struct {
	Version    int         `json:"version"`
	Selections []Selection `json:"selections"`
}

type verifiedPackage struct {
	key        string
	inspection corepackage.Inspection
}

type Store struct {
	root       string
	info       os.FileInfo
	children   map[string]os.FileInfo
	lockInfo   os.FileInfo
	archives   *corepackage.Store
	persistent bool

	verifyMu sync.Mutex
	verified map[string]verifiedPackage
}

// fullVerificationReads counts payload inspections performed by read.
// Listing that trusts a matching stat fingerprint does not increment it.
var fullVerificationReads atomic.Int64

// New opens a private POSIX store. Production callers use OpenPersistent;
// accepting a temporary directory here does not prove target persistence.
func New(path string) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("kit packages: root must be a clean absolute path")
	}
	if err := realDirectories(path, true); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0o700 {
		return nil, errors.New("kit packages: root must be private (0700)")
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	opened, err := r.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("kit packages: root changed while opening")
	}
	s := &Store{root: path, info: opened, children: make(map[string]os.FileInfo), verified: make(map[string]verifiedPackage)}
	for _, name := range []string{"archives", "packages"} {
		if err := r.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		child, err := r.Lstat(name)
		if err != nil || !child.IsDir() || child.Mode()&os.ModeSymlink != 0 || child.Mode().Perm() != 0o700 {
			return nil, fmt.Errorf("kit packages: %s must be a private real directory", name)
		}
		s.children[name] = child
	}
	if existing, err := r.Lstat(".lock"); err == nil {
		if !existing.Mode().IsRegular() || existing.Mode().Perm() != 0o600 {
			return nil, errors.New("kit packages: lock must be a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock, err := r.OpenFile(".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	s.lockInfo, err = lock.Stat()
	if err == nil {
		err = lock.Sync()
	}
	closeErr := lock.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	linkInfo, err := r.Lstat(".lock")
	if err != nil || !linkInfo.Mode().IsRegular() || linkInfo.Mode().Perm() != 0o600 || !os.SameFile(s.lockInfo, linkInfo) {
		return nil, errors.New("kit packages: lock must be a private regular file")
	}
	s.archives, err = corepackage.NewStore(filepath.Join(path, "archives"))
	if err != nil {
		return nil, err
	}
	if err := syncDirectory(r, "."); err != nil {
		return nil, err
	}
	openedRoot, unlock, err := s.lock(context.Background())
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.recover(context.Background(), openedRoot); err != nil {
		return nil, err
	}
	return s, nil
}

// Install authenticates a canonical archive using the shared package reader,
// and publishes its sealed members at packages/<package-id>. A successful
// retry is idempotent; no selection is made and all earlier versions remain.
func (s *Store) Install(ctx context.Context, length int64, reader io.Reader) (corepackage.Inspection, bool, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return corepackage.Inspection{}, false, err
	}
	defer unlock()
	// Never use a reinstall to conceal missing or corrupt selected data. An
	// interrupted initial install has no selection, so its retry remains safe.
	if _, err := s.selections(ctx, r); err != nil {
		return corepackage.Inspection{}, false, err
	}
	inspection, _, err := s.archives.Import(ctx, length, reader)
	if err != nil {
		return corepackage.Inspection{}, false, err
	}
	created, err := s.publishRetained(ctx, r, inspection)
	return inspection, created, err
}

// publishRetained completes only an absent directory from a fully authenticated
// archive. Present directories are independently verified and never replaced.
func (s *Store) publishRetained(ctx context.Context, r *os.Root, inspection corepackage.Inspection) (bool, error) {
	id := inspection.PackageID
	if _, err := r.Lstat(filepath.Join("packages", id)); err == nil {
		_, _, err = s.read(ctx, r, id)
		return false, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	// A crash between archive and member publication can leave a validated
	// archive. A retry completes only the missing publication, never overwrites it.
	_, data, err := s.archives.Read(ctx, id)
	if err != nil {
		return false, err
	}
	staged, err := corepackage.Stage(ctx, filepath.Join(s.root, "packages"), int64(len(data)), bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	owned := true
	defer func() {
		if owned {
			_ = staged.Cleanup()
		}
	}()
	if staged.PackageID != id || !reflect.DeepEqual(staged.Descriptor, inspection.Descriptor) {
		return false, errors.New("kit packages: staged identity changed")
	}
	if err := syncDirectory(r, filepath.Join("packages", filepath.Base(staged.Directory))); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := s.validateRoot(r); err != nil {
		return false, err
	}
	if err := r.Rename(filepath.Join("packages", filepath.Base(staged.Directory)), filepath.Join("packages", id)); err != nil {
		return false, err
	}
	owned = false
	if err := syncDirectory(r, "packages"); err != nil {
		return false, err
	}
	if _, _, err := s.read(ctx, r, id); err != nil {
		return false, err
	}
	return true, nil
}

// recover reconciles an interrupted install after process or OS restart. A
// selected package must already be intact; restoring selected missing data would
// conceal corruption rather than finish an unselected installation.
func (s *Store) recover(ctx context.Context, r *os.Root) error {
	if _, err := s.selections(ctx, r); err != nil {
		return err
	}
	list, err := s.archives.List(ctx)
	if err != nil {
		return err
	}
	for _, inspection := range list {
		if _, err := s.publishRetained(ctx, r, inspection); err != nil {
			return err
		}
	}
	entries, err := directoryEntries(r, "packages")
	if err != nil {
		return err
	}
	if len(entries) != len(list) {
		return errors.New("kit packages: archive and member inventories differ")
	}
	return nil
}

func (s *Store) List(ctx context.Context) ([]corepackage.Inspection, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	ids, err := installedIDs(r)
	if err != nil {
		return nil, err
	}
	result := make([]corepackage.Inspection, 0, len(ids))
	for _, id := range ids {
		inspection, err := s.listingInspection(ctx, r, id)
		if err != nil {
			return nil, err
		}
		result = append(result, inspection)
	}
	return result, nil
}

func (s *Store) Read(ctx context.Context, id string) (corepackage.Inspection, []byte, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	defer unlock()
	return s.read(ctx, r, id)
}

// Path returns a local launch path only after authenticating both retained
// representations. Network callers never choose or receive filesystem paths.
func (s *Store) Path(ctx context.Context, id string) (string, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, _, err := s.read(ctx, r, id); err != nil {
		return "", err
	}
	return filepath.Join(s.root, "packages", id), nil
}

func (s *Store) read(ctx context.Context, r *os.Root, id string) (corepackage.Inspection, []byte, error) {
	if !packageIDRE.MatchString(id) {
		return corepackage.Inspection{}, nil, ErrInvalidPackageID
	}
	inspection, data, err := s.archives.Read(ctx, id)
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	name := filepath.Join("packages", id)
	info, err := r.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o500 {
		return corepackage.Inspection{}, nil, errors.New("kit packages: installed members must be a sealed real directory")
	}
	entries, err := directoryEntries(r, name)
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	for _, entry := range entries {
		member, err := r.Lstat(filepath.Join(name, entry.Name()))
		if err != nil || !member.Mode().IsRegular() || member.Mode().Perm() != 0o400 {
			return corepackage.Inspection{}, nil, errors.New("kit packages: installed member must be a sealed regular file")
		}
	}
	beforeKey, beforeErr := packageStatKey(r, id)
	fullVerificationReads.Add(1)
	exploded, err := corepackage.InspectPackage(filepath.Join(s.root, name))
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	after, err := r.Lstat(name)
	if err != nil || !os.SameFile(info, after) || !reflect.DeepEqual(inspection, exploded) {
		return corepackage.Inspection{}, nil, errors.New("kit packages: installed member identity changed")
	}
	if err := s.validateRoot(r); err != nil {
		return corepackage.Inspection{}, nil, err
	}
	if afterKey, afterErr := packageStatKey(r, id); beforeErr == nil && afterErr == nil && beforeKey == afterKey {
		s.rememberVerified(id, afterKey, inspection)
	}
	return inspection, data, nil
}

func (s *Store) Select(ctx context.Context, coreID, expectedID, id string) (Selection, error) {
	if !coreIDRE.MatchString(coreID) {
		return Selection{}, errors.New("kit packages: invalid core ID")
	}
	if !packageIDRE.MatchString(id) || (expectedID != "" && !packageIDRE.MatchString(expectedID)) {
		return Selection{}, ErrInvalidPackageID
	}
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return Selection{}, err
	}
	defer unlock()
	selections, err := s.selections(ctx, r)
	if err != nil {
		return Selection{}, err
	}
	inspection, _, err := s.read(ctx, r, id)
	if err != nil {
		return Selection{}, err
	}
	if inspection.Descriptor.Core.ID != coreID {
		return Selection{}, ErrCoreMismatch
	}
	current := ""
	index := -1
	for i, selection := range selections {
		if selection.CoreID == coreID {
			current, index = selection.PackageID, i
			break
		}
	}
	result := Selection{CoreID: coreID, PackageID: id}
	// The same completed change may be retried after a lost HTTP response.
	if current == id {
		return result, nil
	}
	if current != expectedID {
		return Selection{}, ErrSelectionConflict
	}
	if index < 0 {
		selections = append(selections, result)
	} else {
		selections[index] = result
	}
	sort.Slice(selections, func(i, j int) bool { return selections[i].CoreID < selections[j].CoreID })
	data, err := json.Marshal(selectionFile{Version: 1, Selections: selections})
	if err != nil {
		return Selection{}, err
	}
	if len(data) > 1024*1024 {
		return Selection{}, errors.New("kit packages: too many selections")
	}
	if err := atomicSelections(ctx, r, data); err != nil {
		return Selection{}, err
	}
	return result, nil
}

func (s *Store) Selections(ctx context.Context) ([]Selection, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.selections(ctx, r)
}

// SelectedPackages returns the selected launch inventory. Listing reuses the
// verification recorded at install while the archive and member stat
// fingerprint still matches. Path, Read, and launch still call read, which
// inspects the payload bytes.
func (s *Store) SelectedPackages(ctx context.Context) ([]SelectedPackage, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.selectedPackages(ctx, r)
}

func (s *Store) selectedPackages(ctx context.Context, r *os.Root) ([]SelectedPackage, error) {
	selections, err := readSelections(r)
	if err != nil {
		return nil, err
	}
	result := make([]SelectedPackage, 0, len(selections))
	for _, selection := range selections {
		inspection, err := s.listingInspection(ctx, r, selection.PackageID)
		if err != nil {
			return nil, err
		}
		if inspection.Descriptor.Core.ID != selection.CoreID {
			return nil, ErrCoreMismatch
		}
		result = append(result, SelectedPackage{Selection: selection, Inspection: inspection, Path: filepath.Join(s.root, "packages", selection.PackageID)})
	}
	return result, nil
}

// listingInspection trusts the install-time verification when size, mtime,
// inode, mode, and directory entries are unchanged. A miss inspects the
// payload once and records that fingerprint.
func (s *Store) listingInspection(ctx context.Context, r *os.Root, id string) (corepackage.Inspection, error) {
	if err := ctx.Err(); err != nil {
		return corepackage.Inspection{}, err
	}
	if key, err := packageStatKey(r, id); err == nil {
		if inspection, ok := s.cachedVerified(id, key); ok {
			return inspection, nil
		}
	}
	inspection, _, err := s.read(ctx, r, id)
	return inspection, err
}

func (s *Store) rememberVerified(id, key string, inspection corepackage.Inspection) {
	s.verifyMu.Lock()
	defer s.verifyMu.Unlock()
	if s.verified == nil {
		s.verified = make(map[string]verifiedPackage)
	}
	s.verified[id] = verifiedPackage{key: key, inspection: inspection}
}

func (s *Store) cachedVerified(id, key string) (corepackage.Inspection, bool) {
	s.verifyMu.Lock()
	defer s.verifyMu.Unlock()
	hit, ok := s.verified[id]
	if !ok || hit.key != key {
		return corepackage.Inspection{}, false
	}
	return hit.inspection, true
}

func packageStatKey(r *os.Root, id string) (string, error) {
	var b strings.Builder
	if err := writeStat(&b, r, filepath.Join("archives", id+".fcore")); err != nil {
		return "", err
	}
	dir := filepath.Join("packages", id)
	if err := writeStat(&b, r, dir); err != nil {
		return "", err
	}
	entries, err := directoryEntries(r, dir)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		fmt.Fprintf(&b, "entry %s\n", entry.Name())
		if err := writeStat(&b, r, filepath.Join(dir, entry.Name())); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func writeStat(b *strings.Builder, r *os.Root, name string) error {
	info, err := r.Lstat(name)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "%s %d %d %d %d\n", name, info.Size(), info.ModTime().UnixNano(), uint64(info.Mode()), inodeOf(info))
	return nil
}

func installedIDs(r *os.Root) ([]string, error) {
	packages, err := directoryEntries(r, "packages")
	if err != nil {
		return nil, err
	}
	archives, err := directoryEntries(r, "archives")
	if err != nil {
		return nil, err
	}
	packageSet := make(map[string]struct{}, len(packages))
	for _, entry := range packages {
		name := entry.Name()
		if stageRE.MatchString(name) {
			return nil, errors.New("kit packages: unexpected staging entry")
		}
		info, err := entry.Info()
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !packageIDRE.MatchString(name) {
			return nil, fmt.Errorf("kit packages: unexpected package entry %q", name)
		}
		packageSet[name] = struct{}{}
	}
	ids := make([]string, 0, len(archives))
	for _, entry := range archives {
		name := entry.Name()
		if archiveTemporaryRE.MatchString(name) {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
				return nil, errors.New("kit packages: invalid archive temporary")
			}
			continue
		}
		if len(name) != 70 || !strings.HasSuffix(name, ".fcore") || !packageIDRE.MatchString(name[:64]) {
			return nil, fmt.Errorf("kit packages: unexpected archive entry %q", name)
		}
		id := name[:64]
		if _, ok := packageSet[id]; !ok {
			return nil, errors.New("kit packages: archive and member inventories differ")
		}
		delete(packageSet, id)
		ids = append(ids, id)
	}
	if len(packageSet) != 0 {
		return nil, errors.New("kit packages: archive and member inventories differ")
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *Store) selections(ctx context.Context, r *os.Root) ([]Selection, error) {
	packages, err := s.selectedPackages(ctx, r)
	if err != nil {
		return nil, err
	}
	result := make([]Selection, 0, len(packages))
	for _, selected := range packages {
		result = append(result, selected.Selection)
	}
	return result, nil
}

func readSelections(r *os.Root) ([]Selection, error) {
	info, err := r.Lstat("selections.json")
	if errors.Is(err, os.ErrNotExist) {
		return []Selection{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > 1024*1024 {
		return nil, errors.New("kit packages: invalid selection file")
	}
	f, err := r.Open("selections.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("kit packages: selection file changed")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 1024*1024+1))
	decoder.DisallowUnknownFields()
	var document selectionFile
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF || document.Version != 1 || document.Selections == nil {
		return nil, errors.New("kit packages: invalid selection document")
	}
	seen := make(map[string]bool)
	for _, selection := range document.Selections {
		if !coreIDRE.MatchString(selection.CoreID) || !packageIDRE.MatchString(selection.PackageID) || seen[selection.CoreID] {
			return nil, errors.New("kit packages: invalid or duplicate selected core")
		}
		seen[selection.CoreID] = true

	}
	sort.Slice(document.Selections, func(i, j int) bool { return document.Selections[i].CoreID < document.Selections[j].CoreID })
	return document.Selections, nil
}

func atomicSelections(ctx context.Context, r *os.Root, data []byte) error {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	name := ".selections-" + hex.EncodeToString(random[:]) + ".tmp"
	f, err := r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer r.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.Rename(name, "selections.json"); err != nil {
		return err
	}
	return syncDirectory(r, ".")
}

func (s *Store) validateRoot(r *os.Root) error {
	if err := realDirectories(s.root, false); err != nil {
		return err
	}
	info, err := os.Lstat(s.root)
	if err != nil || !os.SameFile(s.info, info) || info.Mode().Perm() != 0o700 {
		return errors.New("kit packages: root changed")
	}
	opened, err := r.Stat(".")
	if err != nil || !os.SameFile(s.info, opened) {
		return errors.New("kit packages: opened root changed")
	}
	for name, expected := range s.children {
		info, err := r.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, info) || info.Mode().Perm() != 0o700 {
			return fmt.Errorf("kit packages: %s root changed", name)
		}
	}
	if s.persistent {
		return s.CheckPersistent()
	}
	return nil
}

func realDirectories(path string, create bool) error {
	parent := filepath.Dir(path)
	if parent != path {
		if err := realDirectories(parent, create); err != nil {
			return err
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		// Persist a newly created library's directory entry as well as its
		// contents, including when its private parent had to be created.
		parentDirectory, openErr := os.Open(parent)
		if openErr != nil {
			return openErr
		}
		syncErr := parentDirectory.Sync()
		closeErr := parentDirectory.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		info, err = os.Lstat(path)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("kit packages: path %s must be a real directory", path)
	}
	return nil
}

func directoryEntries(r *os.Root, name string) ([]os.DirEntry, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

func syncDirectory(r *os.Root, name string) error {
	f, err := r.Open(name)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

// Only private, recognizable incomplete publications are reclaimed under the
// cross-process lock. Immutable installed packages are never removed.
func cleanTemporaries(r *os.Root) error {
	entries, err := directoryEntries(r, "packages")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !stageRE.MatchString(entry.Name()) {
			continue
		}
		name := filepath.Join("packages", entry.Name())
		info, err := r.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("kit packages: invalid staging entry")
		}
		if err := r.Chmod(name, 0o700); err != nil {
			return err
		}
		if err := r.RemoveAll(name); err != nil {
			return err
		}
	}
	entries, err = directoryEntries(r, ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !selectionTemporaryRE.MatchString(entry.Name()) {
			continue
		}
		info, err := r.Lstat(entry.Name())
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.New("kit packages: invalid selection temporary")
		}
		if err := r.Remove(entry.Name()); err != nil {
			return err
		}
	}
	entries, err = directoryEntries(r, "archives")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !archiveTemporaryRE.MatchString(entry.Name()) {
			continue
		}
		name := filepath.Join("archives", entry.Name())
		info, err := r.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
			return errors.New("kit packages: invalid archive temporary")
		}
		if err := r.Remove(name); err != nil {
			return err
		}
	}
	return nil
}
