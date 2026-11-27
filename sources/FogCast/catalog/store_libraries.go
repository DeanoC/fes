package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

const reasonLibraryRetired = "superseded_library"

const reasonLibraryRebound = "library_path_changed"

// Libraries returns stored library identities. Folder-watch uses this to
// retire SNES rows that are no longer the configured source of truth.
func (s *Store) Libraries(ctx context.Context) ([]Root, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, system, root FROM libraries WHERE root NOT IN (?, ?) ORDER BY id`, builtinRoot, corePackageRoot)
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
	if libraryID == corePackageLibraryID {
		return fmt.Errorf("%w: reserved core package collection cannot be retired", ErrInvalidCoreEntry)
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
	if err := rejectCorePackageRoot(root); err != nil {
		return err
	}
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
	if err := rejectCorePackageRoot(root); err != nil {
		return err
	}
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
	if err := rejectCorePackageRoot(root); err != nil {
		return RootReport{}, err
	}
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

// GameMatchesRoot reports whether the catalog row represented by game still
// belongs to the configured root without exposing that root through Game.
func (s *Store) GameMatchesRoot(ctx context.Context, game Game, root Root) (bool, error) {
	var matches int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM games AS g JOIN libraries AS l ON l.id = g.library_id
		  WHERE g.game_id = ? AND g.library_id = ? AND g.system = ?
		    AND (g.source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = g.game_id))
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
		    AND (source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = games.game_id))
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
		  AND (source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = games.game_id))
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
		  AND (source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = games.game_id))
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
