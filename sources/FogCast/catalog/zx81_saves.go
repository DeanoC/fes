package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// ZX81Save is one BASIC program captured from SAVE and kept for the cassette picker.
type ZX81Save struct {
	GameID    string
	MediaID   string
	Name      string
	Size      int64
	CreatedAt int64
}

// InsertZX81Save records a captured .p for a library machine. Saving the same
// bytes again keeps the first row.
func (s *Store) InsertZX81Save(ctx context.Context, gameID, mediaID, name string) error {
	name = strings.TrimSpace(name)
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(mediaID) != nil || name == "" || len(name) > 80 {
		return fmt.Errorf("%w: ZX81 save identity", ErrInvalidCoreMedia)
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO zx81_saves(game_id, media_id, name, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(game_id, media_id) DO NOTHING`,
		gameID, mediaID, name, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 1 {
		return nil
	}
	var existing string
	err = s.db.QueryRowContext(ctx, `SELECT media_id FROM zx81_saves WHERE game_id = ? AND media_id = ?`, gameID, mediaID).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: ZX81 save was not stored", ErrInvalidCoreMedia)
	}
	return err
}

// ListZX81Saves returns captured programs for one machine, oldest first.
func (s *Store) ListZX81Saves(ctx context.Context, gameID string) ([]ZX81Save, error) {
	if protocol.ValidateGameID(gameID) != nil {
		return nil, fmt.Errorf("%w: ZX81 save game", ErrInvalidCoreMedia)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.game_id, s.media_id, s.name, m.size, s.created_at
		FROM zx81_saves s
		JOIN core_media m ON m.media_id = s.media_id
		WHERE s.game_id = ?
		ORDER BY s.created_at, s.media_id`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var saves []ZX81Save
	for rows.Next() {
		var save ZX81Save
		if err := rows.Scan(&save.GameID, &save.MediaID, &save.Name, &save.Size, &save.CreatedAt); err != nil {
			return nil, err
		}
		saves = append(saves, save)
	}
	return saves, rows.Err()
}
