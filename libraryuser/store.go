package libraryuser

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
	_ "modernc.org/sqlite"
)

const schemaVersion = 1

const schemaV1 = `
CREATE TABLE game_state (
  game_id TEXT PRIMARY KEY,
  favorite INTEGER NOT NULL DEFAULT 0,
  favorited_at INTEGER,
  last_played_at INTEGER,
  play_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX game_state_favorites ON game_state(favorite, favorited_at DESC, game_id);
CREATE INDEX game_state_recents ON game_state(last_played_at DESC, game_id);
PRAGMA user_version = 1;
`

type State struct {
	GameID       string
	Favorite     bool
	FavoritedAt  int64
	LastPlayedAt int64
	PlayCount    int64
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	return OpenContext(context.Background(), path)
}

func OpenContext(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open user library: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	connection, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect user library: %w", err)
	}
	defer connection.Close()
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure user library: %w", err)
		}
	}
	if err := migrate(ctx, connection); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func migrate(ctx context.Context, connection *sql.Conn) (err error) {
	if _, err := connection.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("begin user library migration: %w", err)
	}
	defer func() {
		if err != nil {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	var version int
	if err := connection.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read user library schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("user library schema version %d is newer than supported version %d", version, schemaVersion)
	}
	if version == 0 {
		if _, err := connection.ExecContext(ctx, schemaV1); err != nil {
			return fmt.Errorf("apply user library schema: %w", err)
		}
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit user library migration: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) State(ctx context.Context, gameID string) (State, error) {
	if err := protocol.ValidateGameID(gameID); err != nil {
		return State{}, err
	}
	var state State
	var favorite int
	var favoritedAt, lastPlayed sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT game_id, favorite, favorited_at, last_played_at, play_count
		FROM game_state WHERE game_id = ?`, gameID,
	).Scan(&state.GameID, &favorite, &favoritedAt, &lastPlayed, &state.PlayCount)
	if err != nil {
		if err == sql.ErrNoRows {
			return State{GameID: gameID}, nil
		}
		return State{}, fmt.Errorf("read user library state: %w", err)
	}
	state.Favorite = favorite != 0
	state.FavoritedAt = favoritedAt.Int64
	state.LastPlayedAt = lastPlayed.Int64
	return state, nil
}

func (s *Store) SetFavorite(ctx context.Context, gameID string, favorite bool) error {
	if err := protocol.ValidateGameID(gameID); err != nil {
		return err
	}
	now := time.Now().UnixNano()
	if favorite {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO game_state(game_id, favorite, favorited_at)
			VALUES(?, 1, ?)
			ON CONFLICT(game_id) DO UPDATE SET favorite = 1, favorited_at = excluded.favorited_at`,
			gameID, now,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO game_state(game_id, favorite, favorited_at)
		VALUES(?, 0, NULL)
		ON CONFLICT(game_id) DO UPDATE SET favorite = 0, favorited_at = NULL`,
		gameID,
	)
	return err
}

func (s *Store) RecordPlay(ctx context.Context, gameID string) error {
	if err := protocol.ValidateGameID(gameID); err != nil {
		return err
	}
	now := time.Now().UnixNano()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO game_state(game_id, last_played_at, play_count)
		VALUES(?, ?, 1)
		ON CONFLICT(game_id) DO UPDATE SET
		  last_played_at = excluded.last_played_at,
		  play_count = game_state.play_count + 1`,
		gameID, now,
	)
	return err
}

func (s *Store) PlayedIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT game_id FROM game_state
		WHERE last_played_at IS NOT NULL
		ORDER BY game_id`)
	if err != nil {
		return nil, fmt.Errorf("list played games: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (s *Store) FavoriteIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT game_id FROM game_state
		WHERE favorite = 1
		ORDER BY favorited_at DESC, game_id`)
	if err != nil {
		return nil, fmt.Errorf("list favorites: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (s *Store) RecentIDs(ctx context.Context, limit int) ([]string, error) {
	query := `
		SELECT game_id FROM game_state
		WHERE last_played_at IS NOT NULL
		ORDER BY last_played_at DESC, game_id`
	args := make([]any, 0, 1)
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list recents: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (s *Store) States(ctx context.Context, ids []string) (map[string]State, error) {
	result := make(map[string]State, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT game_id, favorite, favorited_at, last_played_at, play_count
		FROM game_state WHERE game_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("list user library states: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var state State
		var favorite int
		var favoritedAt, lastPlayed sql.NullInt64
		if err := rows.Scan(&state.GameID, &favorite, &favoritedAt, &lastPlayed, &state.PlayCount); err != nil {
			return nil, err
		}
		state.Favorite = favorite != 0
		state.FavoritedAt = favoritedAt.Int64
		state.LastPlayedAt = lastPlayed.Int64
		result[state.GameID] = state
	}
	return result, rows.Err()
}

func scanIDs(rows *sql.Rows) ([]string, error) {
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
