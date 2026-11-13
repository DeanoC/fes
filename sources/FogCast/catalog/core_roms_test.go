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
	old, err := s.CoreEntryROM(ctx, entry.GameID, "machine")
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

func TestNamedROMSelectionsAreIndependent(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	pkg := strings.Repeat("a", 64)
	entry, err := s.CreateCoreEntry(ctx, "Coleco game", "fes.coleco", pkg)
	if err != nil {
		t.Fatal(err)
	}
	bios, _, err := s.ImportCoreMedia(ctx, []byte("bios"))
	if err != nil {
		t.Fatal(err)
	}
	cart, _, err := s.ImportCoreMedia(ctx, []byte("cartridge"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "bios", 4, "", bios.MediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "cart", 9, "", cart.MediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "bios", 4, cart.MediaID, ""); !errors.Is(err, catalog.ErrCoreEntryConflict) {
		t.Fatalf("accepted another ROM's digest for CAS: %v", err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "bios", 4, bios.MediaID, ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.CoreEntryROM(ctx, entry.GameID, "cart")
	if err != nil || got.MediaID != cart.MediaID || got.ROMID != "cart" {
		t.Fatalf("clearing BIOS changed cartridge: %+v %v", got, err)
	}
	got, err = s.CoreEntryROM(ctx, entry.GameID, "bios")
	if err != nil || got.MediaID != "" {
		t.Fatalf("BIOS was not cleared: %+v %v", got, err)
	}
}

func TestCreateCoreROMEntryIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	pkg := strings.Repeat("a", 64)
	media, _, err := s.ImportCoreMedia(ctx, []byte("cartridge"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mediaID string
		size    int64
		want    error
	}{
		{media.MediaID, 8, catalog.ErrInvalidCoreROM},
		{strings.Repeat("b", 64), 9, catalog.ErrCoreMediaNotFound},
	} {
		if _, err := s.CreateCoreROMEntry(ctx, "Atomic cartridge", "fes.coleco", pkg, "cart", tc.size, tc.mediaID, false); !errors.Is(err, tc.want) {
			t.Fatalf("invalid initial ROM: got %v, want %v", err, tc.want)
		}
		entries, err := s.CoreEntries(ctx)
		if err != nil || len(entries) != 0 {
			t.Fatalf("failed creation retained entry: %+v, %v", entries, err)
		}
	}
	entry, err := s.CreateCoreROMEntry(ctx, "Atomic cartridge", "fes.coleco", pkg, "cart", 9, media.MediaID, true)
	if err != nil {
		t.Fatal(err)
	}
	rom, err := s.CoreEntryROM(ctx, entry.GameID, "cart")
	if err != nil || rom.PackageID != pkg || rom.MediaID != media.MediaID || rom.SourceSize != 9 || !entry.FirmwareRequired {
		t.Fatalf("entry=%+v ROM=%+v err=%v", entry, rom, err)
	}
}
