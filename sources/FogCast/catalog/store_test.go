package catalog_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestStoreGamesAndSearchDoNotExposeAbsoluteRootBinding(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	privateRoot := "/games/private-token-library"
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: privateRoot}
	c := candidate("snes-game", "Private Token Game", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)

	for name, load := range map[string]func() ([]catalog.Game, error){
		"games":  func() ([]catalog.Game, error) { return store.Games(ctx) },
		"search": func() ([]catalog.Game, error) { return store.Search(ctx, "private token") },
	} {
		t.Run(name, func(t *testing.T) {
			games, err := load()
			if err != nil || len(games) != 1 {
				t.Fatalf("load = %+v, %v", games, err)
			}
			encoded, err := json.Marshal(games)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), privateRoot) || strings.Contains(fmt.Sprintf("%+v", games), privateRoot) {
				t.Fatalf("public game value exposed absolute root: json=%s value=%+v", encoded, games)
			}
		})
	}
}

func TestSchemaMigratesNewDatabaseAndRejectsFutureVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite3")
	store, err := catalog.Open(path)
	if err != nil {
		t.Fatalf("Open(new database): %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(new database): %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 10 {
		t.Fatalf("user_version = %d, want 10", version)
	}
	var triggerSQL string
	if err := db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name = 'games_au'").Scan(&triggerSQL); err != nil {
		t.Fatalf("read games_au: %v", err)
	}
	if !strings.Contains(triggerSQL, "new.search_text IS NOT old.search_text") {
		t.Fatalf("games_au = %q, want search_text WHEN clause", triggerSQL)
	}
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type IN ('table', 'index') AND name IN ('core_entries', 'games', 'libraries', 'games_fts', 'games_system_title_id') ORDER BY name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	if want := []string{"core_entries", "games", "games_fts", "games_system_title_id", "libraries"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("schema objects = %v, want %v", names, want)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 11"); err != nil {
		t.Fatalf("set future user_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close future database: %v", err)
	}
	if _, err := catalog.Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open(future database) error = %v, want newer-schema error", err)
	}
}

func TestOpenEscapesReservedCatalogPathCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog ?#%.sqlite3")
	store, err := catalog.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat catalog at literal path: %v", err)
	}
}

func TestSchemaIndexMatchesLowerTitleOrder(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite3")
	store, err := catalog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var indexSQL string
	if err := db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name = 'games_system_title_id'").Scan(&indexSQL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(indexSQL, "lower(title)") {
		t.Fatalf("index sql = %q, want lower(title) expression", indexSQL)
	}
}

