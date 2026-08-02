package catalog

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 1

const schemaV1 = `
CREATE TABLE libraries (
  id TEXT PRIMARY KEY,
  system TEXT NOT NULL,
  root TEXT NOT NULL UNIQUE,
  online INTEGER NOT NULL,
  generation INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE games (
  game_id TEXT PRIMARY KEY,
  library_id TEXT NOT NULL REFERENCES libraries(id),
  system TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  title TEXT NOT NULL,
  source_kind TEXT NOT NULL,
  source_state TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  source_size INTEGER NOT NULL,
  modified_ns INTEGER NOT NULL,
  zip_member TEXT NOT NULL DEFAULT '',
  zip_size INTEGER NOT NULL DEFAULT 0,
  zip_crc32 INTEGER NOT NULL DEFAULT 0,
  zip_entry_count INTEGER NOT NULL DEFAULT 0,
  seen_generation INTEGER NOT NULL,
  content_sha256 TEXT,
  content_size INTEGER,
  content_extension TEXT,
  UNIQUE(library_id, relative_path)
);
PRAGMA user_version = 1;
`

func migrate(ctx context.Context, connection *sql.Conn) (err error) {
	if _, err := connection.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("begin catalog migration: %w", err)
	}
	defer func() {
		if err != nil {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	var version int
	if err := connection.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read catalog schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("catalog schema version %d is newer than supported version %d", version, schemaVersion)
	}
	if version == 0 {
		if _, err := connection.ExecContext(ctx, schemaV1); err != nil {
			return fmt.Errorf("apply catalog schema version 1: %w", err)
		}
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit catalog migration: %w", err)
	}
	return nil
}
