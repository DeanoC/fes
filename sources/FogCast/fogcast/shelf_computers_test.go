package fogcast

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
)

func TestShelfComputersIncludesPreparedFamiliesAndPrefersBareSetup(t *testing.T) {
	ctx := context.Background()
	archive := libraryPackageFixture(t, "1.0.0")
	s, _, _, _ := newCoreEntryLaunchFixture(t, archive, "Pong")
	store := s.catalog.(*catalog.Store)
	cores := []string{"fes.apple2", "fes.atari-st", "fes.c64", "fes.catch", "fes.coleco", "fes.pentagon", "fes.ramtest", "fes.riscv", "fes.sg1000", "fes.sms", "fes.spectrum", "fes.spectrum128", "fes.spectrum-plus3", "fes.ts1000", "fes.ts1500", "fes.zx80"}
	for _, coreID := range cores {
		raw, _ := apple2LibraryPackageFixture(t, false, func(manifest string) string { return strings.Replace(manifest, "fes.apple2", coreID, 1) })
		installed, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateCoreEntry(ctx, coreID, coreID, installed.PackageID); err != nil {
			t.Fatal(err)
		}
	}
	// MENU is firmware even though its manifest shares an application ABI.
	raw, _ := apple2LibraryPackageFixture(t, false, func(manifest string) string { return strings.Replace(manifest, "fes.apple2", "fes.menu", 1) })
	installed, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCoreEntry(ctx, "Menu firmware", "fes.menu", installed.PackageID); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ShelfComputers(ctx)
	if err != nil || len(listed) != len(cores)+1 {
		t.Fatalf("families %+v err=%v", listed, err)
	}
	for i, card := range listed {
		if !card.Ready || card.CoreID == "fes.menu" {
			t.Fatalf("invalid shelf entry %+v", card)
		}
		if i > 0 && listed[i-1].CoreID >= card.CoreID {
			t.Fatalf("shelf order is not stable: %+v", listed)
		}
	}

	// Prefer a diskless computer over a title on the same core, while keeping
	// the title selectable from the room's complete library list.
	diskArchive, _ := apple2LibraryPackageFixture(t, false)
	installed, _, err = s.ImportCorePackage(ctx, int64(len(diskArchive)), bytes.NewReader(diskArchive))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateCoreEntry(ctx, "Z bare Apple II", "fes.apple2", installed.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	disk := bytes.Repeat([]byte{0}, 143360)
	media, _, err := s.ImportCoreMedia(ctx, int64(len(disk)), bytes.NewReader(disk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCoreMediaEntry(ctx, "A disk title", "fes.apple2", installed.PackageID, "disk", media.MediaID); err != nil {
		t.Fatal(err)
	}
	listed, err = s.ShelfComputers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range listed {
		if card.CoreID == "fes.apple2" && card.Title == "A disk title" {
			t.Fatalf("media title displaced a ready bare machine: %+v", card)
		}
	}
}
