package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestRemoteCoreEntryMigrationAndVisibleDistinctTitles(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	local, err := s.CreateCoreEntry(ctx, "Guess", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct a schema-21 catalog containing an existing user title.
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER remote_core_entry_selected; DROP TABLE remote_core_entries; PRAGMA user_version = 21`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.CoreEntry(ctx, local.GameID); err != nil || got != local {
		t.Fatalf("prior local title = %+v, %v", got, err)
	}
	origin := RemoteCoreEntryOrigin{SourceID: "remote-a", SourceGameID: "source-game", MediaID: strings.Repeat("b", 64), MediaName: "guess.p"}
	first, err := s.CreateRemoteCoreEntry(ctx, "Guess", "fes.zx81", pkg, origin)
	if err != nil {
		t.Fatal(err)
	}
	origin.SourceID = "remote-b"
	second, err := s.CreateRemoteCoreEntry(ctx, "Guess", "fes.zx81", pkg, origin)
	if err != nil {
		t.Fatal(err)
	}
	if first.GameID == local.GameID || first.GameID == second.GameID || second.GameID == local.GameID || first.MediaID != "" || first.MediaRole != "" {
		t.Fatalf("remote entries not distinct or unexpectedly selected: local=%+v first=%+v second=%+v", local, first, second)
	}
	entries, err := s.CoreEntries(ctx)
	if err != nil || len(entries) != 3 {
		t.Fatalf("visible entries = %+v, %v", entries, err)
	}
	page, err := s.QueryGames(ctx, Query{Platform: CorePlatform, Limit: 10})
	if err != nil || len(page.Games) != 3 {
		t.Fatalf("visible games = %+v, %v", page.Games, err)
	}
	machines, err := s.QueryGames(ctx, Query{Platform: CorePlatform, CoreEntryKind: "machine", Limit: 10})
	if err != nil || len(machines.Games) != 1 || machines.Games[0].ID != local.GameID {
		t.Fatalf("machine filter = %+v, %v", machines, err)
	}
	titles, err := s.QueryGames(ctx, Query{Platform: CorePlatform, CoreEntryKind: "title", Limit: 1})
	if err != nil || len(titles.Games) != 1 || titles.NextCursor == "" {
		t.Fatalf("pending remote title filter = %+v, %v", titles, err)
	}
	next, err := s.QueryGames(ctx, Query{Platform: CorePlatform, CoreEntryKind: "title", Limit: 1, Cursor: titles.NextCursor})
	if err != nil || len(next.Games) != 1 || next.NextCursor != "" || next.Games[0].ID == titles.Games[0].ID {
		t.Fatalf("next title page = %+v, %v", next, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	gotEntry, gotOrigin, found, err := reopened.RemoteCoreEntryBySource(ctx, "remote-a", "source-game")
	if err != nil || !found || gotEntry != first || gotOrigin.PackageID != pkg || gotOrigin.MediaID != strings.Repeat("b", 64) || !gotOrigin.Pending {
		t.Fatalf("reopened mapping = %+v, %+v, %v, %v", gotEntry, gotOrigin, found, err)
	}
	for _, item := range []struct {
		id   string
		want bool
	}{{first.GameID, true}, {local.GameID, false}} {
		found, err := reopened.IsRemoteSourceEntry(ctx, item.id)
		if err != nil || found != item.want {
			t.Fatalf("is remote %s = %v, %v", item.id, found, err)
		}
	}
}

func TestRemoteCoreEntryCreatedBeforeLocalSameTitle(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	_, err := s.CreateRemoteCoreEntry(ctx, "Guess", "fes.zx81", pkg, RemoteCoreEntryOrigin{SourceID: "home", SourceGameID: "remote-game", MediaID: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCoreEntry(ctx, "Guess", "fes.zx81", pkg); err != nil {
		t.Fatalf("local title after remote: %v", err)
	}
}

func TestRemoteCoreEntryBindAndIdempotentSyncPreservesEdits(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	media, _, err := s.ImportCoreMedia(ctx, []byte("cassette"))
	if err != nil {
		t.Fatal(err)
	}
	origin := RemoteCoreEntryOrigin{SourceID: "home", SourceGameID: "remote-game", MediaID: media.MediaID, MediaName: "guess.p"}
	entry, err := s.CreateRemoteCoreEntry(ctx, "Guess", "fes.zx81", pkg, origin)
	if err != nil {
		t.Fatal(err)
	}
	saved, found, err := s.RemoteCoreEntry(ctx, entry.GameID)
	if err != nil || !found || !saved.Pending || saved.PackageID != pkg {
		t.Fatalf("origin = %+v, %v, %v", saved, found, err)
	}
	bound, err := s.BindRemoteCoreEntryMedia(ctx, entry.GameID, saved)
	if err != nil || bound.MediaID != media.MediaID || bound.MediaRole != protocol.CassetteRole || bound.MediaName != "guess.p" {
		t.Fatalf("bound = %+v, %v", bound, err)
	}
	got, _, err := s.RemoteCoreEntry(ctx, entry.GameID)
	if err != nil || got.Pending {
		t.Fatalf("bound origin = %+v, %v", got, err)
	}
	if _, err := s.BindRemoteCoreEntryMedia(ctx, entry.GameID, saved); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("replay bind = %v", err)
	}
	// The source may change title, package and content on a later scan. It
	// must not rewrite the existing local title or its provenance snapshot.
	resync := origin
	resync.MediaID = strings.Repeat("c", 64)
	resync.MediaName = "new.p"
	again, err := s.CreateRemoteCoreEntry(ctx, "Renamed", "fes.zx81", strings.Repeat("b", 64), resync)
	if err != nil || again != bound {
		t.Fatalf("resync = %+v, %v", again, err)
	}
	unchanged, _, err := s.RemoteCoreEntry(ctx, entry.GameID)
	if err != nil || unchanged != got {
		t.Fatalf("origin changed = %+v, %v", unchanged, err)
	}
}

func TestRemoteCoreEntryPendingBindHonorsExplicitNoopSelections(t *testing.T) {
	for _, mode := range []string{"media", "package"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _ := mediaTestStore(t)
			pkg := strings.Repeat("a", 64)
			media, _, err := s.ImportCoreMedia(ctx, []byte("cassette"))
			if err != nil {
				t.Fatal(err)
			}
			entry, err := s.CreateRemoteCoreEntry(ctx, "Guess", "fes.zx81", pkg, RemoteCoreEntryOrigin{SourceID: "home", SourceGameID: "remote-game", MediaID: media.MediaID})
			if err != nil {
				t.Fatal(err)
			}
			origin, _, err := s.RemoteCoreEntry(ctx, entry.GameID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "media" {
				_, err = s.SelectCoreEntryMedia(ctx, entry.GameID, pkg, "", "", "")
			} else {
				_, err = s.SelectCoreEntry(ctx, entry.GameID, "fes.zx81", pkg, pkg)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.BindRemoteCoreEntryMedia(ctx, entry.GameID, origin); !errors.Is(err, ErrCoreEntryConflict) {
				t.Fatalf("stale bind = %v", err)
			}
			after, _, err := s.RemoteCoreEntry(ctx, entry.GameID)
			if err != nil || after.Pending {
				t.Fatalf("origin after edit = %+v, %v", after, err)
			}
			current, err := s.CoreEntry(ctx, entry.GameID)
			if err != nil || current.MediaID != "" {
				t.Fatalf("selection overwritten = %+v, %v", current, err)
			}
		})
	}
}

func TestRemoteCoreEntryRejectsInvalidOriginAndMissingMedia(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	for _, origin := range []RemoteCoreEntryOrigin{
		{SourceID: "../bad", SourceGameID: "game", MediaID: strings.Repeat("b", 64)},
		{SourceID: "home", SourceGameID: "game", MediaID: "bad"},
		{SourceID: "home", SourceGameID: "game", MediaID: strings.Repeat("b", 64), MediaName: "../game.p"},
		{SourceID: "home", SourceGameID: "game", MediaID: strings.Repeat("b", 64), PackageID: strings.Repeat("c", 64)},
	} {
		if _, err := s.CreateRemoteCoreEntry(ctx, "Game", "fes.zx81", pkg, origin); !errors.Is(err, ErrInvalidCoreEntry) {
			t.Fatalf("origin %+v: %v", origin, err)
		}
	}
	entry, err := s.CreateRemoteCoreEntry(ctx, "Game", "fes.zx81", pkg, RemoteCoreEntryOrigin{SourceID: "home", SourceGameID: "game", MediaID: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	origin, _, err := s.RemoteCoreEntry(ctx, entry.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BindRemoteCoreEntryMedia(ctx, entry.GameID, origin); err == nil {
		t.Fatal("bound nonexistent media")
	}
	after, _, err := s.RemoteCoreEntry(ctx, entry.GameID)
	if err != nil || !after.Pending {
		t.Fatalf("failed bind changed origin = %+v, %v", after, err)
	}
}
