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

func TestEditionPreferenceSaveLoadAndSkipMissing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "user.sqlite3")
	store, err := libraryuser.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.SetEditionPreference(ctx, "Super Mario Bros.", "NES", "nes-smb-usa")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Query != "super mario bros" || saved.Platform != "nes" || saved.GameID != "nes-smb-usa" || saved.ChosenAt == 0 {
		t.Fatalf("saved = %+v", saved)
	}
	got, err := store.EditionPreference(ctx, "super mario bros.", "nes")
	if err != nil || got.GameID != "nes-smb-usa" || got.Query != "super mario bros" {
		t.Fatalf("load = %+v, %v", got, err)
	}
	replaced, err := store.SetEditionPreference(ctx, "Super Mario Bros", "nes", "nes-smb-jp")
	if err != nil || replaced.GameID != "nes-smb-jp" {
		t.Fatalf("replace = %+v, %v", replaced, err)
	}
	listed, err := store.EditionPreferences(ctx)
	if err != nil || len(listed) != 1 || listed[0].GameID != "nes-smb-jp" {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := libraryuser.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	again, err := reopened.EditionPreference(ctx, "SUPER MARIO BROS.", "NES")
	if err != nil || again.GameID != "nes-smb-jp" {
		t.Fatalf("reopen = %+v, %v", again, err)
	}
	if _, err := reopened.EditionPreference(ctx, "Zelda", "nes"); !errors.Is(err, libraryuser.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	if _, err := reopened.SetEditionPreference(ctx, "", "nes", "nes-smb-usa"); !errors.Is(err, libraryuser.ErrInvalid) {
		t.Fatalf("empty query = %v", err)
	}
	if _, err := reopened.SetEditionPreference(ctx, "Mario", "nes", "Not A Game"); !errors.Is(err, libraryuser.ErrInvalid) {
		t.Fatalf("invalid game = %v", err)
	}
}

func TestEditionPreferenceMigratesFromV2AndKeepsGameState(t *testing.T) {
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
CREATE TABLE collections (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE collection_membership (
  collection_id TEXT NOT NULL,
  game_id TEXT NOT NULL,
  added_at INTEGER NOT NULL,
  PRIMARY KEY (collection_id, game_id),
  FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
);
INSERT INTO game_state(game_id, favorite, favorited_at, last_played_at, play_count)
VALUES('snes-mario-test', 1, 11, 22, 3);
INSERT INTO collections(id, name, created_at) VALUES('weekend-queue', 'Weekend Queue', 1);
PRAGMA user_version = 2;
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
	if err != nil || !state.Favorite || state.PlayCount != 3 {
		t.Fatalf("migrated state = %+v, %v", state, err)
	}
	listed, err := store.Collections(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != "weekend-queue" {
		t.Fatalf("migrated collections = %+v, %v", listed, err)
	}
	saved, err := store.SetEditionPreference(ctx, "Mario", "snes", "snes-mario-test")
	if err != nil || saved.GameID != "snes-mario-test" {
		t.Fatalf("v3 write = %+v, %v", saved, err)
	}
	empty, err := store.EditionPreferences(ctx)
	if err != nil || len(empty) != 1 {
		t.Fatalf("v3 list = %+v, %v", empty, err)
	}
}
