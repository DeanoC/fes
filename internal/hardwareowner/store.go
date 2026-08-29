package hardwareowner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	OwnerPath = "/var/lib/fogcast/hardware-owner-v1.json"
	LockPath  = "/run/fogcast/hardware-owner-v1.lock"
)

// Store is the durable owner-record store. ExpectedUID is explicit so host
// tests can use their own temporary directory ownership; production
// constructors always set it to root (0).
type Store struct {
	Path        string
	ExpectedUID uint32

	// These seams keep failure-preservation and fsync behavior testable without
	// replacing the real filesystem in ordinary tests. They are intentionally
	// private; production callers use the real operations.
	syncFile   func(*os.File) error
	syncParent func(*os.File) error
	rename     func(string, string) error
}

// NewStore constructs a store for path. If expectedUID is omitted, root (0) is
// required, which is the production ownership policy.
func NewStore(path string, expectedUID ...uint32) *Store {
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	return &Store{Path: path, ExpectedUID: uid}
}

// NewProductionStore returns the root-owned store used by the target profile.
func NewProductionStore() *Store { return NewStore(OwnerPath) }

// Load reads and validates the owner record. An absent record is returned as
// exists=false only when its already-validated parent directory is present;
// malformed, unsafe, or unreadable existing records fail closed.
func (s Store) Load() (record Record, exists bool, err error) {
	if err := s.validatePath(); err != nil {
		return Record{}, false, err
	}
	if err := ensureSecureParent(filepath.Dir(s.Path), s.ExpectedUID); err != nil {
		return Record{}, false, err
	}
	info, err := os.Lstat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("inspect owner record: %w", err)
	}
	if err := validateRecordInfo(info, s.ExpectedUID); err != nil {
		return Record{}, true, err
	}
	data, err := readOwnerFile(s.Path, s.ExpectedUID)
	if err != nil {
		return Record{}, true, err
	}
	record, err = Parse(data)
	if err != nil {
		return Record{}, true, fmt.Errorf("parse owner record: %w", err)
	}
	return record, true, nil
}

// Replace atomically replaces the owner record after validating the complete
// candidate. The temporary file is created in the same secure directory,
// written canonically, file-synced, renamed, and parent-directory-synced.
// Existing bytes are restored if a post-rename operation reports failure.
func (s Store) Replace(record Record) error {
	if err := s.validatePath(); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return fmt.Errorf("validate owner record: %w", err)
	}
	if err := ensureSecureParent(filepath.Dir(s.Path), s.ExpectedUID); err != nil {
		return err
	}

	oldBytes, hadOld, err := s.readExistingForReplacement()
	if err != nil {
		return err
	}
	if hadOld {
		previous, err := Parse(oldBytes)
		if err != nil {
			return fmt.Errorf("parse existing owner record: %w", err)
		}
		if err := record.ValidateTransition(previous); err != nil {
			return fmt.Errorf("validate owner-record transition: %w", err)
		}
	}
	data, err := record.MarshalCanonical()
	if err != nil {
		return fmt.Errorf("marshal owner record: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(s.Path), "."+filepath.Base(s.Path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create owner-record temporary: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set owner-record temporary mode: %w", err)
	}
	if info, err := temporary.Stat(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("inspect owner-record temporary: %w", err)
	} else if err := validateRecordInfo(info, s.ExpectedUID); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("validate owner-record temporary: %w", err)
	}
	n, err := temporary.Write(data)
	if err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write owner record: %w", err)
	}
	if n != len(data) {
		_ = temporary.Close()
		return io.ErrShortWrite
	}
	if err := s.syncOpenFile(temporary); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("fsync owner record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close owner-record temporary: %w", err)
	}
	err = s.renamePath(temporaryPath, s.Path)
	if err != nil {
		return fmt.Errorf("replace owner record: %w", err)
	}
	removeTemporary = false

	parent, err := os.Open(filepath.Dir(s.Path))
	if err != nil {
		return s.restoreAfterFailure(hadOld, oldBytes, fmt.Errorf("open owner-record parent: %w", err))
	}
	syncErr := s.syncOpenParent(parent)
	closeErr := parent.Close()
	if syncErr != nil {
		return s.restoreAfterFailure(hadOld, oldBytes, fmt.Errorf("fsync owner-record parent: %w", syncErr))
	}
	if closeErr != nil {
		return s.restoreAfterFailure(hadOld, oldBytes, fmt.Errorf("close owner-record parent: %w", closeErr))
	}
	if info, err := os.Lstat(s.Path); err != nil {
		return s.restoreAfterFailure(hadOld, oldBytes, fmt.Errorf("inspect replaced owner record: %w", err))
	} else if err := validateRecordInfo(info, s.ExpectedUID); err != nil {
		return s.restoreAfterFailure(hadOld, oldBytes, fmt.Errorf("validate replaced owner record: %w", err))
	}
	if finalBytes, err := readOwnerFile(s.Path, s.ExpectedUID); err != nil {
		return s.restoreAfterFailure(hadOld, oldBytes, fmt.Errorf("read replaced owner record: %w", err))
	} else if !bytes.Equal(finalBytes, data) {
		return s.restoreAfterFailure(hadOld, oldBytes, fmt.Errorf("replaced owner record differs from canonical candidate"))
	}
	return nil
}

