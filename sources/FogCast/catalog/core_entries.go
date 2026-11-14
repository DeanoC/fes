package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
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
	retainedCoreReason   = "retained-core:"
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
	GameID           string `json:"game_id"`
	Title            string `json:"title"`
	CoreID           string `json:"core_id"`
	PackageID        string `json:"package_id"`
	MediaID          string `json:"media_id,omitempty"`
	MediaRole        string `json:"media_role,omitempty"`
	MediaName        string `json:"media_name,omitempty"`
	FirmwareRequired bool   `json:"firmware_required,omitempty"`
	// DiskGeometry overrides the geometry inferred from a hard-disk image's
	// master boot record. Nil means inferred.
	DiskGeometry *protocol.DiskGeometry `json:"disk_geometry,omitempty"`
}

// ValidateCoreEntry checks the persisted identity and media selection without
// reading media bytes. Launch still verifies the selected media and geometry
// against its actual size.
func ValidateCoreEntry(entry CoreEntry) error {
	if err := protocol.ValidateGameID(entry.GameID); err != nil {
		return fmt.Errorf("%w: invalid game id", ErrInvalidCoreEntry)
	}
	if err := validateCoreEntry(strings.TrimSpace(entry.Title), entry.CoreID, entry.PackageID); err != nil {
		return err
	}
	if err := validateCoreMediaSelection(entry.MediaRole, entry.MediaID); err != nil {
		return err
	}
	if entry.MediaName != "" && (entry.MediaID == "" || !AdmitCoreEntryMediaName(entry.MediaName)) {
		return fmt.Errorf("%w: invalid media name", ErrInvalidCoreMedia)
	}
	if entry.DiskGeometry != nil && (entry.MediaRole != protocol.HardDiskRole || !entry.DiskGeometry.Valid(protocol.AtaDiskMaxBytes)) {
		return fmt.Errorf("%w: invalid disk geometry", ErrInvalidCoreEntry)
	}
	return nil
}

// CoreEntryID returns the deterministic ID of a validated local title.
func CoreEntryID(title, coreID, packageID string) (string, error) {
	title = strings.TrimSpace(title)
	if err := validateCoreEntry(title, coreID, packageID); err != nil {
		return "", err
	}
	return GameID(CorePlatform, corePackageLibraryID, coreID+"\x00"+title, title), nil
}

type coreEntryQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// retainedLocalEntryID recovers legacy IDs as well as current ones. Remote
// entries use a separate 256-bit source identity and are not local titles.
func retainedLocalEntryID(ctx context.Context, q coreEntryQueryer, title, coreID string) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT g.game_id FROM games g
		WHERE g.library_id = ? AND g.system = ?
		  AND g.source_kind = ? AND g.reason = ? AND g.title = ?
		  AND NOT (length(g.game_id) = 76 AND substr(g.game_id, 1, 12) = 'fpga-remote-')
		  AND NOT EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = g.game_id)
		ORDER BY g.game_id LIMIT 2`, corePackageLibraryID, CorePlatform, SourceKindCorePackage, retainedCoreReason+coreID, title)
	if err != nil {
		return "", fmt.Errorf("find retained core title: %w", err)
	}
	defer rows.Close()
	var id string
	for rows.Next() {
		if id != "" {
			return "", ErrCoreEntryConflict
		}
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("read retained core title: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("list retained core title: %w", err)
	}
	return id, nil
}

// ResolveCoreEntryID returns the retained stable identity, including a legacy
// ID, before a caller cleans interrupted household-state references.
func (s *Store) ResolveCoreEntryID(ctx context.Context, title, coreID, packageID string) (string, error) {
	id, err := CoreEntryID(title, coreID, packageID)
	if err != nil {
		return "", err
	}
	retained, err := retainedLocalEntryID(ctx, s.db, strings.TrimSpace(title), coreID)
	if err != nil {
		return "", err
	}
	if retained != "" {
		return retained, nil
	}
	return id, nil
}

const coreEntrySelect = `SELECT e.game_id, g.title, e.core_id, e.package_id, e.media_role, e.media_id, e.media_name, e.firmware_required,
		e.disk_cylinders, e.disk_heads, e.disk_sectors
		FROM core_entries AS e JOIN games AS g ON g.game_id = e.game_id`

func scanCoreEntry(scan func(...any) error) (CoreEntry, error) {
	var entry CoreEntry
	var firmware int
	var geometry protocol.DiskGeometry
	err := scan(&entry.GameID, &entry.Title, &entry.CoreID, &entry.PackageID, &entry.MediaRole, &entry.MediaID, &entry.MediaName, &firmware,
		&geometry.Cylinders, &geometry.Heads, &geometry.SectorsPerTrack)
	entry.FirmwareRequired = firmware != 0
	if geometry != (protocol.DiskGeometry{}) {
		entry.DiskGeometry = &geometry
	}
	return entry, err
}

func firmwareRequiredInt(required bool) int {
	if required {
		return 1
	}
	return 0
}

// CreateCoreEntry creates a durable title for coreID without media.
func (s *Store) CreateCoreEntry(ctx context.Context, title, coreID, packageID string) (CoreEntry, error) {
	return s.CreateCoreMediaEntry(ctx, title, coreID, packageID, "", "")
}

// CreateCoreMediaEntry atomically creates a title with an explicit media selection.
func (s *Store) CreateCoreMediaEntry(ctx context.Context, title, coreID, packageID, mediaRole, mediaID string) (CoreEntry, error) {
	return s.createCoreEntry(ctx, title, coreID, packageID, mediaRole, mediaID, false, nil, nil, nil)
}

// CreateFirmwareRequiredEntry creates a title that requires firmware to become Ready.
func (s *Store) CreateFirmwareRequiredEntry(ctx context.Context, title, coreID, packageID, mediaRole, mediaID string) (CoreEntry, error) {
	return s.createCoreEntry(ctx, title, coreID, packageID, mediaRole, mediaID, true, nil, nil, nil)
}

// CreateCoreROMEntry creates a title and its named ROM selection in one transaction.
func (s *Store) CreateCoreROMEntry(ctx context.Context, title, coreID, packageID, romID string, sourceSize int64, mediaID string, firmwareRequired bool) (CoreEntry, error) {
	if strings.TrimSpace(romID) == "" || protocol.ValidateDigest(mediaID) != nil || sourceSize < 1 || sourceSize > MaxCoreMediaBytes {
		return CoreEntry{}, ErrInvalidCoreROM
	}
	rom := &CoreEntryROM{ROMID: romID, MediaID: mediaID, SourceSize: sourceSize}
	return s.createCoreEntry(ctx, title, coreID, packageID, "", "", firmwareRequired, nil, nil, rom)
}

type KitHostEntryIdentity struct {
	Identity      string
	SourceGameID  string
	ROMID         string
	ROMMediaID    string
	ROMSourceSize int64
}

// CreateKitHostEntry atomically creates its provenance and optional ROM binding.
func (s *Store) CreateKitHostEntry(ctx context.Context, title, coreID, packageID, role, mediaID string, identity KitHostEntryIdentity) (CoreEntry, error) {
	if protocol.ValidateDigest(identity.Identity) != nil || (identity.SourceGameID != "" && protocol.ValidateGameID(identity.SourceGameID) != nil) {
		return CoreEntry{}, ErrInvalidCoreEntry
	}
	if identity.ROMID != "" && (strings.TrimSpace(identity.ROMID) == "" || protocol.ValidateDigest(identity.ROMMediaID) != nil || identity.ROMSourceSize < 1 || identity.ROMSourceSize > MaxCoreMediaBytes) {
		return CoreEntry{}, ErrInvalidCoreROM
	}
	if identity.ROMID == "" && (identity.ROMMediaID != "" || identity.ROMSourceSize != 0) {
		return CoreEntry{}, ErrInvalidCoreROM
	}
	return s.createCoreEntry(ctx, title, coreID, packageID, role, mediaID, false, &identity, nil, nil)
}

func (s *Store) createCoreEntry(ctx context.Context, title, coreID, packageID, mediaRole, mediaID string, firmwareRequired bool, kit *KitHostEntryIdentity, remote *RemoteCoreEntryOrigin, initialROM *CoreEntryROM) (CoreEntry, error) {
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
	gameID, _ := CoreEntryID(title, coreID, packageID)
	if remote != nil {
		gameID = RemoteCoreEntryID(remote.SourceID, remote.SourceGameID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("begin core entry creation: %w", err)
	}
	defer tx.Rollback()
	if remote == nil && kit == nil {
		retained, err := retainedLocalEntryID(ctx, tx, title, coreID)
		if err != nil {
			return CoreEntry{}, err
		}
		if retained != "" {
			gameID = retained
		}
	}
	entry := CoreEntry{GameID: gameID, Title: title, CoreID: coreID, PackageID: packageID, MediaRole: mediaRole, MediaID: mediaID, FirmwareRequired: firmwareRequired}
	if remote != nil {
		existing, err := scanCoreEntry(tx.QueryRowContext(ctx, coreEntrySelect+` WHERE e.game_id = (SELECT game_id FROM remote_core_entries WHERE source_id = ? AND source_game_id = ?)`, remote.SourceID, remote.SourceGameID).Scan)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return CoreEntry{}, fmt.Errorf("read remote core entry: %w", err)
		}
	}
	if mediaID != "" {
		if _, err := verifyCoreMedia(ctx, tx, mediaID, io.Discard); err != nil {
			return CoreEntry{}, err
		}
	}
	if err := ensureCorePackageLibrary(ctx, tx); err != nil {
		return CoreEntry{}, err
	}
	// A removed entry keeps its parent game row to retain captured SAVE
	// metadata and target data keyed by this stable ID. Recreate only the
	// exact core-package identity, never an unrelated scanned game.
	var gameExists, reusable int
	allowChangedTitle := 0
	if remote != nil {
		allowChangedTitle = 1
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM games WHERE game_id = ?),
		EXISTS(SELECT 1 FROM games g WHERE g.game_id = ? AND g.library_id = ? AND g.system = ?
		  AND g.source_kind = ? AND g.reason = ? AND (? = 1 OR g.title = ?) AND NOT EXISTS
		  (SELECT 1 FROM core_entries e WHERE e.game_id = g.game_id))`,
		gameID, gameID, corePackageLibraryID, CorePlatform, SourceKindCorePackage,
		retainedCoreReason+coreID, allowChangedTitle, title).Scan(&gameExists, &reusable); err != nil {
		return CoreEntry{}, fmt.Errorf("check retained core entry: %w", err)
	}
	if gameExists != 0 && reusable == 0 {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	if kit != nil && reusable != 0 {
		// A generated helper must never claim a removed user's saved identity.
		return CoreEntry{}, ErrCoreEntryConflict
	}
	if remote == nil {
		// A remote title may share its visible name with a local title.
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM core_entries e
		  JOIN games g ON g.game_id = e.game_id WHERE e.core_id = ? AND g.title = ?
		  AND NOT EXISTS (SELECT 1 FROM remote_core_entries r WHERE r.game_id = e.game_id))`, coreID, title).Scan(&exists); err != nil {
			return CoreEntry{}, fmt.Errorf("check core entry conflict: %w", err)
		}
		if exists != 0 {
			return CoreEntry{}, ErrCoreEntryConflict
		}
	}
	now := time.Now().UnixNano()
	search := searchDocument(gameID, title, CorePlatform)
	if reusable == 0 {
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
	} else {
		// Remote source titles may have changed since removal. Refresh the
		// visible metadata on explicit recreation and clear the retention marker.
		if _, err := tx.ExecContext(ctx, `UPDATE games SET title = ?, canonical_title = ?, search_text = ?, reason = '' WHERE game_id = ?`,
			title, title, search, gameID); err != nil {
			return CoreEntry{}, fmt.Errorf("restore retained core entry: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO core_entries(game_id, core_id, package_id, media_role, media_id, firmware_required) VALUES (?, ?, ?, ?, ?, ?)`, gameID, coreID, packageID, mediaRole, mediaID, firmwareRequiredInt(firmwareRequired)); err != nil {
		return CoreEntry{}, fmt.Errorf("insert core entry: %w", err)
	}
	if remote != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO remote_core_entries(game_id, source_id, source_game_id, package_id, media_id, media_name) VALUES (?, ?, ?, ?, ?, ?)`, gameID, remote.SourceID, remote.SourceGameID, packageID, remote.MediaID, remote.MediaName); err != nil {
			return CoreEntry{}, fmt.Errorf("insert remote core origin: %w", err)
		}
	}
	if initialROM != nil {
		media, err := verifyCoreMedia(ctx, tx, initialROM.MediaID, io.Discard)
		if err != nil {
			return CoreEntry{}, err
		}
		if media.Size != initialROM.SourceSize {
			return CoreEntry{}, ErrInvalidCoreROM
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO core_entry_roms(game_id,package_id,rom_id,media_id,source_size) VALUES(?,?,?,?,?)`, gameID, packageID, initialROM.ROMID, initialROM.MediaID, initialROM.SourceSize); err != nil {
			return CoreEntry{}, err
		}
	}
	if kit != nil {
		if kit.ROMID != "" {
			media, err := verifyCoreMedia(ctx, tx, kit.ROMMediaID, io.Discard)
			if err != nil {
				return CoreEntry{}, err
			}
			if media.Size != kit.ROMSourceSize {
				return CoreEntry{}, ErrInvalidCoreROM
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO core_entry_roms(game_id,package_id,rom_id,media_id,source_size) VALUES(?,?,?,?,?)`, gameID, packageID, kit.ROMID, kit.ROMMediaID, kit.ROMSourceSize); err != nil {
				return CoreEntry{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO kit_host_entries(identity,game_id,source_game_id) VALUES (?,?,?)`, kit.Identity, gameID, kit.SourceGameID); err != nil {
			return CoreEntry{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CoreEntry{}, fmt.Errorf("commit core entry creation: %w", err)
	}
	return entry, nil
}

// RetainedCoreEntry reports an invisible parent left to preserve saved media.
func (s *Store) RetainedCoreEntry(ctx context.Context, gameID string) (bool, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return false, ErrInvalidCoreEntry
	}
	var retained int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM games g
		WHERE g.game_id = ? AND g.library_id = ? AND g.system = ?
		  AND g.source_kind = ? AND substr(g.reason, 1, ?) = ?
		  AND NOT EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = g.game_id))`,
		gameID, corePackageLibraryID, CorePlatform, SourceKindCorePackage,
		len(retainedCoreReason), retainedCoreReason).Scan(&retained)
	if err != nil {
		return false, fmt.Errorf("find retained core entry: %w", err)
	}
	return retained != 0, nil
}

// RemoveCoreEntry removes the visible entry but retains the parent identity,
// saved-cassette records and content-addressed data. Repeating removal for a
// retained identity succeeds, so callers can retry separate user-state cleanup.
func (s *Store) RemoveCoreEntry(ctx context.Context, gameID string) error {
	if protocol.ValidateGameID(gameID) != nil {
		return ErrInvalidCoreEntry
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connect for core entry removal: %w", err)
	}
	defer conn.Close()
	// Foreign keys are connection-local. Enable them on this connection so
	// per-entry ROM, expansion, remote and kit-helper selections cascade.
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable core entry removal foreign keys: %w", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin core entry removal: %w", err)
	}
	defer tx.Rollback()
	var reason string
	var coreID sql.NullString
	var generated int
	err = tx.QueryRowContext(ctx, `SELECT g.reason, e.core_id,
		EXISTS(SELECT 1 FROM kit_host_entries k WHERE k.game_id = g.game_id) FROM games g
		LEFT JOIN core_entries e ON e.game_id = g.game_id
		WHERE g.game_id = ? AND g.library_id = ? AND g.system = ? AND g.source_kind = ?`,
		gameID, corePackageLibraryID, CorePlatform, SourceKindCorePackage).Scan(&reason, &coreID, &generated)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCoreEntryNotFound
	}
	if err != nil {
		return fmt.Errorf("find core entry removal: %w", err)
	}
	if generated != 0 {
		return ErrCoreEntryNotFound
	}
	if !coreID.Valid {
		if !strings.HasPrefix(reason, retainedCoreReason) {
			return ErrCoreEntryNotFound
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE games SET reason = ? WHERE game_id = ?`, retainedCoreReason+coreID.String, gameID); err != nil {
		return fmt.Errorf("retain core entry identity: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM core_entries WHERE game_id = ?`, gameID); err != nil {
		return fmt.Errorf("remove core entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit core entry removal: %w", err)
	}
	return nil
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
	entry, err := scanCoreEntry(tx.QueryRowContext(ctx, coreEntrySelect+` WHERE e.game_id = ?`, gameID).Scan)
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
	entry, err := scanCoreEntry(s.db.QueryRowContext(ctx, coreEntrySelect+` WHERE e.game_id = ?`, gameID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntry{}, ErrCoreEntryNotFound
	}
	if err != nil {
		return CoreEntry{}, fmt.Errorf("read core entry %q: %w", gameID, err)
	}
	return entry, nil
}

// CoreEntriesByIDs returns only the requested non-kit-host entries.
// The JSON-backed ID predicate avoids the SQLite variable-count limit.
func (s *Store) CoreEntriesByIDs(ctx context.Context, ids []string) ([]CoreEntry, error) {
	if len(ids) == 0 {
		return []CoreEntry{}, nil
	}
	clause, args := idListSQL("e.game_id", ids, false)
	rows, err := s.db.QueryContext(ctx, coreEntrySelect+` WHERE NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = e.game_id)`+clause+` ORDER BY e.game_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list selected core entries: %w", err)
	}
	defer rows.Close()
	entries := make([]CoreEntry, 0, len(ids))
	for rows.Next() {
		entry, err := scanCoreEntry(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("read selected core entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate selected core entries: %w", err)
	}
	return entries, nil
}

func (s *Store) CoreEntries(ctx context.Context) ([]CoreEntry, error) {
	return s.coreEntries(ctx, false)
}

// AllCoreEntries includes generated kit-host entries for internal name allocation.
func (s *Store) AllCoreEntries(ctx context.Context) ([]CoreEntry, error) {
	return s.coreEntries(ctx, true)
}

func (s *Store) coreEntries(ctx context.Context, includeKitHost bool) ([]CoreEntry, error) {
	query := coreEntrySelect
	if !includeKitHost {
		query += ` WHERE NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = e.game_id)`
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY e.game_id`)
	if err != nil {
		return nil, fmt.Errorf("list core entries: %w", err)
	}
	defer rows.Close()
	entries := make([]CoreEntry, 0)
	for rows.Next() {
		entry, err := scanCoreEntry(rows.Scan)
		if err != nil {
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
