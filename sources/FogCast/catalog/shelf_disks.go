package catalog

import (
	"context"
	"fmt"

	"github.com/DeanoC/FogCast/protocol"
)

// MarkShelfDisk records that a person made this library disk on the kit. The
// entry must exist and select a disk. Marking twice is harmless.
func (s *Store) MarkShelfDisk(ctx context.Context, gameID string) error {
	if protocol.ValidateGameID(gameID) != nil {
		return ErrInvalidCoreEntry
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO shelf_disks(game_id)
		SELECT game_id FROM core_entries WHERE game_id = ? AND media_role = ? AND media_id <> ''
		ON CONFLICT(game_id) DO NOTHING`, gameID, protocol.DiskRole)
	if err != nil {
		return fmt.Errorf("mark shelf disk: %w", err)
	}
	if changed, err := result.RowsAffected(); err == nil && changed == 0 {
		var marked int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM shelf_disks WHERE game_id = ?)`, gameID).Scan(&marked); err != nil {
			return fmt.Errorf("read shelf disk: %w", err)
		}
		if marked == 0 {
			return ErrCoreEntryNotFound
		}
	}
	return nil
}

// ShelfDisks returns the game IDs of one core's disks that were made on the
// kit.
func (s *Store) ShelfDisks(ctx context.Context, coreID string) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.game_id FROM shelf_disks d
		JOIN core_entries e ON e.game_id = d.game_id WHERE e.core_id = ?`, coreID)
	if err != nil {
		return nil, fmt.Errorf("list shelf disks: %w", err)
	}
	defer rows.Close()
	made := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read shelf disk: %w", err)
		}
		made[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list shelf disks: %w", err)
	}
	return made, nil
}
