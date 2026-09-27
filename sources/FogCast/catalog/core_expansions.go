package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

var ErrCoreExpansionNotFound = errors.New("catalog expansion not found")
var ErrInvalidCoreExpansion = errors.New("catalog expansion is invalid")

type CoreExpansion struct {
	ExpansionID string `json:"expansion_id"`
	PackageID   string `json:"package_id"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	// Slot is the physical socket of a multi-socket card; zero is omitted for
	// single-socket expansions.
	Slot int `json:"slot,omitempty"`
}

// CoreExpansionPresentation is household-authored copy for an exact immutable
// expansion. It never grants compatibility or changes the archive identity.
type CoreExpansionPresentation struct {
	ExpansionID string `json:"expansion_id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type CoreEntryExpansion struct {
	GameID      string `json:"game_id"`
	ExpansionID string `json:"expansion_id,omitempty"`
}

// ImportCoreExpansion uses the existing chunked immutable byte store. An
// expansion has its own identity and selection; it is never a media role.
func (s *Store) ImportCoreExpansion(ctx context.Context, asset expansion.Asset) (CoreExpansion, error) {
	var archive bytes.Buffer
	if err := asset.Write(&archive); err != nil {
		return CoreExpansion{}, fmt.Errorf("%w: %v", ErrInvalidCoreExpansion, err)
	}
	object, _, err := s.ImportCoreMediaStream(ctx, int64(archive.Len()), &archive)
	if err != nil {
		return CoreExpansion{}, err
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO core_expansions(expansion_id,media_id,shell_package_id,slot_index) VALUES(?,?,?,?) ON CONFLICT(expansion_id) DO NOTHING`, asset.ID, object.MediaID, asset.Manifest.ShellPackageID, asset.Manifest.SlotIndex); err != nil {
		return CoreExpansion{}, err
	}
	return CoreExpansion{ExpansionID: asset.ID, PackageID: asset.Manifest.ShellPackageID, Slot: asset.Manifest.SlotIndex}, nil
}

func (s *Store) CoreExpansions(ctx context.Context) ([]CoreExpansion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT expansion_id,shell_package_id,slot_index,label,description FROM core_expansions ORDER BY expansion_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CoreExpansion{}
	for rows.Next() {
		var row CoreExpansion
		if err = rows.Scan(&row.ExpansionID, &row.PackageID, &row.Slot, &row.Label, &row.Description); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Store) CoreExpansionPresentation(ctx context.Context, id string) (CoreExpansionPresentation, error) {
	if protocol.ValidateDigest(id) != nil {
		return CoreExpansionPresentation{}, ErrInvalidCoreExpansion
	}
	value := CoreExpansionPresentation{ExpansionID: id}
	err := s.db.QueryRowContext(ctx, `SELECT label,description FROM core_expansions WHERE expansion_id=?`, id).Scan(&value.Label, &value.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreExpansionPresentation{}, ErrCoreExpansionNotFound
	}
	return value, err
}

func (s *Store) SetCoreExpansionPresentation(ctx context.Context, id, label, description string) (CoreExpansionPresentation, error) {
	label, description = strings.TrimSpace(label), strings.TrimSpace(description)
	if protocol.ValidateDigest(id) != nil || !validExpansionCopy(label, 120, false) || !validExpansionCopy(description, 2000, true) {
		return CoreExpansionPresentation{}, ErrInvalidCoreExpansion
	}
	result, err := s.db.ExecContext(ctx, `UPDATE core_expansions SET label=?,description=? WHERE expansion_id=?`, label, description, id)
	if err != nil {
		return CoreExpansionPresentation{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return CoreExpansionPresentation{}, err
	}
	if changed != 1 {
		return CoreExpansionPresentation{}, ErrCoreExpansionNotFound
	}
	return CoreExpansionPresentation{ExpansionID: id, Label: label, Description: description}, nil
}

func validExpansionCopy(value string, limit int, multiline bool) bool {
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && !(multiline && r == '\n') {
			return false
		}
	}
	return true
}

func (s *Store) ReadCoreExpansion(ctx context.Context, id string) (expansion.Asset, error) {
	if protocol.ValidateDigest(id) != nil {
		return expansion.Asset{}, ErrInvalidCoreExpansion
	}
	var mediaID, packageID string
	var slot int
	err := s.db.QueryRowContext(ctx, `SELECT media_id,shell_package_id,slot_index FROM core_expansions WHERE expansion_id=?`, id).Scan(&mediaID, &packageID, &slot)
	if errors.Is(err, sql.ErrNoRows) {
		return expansion.Asset{}, ErrCoreExpansionNotFound
	}
	if err != nil {
		return expansion.Asset{}, err
	}
	_, reader, err := s.OpenCoreMedia(ctx, mediaID)
	if err != nil {
		return expansion.Asset{}, err
	}
	defer reader.Close()
	asset, err := expansion.ReadAsset(reader)
	if err != nil || asset.ID != id || asset.Manifest.ShellPackageID != packageID || asset.Manifest.SlotIndex != slot {
		return expansion.Asset{}, ErrInvalidCoreExpansion
	}
	return asset, nil
}

func (s *Store) CoreEntryExpansion(ctx context.Context, gameID string) (CoreEntryExpansion, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return CoreEntryExpansion{}, ErrInvalidCoreEntry
	}
	result := CoreEntryExpansion{GameID: gameID}
	err := s.db.QueryRowContext(ctx, `SELECT expansion_id FROM core_entry_expansions WHERE game_id=?`, gameID).Scan(&result.ExpansionID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	return result, err
}

// SelectCoreEntryExpansion binds a cart to an exact title/package selection.
// Empty expansionID clears it. Concurrent package/expansion changes conflict.
func (s *Store) SelectCoreEntryExpansion(ctx context.Context, gameID, packageID, expected, expansionID string) (CoreEntryExpansion, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(packageID) != nil ||
		(expected != "" && protocol.ValidateDigest(expected) != nil) || (expansionID != "" && protocol.ValidateDigest(expansionID) != nil) {
		return CoreEntryExpansion{}, ErrInvalidCoreExpansion
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntryExpansion{}, err
	}
	defer tx.Rollback()
	var selected string
	if err = tx.QueryRowContext(ctx, `SELECT package_id FROM core_entries WHERE game_id=?`, gameID).Scan(&selected); errors.Is(err, sql.ErrNoRows) {
		return CoreEntryExpansion{}, ErrCoreEntryNotFound
	} else if err != nil {
		return CoreEntryExpansion{}, err
	}
	if selected != packageID {
		return CoreEntryExpansion{}, ErrCoreEntryConflict
	}
	current := ""
	err = tx.QueryRowContext(ctx, `SELECT expansion_id FROM core_entry_expansions WHERE game_id=?`, gameID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CoreEntryExpansion{}, err
	}
	if current != expected {
		return CoreEntryExpansion{}, ErrCoreEntryConflict
	}
	if expansionID == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM core_entry_expansions WHERE game_id=?`, gameID)
	} else {
		var shell string
		if err = tx.QueryRowContext(ctx, `SELECT shell_package_id FROM core_expansions WHERE expansion_id=?`, expansionID).Scan(&shell); errors.Is(err, sql.ErrNoRows) {
			return CoreEntryExpansion{}, ErrCoreExpansionNotFound
		} else if err != nil {
			return CoreEntryExpansion{}, err
		}
		if shell != packageID {
			return CoreEntryExpansion{}, ErrInvalidCoreExpansion
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO core_entry_expansions(game_id,expansion_id) VALUES(?,?) ON CONFLICT(game_id) DO UPDATE SET expansion_id=excluded.expansion_id`, gameID, expansionID)
	}
	if err != nil {
		return CoreEntryExpansion{}, err
	}
	if err = tx.Commit(); err != nil {
		return CoreEntryExpansion{}, err
	}
	return CoreEntryExpansion{GameID: gameID, ExpansionID: expansionID}, nil
}
