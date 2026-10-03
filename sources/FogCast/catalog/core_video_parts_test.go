package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

// Catalog fixtures exercise bounded archive storage, not configuration-frame
// decoding or FPGA resource admission; the service owns those checks.
func catalogVideoAsset(t *testing.T, packageID string, payload byte) expansion.Asset {
	t.Helper()
	cart := bytes.Repeat([]byte{payload}, CoreMediaChunkBytes+5000)
	asset, err := expansion.NewAsset(expansion.Manifest{
		CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)),
		Device: expansion.Device, Format: 1, Map: expansion.ColecoVideoMap,
		RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40),
		ShellBuildID: strings.Repeat("d", 32), ShellPackageID: packageID,
		ShellSHA256: strings.Repeat("e", 64), Slot: expansion.VideoSlot, SlotMajor: 1,
	}, cart)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func catalogCPUAsset(t *testing.T, packageID string) expansion.Asset {
	t.Helper()
	video := catalogVideoAsset(t, packageID, 0x33)
	manifest := video.Manifest
	manifest.Slot, manifest.Map = expansion.Slot, expansion.Map
	asset, err := expansion.NewAsset(manifest, video.Cart)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func catalogVideoArchive(t *testing.T, asset expansion.Asset) []byte {
	t.Helper()
	var archive bytes.Buffer
	if err := asset.Write(&archive); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestCoreVideoProfilesAreClosed(t *testing.T) {
	for _, profile := range []string{"direct", "scanlines"} {
		if !ValidVideoProfile(profile) {
			t.Fatalf("valid profile %q rejected", profile)
		}
	}
	for _, profile := range []string{"", "Direct", "SCANLINES", " direct", "scanlines ", "crt", "../direct", "direct\x00"} {
		if ValidVideoProfile(profile) {
			t.Fatalf("invalid profile %q accepted", profile)
		}
	}
}

func TestCoreVideoPartStoragePersistsWithoutCPUInventory(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	if rows, err := s.CoreVideoParts(ctx); err != nil || len(rows) != 0 || rows == nil {
		t.Fatalf("empty inventory = %+v, %v", rows, err)
	}
	pkgA, pkgB := strings.Repeat("a", 64), strings.Repeat("f", 64)
	directA := catalogVideoAsset(t, pkgA, 0x11)
	scanlinesA := catalogVideoAsset(t, pkgA, 0x22)
	directB := catalogVideoAsset(t, pkgB, 0x11)
	manifest := directB.Manifest
	manifest.Slot, manifest.Map = expansion.NativeVideoSlot, expansion.ColecoNativeVideoMap
	var err error
	directB, err = expansion.NewAsset(manifest, directB.Cart)
	if err != nil {
		t.Fatal(err)
	}
	want := []CoreVideoPart{
		{PartID: directA.ID, PackageID: pkgA, Profile: "direct"},
		{PartID: scanlinesA.ID, PackageID: pkgA, Profile: "scanlines"},
		{PartID: directB.ID, PackageID: pkgB, Profile: "direct"},
	}
	for _, input := range []struct {
		asset   expansion.Asset
		profile string
		row     CoreVideoPart
	}{{directB, "direct", want[2]}, {scanlinesA, "scanlines", want[1]}, {directA, "direct", want[0]}, {directA, "direct", want[0]}} {
		row, err := s.ImportCoreVideoPart(ctx, input.asset, input.profile)
		if err != nil || row != input.row {
			t.Fatalf("import = %+v, %v; want %+v", row, err, input.row)
		}
	}
	rows, err := s.CoreVideoParts(ctx)
	if err != nil || !reflect.DeepEqual(rows, want) {
		t.Fatalf("inventory = %+v, %v; want %+v", rows, err, want)
	}
	encoded, err := json.Marshal(rows[0])
	if err != nil || string(encoded) != fmt.Sprintf(`{"part_id":"%s","package_id":"%s","profile":"direct"}`, directA.ID, pkgA) {
		t.Fatalf("JSON = %s, %v", encoded, err)
	}
	if cpu, err := s.CoreExpansions(ctx); err != nil || len(cpu) != 0 {
		t.Fatalf("video contaminated CPU inventory = %+v, %v", cpu, err)
	}
	var chunks int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM core_media_chunks WHERE media_id=(SELECT media_id FROM core_video_parts WHERE part_id=?)`, directA.ID).Scan(&chunks); err != nil || chunks < 2 {
		t.Fatalf("archive did not use chunks: %d, %v", chunks, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err = reopened.CoreVideoParts(ctx)
	if err != nil || !reflect.DeepEqual(rows, want) {
		t.Fatalf("reopened inventory = %+v, %v", rows, err)
	}
	for _, asset := range []expansion.Asset{directA, scanlinesA, directB} {
		got, err := reopened.ReadCoreVideoPart(ctx, asset.ID)
		if err != nil || !reflect.DeepEqual(got, asset) {
			t.Fatalf("reopened part %s changed: %v", asset.ID, err)
		}
	}
}

func TestCoreVideoPartConflictsPreserveMapping(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	original := catalogVideoAsset(t, pkg, 0x11)
	replacement := catalogVideoAsset(t, pkg, 0x22)
	first, err := s.ImportCoreVideoPart(ctx, original, "direct")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		asset   expansion.Asset
		profile string
	}{{replacement, "direct"}, {original, "scanlines"}} {
		if _, err := s.ImportCoreVideoPart(ctx, input.asset, input.profile); !errors.Is(err, ErrCoreVideoProfileConflict) {
			t.Fatalf("conflicting mapping accepted: %v", err)
		}
	}
	rows, err := s.CoreVideoParts(ctx)
	if err != nil || !reflect.DeepEqual(rows, []CoreVideoPart{first}) {
		t.Fatalf("conflict changed inventory: %+v, %v", rows, err)
	}
	got, err := s.ReadCoreVideoPart(ctx, original.ID)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("conflict changed original: %v", err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM core_media`).Scan(&count); err != nil || count != 2 { // Historical seed plus one archive.
		t.Fatalf("conflict imported another archive: %d, %v", count, err)
	}
}

func TestCoreVideoPartConcurrentConflict(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	assets := []expansion.Asset{catalogVideoAsset(t, pkg, 0x11), catalogVideoAsset(t, pkg, 0x22)}
	errorsOut := make(chan error, len(assets))
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, asset := range assets {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := s.ImportCoreVideoPart(ctx, asset, "direct")
			errorsOut <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errorsOut)
	success, conflicts := 0, 0
	for err := range errorsOut {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrCoreVideoProfileConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent import error: %v", err)
		}
	}
	rows, err := s.CoreVideoParts(ctx)
	if success != 1 || conflicts != 1 || err != nil || len(rows) != 1 {
		t.Fatalf("concurrent imports: successes=%d conflicts=%d rows=%+v err=%v", success, conflicts, rows, err)
	}
	if _, err := s.ReadCoreVideoPart(ctx, rows[0].PartID); err != nil {
		t.Fatalf("winning archive invalid: %v", err)
	}
}

