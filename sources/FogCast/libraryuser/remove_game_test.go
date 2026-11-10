package libraryuser

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRemoveGameReferencesOnlyClearsTheSelectedTitle(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "user.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"game-one", "game-two"} {
		if err := s.SetFavorite(ctx, id, true); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordPlay(ctx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetEditionPreference(ctx, id, "fpga", id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.UpsertCollection(ctx, "test-list", "Test list"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"game-one", "game-two"} {
		if err := s.SetCollectionMember(ctx, "test-list", id, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RemoveGameReferences(ctx, "game-one"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGameReferences(ctx, "game-one"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	removed, err := s.State(ctx, "game-one")
	if err != nil || removed.Favorite || removed.LastPlayedAt != 0 || removed.PlayCount != 0 {
		t.Fatalf("removed state %+v %v", removed, err)
	}
	other, err := s.State(ctx, "game-two")
	if err != nil || !other.Favorite || other.LastPlayedAt == 0 || other.PlayCount != 1 {
		t.Fatalf("other state %+v %v", other, err)
	}
	ids, err := s.CollectionGameIDs(ctx, "test-list")
	if err != nil || len(ids) != 1 || ids[0] != "game-two" {
		t.Fatalf("membership %v %v", ids, err)
	}
	if _, err := s.EditionPreference(ctx, "game-one", "fpga"); err == nil {
		t.Fatal("removed edition remains")
	}
	if pref, err := s.EditionPreference(ctx, "game-two", "fpga"); err != nil || pref.GameID != "game-two" {
		t.Fatalf("other edition %+v %v", pref, err)
	}
	if err := s.RemoveGameReferences(ctx, "Bad ID"); err == nil {
		t.Fatal("invalid ID accepted")
	}
}

func TestRemoveGameReferencesRollsBackOnPartialFailure(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "user.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetFavorite(ctx, "game-one", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertCollection(ctx, "mine", "Mine"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCollectionMember(ctx, "mine", "game-one", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER fail_game_cleanup BEFORE DELETE ON collection_membership BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGameReferences(ctx, "game-one"); err == nil {
		t.Fatal("failed cleanup returned success")
	}
	state, err := s.State(ctx, "game-one")
	if err != nil || !state.Favorite {
		t.Fatalf("favorite not rolled back: %+v %v", state, err)
	}
	ids, err := s.CollectionGameIDs(ctx, "mine")
	if err != nil || len(ids) != 1 || ids[0] != "game-one" {
		t.Fatalf("membership changed: %v %v", ids, err)
	}
}
