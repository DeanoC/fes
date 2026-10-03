package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

var (
	ErrCoreVideoPartNotFound    = errors.New("catalog video part not found")
	ErrInvalidCoreVideoPart     = errors.New("catalog video part is invalid")
	ErrCoreVideoProfileConflict = errors.New("catalog video profile already has a different part")
)

// CoreVideoPart maps a household profile to a part sealed for one exact shell.
// Profile is external selection metadata, not part of the immutable manifest.
type CoreVideoPart struct {
	PartID    string `json:"part_id"`
	PackageID string `json:"package_id"`
	Profile   string `json:"profile"`
}

func ValidVideoProfile(profile string) bool {
	return profile == "direct" || profile == "scanlines"
}

func validCoreVideoAsset(asset expansion.Asset) error {
	if err := asset.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCoreVideoPart, err)
	}
	m := asset.Manifest
	videoSocket := (m.Slot == expansion.VideoSlot && m.Map == expansion.ColecoVideoMap) ||
		(m.Slot == expansion.NativeVideoSlot && m.Map == expansion.ColecoNativeVideoMap)
	if !videoSocket ||
		m.SlotMajor != 1 || m.SlotMinor != 0 || m.SlotIndex != 0 || m.BoundaryPatch != nil {
		return fmt.Errorf("%w: requires the supported video socket 1.0", ErrInvalidCoreVideoPart)
	}
	return nil
}

// ImportCoreVideoPart stores a validated archive in the existing immutable
// chunk store. The service must verify composition against the installed shell
// before import and at launch; storage does not decode configuration frames.
// An existing shell/profile mapping cannot be implicitly replaced.
func (s *Store) ImportCoreVideoPart(ctx context.Context, asset expansion.Asset, profile string) (CoreVideoPart, error) {
	if err := ctx.Err(); err != nil {
		return CoreVideoPart{}, err
	}
	if !ValidVideoProfile(profile) {
		return CoreVideoPart{}, ErrInvalidCoreVideoPart
	}
	if err := validCoreVideoAsset(asset); err != nil {
		return CoreVideoPart{}, err
	}
	want := CoreVideoPart{PartID: asset.ID, PackageID: asset.Manifest.ShellPackageID, Profile: profile}
	if row, found, err := s.existingCoreVideoPart(ctx, want); err != nil || found {
		return row, err
	}
	var archive bytes.Buffer
	if err := asset.Write(&archive); err != nil {
		return CoreVideoPart{}, fmt.Errorf("%w: %w", ErrInvalidCoreVideoPart, err)
	}
	object, _, err := s.ImportCoreMediaStream(ctx, int64(archive.Len()), &archive)
	if err != nil {
		return CoreVideoPart{}, err
	}
	// Both unique constraints apply atomically. Recheck the winning mapping so
	// concurrent imports cannot turn a conflict into an apparent success.
	if _, err = s.db.ExecContext(ctx, `INSERT INTO core_video_parts(part_id,media_id,shell_package_id,profile) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, want.PartID, object.MediaID, want.PackageID, want.Profile); err != nil {
		return CoreVideoPart{}, err
	}
	row, found, err := s.existingCoreVideoPart(ctx, want)
	if err != nil {
		return CoreVideoPart{}, err
	}
	if !found {
		return CoreVideoPart{}, ErrInvalidCoreVideoPart
	}
	return row, nil
}

func (s *Store) coreVideoPart(ctx context.Context, id string) (CoreVideoPart, string, error) {
	row := CoreVideoPart{PartID: id}
	var mediaID string
	err := s.db.QueryRowContext(ctx, `SELECT media_id,shell_package_id,profile FROM core_video_parts WHERE part_id=?`, id).Scan(&mediaID, &row.PackageID, &row.Profile)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreVideoPart{}, "", ErrCoreVideoPartNotFound
	}
	if err != nil {
		return CoreVideoPart{}, "", err
	}
	if protocol.ValidateDigest(row.PackageID) != nil || protocol.ValidateDigest(mediaID) != nil || !ValidVideoProfile(row.Profile) {
		return CoreVideoPart{}, "", ErrInvalidCoreVideoPart
	}
	return row, mediaID, nil
}

func (s *Store) existingCoreVideoPart(ctx context.Context, want CoreVideoPart) (CoreVideoPart, bool, error) {
	row, _, err := s.coreVideoPart(ctx, want.PartID)
	if err == nil {
		if row.PackageID != want.PackageID {
			return CoreVideoPart{}, false, ErrInvalidCoreVideoPart
		}
		if row.Profile != want.Profile {
			return CoreVideoPart{}, false, ErrCoreVideoProfileConflict
		}
		if _, err := s.ReadCoreVideoPart(ctx, row.PartID); err != nil {
			return CoreVideoPart{}, false, err
		}
		return row, true, nil
	}
	if !errors.Is(err, ErrCoreVideoPartNotFound) {
		return CoreVideoPart{}, false, err
	}
	var id string
	err = s.db.QueryRowContext(ctx, `SELECT part_id FROM core_video_parts WHERE shell_package_id=? AND profile=?`, want.PackageID, want.Profile).Scan(&id)
	if err == nil {
		return CoreVideoPart{}, false, ErrCoreVideoProfileConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CoreVideoPart{}, false, err
	}
	return CoreVideoPart{}, false, nil
}

func (s *Store) CoreVideoParts(ctx context.Context) ([]CoreVideoPart, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT part_id,shell_package_id,profile FROM core_video_parts ORDER BY shell_package_id,profile,part_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CoreVideoPart{}
	for rows.Next() {
		var row CoreVideoPart
		if err := rows.Scan(&row.PartID, &row.PackageID, &row.Profile); err != nil {
			return nil, err
		}
		if protocol.ValidateDigest(row.PartID) != nil || protocol.ValidateDigest(row.PackageID) != nil || !ValidVideoProfile(row.Profile) {
			return nil, ErrInvalidCoreVideoPart
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Store) ReadCoreVideoPart(ctx context.Context, id string) (expansion.Asset, error) {
	if err := ctx.Err(); err != nil {
		return expansion.Asset{}, err
	}
	if protocol.ValidateDigest(id) != nil {
		return expansion.Asset{}, ErrInvalidCoreVideoPart
	}
	row, mediaID, err := s.coreVideoPart(ctx, id)
	if err != nil {
		return expansion.Asset{}, err
	}
	_, reader, err := s.OpenCoreMedia(ctx, mediaID)
	if err != nil {
		if errors.Is(err, ErrInvalidCoreMedia) || errors.Is(err, ErrCoreMediaNotFound) {
			return expansion.Asset{}, fmt.Errorf("%w: %w", ErrInvalidCoreVideoPart, err)
		}
		return expansion.Asset{}, err
	}
	defer reader.Close()
	asset, err := expansion.ReadAsset(coreMediaContextReader{ctx, reader})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return expansion.Asset{}, err
		}
		return expansion.Asset{}, fmt.Errorf("%w: %w", ErrInvalidCoreVideoPart, err)
	}
	if err := validCoreVideoAsset(asset); err != nil {
		return expansion.Asset{}, err
	}
	if asset.ID != row.PartID || asset.Manifest.ShellPackageID != row.PackageID {
		return expansion.Asset{}, ErrInvalidCoreVideoPart
	}
	return asset, nil
}
