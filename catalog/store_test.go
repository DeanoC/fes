package catalog_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/protocol"
)

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
	if version != 1 {
		t.Fatalf("user_version = %d, want 1", version)
	}
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	if want := []string{"games", "libraries"}; !reflect.DeepEqual(tables, want) {
		t.Fatalf("tables = %v, want %v", tables, want)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		t.Fatalf("set future user_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close future database: %v", err)
	}
	if _, err := catalog.Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open(future database) error = %v, want newer-schema error", err)
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

	if updated, err := store.CompareAndSetContent(ctx, loaded, content); err != nil || updated {
		t.Fatalf("CompareAndSetContent(stale kind) = %v, %v, want false, nil", updated, err)
	}
	if got := mustGame(t, store, original.ID).Content; got != nil {
		t.Fatalf("content after stale kind CAS = %+v, want nil", got)
	}
	current := mustGame(t, store, original.ID)
	if updated, err := store.CompareAndSetContent(ctx, current, content); err != nil || !updated {
		t.Fatalf("CompareAndSetContent(current) = %v, %v, want true, nil", updated, err)
	}
	assertContent(t, mustGame(t, store, original.ID).Content, content)
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
	if loaded.RootPath != root.Path {
		t.Fatalf("loaded root = %q, want %q", loaded.RootPath, root.Path)
	}
	content := catalog.Content{SHA256: strings.Repeat("b", 64), Size: 1024, Extension: "sfc"}
	staleRoot := loaded
	staleRoot.RootPath = "/games/private-b"
	if updated, err := store.CompareAndSetContent(ctx, staleRoot, content); err != nil || updated {
		t.Fatalf("CompareAndSetContent(stale root) = %v, %v, want false, nil", updated, err)
	}
	if got := mustGame(t, store, c.ID).Content; got != nil {
		t.Fatalf("content after stale-root CAS = %+v, want nil", got)
	}
	if updated, err := store.CompareAndSetContent(ctx, loaded, content); err != nil || !updated {
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
