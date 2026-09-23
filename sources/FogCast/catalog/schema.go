package catalog

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

const schemaVersion = 11

const schemaV8 = `
CREATE TABLE core_media_chunks (
  media_id TEXT NOT NULL REFERENCES core_media(media_id) ON DELETE CASCADE,
  chunk_index INTEGER NOT NULL,
  data BLOB NOT NULL,
  PRIMARY KEY(media_id, chunk_index)
);
PRAGMA user_version = 8;
`

// This historical asset is used only by the schema 7 migration. New entries
// always require an explicit media selection, including new Coleco entries.
//
//go:embed seeds/coleco-controllers.hex
var legacyColecoMediaHex string

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

const schemaV2 = `
ALTER TABLE games ADD COLUMN search_text TEXT NOT NULL DEFAULT '';
UPDATE games SET search_text = lower(title) || ' ' || lower(game_id) || ' ' || lower(system);
CREATE INDEX games_system_title_id ON games(system, lower(title), game_id);
CREATE VIRTUAL TABLE games_fts USING fts5(
  search_text,
  content='games',
  content_rowid='rowid',
  tokenize='unicode61'
);
INSERT INTO games_fts(rowid, search_text) SELECT rowid, search_text FROM games;
CREATE TRIGGER games_ai AFTER INSERT ON games BEGIN
  INSERT INTO games_fts(rowid, search_text) VALUES (new.rowid, new.search_text);
END;
CREATE TRIGGER games_ad AFTER DELETE ON games BEGIN
  INSERT INTO games_fts(games_fts, rowid, search_text) VALUES('delete', old.rowid, old.search_text);
END;
CREATE TRIGGER games_au AFTER UPDATE ON games BEGIN
  INSERT INTO games_fts(games_fts, rowid, search_text) VALUES('delete', old.rowid, old.search_text);
  INSERT INTO games_fts(rowid, search_text) VALUES (new.rowid, new.search_text);
END;
PRAGMA user_version = 2;
`

const schemaV3 = `
DROP INDEX IF EXISTS games_system_title_id;
CREATE INDEX games_system_title_id ON games(system, lower(title), game_id);
PRAGMA user_version = 3;
`

const schemaV4 = `
ALTER TABLE games ADD COLUMN canonical_title TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN region TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN revision TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN dump_flags TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN group_key TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN first_seen_ns INTEGER NOT NULL DEFAULT 0;
ALTER TABLE games ADD COLUMN genre TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN year TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN search_aliases TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS games_group_key ON games(group_key, game_id);
CREATE INDEX IF NOT EXISTS games_region ON games(region, group_key);
CREATE INDEX IF NOT EXISTS games_first_seen ON games(first_seen_ns DESC, game_id);
CREATE INDEX IF NOT EXISTS games_genre ON games(genre);
CREATE INDEX IF NOT EXISTS games_year ON games(year);
PRAGMA user_version = 4;
`

const schemaV5 = `
CREATE TABLE core_entries (
  game_id TEXT PRIMARY KEY REFERENCES games(game_id) ON DELETE CASCADE,
  core_id TEXT NOT NULL UNIQUE,
  package_id TEXT NOT NULL
);
PRAGMA user_version = 5;
`

const schemaV6 = `
DROP TRIGGER IF EXISTS games_au;
CREATE TRIGGER games_au AFTER UPDATE ON games
WHEN new.search_text IS NOT old.search_text
BEGIN
  INSERT INTO games_fts(games_fts, rowid, search_text) VALUES('delete', old.rowid, old.search_text);
  INSERT INTO games_fts(rowid, search_text) VALUES (new.rowid, new.search_text);
END;
PRAGMA user_version = 6;
`

const schemaV7 = `
CREATE TABLE core_media (
  media_id TEXT PRIMARY KEY,
  size INTEGER NOT NULL,
  data BLOB NOT NULL
);
CREATE TABLE core_entries_v7 (
  game_id TEXT PRIMARY KEY REFERENCES games(game_id) ON DELETE CASCADE,
  core_id TEXT NOT NULL,
  package_id TEXT NOT NULL,
  media_role TEXT NOT NULL DEFAULT '',
  media_id TEXT NOT NULL DEFAULT '',
  CHECK ((media_role = '' AND media_id = '') OR (media_role = 'blob' AND length(media_id) = 64))
);
INSERT INTO core_entries_v7(game_id, core_id, package_id)
  SELECT game_id, core_id, package_id FROM core_entries;
DROP TABLE core_entries;
ALTER TABLE core_entries_v7 RENAME TO core_entries;
CREATE INDEX core_entries_core_id ON core_entries(core_id);
PRAGMA user_version = 7;
`

const schemaV9 = `
ALTER TABLE core_entries ADD COLUMN firmware_required INTEGER NOT NULL DEFAULT 0;
CREATE TABLE core_firmware (
  slot TEXT PRIMARY KEY,
  media_id TEXT NOT NULL REFERENCES core_media(media_id),
  CHECK (slot = 'firmware')
);
PRAGMA user_version = 9;
`

const schemaV10 = `
CREATE TABLE core_expansions (
  expansion_id TEXT PRIMARY KEY,
  media_id TEXT NOT NULL REFERENCES core_media(media_id),
  shell_package_id TEXT NOT NULL
);
CREATE TABLE core_entry_expansions (
  game_id TEXT PRIMARY KEY REFERENCES core_entries(game_id) ON DELETE CASCADE,
  expansion_id TEXT NOT NULL REFERENCES core_expansions(expansion_id)
);
PRAGMA user_version = 10;
`