func TestMigrateFoldsSearchTextForUnicodeQuery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
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
INSERT INTO libraries(id, system, root, online, generation) VALUES('snes-main', 'snes', '/games/snes', 1, 1);
INSERT INTO games(game_id, library_id, system, relative_path, title, source_kind, source_state, source_size, modified_ns, seen_generation)
VALUES('snes-strasse-test', 'snes-main', 'snes', 'strasse.sfc', 'Straße', 'raw', 'available', 1, 1, 1);
`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	page, err := store.QueryGames(ctx, catalog.Query{Text: "STRASSE", Limit: 10})
	if err != nil || len(page.Games) != 1 || page.Games[0].ID != "snes-strasse-test" {
		t.Fatalf("folded search = %+v, %v", page, err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var searchText string
	if err := db.QueryRowContext(ctx, "SELECT search_text FROM games WHERE game_id = 'snes-strasse-test'").Scan(&searchText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searchText, "strasse") {
		t.Fatalf("search_text = %q, want Go-folded strasse", searchText)
	}
}

func TestStoreEnforcesUniqueRootsAndStableIdentities(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	if _, err := store.MarkRootOffline(ctx, root, "not mounted"); err != nil {
		t.Fatalf("MarkRootOffline(first): %v", err)
	}

	for _, tt := range []struct {
		name string
		root catalog.Root
	}{
		{name: "same ID changes system", root: catalog.Root{ID: "snes-main", System: protocol.SystemMegaDrive, Path: "/games/snes"}},
		{name: "same ID changes path", root: catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/other"}},
		{name: "same path changes ID", root: catalog.Root{ID: "snes-copy", System: protocol.SystemSNES, Path: "/games/snes"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.MarkRootOffline(ctx, tt.root, "still offline"); err == nil {
				t.Fatalf("MarkRootOffline(%+v) succeeded, want identity error", tt.root)
			}
		})
	}

	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	first := candidate("snes-one", "One", "one.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	mustObserve(t, session, first, catalog.ChangeAdded)
	mustComplete(t, session)

	for _, tt := range []struct {
		name      string
		candidate catalog.Candidate
	}{
		{name: "same game ID changes path", candidate: candidate("snes-one", "One", "elsewhere.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)},
		{name: "same library path changes game ID", candidate: candidate("snes-other", "One", "one.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)},
		{name: "candidate system differs from root", candidate: func() catalog.Candidate {
			c := candidate("megadrive-one", "One", "one.md", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
			c.System = protocol.SystemMegaDrive
			return c
		}()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			x, err := store.BeginRootScan(ctx, root)
			if err != nil {
				t.Fatalf("BeginRootScan: %v", err)
			}
			defer x.Rollback()
			if _, err := x.Observe(ctx, tt.candidate); err == nil {
				t.Fatalf("Observe(%+v) succeeded, want identity error", tt.candidate)
			}
		})
	}
}

func TestScanSessionCountsGenerationsAndRetainsMissingRows(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	valid := candidate("snes-valid", "Valid", "valid.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	invalid := candidate("snes-broken", "Broken", "broken.zip", catalog.SourceKindZIP, catalog.SourceStateInvalid, fingerprintB)
	invalid.Reason = "archive has two ROM members"

	first, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(first): %v", err)
	}
	mustObserve(t, first, valid, catalog.ChangeAdded)
	mustObserve(t, first, invalid, catalog.ChangeAdded)
	if got := mustComplete(t, first); got != (catalog.RootReport{
		RootID: "snes-main", System: protocol.SystemSNES, Added: 2, Invalid: 1,
	}) {
		t.Fatalf("first report = %+v", got)
	}
	assertGeneration(t, store.path, 1)

	invalid.Title = "Broken Archive"
	second, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(second): %v", err)
	}
	mustObserve(t, second, valid, catalog.ChangeUnchanged)
	mustObserve(t, second, invalid, catalog.ChangeUpdated)
	if got := mustComplete(t, second); got != (catalog.RootReport{
		RootID: "snes-main", System: protocol.SystemSNES, Updated: 1, Unchanged: 1, Invalid: 1,
	}) {
		t.Fatalf("second report = %+v", got)
	}
	assertGeneration(t, store.path, 2)

	third, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(third): %v", err)
	}
	mustObserve(t, third, valid, catalog.ChangeUnchanged)
	if got := mustComplete(t, third); got != (catalog.RootReport{
		RootID: "snes-main", System: protocol.SystemSNES, Unchanged: 1, Missing: 1,
	}) {
		t.Fatalf("third report = %+v", got)
	}
	assertGeneration(t, store.path, 3)

	missing := mustGame(t, store, "snes-broken")
	if missing.State != catalog.SourceStateMissing {
		t.Fatalf("removed game state = %q, want %q", missing.State, catalog.SourceStateMissing)
	}
	games, err := store.Games(ctx)
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("Games length = %d, want retained 2", len(games))
	}
}

func TestUnchangedObserveDoesNotRewriteSearchText(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	first, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(first): %v", err)
	}
	mustObserve(t, first, candidate("snes-keep", "Keep", "keep.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	if got := mustComplete(t, first); got.Added != 1 {
		t.Fatalf("first report = %+v", got)
	}

	db, err := sql.Open("sqlite", store.path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var before int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM games_fts_data").Scan(&before); err != nil {
		t.Fatalf("fts_data before: %v", err)
	}

	second, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(second): %v", err)
	}
	mustObserve(t, second, candidate("snes-keep", "Keep", "keep.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeUnchanged)
	if got := mustComplete(t, second); got.Unchanged != 1 {
		t.Fatalf("second report = %+v", got)
	}

	var after int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM games_fts_data").Scan(&after); err != nil {
		t.Fatalf("fts_data after: %v", err)
	}
	if after != before {
		t.Fatalf("unchanged observe rebuilt FTS (%d -> %d rows)", before, after)
	}
}

func TestScanSessionPreservesContentForUnchangedFingerprint(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Original", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(first): %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)
	content := catalog.Content{SHA256: strings.Repeat("a", 64), Size: 1024, Extension: "sfc"}
	if updated, err := store.UpdateContent(ctx, c.ID, fingerprintA, content); err != nil || !updated {
		t.Fatalf("UpdateContent() = %v, %v, want true, nil", updated, err)
	}

	x, err = store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(second): %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeUnchanged)
	mustComplete(t, x)
	assertContent(t, mustGame(t, store, c.ID).Content, content)

	c.Title = "Renamed"
	x, err = store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(third): %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeUpdated)
	mustComplete(t, x)
	assertContent(t, mustGame(t, store, c.ID).Content, content)
}

func TestScanSessionFingerprintOrKindChangeClearsContent(t *testing.T) {
	tests := []struct {
		name string
		edit func(*catalog.Candidate)
	}{
		{name: "source kind", edit: func(c *catalog.Candidate) { c.Kind = catalog.SourceKindRaw }},
		{name: "source size", edit: func(c *catalog.Candidate) { c.Fingerprint.SourceSize = 101 }},
		{name: "modified time", edit: func(c *catalog.Candidate) { c.Fingerprint.ModifiedNS = 201 }},
		{name: "ZIP member", edit: func(c *catalog.Candidate) { c.Fingerprint.ZIPMember = "other.sfc" }},
		{name: "ZIP size", edit: func(c *catalog.Candidate) { c.Fingerprint.ZIPSize = 301 }},
		{name: "ZIP CRC32", edit: func(c *catalog.Candidate) { c.Fingerprint.ZIPCRC32 = 401 }},
		{name: "ZIP entry count", edit: func(c *catalog.Candidate) { c.Fingerprint.ZIPEntryCount = 3 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t)
			root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
			original := candidate("snes-game", "Game", "game.zip", catalog.SourceKindZIP, catalog.SourceStateAvailable, fingerprintB)
			x, err := store.BeginRootScan(ctx, root)
			if err != nil {
				t.Fatalf("BeginRootScan(first): %v", err)
			}
			mustObserve(t, x, original, catalog.ChangeAdded)
			mustComplete(t, x)
			content := catalog.Content{SHA256: strings.Repeat("b", 64), Size: 300, Extension: "sfc"}
			if updated, err := store.UpdateContent(ctx, original.ID, fingerprintB, content); err != nil || !updated {
				t.Fatalf("UpdateContent() = %v, %v", updated, err)
			}

			changed := original
			tt.edit(&changed)
			x, err = store.BeginRootScan(ctx, root)
			if err != nil {
				t.Fatalf("BeginRootScan(second): %v", err)
			}
			mustObserve(t, x, changed, catalog.ChangeUpdated)
			mustComplete(t, x)
			if got := mustGame(t, store, original.ID).Content; got != nil {
				t.Fatalf("Content after %s change = %+v, want nil", tt.name, got)
			}
		})
	}
}

func TestStoreRetireLibraryDropsGamesByID(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	keep := catalog.Root{ID: "snes-keep", System: protocol.SystemSNES, Path: "/games/keep"}
	drop := catalog.Root{ID: "snes-drop", System: protocol.SystemSNES, Path: "/games/drop"}
	mega := catalog.Root{ID: "genesis-main", System: protocol.SystemMegaDrive, Path: "/games/mega"}
	keepGame := candidate("snes-keep-game", "Keep", "keep.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	dropGame := candidate("snes-drop-game", "Drop", "drop.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	megaGame := catalog.Candidate{
		ID: "md-game", Title: "Mega", RelativePath: "mega.md", System: protocol.SystemMegaDrive,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: fingerprintA,
	}
	for _, item := range []struct {
		root catalog.Root
		game catalog.Candidate
	}{
		{keep, keepGame},
		{drop, dropGame},
		{mega, megaGame},
	} {
		session, err := store.BeginRootScan(ctx, item.root)
		if err != nil {
			t.Fatalf("BeginRootScan(%s): %v", item.root.ID, err)
		}
		mustObserve(t, session, item.game, catalog.ChangeAdded)
		mustComplete(t, session)
	}

	if err := store.RetireLibrary(ctx, drop.ID); err != nil {
		t.Fatalf("RetireLibrary: %v", err)
	}
	if err := store.RetireLibrary(ctx, drop.ID); err != nil {
		t.Fatalf("RetireLibrary(idempotent): %v", err)
	}
	if _, err := store.Game(ctx, dropGame.ID); err == nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("retired game still present: %v", err)
	}
	if got := mustGame(t, store, keepGame.ID); got.State != catalog.SourceStateAvailable {
		t.Fatalf("kept SNES game = %+v", got)
	}
	if got := mustGame(t, store, megaGame.ID); got.System != protocol.SystemMegaDrive {
		t.Fatalf("non-SNES game = %+v", got)
	}
	libraries, err := store.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, library := range libraries {
		if library.ID == drop.ID {
			found = true
			if library.Path != drop.Path {
				t.Fatalf("retired library identity = %#v", library)
			}
		}
	}
	if !found {
		t.Fatal("retired library row was deleted")
	}
}

func TestStoreReleaseLibraryRootRemovesSupersededIdentity(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	old := catalog.Root{ID: "old-snes-id", System: protocol.SystemSNES, Path: "/games/snes"}
	replacement := catalog.Root{ID: "new-snes-id", System: protocol.SystemSNES, Path: old.Path}
	session, err := store.BeginRootScan(ctx, old)
	if err != nil {
		t.Fatalf("BeginRootScan(old): %v", err)
	}
	game := candidate("old-snes-game", "Axelay", "Axelay.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	mustObserve(t, session, game, catalog.ChangeAdded)
	mustComplete(t, session)

	if err := store.ReleaseLibraryRoot(ctx, replacement); err != nil {
		t.Fatalf("ReleaseLibraryRoot: %v", err)
	}
	if _, err := store.Game(ctx, game.ID); err == nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("released game still present: %v", err)
	}
	libraries, err := store.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries) != 0 {
		t.Fatalf("libraries after release = %#v, want none", libraries)
	}

	rescan, err := store.BeginRootScan(ctx, replacement)
	if err != nil {
		t.Fatalf("BeginRootScan(replacement): %v", err)
	}
	if err := rescan.Rollback(); err != nil {
		t.Fatalf("Rollback(replacement): %v", err)
	}
}

func TestStoreReleaseLibraryRootAllowsSystemReclassification(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	old := catalog.Root{ID: "old-snes-id", System: protocol.SystemSNES, Path: "/games/shared"}
	replacement := catalog.Root{ID: "genesis-main", System: protocol.SystemMegaDrive, Path: old.Path}
	session, err := store.BeginRootScan(ctx, old)
	if err != nil {
		t.Fatalf("BeginRootScan(old): %v", err)
	}
	game := candidate("old-snes-game", "Axelay", "Axelay.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	mustObserve(t, session, game, catalog.ChangeAdded)
	mustComplete(t, session)

	if err := store.ReleaseLibraryRoot(ctx, replacement); err != nil {
		t.Fatalf("ReleaseLibraryRoot: %v", err)
	}
	rescan, err := store.BeginRootScan(ctx, replacement)
	if err != nil {
		t.Fatalf("BeginRootScan(reclassified): %v", err)
	}
	if err := rescan.Rollback(); err != nil {
		t.Fatalf("Rollback(reclassified): %v", err)
	}
}

func TestStoreReleaseLibraryRootAllowsSameIDSystemReclassification(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	old := catalog.Root{ID: "shared-main", System: protocol.SystemMegaDrive, Path: "/games/shared"}
	replacement := catalog.Root{ID: old.ID, System: protocol.SystemSNES, Path: old.Path}
	session, err := store.BeginRootScan(ctx, old)
	if err != nil {
		t.Fatalf("BeginRootScan(old): %v", err)
	}
	game := candidate("old-mega-game", "Sonic", "Sonic.md", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	game.System = protocol.SystemMegaDrive
	mustObserve(t, session, game, catalog.ChangeAdded)
	mustComplete(t, session)

	if err := store.ReleaseLibraryRoot(ctx, replacement); err != nil {
		t.Fatalf("ReleaseLibraryRoot: %v", err)
	}
	rescan, err := store.BeginRootScan(ctx, replacement)
	if err != nil {
		t.Fatalf("BeginRootScan(reclassified): %v", err)
	}
	if err := rescan.Rollback(); err != nil {
		t.Fatalf("Rollback(reclassified): %v", err)
	}
}

func TestStoreReleaseLibraryRootAllowsSameIDSystemAndPathReclassification(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	old := catalog.Root{ID: "shared-main", System: protocol.SystemMegaDrive, Path: "/games/old"}
	replacement := catalog.Root{ID: old.ID, System: protocol.SystemSNES, Path: "/games/new"}
	session, err := store.BeginRootScan(ctx, old)
	if err != nil {
		t.Fatalf("BeginRootScan(old): %v", err)
	}
	game := candidate("old-mega-game", "Sonic", "Sonic.md", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	game.System = protocol.SystemMegaDrive
	mustObserve(t, session, game, catalog.ChangeAdded)
	mustComplete(t, session)

	if err := store.ReleaseLibraryRoot(ctx, replacement); err != nil {
		t.Fatalf("ReleaseLibraryRoot: %v", err)
	}
	rescan, err := store.BeginRootScan(ctx, replacement)
	if err != nil {
		t.Fatalf("BeginRootScan(reclassified): %v", err)
	}
	if err := rescan.Rollback(); err != nil {
		t.Fatalf("Rollback(reclassified): %v", err)
	}
}

func TestStoreRebindLibraryMovesRootAndDropsStaleGames(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	oldRoot := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/old"}
	game := candidate("snes-game", "Old", "old.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	session, err := store.BeginRootScan(ctx, oldRoot)
	if err != nil {
		t.Fatalf("BeginRootScan(old): %v", err)
	}
	mustObserve(t, session, game, catalog.ChangeAdded)
	mustComplete(t, session)

	newRoot := oldRoot
	newRoot.Path = "/games/new"
	if err := store.RebindLibrary(ctx, newRoot); err != nil {
		t.Fatalf("RebindLibrary: %v", err)
	}
	if _, err := store.Game(ctx, game.ID); err == nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale game remained after rebind: %v", err)
	}
	libraries, err := store.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(libraries, []catalog.Root{newRoot}) {
		t.Fatalf("libraries after rebind = %#v, want %#v", libraries, []catalog.Root{newRoot})
	}
	rescan, err := store.BeginRootScan(ctx, newRoot)
	if err != nil {
		t.Fatalf("BeginRootScan(new): %v", err)
	}
	rescan.Rollback()
}

func TestStoreRebindLibraryReplacesSupersededDestinationIdentity(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	configured := catalog.Root{ID: "operator-snes", System: protocol.SystemSNES, Path: "/games/old"}
	synthetic := catalog.Root{ID: "folder-watch-synthetic", System: protocol.SystemSNES, Path: "/games/new"}
	for _, item := range []struct {
		root catalog.Root
		game catalog.Candidate
	}{
		{configured, candidate("configured-game", "Configured", "configured.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)},
		{synthetic, candidate("synthetic-game", "Synthetic", "synthetic.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)},
	} {
		session, err := store.BeginRootScan(ctx, item.root)
		if err != nil {
			t.Fatalf("BeginRootScan(%s): %v", item.root.ID, err)
		}
		mustObserve(t, session, item.game, catalog.ChangeAdded)
		mustComplete(t, session)
	}

	configured.Path = synthetic.Path
	if err := store.RebindLibrary(ctx, configured); err != nil {
		t.Fatalf("RebindLibrary: %v", err)
	}
	if games, err := store.Games(ctx); err != nil || len(games) != 0 {
		t.Fatalf("games after destination replacement = %+v, %v", games, err)
	}
	libraries, err := store.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(libraries, []catalog.Root{configured}) {
		t.Fatalf("libraries after destination replacement = %#v, want %#v", libraries, []catalog.Root{configured})
	}
	rescan, err := store.BeginRootScan(ctx, configured)
	if err != nil {
		t.Fatalf("BeginRootScan(rebound): %v", err)
	}
	rescan.Rollback()
}

func TestStoreOfflineMarkingPreservesRowsAndContent(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Game", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)
	content := catalog.Content{SHA256: strings.Repeat("c", 64), Size: 1024, Extension: "sfc"}
	if updated, err := store.UpdateContent(ctx, c.ID, fingerprintA, content); err != nil || !updated {
		t.Fatalf("UpdateContent() = %v, %v", updated, err)
	}

	report, err := store.MarkRootOffline(ctx, root, "volume unavailable")
	if err != nil {
		t.Fatalf("MarkRootOffline: %v", err)
	}
	if report != (catalog.RootReport{RootID: "snes-main", System: protocol.SystemSNES, Offline: true, Reason: "volume unavailable"}) {
		t.Fatalf("offline report = %+v", report)
	}
	game := mustGame(t, store, c.ID)
	if game.State != catalog.SourceStateAvailable || game.RootOnline {
		t.Fatalf("offline game state/root online = %q/%v, want available/false", game.State, game.RootOnline)
	}
	assertContent(t, game.Content, content)

	x, err = store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(online): %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeUnchanged)
	mustComplete(t, x)
	if game := mustGame(t, store, c.ID); !game.RootOnline {
		t.Fatal("completed online scan left RootOnline false")
	}
}

func TestScanSessionRollbackIsIdempotentAndPreservesGeneration(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Original", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(first): %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)

	x, err = store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(second): %v", err)
	}
	c.Title = "Rolled Back"
	mustObserve(t, x, c, catalog.ChangeUpdated)
	if err := x.Rollback(); err != nil {
		t.Fatalf("Rollback(first): %v", err)
	}
	if err := x.Rollback(); err != nil {
		t.Fatalf("Rollback(second): %v", err)
	}
	if got := mustGame(t, store, c.ID).Title; got != "Original" {
		t.Fatalf("title after rollback = %q, want Original", got)
	}
	assertGeneration(t, store.path, 1)
}

func TestScanSessionCompleteErrorRollsBackAndReleasesStore(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Original", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	first, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(first): %v", err)
	}
	mustObserve(t, first, c, catalog.ChangeAdded)
	mustComplete(t, first)

	interrupted, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(interrupted): %v", err)
	}
	defer interrupted.Rollback()
	c.Title = "Must Roll Back"
	mustObserve(t, interrupted, c, catalog.ChangeUpdated)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := interrupted.Complete(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete(canceled) error = %v, want context.Canceled", err)
	}

	queryCtx, queryCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer queryCancel()
	game, err := store.Game(queryCtx, c.ID)
	if err != nil {
		t.Fatalf("Game after failed Complete: %v", err)
	}
	if game.Title != "Original" {
		t.Fatalf("title after failed Complete = %q, want Original", game.Title)
	}
	assertGeneration(t, store.path, 1)
	if err := interrupted.Rollback(); err != nil {
		t.Fatalf("Rollback after failed Complete: %v", err)
	}
	if err := interrupted.Rollback(); err != nil {
		t.Fatalf("second Rollback after failed Complete: %v", err)
	}
}

func TestSearchUsesLiteralCaseInsensitiveSubstringAndDeterministicOrdering(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	for _, c := range []catalog.Candidate{
		candidate("snes-zulu", "Zulu", "zulu.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA),
		candidate("snes-under_id", "Under_score", "under.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA),
		candidate("snes-percent-id", "100% Adventure", "percent.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA),
		candidate("snes-alpha", "alpha", "alpha.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA),
	} {
		mustObserve(t, x, c, catalog.ChangeAdded)
	}
	mustComplete(t, x)

	assertGameIDs(t, mustGames(t, store), []string{"snes-percent-id", "snes-alpha", "snes-under_id", "snes-zulu"})
	assertSearchIDs(t, store, "ADVENTURE", []string{"snes-percent-id"})
	assertSearchIDs(t, store, "PERCENT-ID", []string{"snes-percent-id"})
	assertSearchIDs(t, store, "SNES", []string{"snes-percent-id", "snes-alpha", "snes-under_id", "snes-zulu"})
	assertSearchIDs(t, store, "%", []string{"snes-percent-id"})
	assertSearchIDs(t, store, "_", []string{"snes-under_id"})
	assertSearchIDs(t, store, "", []string{"snes-percent-id", "snes-alpha", "snes-under_id", "snes-zulu"})
	assertSearchIDs(t, store, "SNES", []string{"snes-percent-id", "snes-alpha", "snes-under_id", "snes-zulu"})
}

func TestSearchUsesUnicodeNormalizationAndCaseFolding(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	for _, c := range []catalog.Candidate{
		candidate("snes-eclair", "Éclair", "eclair.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA),
		candidate("snes-cafe", "Cafe\u0301 Racer", "cafe.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA),
		candidate("snes-straße", "Autobahn", "strasse.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA),
		candidate("snes-backslash", `Path\Game`, "backslash.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA),
	} {
		mustObserve(t, x, c, catalog.ChangeAdded)
	}
	mustComplete(t, x)

	assertSearchIDs(t, store, "éCL", []string{"snes-eclair"})
	assertSearchIDs(t, store, "CAFÉ", []string{"snes-cafe"})
	assertSearchIDs(t, store, "STRASSE", []string{"snes-straße"})
	assertSearchIDs(t, store, `\`, []string{"snes-backslash"})
}

func TestStoreGameUnknownAndContentCompareAndSet(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if _, err := store.Game(ctx, "missing"); err == nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Game(missing) error = %v, want sql.ErrNoRows", err)
	}
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Game", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)

	stale := fingerprintA
	stale.ModifiedNS++
	content := catalog.Content{SHA256: strings.Repeat("d", 64), Size: 1024, Extension: "sfc"}
	if updated, err := store.UpdateContent(ctx, c.ID, stale, content); err != nil || updated {
		t.Fatalf("UpdateContent(stale) = %v, %v, want false, nil", updated, err)
	}
	if got := mustGame(t, store, c.ID).Content; got != nil {
		t.Fatalf("content after stale update = %+v, want nil", got)
	}
	if updated, err := store.UpdateContent(ctx, c.ID, fingerprintA, content); err != nil || !updated {
		t.Fatalf("UpdateContent(current) = %v, %v, want true, nil", updated, err)
	}
	assertContent(t, mustGame(t, store, c.ID).Content, content)
	if updated, err := store.UpdateContent(ctx, "missing", fingerprintA, content); err != nil || updated {
		t.Fatalf("UpdateContent(missing) = %v, %v, want false, nil", updated, err)
	}
}

func TestStoreCompareAndSetContentRejectsChangedSourceKind(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	original := candidate("snes-game", "Game", "game.zip", catalog.SourceKindZIP, catalog.SourceStateAvailable, fingerprintB)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(original): %v", err)
	}
	mustObserve(t, x, original, catalog.ChangeAdded)
	mustComplete(t, x)
	loaded := mustGame(t, store, original.ID)

	changed := original
	changed.Kind = catalog.SourceKindRaw
	x, err = store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(changed): %v", err)
	}
	mustObserve(t, x, changed, catalog.ChangeUpdated)
	mustComplete(t, x)
	content := catalog.Content{SHA256: strings.Repeat("f", 64), Size: 300, Extension: "sfc"}

	if updated, err := store.CompareAndSetContent(ctx, loaded, root, content); err != nil || updated {
		t.Fatalf("CompareAndSetContent(stale kind) = %v, %v, want false, nil", updated, err)
	}
	if got := mustGame(t, store, original.ID).Content; got != nil {
		t.Fatalf("content after stale kind CAS = %+v, want nil", got)
	}
	current := mustGame(t, store, original.ID)
	if updated, err := store.CompareAndSetContent(ctx, current, root, content); err != nil || !updated {
		t.Fatalf("CompareAndSetContent(current) = %v, %v, want true, nil", updated, err)
	}
	assertContent(t, mustGame(t, store, original.ID).Content, content)
}

func TestStoreContentMatchesDoesNotOverwriteNewerDigest(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Game", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)
	game := mustGame(t, store, c.ID)
	oldContent := catalog.Content{SHA256: strings.Repeat("a", 64), Size: 1024, Extension: "sfc"}
	newContent := catalog.Content{SHA256: strings.Repeat("b", 64), Size: 1024, Extension: "sfc"}
	if updated, err := store.CompareAndSetContent(ctx, game, root, oldContent); err != nil || !updated {
		t.Fatalf("CompareAndSetContent(old) = %v, %v, want true, nil", updated, err)
	}
	remembered := mustGame(t, store, c.ID)
	if updated, err := store.CompareAndSetContent(ctx, remembered, root, newContent); err != nil || !updated {
		t.Fatalf("CompareAndSetContent(new) = %v, %v, want true, nil", updated, err)
	}

	if matches, err := store.ContentMatches(ctx, remembered, root, oldContent); err != nil || matches {
		t.Fatalf("ContentMatches(old) = %v, %v, want false, nil", matches, err)
	}
	assertContent(t, mustGame(t, store, c.ID).Content, newContent)
	current := mustGame(t, store, c.ID)
	if matches, err := store.ContentMatches(ctx, current, root, newContent); err != nil || !matches {
		t.Fatalf("ContentMatches(new) = %v, %v, want true, nil", matches, err)
	}
}

func TestStoreContentLaunchAdmissionBlocksExternalScanCommit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite3")
	launchStore, err := catalog.Open(path)
	if err != nil {
		t.Fatalf("Open(launch): %v", err)
	}
	defer launchStore.Close()
	scanStore, err := catalog.Open(path)
	if err != nil {
		t.Fatalf("Open(scan): %v", err)
	}
	defer scanStore.Close()

	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	original := candidate("snes-game", "Game", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	session, err := launchStore.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan(original): %v", err)
	}
	mustObserve(t, session, original, catalog.ChangeAdded)
	mustComplete(t, session)
	game := mustGame(t, launchStore, original.ID)
	content := catalog.Content{SHA256: strings.Repeat("a", 64), Size: 1024, Extension: "sfc"}
	if updated, err := launchStore.CompareAndSetContent(ctx, game, root, content); err != nil || !updated {
		t.Fatalf("CompareAndSetContent = %v, %v, want true, nil", updated, err)
	}
	game = mustGame(t, launchStore, original.ID)
	admission, err := launchStore.BeginContentLaunchAdmission(ctx, game, root, content)
	if err != nil {
		t.Fatalf("BeginContentLaunchAdmission: %v", err)
	}
	if !admission.ContentMatches() {
		t.Fatal("content launch admission rejected current snapshot")
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := launchStore.Games(ctx)
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("catalog read during launch admission: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("catalog read blocked during launch admission")
	}

	changed := original
	changed.Fingerprint.ModifiedNS++
	scanStarted := make(chan struct{})
	scanDone := make(chan error, 1)
	go func() {
		close(scanStarted)
		x, err := scanStore.BeginRootScan(ctx, root)
		if err != nil {
			scanDone <- err
			return
		}
		if _, err := x.Observe(ctx, changed); err != nil {
			_ = x.Rollback()
			scanDone <- err
			return
		}
		_, err = x.Complete(ctx)
		scanDone <- err
	}()
	<-scanStarted
	select {
	case err := <-scanDone:
		t.Fatalf("external scan completed during launch admission: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := admission.Close(); err != nil {
		t.Fatalf("close admission: %v", err)
	}
	select {
	case err := <-scanDone:
		if err != nil {
			t.Fatalf("external scan after admission: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("external scan remained blocked after launch admission closed")
	}
	if matches, err := launchStore.ContentMatches(ctx, game, root, content); err != nil || matches {
		t.Fatalf("ContentMatches(old snapshot) = %v, %v, want false, nil", matches, err)
	}
}

func TestStoreContentMatchesRejectsEverySnapshotDimension(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Game", "game.zip", catalog.SourceKindZIP, catalog.SourceStateAvailable, fingerprintB)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)
	game := mustGame(t, store, c.ID)
	content := catalog.Content{SHA256: strings.Repeat("e", 64), Size: 2048, Extension: "sfc"}
	if updated, err := store.CompareAndSetContent(ctx, game, root, content); err != nil || !updated {
		t.Fatalf("CompareAndSetContent = %v, %v, want true, nil", updated, err)
	}
	game = mustGame(t, store, c.ID)

	tests := []struct {
		name string
		edit func(*catalog.Game, *catalog.Root, *catalog.Content)
	}{
		{name: "game id", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.ID = "snes-other" }},
		{name: "library id", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.LibraryID = "snes-other" }},
		{name: "system", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.System = protocol.SystemMegaDrive }},
		{name: "relative path", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.RelativePath = "other.zip" }},
		{name: "source kind", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.Kind = catalog.SourceKindRaw }},
		{name: "source size", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.Fingerprint.SourceSize++ }},
		{name: "modified ns", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.Fingerprint.ModifiedNS++ }},
		{name: "zip member", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.Fingerprint.ZIPMember = "other.sfc" }},
		{name: "zip size", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.Fingerprint.ZIPSize++ }},
		{name: "zip crc", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.Fingerprint.ZIPCRC32++ }},
		{name: "zip entries", edit: func(g *catalog.Game, _ *catalog.Root, _ *catalog.Content) { g.Fingerprint.ZIPEntryCount++ }},
		{name: "content sha", edit: func(_ *catalog.Game, _ *catalog.Root, c *catalog.Content) { c.SHA256 = strings.Repeat("f", 64) }},
		{name: "content size", edit: func(_ *catalog.Game, _ *catalog.Root, c *catalog.Content) { c.Size++ }},
		{name: "content extension", edit: func(_ *catalog.Game, _ *catalog.Root, c *catalog.Content) { c.Extension = "bin" }},
		{name: "root id", edit: func(_ *catalog.Game, r *catalog.Root, _ *catalog.Content) { r.ID = "snes-other" }},
		{name: "root system", edit: func(_ *catalog.Game, r *catalog.Root, _ *catalog.Content) { r.System = protocol.SystemMegaDrive }},
		{name: "root path", edit: func(_ *catalog.Game, r *catalog.Root, _ *catalog.Content) { r.Path = "/games/other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotGame, gotRoot, gotContent := game, root, content
			test.edit(&gotGame, &gotRoot, &gotContent)
			if matches, err := store.ContentMatches(ctx, gotGame, gotRoot, gotContent); err != nil || matches {
				t.Fatalf("ContentMatches = %v, %v, want false, nil", matches, err)
			}
		})
	}
}

func TestStoreCompareAndSetContentRejectsReplacedPriorDigest(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Game", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)
	initial := mustGame(t, store, c.ID)
	first := catalog.Content{SHA256: strings.Repeat("c", 64), Size: 1024, Extension: "sfc"}
	second := catalog.Content{SHA256: strings.Repeat("d", 64), Size: 1024, Extension: "sfc"}
	if updated, err := store.CompareAndSetContent(ctx, initial, root, first); err != nil || !updated {
		t.Fatalf("CompareAndSetContent(first) = %v, %v, want true, nil", updated, err)
	}
	if updated, err := store.CompareAndSetContent(ctx, initial, root, second); err != nil || updated {
		t.Fatalf("CompareAndSetContent(stale prior) = %v, %v, want false, nil", updated, err)
	}
	assertContent(t, mustGame(t, store, c.ID).Content, first)
}

func TestStoreLoadsLibraryRootAndContentCASBindsIt(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/private-a"}
	c := candidate("snes-game", "Game", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)

	loaded := mustGame(t, store, c.ID)
	if matches, err := store.GameMatchesRoot(ctx, loaded, root); err != nil || !matches {
		t.Fatalf("GameMatchesRoot(current) = %v, %v, want true, nil", matches, err)
	}
	content := catalog.Content{SHA256: strings.Repeat("b", 64), Size: 1024, Extension: "sfc"}
	staleID := root
	staleID.ID = "snes-other"
	if updated, err := store.CompareAndSetContent(ctx, loaded, staleID, content); err != nil || updated {
		t.Fatalf("CompareAndSetContent(stale root ID) = %v, %v, want false, nil", updated, err)
	}
	staleRoot := root
	staleRoot.Path = "/games/private-b"
	if matches, err := store.GameMatchesRoot(ctx, loaded, staleRoot); err != nil || matches {
		t.Fatalf("GameMatchesRoot(stale) = %v, %v, want false, nil", matches, err)
	}
	if updated, err := store.CompareAndSetContent(ctx, loaded, staleRoot, content); err != nil || updated {
		t.Fatalf("CompareAndSetContent(stale root) = %v, %v, want false, nil", updated, err)
	}
	if got := mustGame(t, store, c.ID).Content; got != nil {
		t.Fatalf("content after stale-root CAS = %+v, want nil", got)
	}
	if updated, err := store.CompareAndSetContent(ctx, loaded, root, content); err != nil || !updated {
		t.Fatalf("CompareAndSetContent(current root) = %v, %v, want true, nil", updated, err)
	}
}

func TestStoreRejectsPartiallyNullContentTuple(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "partial.sqlite3")
	store, err := catalog.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	c := candidate("snes-game", "Game", "game.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	x, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	mustObserve(t, x, c, catalog.ChangeAdded)
	mustComplete(t, x)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE games SET content_sha256 = ?, content_size = NULL, content_extension = NULL WHERE game_id = ?", strings.Repeat("e", 64), c.ID); err != nil {
		t.Fatalf("create partial content tuple: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw database: %v", err)
	}
	store, err = catalog.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	if _, err := store.Game(ctx, c.ID); err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("Game(partial content) error = %v, want content tuple error", err)
	}
	if _, err := store.Games(ctx); err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("Games(partial content) error = %v, want content tuple error", err)
	}
}

var fingerprintA = catalog.Fingerprint{SourceSize: 100, ModifiedNS: 200}

var fingerprintB = catalog.Fingerprint{
	SourceSize: 1000, ModifiedNS: 2000, ZIPMember: "game.sfc", ZIPSize: 300,
	ZIPCRC32: 400, ZIPEntryCount: 2,
}

func candidate(id, title, relativePath string, kind catalog.SourceKind, state catalog.SourceState, fingerprint catalog.Fingerprint) catalog.Candidate {
	return catalog.Candidate{
		ID: id, Title: title, RelativePath: relativePath, System: protocol.SystemSNES,
		Kind: kind, State: state, Fingerprint: fingerprint,
	}
}

type testStore struct {
	*catalog.Store
	path string
}

func openStore(t *testing.T) *testStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.sqlite3")
	store, err := catalog.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &testStore{Store: store, path: path}
}

func mustObserve(t *testing.T, session *catalog.ScanSession, candidate catalog.Candidate, want catalog.Change) {
	t.Helper()
	got, err := session.Observe(context.Background(), candidate)
	if err != nil {
		t.Fatalf("Observe(%s): %v", candidate.ID, err)
	}
	if got != want {
		t.Fatalf("Observe(%s) = %q, want %q", candidate.ID, got, want)
	}
}

func mustComplete(t *testing.T, session *catalog.ScanSession) catalog.RootReport {
	t.Helper()
	report, err := session.Complete(context.Background())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return report
}

func mustGame(t *testing.T, store interface {
	Game(context.Context, string) (catalog.Game, error)
}, id string) catalog.Game {
	t.Helper()
	game, err := store.Game(context.Background(), id)
	if err != nil {
		t.Fatalf("Game(%q): %v", id, err)
	}
	return game
}

func mustGames(t *testing.T, store interface {
	Games(context.Context) ([]catalog.Game, error)
}) []catalog.Game {
	t.Helper()
	games, err := store.Games(context.Background())
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	return games
}

func assertSearchIDs(t *testing.T, store interface {
	Search(context.Context, string) ([]catalog.Game, error)
}, query string, want []string) {
	t.Helper()
	games, err := store.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("Search(%q): %v", query, err)
	}
	assertGameIDs(t, games, want)
}

func assertGameIDs(t *testing.T, games []catalog.Game, want []string) {
	t.Helper()
	got := make([]string, len(games))
	for i := range games {
		got[i] = games[i].ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("game IDs = %v, want %v", got, want)
	}
}

func assertContent(t *testing.T, got *catalog.Content, want catalog.Content) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("Content = %+v, want %+v", got, want)
	}
}

func assertGeneration(t *testing.T, path string, want int64) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(generation): %v", err)
	}
	defer db.Close()
	var got int64
	if err := db.QueryRow("SELECT generation FROM libraries WHERE id = 'snes-main'").Scan(&got); err != nil {
		t.Fatalf("read generation: %v", err)
	}
	if got != want {
		t.Fatalf("generation = %d, want %d", got, want)
	}
}
