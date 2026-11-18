package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestShelfDiskMarkFollowsItsEntry(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	defer s.Close()
	pkg := strings.Repeat("a", 64)
	media, _, err := s.ImportCoreMedia(ctx, []byte("a disk image"))
	if err != nil {
		t.Fatal(err)
	}
	made, err := s.CreateCoreMediaEntry(ctx, "Work disk", "fes.atari-st", pkg, "disk", media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	game, err := s.CreateCoreMediaEntry(ctx, "A game", "fes.atari-st", pkg, "disk", media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	bare, err := s.CreateCoreEntry(ctx, "No disk", "fes.atari-st", pkg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.MarkShelfDisk(ctx, made.GameID); err != nil {
			t.Fatalf("mark %d: %v", i, err)
		}
	}
	// Only an entry that selects a disk can be on a shelf.
	if err := s.MarkShelfDisk(ctx, bare.GameID); !errors.Is(err, ErrCoreEntryNotFound) {
		t.Fatalf("diskless entry marked: %v", err)
	}
	if err := s.MarkShelfDisk(ctx, "no-such-disk"); !errors.Is(err, ErrCoreEntryNotFound) {
		t.Fatalf("missing entry marked: %v", err)
	}
	got, err := s.ShelfDisks(ctx, "fes.atari-st")
	if err != nil || len(got) != 1 {
		t.Fatalf("shelf: %v %v", got, err)
	}
	if _, ok := got[made.GameID]; !ok {
		t.Fatalf("made disk missing: %v", got)
	}
	if other, err := s.ShelfDisks(ctx, "fes.pcxt-dos"); err != nil || len(other) != 0 {
		t.Fatalf("another family's shelf: %v %v", other, err)
	}
	// Removing the entry removes the mark, so a later disk with the same
	// name does not inherit it.
	if err := s.RemoveCoreEntry(ctx, made.GameID); err != nil {
		t.Fatal(err)
	}
	if got, err = s.ShelfDisks(ctx, "fes.atari-st"); err != nil || len(got) != 0 {
		t.Fatalf("shelf after removal: %v %v", got, err)
	}
	again, err := s.CreateCoreMediaEntry(ctx, "Work disk", "fes.atari-st", pkg, "disk", media.MediaID)
	if err != nil || again.GameID != made.GameID {
		t.Fatalf("recreated: %+v %v", again, err)
	}
	if got, err = s.ShelfDisks(ctx, "fes.atari-st"); err != nil || len(got) != 0 {
		t.Fatalf("recreated entry inherited the mark: %v %v", got, err)
	}
	_ = game
}
