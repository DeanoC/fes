package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/internal/systems"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestDefaultPlatformsIncludesLaunchableAndBrowseOnly(t *testing.T) {
	if err := catalog.ValidatePlatform(protocol.SystemSNES); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidatePlatform("nes"); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidatePlatform("unknown"); err == nil {
		t.Fatal("ValidatePlatform(unknown) succeeded")
	}
	for _, system := range []protocol.System{
		protocol.SystemMegaDrive, protocol.SystemSNES, protocol.SystemNES, protocol.SystemSMS,
		protocol.SystemGameBoy, protocol.SystemGameBoyColor, protocol.SystemGBA, protocol.SystemPCE,
		protocol.SystemGameGear, protocol.SystemAtari2600, protocol.SystemAtari7800, protocol.SystemColecoVision,
		protocol.SystemAtariLynx, protocol.SystemWonderSwan, protocol.SystemWonderSwanColor, protocol.SystemIntellivision,
	} {
		if !catalog.Launchable(system) {
			t.Fatalf("platform %q is not launchable", system)
		}
	}
	launchable := 0
	for _, row := range systems.Rows() {
		if catalog.Launchable(row.PlatformID) {
			launchable++
		}
	}
	if launchable != 16 {
		t.Fatalf("launchable rows = %d, want 16", launchable)
	}
	for _, system := range []protocol.System{protocol.SystemNES, protocol.SystemSMS} {
		platform, ok := catalog.DefaultPlatforms().Lookup(system)
		if !ok || platform.LaunchSystem != system {
			t.Fatalf("platform %q launch system = %#v, ok=%v", system, platform, ok)
		}
	}
	for _, system := range []protocol.System{
		protocol.SystemGameBoyColor, protocol.SystemAtari2600, protocol.SystemColecoVision, protocol.SystemAtariLynx,
	} {
		platform, ok := catalog.DefaultPlatforms().Lookup(system)
		if !ok || platform.LaunchSystem != system {
			t.Fatalf("platform %q launch system = %#v, ok=%v", system, platform, ok)
		}
	}
	if catalog.PlatformLabel("nes") != "NES" {
		t.Fatalf("label = %q", catalog.PlatformLabel("nes"))
	}
}

