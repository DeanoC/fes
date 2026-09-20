package metadata

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gosqlite.org/vfs"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	sqliteMainLeaf    = "cache.sqlite3"
	sqliteWALLeaf     = "cache.sqlite3-wal"
	sqliteJournalLeaf = "cache.sqlite3-journal"
	sqliteSHMLeaf     = "cache.sqlite3-shm"
)

var sqliteDerivedLeaves = [...]string{
	sqliteMainLeaf,
	sqliteWALLeaf,
	sqliteJournalLeaf,
	sqliteSHMLeaf,
}

func isSQLiteDerivedLeaf(name string) bool {
	for _, allowed := range sqliteDerivedLeaves {
		if name == allowed {
			return true
		}
	}
	return false
}

type sqliteVFSHooks struct {
	beforeOpen func(name string)
	afterOpen  func(name string, file *os.File)
	beforeSync func(name string)
}

var sqliteVFSHookState struct {
	sync.Mutex
	hooks sqliteVFSHooks
}

func setSQLiteVFSHooksForTest(hooks sqliteVFSHooks) func() {
	sqliteVFSHookState.Lock()
	previous := sqliteVFSHookState.hooks
	sqliteVFSHookState.hooks = hooks
	sqliteVFSHookState.Unlock()
	return func() {
		sqliteVFSHookState.Lock()
		sqliteVFSHookState.hooks = previous
		sqliteVFSHookState.Unlock()
	}
}

func currentSQLiteVFSHooks() sqliteVFSHooks {
	sqliteVFSHookState.Lock()
	defer sqliteVFSHookState.Unlock()
	return sqliteVFSHookState.hooks
}

var sqliteVFSSequence atomic.Uint64

var cacheRootOwnership struct {
	sync.Mutex
	roots map[*cacheRootLease]struct{}
}

type cacheRootLease struct {
	info os.FileInfo
	one  sync.Once
}

type rootedSQLiteVFSRetainedError struct {
	vfs   *rootedSQLiteVFS
	conn  *sql.Conn
	db    *sql.DB
	cause error
}

func (e *rootedSQLiteVFSRetainedError) Error() string { return e.cause.Error() }
func (e *rootedSQLiteVFSRetainedError) Unwrap() error { return e.cause }

const sqliteRetirementRetryInterval = 10 * time.Millisecond

type sqliteRetirement struct {
	owner         *privateRoot
	artwork       *artworkDirectory
	lease         *cacheRootLease
	vfs           *rootedSQLiteVFS
	conn          *sql.Conn
	db            *sql.DB
	purge         bool
	removeCreated bool
	done          chan struct{}

	mu  sync.Mutex
	err error
}

func newSQLiteRetirement(owner *privateRoot, artwork *artworkDirectory, lease *cacheRootLease, sqliteVFS *rootedSQLiteVFS, purge, removeCreated bool) *sqliteRetirement {
	return newSQLiteRetirementWithSQL(owner, artwork, lease, sqliteVFS, nil, nil, purge, removeCreated)
}

func newSQLiteRetirementWithSQL(owner *privateRoot, artwork *artworkDirectory, lease *cacheRootLease, sqliteVFS *rootedSQLiteVFS, conn *sql.Conn, db *sql.DB, purge, removeCreated bool) *sqliteRetirement {
	return &sqliteRetirement{
		owner: owner, artwork: artwork, lease: lease, vfs: sqliteVFS, conn: conn, db: db,
		purge: purge, removeCreated: removeCreated, done: make(chan struct{}),
	}
}

func (r *sqliteRetirement) finish(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
	close(r.done)
}

func (r *sqliteRetirement) error() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

var sqliteRetirementManager struct {
	sync.Mutex
	queue   []*sqliteRetirement
	active  int
	running bool
}

func enqueueSQLiteRetirement(record *sqliteRetirement) {
	if record == nil {
		return
	}
	if record.vfs != nil {
		record.vfs.markRetiring()
	}
	sqliteRetirementManager.Lock()
	sqliteRetirementManager.queue = append(sqliteRetirementManager.queue, record)
	if !sqliteRetirementManager.running {
		sqliteRetirementManager.running = true
		go runSQLiteRetirementManager()
	}
	sqliteRetirementManager.Unlock()
}

func sqliteRetirementCount() int {
	sqliteRetirementManager.Lock()
	defer sqliteRetirementManager.Unlock()
	return len(sqliteRetirementManager.queue) + sqliteRetirementManager.active
}

