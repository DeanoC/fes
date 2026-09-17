package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	corePackageLibraryID = "core-packages"
	corePackageRoot      = "logical:core-packages"
	maxCoreEntryTitle    = 256
)

var (
	ErrCoreEntryNotFound     = errors.New("catalog core entry not found")
	ErrCoreEntryConflict     = errors.New("catalog core entry selection conflict")
	ErrCoreEntryCoreMismatch = errors.New("catalog core entry core mismatch")
	ErrInvalidCoreEntry      = errors.New("catalog core entry is invalid")
	coreEntryIDRE            = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
)

// CoreEntry binds one stable catalog game identity and core ID to an explicit
// installed package selection.
type CoreEntry struct {
	GameID    string `json:"game_id"`
	Title     string `json:"title"`
	CoreID    string `json:"core_id"`
	PackageID string `json:"package_id"`
	MediaID   string `json:"media_id,omitempty"`
	MediaRole string `json:"media_role,omitempty"`
}

// CreateCoreEntry creates a durable title for coreID without media.
func (s *Store) CreateCoreEntry(ctx context.Context, title, coreID, packageID string) (CoreEntry, error) {
	return s.CreateCoreMediaEntry(ctx, title, coreID, packageID, "", "")
}

// CreateCoreMediaEntry atomically creates a title with an explicit media selection.
func (s *Store) CreateCoreMediaEntry(ctx context.Context, title, coreID, packageID, mediaRole, mediaID string) (CoreEntry, error) {
	title = strings.TrimSpace(title)
	if err := validateCoreEntry(title, coreID, packageID); err != nil {
		return CoreEntry{}, err
	}
	if err := validateCoreMediaSelection(mediaRole, mediaID); err != nil {
		return CoreEntry{}, err
	}
	// Include the exact trimmed title in the logical identity: the display slug
	// alone loses punctuation, case, non-ASCII text, and long suffixes. Existing
	// rows retain their stored IDs; the core/title query below finds duplicates
	// created by either identity scheme.
	gameID := GameID(CorePlatform, corePackageLibraryID, coreID+"\x00"+title, title)
	entry := CoreEntry{GameID: gameID, Title: title, CoreID: coreID, PackageID: packageID, MediaRole: mediaRole, MediaID: mediaID}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("begin core entry creation: %w", err)
	}
	defer tx.Rollback()
	if mediaID != "" {
		if _, _, err := readCoreMedia(ctx, tx, mediaID); err != nil {
			return CoreEntry{}, err
		}
	}
	if err := ensureCorePackageLibrary(ctx, tx); err != nil {
		return CoreEntry{}, err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM core_entries AS e JOIN games AS g ON g.game_id = e.game_id WHERE e.game_id = ? OR (e.core_id = ? AND g.title = ?)) OR EXISTS(SELECT 1 FROM games WHERE game_id = ?)`, gameID, coreID, title, gameID).Scan(&exists); err != nil {
		return CoreEntry{}, fmt.Errorf("check core entry conflict: %w", err)
	}
	if exists != 0 {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	now := time.Now().UnixNano()
	search := searchDocument(gameID, title, CorePlatform)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO games (
		  game_id, library_id, system, relative_path, title, source_kind, source_state,
		  source_size, modified_ns, seen_generation, search_text, canonical_title,
		  group_key, first_seen_ns
		) VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?, ?, ?)`,
		gameID, corePackageLibraryID, CorePlatform, gameID, title, SourceKindCorePackage,
		SourceStateAvailable, search, title, gameID, now,
	); err != nil {
		return CoreEntry{}, fmt.Errorf("insert core package game: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO core_entries(game_id, core_id, package_id, media_role, media_id) VALUES (?, ?, ?, ?, ?)`, gameID, coreID, packageID, mediaRole, mediaID); err != nil {
		return CoreEntry{}, fmt.Errorf("insert core entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CoreEntry{}, fmt.Errorf("commit core entry creation: %w", err)
	}
	return entry, nil
}

// SelectCoreEntry replaces an entry's package only when both its core identity
// and expected current package still match.
func (s *Store) SelectCoreEntry(ctx context.Context, gameID, coreID, expectedID, packageID string) (CoreEntry, error) {
	if err := validateCoreEntryIDs(gameID, coreID, expectedID, packageID); err != nil {
		return CoreEntry{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("begin core entry selection: %w", err)
	}
	defer tx.Rollback()
	var entry CoreEntry
	err = tx.QueryRowContext(ctx, `
		SELECT e.game_id, g.title, e.core_id, e.package_id, e.media_role, e.media_id
		FROM core_entries AS e JOIN games AS g ON g.game_id = e.game_id
		WHERE e.game_id = ?`, gameID,
	).Scan(&entry.GameID, &entry.Title, &entry.CoreID, &entry.PackageID, &entry.MediaRole, &entry.MediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntry{}, ErrCoreEntryNotFound
	}
	if err != nil {
		return CoreEntry{}, fmt.Errorf("read core entry selection: %w", err)
	}
	if entry.CoreID != coreID {
		return CoreEntry{}, ErrCoreEntryCoreMismatch
	}
	if entry.PackageID != expectedID {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE core_entries SET package_id = ? WHERE game_id = ? AND core_id = ? AND package_id = ?`, packageID, gameID, coreID, expectedID)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("update core entry selection: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return CoreEntry{}, fmt.Errorf("confirm core entry selection: %w", err)
	}
	if changed != 1 {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	if err := tx.Commit(); err != nil {
		return CoreEntry{}, fmt.Errorf("commit core entry selection: %w", err)
	}
	entry.PackageID = packageID
	return entry, nil
}

func (s *Store) CoreEntry(ctx context.Context, gameID string) (CoreEntry, error) {
	var entry CoreEntry
	err := s.db.QueryRowContext(ctx, `
		SELECT e.game_id, g.title, e.core_id, e.package_id, e.media_role, e.media_id
		FROM core_entries AS e JOIN games AS g ON g.game_id = e.game_id
		WHERE e.game_id = ?`, gameID,
	).Scan(&entry.GameID, &entry.Title, &entry.CoreID, &entry.PackageID, &entry.MediaRole, &entry.MediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntry{}, ErrCoreEntryNotFound
	}
	if err != nil {
		return CoreEntry{}, fmt.Errorf("read core entry %q: %w", gameID, err)
	}
	return entry, nil
}

func (s *Store) CoreEntries(ctx context.Context) ([]CoreEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.game_id, g.title, e.core_id, e.package_id, e.media_role, e.media_id
		FROM core_entries AS e JOIN games AS g ON g.game_id = e.game_id
		ORDER BY e.game_id`)
	if err != nil {
		return nil, fmt.Errorf("list core entries: %w", err)
	}
	defer rows.Close()
	entries := make([]CoreEntry, 0)
	for rows.Next() {
		var entry CoreEntry
		if err := rows.Scan(&entry.GameID, &entry.Title, &entry.CoreID, &entry.PackageID, &entry.MediaRole, &entry.MediaID); err != nil {
			return nil, fmt.Errorf("read core entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate core entries: %w", err)
	}
	return entries, nil
}

func validateCoreEntry(title, coreID, packageID string) error {
	if title == "" || len(title) > maxCoreEntryTitle || !utf8.ValidString(title) {
		return fmt.Errorf("%w: title is empty, invalid, or too long", ErrInvalidCoreEntry)
	}
	for _, character := range title {
		if unicode.IsControl(character) {
			return fmt.Errorf("%w: title contains control text", ErrInvalidCoreEntry)
		}
	}
	if !coreEntryIDRE.MatchString(coreID) {
		return fmt.Errorf("%w: invalid core ID", ErrInvalidCoreEntry)
	}
	if err := protocol.ValidateDigest(packageID); err != nil {
		return fmt.Errorf("%w: invalid package ID", ErrInvalidCoreEntry)
	}
	return nil
}

func validateCoreEntryIDs(gameID, coreID, expectedID, packageID string) error {
	if err := protocol.ValidateGameID(gameID); err != nil || !coreEntryIDRE.MatchString(coreID) ||
		protocol.ValidateDigest(expectedID) != nil || protocol.ValidateDigest(packageID) != nil {
		return fmt.Errorf("%w: invalid selection identity", ErrInvalidCoreEntry)
	}
	return nil
}

func ensureCorePackageLibrary(ctx context.Context, tx *sql.Tx) error {
	var system protocol.System
	var root string
	err := tx.QueryRowContext(ctx, `SELECT system, root FROM libraries WHERE id = ?`, corePackageLibraryID).Scan(&system, &root)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var occupied int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM libraries WHERE root = ?)`, corePackageRoot).Scan(&occupied); err != nil {
			return fmt.Errorf("check reserved core package collection: %w", err)
		}
		if occupied != 0 {
			return fmt.Errorf("%w: reserved core package collection is occupied", ErrInvalidCoreEntry)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO libraries(id, system, root, online) VALUES (?, ?, ?, 1)`, corePackageLibraryID, CorePlatform, corePackageRoot); err != nil {
			return fmt.Errorf("create core package collection: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("read core package collection: %w", err)
	case system != CorePlatform || root != corePackageRoot:
		return fmt.Errorf("%w: reserved core package collection is occupied", ErrInvalidCoreEntry)
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE libraries SET online = 1, last_error = '' WHERE id = ?`, corePackageLibraryID); err != nil {
			return fmt.Errorf("activate core package collection: %w", err)
		}
		return nil
	}
}

func isCorePackageRoot(root Root) bool {
	return root.ID == corePackageLibraryID || root.System == CorePlatform || root.Path == corePackageRoot
}

func rejectCorePackageRoot(root Root) error {
	if isCorePackageRoot(root) {
		return fmt.Errorf("%w: reserved core package collection cannot be scanned or rebound", ErrInvalidCoreEntry)
	}
	return nil
}
