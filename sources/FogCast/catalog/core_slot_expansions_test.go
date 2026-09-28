package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func slotCardAsset(t *testing.T, packageID string, slot int, fill byte) expansion.Asset {
	t.Helper()
	cart := bytes.Repeat([]byte{fill}, 45000)
	asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)), Device: expansion.Device,
		Format: 1, Map: expansion.Apple2Map, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: strings.Repeat("d", 32),
		ShellPackageID: packageID, ShellSHA256: strings.Repeat("e", 64), Slot: expansion.Apple2Slot, SlotIndex: slot, SlotMajor: 1}, cart)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestSlotExpansionSelectionIsPerSlotCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	store, path := mediaTestStore(t)
	packageID := strings.Repeat("a", 64)
	slot2, slot4 := slotCardAsset(t, packageID, 2, 0x22), slotCardAsset(t, packageID, 4, 0x44)
	for _, asset := range []expansion.Asset{slot2, slot4} {
		imported, err := store.ImportCoreExpansion(ctx, asset)
		if err != nil || imported.Slot != asset.Manifest.SlotIndex {
			t.Fatalf("import %+v %v", imported, err)
		}
	}
	listed, err := store.CoreExpansions(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list %+v %v", listed, err)
	}
	encoded, _ := json.Marshal(listed)
	if !strings.Contains(string(encoded), `"slot":2`) {
		t.Fatalf("slot omitted from inventory: %s", encoded)
	}
	entry, err := store.CreateCoreEntry(ctx, "Apple II", "fes.apple2", packageID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, packageID, 2, "", slot2.ID)
	if err != nil || !reflect.DeepEqual(got, []CoreEntrySlotExpansion{{Slot: 2, ExpansionID: slot2.ID}}) {
		t.Fatalf("select slot 2 %+v %v", got, err)
	}
	got, err = store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, packageID, 4, "", slot4.ID)
	if err != nil || len(got) != 2 || got[1] != (CoreEntrySlotExpansion{Slot: 4, ExpansionID: slot4.ID}) {
		t.Fatalf("select slot 4 %+v %v", got, err)
	}
	for name, call := range map[string]func() error{
		"stale expected": func() error {
			_, err := store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, packageID, 2, "", slot2.ID)
			return err
		},
		"stale package": func() error {
			_, err := store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, strings.Repeat("f", 64), 2, slot2.ID, "")
			return err
		},
	} {
		if err := call(); err != ErrCoreEntryConflict {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, packageID, 5, "", slot4.ID); err != ErrInvalidCoreExpansion {
		t.Fatalf("card for another slot: %v", err)
	}
	if _, err := store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, packageID, 5, "", strings.Repeat("9", 64)); err != ErrCoreExpansionNotFound {
		t.Fatalf("missing card: %v", err)
	}
	for _, slot := range []int{0, 8} {
		if _, err := store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, packageID, slot, "", ""); err != ErrInvalidCoreExpansion {
			t.Fatalf("slot %d: %v", slot, err)
		}
	}
	if _, err := store.SelectCoreEntrySlotExpansion(ctx, strings.Repeat("z", 8), packageID, 2, "", ""); err != ErrCoreEntryNotFound {
		t.Fatalf("missing entry: %v", err)
	}
	other := slotCardAsset(t, strings.Repeat("b", 64), 5, 0x55)
	if _, err := store.ImportCoreExpansion(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, packageID, 5, "", other.ID); err != ErrInvalidCoreExpansion {
		t.Fatalf("card for another shell: %v", err)
	}
	if single, err := store.CoreEntryExpansion(ctx, entry.GameID); err != nil || single.ExpansionID != "" {
		t.Fatalf("slot cards leaked into the single expansion: %+v %v", single, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.CoreEntrySlotExpansions(ctx, entry.GameID)
	if err != nil || len(got) != 2 || got[0].Slot != 2 {
		t.Fatalf("reopened %+v %v", got, err)
	}
	loaded, err := reopened.ReadCoreExpansion(ctx, slot4.ID)
	if err != nil || loaded.Manifest.SlotIndex != 4 {
		t.Fatalf("reload %v", err)
	}
	got, err = reopened.SelectCoreEntrySlotExpansion(ctx, entry.GameID, packageID, 2, slot2.ID, "")
	if err != nil || !reflect.DeepEqual(got, []CoreEntrySlotExpansion{{Slot: 4, ExpansionID: slot4.ID}}) {
		t.Fatalf("clear %+v %v", got, err)
	}
}

// Schemas 12 and 13 keep every existing selection: 12 adds slot cards and
// 13 rebuilds core_entries for the disk role without cascading its children.
func TestSchemaMigrationsKeepExistingSelections(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{schemaV1, schemaV2, schemaV3, schemaV4, schemaV5, schemaV6} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	connection, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateCoreMedia(ctx, connection); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{schemaV8, schemaV9, schemaV10, schemaV11} {
		if _, err := connection.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	packageID := strings.Repeat("a", 64)
	gameID := GameID(CorePlatform, corePackageLibraryID, "fes.zx81", "ZX81")
	mediaID := "ef9443c2787cd02b6d78d233d015b0bbf3fb21d53d1a5890497cdbb7897f053c"
	expansionID := strings.Repeat("c", 64)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO libraries(id,system,root,online) VALUES (?,?,?,1)`, []any{corePackageLibraryID, CorePlatform, corePackageRoot}},
		{`INSERT INTO games(game_id,library_id,system,relative_path,title,source_kind,source_state,source_size,modified_ns,seen_generation,search_text,group_key,first_seen_ns) VALUES (?,?,?,?,?,?,?,0,0,0,?,?,1)`,
			[]any{gameID, corePackageLibraryID, CorePlatform, gameID, "ZX81", SourceKindCorePackage, SourceStateAvailable, "zx81", gameID}},
		{`INSERT INTO core_entries(game_id,core_id,package_id) VALUES (?,?,?)`, []any{gameID, "fes.zx81", packageID}},
		{`INSERT INTO core_expansions(expansion_id,media_id,shell_package_id) VALUES (?,?,?)`, []any{expansionID, mediaID, packageID}},
		{`INSERT INTO core_entry_expansions(game_id,expansion_id) VALUES (?,?)`, []any{gameID, expansionID}},
		{`INSERT INTO core_entry_roms(game_id,package_id,rom_id,media_id,source_size) VALUES (?,?,?,?,?)`, []any{gameID, packageID, "machine-rom", mediaID, 8192}},
		{`UPDATE core_entries SET media_role='blob', media_id=?, firmware_required=1 WHERE game_id=?`, []any{mediaID, gameID}},
	} {
		if _, err := connection.ExecContext(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	single, err := store.CoreEntryExpansion(ctx, gameID)
	if err != nil || single.ExpansionID != expansionID {
		t.Fatalf("single expansion lost: %+v %v", single, err)
	}
	listed, err := store.CoreExpansions(ctx)
	if err != nil || !reflect.DeepEqual(listed, []CoreExpansion{{ExpansionID: expansionID, PackageID: packageID}}) {
		t.Fatalf("inventory %+v %v", listed, err)
	}
	slots, err := store.CoreEntrySlotExpansions(ctx, gameID)
	if err != nil || len(slots) != 0 {
		t.Fatalf("slots %+v %v", slots, err)
	}
	rom, err := store.CoreEntryROM(ctx, gameID)
	if err != nil || rom.MediaID != mediaID || rom.ROMID != "machine-rom" {
		t.Fatalf("ROM selection lost: %+v %v", rom, err)
	}
	entry, err := store.CoreEntry(ctx, gameID)
	if err != nil || entry.MediaRole != "blob" || entry.MediaID != mediaID || !entry.FirmwareRequired || entry.PackageID != packageID {
		t.Fatalf("entry changed: %+v %v", entry, err)
	}
	disk, err := store.SelectCoreEntryMedia(ctx, gameID, packageID, mediaID, "disk", mediaID)
	if err != nil || disk.MediaRole != "disk" {
		t.Fatalf("disk role refused: %+v %v", disk, err)
	}
	cassette, err := store.SelectCoreEntryMedia(ctx, gameID, packageID, mediaID, "cassette", mediaID)
	if err != nil || cassette.MediaRole != "cassette" {
		t.Fatalf("cassette role refused: %+v %v", cassette, err)
	}
	if _, err := store.SelectCoreEntryMedia(ctx, gameID, packageID, mediaID, "tape", mediaID); err == nil {
		t.Fatal("unknown media role accepted")
	}
	rows, err := store.db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration broke a foreign key")
	}
}
