package libraryuser_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/libraryuser"
	_ "modernc.org/sqlite"
)

func TestFavoritesSurviveIndependentOfCatalogRescan(t *testing.T) {
	ctx := context.Background()
	store, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetFavorite(ctx, "snes-mario-test", true); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPlay(ctx, "snes-mario-test"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPlay(ctx, "megadrive-sonic-test"); err != nil {
		t.Fatal(err)
	}
	state, err := store.State(ctx, "snes-mario-test")
	if err != nil || !state.Favorite || state.PlayCount != 1 || state.LastPlayedAt == 0 {
		t.Fatalf("state = %+v, %v", state, err)
	}
	favorites, err := store.FavoriteIDs(ctx)
	if err != nil || len(favorites) != 1 || favorites[0] != "snes-mario-test" {
		t.Fatalf("favorites = %v, %v", favorites, err)
	}
	recents, err := store.RecentIDs(ctx, 10)
	if err != nil || len(recents) != 2 || recents[0] != "megadrive-sonic-test" {
		t.Fatalf("recents = %v, %v", recents, err)
	}
	unlimited, err := store.RecentIDs(ctx, 0)
	if err != nil || len(unlimited) != 2 {
		t.Fatalf("unlimited recents = %v, %v", unlimited, err)
	}
	capped, err := store.RecentIDs(ctx, 1)
	if err != nil || len(capped) != 1 || capped[0] != "megadrive-sonic-test" {
		t.Fatalf("capped recents = %v, %v", capped, err)
	}
	played, err := store.PlayedIDs(ctx)
	if err != nil || len(played) != 2 {
		t.Fatalf("played = %v, %v", played, err)
	}
	missing, err := store.State(ctx, "nes-missing-test")
	if err != nil || missing.Favorite || missing.PlayCount != 0 {
		t.Fatalf("missing = %+v, %v", missing, err)
	}
}

func TestCollectionsMigrateFromV1AndKeepGameState(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "user.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE game_state (
  game_id TEXT PRIMARY KEY,
  favorite INTEGER NOT NULL DEFAULT 0,
  favorited_at INTEGER,
  last_played_at INTEGER,
  play_count INTEGER NOT NULL DEFAULT 0
);
INSERT INTO game_state(game_id, favorite, favorited_at, last_played_at, play_count)
VALUES('snes-mario-test', 1, 11, 22, 3);
PRAGMA user_version = 1;
`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := libraryuser.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	state, err := store.State(ctx, "snes-mario-test")
	if err != nil || !state.Favorite || state.PlayCount != 3 || state.LastPlayedAt != 22 {
		t.Fatalf("migrated state = %+v, %v", state, err)
	}
	created, err := store.UpsertCollection(ctx, "weekend-queue", "Weekend Queue")
	if err != nil || created.ID != "weekend-queue" || created.Name != "Weekend Queue" || created.CreatedAt == 0 {
		t.Fatalf("create = %+v, %v", created, err)
	}
	if err := store.SetCollectionMember(ctx, created.ID, "snes-mario-test", true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCollectionMember(ctx, created.ID, "megadrive-sonic-test", true); err != nil {
		t.Fatal(err)
	}
	ids, err := store.CollectionGameIDs(ctx, created.ID)
	if err != nil || len(ids) != 2 || ids[0] != "megadrive-sonic-test" || ids[1] != "snes-mario-test" {
		t.Fatalf("members = %v, %v", ids, err)
	}
	if err := store.SetCollectionMember(ctx, created.ID, "snes-mario-test", false); err != nil {
		t.Fatal(err)
	}
	ids, err = store.CollectionGameIDs(ctx, created.ID)
	if err != nil || len(ids) != 1 || ids[0] != "megadrive-sonic-test" {
		t.Fatalf("after remove = %v, %v", ids, err)
	}
	renamed, err := store.UpsertCollection(ctx, created.ID, "Saturday")
	if err != nil || renamed.Name != "Saturday" || renamed.ID != created.ID {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
	listed, err := store.Collections(ctx)
	if err != nil || len(listed) != 1 || listed[0].Name != "Saturday" {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	membership, err := store.GameCollectionIDs(ctx, "megadrive-sonic-test")
	if err != nil || len(membership) != 1 || membership[0] != created.ID {
		t.Fatalf("game collections = %v, %v", membership, err)
	}
	if err := store.DeleteCollection(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Collection(ctx, created.ID); !errors.Is(err, libraryuser.ErrNotFound) {
		t.Fatalf("deleted collection err = %v", err)
	}
	empty, err := store.Collections(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty list = %+v, %v", empty, err)
	}
	state, err = store.State(ctx, "snes-mario-test")
	if err != nil || !state.Favorite || state.PlayCount != 3 {
		t.Fatalf("game_state after collection delete = %+v, %v", state, err)
	}
}

func TestCollectionsRejectReservedAndInvalidIDs(t *testing.T) {
	ctx := context.Background()
	store, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.UpsertCollection(ctx, "favorites", "Favorites"); !errors.Is(err, libraryuser.ErrReservedID) {
		t.Fatalf("reserved = %v", err)
	}
	if _, err := store.UpsertCollection(ctx, "recently-added", "Recently Added"); !errors.Is(err, libraryuser.ErrReservedID) {
		t.Fatalf("hyphen reserved = %v", err)
	}
	if _, err := store.UpsertCollection(ctx, "Not A Slug", "Nope"); !errors.Is(err, libraryuser.ErrInvalid) {
		t.Fatalf("invalid id = %v", err)
	}
	if _, err := store.UpsertCollection(ctx, "ok-list", ""); err != nil {
		t.Fatal(err)
	}
	named, err := store.Collection(ctx, "ok-list")
	if err != nil || named.Name != "ok-list" {
		t.Fatalf("default name = %+v, %v", named, err)
	}
	if err := store.SetCollectionMember(ctx, "missing-list", "snes-mario-test", true); !errors.Is(err, libraryuser.ErrNotFound) && !errors.Is(err, libraryuser.ErrInvalid) {
		if err == nil || !errors.Is(err, libraryuser.ErrNotFound) {
			t.Fatalf("missing collection = %v", err)
		}
	}
	if err := store.SetCollectionMember(ctx, "ok-list", "NotAGame", true); !errors.Is(err, libraryuser.ErrInvalid) {
		t.Fatalf("invalid game = %v", err)
	}
	ids, err := store.CollectionIDsByGame(ctx, []string{"snes-mario-test"})
	if err != nil || len(ids) != 0 {
		t.Fatalf("empty membership map = %v, %v", ids, err)
	}
}
