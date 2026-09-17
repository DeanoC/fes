package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

func mediaTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.sqlite3")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestCoreEntryDistinctTitlesWithSameSlug(t *testing.T) {
	ctx := context.Background()
	for _, titles := range [][]string{
		{"Tape!", "Tape?", "tape!"},
		{"日本語", "中文"},
		{strings.Repeat("Long", 40) + "A", strings.Repeat("Long", 40) + "B"},
	} {
		s, path := mediaTestStore(t)
		seen := make(map[string]bool)
		entries := make([]CoreEntry, 0, len(titles))
		for _, title := range titles {
			entry, err := s.CreateCoreEntry(ctx, title, "fes.test", strings.Repeat("a", 64))
			if err != nil || seen[entry.GameID] {
				t.Fatalf("distinct title %q: %+v, %v", title, entry, err)
			}
			seen[entry.GameID] = true
			entries = append(entries, entry)
			if _, err := s.CreateCoreEntry(ctx, " "+title+" ", "fes.test", strings.Repeat("b", 64)); !errors.Is(err, ErrCoreEntryConflict) {
				t.Fatalf("duplicate trimmed title %q: %v", title, err)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if got, err := reopened.CoreEntry(ctx, entry.GameID); err != nil || got != entry {
				t.Fatalf("reopened title: %+v, %v", got, err)
			}
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoreMediaBytesPersistenceAndIntegrity(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	data := []byte{0, 1, 2, 255}
	want := bytes.Clone(data)
	media, added, err := s.ImportCoreMedia(ctx, data)
	if err != nil || !added || media.Size != 4 || media.MediaID != fmt.Sprintf("%x", sha256.Sum256(want)) {
		t.Fatalf("import = %+v, %v, %v", media, added, err)
	}
	data[0] = 99
	if got, added, err := s.ImportCoreMedia(ctx, want); err != nil || added || got != media {
		t.Fatalf("deduplicate = %+v, %v, %v", got, added, err)
	}
	got, returned, err := s.CoreMedia(ctx, media.MediaID)
	if err != nil || got != media || !bytes.Equal(returned, want) {
		t.Fatalf("read = %+v, %x, %v", got, returned, err)
	}
	returned[0] = 77
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, returned, err = s.CoreMedia(ctx, media.MediaID)
	if err != nil || got != media || !bytes.Equal(returned, want) {
		t.Fatalf("reopen = %+v, %x, %v", got, returned, err)
	}
	raw, err := json.Marshal(media)
	if err != nil || string(raw) != fmt.Sprintf(`{"media_id":"%s","size":4}`, media.MediaID) {
		t.Fatalf("JSON = %s, %v", raw, err)
	}
	for _, data := range [][]byte{nil, {}} {
		if _, _, err := s.ImportCoreMedia(ctx, data); !errors.Is(err, ErrInvalidCoreMedia) {
			t.Fatalf("invalid size %d: %v", len(data), err)
		}
	}
	for _, size := range []int{1, int(protocol.MaxDevelopmentMediaBytes)} {
		if _, _, err := s.ImportCoreMedia(ctx, make([]byte, size)); err != nil {
			t.Fatalf("valid size %d: %v", size, err)
		}
	}
	for _, id := range []string{"", "../private", strings.Repeat("A", 64)} {
		if _, _, err := s.CoreMedia(ctx, id); !errors.Is(err, ErrInvalidCoreMedia) {
			t.Fatalf("invalid ID: %v", err)
		}
	}
	if _, _, err := s.CoreMedia(ctx, strings.Repeat("f", 64)); !errors.Is(err, ErrCoreMediaNotFound) {
		t.Fatalf("missing: %v", err)
	}
	for _, corrupt := range []struct {
		name string
		size int64
		data []byte
	}{
		{"size", 5, want}, {"hash", 4, []byte{0, 1, 2, 254}}, {"empty", 0, []byte{}}, {"oversize", protocol.MaxDevelopmentMediaBytes + 1, make([]byte, protocol.MaxDevelopmentMediaBytes+1)},
	} {
		t.Run(corrupt.name, func(t *testing.T) {
			if _, err := s.db.ExecContext(ctx, "UPDATE core_media SET size = ?, data = ? WHERE media_id = ?", corrupt.size, corrupt.data, media.MediaID); err != nil {
				t.Fatal(err)
			}
			if _, data, err := s.CoreMedia(ctx, media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) || data != nil {
				t.Fatalf("corrupt read = %x, %v", data, err)
			}
			if _, _, err := s.ImportCoreMedia(ctx, want); !errors.Is(err, ErrInvalidCoreMedia) {
				t.Fatalf("import silently repaired corruption: %v", err)
			}
			if _, err := s.CreateCoreMediaEntry(ctx, "corrupt", "fes.test", strings.Repeat("a", 64), "blob", media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) {
				t.Fatalf("bind corruption: %v", err)
			}
		})
	}
}

func TestCoreMediaRejectsOversizedStoredBlob(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	media, _, err := s.ImportCoreMedia(ctx, []byte("valid"))
	if err != nil {
		t.Fatal(err)
	}
	const oversized = 8 << 20
	// Build the corrupt payload inside SQLite, including a dishonest in-range
	// size. Admission must check the actual blob length, not just that field.
	for _, size := range []int64{media.Size, oversized} {
		if _, err := s.db.ExecContext(ctx, `UPDATE core_media SET size = ?, data = zeroblob(?) WHERE media_id = ?`, size, oversized, media.MediaID); err != nil {
			t.Fatal(err)
		}
		got, data, err := s.CoreMedia(ctx, media.MediaID)
		if !errors.Is(err, ErrInvalidCoreMedia) || got != (CoreMedia{}) || data != nil {
			t.Fatalf("oversized blob (size=%d): %+v, %d bytes, %v", size, got, len(data), err)
		}
	}
}

func TestCoreMediaEntrySelectionAndAtomicity(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	pkg, next := strings.Repeat("a", 64), strings.Repeat("b", 64)
	media, _, err := s.ImportCoreMedia(ctx, []byte("tape"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateCoreMediaEntry(ctx, "Tape", "fes.zx81", pkg, "blob", media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"blob", ""}, {"", media.MediaID}, {"rom", media.MediaID}, {"blob", "bad"}} {
		if _, err := s.CreateCoreMediaEntry(ctx, "invalid", "fes.zx81", pkg, pair[0], pair[1]); !errors.Is(err, ErrInvalidCoreMedia) {
			t.Fatalf("invalid creation %v: %v", pair, err)
		}
		if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, pkg, media.MediaID, pair[0], pair[1]); !errors.Is(err, ErrInvalidCoreMedia) {
			t.Fatalf("invalid selection %v: %v", pair, err)
		}
	}
	missing := strings.Repeat("c", 64)
	if _, err := s.CreateCoreMediaEntry(ctx, "missing", "fes.zx81", pkg, "blob", missing); !errors.Is(err, ErrCoreMediaNotFound) {
		t.Fatalf("missing creation: %v", err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, pkg, media.MediaID, "blob", missing); !errors.Is(err, ErrCoreMediaNotFound) {
		t.Fatalf("missing selection: %v", err)
	}
	if entries, err := s.CoreEntries(ctx); err != nil || !reflect.DeepEqual(entries, []CoreEntry{entry}) {
		t.Fatalf("failed creation leaked rows: %+v, %v", entries, err)
	}
	var games int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM games").Scan(&games); err != nil || games != 1 {
		t.Fatalf("games = %d, %v", games, err)
	}
	selected, err := s.SelectCoreEntry(ctx, entry.GameID, entry.CoreID, pkg, next)
	expected := entry
	expected.PackageID = next
	if err != nil || selected != expected {
		t.Fatalf("package selection lost media: %+v, %v", selected, err)
	}
	for _, stale := range [][2]string{{pkg, media.MediaID}, {next, ""}} {
		if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, stale[0], stale[1], "", ""); !errors.Is(err, ErrCoreEntryConflict) {
			t.Fatalf("stale selection: %v", err)
		}
	}
	if _, err := s.SelectCoreEntryMedia(ctx, "missing", next, "", "", ""); !errors.Is(err, ErrCoreEntryNotFound) {
		t.Fatalf("missing entry: %v", err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, next, "bad", "", ""); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("invalid expectation: %v", err)
	}
	cleared, err := s.SelectCoreEntryMedia(ctx, entry.GameID, next, media.MediaID, "", "")
	expected.MediaID, expected.MediaRole = "", ""
	if err != nil || cleared != expected {
		t.Fatalf("clear = %+v, %v", cleared, err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, next, media.MediaID, "", ""); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("replay: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.CoreEntry(ctx, entry.GameID); err != nil || got != expected {
		t.Fatalf("reopen clear = %+v, %v", got, err)
	}
	attached, err := s.SelectCoreEntryMedia(ctx, entry.GameID, next, "", "blob", media.MediaID)
	expected.MediaID, expected.MediaRole = media.MediaID, "blob"
	if err != nil || attached != expected {
		t.Fatalf("attach = %+v, %v", attached, err)
	}
}

func TestSchemaSevenMigratesLegacyEntriesOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{schemaV1, schemaV2, schemaV3, schemaV4, schemaV5, schemaV6} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	pkg := strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO libraries(id,system,root,online) VALUES (?,?,?,1)`, corePackageLibraryID, CorePlatform, corePackageRoot); err != nil {
		t.Fatal(err)
	}
	entries := []CoreEntry{
		{GameID: GameID(CorePlatform, corePackageLibraryID, "fes.coleco", "Legacy Coleco"), Title: "Legacy Coleco", CoreID: "fes.coleco", PackageID: pkg},
		{GameID: GameID(CorePlatform, corePackageLibraryID, "fes.pong", "Legacy Pong"), Title: "Legacy Pong", CoreID: "fes.pong", PackageID: pkg},
	}
	for _, e := range entries {
		if _, err := db.ExecContext(ctx, `INSERT INTO games(game_id,library_id,system,relative_path,title,source_kind,source_state,source_size,modified_ns,seen_generation,search_text,group_key,first_seen_ns) VALUES (?,?,?,?,?,?,?,0,0,0,?,?,123)`,
			e.GameID, corePackageLibraryID, CorePlatform, e.CoreID, e.Title, SourceKindCorePackage, SourceStateAvailable, searchDocument(e.GameID, e.Title, CorePlatform), e.GameID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO core_entries(game_id,core_id,package_id) VALUES (?,?,?)", e.GameID, e.CoreID, e.PackageID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	users, err := libraryuser.Open(filepath.Join(dir, "users.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer users.Close()
	original := make(map[string]libraryuser.State)
	for _, e := range entries {
		if err := users.SetFavorite(ctx, e.GameID, true); err != nil {
			t.Fatal(err)
		}
		if err := users.RecordPlay(ctx, e.GameID); err != nil {
			t.Fatal(err)
		}
		state, err := users.State(ctx, e.GameID)
		if err != nil {
			t.Fatal(err)
		}
		original[e.GameID] = state
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seed := "ef9443c2787cd02b6d78d233d015b0bbf3fb21d53d1a5890497cdbb7897f053c"
	entries[0].MediaID, entries[0].MediaRole = seed, "blob"
	for _, e := range entries {
		if got, err := s.CoreEntry(ctx, e.GameID); err != nil || got != e {
			t.Fatalf("migrated = %+v, %v", got, err)
		}
		game, err := s.Game(ctx, e.GameID)
		if err != nil || game.RelativePath != e.CoreID || game.FirstSeenNS != 123 || game.GroupKey != e.GameID {
			t.Fatalf("rewrote legacy game: %+v, %v", game, err)
		}
		if _, err := s.CreateCoreEntry(ctx, e.Title, e.CoreID, pkg); !errors.Is(err, ErrCoreEntryConflict) {
			t.Fatalf("legacy duplicate: %v", err)
		}
		fresh, err := s.CreateCoreEntry(ctx, "Another "+e.Title, e.CoreID, pkg)
		if err != nil || fresh.GameID == e.GameID || fresh.MediaID != "" || fresh.MediaRole != "" {
			t.Fatalf("new title = %+v, %v", fresh, err)
		}
		if game, err := s.Game(ctx, fresh.GameID); err != nil || game.RelativePath != fresh.GameID {
			t.Fatalf("new path = %+v, %v", game, err)
		}
	}
	media, data, err := s.CoreMedia(ctx, seed)
	decoded, decodeErr := hex.DecodeString(strings.Join(strings.Fields(legacyColecoMediaHex), ""))
	if err != nil || decodeErr != nil || media.Size != 2299 || !bytes.Equal(data, decoded) {
		t.Fatalf("legacy seed = %+v, %v, %v", media, err, decodeErr)
	}
	if _, added, err := s.ImportCoreMedia(ctx, decoded); err != nil || added {
		t.Fatalf("seed is not ordinary media: %v, %v", added, err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entries[0].GameID, pkg, seed, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntry(ctx, entries[0].GameID, "fes.coleco", pkg, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.CoreEntry(ctx, entries[0].GameID); err != nil || got.MediaID != "" || got.MediaRole != "" {
		t.Fatalf("seed rebound on reopen: %+v, %v", got, err)
	}
	for _, e := range entries {
		state, err := users.State(ctx, e.GameID)
		if err != nil || state != original[e.GameID] {
			t.Fatalf("user state changed: %+v, %v", state, err)
		}
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 8 {
		t.Fatalf("version = %d, %v", version, err)
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration broke foreign keys")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestCoreMediaCASAcrossStores(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	pkg := strings.Repeat("a", 64)
	e, err := s.CreateCoreEntry(ctx, "Race", "fes.test", pkg)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := s.ImportCoreMedia(ctx, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.ImportCoreMedia(ctx, []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, store := range []*Store{s, other} {
		wg.Add(1)
		go func(i int, store *Store) {
			defer wg.Done()
			<-start
			_, err := store.SelectCoreEntryMedia(ctx, e.GameID, pkg, "", "blob", []string{a.MediaID, b.MediaID}[i])
			results <- err
		}(i, store)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrCoreEntryConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}

func TestCoreMediaEntryPersistenceAndRollback(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	media, _, err := s.ImportCoreMedia(ctx, []byte("persist this tape"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateCoreMediaEntry(ctx, "Persistent", "fes.zx81", pkg, "blob", media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.CreateCoreEntry(ctx, "Fresh Coleco", "fes.coleco", pkg)
	if err != nil || empty.MediaID != "" || empty.MediaRole != "" {
		t.Fatalf("implicit media: %+v, %v", empty, err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_entry BEFORE INSERT ON core_entries BEGIN SELECT RAISE(ABORT, 'injected failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCoreMediaEntry(ctx, "Rollback", "fes.zx81", pkg, "blob", media.MediaID); err == nil {
		t.Fatal("expected injected failure")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM games WHERE title = 'Rollback'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial creation: %d, %v", count, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.CoreEntry(ctx, entry.GameID); err != nil || got != entry {
		t.Fatalf("reopened media entry: %+v, %v", got, err)
	}
	got, data, err := s.CoreMedia(ctx, entry.MediaID)
	if err != nil || got != media || string(data) != "persist this tape" {
		t.Fatalf("reopened media: %+v, %q, %v", got, data, err)
	}
	if got, err := s.CoreEntry(ctx, empty.GameID); err != nil || got != empty {
		t.Fatalf("reopened empty entry: %+v, %v", got, err)
	}
}