func runSQLiteRetirementManager() {
	for {
		sqliteRetirementManager.Lock()
		if len(sqliteRetirementManager.queue) == 0 {
			sqliteRetirementManager.running = false
			sqliteRetirementManager.Unlock()
			return
		}
		record := sqliteRetirementManager.queue[0]
		sqliteRetirementManager.queue = sqliteRetirementManager.queue[1:]
		sqliteRetirementManager.active++
		sqliteRetirementManager.Unlock()

		retry, err := retireSQLiteRecord(record)

		sqliteRetirementManager.Lock()
		sqliteRetirementManager.active--
		if retry {
			sqliteRetirementManager.queue = append(sqliteRetirementManager.queue, record)
		}
		sqliteRetirementManager.Unlock()
		if retry {
			time.Sleep(sqliteRetirementRetryInterval)
			continue
		}
		record.finish(err)
	}
}

func retireSQLiteRecord(record *sqliteRetirement) (bool, error) {
	if record == nil {
		return false, nil
	}
	var errs []error
	if record.conn != nil {
		if err := record.conn.Close(); err != nil {
			errs = append(errs, err)
		} else {
			record.conn = nil
		}
	}
	if record.db != nil {
		if err := record.db.Close(); err != nil {
			errs = append(errs, err)
		} else {
			record.db = nil
		}
	}
	if record.conn != nil || record.db != nil {
		return true, errors.Join(errs...)
	}
	if record.vfs != nil {
		if _, registered := vfs.Find(record.vfs.logicalPrefix); registered {
			if err := unregisterRootedSQLiteVFS(record.vfs); err != nil {
				if _, stillRegistered := vfs.Find(record.vfs.logicalPrefix); stillRegistered {
					return true, errors.Join(append(errs, err)...)
				}
				errs = append(errs, err)
			}
		}
	}
	if record.purge && record.owner != nil {
		if err := purgeDerivedFilesOwned(record.owner, record.artwork); err != nil {
			errs = append(errs, err)
		}
	}
	if record.artwork != nil {
		if record.removeCreated {
			errs = append(errs, record.artwork.removeCreated())
		}
		if err := record.artwork.close(); err != nil {
			errs = append(errs, err)
		}
	}
	if record.owner != nil {
		if err := record.owner.abort(record.removeCreated); err != nil {
			errs = append(errs, err)
		}
	}
	if record.lease != nil {
		record.lease.release()
	}
	return false, errors.Join(errs...)
}

func acquireCacheRootLease(info os.FileInfo) (*cacheRootLease, error) {
	if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("metadata cache root identity is unavailable")
	}
	cacheRootOwnership.Lock()
	defer cacheRootOwnership.Unlock()
	if cacheRootOwnership.roots == nil {
		cacheRootOwnership.roots = make(map[*cacheRootLease]struct{})
	}
	for lease := range cacheRootOwnership.roots {
		if os.SameFile(lease.info, info) {
			return nil, errors.New("metadata cache root is already open")
		}
	}
	lease := &cacheRootLease{info: info}
	cacheRootOwnership.roots[lease] = struct{}{}
	return lease, nil
}

func (l *cacheRootLease) release() {
	if l == nil {
		return
	}
	l.one.Do(func() {
		cacheRootOwnership.Lock()
		defer cacheRootOwnership.Unlock()
		delete(cacheRootOwnership.roots, l)
	})
}

func registerRootedSQLiteVFS(v *rootedSQLiteVFS) error {
	return vfs.Register(v.logicalPrefix, v)
}

func unregisterRootedSQLiteVFS(v *rootedSQLiteVFS) error {
	if v == nil {
		return nil
	}
	if err := vfs.Unregister(v.logicalPrefix); err != nil {
		if _, ok := vfs.Find(v.logicalPrefix); ok {
			return &rootedSQLiteVFSRetainedError{vfs: v, cause: err}
		}
		return err
	}
	if _, ok := vfs.Find(v.logicalPrefix); ok {
		return &rootedSQLiteVFSRetainedError{vfs: v, cause: errors.New("metadata SQLite VFS remained registered")}
	}
	return nil
}

func rootedSQLiteVFSRegistered(v *rootedSQLiteVFS) bool {
	if v == nil {
		return false
	}
	_, ok := vfs.Find(v.logicalPrefix)
	return ok
}

type rootedSQLiteVFS struct {
	root          *os.Root
	logicalPrefix string
	shmGroup      string
	advisoryLock  vfs.AdvisoryLock
	retiring      atomic.Bool

	openedMu     sync.Mutex
	openedLeaves []string

	hooksMu sync.Mutex
	hooks   sqliteVFSHooks
}

func newRootedSQLiteVFS(root *os.Root) *rootedSQLiteVFS {
	sequence := sqliteVFSSequence.Add(1)
	prefix := fmt.Sprintf("fogcast-cache-%d", sequence)
	return &rootedSQLiteVFS{
		root:          root,
		logicalPrefix: prefix,
		shmGroup:      prefix,
		hooks:         currentSQLiteVFSHooks(),
	}
}

