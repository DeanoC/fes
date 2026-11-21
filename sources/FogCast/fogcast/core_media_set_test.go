package fogcast

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestServiceCoreEntryMediaSetFitsTheDrive(t *testing.T) {
	ctx := context.Background()
	f := newShelf(t)
	s, game := f.s, f.game
	second := bytes.Clone(f.gameRaw)
	second[4096] = 0x43
	disk2, _, err := s.ImportCoreMedia(ctx, int64(len(second)), bytes.NewReader(second))
	if err != nil {
		t.Fatal(err)
	}
	set, err := s.SetCoreEntryMediaSet(ctx, game.GameID, f.pkg, game.MediaID, nil, []catalog.SetMedia{{MediaID: disk2.MediaID, MediaName: "DUNGEON2.ST"}})
	if err != nil || !slices.Equal(catalog.SetMediaIDs(set.MoreMedia), []string{disk2.MediaID}) {
		t.Fatalf("set: %+v, %v", set, err)
	}
	// A file the ST drive cannot take is refused, and the set is unchanged.
	tiny, _, err := s.ImportCoreMedia(ctx, 3, bytes.NewReader([]byte("abc")))
	if err != nil {
		t.Fatal(err)
	}
	var apiErr *protocol.APIError
	if _, err := s.SetCoreEntryMediaSet(ctx, game.GameID, f.pkg, game.MediaID, catalog.SetMediaIDs(set.MoreMedia), append(set.MoreMedia, catalog.SetMedia{MediaID: tiny.MediaID, MediaName: "TINY.ST"})); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("undersized disk: %v", err)
	}
	// A caller that read an older set is a conflict.
	if _, err := s.SetCoreEntryMediaSet(ctx, game.GameID, f.pkg, game.MediaID, nil, nil); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeStaleRevision {
		t.Fatalf("stale set: %v", err)
	}
	stored, err := s.CoreEntry(ctx, game.GameID)
	if err != nil || !slices.Equal(catalog.SetMediaIDs(stored.MoreMedia), []string{disk2.MediaID}) {
		t.Fatalf("stored: %+v, %v", stored, err)
	}
	// A tape name cannot go in a disk drive: a live change would take the
	// cassette path.
	if _, err := s.SetCoreEntryMediaSet(ctx, game.GameID, f.pkg, game.MediaID, catalog.SetMediaIDs(stored.MoreMedia), []catalog.SetMedia{{MediaID: disk2.MediaID, MediaName: "DUNGEON2.TAP"}}); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("tape name in a disk set: %v", err)
	}
	// A title with no disk cannot start a set.
	bare, err := s.CreateCoreEntry(ctx, "Bare ST", f.pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, bare.GameID, f.pkg, disk2.MediaID, nil, nil); err == nil {
		t.Fatal("a bare title accepted a set")
	}
}

func TestServicePackageChangeRechecksEveryDiskOfTheSet(t *testing.T) {
	ctx := context.Background()
	f := newShelf(t)
	s, game := f.s, f.game
	// A single-sided 360K disk fits a drive that takes other geometries.
	disk2, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{Name: "SIDE2", Tracks: 80, Heads: 1, Sectors: 9})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, game.GameID, f.pkg, game.MediaID, nil, []catalog.SetMedia{{MediaID: disk2.MediaID, MediaName: "SIDE2.ST"}}); err != nil {
		t.Fatal(err)
	}
	raw := atariLibraryPackage(t, true, true, false, func(text string) string { return strings.Replace(text, `version = "`, `version = "9`, 1) })
	fixed, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntry(ctx, game.GameID, f.pkg, fixed.PackageID); err == nil {
		t.Fatal("a package whose drive cannot take disk 2 was selected")
	}
	stored, err := s.CoreEntry(ctx, game.GameID)
	if err != nil || stored.PackageID != f.pkg {
		t.Fatalf("entry changed: %+v, %v", stored, err)
	}
}
