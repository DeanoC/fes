package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	_ "modernc.org/sqlite"
)

type Candidate struct {
	ID, Title, RelativePath, Reason string
	System                          protocol.System
	Kind                            SourceKind
	State                           SourceState
	Fingerprint                     Fingerprint
}

type Change string

const (
	ChangeAdded     Change = "added"
	ChangeUpdated   Change = "updated"
	ChangeUnchanged Change = "unchanged"
)

type RootReport struct {
	RootID                                      string
	System                                      protocol.System
	Added, Updated, Unchanged, Invalid, Missing int
	Offline                                     bool
	Reason                                      string
}

type Store struct {
	db                     *sql.DB
	scanLeaseDirectory     string
	scanLeaseDirectoryErr  error
	scanLeaseDirectoryOnce sync.Once
	memoryScanLease        chan struct{}
}

const maxCatalogOpenConnections = 4

// ContentLaunchAdmission holds SQLite's writer reservation while a caller
// launches content that matched the catalog snapshot. Holding the reservation
// prevents scanners in other processes from committing a changed fingerprint
// between the match and the launch request.
type ContentLaunchAdmission interface {
	ContentMatches() bool
	Close() error
}

type contentLaunchAdmission struct {
	tx        *sql.Tx
	matches   bool
	closeOnce sync.Once
	closeErr  error
}

type ScanSession struct {
	mu         sync.Mutex
	tx         *sql.Tx
	root       Root
	generation int64
	report     RootReport
	finished   bool
}

func Open(path string) (*Store, error) {
	return OpenContext(context.Background(), path)
}

func OpenContext(ctx context.Context, path string) (*Store, error) {
	dsn, err := catalogDSN(path)
	if err != nil {
		return nil, fmt.Errorf("resolve catalog database path: %w", err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open catalog database: %w", err)
	}
	maximumConnections := maxCatalogOpenConnections
	if path == ":memory:" {
		maximumConnections = 1
	}
	db.SetMaxOpenConns(maximumConnections)
	db.SetMaxIdleConns(maximumConnections)

	connection, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to catalog database: %w", err)
	}
	defer connection.Close()
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure catalog database: %w", err)
		}
	}
	var journalMode string
	if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure catalog WAL mode: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		_ = db.Close()
		return nil, fmt.Errorf("configure catalog WAL mode: SQLite selected %q", journalMode)
	}
	if err := migrate(ctx, connection); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db, memoryScanLease: make(chan struct{}, 1)}
	store.memoryScanLease <- struct{}{}
	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("resolve catalog scan lease path: %w", err)
		}
		store.scanLeaseDirectory = absolute + ".scan-locks"
	}
	return store, nil
}

func catalogDSN(path string) (string, error) {
	if path == ":memory:" {
		return "file::memory:?_txlock=immediate", nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	query := u.Query()
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "on")
	query.Set("_txlock", "immediate")
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) acquireRootScanLease(ctx context.Context, root Root) (func() error, error) {
	if s.scanLeaseDirectory == "" {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.memoryScanLease:
			return func() error {
				s.memoryScanLease <- struct{}{}
				return nil
			}, nil
		}
	}
	if err := s.prepareScanLeaseDirectory(); err != nil {
		return nil, err
	}
	directory, err := os.OpenRoot(s.scanLeaseDirectory)
	if err != nil {
		return nil, fmt.Errorf("open catalog scan lease directory: %w", err)
	}
	digest := sha256.Sum256([]byte(string(root.System) + "\x00" + root.Path))
	file, err := directory.OpenFile(hex.EncodeToString(digest[:])+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	_ = directory.Close()
	if err != nil {
		return nil, fmt.Errorf("open catalog root scan lease: %w", err)
	}
	for {
		acquired, err := tryLockScanLease(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("lock catalog root scan lease: %w", err)
		}
		if acquired {
			var once sync.Once
			var releaseErr error
			return func() error {
				once.Do(func() {
					releaseErr = errors.Join(unlockScanLease(file), file.Close())
				})
				return releaseErr
			}, nil
		}
		timer := time.NewTimer(scanLeaseRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Store) prepareScanLeaseDirectory() error {
	s.scanLeaseDirectoryOnce.Do(func() {
		if err := os.Mkdir(s.scanLeaseDirectory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			s.scanLeaseDirectoryErr = fmt.Errorf("create catalog scan lease directory: %w", err)
			return
		}
		info, err := os.Lstat(s.scanLeaseDirectory)
		if err != nil {
			s.scanLeaseDirectoryErr = fmt.Errorf("inspect catalog scan lease directory: %w", err)
			return
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !scanLeaseDirectoryModeIsPrivate(info.Mode()) {
			s.scanLeaseDirectoryErr = errors.New("catalog scan lease directory must be a private real directory")
		}
	})
	return s.scanLeaseDirectoryErr
}

func (a *contentLaunchAdmission) ContentMatches() bool {
	return a.matches
}

func (a *contentLaunchAdmission) Close() error {
	a.closeOnce.Do(func() {
		a.closeErr = a.tx.Rollback()
		if errors.Is(a.closeErr, sql.ErrTxDone) {
			a.closeErr = nil
		}
	})
	return a.closeErr
}

func (s *Store) BeginRootScan(ctx context.Context, root Root) (*ScanSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin scan for root %q: %w", root.ID, err)
	}
	fail := func(err error) (*ScanSession, error) {
		_ = tx.Rollback()
		return nil, err
	}

	var storedSystem protocol.System
	var storedPath string
	var generation int64
	err = tx.QueryRowContext(ctx,
		"SELECT system, root, generation FROM libraries WHERE id = ?", root.ID,
	).Scan(&storedSystem, &storedPath, &generation)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		generation = 1
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO libraries (id, system, root, online, generation) VALUES (?, ?, ?, 0, ?)",
			root.ID, root.System, root.Path, generation,
		); err != nil {
			return fail(fmt.Errorf("insert root %q: %w", root.ID, err))
		}
	case err != nil:
		return fail(fmt.Errorf("read root %q: %w", root.ID, err))
	default:
		if storedSystem != root.System || storedPath != root.Path {
			return fail(fmt.Errorf("root %q identity changed from system %q path %q to system %q path %q", root.ID, storedSystem, storedPath, root.System, root.Path))
		}
		generation++
		if _, err := tx.ExecContext(ctx, "UPDATE libraries SET generation = ? WHERE id = ?", generation, root.ID); err != nil {
			return fail(fmt.Errorf("advance root %q generation: %w", root.ID, err))
		}
	}

	return &ScanSession{
		tx: tx, root: root, generation: generation,
		report: RootReport{RootID: root.ID, System: root.System},
	}, nil
}

