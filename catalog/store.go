package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

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
	db *sql.DB
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
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open catalog database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx := context.Background()
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
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
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
	libraryID, relativePath, title, reason string
	system                                 protocol.System
	kind                                   SourceKind
	state                                  SourceState
	fingerprint                            Fingerprint
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
		       source_size, modified_ns, zip_member, zip_size, zip_crc32, zip_entry_count
		FROM games WHERE game_id = ?`, candidate.ID).Scan(
		&previous.libraryID, &previous.system, &previous.relativePath, &previous.title,
		&previous.kind, &previous.state, &previous.reason,
		&previous.fingerprint.SourceSize, &previous.fingerprint.ModifiedNS,
		&previous.fingerprint.ZIPMember, &previous.fingerprint.ZIPSize,
		&previous.fingerprint.ZIPCRC32, &previous.fingerprint.ZIPEntryCount,
	)

	var change Change
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = x.tx.ExecContext(ctx, `
			INSERT INTO games (
				game_id, library_id, system, relative_path, title, source_kind, source_state, reason,
				source_size, modified_ns, zip_member, zip_size, zip_crc32, zip_entry_count, seen_generation
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			candidate.ID, x.root.ID, candidate.System, candidate.RelativePath, candidate.Title,
			candidate.Kind, candidate.State, candidate.Reason,
			candidate.Fingerprint.SourceSize, candidate.Fingerprint.ModifiedNS,
			candidate.Fingerprint.ZIPMember, candidate.Fingerprint.ZIPSize,
			candidate.Fingerprint.ZIPCRC32, candidate.Fingerprint.ZIPEntryCount, x.generation,
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
				zip_entry_count = ?, seen_generation = ?`+contentUpdate+`
			WHERE game_id = ?`,
			candidate.Title, candidate.Kind, candidate.State, candidate.Reason,
			candidate.Fingerprint.SourceSize, candidate.Fingerprint.ModifiedNS,
			candidate.Fingerprint.ZIPMember, candidate.Fingerprint.ZIPSize,
			candidate.Fingerprint.ZIPCRC32, candidate.Fingerprint.ZIPEntryCount,
			x.generation, candidate.ID,
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
	       g.content_sha256, g.content_size, g.content_extension
	FROM games AS g JOIN libraries AS l ON l.id = g.library_id`

func (s *Store) Games(ctx context.Context) ([]Game, error) {
	return s.queryGames(ctx, selectGames+" ORDER BY lower(g.title), g.game_id")
}

func (s *Store) Search(ctx context.Context, query string) ([]Game, error) {
	games, err := s.queryGames(ctx, selectGames+" ORDER BY lower(g.title), g.game_id")
	if err != nil {
		return nil, err
	}
	foldedQuery := foldSearchText(query)
	matches := make([]Game, 0)
	for _, game := range games {
		if strings.Contains(foldSearchText(game.Title), foldedQuery) ||
			strings.Contains(foldSearchText(game.ID), foldedQuery) ||
			strings.Contains(foldSearchText(string(game.System)), foldedQuery) {
			matches = append(matches, game)
		}
	}
	return matches, nil
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
	); err != nil {
		return Game{}, err
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
