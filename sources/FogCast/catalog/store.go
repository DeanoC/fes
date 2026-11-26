package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/protocol"
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

type sourceIndex struct {
	Kind        SourceKind
	State       SourceState
	Reason      string
	Fingerprint Fingerprint
}

func (s *Store) sourceIndexForRoot(ctx context.Context, rootID string) (map[string]sourceIndex, error) {
	if s == nil {
		return nil, errors.New("catalog store is unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT relative_path, source_kind, source_state, reason,
		       source_size, modified_ns, zip_member, zip_size, zip_crc32, zip_entry_count
		FROM games WHERE library_id = ?`, rootID)
	if err != nil {
		return nil, fmt.Errorf("read source index for root %q: %w", rootID, err)
	}
	defer rows.Close()
	index := make(map[string]sourceIndex)
	for rows.Next() {
		var relativePath string
		var record sourceIndex
		if err := rows.Scan(
			&relativePath, &record.Kind, &record.State, &record.Reason,
			&record.Fingerprint.SourceSize, &record.Fingerprint.ModifiedNS,
			&record.Fingerprint.ZIPMember, &record.Fingerprint.ZIPSize,
			&record.Fingerprint.ZIPCRC32, &record.Fingerprint.ZIPEntryCount,
		); err != nil {
			return nil, fmt.Errorf("read source index for root %q: %w", rootID, err)
		}
		index[relativePath] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read source index for root %q: %w", rootID, err)
	}
	return index, nil
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
	if err := rejectCorePackageRoot(root); err != nil {
		return nil, err
	}
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
			_, err = x.tx.ExecContext(ctx, `UPDATE games SET seen_generation = ? WHERE game_id = ?`, x.generation, candidate.ID)
			if err != nil {
				return "", fmt.Errorf("touch candidate %q: %w", candidate.ID, err)
			}
			change = ChangeUnchanged
			break
		}
		change = ChangeUpdated
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