type storedCandidate struct {
	libraryID, relativePath, title, reason, aliases string
	system                                          protocol.System
	kind                                            SourceKind
	state                                           SourceState
	fingerprint                                     Fingerprint
}

func (x *ScanSession) Observe(ctx context.Context, candidate Candidate) (Change, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.finished {
		return "", errors.New("catalog scan session is already finished")
	}
	if candidate.System != x.root.System {
		return "", fmt.Errorf("candidate %q system %q does not match root system %q", candidate.ID, candidate.System, x.root.System)
	}

	var previous storedCandidate
	err := x.tx.QueryRowContext(ctx, `
		SELECT library_id, system, relative_path, title, source_kind, source_state, reason,
		       source_size, modified_ns, zip_member, zip_size, zip_crc32, zip_entry_count,
		       search_aliases
		FROM games WHERE game_id = ?`, candidate.ID).Scan(
		&previous.libraryID, &previous.system, &previous.relativePath, &previous.title,
		&previous.kind, &previous.state, &previous.reason,
		&previous.fingerprint.SourceSize, &previous.fingerprint.ModifiedNS,
		&previous.fingerprint.ZIPMember, &previous.fingerprint.ZIPSize,
		&previous.fingerprint.ZIPCRC32, &previous.fingerprint.ZIPEntryCount,
		&previous.aliases,
	)

	var change Change
	dump := ParseDump(candidate.Title)
	groupKey := GroupKey(candidate.System, dump.CanonicalTitle)
	aliases := SeededSearchAliases(dump.CanonicalTitle, candidate.Title, previous.aliases)
	searchText := dumpSearchDocument(candidate.ID, candidate.Title, dump.CanonicalTitle, aliases, candidate.System)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = x.tx.ExecContext(ctx, `
			INSERT INTO games (
				game_id, library_id, system, relative_path, title, source_kind, source_state, reason,
				source_size, modified_ns, zip_member, zip_size, zip_crc32, zip_entry_count, seen_generation,
				search_text, search_aliases, canonical_title, region, revision, dump_flags, group_key, first_seen_ns
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			candidate.ID, x.root.ID, candidate.System, candidate.RelativePath, candidate.Title,
			candidate.Kind, candidate.State, candidate.Reason,
			candidate.Fingerprint.SourceSize, candidate.Fingerprint.ModifiedNS,
			candidate.Fingerprint.ZIPMember, candidate.Fingerprint.ZIPSize,
			candidate.Fingerprint.ZIPCRC32, candidate.Fingerprint.ZIPEntryCount, x.generation,
			searchText, aliases, dump.CanonicalTitle, dump.Region, dump.Revision, dump.FlagString(), groupKey,
			time.Now().UnixNano(),
		)
		if err != nil {
			return "", fmt.Errorf("insert candidate %q: %w", candidate.ID, err)
		}
		change = ChangeAdded
	case err != nil:
		return "", fmt.Errorf("read candidate %q: %w", candidate.ID, err)
	default:
		if previous.libraryID != x.root.ID || previous.system != candidate.System || previous.relativePath != candidate.RelativePath {
			return "", fmt.Errorf("candidate %q identity does not match its stored library, system, and relative path", candidate.ID)
		}
		unchanged := previous.title == candidate.Title &&
			previous.kind == candidate.Kind && previous.state == candidate.State &&
			previous.reason == candidate.Reason && previous.fingerprint == candidate.Fingerprint
		if unchanged {
			change = ChangeUnchanged
		} else {
			change = ChangeUpdated
		}
		preserveContent := previous.kind == candidate.Kind && previous.fingerprint == candidate.Fingerprint
		contentUpdate := ""
		if !preserveContent {
			contentUpdate = ", content_sha256 = NULL, content_size = NULL, content_extension = NULL"
		}
		_, err = x.tx.ExecContext(ctx, `
			UPDATE games SET title = ?, source_kind = ?, source_state = ?, reason = ?,
				source_size = ?, modified_ns = ?, zip_member = ?, zip_size = ?, zip_crc32 = ?,
				zip_entry_count = ?, seen_generation = ?, search_text = ?, search_aliases = ?,
				canonical_title = ?, region = ?, revision = ?, dump_flags = ?, group_key = ?`+contentUpdate+`
			WHERE game_id = ?`,
			candidate.Title, candidate.Kind, candidate.State, candidate.Reason,
			candidate.Fingerprint.SourceSize, candidate.Fingerprint.ModifiedNS,
			candidate.Fingerprint.ZIPMember, candidate.Fingerprint.ZIPSize,
			candidate.Fingerprint.ZIPCRC32, candidate.Fingerprint.ZIPEntryCount,
			x.generation, searchText, aliases,
			dump.CanonicalTitle, dump.Region, dump.Revision, dump.FlagString(), groupKey, candidate.ID,
		)
		if err != nil {
			return "", fmt.Errorf("update candidate %q: %w", candidate.ID, err)
		}
	}

	switch change {
	case ChangeAdded:
		x.report.Added++
	case ChangeUpdated:
		x.report.Updated++
	case ChangeUnchanged:
		x.report.Unchanged++
	}
	if candidate.State == SourceStateInvalid {
		x.report.Invalid++
	}
	return change, nil
}

func (x *ScanSession) Complete(ctx context.Context) (RootReport, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.finished {
		return RootReport{}, errors.New("catalog scan session is already finished")
	}
	if err := x.tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM games WHERE library_id = ? AND seen_generation <> ?",
		x.root.ID, x.generation,
	).Scan(&x.report.Missing); err != nil {
		return x.terminate(fmt.Errorf("count missing games for root %q: %w", x.root.ID, err))
	}
	if _, err := x.tx.ExecContext(ctx,
		"UPDATE games SET source_state = ? WHERE library_id = ? AND seen_generation <> ?",
		SourceStateMissing, x.root.ID, x.generation,
	); err != nil {
		return x.terminate(fmt.Errorf("mark missing games for root %q: %w", x.root.ID, err))
	}
	if _, err := x.tx.ExecContext(ctx,
		"UPDATE libraries SET online = 1, last_error = '' WHERE id = ?", x.root.ID,
	); err != nil {
		return x.terminate(fmt.Errorf("mark root %q online: %w", x.root.ID, err))
	}
	if err := x.tx.Commit(); err != nil {
		return x.terminate(fmt.Errorf("commit scan for root %q: %w", x.root.ID, err))
	}
	x.finished = true
	return x.report, nil
}

func (x *ScanSession) terminate(cause error) (RootReport, error) {
	x.finished = true
	if err := x.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return RootReport{}, errors.Join(cause, fmt.Errorf("roll back failed catalog scan: %w", err))
	}
	return RootReport{}, cause
}

func (x *ScanSession) Rollback() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.finished {
		return nil
	}
	err := x.tx.Rollback()
	x.finished = true
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}

const reasonLibraryRetired = "superseded_library"

const reasonLibraryRebound = "library_path_changed"

// Libraries returns stored library identities. Folder-watch uses this to
// retire SNES rows that are no longer the configured source of truth.
func (s *Store) Libraries(ctx context.Context) ([]Root, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, system, root FROM libraries ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list catalog libraries: %w", err)
	}
	defer rows.Close()
	var libraries []Root
	for rows.Next() {
		var root Root
		if err := rows.Scan(&root.ID, &root.System, &root.Path); err != nil {
			return nil, fmt.Errorf("read catalog library: %w", err)
		}
		libraries = append(libraries, root)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog libraries: %w", err)
	}
	return libraries, nil
}

// RetireLibrary drops games for libraryID and marks the library offline so
// the UI cannot select a dead id after watch_root replaces that SoT.
// Identity is by id only; a changed path cannot use MarkRootOffline.
func (s *Store) RetireLibrary(ctx context.Context, libraryID string) error {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("retire catalog library: empty id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retire library %q: %w", libraryID, err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM libraries WHERE id = ?", libraryID).Scan(&exists)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read library %q: %w", libraryID, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM games WHERE library_id = ?", libraryID); err != nil {
		return fmt.Errorf("drop games for retired library %q: %w", libraryID, err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE libraries SET online = 0, last_error = ?, generation = generation + 1 WHERE id = ?", reasonLibraryRetired, libraryID); err != nil {
		return fmt.Errorf("mark library %q retired: %w", libraryID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit retire library %q: %w", libraryID, err)
	}
	return nil
}

// ReleaseLibraryRoot removes stale identities that conflict with root's path
// or reuse root's id for a different system. Games are dropped because their
// ids are bound to the retired library identity; the following scan rebuilds
// them under root.ID. A same-system id at another path remains for
// RebindLibrary to move.
func (s *Store) ReleaseLibraryRoot(ctx context.Context, root Root) error {
	root.ID = strings.TrimSpace(root.ID)
	if root.ID == "" {
		return fmt.Errorf("release catalog library root: empty id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release library root %q: %w", root.ID, err)
	}
	defer tx.Rollback()

	dropLibrary := func(libraryID string) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM games WHERE library_id = ?", libraryID); err != nil {
			return fmt.Errorf("drop games for released library %q: %w", libraryID, err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM libraries WHERE id = ?", libraryID); err != nil {
			return fmt.Errorf("delete released library %q: %w", libraryID, err)
		}
		return nil
	}
	changed := false
	var retiredID string
	var retiredSystem protocol.System
	err = tx.QueryRowContext(ctx,
		"SELECT id, system FROM libraries WHERE root = ?", root.Path,
	).Scan(&retiredID, &retiredSystem)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("read library owning replacement root %q: %w", root.Path, err)
	case retiredID == root.ID && retiredSystem == root.System:
		return nil
	default:
		if err := dropLibrary(retiredID); err != nil {
			return err
		}
		changed = true
	}

	var reusedSystem protocol.System
	err = tx.QueryRowContext(ctx, "SELECT system FROM libraries WHERE id = ?", root.ID).Scan(&reusedSystem)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("read reused library identity %q: %w", root.ID, err)
	case reusedSystem != root.System:
		if err := dropLibrary(root.ID); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release library root %q: %w", root.ID, err)
	}
	return nil
}

// RebindLibrary moves an existing library id to a new root path. Existing
// catalog rows are removed so callers cannot launch content collected from the
// old root; a following scan recreates matching game ids from the new root.
func (s *Store) RebindLibrary(ctx context.Context, root Root) error {
	root.ID = strings.TrimSpace(root.ID)
	if root.ID == "" {
		return fmt.Errorf("rebind catalog library: empty id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rebind library %q: %w", root.ID, err)
	}
	defer tx.Rollback()

	var storedSystem protocol.System
	var storedPath string
	err = tx.QueryRowContext(ctx, "SELECT system, root FROM libraries WHERE id = ?", root.ID).Scan(&storedSystem, &storedPath)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read library %q: %w", root.ID, err)
	case storedSystem != root.System:
		return fmt.Errorf("rebind library %q: system changed from %q to %q", root.ID, storedSystem, root.System)
	case storedPath == root.Path:
		return nil
	}
	var conflictingID string
	var conflictingSystem protocol.System
	err = tx.QueryRowContext(ctx,
		"SELECT id, system FROM libraries WHERE root = ? AND id <> ?", root.Path, root.ID,
	).Scan(&conflictingID, &conflictingSystem)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("read destination root for library %q: %w", root.ID, err)
	case conflictingSystem != root.System:
		return fmt.Errorf("rebind library %q: destination root belongs to system %q", root.ID, conflictingSystem)
	default:
		if _, err := tx.ExecContext(ctx, "DELETE FROM games WHERE library_id = ?", conflictingID); err != nil {
			return fmt.Errorf("drop games for superseded library %q: %w", conflictingID, err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM libraries WHERE id = ?", conflictingID); err != nil {
			return fmt.Errorf("drop superseded library %q: %w", conflictingID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM games WHERE library_id = ?", root.ID); err != nil {
		return fmt.Errorf("drop games for rebound library %q: %w", root.ID, err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE libraries SET root = ?, online = 0, last_error = ?, generation = generation + 1 WHERE id = ?",
		root.Path, reasonLibraryRebound, root.ID,
	); err != nil {
		return fmt.Errorf("rebind library %q: %w", root.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rebind library %q: %w", root.ID, err)
	}
	return nil
}

func (s *Store) MarkRootOffline(ctx context.Context, root Root, reason string) (RootReport, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RootReport{}, fmt.Errorf("begin offline update for root %q: %w", root.ID, err)
	}
	defer tx.Rollback()

	var storedSystem protocol.System
	var storedPath string
	err = tx.QueryRowContext(ctx, "SELECT system, root FROM libraries WHERE id = ?", root.ID).Scan(&storedSystem, &storedPath)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO libraries (id, system, root, online, last_error) VALUES (?, ?, ?, 0, ?)",
			root.ID, root.System, root.Path, reason,
		); err != nil {
			return RootReport{}, fmt.Errorf("insert offline root %q: %w", root.ID, err)
		}
	case err != nil:
		return RootReport{}, fmt.Errorf("read root %q: %w", root.ID, err)
	default:
		if storedSystem != root.System || storedPath != root.Path {
			return RootReport{}, fmt.Errorf("root %q identity changed from system %q path %q to system %q path %q", root.ID, storedSystem, storedPath, root.System, root.Path)
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE libraries SET online = 0, last_error = ? WHERE id = ?", reason, root.ID,
		); err != nil {
			return RootReport{}, fmt.Errorf("mark root %q offline: %w", root.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return RootReport{}, fmt.Errorf("commit offline update for root %q: %w", root.ID, err)
	}
	return RootReport{RootID: root.ID, System: root.System, Offline: true, Reason: reason}, nil
}

const selectGames = `
	SELECT g.game_id, g.title, g.library_id, g.relative_path, g.reason, g.system,
	       g.source_kind, g.source_state, l.online,
	       g.source_size, g.modified_ns, g.zip_member, g.zip_size, g.zip_crc32, g.zip_entry_count,
	       g.content_sha256, g.content_size, g.content_extension,
	       g.canonical_title, g.region, g.revision, g.dump_flags, g.group_key,
	       g.first_seen_ns, g.genre, g.year, g.search_aliases, 1
	FROM games AS g JOIN libraries AS l ON l.id = g.library_id`

const selectGameColumns = `g.game_id, g.title, g.library_id, g.relative_path, g.reason, g.system,
	       g.source_kind, g.source_state, l.online,
	       g.source_size, g.modified_ns, g.zip_member, g.zip_size, g.zip_crc32, g.zip_entry_count,
	       g.content_sha256, g.content_size, g.content_extension,
	       g.canonical_title, g.region, g.revision, g.dump_flags, g.group_key,
	       g.first_seen_ns, g.genre, g.year, g.search_aliases`

func (s *Store) Games(ctx context.Context) ([]Game, error) {
	return s.queryGames(ctx, selectGames+" ORDER BY lower(g.title), g.game_id")
}

func (s *Store) Search(ctx context.Context, query string) ([]Game, error) {
	foldedQuery := foldSearchText(query)
	if foldedQuery == "" {
		return s.Games(ctx)
	}
	return s.queryGames(ctx, selectGames+` WHERE g.search_text LIKE '%' || ? || '%' ESCAPE '\' ORDER BY lower(g.title), g.game_id`, escapeLIKE(foldedQuery))
}

func foldSearchText(value string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(value)))
}

