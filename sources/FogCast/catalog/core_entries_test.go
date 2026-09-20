package catalog_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestCoreEntryCreateBrowseAndReopen(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	packageID := strings.Repeat("a", 64)
	entry, err := store.CreateCoreEntry(ctx, "Standalone Pong", "fes.pong", packageID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "Standalone Pong" || entry.CoreID != "fes.pong" || entry.PackageID != packageID {
		t.Fatalf("entry = %+v", entry)
	}
	if err := protocol.ValidateGameID(entry.GameID); err != nil {
		t.Fatalf("game ID %q: %v", entry.GameID, err)
	}
	game := mustGame(t, store, entry.GameID)
	if game.Title != entry.Title || game.System != catalog.CorePlatform ||
		game.Kind != catalog.SourceKindCorePackage || game.State != catalog.SourceStateAvailable ||
		!game.RootOnline || game.Content != nil || game.Fingerprint != (catalog.Fingerprint{}) ||
		game.RelativePath != entry.GameID || game.GroupKey != entry.GameID {
		t.Fatalf("catalog game = %+v", game)
	}
	if catalog.Launchable(catalog.CorePlatform) {
		t.Fatal("core package browse platform is protocol-launchable")
	}
	platform, ok := catalog.DefaultPlatforms().Lookup(catalog.CorePlatform)
	if !ok || platform.Label != "FPGA cores" || platform.LaunchSystem != "" || len(platform.Extensions) != 0 {
		t.Fatalf("core package platform = %+v, ok=%v", platform, ok)
	}
	platforms, err := store.Platforms(ctx)
	if err != nil || len(platforms) != 1 || platforms[0].ID != catalog.CorePlatform ||
		platforms[0].GameCount != 1 || !platforms[0].Online || platforms[0].Launchable {
		t.Fatalf("Platforms = %+v, %v", platforms, err)
	}
	page, err := store.QueryGames(ctx, catalog.Query{Platform: catalog.CorePlatform, Limit: 10})
	if err != nil || len(page.Games) != 1 || page.Games[0].ID != entry.GameID {
		t.Fatalf("QueryGames(fpga) = %+v, %v", page, err)
	}
	found, err := store.Search(ctx, "standalone")
	if err != nil || len(found) != 1 || found[0].ID != entry.GameID {
		t.Fatalf("Search = %+v, %v", found, err)
	}
	entries, err := store.CoreEntries(ctx)
	if err != nil || !reflect.DeepEqual(entries, []catalog.CoreEntry{entry}) {
		t.Fatalf("CoreEntries = %+v, %v", entries, err)
	}
	loaded, err := store.CoreEntry(ctx, entry.GameID)
	if err != nil || loaded != entry {
		t.Fatalf("CoreEntry = %+v, %v", loaded, err)
	}
	if libraries, err := store.Libraries(ctx); err != nil || len(libraries) != 0 {
		t.Fatalf("Libraries exposed reserved core collection: %+v, %v", libraries, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := catalog.Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if loaded, err := reopened.CoreEntry(ctx, entry.GameID); err != nil || loaded != entry {
		t.Fatalf("reopened CoreEntry = %+v, %v", loaded, err)
	}
}

func TestCoreEntryCreateConflictsAndSelectionIsCAS(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	firstID := strings.Repeat("a", 64)
	secondID := strings.Repeat("b", 64)
	entry, err := store.CreateCoreEntry(ctx, "First", "fes.pong", firstID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCoreEntry(ctx, "First", "fes.pong", firstID); !errors.Is(err, catalog.ErrCoreEntryConflict) {
		t.Fatalf("duplicate CreateCoreEntry error = %v", err)
	}
	if other, err := store.CreateCoreEntry(ctx, "Other", "fes.pong", secondID); err != nil || other.GameID == entry.GameID {
		t.Fatalf("distinct-title CreateCoreEntry = %+v, %v", other, err)
	}
	if _, err := store.SelectCoreEntry(ctx, entry.GameID, "fes.other", firstID, secondID); !errors.Is(err, catalog.ErrCoreEntryCoreMismatch) {
		t.Fatalf("wrong-core SelectCoreEntry error = %v", err)
	}
	if _, err := store.SelectCoreEntry(ctx, entry.GameID, "fes.pong", secondID, firstID); !errors.Is(err, catalog.ErrCoreEntryConflict) {
		t.Fatalf("stale SelectCoreEntry error = %v", err)
	}
	selected, err := store.SelectCoreEntry(ctx, entry.GameID, "fes.pong", firstID, secondID)
	if err != nil || selected.GameID != entry.GameID || selected.Title != entry.Title ||
		selected.CoreID != entry.CoreID || selected.PackageID != secondID {
		t.Fatalf("SelectCoreEntry = %+v, %v", selected, err)
	}
	if _, err := store.SelectCoreEntry(ctx, entry.GameID, "fes.pong", firstID, secondID); !errors.Is(err, catalog.ErrCoreEntryConflict) {
		t.Fatalf("replayed SelectCoreEntry error = %v", err)
	}
	_, err = store.CreateCoreEntry(ctx, "Alpha", "fes.alpha", firstID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.CoreEntries(ctx)
	if err != nil || len(entries) != 3 || entries[0].GameID > entries[1].GameID || entries[1].GameID > entries[2].GameID {
		t.Fatalf("sorted CoreEntries = %+v, %v", entries, err)
	}
	if _, err := store.CoreEntry(ctx, "missing"); !errors.Is(err, catalog.ErrCoreEntryNotFound) {
		t.Fatalf("missing CoreEntry error = %v", err)
	}
}

func TestCoreEntryValidationIsFailClosed(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name, title, coreID, packageID string
	}{
		{"empty title", "", "fes.pong", strings.Repeat("a", 64)},
		{"control title", "bad\nname", "fes.pong", strings.Repeat("a", 64)},
		{"invalid core", "Pong", "../pong", strings.Repeat("a", 64)},
		{"invalid package", "Pong", "fes.pong", "bad"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openStore(t)
			if _, err := store.CreateCoreEntry(ctx, test.title, test.coreID, test.packageID); !errors.Is(err, catalog.ErrInvalidCoreEntry) {
				t.Fatalf("CreateCoreEntry error = %v", err)
			}
		})
	}
}

func TestCorePackageReservedCollectionCannotBeScannedOrRetired(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	entry, err := store.CreateCoreEntry(ctx, "Pong", "fes.pong", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	root := catalog.Root{ID: "attacker", System: catalog.CorePlatform, Path: t.TempDir()}
	if _, err := store.BeginRootScan(ctx, root); !errors.Is(err, catalog.ErrInvalidCoreEntry) {
		t.Fatalf("BeginRootScan(fpga) error = %v", err)
	}
	if _, err := (catalog.Scanner{Store: store.Store, Platforms: catalog.DefaultPlatforms()}).Scan(ctx, []catalog.Root{root}); !errors.Is(err, catalog.ErrInvalidCoreEntry) {
		t.Fatalf("Scan(fpga) error = %v", err)
	}
	// The reserved row is intentionally absent from Libraries, so its stable
	// ID is derived from the created game's owning library.
	game := mustGame(t, store, entry.GameID)
	if err := store.RetireLibrary(ctx, game.LibraryID); !errors.Is(err, catalog.ErrInvalidCoreEntry) {
		t.Fatalf("RetireLibrary(reserved) error = %v", err)
	}
	if _, err := store.CoreEntry(ctx, entry.GameID); err != nil {
		t.Fatalf("reserved entry disappeared: %v", err)
	}
}
