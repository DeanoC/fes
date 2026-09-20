package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/DeanoC/misteross/expansion"
	"strings"
	"testing"
)

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
	loaded, err := reopened.ReadCoreExpansion(ctx, asset.ID)
	if err != nil || !bytes.Equal(loaded.Cart, cart) {
		t.Fatalf("asset reload %v", err)
	}
	if _, err = reopened.SelectCoreEntryExpansion(ctx, entry.GameID, packageID, asset.ID, ""); err != nil {
		t.Fatal(err)
	}
}
