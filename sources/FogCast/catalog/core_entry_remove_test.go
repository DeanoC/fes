package catalog

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRemoveCoreEntryRetainsSavesAndHidesEveryLibraryView(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	defer s.Close()
	pkg := strings.Repeat("a", 64)
	removed, err := s.CreateCoreEntry(ctx, "Retained ZX81", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := s.CreateCoreEntry(ctx, "Keep", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	media, _, err := s.ImportCoreMedia(ctx, []byte{0, 10, 0x76})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertZX81Save(ctx, removed.GameID, media.MediaID, "My SAVE"); err != nil {
		t.Fatal(err)
	}
	gameBefore, err := s.Game(ctx, removed.GameID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.ListZX81Saves(ctx, removed.GameID)
	if err != nil || len(before) != 1 {
		t.Fatalf("saves before: %+v %v", before, err)
	}
	if err := s.SetFacets(ctx, removed.GameID, "Hidden genre", "1979", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, removed.GameID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, removed.GameID); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if _, err := s.CoreEntry(ctx, removed.GameID); !errors.Is(err, ErrCoreEntryNotFound) {
		t.Fatalf("entry remains: %v", err)
	}
	entries, err := s.CoreEntries(ctx)
	if err != nil || len(entries) != 1 || entries[0].GameID != keep.GameID {
		t.Fatalf("exported core entries: %+v %v", entries, err)
	}
	if _, err := s.Game(ctx, removed.GameID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("game remains visible: %v", err)
	}
	// Even a caller holding a pre-removal snapshot cannot re-admit or mutate
	// the retained parent as a source-backed game.
	root := Root{ID: corePackageLibraryID, System: CorePlatform, Path: corePackageRoot}
	if match, err := s.GameMatchesRoot(ctx, gameBefore, root); err != nil || match {
		t.Fatalf("stale game root match: %v %v", match, err)
	}
	content := Content{SHA256: strings.Repeat("b", 64), Size: 3, Extension: ".rom"}
	if changed, err := s.UpdateContent(ctx, removed.GameID, gameBefore.Fingerprint, content); err != nil || changed {
		t.Fatalf("stale game content update: %v %v", changed, err)
	}
	if changed, err := s.CompareAndSetContent(ctx, gameBefore, root, content); err != nil || changed {
		t.Fatalf("stale game compare-and-set: %v %v", changed, err)
	}
	for name, list := range map[string]func() ([]Game, error){
		"games":  func() ([]Game, error) { return s.Games(ctx) },
		"search": func() ([]Game, error) { return s.Search(ctx, "Retained") },
		"ids":    func() ([]Game, error) { return s.GamesByIDs(ctx, []string{removed.GameID, keep.GameID}) },
		"group":  func() ([]Game, error) { return s.GamesInGroup(ctx, removed.GameID, UnboundedVariantLimit) },
	} {
		games, err := list()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, game := range games {
			if game.ID == removed.GameID {
				t.Fatalf("%s shows removed game", name)
			}
		}
	}
	page, err := s.QueryGames(ctx, Query{Platform: CorePlatform, CoreIDs: []string{"fes.zx81"}, Text: "Retained", Grouped: true})
	if err != nil || len(page.Games) != 0 {
		t.Fatalf("query = %+v %v", page, err)
	}
	page, err = s.QueryGames(ctx, Query{Platform: CorePlatform, Text: "Retained"})
	if err != nil || len(page.Games) != 0 {
		t.Fatalf("ungrouped FTS query = %+v %v", page, err)
	}
	facets, err := s.Facets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, genre := range facets.Genres {
		if genre == "Hidden genre" {
			t.Fatal("removed facet visible")
		}
	}
	for _, year := range facets.Years {
		if year == "1979" {
			t.Fatal("removed year visible")
		}
	}
	platforms, err := s.Platforms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range platforms {
		if platform.ID == CorePlatform && platform.GameCount != 1 {
			t.Fatalf("platform count %+v", platform)
		}
	}
	after, err := s.ListZX81Saves(ctx, removed.GameID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("saves changed: before=%+v after=%+v err=%v", before, after, err)
	}
	_, data, err := s.CoreMedia(ctx, media.MediaID)
	if err != nil || !reflect.DeepEqual(data, []byte{0, 10, 0x76}) {
		t.Fatalf("media changed: %v %v", data, err)
	}
	recreated, err := s.CreateCoreEntry(ctx, "Retained ZX81", "fes.zx81", pkg)
	if err != nil || recreated.GameID != removed.GameID {
		t.Fatalf("recreate: %+v %v", recreated, err)
	}
	after, err = s.ListZX81Saves(ctx, recreated.GameID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("recreated saves: %+v %v", after, err)
	}
	if _, err := s.CoreEntry(ctx, keep.GameID); err != nil {
		t.Fatalf("other entry lost: %v", err)
	}
	if err := s.RemoveCoreEntry(ctx, "missing"); !errors.Is(err, ErrCoreEntryNotFound) {
		t.Fatalf("missing removal = %v", err)
	}
	if err := s.RemoveCoreEntry(ctx, "Bad ID"); !errors.Is(err, ErrInvalidCoreEntry) {
		t.Fatalf("invalid removal = %v", err)
	}
}

func TestRemoveRemoteCoreEntryCanBeReimportedOnExplicitRefresh(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	defer s.Close()
	pkg := strings.Repeat("a", 64)
	origin := RemoteCoreEntryOrigin{SourceID: "remote-a", SourceGameID: "source-game", MediaID: strings.Repeat("b", 64)}
	first, err := s.CreateRemoteCoreEntry(ctx, "Remote", "fes.zx81", pkg, origin)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, first.GameID); err != nil {
		t.Fatal(err)
	}
	if _, _, found, err := s.RemoteCoreEntryBySource(ctx, origin.SourceID, origin.SourceGameID); err != nil || found {
		t.Fatalf("remote origin remains: %v %v", found, err)
	}
	second, err := s.CreateRemoteCoreEntry(ctx, "Renamed remote", "fes.zx81", pkg, origin)
	if err != nil || second.GameID != first.GameID {
		t.Fatalf("reimport: %+v %v", second, err)
	}
	game, err := s.Game(ctx, second.GameID)
	if err != nil || game.Title != "Renamed remote" || second.Title != game.Title {
		t.Fatalf("reimported title: %+v %+v %v", second, game, err)
	}
}

func TestRetainedCoreEntryRejectsUnrelatedIdentity(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	defer s.Close()
	pkg := strings.Repeat("a", 64)
	entry, err := s.CreateCoreEntry(ctx, "ZX81", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, entry.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE games SET reason = ? WHERE game_id = ?`, retainedCoreReason+"fes.other", entry.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCoreEntry(ctx, "ZX81", "fes.zx81", pkg); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("unrelated retained identity accepted: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE games SET reason = '' WHERE game_id = ?`, entry.GameID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, entry.GameID); !errors.Is(err, ErrCoreEntryNotFound) {
		t.Fatalf("orphan mistaken for removed entry: %v", err)
	}
}

func TestRemoveCoreEntryRecreatesLegacyIdentityAndSavedTapes(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	defer s.Close()
	pkg := strings.Repeat("a", 64)
	legacyID := "fpga-zx81-legacy"
	if _, err := s.db.ExecContext(ctx, `INSERT INTO libraries(id,system,root,online) VALUES (?,?,?,1)`, corePackageLibraryID, CorePlatform, corePackageRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO games(game_id,library_id,system,relative_path,title,source_kind,source_state,source_size,modified_ns,seen_generation,search_text,canonical_title,group_key,first_seen_ns) VALUES (?,?,?,?,?,?,?,0,0,0,?,?,?,1)`, legacyID, corePackageLibraryID, CorePlatform, "fes.zx81", "Legacy ZX81", SourceKindCorePackage, SourceStateAvailable, "Legacy ZX81", "Legacy ZX81", legacyID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO core_entries(game_id,core_id,package_id) VALUES (?,?,?)`, legacyID, "fes.zx81", pkg); err != nil {
		t.Fatal(err)
	}
	media, _, err := s.ImportCoreMedia(ctx, []byte{0, 10, 0x76})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertZX81Save(ctx, legacyID, media.MediaID, "Legacy save"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, legacyID); err != nil {
		t.Fatal(err)
	}
	id, err := s.ResolveCoreEntryID(ctx, "Legacy ZX81", "fes.zx81", pkg)
	if err != nil || id != legacyID {
		t.Fatalf("resolved %q %v", id, err)
	}
	recreated, err := s.CreateCoreEntry(ctx, "Legacy ZX81", "fes.zx81", pkg)
	if err != nil || recreated.GameID != legacyID {
		t.Fatalf("recreate %+v %v", recreated, err)
	}
	saves, err := s.ListZX81Saves(ctx, legacyID)
	if err != nil || len(saves) != 1 || saves[0].MediaID != media.MediaID {
		t.Fatalf("legacy saves %+v %v", saves, err)
	}
}

func TestResolveRetainedLegacyIdentityRejectsAmbiguousTitles(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	defer s.Close()
	pkg := strings.Repeat("a", 64)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO libraries(id,system,root,online) VALUES (?,?,?,1)`, corePackageLibraryID, CorePlatform, corePackageRoot); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"fpga-legacy-one", "fpga-legacy-two"} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO games(game_id,library_id,system,relative_path,title,source_kind,source_state,reason,source_size,modified_ns,seen_generation,search_text,canonical_title,group_key,first_seen_ns) VALUES (?,?,?,?,?,?,?,?,0,0,0,?,?,?,1)`, id, corePackageLibraryID, CorePlatform, id, "Legacy ZX81", SourceKindCorePackage, SourceStateAvailable, retainedCoreReason+"fes.zx81", "Legacy ZX81", "Legacy ZX81", id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ResolveCoreEntryID(ctx, "Legacy ZX81", "fes.zx81", pkg); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("ambiguous retained titles resolved: %v", err)
	}
	if _, err := s.CreateCoreEntry(ctx, "Legacy ZX81", "fes.zx81", pkg); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("ambiguous retained titles recreated: %v", err)
	}
}

