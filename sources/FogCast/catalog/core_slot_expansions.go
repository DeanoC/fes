package catalog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DeanoC/FogCast/protocol"
)

// CoreEntrySlotExpansion is the card selected for one physical slot of a
// multi-socket shell. A title selects at most one card per slot.
type CoreEntrySlotExpansion struct {
	Slot        int    `json:"slot"`
	ExpansionID string `json:"expansion_id"`
}

func validSlot(slot int) bool { return slot >= 1 && slot <= 7 }

// CoreEntrySlotExpansions lists a title's slot cards in ascending slot order.
func (s *Store) CoreEntrySlotExpansions(ctx context.Context, gameID string) ([]CoreEntrySlotExpansion, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return nil, ErrInvalidCoreEntry
	}
	return coreEntrySlotExpansions(ctx, s.db, gameID)
}

type slotQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func coreEntrySlotExpansions(ctx context.Context, db slotQuerier, gameID string) ([]CoreEntrySlotExpansion, error) {
	rows, err := db.QueryContext(ctx, `SELECT slot,expansion_id FROM core_entry_slot_expansions WHERE game_id=? ORDER BY slot`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CoreEntrySlotExpansion{}
	for rows.Next() {
		var row CoreEntrySlotExpansion
		if err = rows.Scan(&row.Slot, &row.ExpansionID); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// SelectCoreEntrySlotExpansion binds one card to a slot of an exact title and
// package selection, compare-and-swap on the slot's current card. An empty
// expansionID clears the slot. The card must be imported for this shell and
// built for this slot. It returns every slot selection after the change.
func (s *Store) SelectCoreEntrySlotExpansion(ctx context.Context, gameID, packageID string, slot int, expected, expansionID string) ([]CoreEntrySlotExpansion, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(packageID) != nil || !validSlot(slot) ||
		(expected != "" && protocol.ValidateDigest(expected) != nil) || (expansionID != "" && protocol.ValidateDigest(expansionID) != nil) {
		return nil, ErrInvalidCoreExpansion
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var selected string
	if err = tx.QueryRowContext(ctx, `SELECT package_id FROM core_entries WHERE game_id=?`, gameID).Scan(&selected); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCoreEntryNotFound
	} else if err != nil {
		return nil, err
	}
	if selected != packageID {
		return nil, ErrCoreEntryConflict
	}
	current := ""
	err = tx.QueryRowContext(ctx, `SELECT expansion_id FROM core_entry_slot_expansions WHERE game_id=? AND slot=?`, gameID, slot).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if current != expected {
		return nil, ErrCoreEntryConflict
	}
	if expansionID == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM core_entry_slot_expansions WHERE game_id=? AND slot=?`, gameID, slot)
	} else {
		var shell string
		var index int
		if err = tx.QueryRowContext(ctx, `SELECT shell_package_id,slot_index FROM core_expansions WHERE expansion_id=?`, expansionID).Scan(&shell, &index); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCoreExpansionNotFound
		} else if err != nil {
			return nil, err
		}
		if shell != packageID || index != slot {
			return nil, ErrInvalidCoreExpansion
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO core_entry_slot_expansions(game_id,slot,expansion_id) VALUES(?,?,?) ON CONFLICT(game_id,slot) DO UPDATE SET expansion_id=excluded.expansion_id`, gameID, slot, expansionID)
	}
	if err != nil {
		return nil, err
	}
	result, err := coreEntrySlotExpansions(ctx, tx, gameID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