func (s Store) readExistingForReplacement() ([]byte, bool, error) {
	info, err := os.Lstat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect existing owner record: %w", err)
	}
	if err := validateRecordInfo(info, s.ExpectedUID); err != nil {
		return nil, false, err
	}
	data, err := readOwnerFile(s.Path, s.ExpectedUID)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s Store) restoreAfterFailure(hadOld bool, oldBytes []byte, cause error) error {
	if hadOld {
		if err := s.restoreBytesAtomically(oldBytes); err != nil {
			return fmt.Errorf("%w (restore previous owner record: %v)", cause, err)
		}
		return cause
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w (remove failed owner record: %v)", cause, err)
	}
	if err := s.syncParentDirectory(); err != nil {
		return fmt.Errorf("%w (fsync owner-record parent after removal: %v)", cause, err)
	}
	return cause
}

func (s Store) restoreBytesAtomically(oldBytes []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(s.Path), "."+filepath.Base(s.Path)+".rollback-*")
	if err != nil {
		return fmt.Errorf("create rollback temporary: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set rollback temporary mode: %w", err)
	}
	if info, err := temporary.Stat(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("inspect rollback temporary: %w", err)
	} else if err := validateRecordInfo(info, s.ExpectedUID); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("validate rollback temporary: %w", err)
	}
	n, err := temporary.Write(oldBytes)
	if err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write rollback owner record: %w", err)
	}
	if n != len(oldBytes) {
		_ = temporary.Close()
		return io.ErrShortWrite
	}
	if err := s.syncOpenFile(temporary); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("fsync rollback temporary: %w", err)
	}
	if err := s.renamePath(temporaryPath, s.Path); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("replace owner record with rollback: %w", err)
	}
	removeTemporary = false
	if err := s.syncOpenFile(temporary); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("fsync restored owner record: %w", err)
	}
	closeErr := temporary.Close()
	parentErr := s.syncParentDirectory()
	if closeErr != nil {
		if parentErr != nil {
			return fmt.Errorf("close restored owner record: %v (also fsync owner-record parent: %v)", closeErr, parentErr)
		}
		return fmt.Errorf("close restored owner record: %w", closeErr)
	}
	if parentErr != nil {
		return parentErr
	}
	info, err := os.Lstat(s.Path)
	if err != nil {
		return fmt.Errorf("inspect restored owner record: %w", err)
	}
	if err := validateRecordInfo(info, s.ExpectedUID); err != nil {
		return fmt.Errorf("validate restored owner record: %w", err)
	}
	got, err := readOwnerFile(s.Path, s.ExpectedUID)
	if err != nil {
		return fmt.Errorf("read restored owner record: %w", err)
	}
	if !bytes.Equal(got, oldBytes) {
		return fmt.Errorf("restored owner record differs from previous bytes")
	}
	return nil
}

func (s Store) syncOpenFile(file *os.File) error {
	if s.syncFile != nil {
		return s.syncFile(file)
	}
	return file.Sync()
}

func (s Store) syncOpenParent(parent *os.File) error {
	if s.syncParent != nil {
		return s.syncParent(parent)
	}
	return parent.Sync()
}

func (s Store) renamePath(oldPath, newPath string) error {
	if s.rename != nil {
		return s.rename(oldPath, newPath)
	}
	return os.Rename(oldPath, newPath)
}

func (s Store) syncParentDirectory() error {
	parent, err := os.Open(filepath.Dir(s.Path))
	if err != nil {
		return fmt.Errorf("open owner-record parent: %w", err)
	}
	syncErr := s.syncOpenParent(parent)
	closeErr := parent.Close()
	if syncErr != nil {
		return fmt.Errorf("fsync owner-record parent: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close owner-record parent: %w", closeErr)
	}
	return nil
}

func (s Store) validatePath() error {
	if strings.TrimSpace(s.Path) == "" || filepath.Base(s.Path) == "." || filepath.Base(s.Path) == string(filepath.Separator) {
		return fmt.Errorf("owner record path is required")
	}
	return nil
}

func ensureSecureParent(parent string, expectedUID uint32) error {
	if parent == "" {
		return fmt.Errorf("owner record parent is required")
	}
	if err := ensureNoSymlinkPath(parent); err != nil {
		return err
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect owner-record parent: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("owner-record parent is not a directory")
	}
	if info.Mode().Perm() != 0o700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("owner-record parent mode is %o, want 700", info.Mode().Perm())
	}
	uid, ok := statUID(info)
	if !ok || uid != expectedUID {
		return fmt.Errorf("owner-record parent uid is %d, want %d", uid, expectedUID)
	}
	return nil
}

func ensureNoSymlinkPath(path string) error {
	clean := filepath.Clean(path)
	for current := clean; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect owner-record path component %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("owner-record path component %q is a symlink", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func validateRecordInfo(info fs.FileInfo, expectedUID uint32) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("owner record is a symlink")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("owner record is not a regular file")
	}
	if info.Mode().Perm() != 0o600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("owner record mode is %o, want 600", info.Mode().Perm())
	}
	uid, ok := statUID(info)
	if !ok || uid != expectedUID {
		return fmt.Errorf("owner record uid is %d, want %d", uid, expectedUID)
	}
	return nil
}

func readOwnerFile(path string, expectedUID uint32) ([]byte, error) {
	file, err := openOwnerRead(path)
	if err != nil {
		return nil, fmt.Errorf("open owner record: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat owner record: %w", err)
	}
	if err := validateRecordInfo(info, expectedUID); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read owner record: %w", err)
	}
	return data, nil
}
