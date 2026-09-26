package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func romPackageFixture(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	manifest, payload := fixtureBytes(t, loadCases(t)[0])
	sum := sha256.Sum256(payload)
	m := expansion.ROMMap{Format: 1, Device: expansion.Device, Encoding: "m10k-1024x10-v1", BaseSHA256: hex.EncodeToString(sum[:]), SourceSize: 1024, Blocks: []expansion.ROMBlock{{BEL: "M10K.005.073", WordBits: make([][]uint32, 256)}}}
	for w := range m.Blocks[0].WordBits {
		m.Blocks[0].WordBits[w] = make([]uint32, 40)
		for b := range m.Blocks[0].WordBits[w] {
			m.Blocks[0].WordBits[w][b] = uint32(32*7605 + w*40 + b)
		}
	}
	mapping, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(mapping)
	manifest = []byte(strings.Replace(string(manifest), "format = 2", "format = 3", 1) + fmt.Sprintf("\n[rom]\nid = \"machine\"\nrole = \"firmware\"\nsource_size = 1024\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(mapping), hash))
	archive := canonicalArchive(manifest, payload)
	archive = archive[:len(archive)-1024]
	archive = append(archive, canonicalHeader("rom-map.json", int64(len(mapping)))...)
	archive = append(archive, mapping...)
	archive = append(archive, make([]byte, (512-len(mapping)%512)%512+1024)...)
	return manifest, payload, mapping, archive
}

func TestROMPackageStagesAndAdoptsSealedMap(t *testing.T) {
	manifest, payload, mapping, archive := romPackageFixture(t)
	dir := writeDirectory(t, manifest, payload)
	if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), mapping, 0600); err != nil {
		t.Fatal(err)
	}
	inspected, err := InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged, err := Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if staged.PackageID != inspected.PackageID {
		t.Fatal("archive/directory identity mismatch")
	}
	got, err := os.ReadFile(filepath.Join(staged.Directory, "rom-map.json"))
	if err != nil || !bytes.Equal(got, mapping) {
		t.Fatalf("lost map: %v", err)
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || adopted[0].PackageID != staged.PackageID {
		t.Fatalf("adopt: %v %v", adopted, err)
	}
	if err := staged.Cleanup(); err != nil {
		t.Fatal(err)
	}
	// A third member is forbidden in format 2, and v3 cannot omit its map.
	if _, err := Inspect(writeDirectory(t, manifest, payload)); err == nil {
		t.Fatal("missing map accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), append(mapping, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(dir); err == nil {
		t.Fatal("tampered map accepted")
	}
}

func TestROMPackageSharedConformance(t *testing.T) {
	root := "testdata/core-bundle-v3"
	data, err := os.ReadFile(filepath.Join(root, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Manifest, Payload string
		Map                     string `json:"rom_map"`
		Valid                   bool
		PackageID               string `json:"package_id"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			read := func(path string) []byte {
				t.Helper()
				v, e := os.ReadFile(filepath.Join(root, path))
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			manifest, payload, mapping := read(c.Manifest), read(c.Payload), read(c.Map)
			dir := writeDirectory(t, manifest, payload)
			if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), mapping, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := InspectPackage(dir)
			if c.Valid {
				if err != nil || got.PackageID != c.PackageID {
					t.Fatalf("identity got %s want %s: %v", got.PackageID, c.PackageID, err)
				}
			} else if err == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}

func TestROMPackageV4SharedConformance(t *testing.T) {
	root := "testdata/core-bundle-v4"
	data, err := os.ReadFile(filepath.Join(root, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Manifest, Payload string
		Map                     string `json:"rom_map"`
		Valid                   bool
		PackageID               string   `json:"package_id"`
		ArchiveMembers          []string `json:"archive_members"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			read := func(path string) []byte {
				t.Helper()
				b, e := os.ReadFile(filepath.Join(root, path))
				if e != nil {
					t.Fatal(e)
				}
				return b
			}
			manifest, payload, mapping := read(c.Manifest), read(c.Payload), read(c.Map)
			dir := writeDirectory(t, manifest, payload)
			if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), mapping, 0600); err != nil {
				t.Fatal(err)
			}
			if len(c.ArchiveMembers) > 0 {
				if err := os.WriteFile(filepath.Join(dir, "extra.bin"), []byte("extra"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := InspectPackage(dir)
			if c.Valid {
				if err != nil {
					t.Fatal(err)
				}
				if got.PackageID != c.PackageID || got.Descriptor.Format != 4 || len(got.Descriptor.ROMs) != 2 || got.Descriptor.ROMs[0].Role != "firmware" || got.Descriptor.ROMs[1].Role != "cartridge" {
					t.Fatalf("format-4 identity or sources: %+v", got)
				}
			} else if err == nil {
				t.Fatal("invalid format-4 fixture accepted")
			}
		})
	}
}

func TestROMPackageStorePreservesExactArchive(t *testing.T) {
	_, _, _, archive := romPackageFixture(t)
	store, err := NewStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	got, created, err := store.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || !created {
		t.Fatalf("import: %v", err)
	}
	inspection, read, err := store.Read(context.Background(), got.PackageID)
	if err != nil || !bytes.Equal(read, archive) || inspection.Descriptor.ROM == nil {
		t.Fatalf("read: %v", err)
	}
}
