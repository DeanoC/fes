package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DeanoC/FogCast/protocol"
)

var (
	ErrInvalidCoreMedia  = errors.New("catalog core media is invalid")
	ErrCoreMediaNotFound = errors.New("catalog core media not found")
)

// CoreMedia describes immutable, content-addressed bytes. It exposes no path.
type CoreMedia struct {
	MediaID string `json:"media_id"`
	Size    int64  `json:"size"`
}

func coreMediaIdentity(data []byte) (CoreMedia, error) {
	if len(data) < 1 || int64(len(data)) > protocol.MaxDevelopmentMediaBytes {
		return CoreMedia{}, fmt.Errorf("%w: size must be 1..%d bytes", ErrInvalidCoreMedia, protocol.MaxDevelopmentMediaBytes)
	}
	return CoreMedia{MediaID: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data))}, nil
}

// ImportCoreMedia stores a copy of data, returning true only for a new object.
// An existing object is verified, never replaced.
func (s *Store) ImportCoreMedia(ctx context.Context, data []byte) (CoreMedia, bool, error) {
	// Reject oversized input before copying, then hash exactly the owned bytes
	// that will be stored.
	if len(data) < 1 || int64(len(data)) > protocol.MaxDevelopmentMediaBytes {
		return CoreMedia{}, false, fmt.Errorf("%w: size must be 1..%d bytes", ErrInvalidCoreMedia, protocol.MaxDevelopmentMediaBytes)
	}
	data = bytes.Clone(data)
	media, err := coreMediaIdentity(data)
	if err != nil {
		return CoreMedia{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreMedia{}, false, fmt.Errorf("begin core media import: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO core_media(media_id, size, data) VALUES (?, ?, ?) ON CONFLICT(media_id) DO NOTHING`, media.MediaID, media.Size, data)
	if err != nil {
		return CoreMedia{}, false, fmt.Errorf("insert core media: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return CoreMedia{}, false, fmt.Errorf("count core media import: %w", err)
	}
	if _, _, err := readCoreMedia(ctx, tx, media.MediaID); err != nil {
		return CoreMedia{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return CoreMedia{}, false, fmt.Errorf("commit core media import: %w", err)
	}
	return media, changed == 1, nil
}

// CoreMedia returns a defensive copy after revalidating the stored size and hash.
func (s *Store) CoreMedia(ctx context.Context, id string) (CoreMedia, []byte, error) {
	return readCoreMedia(ctx, s.db, id)
}

func readCoreMedia(ctx context.Context, q contentMatchQuerier, id string) (CoreMedia, []byte, error) {
	if protocol.ValidateDigest(id) != nil {
		return CoreMedia{}, nil, fmt.Errorf("%w: invalid media ID", ErrInvalidCoreMedia)
	}
	var size int64
	var data []byte
	// CASE prevents corrupt oversized bytes from reaching the driver. Both
	// metadata and bytes are checked in the same statement snapshot; NULL
	// deliberately flows through the existing invalid-media check below.
	err := q.QueryRowContext(ctx, `SELECT size,
		CASE WHEN typeof(data) = 'blob' AND length(data) BETWEEN 1 AND ?
		          AND length(data) = size THEN data ELSE NULL END
		FROM core_media WHERE media_id = ?`, protocol.MaxDevelopmentMediaBytes, id).Scan(&size, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreMedia{}, nil, ErrCoreMediaNotFound
	}
	if err != nil {
		return CoreMedia{}, nil, fmt.Errorf("read core media: %w", err)
	}
	media, err := coreMediaIdentity(data)
	if err != nil || media.MediaID != id || media.Size != size {
		return CoreMedia{}, nil, fmt.Errorf("%w: stored size or digest mismatch", ErrInvalidCoreMedia)
	}
	return media, bytes.Clone(data), nil
}

func validateCoreMediaSelection(role, id string) error {
	if role == "" && id == "" {
		return nil
	}
	if role != "blob" || protocol.ValidateDigest(id) != nil {
		return fmt.Errorf("%w: expected blob role and media digest, or both empty", ErrInvalidCoreMedia)
	}
	return nil
}

// SelectCoreEntryMedia changes only media, conditional on the package and media
// IDs observed by the caller. Empty role and ID clear the selection.
func (s *Store) SelectCoreEntryMedia(ctx context.Context, gameID, expectedPackageID, expectedMediaID, mediaRole, mediaID string) (CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(expectedPackageID) != nil {
		return CoreEntry{}, fmt.Errorf("%w: invalid selection identity", ErrInvalidCoreEntry)
	}
	if expectedMediaID != "" && protocol.ValidateDigest(expectedMediaID) != nil {
		return CoreEntry{}, fmt.Errorf("%w: invalid expected media ID", ErrInvalidCoreMedia)
	}
	if err := validateCoreMediaSelection(mediaRole, mediaID); err != nil {
		return CoreEntry{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("begin core entry media selection: %w", err)
	}
	defer tx.Rollback()
	var entry CoreEntry
	err = tx.QueryRowContext(ctx, `
  SELECT e.game_id, g.title, e.core_id, e.package_id, e.media_role, e.media_id
  FROM core_entries AS e JOIN games AS g ON g.game_id = e.game_id WHERE e.game_id = ?`, gameID).
		Scan(&entry.GameID, &entry.Title, &entry.CoreID, &entry.PackageID, &entry.MediaRole, &entry.MediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntry{}, ErrCoreEntryNotFound
	}
	if err != nil {
		return CoreEntry{}, fmt.Errorf("read core entry media selection: %w", err)
	}
	if entry.PackageID != expectedPackageID || entry.MediaID != expectedMediaID {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	if mediaID != "" {
		if _, _, err := readCoreMedia(ctx, tx, mediaID); err != nil {
			return CoreEntry{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE core_entries SET media_role = ?, media_id = ? WHERE game_id = ? AND package_id = ? AND media_id = ?`,
		mediaRole, mediaID, gameID, expectedPackageID, expectedMediaID)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("update core entry media selection: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return CoreEntry{}, fmt.Errorf("count core entry media selection: %w", err)
	}
	if changed != 1 {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	if err := tx.Commit(); err != nil {
		return CoreEntry{}, fmt.Errorf("commit core entry media selection: %w", err)
	}
	entry.MediaRole, entry.MediaID = mediaRole, mediaID
	return entry, nil
}