const schemaV11 = `
CREATE TABLE core_entry_roms (
  game_id TEXT PRIMARY KEY REFERENCES core_entries(game_id) ON DELETE CASCADE,
  package_id TEXT NOT NULL,
  rom_id TEXT NOT NULL,
  media_id TEXT NOT NULL REFERENCES core_media(media_id),
  source_size INTEGER NOT NULL CHECK(source_size > 0)
);
PRAGMA user_version = 11;
`

func migrateCoreMedia(ctx context.Context, connection *sql.Conn) error {
	if _, err := connection.ExecContext(ctx, schemaV7); err != nil {
		return err
	}
	data, err := hex.DecodeString(strings.Join(strings.Fields(legacyColecoMediaHex), ""))
	if err != nil {
		return fmt.Errorf("decode legacy Coleco media: %w", err)
	}
	media, err := coreMediaIdentity(data)
	if err != nil {
		return err
	}
	if media.MediaID != "ef9443c2787cd02b6d78d233d015b0bbf3fb21d53d1a5890497cdbb7897f053c" {
		return fmt.Errorf("%w: legacy Coleco seed digest mismatch", ErrInvalidCoreMedia)
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO core_media(media_id, size, data) VALUES (?, ?, ?)`, media.MediaID, media.Size, data); err != nil {
		return err
	}
	_, err = connection.ExecContext(ctx, `UPDATE core_entries SET media_role = 'blob', media_id = ? WHERE core_id = 'fes.coleco'`, media.MediaID)
	return err
}

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
		version = 1
	}
	if version == 1 {
		if _, err := connection.ExecContext(ctx, schemaV2); err != nil {
			return fmt.Errorf("apply catalog schema version 2: %w", err)
		}
		version = 2
	}
	if version == 2 {
		if _, err := connection.ExecContext(ctx, schemaV3); err != nil {
			return fmt.Errorf("apply catalog schema version 3: %w", err)
		}
		if err := rewriteSearchText(ctx, connection); err != nil {
			return fmt.Errorf("fold catalog search text: %w", err)
		}
		version = 3
	}
	if version == 3 {
		if _, err := connection.ExecContext(ctx, schemaV4); err != nil {
			return fmt.Errorf("apply catalog schema version 4: %w", err)
		}
		if err := rewriteDumpFields(ctx, connection); err != nil {
			return fmt.Errorf("backfill catalog dump fields: %w", err)
		}
		version = 4
	}
	if version == 4 {
		if _, err := connection.ExecContext(ctx, schemaV5); err != nil {
			return fmt.Errorf("apply catalog schema version 5: %w", err)
		}
		version = 5
	}
	if version == 5 {
		if _, err := connection.ExecContext(ctx, schemaV6); err != nil {
			return fmt.Errorf("apply catalog schema version 6: %w", err)
		}
		version = 6
	}
	if version == 6 {
		if err := migrateCoreMedia(ctx, connection); err != nil {
			return fmt.Errorf("apply catalog schema version 7: %w", err)
		}
		version = 7
	}
	if version == 7 {
		if _, err := connection.ExecContext(ctx, schemaV8); err != nil {
			return fmt.Errorf("apply catalog schema version 8: %w", err)
		}
		version = 8
	}
	if version == 8 {
		if _, err := connection.ExecContext(ctx, schemaV9); err != nil {
			return fmt.Errorf("apply catalog schema version 9: %w", err)
		}
		version = 9
	}
	if version == 9 {
		if _, err := connection.ExecContext(ctx, schemaV10); err != nil {
			return fmt.Errorf("apply catalog schema version 10: %w", err)
		}
		version = 10
	}
	if version == 10 {
		if _, err := connection.ExecContext(ctx, schemaV11); err != nil {
			return fmt.Errorf("apply catalog schema version 11: %w", err)
		}
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit catalog migration: %w", err)
	}
	return nil
}

func rewriteSearchText(ctx context.Context, connection *sql.Conn) error {
	rows, err := connection.QueryContext(ctx, `SELECT game_id, title, system FROM games`)
	if err != nil {
		return err
	}
	type row struct {
		id, title, system string
	}
	games := make([]row, 0)
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.title, &item.system); err != nil {
			_ = rows.Close()
			return err
		}
		games = append(games, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, game := range games {
		if _, err := connection.ExecContext(ctx, `UPDATE games SET search_text = ? WHERE game_id = ?`,
			searchDocument(game.id, game.title, protocol.System(game.system)), game.id); err != nil {
			return err
		}
	}
	return nil
}

func rewriteDumpFields(ctx context.Context, connection *sql.Conn) error {
	rows, err := connection.QueryContext(ctx, `SELECT game_id, title, system, search_aliases, modified_ns FROM games`)
	if err != nil {
		return err
	}
	type row struct {
		id, title, system, aliases string
		modified                   int64
	}
	games := make([]row, 0)
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.title, &item.system, &item.aliases, &item.modified); err != nil {
			_ = rows.Close()
			return err
		}
		games = append(games, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, game := range games {
		dump := ParseDump(game.title)
		system := protocol.System(game.system)
		aliases := SeededSearchAliases(dump.CanonicalTitle, game.title, game.aliases)
		if _, err := connection.ExecContext(ctx, `
			UPDATE games SET canonical_title = ?, region = ?, revision = ?, dump_flags = ?, group_key = ?,
			  first_seen_ns = CASE WHEN first_seen_ns = 0 THEN ? ELSE first_seen_ns END,
			  search_aliases = ?, search_text = ?
			WHERE game_id = ?`,
			dump.CanonicalTitle, dump.Region, dump.Revision, dump.FlagString(), GroupKey(system, dump.CanonicalTitle),
			game.modified, aliases, dumpSearchDocument(game.id, game.title, dump.CanonicalTitle, aliases, system), game.id,
		); err != nil {
			return err
		}
	}
	return nil
}
