package catalog

import (
	"context"
	"strings"
	"testing"
)

func TestNamedROMMigrationPreservesSelectionsAndKitHostTriggers(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	pkg, identity := strings.Repeat("a", 64), strings.Repeat("b", 64)
	media, _, err := s.ImportCoreMedia(ctx, []byte("cart bytes"))
	if err != nil {
		t.Fatal(err)
	}
	bios, _, err := s.ImportCoreMedia(ctx, []byte("bios"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateKitHostEntry(ctx, "Test", "fes.coleco", pkg, "", "", KitHostEntryIdentity{
		Identity: identity, ROMID: "named-cart", ROMMediaID: media.MediaID, ROMSourceSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct schema 22's single-ROM table without firing the edit triggers.
	_, err = s.db.ExecContext(ctx, `
DROP TRIGGER kit_host_rom_insert;
DROP TRIGGER kit_host_rom_update;
DROP TRIGGER kit_host_rom_delete;
CREATE TABLE old_roms (
  game_id TEXT PRIMARY KEY REFERENCES core_entries(game_id) ON DELETE CASCADE,
  package_id TEXT NOT NULL,
  rom_id TEXT NOT NULL,
  media_id TEXT NOT NULL REFERENCES core_media(media_id),
  source_size INTEGER NOT NULL CHECK(source_size > 0)
);
CREATE TABLE core_firmware (
  slot TEXT PRIMARY KEY,
  media_id TEXT NOT NULL REFERENCES core_media(media_id),
  CHECK (slot = 'firmware')
);
INSERT INTO old_roms SELECT game_id,package_id,rom_id,media_id,source_size FROM core_entry_roms;
DROP TABLE core_entry_roms;
ALTER TABLE old_roms RENAME TO core_entry_roms;
CREATE TRIGGER kit_host_rom_insert AFTER INSERT ON core_entry_roms BEGIN
  DELETE FROM kit_host_entries WHERE game_id = NEW.game_id;
END;
CREATE TRIGGER kit_host_rom_update AFTER UPDATE ON core_entry_roms BEGIN
  DELETE FROM kit_host_entries WHERE game_id = NEW.game_id;
END;
CREATE TRIGGER kit_host_rom_delete AFTER DELETE ON core_entry_roms BEGIN
  DELETE FROM kit_host_entries WHERE game_id = OLD.game_id;
END;
PRAGMA user_version = 22;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO core_firmware(slot, media_id) VALUES ('firmware', ?)`, bios.MediaID); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.CoreEntryROM(ctx, entry.GameID, "named-cart")
	if err != nil || got.PackageID != pkg || got.MediaID != media.MediaID || got.SourceSize != 10 {
		t.Fatalf("selection changed during migration: %+v %v", got, err)
	}
	if _, found, err := s.KitHostEntry(ctx, identity); err != nil || !found {
		t.Fatalf("migration detached kit-host mapping: found=%v err=%v", found, err)
	}
	if got, err := s.CoreMediaInfo(ctx, bios.MediaID); err != nil || got.MediaID != bios.MediaID {
		t.Fatalf("household BIOS bytes lost during migration: %+v %v", got, err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='core_firmware'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("legacy firmware table remains: count=%d err=%v", count, err)
	}
	if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, pkg, "named-bios", 4, "", bios.MediaID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.KitHostEntry(ctx, identity); err != nil || found {
		t.Fatalf("ROM edit did not detach kit-host mapping: found=%v err=%v", found, err)
	}
}

func TestFirmwareRequirementPersistsOnEntry(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	entry, err := s.CreateFirmwareRequiredEntry(ctx, "Frogger", "fes.coleco", strings.Repeat("a", 64), "", "")
	if err != nil || !entry.FirmwareRequired {
		t.Fatalf("create firmware-required entry: %+v %v", entry, err)
	}
	got, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || got != entry {
		t.Fatalf("reloaded firmware requirement: %+v %v", got, err)
	}
}
