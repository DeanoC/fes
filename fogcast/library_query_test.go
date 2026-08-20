package fogcast

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/internal/metadata"
	"github.com/DeanoC/FogCast-POC/libraryuser"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestQueryGamesMapsMalformedCursorToBadRequest(t *testing.T) {
	store := &fakeServiceCatalog{queryErr: catalog.ErrInvalidQuery}
	service := newTestService(store, &fakeServicePreparer{}, &fakeServiceClient{})
	_, err := service.QueryGames(context.Background(), catalog.Query{Cursor: "not-a-cursor", Limit: 10})
	assertServiceErrorCode(t, err, protocol.CodeBadRequest)
}

func TestQueryRecentsAppliesTextFilterAndOpaqueCursor(t *testing.T) {
	ctx := context.Background()
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	alpha := catalog.Game{
		ID: "snes-alpha-test", Title: "Alpha", System: protocol.SystemSNES,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	sonic := catalog.Game{
		ID: "snes-sonic-test", Title: "Sonic", System: protocol.SystemSNES,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	mega := catalog.Game{
		ID: "megadrive-sonic-test", Title: "Sonic MD", System: protocol.SystemMegaDrive,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	store := &fakeServiceCatalog{games: []catalog.Game{alpha, sonic, mega}}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	if err := users.RecordPlay(ctx, alpha.ID); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, sonic.ID); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, mega.ID); err != nil {
		t.Fatal(err)
	}
	page, err := service.QueryGames(ctx, catalog.Query{Collection: "recents", Text: "Sonic", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Games) != 1 || page.Games[0].ID != mega.ID || page.NextCursor == "" || page.NextCursor == mega.ID {
		t.Fatalf("recents page = %+v", page)
	}
	next, err := service.QueryGames(ctx, catalog.Query{Collection: "recents", Text: "Sonic", Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Games) != 1 || next.Games[0].ID != sonic.ID {
		t.Fatalf("recents next = %+v", next)
	}
	_, err = service.QueryGames(ctx, catalog.Query{Collection: "recents", Cursor: "not-a-cursor"})
	assertServiceErrorCode(t, err, protocol.CodeBadRequest)
}

func TestQueryContinueUnplayedAndRecentlyAdded(t *testing.T) {
	ctx := context.Background()
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	alpha := catalog.Candidate{
		ID: "snes-alpha-test", Title: "Alpha", RelativePath: "alpha.sfc", System: root.System,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 1},
	}
	sonicUSA := catalog.Candidate{
		ID: "snes-sonic-usa", Title: "Sonic (USA)", RelativePath: "sonic-us.sfc", System: root.System,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 2},
	}
	sonicJP := catalog.Candidate{
		ID: "snes-sonic-japan", Title: "Sonic (Japan)", RelativePath: "sonic-jp.sfc", System: root.System,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 3},
	}
	for _, item := range []catalog.Candidate{alpha, sonicUSA, sonicJP} {
		if _, err := session.Observe(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := session.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, "snes-sonic-japan"); err != nil {
		t.Fatal(err)
	}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	continued, err := service.QueryGames(ctx, catalog.Query{Collection: "continue", Grouped: true, Limit: 10})
	if err != nil || len(continued.Games) != 1 || continued.Games[0].ID != "snes-sonic-usa" {
		t.Fatalf("continue = %+v, %v", continued, err)
	}
	unplayed, err := service.QueryGames(ctx, catalog.Query{Collection: "unplayed", Grouped: false, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	unplayedIDs := map[string]bool{}
	for _, game := range unplayed.Games {
		unplayedIDs[game.ID] = true
	}
	if len(unplayed.Games) != 2 || unplayedIDs["snes-sonic-japan"] || !unplayedIDs["snes-alpha-test"] || !unplayedIDs["snes-sonic-usa"] {
		t.Fatalf("unplayed = %+v", unplayed)
	}
	added, err := service.QueryGames(ctx, catalog.Query{Collection: "recently_added", Grouped: false, Limit: 10})
	if err != nil || len(added.Games) != 3 {
		t.Fatalf("recently added = %+v, %v", added, err)
	}
}

func TestQueryContinueFiltersGroupThenPrefersSurvivor(t *testing.T) {
	ctx := context.Background()
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []catalog.Candidate{
		{
			ID: "snes-sonic-usa", Title: "Sonic (USA)", RelativePath: "sonic-us.sfc", System: root.System,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 2},
		},
		{
			ID: "snes-sonic-japan", Title: "Sonic (Japan)", RelativePath: "sonic-jp.sfc", System: root.System,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 3},
		},
	} {
		if _, err := session.Observe(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := session.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, "snes-sonic-usa"); err != nil {
		t.Fatal(err)
	}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	filtered, err := service.QueryGames(ctx, catalog.Query{Collection: "continue", Grouped: true, Region: "japan", Limit: 10})
	if err != nil || len(filtered.Games) != 1 || filtered.Games[0].ID != "snes-sonic-japan" {
		t.Fatalf("continue japan = %+v, %v", filtered, err)
	}
}

func TestQueryContinueSearchesBeyondFirstTwoHundredPlays(t *testing.T) {
	ctx := context.Background()
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	sonic := catalog.Candidate{
		ID: "snes-sonic-usa", Title: "Sonic (USA)", RelativePath: "sonic-us.sfc", System: root.System,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 2},
	}
	if _, err := session.Observe(ctx, sonic); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, sonic.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	for i := 0; i < 200; i++ {
		if err := users.RecordPlay(ctx, fmt.Sprintf("snes-missing-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	continued, err := service.QueryGames(ctx, catalog.Query{Collection: "continue", Grouped: true, Limit: 10})
	if err != nil || len(continued.Games) != 1 || continued.Games[0].ID != sonic.ID {
		t.Fatalf("continue older play = %+v, %v", continued, err)
	}
}

func TestSyncFacetsNoopsWhenCacheMissing(t *testing.T) {
	dir := t.TempDir()
	store := &fakeServiceCatalog{games: []catalog.Game{serviceGame(catalog.Content{})}}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/games"}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(dir, "staging"), MetadataRoot: dir},
		store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	updated, err := service.SyncFacets(context.Background())
	if err != nil || updated != 0 {
		t.Fatalf("SyncFacets = %d, %v", updated, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "cache.sqlite3")); !os.IsNotExist(err) {
		t.Fatalf("cache created: %v", err)
	}
}

func TestSyncFacetsMatchesTitleAndPlatformOnly(t *testing.T) {
	ctx := context.Background()
	metadataRoot := filepath.Join(t.TempDir(), "metadata")
	cache, err := metadata.OpenCache(ctx, metadata.CacheConfig{Root: metadataRoot, CredentialScope: "client-id"})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(30 * 24 * time.Hour)
	snesKey, err := metadata.BuildCacheKey(metadata.ProviderIGDB, "snes", "sonic", "")
	if err != nil {
		t.Fatal(err)
	}
	nesKey, err := metadata.BuildCacheKey(metadata.ProviderIGDB, "nes", "sonic", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.PutWithMetadata(snesKey, metadata.CacheRecordMetadata{
		Provider: metadata.ProviderIGDB, PlatformID: "snes", NormalizedTitle: "sonic",
	}, metadata.Result{Outcome: metadata.OutcomeExact, Presentation: metadata.Presentation{Genre: "Platformer", Year: "1991"}}, expires, nil); err != nil {
		t.Fatal(err)
	}
	if err := cache.PutWithMetadata(nesKey, metadata.CacheRecordMetadata{
		Provider: metadata.ProviderIGDB, PlatformID: "nes", NormalizedTitle: "sonic",
	}, metadata.Result{Outcome: metadata.OutcomeExact, Presentation: metadata.Presentation{Genre: "Puzzle", Year: "1986"}}, expires, nil); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Observe(ctx, catalog.Candidate{
		ID: "snes-sonic-usa", Title: "Sonic (USA)", RelativePath: "sonic-us.sfc", System: root.System,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	service := newService(
		Config{
			Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second,
			Metadata: MetadataConfig{ClientID: "client-id"},
		},
		Paths{Staging: filepath.Join(t.TempDir(), "staging"), MetadataRoot: metadataRoot},
		store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	updated, err := service.SyncFacets(ctx)
	if err != nil || updated != 1 {
		t.Fatalf("SyncFacets = %d, %v", updated, err)
	}
	game, err := store.Game(ctx, "snes-sonic-usa")
	if err != nil || game.Genre != "Platformer" || game.Year != "1991" {
		t.Fatalf("facets = %#v, %v", game, err)
	}
}

func TestQueryContinueConsidersVariantsBeyondDetailCap(t *testing.T) {
	ctx := context.Background()
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 51; i++ {
		item := catalog.Candidate{
			ID: fmt.Sprintf("snes-sonic-jp-%d", i), Title: "Sonic (Japan)", RelativePath: fmt.Sprintf("sonic-jp-%d.sfc", i),
			System: root.System, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
			Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: int64(i + 1)},
		}
		if _, err := session.Observe(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	usa := catalog.Candidate{
		ID: "snes-sonic-usa", Title: "Sonic (USA)", RelativePath: "sonic-us.sfc", System: root.System,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 100},
	}
	if _, err := session.Observe(ctx, usa); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, "snes-sonic-jp-0"); err != nil {
		t.Fatal(err)
	}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	continued, err := service.QueryGames(ctx, catalog.Query{Collection: "continue", Grouped: true, Region: "usa", Limit: 10})
	if err != nil || len(continued.Games) != 1 || continued.Games[0].ID != "snes-sonic-usa" {
		t.Fatalf("continue usa beyond cap = %+v, %v", continued, err)
	}
}

func TestQueryRecentsGroupsPlayedSiblings(t *testing.T) {
	ctx := context.Background()
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []catalog.Candidate{
		{
			ID: "snes-sonic-usa", Title: "Sonic (USA)", RelativePath: "sonic-us.sfc", System: root.System,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 2},
		},
		{
			ID: "snes-sonic-japan", Title: "Sonic (Japan)", RelativePath: "sonic-jp.sfc", System: root.System,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 3},
		},
		{
			ID: "snes-alpha-test", Title: "Alpha", RelativePath: "alpha.sfc", System: root.System,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 1},
		},
	} {
		if _, err := session.Observe(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := session.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, "snes-sonic-japan"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := users.RecordPlay(ctx, "snes-sonic-usa"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := users.RecordPlay(ctx, "snes-alpha-test"); err != nil {
		t.Fatal(err)
	}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	page, err := service.QueryGames(ctx, catalog.Query{Collection: "recents", Grouped: true, Limit: 10})
	if err != nil || len(page.Games) != 2 {
		t.Fatalf("grouped recents = %+v, %v", page, err)
	}
	ids := []string{page.Games[0].ID, page.Games[1].ID}
	if ids[0] != "snes-alpha-test" || ids[1] != "snes-sonic-usa" {
		t.Fatalf("grouped recents ids = %#v", ids)
	}
}

func TestGamesInGroupReturnsEverySibling(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < catalog.MaxVariantLimit+1; index++ {
		item := catalog.Candidate{
			ID: fmt.Sprintf("snes-sonic-%02d-test", index), Title: "Sonic (USA)",
			RelativePath: fmt.Sprintf("sonic-%02d.sfc", index), System: root.System,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
			Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: int64(index + 1)},
		}
		if _, err := session.Observe(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := session.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	game, err := store.Game(ctx, "snes-sonic-00-test")
	if err != nil || game.GroupKey == "" {
		t.Fatalf("game = %+v, %v", game, err)
	}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	variants, err := service.GamesInGroup(ctx, game.GroupKey)
	if err != nil || len(variants) != catalog.MaxVariantLimit+1 {
		t.Fatalf("variants = %d, %v", len(variants), err)
	}
}