func (s *Store) Game(ctx context.Context, id string) (Game, error) {
	game, err := scanGame(s.db.QueryRowContext(ctx, selectGames+" WHERE g.game_id = ?", id))
	if err != nil {
		return Game{}, fmt.Errorf("read catalog game %q: %w", id, err)
	}
	return game, nil
}

func (s *Store) Platforms(ctx context.Context) ([]PlatformInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.system,
		       COUNT(g.game_id),
		       MAX(l.online)
		FROM libraries AS l
		LEFT JOIN games AS g ON g.library_id = l.id
		GROUP BY l.system
		ORDER BY lower(l.system)`)
	if err != nil {
		return nil, fmt.Errorf("query catalog platforms: %w", err)
	}
	defer rows.Close()
	platforms := make([]PlatformInfo, 0)
	for rows.Next() {
		var info PlatformInfo
		var online int
		if err := rows.Scan(&info.ID, &info.GameCount, &online); err != nil {
			return nil, fmt.Errorf("scan catalog platform: %w", err)
		}
		info.Label = PlatformLabel(info.ID)
		info.Online = online != 0
		info.Launchable = Launchable(info.ID)
		platforms = append(platforms, info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog platforms: %w", err)
	}
	return platforms, nil
}

func (s *Store) QueryGames(ctx context.Context, query Query) (Page, error) {
	normalized, err := NormalizeQuery(query)
	if err != nil {
		return Page{}, err
	}
	if normalized.Restrict && len(normalized.RestrictIDs) == 0 {
		return Page{}, nil
	}
	args := make([]any, 0, 24)
	filters, filterArgs := gameFilterSQL(normalized, "g", "l")
	args = append(args, filterArgs...)
	rankSQL, rankArgs := regionCaseSQL(normalized.PreferredRegions, "g.region")
	from := ` FROM games AS g JOIN libraries AS l ON l.id = g.library_id` + filters
	var builder strings.Builder
	if normalized.Grouped {
		builder.WriteString(`SELECT `)
		builder.WriteString(selectGameColumns)
		builder.WriteString(`, ranked.variant_count FROM (SELECT `)
		builder.WriteString(selectGameColumns)
		builder.WriteString(`, ROW_NUMBER() OVER (PARTITION BY g.group_key ORDER BY `)
		builder.WriteString(rankSQL)
		builder.WriteString(`, `)
		builder.WriteString(dumpPenaltySQL("g.dump_flags"))
		builder.WriteString(`, g.revision DESC, g.game_id) AS rn, COUNT(*) OVER (PARTITION BY g.group_key) AS variant_count`)
		builder.WriteString(from)
		builder.WriteString(`) AS ranked JOIN games AS g ON g.game_id = ranked.game_id JOIN libraries AS l ON l.id = g.library_id WHERE ranked.rn = 1`)
		args = append(append([]any{}, rankArgs...), args...)
		if normalized.Cursor != "" {
			cursorSQL, cursorArgs, err := groupedCursorSQL(normalized)
			if err != nil {
				return Page{}, err
			}
			builder.WriteString(cursorSQL)
			args = append(args, cursorArgs...)
		}
		builder.WriteString(groupedOrderSQL(normalized.Sort))
	} else {
		builder.WriteString(`SELECT `)
		builder.WriteString(selectGameColumns)
		builder.WriteString(`, 1`)
		builder.WriteString(from)
		if normalized.Cursor != "" {
			cursorSQL, cursorArgs, err := fileCursorSQL(normalized)
			if err != nil {
				return Page{}, err
			}
			builder.WriteString(cursorSQL)
			args = append(args, cursorArgs...)
		}
		builder.WriteString(fileOrderSQL(normalized.Sort))
	}
	builder.WriteString(" LIMIT ?")
	args = append(args, normalized.Limit+1)
	games, err := s.queryGames(ctx, builder.String(), args...)
	if err != nil {
		return Page{}, err
	}
	page := Page{Games: games}
	if len(page.Games) > normalized.Limit {
		page.Games = page.Games[:normalized.Limit]
		if normalized.Grouped {
			page.NextCursor = gameCursor(page.Games[len(page.Games)-1], normalized.Sort, true)
		} else {
			page.NextCursor = gameCursor(page.Games[len(page.Games)-1], normalized.Sort, false)
		}
	}
	return page, nil
}

func gameFilterSQL(query Query, gameAlias, libraryAlias string) (string, []any) {
	var builder strings.Builder
	builder.WriteString(" WHERE 1 = 1")
	args := make([]any, 0, 16)
	if query.Platform != "" {
		builder.WriteString(" AND " + gameAlias + ".system = ?")
		args = append(args, query.Platform)
	}
	if query.Region == "other" {
		builder.WriteString(" AND (" + gameAlias + ".region = '' OR " + gameAlias + ".region = 'other')")
	} else if query.Region != "" {
		builder.WriteString(" AND " + gameAlias + ".region = ?")
		args = append(args, query.Region)
	}
	if query.Genre != "" {
		builder.WriteString(" AND " + gameAlias + ".genre = ?")
		args = append(args, query.Genre)
	}
	if query.Year != "" {
		builder.WriteString(" AND " + gameAlias + ".year = ?")
		args = append(args, query.Year)
	}
	if query.HidePrerelease {
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,beta,%'")
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,proto,%'")
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,sample,%'")
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,demo,%'")
	}
	if query.HideHacks {
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,hack,%'")
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,unl,%'")
	}
	switch query.Availability {
	case AvailabilityReady:
		builder.WriteString(" AND " + gameAlias + ".source_state = ? AND " + libraryAlias + ".online = 1")
		args = append(args, SourceStateAvailable)
	case AvailabilityOffline:
		builder.WriteString(" AND ((" + gameAlias + ".source_state = ?) OR (" + libraryAlias + ".online = 0 AND " + gameAlias + ".source_state = ?))")
		args = append(args, SourceStateMissing, SourceStateAvailable)
	}
	if query.Restrict {
		clause, listArgs := idListSQL(gameAlias+".game_id", query.RestrictIDs, false)
		builder.WriteString(clause)
		args = append(args, listArgs...)
	}
	if len(query.ExcludeIDs) > 0 {
		clause, listArgs := idListSQL(gameAlias+".game_id", query.ExcludeIDs, true)
		builder.WriteString(clause)
		args = append(args, listArgs...)
	}
	if folded := foldSearchText(query.Text); folded != "" {
		like := escapeLIKE(folded)
		seedClause := ""
		if folded == foldSearchText(SeededActRaiserAlias) {
			seedClause = " OR lower(" + gameAlias + ".canonical_title) = 'actraiser'"
		}
		if match := ftsMatchQuery(folded); match != "" {
			builder.WriteString(" AND (" + gameAlias + ".rowid IN (SELECT rowid FROM games_fts WHERE games_fts MATCH ?) OR " + gameAlias + ".search_text LIKE '%' || ? || '%' ESCAPE '\\'" + seedClause + ")")
			args = append(args, match, like)
		} else {
			builder.WriteString(" AND (" + gameAlias + ".search_text LIKE '%' || ? || '%' ESCAPE '\\'" + seedClause + ")")
			args = append(args, like)
		}
	}
	return builder.String(), args
}

func fileOrderSQL(sort Sort) string {
	switch sort {
	case SortPlatform:
		return " ORDER BY g.system, lower(g.title), g.game_id"
	case SortYear:
		return " ORDER BY CASE WHEN g.year = '' THEN 1 ELSE 0 END, g.year DESC, lower(g.title), g.game_id"
	case SortAdded:
		return " ORDER BY g.first_seen_ns DESC, g.game_id"
	default:
		return " ORDER BY lower(g.title), g.game_id"
	}
}

func groupedOrderSQL(sort Sort) string {
	switch sort {
	case SortPlatform:
		return " ORDER BY g.system, lower(g.canonical_title), g.group_key"
	case SortYear:
		return " ORDER BY CASE WHEN g.year = '' THEN 1 ELSE 0 END, g.year DESC, lower(g.canonical_title), g.group_key"
	case SortAdded:
		return " ORDER BY g.first_seen_ns DESC, g.group_key"
	default:
		return " ORDER BY lower(g.canonical_title), g.group_key"
	}
}

func fileCursorSQL(query Query) (string, []any, error) {
	key, err := decodeCursor(query.Cursor)
	if err != nil {
		return "", nil, err
	}
	switch query.Sort {
	case SortPlatform:
		return " AND (g.system > ? OR (g.system = ? AND (lower(g.title) > ? OR (lower(g.title) = ? AND g.game_id > ?))))",
			[]any{key.platform, key.platform, key.title, key.title, key.id}, nil
	case SortYear:
		return " AND ((CASE WHEN g.year = '' THEN 1 ELSE 0 END) > (CASE WHEN ? = '' THEN 1 ELSE 0 END) OR ((CASE WHEN g.year = '' THEN 1 ELSE 0 END) = (CASE WHEN ? = '' THEN 1 ELSE 0 END) AND (g.year < ? OR (g.year = ? AND (lower(g.title) > ? OR (lower(g.title) = ? AND g.game_id > ?))))))",
			[]any{key.extra, key.extra, key.extra, key.extra, key.title, key.title, key.id}, nil
	case SortAdded:
		return " AND (g.first_seen_ns < ? OR (g.first_seen_ns = ? AND g.game_id > ?))",
			[]any{key.extra, key.extra, key.id}, nil
	default:
		return " AND (lower(g.title) > ? OR (lower(g.title) = ? AND g.game_id > ?))",
			[]any{key.title, key.title, key.id}, nil
	}
}

func groupedCursorSQL(query Query) (string, []any, error) {
	key, err := decodeCursor(query.Cursor)
	if err != nil {
		return "", nil, err
	}
	switch query.Sort {
	case SortPlatform:
		return " AND (g.system > ? OR (g.system = ? AND (lower(g.canonical_title) > ? OR (lower(g.canonical_title) = ? AND g.group_key > ?))))",
			[]any{key.platform, key.platform, key.title, key.title, key.id}, nil
	case SortYear:
		return " AND ((CASE WHEN g.year = '' THEN 1 ELSE 0 END) > (CASE WHEN ? = '' THEN 1 ELSE 0 END) OR ((CASE WHEN g.year = '' THEN 1 ELSE 0 END) = (CASE WHEN ? = '' THEN 1 ELSE 0 END) AND (g.year < ? OR (g.year = ? AND (lower(g.canonical_title) > ? OR (lower(g.canonical_title) = ? AND g.group_key > ?))))))",
			[]any{key.extra, key.extra, key.extra, key.extra, key.title, key.title, key.id}, nil
	case SortAdded:
		return " AND (g.first_seen_ns < ? OR (g.first_seen_ns = ? AND g.group_key > ?))",
			[]any{key.extra, key.extra, key.id}, nil
	default:
		return " AND (lower(g.canonical_title) > ? OR (lower(g.canonical_title) = ? AND g.group_key > ?))",
			[]any{key.title, key.title, key.id}, nil
	}
}

func (s *Store) GamesByIDs(ctx context.Context, ids []string) ([]Game, error) {
	if len(ids) == 0 {
		return []Game{}, nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	games, err := s.queryGames(ctx, selectGames+" WHERE g.game_id IN (SELECT value FROM json_each(?))", string(raw))
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Game, len(games))
	for _, game := range games {
		byID[game.ID] = game
	}
	ordered := make([]Game, 0, len(ids))
	for _, id := range ids {
		if game, ok := byID[id]; ok {
			ordered = append(ordered, game)
		}
	}
	return ordered, nil
}

func idListSQL(column string, ids []string, exclude bool) (string, []any) {
	if ids == nil {
		ids = []string{}
	}
	raw, _ := json.Marshal(ids)
	op := " IN "
	if exclude {
		op = " NOT IN "
	}
	return " AND " + column + op + "(SELECT value FROM json_each(?))", []any{string(raw)}
}

func (s *Store) queryGames(ctx context.Context, query string, args ...any) ([]Game, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query catalog games: %w", err)
	}
	defer rows.Close()
	games := make([]Game, 0)
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog game: %w", err)
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog games: %w", err)
	}
	return games, nil
}

func (s *Store) GamesInGroup(ctx context.Context, groupKey string, limit int) ([]Game, error) {
	groupKey = strings.TrimSpace(groupKey)
	if groupKey == "" {
		return []Game{}, nil
	}
	query := `SELECT ` + selectGameColumns + `, 1` + `
		FROM games AS g JOIN libraries AS l ON l.id = g.library_id
		WHERE g.group_key = ?
		ORDER BY g.region, g.revision DESC, g.game_id`
	if limit == UnboundedVariantLimit {
		return s.queryGames(ctx, query, groupKey)
	}
	if limit <= 0 || limit > MaxVariantLimit {
		limit = MaxVariantLimit
	}
	return s.queryGames(ctx, query+` LIMIT ?`, groupKey, limit)
}

func (s *Store) SetFacets(ctx context.Context, id, genre, year, aliases string) error {
	if err := protocol.ValidateGameID(id); err != nil {
		return err
	}
	game, err := s.Game(ctx, id)
	if err != nil {
		return err
	}
	genre = strings.TrimSpace(genre)
	year = strings.TrimSpace(year)
	aliases = strings.TrimSpace(aliases)
	if genre == "" {
		genre = game.Genre
	}
	if year == "" {
		year = game.Year
	}
	if aliases == "" {
		aliases = game.SearchAliases
	}
	aliases = SeededSearchAliases(game.CanonicalTitle, game.Title, aliases)
	search := dumpSearchDocument(game.ID, game.Title, game.CanonicalTitle, aliases, game.System)
	_, err = s.db.ExecContext(ctx, `
		UPDATE games SET genre = ?, year = ?, search_aliases = ?, search_text = ?
		WHERE game_id = ?`, genre, year, aliases, search, id)
	if err != nil {
		return fmt.Errorf("update catalog facets for %q: %w", id, err)
	}
	return nil
}

func (s *Store) Facets(ctx context.Context) (FacetValues, error) {
	genres, err := s.distinctColumn(ctx, `SELECT DISTINCT genre FROM games WHERE genre <> '' ORDER BY lower(genre) LIMIT 200`)
	if err != nil {
		return FacetValues{}, err
	}
	years, err := s.distinctColumn(ctx, `SELECT DISTINCT year FROM games WHERE year <> '' ORDER BY year DESC LIMIT 64`)
	if err != nil {
		return FacetValues{}, err
	}
	return FacetValues{Genres: genres, Years: years}, nil
}

func (s *Store) distinctColumn(ctx context.Context, query string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query catalog facets: %w", err)
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanGame(row rowScanner) (Game, error) {
	var game Game
	var online int
	var contentSHA256, contentExtension sql.NullString
	var contentSize sql.NullInt64
	if err := row.Scan(
		&game.ID, &game.Title, &game.LibraryID, &game.RelativePath, &game.Reason, &game.System,
		&game.Kind, &game.State, &online,
		&game.Fingerprint.SourceSize, &game.Fingerprint.ModifiedNS, &game.Fingerprint.ZIPMember,
		&game.Fingerprint.ZIPSize, &game.Fingerprint.ZIPCRC32, &game.Fingerprint.ZIPEntryCount,
		&contentSHA256, &contentSize, &contentExtension,
		&game.CanonicalTitle, &game.Region, &game.Revision, &game.DumpFlags, &game.GroupKey,
		&game.FirstSeenNS, &game.Genre, &game.Year, &game.SearchAliases, &game.VariantCount,
	); err != nil {
		return Game{}, err
	}
	if game.VariantCount <= 0 {
		game.VariantCount = 1
	}
	game.RootOnline = online != 0
	validContentFields := 0
	if contentSHA256.Valid {
		validContentFields++
	}
	if contentSize.Valid {
		validContentFields++
	}
	if contentExtension.Valid {
		validContentFields++
	}
	switch validContentFields {
	case 0:
	case 3:
		game.Content = &Content{SHA256: contentSHA256.String, Size: contentSize.Int64, Extension: contentExtension.String}
	default:
		return Game{}, fmt.Errorf("game %q has invalid partial content tuple", game.ID)
	}
	return game, nil
}

// GameMatchesRoot reports whether the catalog row represented by game still
// belongs to the configured root without exposing that root through Game.
func (s *Store) GameMatchesRoot(ctx context.Context, game Game, root Root) (bool, error) {
	var matches int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM games AS g JOIN libraries AS l ON l.id = g.library_id
		  WHERE g.game_id = ? AND g.library_id = ? AND g.system = ?
		    AND l.id = ? AND l.system = ? AND l.root = ?
		)`,
		game.ID, game.LibraryID, game.System,
		root.ID, root.System, root.Path,
	).Scan(&matches)
	if err != nil {
		return false, fmt.Errorf("match catalog game %q to root: %w", game.ID, err)
	}
	return matches != 0, nil
}

