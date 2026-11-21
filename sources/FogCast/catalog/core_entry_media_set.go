package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/DeanoC/FogCast/protocol"
)

// MaxCoreEntryDisks is the most media one title's set holds, disk 1 included.
const MaxCoreEntryDisks = 16

// SetCoreEntryMediaSet replaces disks 2 to N of a title's set. Disk 1 is the
// entry's selected media and must be a disk or a cassette; every further disk
// takes its role and drive, and no disk appears twice. An empty list makes
// the title a single disk again. expectedMore is the set the caller last
// read: a title whose package, disk 1 or set has moved on is a conflict.
func (s *Store) SetCoreEntryMediaSet(ctx context.Context, gameID, expectedPackageID, expectedMediaID string, expectedMore, more []string) (CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(expectedPackageID) != nil || protocol.ValidateDigest(expectedMediaID) != nil {
		return CoreEntry{}, fmt.Errorf("%w: invalid media set identity", ErrInvalidCoreEntry)
	}
	if err := ValidateCoreEntryMediaSet(expectedMediaID, expectedMore); err != nil {
		return CoreEntry{}, err
	}
	if err := ValidateCoreEntryMediaSet(expectedMediaID, more); err != nil {
		return CoreEntry{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("begin core entry media set: %w", err)
	}
	defer tx.Rollback()
	entry, err := scanCoreEntry(tx.QueryRowContext(ctx, coreEntrySelect+` WHERE e.game_id = ?`, gameID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntry{}, ErrCoreEntryNotFound
	}
	if err != nil {
		return CoreEntry{}, fmt.Errorf("read core entry media set: %w", err)
	}
	if entry.PackageID != expectedPackageID || entry.MediaID != expectedMediaID || !slices.Equal(entry.MoreMedia, expectedMore) {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	if entry.MediaRole != protocol.DiskRole && entry.MediaRole != protocol.CassetteRole {
		return CoreEntry{}, fmt.Errorf("%w: only a disk or a cassette can start a set", ErrInvalidCoreMedia)
	}
	for _, id := range more {
		if _, err := verifyCoreMedia(ctx, tx, id, io.Discard); err != nil {
			return CoreEntry{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM core_entry_media_set WHERE game_id = ?`, gameID); err != nil {
		return CoreEntry{}, fmt.Errorf("clear core entry media set: %w", err)
	}
	for i, id := range more {
		if _, err := tx.ExecContext(ctx, `INSERT INTO core_entry_media_set(game_id, position, media_id) VALUES (?, ?, ?)`, gameID, i+2, id); err != nil {
			return CoreEntry{}, fmt.Errorf("store core entry media set: %w", err)
		}
	}
	entry, err = scanCoreEntry(tx.QueryRowContext(ctx, coreEntrySelect+` WHERE e.game_id = ?`, gameID).Scan)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("read core entry media set: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CoreEntry{}, fmt.Errorf("commit core entry media set: %w", err)
	}
	return entry, nil
}

// ValidateCoreEntryMediaSet checks the shape of disks 2 to N against disk 1
// without reading media: digests, the size limit, and no disk twice.
func ValidateCoreEntryMediaSet(first string, more []string) error {
	if len(more) > MaxCoreEntryDisks-1 {
		return fmt.Errorf("%w: a set holds at most %d disks", ErrInvalidCoreMedia, MaxCoreEntryDisks)
	}
	seen := map[string]bool{first: true}
	for _, id := range more {
		if protocol.ValidateDigest(id) != nil {
			return fmt.Errorf("%w: invalid media ID in set", ErrInvalidCoreMedia)
		}
		if seen[id] {
			return fmt.Errorf("%w: a disk appears twice in the set", ErrInvalidCoreMedia)
		}
		seen[id] = true
	}
	return nil
}
