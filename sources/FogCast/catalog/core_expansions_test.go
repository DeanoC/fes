package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"github.com/DeanoC/misteross/expansion"
	"strings"
	"testing"
)

func TestExpansionProgressMigrationPreservesExistingSelection(t *testing.T) {
	ctx := context.Background()
	store, path := mediaTestStore(t)
	cart := bytes.Repeat([]byte{0x55}, 45000)
	pkg := strings.Repeat("a", 64)
	asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)), Device: expansion.Device, Format: 1, Map: expansion.Map, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: strings.Repeat("d", 32), ShellPackageID: pkg, ShellSHA256: strings.Repeat("e", 64), Slot: expansion.Slot, SlotMajor: 1}, cart)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportCoreExpansion(ctx, asset); err != nil {
		t.Fatal(err)
	}
	entry, err := store.CreateCoreEntry(ctx, "Existing Zx81", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.SelectCoreEntryExpansion(ctx, entry.GameID, pkg, "", asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetCoreExpansionPresentation(ctx, asset.ID, "Existing card", "Keep this description"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the previous schema without touching its saved records.
	_, err = db.ExecContext(ctx, "DROP TABLE core_video_parts; ALTER TABLE core_expansions DROP COLUMN in_progress; PRAGMA user_version = 15;")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.CoreEntryExpansion(ctx, entry.GameID)
	if err != nil || got != selection {
		t.Fatalf("migration changed selection: %+v %v", got, err)
	}
	presentation, err := reopened.CoreExpansionPresentation(ctx, asset.ID)
	if err != nil || presentation.Label != "Existing card" || presentation.Description != "Keep this description" || presentation.InProgress {
		t.Fatalf("migration changed presentation: %+v %v", presentation, err)
	}
}

func TestExpansionSelectionPersistsAndRejectsStaleBinding(t *testing.T) {
	ctx := context.Background()
	store, path := mediaTestStore(t)
	packageID := strings.Repeat("a", 64)
	cart := bytes.Repeat([]byte{0x55}, 45000)
	asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)), Device: expansion.Device, Format: 1, Map: expansion.Map, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: strings.Repeat("d", 32), ShellPackageID: packageID, ShellSHA256: strings.Repeat("e", 64), Slot: expansion.Slot, SlotMajor: 1}, cart)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := store.ImportCoreExpansion(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := store.CreateCoreEntry(ctx, "ZX81", "fes.zx81", packageID)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := store.SelectCoreEntryExpansion(ctx, entry.GameID, packageID, "", asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ExpansionID != imported.ExpansionID {
		t.Fatal("wrong selection")
	}
	copy, err := store.SetCoreExpansionPresentation(ctx, asset.ID, " Household test card ", " A synthetic fixture, not a RAM capacity claim. ")
	if err != nil || copy.Label != "Household test card" || copy.ExpansionID != asset.ID {
		t.Fatalf("presentation %+v %v", copy, err)
	}
	copy, err = store.SetCoreExpansionPresentationWithProgress(ctx, asset.ID, copy.Label, copy.Description, true)
	if err != nil || !copy.InProgress {
		t.Fatalf("progress badge not stored: %+v %v", copy, err)
	}
	copy, err = store.SetCoreExpansionPresentation(ctx, asset.ID, copy.Label, copy.Description)
	if err != nil || !copy.InProgress {
		t.Fatalf("older presentation edit erased badge: %+v %v", copy, err)
	}
	for _, invalid := range []struct{ label, description string }{
		{"line\nbreak", ""}, {strings.Repeat("x", 121), ""}, {"test", strings.Repeat("x", 2001)}, {"test", "escape\x1b"}, {"\xff", ""},
	} {
		if _, err := store.SetCoreExpansionPresentation(ctx, asset.ID, invalid.label, invalid.description); err != ErrInvalidCoreExpansion {
			t.Fatalf("invalid presentation accepted: %v", err)
		}
	}
	if _, err = store.SelectCoreEntryExpansion(ctx, entry.GameID, packageID, "", ""); err != ErrCoreEntryConflict {
		t.Fatalf("stale selection: %v", err)
	}
	if _, err = store.SelectCoreEntryExpansion(ctx, entry.GameID, strings.Repeat("f", 64), asset.ID, ""); err != ErrCoreEntryConflict {
		t.Fatalf("stale package: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.CoreEntryExpansion(ctx, entry.GameID)
	if err != nil || got != selected {
		t.Fatalf("reopened %v %v", got, err)
	}
	retained, err := reopened.CoreExpansionPresentation(ctx, asset.ID)
	if err != nil || retained != copy {
		t.Fatalf("presentation not retained: %+v %v", retained, err)
	}
	listed, err := reopened.CoreExpansions(ctx)
	if err != nil || len(listed) != 1 || listed[0].Label != copy.Label || listed[0].Description != copy.Description || !listed[0].InProgress {
		t.Fatalf("inventory lost presentation: %+v %v", listed, err)
	}
	loaded, err := reopened.ReadCoreExpansion(ctx, asset.ID)
	if err != nil || !bytes.Equal(loaded.Cart, cart) {
		t.Fatalf("asset reload %v", err)
	}
	if _, err = reopened.SelectCoreEntryExpansion(ctx, entry.GameID, packageID, asset.ID, ""); err != nil {
		t.Fatal(err)
	}
}
