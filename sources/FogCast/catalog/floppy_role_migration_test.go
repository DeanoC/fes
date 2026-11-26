package catalog

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

// Schema 27 rebuilds core_entries for the floppy role. A version-26 catalog
// keeps its entries, names, geometry and disk sets, and every trigger on
// core_entries that an earlier version created exists again.
func TestFloppyRoleMigrationKeepsEntriesSetsAndTriggers(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	disk1, _, err := s.ImportCoreMedia(ctx, []byte("disk one"))
	if err != nil {
		t.Fatal(err)
	}
	disk2, _, err := s.ImportCoreMedia(ctx, []byte("disk two"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateCoreMediaEntry(ctx, "Two Sides", "fes.atari-st", pkg, protocol.DiskRole, disk1.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disk1.MediaID, nil, []SetMedia{{MediaID: disk2.MediaID, MediaName: "SIDE2.ST"}}); err != nil {
		t.Fatal(err)
	}
	// Back to version 26: its table admits no floppy role.
	if _, err := s.db.ExecContext(ctx, `PRAGMA user_version = 26`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	migrated, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || migrated.MediaID != disk1.MediaID || migrated.MediaRole != protocol.DiskRole ||
		len(migrated.MoreMedia) != 1 || migrated.MoreMedia[0].MediaName != "SIDE2.ST" {
		t.Fatalf("migrated %+v %v", migrated, err)
	}
	// Every trigger an earlier version put on core_entries is back.
	created := regexp.MustCompile(`CREATE TRIGGER (\w+) [^;]*?ON core_entries\b`)
	want := map[string]bool{}
	for _, schema := range []string{schemaV21, schemaV22, schemaV24, schemaV25, schemaV26} {
		for _, m := range created.FindAllStringSubmatch(schema, -1) {
			want[m[1]] = true
		}
	}
	if len(want) != 6 {
		t.Fatalf("earlier versions created %d core_entries triggers: %v", len(want), want)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'trigger' AND tbl_name = 'core_entries'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("missing after migration: %v", want)
	}
	// The set still ends when disk 1 changes, and the floppy role is admitted.
	floppy, _, err := s.ImportCoreMedia(ctx, []byte("floppy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, pkg, disk1.MediaID, protocol.FloppyRole, floppy.MediaID); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CoreEntry(ctx, entry.GameID); err != nil || changed.MediaRole != protocol.FloppyRole || len(changed.MoreMedia) != 0 {
		t.Fatalf("after disk 1 changed %+v %v", changed, err)
	}
}