func TestCoreVideoPartRejectsInvalidImports(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	for name, mutate := range map[string]func(*expansion.Asset, *string){
		"CPU slot": func(asset *expansion.Asset, _ *string) { *asset = catalogCPUAsset(t, pkg) },
		"profile":  func(_ *expansion.Asset, profile *string) { *profile = "crt" },
		"ID":       func(asset *expansion.Asset, _ *string) { asset.ID = strings.Repeat("f", 64) },
		"payload":  func(asset *expansion.Asset, _ *string) { asset.Cart[0] ^= 1 },
		"manifest": func(asset *expansion.Asset, _ *string) { asset.ManifestBytes = append(asset.ManifestBytes, '\n') },
		"socket":   func(asset *expansion.Asset, _ *string) { asset.Manifest.Map = expansion.ColecoMapV2 },
		"slot index": func(asset *expansion.Asset, _ *string) {
			asset.Manifest.SlotIndex = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := mediaTestStore(t)
			asset, profile := catalogVideoAsset(t, pkg, 0x11), "direct"
			mutate(&asset, &profile)
			if _, err := s.ImportCoreVideoPart(context.Background(), asset, profile); !errors.Is(err, ErrInvalidCoreVideoPart) {
				t.Fatalf("invalid import accepted: %v", err)
			}
			rows, err := s.CoreVideoParts(context.Background())
			if err != nil || len(rows) != 0 {
				t.Fatalf("invalid import added row: %+v, %v", rows, err)
			}
			var count int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM core_media`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("invalid import stored bytes: %d, %v", count, err)
			}
		})
	}
}

func TestCoreVideoPartReadRechecksArchiveAndRowBinding(t *testing.T) {
	ctx := context.Background()
	pkg := strings.Repeat("a", 64)
	for _, name := range []string{"shell", "archive identity", "CPU archive", "malformed archive", "chunk hash", "profile"} {
		t.Run(name, func(t *testing.T) {
			s, _ := mediaTestStore(t)
			asset := catalogVideoAsset(t, pkg, 0x11)
			if _, err := s.ImportCoreVideoPart(ctx, asset, "direct"); err != nil {
				t.Fatal(err)
			}
			readID := asset.ID
			var err error
			switch name {
			case "shell":
				_, err = s.db.ExecContext(ctx, `UPDATE core_video_parts SET shell_package_id=? WHERE part_id=?`, strings.Repeat("f", 64), asset.ID)
			case "archive identity", "CPU archive", "malformed archive":
				data := []byte("invalid archive")
				if name == "archive identity" {
					data = catalogVideoArchive(t, catalogVideoAsset(t, pkg, 0x22))
				} else if name == "CPU archive" {
					cpu := catalogCPUAsset(t, pkg)
					data, readID = catalogVideoArchive(t, cpu), cpu.ID
				}
				media, _, importErr := s.ImportCoreMedia(ctx, data)
				if importErr != nil {
					t.Fatal(importErr)
				}
				_, err = s.db.ExecContext(ctx, `UPDATE core_video_parts SET media_id=?,part_id=? WHERE part_id=?`, media.MediaID, readID, asset.ID)
			case "chunk hash":
				_, err = s.db.ExecContext(ctx, `UPDATE core_media_chunks SET data=X'00' WHERE chunk_index=0 AND media_id=(SELECT media_id FROM core_video_parts WHERE part_id=?)`, asset.ID)
			case "profile":
				connection, connErr := s.db.Conn(ctx)
				if connErr != nil {
					t.Fatal(connErr)
				}
				_, err = connection.ExecContext(ctx, `PRAGMA ignore_check_constraints=ON; UPDATE core_video_parts SET profile='crt'; PRAGMA ignore_check_constraints=OFF;`)
				_ = connection.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReadCoreVideoPart(ctx, readID); !errors.Is(err, ErrInvalidCoreVideoPart) {
				t.Fatalf("corrupt stored part accepted: %v", err)
			}
			if name != "CPU archive" {
				if _, err := s.ImportCoreVideoPart(ctx, asset, "direct"); !errors.Is(err, ErrInvalidCoreVideoPart) {
					t.Fatalf("idempotent import accepted corrupt mapping: %v", err)
				}
			}
		})
	}
}

func TestCoreVideoPartMissingIDsAndCancellation(t *testing.T) {
	s, _ := mediaTestStore(t)
	ctx := context.Background()
	if _, err := s.ReadCoreVideoPart(ctx, strings.Repeat("a", 64)); !errors.Is(err, ErrCoreVideoPartNotFound) {
		t.Fatalf("missing part: %v", err)
	}
	for _, id := range []string{"", "../private", strings.Repeat("A", 64)} {
		if _, err := s.ReadCoreVideoPart(ctx, id); !errors.Is(err, ErrInvalidCoreVideoPart) {
			t.Fatalf("invalid ID %q: %v", id, err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	asset := catalogVideoAsset(t, strings.Repeat("a", 64), 0x11)
	if _, err := s.ImportCoreVideoPart(canceled, asset, "direct"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled import: %v", err)
	}
	if _, err := s.ReadCoreVideoPart(canceled, asset.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read: %v", err)
	}
	if _, err := s.CoreVideoParts(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled inventory: %v", err)
	}
}

func TestCoreVideoPartMigration16PreservesCPUSelectionAndMedia(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	cpu := catalogCPUAsset(t, pkg)
	if _, err := s.ImportCoreExpansion(ctx, cpu); err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateCoreEntry(ctx, "Existing ZX81", "fes.zx81", pkg)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, pkg, "", cpu.ID)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := s.SetCoreExpansionPresentationWithProgress(ctx, cpu.ID, "Saved card", "Keep this description", true)
	if err != nil {
		t.Fatal(err)
	}
	media, _, err := s.ImportCoreMedia(ctx, []byte("saved household media"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct exactly schema 16, without leaving a schema-17 table behind.
	if _, err := db.ExecContext(ctx, `DROP TABLE core_video_parts; PRAGMA user_version=16;`); err != nil {
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
	var version int
	if err := reopened.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("migrated schema = %d, %v", version, err)
	}
	if got, err := reopened.CoreEntryExpansion(ctx, entry.GameID); err != nil || got != selection {
		t.Fatalf("migration changed CPU selection: %+v, %v", got, err)
	}
	if got, err := reopened.CoreExpansionPresentation(ctx, cpu.ID); err != nil || got != copy {
		t.Fatalf("migration changed CPU copy: %+v, %v", got, err)
	}
	if got, err := reopened.ReadCoreExpansion(ctx, cpu.ID); err != nil || !reflect.DeepEqual(got, cpu) {
		t.Fatalf("migration changed CPU archive: %v", err)
	}
	if got, data, err := reopened.CoreMedia(ctx, media.MediaID); err != nil || got != media || string(data) != "saved household media" {
		t.Fatalf("migration changed media: %+v, %v", got, err)
	}
	if rows, err := reopened.CoreVideoParts(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("migration populated video inventory: %+v, %v", rows, err)
	}
	video := catalogVideoAsset(t, pkg, 0x11)
	if _, err := reopened.ImportCoreVideoPart(ctx, video, "scanlines"); err != nil {
		t.Fatalf("migrated catalog cannot import video: %v", err)
	}
	if rows, err := reopened.CoreExpansions(ctx); err != nil || len(rows) != 1 || rows[0].ExpansionID != cpu.ID {
		t.Fatalf("video changed migrated CPU inventory: %+v, %v", rows, err)
	}
}