func TestRemoveCoreEntryRejectsScannedGameWithoutChangingIt(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	defer s.Close()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO libraries(id,system,root,online) VALUES ('nes-lib','nes','/fixture',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO games(game_id,library_id,system,relative_path,title,source_kind,source_state,source_size,modified_ns,seen_generation,search_text,canonical_title,group_key,first_seen_ns) VALUES ('nes-original','nes-lib','nes','original.nes','Original','raw','available',1,1,1,'Original','Original','nes-original',1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, "nes-original"); !errors.Is(err, ErrCoreEntryNotFound) {
		t.Fatalf("scanned game removal: %v", err)
	}
	game, err := s.Game(ctx, "nes-original")
	if err != nil || game.Title != "Original" || game.Kind != SourceKindRaw {
		t.Fatalf("scanned game changed: %+v %v", game, err)
	}
}

func TestRemoveCoreEntryPreservesUserIdentityFromGeneratedHelpers(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	defer s.Close()
	pkg := strings.Repeat("a", 64)
	user, err := s.CreateCoreEntry(ctx, "My ZX81", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	media, _, err := s.ImportCoreMedia(ctx, []byte{0, 10, 0x76})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertZX81Save(ctx, user.GameID, media.MediaID, "My save"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, user.GameID); err != nil {
		t.Fatal(err)
	}
	kit := KitHostEntryIdentity{Identity: strings.Repeat("b", 64)}
	if _, err := s.CreateKitHostEntry(ctx, "My ZX81", "fes.zx81", pkg, "", "", kit); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("generated helper claimed retained title: %v", err)
	}
	saved, err := s.ListZX81Saves(ctx, user.GameID)
	if err != nil || len(saved) != 1 || saved[0].Name != "My save" {
		t.Fatalf("saved tape changed: %+v %v", saved, err)
	}
	helper, err := s.CreateKitHostEntry(ctx, "Generated ZX81", "fes.zx81", pkg, "", "", kit)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, helper.GameID); !errors.Is(err, ErrCoreEntryNotFound) {
		t.Fatalf("removed generated helper: %v", err)
	}
	if _, err := s.CoreEntry(ctx, helper.GameID); err != nil {
		t.Fatalf("generated helper changed: %v", err)
	}
}