func (v *rootedSQLiteVFS) setHooksForTest(hooks sqliteVFSHooks) func() {
	v.hooksMu.Lock()
	previous := v.hooks
	v.hooks = hooks
	v.hooksMu.Unlock()
	return func() {
		v.hooksMu.Lock()
		v.hooks = previous
		v.hooksMu.Unlock()
	}
}

func (v *rootedSQLiteVFS) hooksSnapshot() sqliteVFSHooks {
	v.hooksMu.Lock()
	defer v.hooksMu.Unlock()
	return v.hooks
}

func (v *rootedSQLiteVFS) openedLeavesSnapshot() []string {
	v.openedMu.Lock()
	defer v.openedMu.Unlock()
	return append([]string(nil), v.openedLeaves...)
}

func (v *rootedSQLiteVFS) markRetiring() {
	if v != nil {
		v.retiring.Store(true)
	}
}

func (v *rootedSQLiteVFS) recordOpened(leaf string) {
	v.openedMu.Lock()
	v.openedLeaves = append(v.openedLeaves, leaf)
	v.openedMu.Unlock()
}

func (v *rootedSQLiteVFS) normalizeName(name string) (string, error) {
	if name == "" {
		return "", sqliteVFSFailure(sqlite3.SQLITE_CANTOPEN, errors.New("anonymous SQLite files are disabled"))
	}
	if isSQLiteDerivedLeaf(name) {
		return name, nil
	}
	prefix := v.logicalPrefix + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", sqliteVFSFailure(sqlite3.SQLITE_CANTOPEN, errors.New("SQLite filename is outside the cache namespace"))
	}
	leaf := strings.TrimPrefix(name, prefix)
	if isSQLiteDerivedLeaf(leaf) {
		return leaf, nil
	}
	return "", sqliteVFSFailure(sqlite3.SQLITE_CANTOPEN, errors.New("SQLite filename is not an allowed cache leaf"))
}

func (v *rootedSQLiteVFS) FullPathname(name string) (string, error) {
	leaf, err := v.normalizeName(name)
	if err != nil {
		return "", err
	}
	return v.logicalPrefix + "/" + leaf, nil
}

func (v *rootedSQLiteVFS) Open(name string, flags vfs.OpenFlags) (vfs.File, vfs.OpenFlags, error) {
	if v.retiring.Load() {
		return nil, 0, sqliteVFSFailure(sqlite3.SQLITE_CANTOPEN, errors.New("SQLite VFS is retiring"))
	}
	leaf, err := v.normalizeName(name)
	if err != nil {
		return nil, 0, err
	}
	if flags.Has(vfs.OpenTempDB) || flags.Has(vfs.OpenTempJournal) || flags.Has(vfs.OpenTransientDB) || flags.Has(vfs.OpenSubJournal) || flags.Has(vfs.OpenDeleteOnClose) {
		return nil, 0, sqliteVFSFailure(sqlite3.SQLITE_CANTOPEN, errors.New("temporary or delete-on-close SQLite files are disabled"))
	}
	if flags.Has(vfs.OpenExclusive) && !flags.Has(vfs.OpenCreate) {
		return nil, 0, sqliteVFSFailure(sqlite3.SQLITE_CANTOPEN, errors.New("exclusive SQLite opens must create"))
	}
	if flags.Has(vfs.OpenReadOnly) && flags.Has(vfs.OpenReadWrite) {
		return nil, 0, sqliteVFSFailure(sqlite3.SQLITE_CANTOPEN, errors.New("conflicting SQLite access flags"))
	}
	access := os.O_RDWR
	if flags.Has(vfs.OpenReadOnly) && !flags.Has(vfs.OpenReadWrite) {
		access = os.O_RDONLY
	}

	hooks := v.hooksSnapshot()
	if hooks.beforeOpen != nil {
		hooks.beforeOpen(name)
	}

	var file *os.File
	if flags.Has(vfs.OpenExclusive) {
		file, err = v.root.OpenFile(leaf, access|os.O_CREATE|os.O_EXCL, 0o600)
	} else {
		file, err = v.root.OpenFile(leaf, access, 0)
		if errors.Is(err, fs.ErrNotExist) && flags.Has(vfs.OpenCreate) {
			file, err = v.root.OpenFile(leaf, access|os.O_CREATE|os.O_EXCL, 0o600)
		}
	}
	if err != nil {
		return nil, 0, sqliteVFSFailure(sqlite3.SQLITE_CANTOPEN, err)
	}

	if hooks.afterOpen != nil {
		hooks.afterOpen(name, file)
	}
	if err := v.verifyLeaf(file, leaf); err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, 0, sqliteVFSFailure(sqlite3.SQLITE_IOERR, err)
	}
	v.recordOpened(leaf)
	return &rootedSQLiteFile{owner: v, file: file, leaf: leaf}, flags, nil
}

