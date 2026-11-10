package catalog

import (
	"context"
	"strings"
	"testing"
)

func TestKitHostEntryMigrationAndAtomicCreation(t *testing.T) {
	ctx := context.Background()
	store, path := mediaTestStore(t)
	pkg, identity := strings.Repeat("a", 64), strings.Repeat("b", 64)
	prior, err := store.CreateCoreEntry(ctx, "User machine", "fes.sms", pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DROP TRIGGER remote_core_entry_selected; DROP TABLE remote_core_entries; DROP TRIGGER kit_host_entry_edit; DROP TRIGGER kit_host_rom_insert; DROP TRIGGER kit_host_rom_update; DROP TRIGGER kit_host_rom_delete; DROP TRIGGER kit_host_expansion_insert; DROP TRIGGER kit_host_expansion_update; DROP TRIGGER kit_host_expansion_delete; DROP TRIGGER kit_host_slot_insert; DROP TRIGGER kit_host_slot_update; DROP TRIGGER kit_host_slot_delete; DROP TABLE kit_host_entries; PRAGMA user_version = 20`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got, err := store.CoreEntry(ctx, prior.GameID); err != nil || got != prior {
		t.Fatalf("prior=%+v err=%v", got, err)
	}
	bad := KitHostEntryIdentity{Identity: identity, ROMID: "cart", ROMMediaID: strings.Repeat("c", 64), ROMSourceSize: 1024}
	if _, err := store.CreateKitHostEntry(ctx, "Generated", "fes.sms", pkg, "", "", bad); err == nil {
		t.Fatal("accepted missing ROM")
	}
	if _, found, err := store.KitHostEntry(ctx, identity); err != nil || found {
		t.Fatalf("failed create mapped: found=%v err=%v", found, err)
	}
	entries, err := store.CoreEntries(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed create left entry: %+v %v", entries, err)
	}
	entry, err := store.CreateKitHostEntry(ctx, "Generated", "fes.sms", pkg, "", "", KitHostEntryIdentity{Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := store.KitHostEntry(ctx, identity); err != nil || !found || got != entry {
		t.Fatalf("mapping=%+v %v %v", got, found, err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM games WHERE game_id = ?`, entry.GameID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.KitHostEntry(ctx, identity); err != nil || found {
		t.Fatalf("deleted mapping found=%v err=%v", found, err)
	}
}

func TestKitHostExpansionEditsDetachMapping(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"core_entry_expansions", "core_entry_slot_expansions"} {
		t.Run(table, func(t *testing.T) {
			s, _ := mediaTestStore(t)
			pkg, id, expansion := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
			media, _, err := s.ImportCoreMedia(ctx, []byte("card"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO core_expansions(expansion_id,media_id,shell_package_id) VALUES(?,?,?)`, expansion, media.MediaID, pkg); err != nil {
				t.Fatal(err)
			}
			entry, err := s.CreateKitHostEntry(ctx, "Machine", "fes.sms", pkg, "", "", KitHostEntryIdentity{Identity: id})
			if err != nil {
				t.Fatal(err)
			}
			var insert, update, remove string
			if table == "core_entry_expansions" {
				insert = `INSERT INTO core_entry_expansions(game_id,expansion_id) VALUES (?,?)`
				update = `UPDATE core_entry_expansions SET expansion_id=expansion_id WHERE game_id=?`
				remove = `DELETE FROM core_entry_expansions WHERE game_id=?`
			} else {
				insert = `INSERT INTO core_entry_slot_expansions(game_id,slot,expansion_id) VALUES (?,1,?)`
				update = `UPDATE core_entry_slot_expansions SET expansion_id=expansion_id WHERE game_id=?`
				remove = `DELETE FROM core_entry_slot_expansions WHERE game_id=?`
			}
			for i, statement := range []string{insert, update, remove} {
				if i > 0 {
					if _, err := s.db.ExecContext(ctx, `INSERT INTO kit_host_entries(identity,game_id) VALUES (?,?)`, id, entry.GameID); err != nil {
						t.Fatal(err)
					}
				}
				args := []any{entry.GameID}
				if i == 0 {
					args = append(args, expansion)
				}
				if _, err := s.db.ExecContext(ctx, statement, args...); err != nil {
					t.Fatal(err)
				}
				if _, found, err := s.KitHostEntry(ctx, id); err != nil || found {
					t.Fatalf("edit %d: mapped=%v err=%v", i, found, err)
				}
				visible, err := s.CoreEntries(ctx)
				if err != nil || len(visible) != 1 || visible[0].GameID != entry.GameID {
					t.Fatalf("edit %d visible=%+v err=%v", i, visible, err)
				}
			}
		})
	}
}
