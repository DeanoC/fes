package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func remoteCacheImport(t *testing.T, s *Store, value string) CoreMedia {
	t.Helper()
	media, created, err := s.ImportRemoteCassetteStream(context.Background(), int64(len(value)), strings.NewReader(value))
	if err != nil || !created {
		t.Fatalf("remote import: %+v created=%v error=%v", media, created, err)
	}
	return media
}

func TestRemoteCacheOnlyCollectsProvenRemoteUnreferencedContent(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	removed := remoteCacheImport(t, s, "removed remote cassette")
	selected := remoteCacheImport(t, s, "selected remote cassette")
	saved := remoteCacheImport(t, s, "saved cassette")
	firmware := remoteCacheImport(t, s, "firmware bytes")
	rom := remoteCacheImport(t, s, "ROM bytes")
	expansion := remoteCacheImport(t, s, "expansion bytes")
	video := remoteCacheImport(t, s, "video part bytes")
	pending := remoteCacheImport(t, s, "pending cassette")
	local := remoteCacheImport(t, s, "then imported by a local user")
	if _, created, err := s.ImportCoreMedia(ctx, []byte("then imported by a local user")); err != nil || created {
		t.Fatalf("local reimport: %v %v", created, err)
	}
	unknown, _, err := s.ImportCoreMedia(ctx, []byte("unknown prior object"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateCoreMediaEntry(ctx, "Selected", "fes.zx81", pkg, "cassette", selected.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := s.CreateCoreEntry(ctx, "Retained SAVE", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO zx81_saves(game_id,media_id,name,created_at) VALUES (?,?,?,1)`, retained.GameID, saved.MediaID, "S"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, retained.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO core_firmware(slot,media_id) VALUES ('firmware',?)`, firmware.MediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO core_entry_roms(game_id,package_id,rom_id,media_id,source_size) VALUES (?,?,?,?,?)`, entry.GameID, pkg, "rom", rom.MediaID, rom.Size); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO core_expansions(expansion_id,media_id,shell_package_id) VALUES (?,?,?)`, strings.Repeat("b", 64), expansion.MediaID, pkg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO core_video_parts(part_id,media_id,shell_package_id,profile) VALUES (?,?,?,'direct')`, strings.Repeat("c", 64), video.MediaID, pkg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRemoteCoreEntry(ctx, "Pending", "fes.zx81", pkg, RemoteCoreEntryOrigin{SourceID: "home", SourceGameID: "pending", MediaID: pending.MediaID}); err != nil {
		t.Fatal(err)
	}
	usage, err := s.RemoteCassetteCache(ctx, "", 100)
	if err != nil || usage.ReclaimableObjects != 1 || len(usage.Candidates) != 1 || usage.Candidates[0].MediaID != removed.MediaID {
		t.Fatalf("preview: %+v %v", usage, err)
	}
	ids := []string{removed.MediaID, selected.MediaID, saved.MediaID, firmware.MediaID, rom.MediaID, expansion.MediaID, video.MediaID, pending.MediaID, local.MediaID, unknown.MediaID}
	result, err := s.RemoveRemoteCassetteCache(ctx, ids)
	if err != nil || len(result.Deleted) != 1 || result.Deleted[0].MediaID != removed.MediaID || result.LogicalBytes != removed.Size || len(result.Skipped) != len(ids)-1 {
		t.Fatalf("cleanup: %+v %v", result, err)
	}
	if _, err := s.CoreMediaInfo(ctx, removed.MediaID); !errors.Is(err, ErrCoreMediaNotFound) {
		t.Fatalf("removed content: %v", err)
	}
	for _, id := range ids[1:] {
		if _, err := s.CoreMediaInfo(ctx, id); err != nil {
			t.Fatalf("protected %s: %v", id, err)
		}
	}
	if _, err := s.RemoveRemoteCassetteCache(ctx, []string{removed.MediaID}); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
}

func TestRemoteCachePreviewPaginationAndValidation(t *testing.T) {
	s, _ := mediaTestStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		remoteCacheImport(t, s, fmt.Sprintf("remote %d", i))
	}
	var ids []string
	after := ""
	for {
		page, err := s.RemoteCassetteCache(ctx, after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if page.TrackedObjects != 3 || page.ReclaimableObjects != 3 || len(page.Candidates) != 1 {
			t.Fatalf("page: %+v", page)
		}
		ids = append(ids, page.Candidates[0].MediaID)
		if page.NextAfter == "" {
			break
		}
		after = page.NextAfter
	}
	if len(ids) != 3 || !(ids[0] < ids[1] && ids[1] < ids[2]) {
		t.Fatalf("pagination: %v", ids)
	}
	for _, bad := range [][]string{nil, {"bad"}, {ids[0], ids[0]}} {
		if _, err := s.RemoveRemoteCassetteCache(ctx, bad); !errors.Is(err, ErrInvalidCoreMedia) {
			t.Fatalf("bad ids %v: %v", bad, err)
		}
	}
	if _, err := s.RemoteCassetteCache(ctx, "bad", 1); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("bad cursor: %v", err)
	}
}

func TestRemoteCacheDoesNotClaimPreviouslyImportedLocalContent(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	data := []byte("local before remote")
	local, _, err := s.ImportCoreMedia(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	_, created, err := s.ImportRemoteCassetteStream(ctx, int64(len(data)), bytes.NewReader(data))
	if err != nil || created {
		t.Fatalf("dedup: %v %v", created, err)
	}
	result, err := s.RemoveRemoteCassetteCache(ctx, []string{local.MediaID})
	if err != nil || len(result.Deleted) != 0 {
		t.Fatalf("protected local: %+v %v", result, err)
	}
}

func TestRemoteCacheOldStyleDuplicateImportWinsBeforeCleanup(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	remote := remoteCacheImport(t, s, "old client content")
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	// A client from before schema 23 uses this exact INSERT ... DO NOTHING.
	// Hold its transaction while cleanup starts on a separate Store.
	tx, err := other.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO core_media(media_id,size,data) VALUES (?,?,X'') ON CONFLICT(media_id) DO NOTHING`, remote.MediaID, remote.Size); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		result, e := s.RemoveRemoteCassetteCache(ctx, []string{remote.MediaID})
		if e == nil && len(result.Deleted) != 0 {
			e = errors.New("removed imported object")
		}
		done <- e
	}()
	select {
	case err := <-done:
		t.Fatalf("cleanup passed writer before import committed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup blocked after commit")
	}
	if _, err := s.CoreMediaInfo(ctx, remote.MediaID); err != nil {
		t.Fatalf("local import lost: %v", err)
	}
}

func TestRemoteCacheSecondProcessCannotCollectBetweenImportAndBind(t *testing.T) {
	ctx := context.Background()
	writer, path := mediaTestStore(t)
	cleaner, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cleaner.Close()
	payload := []byte("download between import and bind")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	pkg := strings.Repeat("a", 64)
	entry, err := writer.CreateRemoteCoreEntry(ctx, "Pending", "fes.zx81", pkg, RemoteCoreEntryOrigin{SourceID: "remote", SourceGameID: "pending", MediaID: digest})
	if err != nil {
		t.Fatal(err)
	}
	origin, found, err := writer.RemoteCoreEntry(ctx, entry.GameID)
	if err != nil || !found || !origin.Pending {
		t.Fatalf("origin: %+v %v %v", origin, found, err)
	}
	media, created, err := writer.ImportRemoteCassetteStream(ctx, int64(len(payload)), bytes.NewReader(payload))
	if err != nil || !created {
		t.Fatalf("import: %v %v", created, err)
	}
	result, err := cleaner.RemoveRemoteCassetteCache(ctx, []string{media.MediaID})
	if err != nil || len(result.Deleted) != 0 {
		t.Fatalf("pending import was collected: %+v %v", result, err)
	}
	bound, err := writer.BindRemoteCoreEntryMedia(ctx, entry.GameID, origin)
	if err != nil || bound.MediaID != digest {
		t.Fatalf("bind after cleanup: %+v %v", bound, err)
	}
	if _, err := writer.CoreMediaInfo(ctx, digest); err != nil {
		t.Fatal(err)
	}
	if err := writer.RemoveCoreEntry(ctx, entry.GameID); err != nil {
		t.Fatal(err)
	}
	result, err = cleaner.RemoveRemoteCassetteCache(ctx, []string{digest})
	if err != nil || len(result.Deleted) != 1 {
		t.Fatalf("after explicit removal: %+v %v", result, err)
	}
	if _, err := writer.BindRemoteCoreEntryMedia(ctx, entry.GameID, origin); err == nil {
		t.Fatal("removed origin unexpectedly bound")
	}
}

func TestRemoteCacheMigrationLeavesSchema22ContentUnclassified(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	old := remoteCacheImport(t, s, "pre-migration unknown")
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER core_media_import_protect; DROP TABLE remote_cached_media; PRAGMA user_version=22`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	usage, err := upgraded.RemoteCassetteCache(ctx, "", 100)
	if err != nil || usage.TrackedObjects != 0 || usage.ReclaimableObjects != 0 {
		t.Fatalf("migration inferred unknown provenance: %+v %v", usage, err)
	}
	result, err := upgraded.RemoveRemoteCassetteCache(ctx, []string{old.MediaID})
	if err != nil || len(result.Deleted) != 0 {
		t.Fatalf("migration removed unknown: %+v %v", result, err)
	}
	if _, err := upgraded.CoreMediaInfo(ctx, old.MediaID); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCachePreviewDoesNotReserveWriter(t *testing.T) {
	s, path := mediaTestStore(t)
	remoteCacheImport(t, s, "preview while another writer holds reservation")
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := other.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	usage, err := s.RemoteCassetteCache(ctx, "", 100)
	if err != nil || usage.ReclaimableObjects != 1 {
		t.Fatalf("read-only preview blocked by writer: %+v %v", usage, err)
	}
}

func TestRemoteCacheRecognizesEverySchemaForeignKeyToMedia(t *testing.T) {
	s, _ := mediaTestStore(t)
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	// core_media_chunks and the provenance marker are storage owned by the
	// candidate itself. Every other FK requires an explicit protection rule.
	want := map[string]bool{"core_media_chunks": true, "remote_cached_media": true, "core_firmware": true, "core_entry_roms": true, "core_expansions": true, "core_video_parts": true, "zx81_saves": true}
	for _, table := range tables {
		quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		fk, err := s.db.QueryContext(ctx, `PRAGMA foreign_key_list(`+quoted+`)`)
		if err != nil {
			t.Fatal(err)
		}
		for fk.Next() {
			var id, seq int
			var ref, from, to, update, del, match string
			if err := fk.Scan(&id, &seq, &ref, &from, &to, &update, &del, &match); err != nil {
				t.Fatal(err)
			}
			if ref == "core_media" && !want[table] {
				t.Errorf("new %s.%s reference to core_media needs cleanup protection", table, from)
			}
		}
		if err := errors.Join(fk.Err(), fk.Close()); err != nil {
			t.Fatal(err)
		}
	}
}