func (v *rootedSQLiteVFS) verifyLeaf(file *os.File, leaf string) error {
	opened, err := file.Stat()
	if err != nil {
		return sqliteVFSFailure(sqlite3.SQLITE_IOERR_FSTAT, err)
	}
	current, err := v.root.Lstat(leaf)
	if err != nil {
		return sqliteVFSFailure(sqlite3.SQLITE_IOERR, err)
	}
	if opened.Mode()&os.ModeSymlink != 0 || !opened.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return sqliteVFSFailure(sqlite3.SQLITE_IOERR, errors.New("SQLite cache leaf identity is unsafe"))
	}
	return nil
}

func (v *rootedSQLiteVFS) verifyReadableLeaf(leaf string, access int) (*os.File, error) {
	file, err := v.root.OpenFile(leaf, access, 0)
	if err != nil {
		return nil, err
	}
	if err := v.verifyLeaf(file, leaf); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (v *rootedSQLiteVFS) Delete(name string, syncDir bool) error {
	leaf, err := v.normalizeName(name)
	if err != nil {
		return err
	}
	if err := v.root.Remove(leaf); err != nil {
		return err
	}
	if !syncDir {
		return nil
	}
	directory, err := v.root.Open(".")
	if err != nil {
		return sqliteVFSFailure(sqlite3.SQLITE_IOERR_DELETE, err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}

func (v *rootedSQLiteVFS) Access(name string, op vfs.AccessOp) (bool, error) {
	leaf, err := v.normalizeName(name)
	if err != nil {
		return false, err
	}
	info, err := v.root.Lstat(leaf)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, sqliteVFSFailure(sqlite3.SQLITE_IOERR, errors.New("SQLite cache leaf is not a regular file"))
	}
	if op == vfs.AccessExists {
		return true, nil
	}
	access := os.O_RDONLY
	if op == vfs.AccessReadWrite {
		access = os.O_RDWR
	}
	file, err := v.verifyReadableLeaf(leaf, access)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func sqliteVFSFailure(code int, err error) error {
	return &vfs.VFSError{Code: code, Err: err}
}

type rootedSQLiteFile struct {
	owner *rootedSQLiteVFS
	file  *os.File
	leaf  string
	lock  vfs.LockLevel

	closeOnce sync.Once
	closeErr  error
}

func (f *rootedSQLiteFile) ReadAt(p []byte, off int64) (int, error) {
	return f.file.ReadAt(p, off)
}

func (f *rootedSQLiteFile) WriteAt(p []byte, off int64) (int, error) {
	return f.file.WriteAt(p, off)
}

func (f *rootedSQLiteFile) Truncate(size int64) error {
	return f.file.Truncate(size)
}

func (f *rootedSQLiteFile) Sync(_ vfs.SyncFlags) error {
	hooks := f.owner.hooksSnapshot()
	if hooks.beforeSync != nil {
		hooks.beforeSync(f.leaf)
	}
	return f.file.Sync()
}

func (f *rootedSQLiteFile) Size() (int64, error) {
	info, err := f.file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (f *rootedSQLiteFile) Lock(level vfs.LockLevel) error {
	return f.owner.advisoryLock.Lock(f, &f.lock, level)
}

func (f *rootedSQLiteFile) Unlock(level vfs.LockLevel) error {
	return f.owner.advisoryLock.Unlock(f, &f.lock, level)
}

func (f *rootedSQLiteFile) CheckReservedLock() (bool, error) {
	return f.owner.advisoryLock.CheckReservedLock()
}

func (f *rootedSQLiteFile) SectorSize() int {
	return 4096
}

func (f *rootedSQLiteFile) DeviceCharacteristics() vfs.DeviceFlags {
	return 0
}

func (f *rootedSQLiteFile) ShmGroup() string {
	return f.owner.shmGroup
}

func (f *rootedSQLiteFile) Close() error {
	f.closeOnce.Do(func() {
		_ = f.owner.advisoryLock.Unlock(f, &f.lock, vfs.LockNone)
		f.closeErr = f.file.Close()
	})
	return f.closeErr
}

var (
	_ vfs.VFS     = (*rootedSQLiteVFS)(nil)
	_ vfs.ShmFile = (*rootedSQLiteFile)(nil)
	_ vfs.File    = (*rootedSQLiteFile)(nil)
	_ io.ReaderAt = (*rootedSQLiteFile)(nil)
)
