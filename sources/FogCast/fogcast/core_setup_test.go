package fogcast

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoreSetupRepeatPreservesSelections(t *testing.T) {
	s, pid := catalogServiceFixture(t)
	ctx := context.Background()
	req := CoreSetupRequest{LibrarySourceID: "test-library", SourceID: "fes-first-party", CoreID: "fes.pong", PackageID: pid, Title: "Default Pong"}
	a, err := s.CreateCoreSetupEntry(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateCoreSetupEntry(ctx, req)
	if err != nil || a != b {
		t.Fatalf("retry %+v %v", b, err)
	}
	setup, err := s.CoreSetup(ctx, req.SourceID, req.CoreID, pid)
	if err != nil || len(setup.Entries) != 1 || len(setup.ROMs) != 0 {
		t.Fatalf("%+v %v", setup, err)
	}
	req.ROMs = map[string]string{"unknown": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, err = s.CreateCoreSetupEntry(ctx, req); err == nil {
		t.Fatal("accepted unknown ROM")
	}
	for _, c := range s.targetClients {
		if c.(*packageLibraryClient).inspections != 0 {
			t.Fatal("setup contacted target")
		}
	}
}

func publishSetupFixture(t *testing.T, s *Service, raw []byte) (string, string) {
	t.Helper()
	ctx := context.Background()
	p, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(s.coreCatalogPath)
	sum := sha256.Sum256(raw)
	if err := os.WriteFile(filepath.Join(root, "core.fcore"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"version": 1, "source_id": "fes-first-party", "entries": []any{map[string]any{"core_id": p.Descriptor.Core.ID, "label": "Synthetic", "system": "fixture", "standing": "supported", "package_id": p.PackageID, "archive_path": "core.fcore", "archive_sha256": fmt.Sprintf("%x", sum), "archive_size": len(raw)}}}
	b, _ := json.Marshal(body)
	hash := sha256.Sum256(b)
	body["catalog_sha256"] = fmt.Sprintf("%x", hash)
	b, _ = json.Marshal(body)
	if err := os.WriteFile(s.coreCatalogPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p.Descriptor.Core.ID, p.PackageID
}
func sizedSetupArchive(t *testing.T, cid string, size int) []byte {
	t.Helper()
	raw := libraryROMPackageFixture(t, "cartridge")
	reader := tar.NewReader(bytes.NewReader(raw))
	names := []string{}
	files := map[string][]byte{}
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		files[h.Name], err = io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
	}
	var mapping expansion.ROMMap
	if err := json.Unmarshal(files["rom-map.json"], &mapping); err != nil {
		t.Fatal(err)
	}
	mapping.SourceSize = size
	mapping.Blocks = nil
	for block := 0; block < size/1024; block++ {
		b := expansion.ROMBlock{BEL: fmt.Sprintf("M10K.005.%03d", block), SourceOffset: block * 1024, WordBits: make([][]uint32, 256)}
		for word := range b.WordBits {
			b.WordBits[word] = make([]uint32, 40)
			for bit := range b.WordBits[word] {
				b.WordBits[word][bit] = uint32(32*7605 + block*10240 + word*40 + bit)
			}
		}
		mapping.Blocks = append(mapping.Blocks, b)
	}
	oldMap := files["rom-map.json"]
	newMap, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	manifest := files["manifest.toml"]
	manifest = bytes.Replace(manifest, []byte(`id = "fes.zx81"`), []byte(`id = "`+cid+`"`), 1)
	manifest = bytes.Replace(manifest, []byte("source_size = 1024"), []byte(fmt.Sprintf("source_size = %d", size)), 1)
	manifest = bytes.Replace(manifest, []byte(fmt.Sprintf("size = %d\nsha256 = \"%x\"", len(oldMap), sha256.Sum256(oldMap))), []byte(fmt.Sprintf("size = %d\nsha256 = \"%x\"", len(newMap), sha256.Sum256(newMap))), 1)
	files["manifest.toml"] = manifest
	files["rom-map.json"] = newMap
	var out bytes.Buffer
	for _, name := range names {
		data := files[name]
		h := make([]byte, 512)
		copy(h, name)
		copy(h[100:], "0000644\x00")
		copy(h[108:], "0000000\x00")
		copy(h[116:], "0000000\x00")
		copy(h[124:], fmt.Sprintf("%011o\x00", len(data)))
		copy(h[136:], "00000000000\x00")
		copy(h[148:], "        ")
		h[156] = '0'
		copy(h[257:], "ustar\x00")
		copy(h[263:], "00")
		sum := 0
		for _, v := range h {
			sum += int(v)
		}
		copy(h[148:], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(h)
		out.Write(data)
		out.Write(make([]byte, (512-len(data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}
func TestCoreSetupExactCartridgeSizesAndRetry(t *testing.T) {
	for _, tc := range []struct {
		cid  string
		size int
	}{{"fes.sms", 32768}, {"fes.sg1000", 16384}} {
		t.Run(tc.cid, func(t *testing.T) {
			s, _ := catalogServiceFixture(t)
			s.uploadTimeout = 2 * time.Minute // Full-size map parsing under the race detector is not a latency assertion.
			cid, pid := publishSetupFixture(t, s, sizedSetupArchive(t, tc.cid, tc.size))
			ctx := context.Background()
			req := CoreSetupRequest{LibrarySourceID: "test-library", SourceID: "fes-first-party", CoreID: cid, PackageID: pid, Title: "Synthetic"}
			setup, err := s.CoreSetup(ctx, req.SourceID, cid, pid)
			if err != nil || len(setup.ROMs) != 1 || setup.ROMs[0].SourceSize != int64(tc.size) {
				t.Fatalf("%+v %v", setup, err)
			}
			entry, err := s.CreateCoreSetupEntry(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			comps, err := s.CoreCompositions(ctx, []string{entry.GameID})
			if err != nil || comps[entry.GameID].ROMReady {
				t.Fatalf("missing ROM became ready: %+v %v", comps, err)
			}
			wrong, _, err := s.ImportCoreMedia(ctx, int64(tc.size-1), bytes.NewReader(make([]byte, tc.size-1)))
			if err != nil {
				t.Fatal(err)
			}
			req.ROMs = map[string]string{setup.ROMs[0].ID: wrong.MediaID}
			if _, err := s.CreateCoreSetupEntry(ctx, req); err == nil {
				t.Fatal("accepted wrong size")
			}
			media, _, err := s.ImportCoreMedia(ctx, int64(tc.size), bytes.NewReader(make([]byte, tc.size)))
			if err != nil {
				t.Fatal(err)
			}
			req.ROMs[setup.ROMs[0].ID] = media.MediaID
			for i := 0; i < 2; i++ {
				e, err := s.CreateCoreSetupEntry(ctx, req)
				if err != nil || e.GameID != entry.GameID {
					t.Fatalf("retry %+v %v", e, err)
				}
			}
			other, _, err := s.ImportCoreMedia(ctx, int64(tc.size), bytes.NewReader(bytes.Repeat([]byte{1}, tc.size)))
			if err != nil {
				t.Fatal(err)
			}
			req.ROMs[setup.ROMs[0].ID] = other.MediaID
			if _, err := s.CreateCoreSetupEntry(ctx, req); err == nil {
				t.Fatal("replaced existing selection")
			}
			selected, err := s.CoreEntryROM(ctx, entry.GameID)
			if err != nil || selected.MediaID != media.MediaID {
				t.Fatalf("selection changed %+v %v", selected, err)
			}
			projected, ok := s.meshCatalogEntry(entry.GameID)
			if !ok || len(projected.Slots) != 2 || projected.Slots[0].Package.PackageID != pid || projected.Slots[1].Content == nil || projected.Slots[1].Content.Digest != media.MediaID {
				t.Fatalf("named ROM absent from mesh projection: %+v %v", projected, ok)
			}
			root := filepath.Dir(s.coreCatalogPath)
			s.catalog.(*catalog.Store).Close()
			reopened, err := catalog.OpenContext(ctx, filepath.Join(root, "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			s.catalog = reopened
			entries, err := s.CoreEntries(ctx)
			if err != nil || len(entries) != 1 || entries[0].GameID != entry.GameID {
				t.Fatalf("restart %+v %v", entries, err)
			}
			selected, err = s.CoreEntryROM(ctx, entry.GameID)
			if err != nil || selected.MediaID != media.MediaID {
				t.Fatal(selected, err)
			}
			for _, client := range s.targetClients {
				c := client.(*packageLibraryClient)
				if c.inspections != 0 || c.coreCalls != 0 || c.stopCalls != 0 {
					t.Fatal("target contacted")
				}
			}
		})
	}
}
func TestCoreSetupTwoROMBIOSSelectionIsExplicit(t *testing.T) {
	s, _ := catalogServiceFixture(t)
	s.uploadTimeout = 2 * time.Minute
	cid, pid := publishSetupFixture(t, s, twoROMLibraryPackageFixture(t))
	ctx := context.Background()
	setup, err := s.CoreSetup(ctx, "fes-first-party", cid, pid)
	if err != nil || len(setup.ROMs) != 2 {
		t.Fatal(setup, err)
	}
	bios, _, err := s.ImportCoreMedia(ctx, 8192, bytes.NewReader(make([]byte, 8192)))
	if err != nil {
		t.Fatal(err)
	}
	req := CoreSetupRequest{LibrarySourceID: setup.LibrarySourceID, SourceID: setup.SourceID, CoreID: cid, PackageID: pid, Title: "MegaCart", ROMs: map[string]string{setup.ROMs[0].ID: bios.MediaID}}
	if _, err := s.CreateCoreSetupEntry(ctx, req); err == nil {
		t.Fatal("implicitly selected BIOS")
	}
	f, err := s.CoreFirmware(ctx, protocol.FirmwareRole)
	if err != nil || f.MediaID != "" {
		t.Fatal(f, err)
	}
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, bios.MediaID); err != nil {
		t.Fatal(err)
	}
	e, err := s.CreateCoreSetupEntry(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	comps, err := s.CoreCompositions(ctx, []string{e.GameID})
	if err != nil || comps[e.GameID].ROMReady || !comps[e.GameID].FirmwareReady {
		t.Fatal(comps, err)
	}
}
func TestCoreSetupIndependentLibrarySources(t *testing.T) {
	a, _ := catalogServiceFixture(t)
	b, _ := catalogServiceFixture(t)
	raw := libraryROMPackageFixture(t, "cartridge")
	cid, pidA := publishSetupFixture(t, a, raw)
	_, pidB := publishSetupFixture(t, b, raw)
	a.coreLibrarySourceID = "library-one"
	b.coreLibrarySourceID = "library-two"
	ctx := context.Background()
	if pidA != pidB {
		t.Fatal("publication differs")
	}
	req := CoreSetupRequest{SourceID: "fes-first-party", CoreID: cid, PackageID: pidA, Title: "Same title", LibrarySourceID: "library-one"}
	ma, _, err := a.ImportCoreMedia(ctx, 1024, bytes.NewReader(bytes.Repeat([]byte{1}, 1024)))
	if err != nil {
		t.Fatal(err)
	}
	mb, _, err := b.ImportCoreMedia(ctx, 1024, bytes.NewReader(bytes.Repeat([]byte{2}, 1024)))
	if err != nil {
		t.Fatal(err)
	}
	req.ROMs = map[string]string{"machine-rom": ma.MediaID}
	ea, err := a.CreateCoreSetupEntry(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.CreateCoreSetupEntry(ctx, req); err == nil {
		t.Fatal("accepted other library context")
	}
	req.LibrarySourceID = "library-two"
	req.ROMs["machine-rom"] = mb.MediaID
	eb, err := b.CreateCoreSetupEntry(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	sa, err := a.CoreSetup(ctx, req.SourceID, req.CoreID, pidA)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := b.CoreSetup(ctx, req.SourceID, req.CoreID, pidB)
	if err != nil {
		t.Fatal(err)
	}
	ra, _ := a.CoreEntryROM(ctx, ea.GameID)
	rb, _ := b.CoreEntryROM(ctx, eb.GameID)
	if ra.MediaID == rb.MediaID {
		t.Fatal("ROM selections collided")
	}
	if ea.GameID != eb.GameID || sa.LibrarySourceID == sb.LibrarySourceID || sa.SourceID != sb.SourceID {
		t.Fatalf("library references %+v %+v %+v %+v", ea, eb, sa, sb)
	}
}
