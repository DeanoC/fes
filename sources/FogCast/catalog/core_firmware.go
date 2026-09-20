package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/DeanoC/FogCast/protocol"
)

var (
	ErrCoreFirmwareNotFound = errors.New("catalog household firmware not found")
	ErrInvalidCoreFirmware  = errors.New("catalog household firmware is invalid")
)

// CoreFirmware is the household firmware slot. Bytes live in core_media.
type CoreFirmware struct {
	Slot    string `json:"slot"`
	MediaID string `json:"media_id,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

// CoreFirmware returns the household firmware slot. An empty MediaID means unset.
func (s *Store) CoreFirmware(ctx context.Context, slot string) (CoreFirmware, error) {
	if slot != protocol.FirmwareRole {
		return CoreFirmware{}, fmt.Errorf("%w: household firmware slot must be %q", ErrInvalidCoreFirmware, protocol.FirmwareRole)
	}
	var mediaID string
	err := s.db.QueryRowContext(ctx, `SELECT media_id FROM core_firmware WHERE slot = ?`, slot).Scan(&mediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreFirmware{Slot: slot}, nil
	}
	if err != nil {
		return CoreFirmware{}, fmt.Errorf("read household firmware: %w", err)
	}
	media, err := s.CoreMediaInfo(ctx, mediaID)
	if err != nil {
		return CoreFirmware{}, err
	}
	return CoreFirmware{Slot: slot, MediaID: media.MediaID, Size: media.Size}, nil
}

// HouseholdFirmwareFilled reports whether the Coleco firmware slot has a verified object.
func (s *Store) HouseholdFirmwareFilled(ctx context.Context) (bool, error) {
	firmware, err := s.CoreFirmware(ctx, protocol.FirmwareRole)
	if err != nil {
		return false, err
	}
	return firmware.MediaID != "" && firmware.Size == protocol.FirmwareBytes, nil
}

// SelectCoreFirmware binds or clears the household firmware slot. Empty mediaID clears.
func (s *Store) SelectCoreFirmware(ctx context.Context, slot, mediaID string) (CoreFirmware, error) {
	if slot != protocol.FirmwareRole {
		return CoreFirmware{}, fmt.Errorf("%w: household firmware slot must be %q", ErrInvalidCoreFirmware, protocol.FirmwareRole)
	}
	if mediaID == "" {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM core_firmware WHERE slot = ?`, slot); err != nil {
			return CoreFirmware{}, fmt.Errorf("clear household firmware: %w", err)
		}
		return CoreFirmware{Slot: slot}, nil
	}
	if protocol.ValidateDigest(mediaID) != nil {
		return CoreFirmware{}, fmt.Errorf("%w: invalid firmware media ID", ErrInvalidCoreFirmware)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreFirmware{}, fmt.Errorf("begin household firmware selection: %w", err)
	}
	defer tx.Rollback()
	media, err := verifyCoreMedia(ctx, tx, mediaID, io.Discard)
	if err != nil {
		return CoreFirmware{}, err
	}
	if media.Size != protocol.FirmwareBytes {
		return CoreFirmware{}, fmt.Errorf("%w: Coleco firmware must be exactly %d bytes", ErrInvalidCoreFirmware, protocol.FirmwareBytes)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO core_firmware(slot, media_id) VALUES (?, ?) ON CONFLICT(slot) DO UPDATE SET media_id = excluded.media_id`, slot, mediaID); err != nil {
		return CoreFirmware{}, fmt.Errorf("store household firmware: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CoreFirmware{}, fmt.Errorf("commit household firmware: %w", err)
	}
	return CoreFirmware{Slot: slot, MediaID: media.MediaID, Size: media.Size}, nil
}
