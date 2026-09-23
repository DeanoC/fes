package catalog_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
)

func TestCoreROMSelectionBindsExactPackageRequirementAndBinary(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	pkg := strings.Repeat("a", 64)
	entry, err := s.CreateCoreEntry(ctx, "ROM title", "fes.pong", pkg)
	if err != nil {
		t.Fatal(err)
	}
	media, _, err := s.ImportCoreMedia(ctx, []byte("exact ROM"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		pkg, expected, id string
		size              int64
		want              error
	}{
		{pkg, "", media.MediaID, 8, catalog.ErrInvalidCoreROM},
		{pkg, "", "bad", 9, catalog.ErrInvalidCoreROM},
		{pkg, "", strings.Repeat("f", 64), 9, catalog.ErrCoreMediaNotFound},
		{strings.Repeat("b", 64), "", media.MediaID, 9, catalog.ErrCoreEntryConflict},
		{pkg, strings.Repeat("c", 64), media.MediaID, 9, catalog.ErrCoreEntryConflict},
	} {
		_, err := s.SelectCoreEntryROM(ctx, entry.GameID, tc.pkg, "machine", tc.size, tc.expected, tc.id)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	got, err := s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "machine", 9, "", media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PackageID != pkg || got.ROMID != "machine" || got.MediaID != media.MediaID || got.SourceSize != 9 {
		t.Fatalf("%+v", got)
	}
	if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "machine", 9, "", ""); !errors.Is(err, catalog.ErrCoreEntryConflict) {
		t.Fatalf("stale clear: %v", err)
	}
	if _, err = s.SelectCoreEntry(ctx, entry.GameID, "fes.pong", pkg, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	old, err := s.CoreEntryROM(ctx, entry.GameID)
	if err != nil || old.PackageID != pkg {
		t.Fatalf("ROM rebound implicitly: %+v %v", old, err)
	}
	if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "machine", 9, media.MediaID, ""); !errors.Is(err, catalog.ErrCoreEntryConflict) {
		t.Fatalf("stale package accepted: %v", err)
	}
	cleared, err := s.SelectCoreEntryROM(ctx, entry.GameID, strings.Repeat("b", 64), "machine", 9, media.MediaID, "")
	if err != nil || cleared.MediaID != "" {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
}
