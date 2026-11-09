package catalog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DeanoC/FogCast/protocol"
)

// KitHostEntry returns a previously generated entry for the given identity.
func (s *Store) KitHostEntry(ctx context.Context, identity string) (CoreEntry, bool, error) {
	if protocol.ValidateDigest(identity) != nil {
		return CoreEntry{}, false, ErrInvalidCoreEntry
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT game_id FROM kit_host_entries WHERE identity = ?`, identity).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntry{}, false, nil
	}
	if err != nil {
		return CoreEntry{}, false, err
	}
	entry, err := s.CoreEntry(ctx, id)
	return entry, err == nil, err
}

// ForgetKitHostEntry removes only the mapping still pointing to gameID.
func (s *Store) ForgetKitHostEntry(ctx context.Context, identity, gameID string) error {
	if protocol.ValidateDigest(identity) != nil || protocol.ValidateGameID(gameID) != nil {
		return ErrInvalidCoreEntry
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM kit_host_entries WHERE identity = ? AND game_id = ?`, identity, gameID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrCoreEntryConflict
	}
	return nil
}

// KitHostPlaySource maps a generated cartridge helper to its scanned title.
// An empty source means a generated bare core has no ordinary library row.
func (s *Store) KitHostPlaySource(ctx context.Context, gameID string) (string, bool, error) {
	var source string
	err := s.db.QueryRowContext(ctx, `SELECT source_game_id FROM kit_host_entries WHERE game_id = ?`, gameID).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return source, err == nil, err
}

// DetachKitHostEntry makes a helper a normal user entry before a settings edit.
func (s *Store) DetachKitHostEntry(ctx context.Context, gameID string) error {
	if protocol.ValidateGameID(gameID) != nil {
		return ErrInvalidCoreEntry
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM kit_host_entries WHERE game_id = ?`, gameID)
	return err
}