func TestQueryGamesPaginatesAndFilters(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	snes := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	mega := catalog.Root{ID: "genesis-main", System: protocol.SystemMegaDrive, Path: "/games/md"}
	snesScan, err := store.BeginRootScan(ctx, snes)
	if err != nil {
		t.Fatal(err)
	}
	mustObserve(t, snesScan, candidate("snes-alpha", "Alpha", "alpha.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustObserve(t, snesScan, candidate("snes-zulu", "Zulu", "zulu.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustComplete(t, snesScan)
	megaScan, err := store.BeginRootScan(ctx, mega)
	if err != nil {
		t.Fatal(err)
	}
	c := candidate("megadrive-sonic", "Sonic", "sonic.md", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
	c.System = protocol.SystemMegaDrive
	mustObserve(t, megaScan, c, catalog.ChangeAdded)
	mustComplete(t, megaScan)

	page, err := store.QueryGames(ctx, catalog.Query{Limit: 2, Sort: catalog.SortTitle})
	if err != nil || len(page.Games) != 2 || page.NextCursor == "" {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	assertGameIDs(t, page.Games, []string{"snes-alpha", "megadrive-sonic"})
	next, err := store.QueryGames(ctx, catalog.Query{Limit: 2, Sort: catalog.SortTitle, Cursor: page.NextCursor})
	if err != nil || next.NextCursor != "" {
		t.Fatalf("second page = %+v, %v", next, err)
	}
	assertGameIDs(t, next.Games, []string{"snes-zulu"})

	filtered, err := store.QueryGames(ctx, catalog.Query{Platform: protocol.SystemMegaDrive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, filtered.Games, []string{"megadrive-sonic"})

	restricted, err := store.QueryGames(ctx, catalog.Query{Restrict: true, RestrictIDs: []string{"snes-zulu"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, restricted.Games, []string{"snes-zulu"})
	empty, err := store.QueryGames(ctx, catalog.Query{Restrict: true, Limit: 10})
	if err != nil || len(empty.Games) != 0 {
		t.Fatalf("empty restrict = %+v, %v", empty, err)
	}

	found, err := store.QueryGames(ctx, catalog.Query{Text: "SONIC", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, found.Games, []string{"megadrive-sonic"})

	_, err = store.QueryGames(ctx, catalog.Query{Cursor: "not-a-cursor", Limit: 10})
	if !errors.Is(err, catalog.ErrInvalidQuery) {
		t.Fatalf("malformed cursor error = %v", err)
	}

	platforms, err := store.Platforms(ctx)
	if err != nil || len(platforms) != 2 {
		t.Fatalf("platforms = %+v, %v", platforms, err)
	}
}

func TestQueryGamesTenThousandFixtureStaysBounded(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	const total = 10000
	for index := 0; index < total; index++ {
		id := fmt.Sprintf("snes-game-%05d", index)
		title := fmt.Sprintf("Title %05d", index)
		if index < 20 {
			title = "Shared Hedgehog (USA)"
		} else if index < 40 {
			title = "Shared Hedgehog (Japan)"
		} else if index == 4321 {
			title = "Unique Hedgehog"
		}
		c := candidate(id, title, fmt.Sprintf("game-%05d.sfc", index), catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA)
		mustObserve(t, session, c, catalog.ChangeAdded)
	}
	mustComplete(t, session)

	page, err := store.QueryGames(ctx, catalog.Query{Limit: 100, Sort: catalog.SortTitle})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Games) != 100 || page.NextCursor == "" {
		t.Fatalf("page size = %d cursor=%q", len(page.Games), page.NextCursor)
	}
	started := time.Now()
	found, err := store.QueryGames(ctx, catalog.Query{Text: "Unique Hedgehog", Limit: 10})
	if err != nil || len(found.Games) != 1 || found.Games[0].Title != "Unique Hedgehog" {
		t.Fatalf("fts/search = %+v, %v", found, err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("fts/search latency %s exceeded 2s software bound", elapsed)
	}
	walked := 0
	cursor := ""
	for {
		chunk, err := store.QueryGames(ctx, catalog.Query{Limit: 200, Cursor: cursor, Sort: catalog.SortTitle})
		if err != nil {
			t.Fatal(err)
		}
		walked += len(chunk.Games)
		if chunk.NextCursor == "" {
			break
		}
		cursor = chunk.NextCursor
	}
	if walked != total {
		t.Fatalf("walked %d, want %d", walked, total)
	}

	grouped, err := store.QueryGames(ctx, catalog.Query{Grouped: true, Limit: 100, Sort: catalog.SortTitle})
	if err != nil || len(grouped.Games) != 100 {
		t.Fatalf("grouped page = %+v, %v", grouped, err)
	}
	walkedGroups := 0
	cursor = ""
	for {
		chunk, err := store.QueryGames(ctx, catalog.Query{Grouped: true, Limit: 200, Cursor: cursor, Sort: catalog.SortTitle})
		if err != nil {
			t.Fatal(err)
		}
		walkedGroups += len(chunk.Games)
		if chunk.NextCursor == "" {
			break
		}
		cursor = chunk.NextCursor
	}
	if walkedGroups != total-39 {
		t.Fatalf("grouped walk %d, want %d", walkedGroups, total-39)
	}
	japan, err := store.QueryGames(ctx, catalog.Query{Grouped: true, Region: "japan", Limit: 100})
	if err != nil || len(japan.Games) != 1 || japan.Games[0].CanonicalTitle != "Shared Hedgehog" || japan.Games[0].Region != "japan" {
		t.Fatalf("grouped japan = %+v, %v", japan, err)
	}
}

func TestQueryGamesGroupsPreferredDumpAndFiltersRegion(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mustObserve(t, session, candidate("snes-sonic-japan", "Sonic (Japan)", "sonic-jp.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustObserve(t, session, candidate("snes-sonic-usa", "Sonic (USA)", "sonic-us.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustObserve(t, session, candidate("snes-sonic-beta", "Sonic (USA) (Beta)", "sonic-beta.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustObserve(t, session, candidate("snes-alpha", "Alpha", "alpha.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustComplete(t, session)

	grouped, err := store.QueryGames(ctx, catalog.Query{Grouped: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, grouped.Games, []string{"snes-alpha", "snes-sonic-usa"})
	if grouped.Games[1].VariantCount != 3 || grouped.Games[1].CanonicalTitle != "Sonic" || grouped.Games[1].Region != "usa" {
		t.Fatalf("preferred = %#v", grouped.Games[1])
	}

	japan, err := store.QueryGames(ctx, catalog.Query{Grouped: true, Region: "japan", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, japan.Games, []string{"snes-sonic-japan"})

	hidden, err := store.QueryGames(ctx, catalog.Query{Grouped: false, HidePrerelease: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, hidden.Games, []string{"snes-alpha", "snes-sonic-japan", "snes-sonic-usa"})

	variants, err := store.GamesInGroup(ctx, grouped.Games[1].GroupKey, 10)
	if err != nil || len(variants) != 3 {
		t.Fatalf("variants = %+v, %v", variants, err)
	}
}

func TestQueryGamesGroupedPreferredDumpStableAcrossRescan(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mustObserve(t, session, candidate("snes-sonic-japan", "Sonic (Japan)", "sonic-jp.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustObserve(t, session, candidate("snes-sonic-usa", "Sonic (USA)", "sonic-us.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustComplete(t, session)
	first, err := store.QueryGames(ctx, catalog.Query{Grouped: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, first.Games, []string{"snes-sonic-usa"})
	seen := first.Games[0].FirstSeenNS
	if seen == 0 {
		t.Fatal("first_seen_ns missing")
	}

	rescan, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mustObserve(t, rescan, candidate("snes-sonic-japan", "Sonic (Japan)", "sonic-jp.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeUnchanged)
	mustObserve(t, rescan, candidate("snes-sonic-usa", "Sonic (USA)", "sonic-us.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeUnchanged)
	mustComplete(t, rescan)
	second, err := store.QueryGames(ctx, catalog.Query{Grouped: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, second.Games, []string{"snes-sonic-usa"})
	if second.Games[0].FirstSeenNS != seen || second.Games[0].CanonicalTitle != "Sonic" {
		t.Fatalf("rescan preferred = %#v", second.Games[0])
	}
}

func TestQueryGamesExcludeIDsScalesPastSQLiteVariableLimit(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/snes"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mustObserve(t, session, candidate("snes-keep", "Keep", "keep.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustObserve(t, session, candidate("snes-drop", "Drop", "drop.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustComplete(t, session)
	ids := make([]string, 40000)
	for i := range ids {
		ids[i] = fmt.Sprintf("snes-played-%d", i)
	}
	ids[0] = "snes-drop"
	page, err := store.QueryGames(ctx, catalog.Query{ExcludeIDs: ids, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, page.Games, []string{"snes-keep"})
}

func TestGroupedCursorRoundTripsGroupKey(t *testing.T) {
	game := catalog.Game{
		ID: "snes-sonic-usa", Title: "Sonic (USA)", CanonicalTitle: "Sonic",
		System: protocol.SystemSNES, GroupKey: catalog.GroupKey(protocol.SystemSNES, "Sonic"),
	}
	raw := catalog.CursorForGrouped(game, catalog.SortTitle)
	id, err := catalog.CursorGameID(raw)
	if err != nil || id != game.GroupKey {
		t.Fatalf("cursor id = %q %v, want %q", id, err, game.GroupKey)
	}
}

func TestMatchesQueryFiltersAlignsRegionOtherAndOffline(t *testing.T) {
	undecorated := catalog.Game{Region: "", State: catalog.SourceStateAvailable, RootOnline: true}
	other := catalog.Game{Region: "other", State: catalog.SourceStateAvailable, RootOnline: true}
	japan := catalog.Game{Region: "japan", State: catalog.SourceStateAvailable, RootOnline: true}
	if !catalog.MatchesQueryFilters(undecorated, catalog.Query{Region: "other"}) || !catalog.MatchesQueryFilters(other, catalog.Query{Region: "other"}) {
		t.Fatal("empty and other should match region=other")
	}
	if catalog.MatchesQueryFilters(japan, catalog.Query{Region: "other"}) {
		t.Fatal("japan matched region=other")
	}
	invalidOnline := catalog.Game{State: catalog.SourceStateInvalid, RootOnline: true}
	missing := catalog.Game{State: catalog.SourceStateMissing, RootOnline: true}
	offlineRoot := catalog.Game{State: catalog.SourceStateAvailable, RootOnline: false}
	if catalog.MatchesQueryFilters(invalidOnline, catalog.Query{Availability: catalog.AvailabilityOffline}) {
		t.Fatal("invalid online row matched availability=offline")
	}
	if !catalog.MatchesQueryFilters(missing, catalog.Query{Availability: catalog.AvailabilityOffline}) {
		t.Fatal("missing row should match availability=offline")
	}
	if !catalog.MatchesQueryFilters(offlineRoot, catalog.Query{Availability: catalog.AvailabilityOffline}) {
		t.Fatal("offline root should match availability=offline")
	}
	invalidOffline := catalog.Game{State: catalog.SourceStateInvalid, RootOnline: false}
	if catalog.MatchesQueryFilters(invalidOffline, catalog.Query{Availability: catalog.AvailabilityOffline}) {
		t.Fatal("invalid offline row matched availability=offline")
	}
}

func TestQueryGamesOfflineExcludesInvalidRows(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	online := catalog.Root{ID: "snes-online", System: protocol.SystemSNES, Path: "/games/online"}
	offline := catalog.Root{ID: "snes-offline", System: protocol.SystemSNES, Path: "/games/offline"}
	onlineScan, err := store.BeginRootScan(ctx, online)
	if err != nil {
		t.Fatal(err)
	}
	mustObserve(t, onlineScan, candidate("snes-ready", "Ready", "ready.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	missing := candidate("snes-gone", "Gone", "gone.sfc", catalog.SourceKindRaw, catalog.SourceStateMissing, fingerprintA)
	mustObserve(t, onlineScan, missing, catalog.ChangeAdded)
	mustComplete(t, onlineScan)
	offlineScan, err := store.BeginRootScan(ctx, offline)
	if err != nil {
		t.Fatal(err)
	}
	mustObserve(t, offlineScan, candidate("snes-away", "Away", "away.sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	corrupt := candidate("snes-corrupt", "Corrupt", "corrupt.sfc", catalog.SourceKindRaw, catalog.SourceStateInvalid, fingerprintA)
	corrupt.Reason = "unreadable"
	mustObserve(t, offlineScan, corrupt, catalog.ChangeAdded)
	mustComplete(t, offlineScan)
	if _, err := store.MarkRootOffline(ctx, offline, "root offline"); err != nil {
		t.Fatal(err)
	}
	page, err := store.QueryGames(ctx, catalog.Query{Availability: catalog.AvailabilityOffline, Grouped: false, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertGameIDs(t, page.Games, []string{"snes-away", "snes-gone"})
}

func TestQueryGamesFindsOperatorSNESRootActRaiser(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	root := catalog.Root{ID: "operator-snes-root", System: protocol.SystemSNES, Path: "/games/Games/SNES"}
	session, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mustObserve(t, session, candidate("snes-actraiser-usa", "ActRaiser (USA)", "ActRaiser (USA).sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustObserve(t, session, candidate("snes-actraiser-plain", "ActRaiser", "ActRaiser.smc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustObserve(t, session, candidate("snes-actraiser-2", "ActRaiser 2 (USA)", "ActRaiser 2 (USA).sfc", catalog.SourceKindRaw, catalog.SourceStateAvailable, fingerprintA), catalog.ChangeAdded)
	mustComplete(t, session)

	for _, query := range []string{"ActRaiser", "actraiser"} {
		page, err := store.QueryGames(ctx, catalog.Query{Text: query, Grouped: true, Limit: 20})
		if err != nil {
			t.Fatalf("QueryGames(%q): %v", query, err)
		}
		var foundExact, foundSequel bool
		for _, game := range page.Games {
			if game.LibraryID != "operator-snes-root" {
				t.Fatalf("%q result library = %q", query, game.LibraryID)
			}
			if catalog.ExactActRaiserTitle(game.CanonicalTitle, game.Title) {
				foundExact = true
				if game.SearchAliases != catalog.SeededActRaiserAlias {
					t.Fatalf("%q exact aliases = %q", query, game.SearchAliases)
				}
			}
			if game.CanonicalTitle == "ActRaiser 2" {
				foundSequel = true
			}
		}
		if !foundExact {
			t.Fatalf("QueryGames(%q) missing exact ActRaiser: %+v", query, page.Games)
		}
		if query == "ActRaiser 2" && !foundSequel {
			t.Fatalf("QueryGames(%q) missing sequel: %+v", query, page.Games)
		}
	}

	sequel, err := store.QueryGames(ctx, catalog.Query{Text: "ActRaiser 2", Grouped: true, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(sequel.Games) != 1 || sequel.Games[0].CanonicalTitle != "ActRaiser 2" {
		t.Fatalf("sequel search = %+v", sequel.Games)
	}
}

func TestCursorRoundTripsTitleWithRecordSeparator(t *testing.T) {
	game := catalog.Game{
		ID: "snes-split", Title: "Alpha\x1eZulu", CanonicalTitle: "Alpha\x1eZulu",
		System: protocol.SystemSNES, GroupKey: "snes\x1fsplit",
	}
	raw := catalog.CursorForGrouped(game, catalog.SortTitle)
	id, err := catalog.CursorGameID(raw)
	if err != nil || id != game.GroupKey {
		t.Fatalf("cursor id = %q %v, want %q", id, err, game.GroupKey)
	}
	next, err := catalog.NormalizeQuery(catalog.Query{Sort: catalog.SortTitle, Cursor: raw, Limit: 10})
	if err != nil || next.Cursor != raw {
		t.Fatalf("normalize cursor = %#v %v", next, err)
	}
}
