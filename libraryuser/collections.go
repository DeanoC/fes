package libraryuser

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/DeanoC/FogCast-POC/protocol"
)

const (
	maxCollectionIDLen   = 64
	maxCollectionNameLen = 80
)

var (
	ErrNotFound   = errors.New("user library item was not found")
	ErrInvalid    = errors.New("user library value is invalid")
	ErrReservedID = errors.New("collection id is reserved")

	collectionIDPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	reservedCollectionIDs = map[string]struct{}{
		"all":            {},
		"favorites":      {},
		"recents":        {},
		"continue":       {},
		"unplayed":       {},
		"recently_added": {},
		"recently-added": {},
	}
)

type Collection struct {
	ID        string
	Name      string
	CreatedAt int64
}

func IsReservedCollectionID(id string) bool {
	_, reserved := reservedCollectionIDs[id]
	return reserved
}

func ValidateCollectionID(id string) error {
	if !collectionIDPattern.MatchString(id) || len(id) > maxCollectionIDLen {
		return fmt.Errorf("%w: collection ID %q must be a lowercase ASCII slug", ErrInvalid, id)
	}
	if IsReservedCollectionID(id) {
		return fmt.Errorf("%w: %s", ErrReservedID, id)
	}
	return nil
}

func ValidateCollectionName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) {
		return fmt.Errorf("%w: collection name is required", ErrInvalid)
	}
	if utf8.RuneCountInString(name) > maxCollectionNameLen {
		return fmt.Errorf("%w: collection name is too long", ErrInvalid)
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return fmt.Errorf("%w: collection name contains control characters", ErrInvalid)
		}
	}
	return nil
}

func (s *Store) UpsertCollection(ctx context.Context, id, name string) (Collection, error) {
	if err := ValidateCollectionID(id); err != nil {
		return Collection{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = id
	}
	if err := ValidateCollectionName(name); err != nil {
		return Collection{}, err
	}
	now := time.Now().UnixNano()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO collections(id, name, created_at)
		VALUES(?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name`,
		id, name, now,
	)
	if err != nil {
		return Collection{}, fmt.Errorf("upsert collection: %w", err)
	}
	return s.Collection(ctx, id)
}

func (s *Store) Collection(ctx context.Context, id string) (Collection, error) {
	if err := ValidateCollectionID(id); err != nil {
		return Collection{}, err
	}
	var collection Collection
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, created_at FROM collections WHERE id = ?`, id,
	).Scan(&collection.ID, &collection.Name, &collection.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return Collection{}, ErrNotFound
		}
		return Collection{}, fmt.Errorf("read collection: %w", err)
	}
	return collection, nil
}

func (s *Store) Collections(ctx context.Context) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at FROM collections
		ORDER BY created_at ASC, id`)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close()
	collections := make([]Collection, 0)
	for rows.Next() {
		var collection Collection
		if err := rows.Scan(&collection.ID, &collection.Name, &collection.CreatedAt); err != nil {
			return nil, err
		}
		collections = append(collections, collection)
	}
	return collections, rows.Err()
}

func (s *Store) DeleteCollection(ctx context.Context, id string) error {
	if err := ValidateCollectionID(id); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM collections WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetCollectionMember(ctx context.Context, collectionID, gameID string, member bool) error {
	if err := ValidateCollectionID(collectionID); err != nil {
		return err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	if _, err := s.Collection(ctx, collectionID); err != nil {
		return err
	}
	if !member {
		_, err := s.db.ExecContext(ctx, `
			DELETE FROM collection_membership
			WHERE collection_id = ? AND game_id = ?`,
			collectionID, gameID,
		)
		if err != nil {
			return fmt.Errorf("remove collection member: %w", err)
		}
		return nil
	}
	now := time.Now().UnixNano()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO collection_membership(collection_id, game_id, added_at)
		VALUES(?, ?, ?)
		ON CONFLICT(collection_id, game_id) DO NOTHING`,
		collectionID, gameID, now,
	)
	if err != nil {
		return fmt.Errorf("add collection member: %w", err)
	}
	return nil
}

func (s *Store) CollectionGameIDs(ctx context.Context, collectionID string) ([]string, error) {
	if err := ValidateCollectionID(collectionID); err != nil {
		return nil, err
	}
	if _, err := s.Collection(ctx, collectionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT game_id FROM collection_membership
		WHERE collection_id = ?
		ORDER BY added_at DESC, game_id`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list collection games: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (s *Store) GameCollectionIDs(ctx context.Context, gameID string) ([]string, error) {
	if err := protocol.ValidateGameID(gameID); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT collection_id FROM collection_membership
		WHERE game_id = ?
		ORDER BY collection_id`,
		gameID,
	)
	if err != nil {
		return nil, fmt.Errorf("list game collections: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (s *Store) CollectionIDsByGame(ctx context.Context, ids []string) (map[string][]string, error) {
	result := make(map[string][]string, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT game_id, collection_id FROM collection_membership
		WHERE game_id IN (`+placeholders+`)
		ORDER BY game_id, collection_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list game collection membership: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var gameID, collectionID string
		if err := rows.Scan(&gameID, &collectionID); err != nil {
			return nil, err
		}
		result[gameID] = append(result[gameID], collectionID)
	}
	return result, rows.Err()
}
