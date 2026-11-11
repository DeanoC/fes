package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/DeanoC/FogCast/protocol"
)

// RemoteCoreEntryOrigin is the immutable source snapshot associated with a
// visible local entry. PackageID names the kit's package, not the source's.
// Pending is consumed by an automatic bind or an explicit selection edit.
type RemoteCoreEntryOrigin struct {
	SourceID     string
	SourceGameID string
	PackageID    string
	MediaID      string
	MediaName    string
	Pending      bool
}

var remoteSourceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`)

// RemoteCoreEntryID identifies an already validated source/title pair.
func RemoteCoreEntryID(sourceID, sourceGameID string) string {
	digest := sha256.Sum256([]byte("remote-core-entry\x00" + sourceID + "\x00" + sourceGameID))
	return fmt.Sprintf("fpga-remote-%x", digest)
}

func validRemoteCoreOrigin(origin RemoteCoreEntryOrigin) bool {
	return remoteSourceIDPattern.MatchString(origin.SourceID) && len(origin.SourceGameID) <= 256 &&
		protocol.ValidateGameID(origin.SourceGameID) == nil && protocol.ValidateDigest(origin.PackageID) == nil &&
		protocol.ValidateDigest(origin.MediaID) == nil && (origin.MediaName == "" || AdmitCoreEntryMediaName(origin.MediaName))
}

// CreateRemoteCoreEntry creates a visible title and its provenance together.
// Re-observing the same source title returns the existing row without changing
// either its original provenance or any local user selections.
func (s *Store) CreateRemoteCoreEntry(ctx context.Context, title, coreID, packageID string, origin RemoteCoreEntryOrigin) (CoreEntry, error) {
	if origin.PackageID != "" && origin.PackageID != packageID {
		return CoreEntry{}, ErrInvalidCoreEntry
	}
	origin.PackageID = packageID
	if !validRemoteCoreOrigin(origin) {
		return CoreEntry{}, ErrInvalidCoreEntry
	}
	return s.createCoreEntry(ctx, title, coreID, packageID, "", "", false, nil, &origin)
}

func scanRemoteCoreOrigin(scan func(...any) error) (RemoteCoreEntryOrigin, error) {
	var origin RemoteCoreEntryOrigin
	var pending int
	err := scan(&origin.SourceID, &origin.SourceGameID, &origin.PackageID, &origin.MediaID, &origin.MediaName, &pending)
	origin.Pending = pending != 0
	return origin, err
}

// RemoteCoreEntry returns source provenance even after a user edits the entry.
func (s *Store) RemoteCoreEntry(ctx context.Context, gameID string) (RemoteCoreEntryOrigin, bool, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return RemoteCoreEntryOrigin{}, false, ErrInvalidCoreEntry
	}
	origin, err := scanRemoteCoreOrigin(s.db.QueryRowContext(ctx, `SELECT source_id, source_game_id, package_id, media_id, media_name, pending FROM remote_core_entries WHERE game_id = ?`, gameID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return RemoteCoreEntryOrigin{}, false, nil
	}
	if err != nil {
		return RemoteCoreEntryOrigin{}, false, fmt.Errorf("read remote core origin: %w", err)
	}
	return origin, true, nil
}

// IsRemoteSourceEntry keeps imported titles from being exported to a source.
func (s *Store) IsRemoteSourceEntry(ctx context.Context, gameID string) (bool, error) {
	_, found, err := s.RemoteCoreEntry(ctx, gameID)
	return found, err
}

// RemoteCoreEntryBySource resolves a previously observed source identity.
func (s *Store) RemoteCoreEntryBySource(ctx context.Context, sourceID, sourceGameID string) (CoreEntry, RemoteCoreEntryOrigin, bool, error) {
	if !remoteSourceIDPattern.MatchString(sourceID) || len(sourceGameID) > 256 || protocol.ValidateGameID(sourceGameID) != nil {
		return CoreEntry{}, RemoteCoreEntryOrigin{}, false, ErrInvalidCoreEntry
	}
	var gameID string
	err := s.db.QueryRowContext(ctx, `SELECT game_id FROM remote_core_entries WHERE source_id = ? AND source_game_id = ?`, sourceID, sourceGameID).Scan(&gameID)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntry{}, RemoteCoreEntryOrigin{}, false, nil
	}
	if err != nil {
		return CoreEntry{}, RemoteCoreEntryOrigin{}, false, fmt.Errorf("read remote core identity: %w", err)
	}
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return CoreEntry{}, RemoteCoreEntryOrigin{}, false, err
	}
	origin, found, err := s.RemoteCoreEntry(ctx, gameID)
	if err != nil || !found {
		return CoreEntry{}, RemoteCoreEntryOrigin{}, false, err
	}
	return entry, origin, true, nil
}

// BindRemoteCoreEntryMedia commits a verified cassette only if its exact
// pending origin and the empty local selection still match. User selections,
// including no-op edits while a download runs, win over the automatic bind.
func (s *Store) BindRemoteCoreEntryMedia(ctx context.Context, gameID string, origin RemoteCoreEntryOrigin) (CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil || !validRemoteCoreOrigin(origin) || !origin.Pending {
		return CoreEntry{}, ErrInvalidCoreEntry
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("begin remote media binding: %w", err)
	}
	defer tx.Rollback()
	if _, err := verifyCoreMedia(ctx, tx, origin.MediaID, io.Discard); err != nil {
		return CoreEntry{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE core_entries SET media_role = ?, media_id = ?, media_name = ?
		WHERE game_id = ? AND package_id = ? AND media_role = '' AND media_id = ''
		AND EXISTS (SELECT 1 FROM remote_core_entries r WHERE r.game_id = core_entries.game_id
		  AND r.source_id = ? AND r.source_game_id = ? AND r.package_id = ? AND r.media_id = ? AND r.media_name = ? AND r.pending = 1)`,
		protocol.CassetteRole, origin.MediaID, origin.MediaName, gameID, origin.PackageID,
		origin.SourceID, origin.SourceGameID, origin.PackageID, origin.MediaID, origin.MediaName)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("bind remote media: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return CoreEntry{}, fmt.Errorf("count remote media bindings: %w", err)
	}
	if changed != 1 {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	entry, err := scanCoreEntry(tx.QueryRowContext(ctx, coreEntrySelect+` WHERE e.game_id = ?`, gameID).Scan)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("read bound remote entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CoreEntry{}, fmt.Errorf("commit remote media binding: %w", err)
	}
	return entry, nil
}
