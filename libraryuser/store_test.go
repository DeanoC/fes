package libraryuser_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast-POC/libraryuser"
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
