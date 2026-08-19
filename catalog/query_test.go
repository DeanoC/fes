package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
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
	if !catalog.Launchable(protocol.SystemMegaDrive) || catalog.Launchable("nes") {
		t.Fatalf("launchable mapping is wrong")
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
		if index == 4321 {
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
}
