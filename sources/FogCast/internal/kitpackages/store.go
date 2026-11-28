// Package kitpackages retains installed target core packages independently of
// the appliance image. Installing and selecting never activate FPGA hardware.
package kitpackages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sync"
	"sync/atomic"

	"github.com/DeanoC/FogCast/corepackage"
	"golang.org/x/sync/errgroup"
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
	hostView   bool

	verifyMu sync.Mutex
	verified map[string]verifiedPackage
}

// fullVerificationReads counts payload inspections performed by read.
// Listing that trusts a matching stat fingerprint does not increment it.
var fullVerificationReads atomic.Int64

// Limit retained upload buffers across every kit Store in this process. The
// host and agent have separate processes, so each can retain at most one.
// Hold admission through publication, including while waiting for the flock.
var importAdmission = make(chan struct{}, 1)

// New opens a private POSIX store. Production callers use OpenPersistent;
// accepting a temporary directory here does not prove target persistence.
func New(path string) (*Store, error) {
	return openStore(path, true)
}

// openStore opens the host view without recovery when recoverInstallations is
// false. The agent owns recovery; package access verifies on a cache miss.
func openStore(path string, recoverInstallations bool) (*Store, error) {
	return openStoreContext(context.Background(), path, recoverInstallations)
}

// openStoreContext checks cancellation between filesystem operations. An OS
// call already in progress cannot be interrupted by a Go context.
func openStoreContext(ctx context.Context, path string, recoverInstallations bool) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("kit packages: missing open context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("kit packages: root must be a clean absolute path")
	}
	if err := realDirectoriesContext(ctx, path, true); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil || info.Mode().Perm() != 0o700 {
		return nil, errors.New("kit packages: root must be private (0700)")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opened, err := r.Stat(".")
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("kit packages: root changed while opening")
	}
	s := &Store{root: path, info: opened, children: make(map[string]os.FileInfo), verified: make(map[string]verifiedPackage), hostView: !recoverInstallations}
	for _, name := range []string{"archives", "packages"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mkdirErr := r.Mkdir(name, 0o700)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return nil, mkdirErr
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		child, err := r.Lstat(name)
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		if err != nil || !child.IsDir() || child.Mode()&os.ModeSymlink != 0 || child.Mode().Perm() != 0o700 {
			return nil, fmt.Errorf("kit packages: %s must be a private real directory", name)
		}
		s.children[name] = child
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	existing, lockStatErr := r.Lstat(".lock")
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if lockStatErr == nil {
		if !existing.Mode().IsRegular() || existing.Mode().Perm() != 0o600 {
			return nil, errors.New("kit packages: lock must be a private regular file")
		}
	} else if !errors.Is(lockStatErr, os.ErrNotExist) {
		return nil, lockStatErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lock, err := r.OpenFile(".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		_ = lock.Close()
		return nil, contextErr
	}
	s.lockInfo, err = lock.Stat()
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	if err == nil {
		if contextErr := ctx.Err(); contextErr != nil {
			err = contextErr
		} else {
			err = lock.Sync()
			if contextErr := ctx.Err(); contextErr != nil {
				err = contextErr
			}
		}
	}
	closeErr := lock.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	linkInfo, err := r.Lstat(".lock")
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil || !linkInfo.Mode().IsRegular() || linkInfo.Mode().Perm() != 0o600 || !os.SameFile(s.lockInfo, linkInfo) {
		return nil, errors.New("kit packages: lock must be a private regular file")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.archives, err = corepackage.NewStore(filepath.Join(path, "archives"))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := syncDirectoryContext(ctx, r, "."); err != nil {
		return nil, err
	}
	if recoverInstallations {
		openedRoot, unlock, err := s.lock(ctx)
		if err != nil {
			return nil, err
		}
		defer unlock()
		if err := s.recover(ctx, openedRoot); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Install authenticates a canonical archive using the shared package reader,
// and publishes its sealed members at packages/<package-id>. A successful
// retry is idempotent; no selection is made and all earlier versions remain.
func (s *Store) Install(ctx context.Context, length int64, reader io.Reader) (corepackage.Inspection, bool, error) {
	if ctx == nil {
		return corepackage.Inspection{}, false, errors.New("kit packages: missing import context")
	}
	select {
	case importAdmission <- struct{}{}:
		defer func() { <-importAdmission }()
	case <-ctx.Done():
		return corepackage.Inspection{}, false, ctx.Err()
	}
	// Upload and validate in private memory before taking the cross-process
	// store lock. Publication and selected-package checks stay under it.
	prepared, err := corepackage.PrepareImport(ctx, length, reader)
	if err != nil {
		return corepackage.Inspection{}, false, err
	}
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
	inspection, _, err := s.archives.ImportPrepared(ctx, prepared)
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
	staged, err := corepackage.StageBytes(ctx, filepath.Join(s.root, "packages"), data)
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
	// A complete publication needs one archive/member verification per package.
	// selections has already verified selected packages in this process; reuse
	// those exact stat-bound results instead of hashing them again. A missing
	// publication still takes the archive recovery path below.
	if ids, err := installedIDs(r); err == nil {
		// Two workers use both kit CPUs without retaining an unbounded number
		// of payload buffers. Wait for all checks before publishing the store.
		checks, checkContext := errgroup.WithContext(ctx)
		checks.SetLimit(2)
		for _, id := range ids {
			checks.Go(func() error {
				_, err := s.listingInspection(checkContext, r, id)
				return err
			})
		}
		return checks.Wait()
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
	if err := s.validateHostView(ctx, r); err != nil {
		return nil, err
	}
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
	if err := s.validateHostView(ctx, r); err != nil {
		return corepackage.Inspection{}, nil, err
	}
	return s.read(ctx, r, id)
}

// ReadCached is Read that trusts the stat-fingerprint cache, as List does:
// an unchanged package returns its archive bytes without hashing the unpacked
// members or decoding a ROM map again. The kit host sends those bytes to
// mister-agent, which verifies them before programming; the agent's own
// selection and launch paths keep the full Read.
func (s *Store) ReadCached(ctx context.Context, id string) (corepackage.Inspection, []byte, error) {
	if !packageIDRE.MatchString(id) {
		return corepackage.Inspection{}, nil, ErrInvalidPackageID
	}
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	defer unlock()
	if err := s.validateHostView(ctx, r); err != nil {
		return corepackage.Inspection{}, nil, err
	}
	if key, err := packageStatKey(r, id); err == nil {
		if cached, ok := s.cachedVerified(id, key); ok {
			inspection, data, err := s.archives.Read(ctx, id)
			if err != nil {
				return corepackage.Inspection{}, nil, err
			}
			if inspection.PackageID != cached.PackageID {
				return corepackage.Inspection{}, nil, errors.New("kit packages: installed archive identity changed")
			}
			return inspection, data, nil
		}
	}
	return s.read(ctx, r, id)
}

// Inspect is Read without the archive bytes. An unchanged package is not
// read or hashed again.
func (s *Store) Inspect(ctx context.Context, id string) (corepackage.Inspection, error) {
	if !packageIDRE.MatchString(id) {
		return corepackage.Inspection{}, ErrInvalidPackageID
	}
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return corepackage.Inspection{}, err
	}
	defer unlock()
	if err := s.validateHostView(ctx, r); err != nil {
		return corepackage.Inspection{}, err
	}
	return s.listingInspection(ctx, r, id)
}

// A host opener skips recovery, so check the durable inventory and selections
// lazily under the same lock used for each read. In particular, a missing
// selected package is a store error, not a request to use a factory package.
func (s *Store) validateHostView(ctx context.Context, r *os.Root) error {
	if !s.hostView {
		return nil
	}
	if _, err := installedIDs(r); err != nil {
		return fmt.Errorf("kit packages: invalid durable inventory: %w", err)
	}
	if _, err := s.selectedPackages(ctx, r); err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			return errors.New("kit packages: selected package is missing")
		}
		return fmt.Errorf("kit packages: invalid durable selections: %w", err)
	}
	return nil
}

// Path returns a local launch path only after authenticating both retained
// representations. Network callers never choose or receive filesystem paths.
func (s *Store) Path(ctx context.Context, id string) (string, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := s.validateHostView(ctx, r); err != nil {
		return "", err
	}
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
	// The archive was fully validated above. Compare each installed byte to
	// that archive rather than parsing identical ROM maps a second time.
	if err := corepackage.MatchDirectoryToArchive(ctx, filepath.Join(s.root, name), data); err != nil {
		return corepackage.Inspection{}, nil, err
	}
	after, err := r.Lstat(name)
	if err != nil || !os.SameFile(info, after) {
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