// ContentMatches reports whether the complete source and content snapshot is
// still current for the configured root. Unlike CompareAndSetContent, it never
// writes a remembered digest back into the catalog.
func (s *Store) ContentMatches(ctx context.Context, game Game, root Root, content Content) (bool, error) {
	return contentMatches(ctx, s.db, game, root, content)
}

type contentMatchQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func contentMatches(ctx context.Context, querier contentMatchQuerier, game Game, root Root, content Content) (bool, error) {
	var matches int
	err := querier.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM games
		  WHERE game_id = ? AND library_id = ? AND system = ? AND relative_path = ? AND source_kind = ?
		    AND source_size = ? AND modified_ns = ? AND zip_member = ?
		    AND zip_size = ? AND zip_crc32 = ? AND zip_entry_count = ?
		    AND content_sha256 = ? AND content_size = ? AND content_extension = ?
		    AND EXISTS (
		      SELECT 1 FROM libraries AS l
		      WHERE l.id = games.library_id AND l.id = ? AND l.system = ? AND l.root = ?
		    )
		)`,
		game.ID, game.LibraryID, game.System, game.RelativePath, game.Kind,
		game.Fingerprint.SourceSize, game.Fingerprint.ModifiedNS, game.Fingerprint.ZIPMember,
		game.Fingerprint.ZIPSize, game.Fingerprint.ZIPCRC32, game.Fingerprint.ZIPEntryCount,
		content.SHA256, content.Size, content.Extension,
		root.ID, root.System, root.Path,
	).Scan(&matches)
	if err != nil {
		return false, fmt.Errorf("match catalog content for game %q: %w", game.ID, err)
	}
	return matches != 0, nil
}

// BeginContentLaunchAdmission atomically checks the content snapshot while
// reserving SQLite's single writer slot. The caller must close the returned
// admission after the launch request finishes so external catalog writers can
// proceed.
func (s *Store) BeginContentLaunchAdmission(ctx context.Context, game Game, root Root, content Content) (ContentLaunchAdmission, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin catalog content launch admission: %w", err)
	}
	fail := func(cause error) (ContentLaunchAdmission, error) {
		return nil, errors.Join(cause, tx.Rollback())
	}
	matches, err := contentMatches(ctx, tx, game, root, content)
	if err != nil {
		return fail(err)
	}
	return &contentLaunchAdmission{tx: tx, matches: matches}, nil
}

func (s *Store) UpdateContent(ctx context.Context, id string, fingerprint Fingerprint, content Content) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE games SET content_sha256 = ?, content_size = ?, content_extension = ?
		WHERE game_id = ? AND source_size = ? AND modified_ns = ? AND zip_member = ?
		  AND zip_size = ? AND zip_crc32 = ? AND zip_entry_count = ?`,
		content.SHA256, content.Size, content.Extension, id,
		fingerprint.SourceSize, fingerprint.ModifiedNS, fingerprint.ZIPMember,
		fingerprint.ZIPSize, fingerprint.ZIPCRC32, fingerprint.ZIPEntryCount,
	)
	if err != nil {
		return false, fmt.Errorf("update content for game %q: %w", id, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count content updates for game %q: %w", id, err)
	}
	return rows == 1, nil
}

