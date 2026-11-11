package catalogcheckpoint

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
)

func raw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, path, table string) int {
	t.Helper()
	db := raw(t, path)
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func mustInspect(t *testing.T, path string) Metadata {
	t.Helper()
	got, err := Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func realFixture(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "library.sqlite3")
	store, err := catalog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := store.CreateCoreEntry(ctx, "Original user title", "fes.zx81", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	rom, _, err := store.ImportCoreMedia(ctx, bytes.Repeat([]byte{0x5a}, 8192))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "machine-rom", rom.Size, "", rom.MediaID); err != nil {
		t.Fatal(err)
	}
	media, _, err := store.ImportCoreMedia(ctx, []byte("original cassette bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", "cassette", media.MediaID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the schema-21 format, including its single-ROM table and
	// household pointer, before exercising the real migrations to schema 23.
	db := raw(t, path)
	mustExec(t, db, `DROP TRIGGER remote_core_entry_selected; DROP TABLE remote_core_entries;
DROP TRIGGER kit_host_rom_insert; DROP TRIGGER kit_host_rom_update; DROP TRIGGER kit_host_rom_delete;
CREATE TABLE old_roms (game_id TEXT PRIMARY KEY REFERENCES core_entries(game_id) ON DELETE CASCADE, package_id TEXT NOT NULL, rom_id TEXT NOT NULL, media_id TEXT NOT NULL REFERENCES core_media(media_id), source_size INTEGER NOT NULL CHECK(source_size > 0));
INSERT INTO old_roms SELECT game_id,package_id,rom_id,media_id,source_size FROM core_entry_roms;
DROP TABLE core_entry_roms; ALTER TABLE old_roms RENAME TO core_entry_roms;
CREATE TRIGGER kit_host_rom_insert AFTER INSERT ON core_entry_roms BEGIN DELETE FROM kit_host_entries WHERE game_id=NEW.game_id; END;
CREATE TRIGGER kit_host_rom_update AFTER UPDATE ON core_entry_roms BEGIN DELETE FROM kit_host_entries WHERE game_id=NEW.game_id; END;
CREATE TRIGGER kit_host_rom_delete AFTER DELETE ON core_entry_roms BEGIN DELETE FROM kit_host_entries WHERE game_id=OLD.game_id; END;
CREATE TABLE core_firmware (slot TEXT PRIMARY KEY, media_id TEXT NOT NULL REFERENCES core_media(media_id), CHECK(slot='firmware'));
PRAGMA user_version=21`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return root, path, entry.GameID
}

func upgrade(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()
	store, err := catalog.Open(path) // Run the actual 21→23 migrations.
	if err != nil {
		t.Fatal(err)
	}
	media, _, err := store.ImportCoreMedia(ctx, []byte("newer remotely cached cassette"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRemoteCoreEntry(ctx, "New remote title", "fes.zx81", strings.Repeat("a", 64), catalog.RemoteCoreEntryOrigin{SourceID: "remote-a", SourceGameID: "source-game", MediaID: media.MediaID}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeRestorePreservesOriginalAndRetainsNewerCatalog(t *testing.T) {
	ctx := context.Background()
	root, database, gameID := realFixture(t)
	originalEntries, originalMedia := count(t, database, "core_entries"), count(t, database, "core_media")
	for _, name := range []string{"config.toml", "library-user.sqlite3", "library-settings.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("unchanged "+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := Checkpoint(ctx, database, filepath.Join(root, "before"))
	if err != nil || before.Schema != 21 {
		t.Fatalf("checkpoint: %+v %v", before, err)
	}
	upgrade(t, database)
	newer := mustInspect(t, database)
	if newer.Schema != 23 || count(t, database, "core_entries") != originalEntries+1 || count(t, database, "core_media") != originalMedia+1 {
		t.Fatal("real upgraded catalog did not contain new title and media")
	}
	retained := filepath.Join(root, "retained")
	got, err := Restore(ctx, database, filepath.Join(root, "before"), retained, 23)
	if err != nil || got != before || mustInspect(t, database) != before {
		t.Fatalf("restore: %+v %v", got, err)
	}
	if count(t, database, "core_entries") != originalEntries || count(t, database, "core_media") != originalMedia || count(t, database, "core_entry_roms") != 1 {
		t.Fatal("restored original title, embedded firmware or cassette was lost")
	}
	db := raw(t, database)
	var title, media string
	if err := db.QueryRow("SELECT games.title, core_entries.media_id FROM core_entries JOIN games USING(game_id) WHERE game_id=?", gameID).Scan(&title, &media); err != nil || title != "Original user title" || media == "" {
		t.Fatalf("original selection: title=%q media=%q error=%v", title, media, err)
	}
	_ = db.Close()
	if saved, err := Load(ctx, retained); err != nil || saved != newer || count(t, filepath.Join(retained, databaseName), "remote_core_entries") != 1 || count(t, filepath.Join(retained, databaseName), "core_media") != originalMedia+1 {
		t.Fatalf("newer catalog retained: %+v %v", saved, err)
	}
	if replay, err := Restore(ctx, database, filepath.Join(root, "before"), retained, 23); err != nil || replay != before {
		t.Fatalf("replay after committed rename: %+v %v", replay, err)
	}
	for _, name := range []string{"config.toml", "library-user.sqlite3", "library-settings.json"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != "unchanged "+name {
			t.Fatalf("unrelated %s was changed: %q %v", name, got, err)
		}
	}
	// If a rollback's old software fails startup, that operation can recover
	// its own newer checkpoint without overwriting either earlier snapshot.
	if got, err := Restore(ctx, database, retained, filepath.Join(root, "recovery-retained"), 21); err != nil || got != newer || mustInspect(t, database) != newer {
		t.Fatalf("recover newer after rollback failure: %+v %v", got, err)
	}
}

func TestRestoreCopyFailureAndForeignStateDoNotReplaceLiveCatalog(t *testing.T) {
	ctx := context.Background()
	root, database, _ := realFixture(t)
	snapshot := filepath.Join(root, "before")
	if _, err := Checkpoint(ctx, database, snapshot); err != nil {
		t.Fatal(err)
	}
	upgrade(t, database)
	newer := mustInspect(t, database)
	retained := filepath.Join(root, "retained")
	copyFailure := errors.New("injected copy or file-sync failure")
	if _, err := restore(ctx, database, snapshot, retained, 23, func(_ context.Context, _, destination string, _ Metadata) error {
		if err := os.WriteFile(destination, []byte("partial copy"), 0o600); err != nil {
			t.Fatal(err)
		}
		return copyFailure
	}); !errors.Is(err, copyFailure) {
		t.Fatalf("copy failure: %v", err)
	}
	if got := mustInspect(t, database); got != newer {
		t.Fatal("failed copy changed live catalog")
	}
	if got, err := Load(ctx, retained); err != nil || got != newer {
		t.Fatal("newer catalog was not durably retained before the failed copy")
	}
	if leftovers, err := filepath.Glob(filepath.Join(root, ".catalog-restore-*")); err != nil || len(leftovers) != 0 {
		t.Fatalf("failed copy left staged files: %v %v", leftovers, err)
	}
	if _, err := Restore(ctx, database, snapshot, retained, 23); err != nil {
		t.Fatalf("retry after durable retain and failed live copy: %v", err)
	}
	// A third catalog written after the failed restore is foreign to that
	// operation and cannot be overwritten on retry.
	db := raw(t, database)
	mustExec(t, db, "UPDATE games SET title='Later user edit'")
	_ = db.Close()
	foreign := mustInspect(t, database)
	if _, err := Restore(ctx, database, snapshot, retained, 23); err == nil || mustInspect(t, database) != foreign {
		t.Fatal("retry replaced a later catalog")
	}
}

func TestRejectCorruptionWrongSchemaAndExistingCheckpoint(t *testing.T) {
	ctx := context.Background()
	root, database, _ := realFixture(t)
	snapshot := filepath.Join(root, "before")
	if _, err := Checkpoint(ctx, database, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkpoint(ctx, database, snapshot); err == nil {
		t.Fatal("checkpoint overwrote an existing snapshot")
	}
	upgrade(t, database)
	newer := mustInspect(t, database)
	if _, err := Restore(ctx, database, snapshot, filepath.Join(root, "wrong-schema"), 24); err == nil {
		t.Fatal("unexpected current schema was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "wrong-schema")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrong schema published retention state")
	}
	if err := os.WriteFile(filepath.Join(snapshot, databaseName), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, database, snapshot, filepath.Join(root, "corrupt"), 23); err == nil || mustInspect(t, database) != newer {
		t.Fatal("corrupt snapshot changed live catalog")
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkpoint(ctx, database, foreign); err == nil {
		t.Fatal("empty foreign destination was overwritten")
	}
}

func TestAbsentCatalogRestoresAbsenceAndRetainsCreatedData(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database := filepath.Join(root, "library.sqlite3")
	snapshot := filepath.Join(root, "before")
	if got, err := Checkpoint(ctx, database, snapshot); err != nil || got != (Metadata{}) {
		t.Fatalf("absent checkpoint: %+v %v", got, err)
	}
	store, err := catalog.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCoreEntry(ctx, "Created later", "fes.zx81", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	retained := filepath.Join(root, "retained")
	if got, err := Restore(ctx, database, snapshot, retained, 23); err != nil || got != (Metadata{}) {
		t.Fatalf("restore absence: %+v %v", got, err)
	}
	if _, err := os.Stat(database); !errors.Is(err, os.ErrNotExist) || count(t, filepath.Join(retained, databaseName), "core_entries") != 1 {
		t.Fatal("restore did not retain newly created catalog before restoring absence")
	}
	if _, err := Restore(ctx, database, snapshot, retained, 23); err != nil {
		t.Fatalf("absent restore replay: %v", err)
	}
}

func TestCommittedWALCheckpoint(t *testing.T) {
	if path := os.Getenv("FES_CATALOG_WAL_CHILD"); path != "" {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			os.Exit(2)
		}
		if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE committed(value TEXT); INSERT INTO committed VALUES ('in WAL'); PRAGMA user_version=21"); err != nil {
			os.Exit(3)
		}
		os.Exit(0) // Intentionally leave SQLite/WAL unclosed.
	}
	root := t.TempDir()
	database := filepath.Join(root, "library.sqlite3")
	command := exec.Command(os.Args[0], "-test.run=^TestCommittedWALCheckpoint$")
	command.Env = append(os.Environ(), "FES_CATALOG_WAL_CHILD="+database)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("WAL writer: %v %s", err, output)
	}
	if info, err := os.Stat(database + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("fixture has no committed WAL: %v", err)
	}
	snapshot := filepath.Join(root, "before")
	if got, err := Checkpoint(context.Background(), database, snapshot); err != nil || got.Schema != 21 || count(t, filepath.Join(snapshot, databaseName), "committed") != 1 {
		t.Fatalf("committed WAL was not saved: %+v %v", got, err)
	}
}

func TestBusyWALIsNotCopiedOrRemoved(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "library.sqlite3")
	db := raw(t, database)
	db.SetMaxOpenConns(2)
	mustExec(t, db, "PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE held(value TEXT); INSERT INTO held VALUES ('first'); PRAGMA user_version=21")
	reader, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	var value string
	if err := reader.QueryRow("SELECT value FROM held").Scan(&value); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO held VALUES ('committed while reader holds old snapshot')")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	snapshot := filepath.Join(root, "blocked")
	if _, err := Checkpoint(ctx, database, snapshot); err == nil {
		t.Fatal("busy WAL was accepted")
	}
	if info, err := os.Stat(database + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("busy committed WAL was discarded: %v", err)
	}
	if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("busy checkpoint published a snapshot")
	}
	if err := reader.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkpoint(context.Background(), database, snapshot); err != nil || count(t, filepath.Join(snapshot, databaseName), "held") != 2 {
		t.Fatalf("checkpoint after quiescence: %v", err)
	}
}

func TestSnapshotIntegrityCheckAndSymlinkRefusal(t *testing.T) {
	ctx := context.Background()
	root, database, _ := realFixture(t)
	snapshot := filepath.Join(root, "before")
	if _, err := Checkpoint(ctx, database, snapshot); err != nil {
		t.Fatal(err)
	}
	upgrade(t, database)
	newer := mustInspect(t, database)
	if err := os.WriteFile(filepath.Join(snapshot, databaseName), []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A matching digest alone cannot authorize restoring malformed SQLite.
	corrupt, err := fileMetadata(ctx, filepath.Join(snapshot, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	corrupt.Schema = 21
	if err := os.Remove(filepath.Join(snapshot, metadataName)); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(snapshot, metadataName), corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, database, snapshot, filepath.Join(root, "retain"), 23); err == nil || mustInspect(t, database) != newer {
		t.Fatal("matching digest allowed restoring an invalid SQLite file")
	}
	link := filepath.Join(root, "linked.sqlite3")
	if err := os.Symlink(database, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, link); err == nil {
		t.Fatal("catalog symlink was opened")
	}
	if err := os.Symlink(database, database+"-wal"); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, database); err == nil {
		t.Fatal("SQLite sidecar symlink was opened")
	}
}
