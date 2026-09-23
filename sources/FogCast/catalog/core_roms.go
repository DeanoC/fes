package catalog

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"strings"
)

var ErrInvalidCoreROM = errors.New("catalog ROM selection is invalid")

type CoreEntryROM struct {
	GameID     string `json:"game_id"`
	PackageID  string `json:"package_id,omitempty"`
	ROMID      string `json:"rom_id,omitempty"`
	MediaID    string `json:"media_id,omitempty"`
	SourceSize int64  `json:"source_size,omitempty"`
}

func (s *Store) CoreEntryROM(ctx context.Context, gameID string) (CoreEntryROM, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return CoreEntryROM{}, ErrInvalidCoreEntry
	}
	result := CoreEntryROM{GameID: gameID}
	err := s.db.QueryRowContext(ctx, `SELECT package_id,rom_id,media_id,source_size FROM core_entry_roms WHERE game_id=?`, gameID).Scan(&result.PackageID, &result.ROMID, &result.MediaID, &result.SourceSize)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	return result, err
}

// SelectCoreEntryROM binds an existing immutable binary to an exact package and
// named requirement. The expected digest prevents overwriting concurrent edits.
func (s *Store) SelectCoreEntryROM(ctx context.Context, gameID, packageID, romID string, sourceSize int64, expected, mediaID string) (CoreEntryROM, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(packageID) != nil || strings.TrimSpace(romID) == "" || sourceSize < 1 || sourceSize > MaxCoreMediaBytes || (expected != "" && protocol.ValidateDigest(expected) != nil) || (mediaID != "" && protocol.ValidateDigest(mediaID) != nil) {
		return CoreEntryROM{}, ErrInvalidCoreROM
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntryROM{}, err
	}
	defer tx.Rollback()
	var selected string
	err = tx.QueryRowContext(ctx, `SELECT package_id FROM core_entries WHERE game_id=?`, gameID).Scan(&selected)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntryROM{}, ErrCoreEntryNotFound
	}
	if err != nil {
		return CoreEntryROM{}, err
	}
	if selected != packageID {
		return CoreEntryROM{}, ErrCoreEntryConflict
	}
	current := ""
	err = tx.QueryRowContext(ctx, `SELECT media_id FROM core_entry_roms WHERE game_id=?`, gameID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CoreEntryROM{}, err
	}
	if current != expected {
		return CoreEntryROM{}, ErrCoreEntryConflict
	}
	result := CoreEntryROM{GameID: gameID}
	if mediaID == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM core_entry_roms WHERE game_id=?`, gameID)
	} else {
		media, verifyErr := verifyCoreMedia(ctx, tx, mediaID, io.Discard)
		if verifyErr != nil {
			return CoreEntryROM{}, verifyErr
		}
		if media.Size != sourceSize {
			return CoreEntryROM{}, ErrInvalidCoreROM
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO core_entry_roms(game_id,package_id,rom_id,media_id,source_size) VALUES(?,?,?,?,?) ON CONFLICT(game_id) DO UPDATE SET package_id=excluded.package_id,rom_id=excluded.rom_id,media_id=excluded.media_id,source_size=excluded.source_size`, gameID, packageID, romID, mediaID, sourceSize)
		result = CoreEntryROM{GameID: gameID, PackageID: packageID, ROMID: romID, MediaID: mediaID, SourceSize: sourceSize}
	}
	if err != nil {
		return CoreEntryROM{}, err
	}
	if err = tx.Commit(); err != nil {
		return CoreEntryROM{}, err
	}
	return result, nil
}