// CompareAndSetContent records prepared content only while the complete source
// identity used by the preparer is still the catalog's current identity.
func (s *Store) CompareAndSetContent(ctx context.Context, game Game, root Root, content Content) (bool, error) {
	var expectedSHA256, expectedSize, expectedExtension any
	if game.Content != nil {
		expectedSHA256 = game.Content.SHA256
		expectedSize = game.Content.Size
		expectedExtension = game.Content.Extension
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE games SET content_sha256 = ?, content_size = ?, content_extension = ?
		WHERE game_id = ? AND library_id = ? AND system = ? AND relative_path = ? AND source_kind = ?
		  AND source_size = ? AND modified_ns = ? AND zip_member = ?
		  AND zip_size = ? AND zip_crc32 = ? AND zip_entry_count = ?
		  AND ((? IS NULL AND content_sha256 IS NULL AND content_size IS NULL AND content_extension IS NULL)
		       OR (content_sha256 = ? AND content_size = ? AND content_extension = ?))
		  AND EXISTS (
		    SELECT 1 FROM libraries AS l
		    WHERE l.id = games.library_id AND l.id = ? AND l.system = ? AND l.root = ?
		  )`,
		content.SHA256, content.Size, content.Extension,
		game.ID, game.LibraryID, game.System, game.RelativePath, game.Kind,
		game.Fingerprint.SourceSize, game.Fingerprint.ModifiedNS, game.Fingerprint.ZIPMember,
		game.Fingerprint.ZIPSize, game.Fingerprint.ZIPCRC32, game.Fingerprint.ZIPEntryCount,
		expectedSHA256, expectedSHA256, expectedSize, expectedExtension,
		root.ID, root.System, root.Path,
	)
	if err != nil {
		return false, fmt.Errorf("compare and set content for game %q: %w", game.ID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count content compare-and-set updates for game %q: %w", game.ID, err)
	}
	return rows == 1, nil
}
