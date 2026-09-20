package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DeanoC/FogCast/protocol"
)

const BuiltinPongID = "pong"
const BuiltinLibraryID = "builtin-pong"
const builtinRoot = "builtin:pong"
const SourceKindBuiltin SourceKind = "builtin"

// IsBuiltinPong admits only the registered media-free product. A caller cannot
// make a rooted game ROM-less by changing its source kind.
func IsBuiltinPong(game Game) bool {
	return game.ID == BuiltinPongID && game.System == protocol.SystemPong &&
		game.LibraryID == BuiltinLibraryID && game.Kind == SourceKindBuiltin &&
		game.RelativePath == "" && game.Content == nil && game.Fingerprint == (Fingerprint{}) &&
		game.State == SourceStateAvailable && game.RootOnline
}

// EnsureBuiltinPong adds the built-in product to the ordinary catalog query path.
// The collection URI is logical, never a filesystem root. No ROM or content
// identity is fabricated; normal scanning never visits this collection.
func (s *Store) EnsureBuiltinPong(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var system, root string
	err = tx.QueryRowContext(ctx, "SELECT system, root FROM libraries WHERE id = ?", BuiltinLibraryID).Scan(&system, &root)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && (system != string(protocol.SystemPong) || root != builtinRoot) {
		return fmt.Errorf("reserved built-in collection is occupied")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO libraries(id,system,root,online) VALUES(?,?,?,1)
 ON CONFLICT(id) DO UPDATE SET online=1`, BuiltinLibraryID, protocol.SystemPong, builtinRoot); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO games(game_id,library_id,system,relative_path,title,source_kind,source_state,source_size,modified_ns,seen_generation,search_text,canonical_title,group_key)
 VALUES(?,?,?,'','Pong','builtin','available',0,0,0,'pong','Pong','builtin:pong')
 ON CONFLICT(game_id) DO NOTHING`, BuiltinPongID, BuiltinLibraryID, protocol.SystemPong); err != nil {
		return err
	}
	return tx.Commit()
}
