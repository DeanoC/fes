package fogcast

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

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
	set, err := s.SetCoreEntryMediaSet(ctx, game.GameID, f.pkg, game.MediaID, nil, []string{disk2.MediaID})
	if err != nil || !slices.Equal(set.MoreMedia, []string{disk2.MediaID}) {
		t.Fatalf("set: %+v, %v", set, err)
	}
	// A file the ST drive cannot take is refused, and the set is unchanged.
	tiny, _, err := s.ImportCoreMedia(ctx, 3, bytes.NewReader([]byte("abc")))
	if err != nil {
		t.Fatal(err)
	}
	var apiErr *protocol.APIError
	if _, err := s.SetCoreEntryMediaSet(ctx, game.GameID, f.pkg, game.MediaID, set.MoreMedia, []string{disk2.MediaID, tiny.MediaID}); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("undersized disk: %v", err)
	}
	// A caller that read an older set is a conflict.
	if _, err := s.SetCoreEntryMediaSet(ctx, game.GameID, f.pkg, game.MediaID, nil, []string{}); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeStaleRevision {
		t.Fatalf("stale set: %v", err)
	}
	stored, err := s.CoreEntry(ctx, game.GameID)
	if err != nil || !slices.Equal(stored.MoreMedia, []string{disk2.MediaID}) {
		t.Fatalf("stored: %+v, %v", stored, err)
	}
	// A title with no disk cannot start a set.
	bare, err := s.CreateCoreEntry(ctx, "Bare ST", f.pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, bare.GameID, f.pkg, disk2.MediaID, nil, []string{}); err == nil {
		t.Fatal("a bare title accepted a set")
	}
}
