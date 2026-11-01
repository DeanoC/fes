package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/bundledfirmware"
	"github.com/DeanoC/FogCast/internal/zx81machine"
)

// bundledFirmwareFixture writes one bundled object for coreID's romID and
// returns its directory root.
func bundledFirmwareFixture(t *testing.T, coreID, romID string, image []byte) string {
	t.Helper()
	root := t.TempDir()
	id := strings.ReplaceAll(strings.TrimPrefix(coreID, "fes."), "_", "-") + "-test"
	m := bundledfirmware.Manifest{
		Format: bundledfirmware.Format, ID: id, Name: "Test firmware", Version: "1",
		CoreID: coreID, ROMID: romID, File: "firmware.bin", Size: int64(len(image)),
		SHA256:  fmt.Sprintf("%x", sha256.Sum256(image)),
		Licence: "GPL-2.0-only", LicenceName: "GNU General Public License v2", LicenceFile: "LICENSE.txt",
		Notice: "Test notice.",
		Origin: bundledfirmware.Location{URL: "https://example.invalid/firmware.zip", SHA256: strings.Repeat("a", 64)},
		Source: &bundledfirmware.Location{URL: "https://example.invalid/firmware-src.tar.gz", SHA256: strings.Repeat("b", 64)},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{bundledfirmware.ManifestName: raw, m.File: image, m.LicenceFile: []byte("GPL text")} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLoadBundledFirmwareImportsAndListsValidObjects(t *testing.T) {
	ctx := context.Background()
	archive, _ := hardwareZX81ContractFixtureWithROMMapAndImage(t, true, true, "fes.computer", "fes.media.zx81-tape", strings.Repeat("6", 40), nil, bytes.Repeat([]byte{0x00}, 8192))
	s, _, _, _ := newCoreEntryLaunchFixture(t, archive, "Workshop ZX81")
	image := bytes.Repeat([]byte{0x42}, 8192)
	root := bundledFirmwareFixture(t, "fes.zx81", "machine-rom", image)
	// A broken object is skipped and logged; the valid one still loads.
	if err := os.Mkdir(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	var logged []string
	previous := bundledFirmwareLogf
	bundledFirmwareLogf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	t.Cleanup(func() { bundledFirmwareLogf = previous })

	s.LoadBundledFirmware(ctx, root)
	list, err := s.BundledFirmware(ctx)
	if err != nil || len(list.Firmware) != 1 {
		t.Fatalf("list %+v err=%v", list, err)
	}
	fw := list.Firmware[0]
	if fw.CoreID != "fes.zx81" || fw.ROMID != "machine-rom" || fw.MediaID != fmt.Sprintf("%x", sha256.Sum256(image)) ||
		fw.LicenceText != "GPL text" || fw.Notice != "Test notice." || fw.SourceURL == "" || fw.OriginURL == "" {
		t.Fatalf("listed firmware %+v", fw)
	}
	if media, err := s.CoreMedia(ctx, fw.MediaID); err != nil || media.Size != 8192 {
		t.Fatalf("bundled image not in the core-media store: %+v %v", media, err)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "broken") {
		t.Fatalf("logged %q", logged)
	}
}

func TestBundledFirmwareListIsEmptyWithoutDirectory(t *testing.T) {
	archive, _ := hardwareZX81ContractFixtureWithROMMapAndImage(t, true, true, "fes.computer", "fes.media.zx81-tape", strings.Repeat("6", 40), nil, bytes.Repeat([]byte{0x00}, 8192))
	s, _, _, _ := newCoreEntryLaunchFixture(t, archive, "Workshop ZX81")
	s.LoadBundledFirmware(context.Background(), filepath.Join(t.TempDir(), "absent"))
	list, err := s.BundledFirmware(context.Background())
	if err != nil || list.Firmware == nil || len(list.Firmware) != 0 {
		t.Fatalf("list %+v err=%v", list, err)
	}
}

func TestBundledFirmwareIgnoresOtherROMContracts(t *testing.T) {
	ctx := context.Background()
	wantROM, err := zx81machine.MachineROM()
	if err != nil {
		t.Fatal(err)
	}
	wantID := fmt.Sprintf("%x", sha256.Sum256(wantROM))
	for _, tc := range []struct {
		name   string
		coreID string
		size   int
	}{{name: "wrong size", coreID: "fes.zx81", size: 4096}, {name: "other core", coreID: "fes.ts1000", size: 8192}} {
		t.Run(tc.name, func(t *testing.T) {
			archive, _ := hardwareZX81ContractFixtureWithROMMapAndImage(t, true, true, "fes.computer", "fes.media.zx81-tape", strings.Repeat("6", 40), nil, bytes.Repeat([]byte{0x00}, 8192))
			s, _, entry, _ := newCoreEntryLaunchFixture(t, archive, "Workshop ZX81")
			s.LoadBundledFirmware(ctx, bundledFirmwareFixture(t, tc.coreID, "machine-rom", bytes.Repeat([]byte{1}, tc.size)))
			compositions, err := s.CoreCompositions(ctx, []string{entry.GameID})
			if err != nil || !compositions[entry.GameID].FirmwareReady || compositions[entry.GameID].ROMMediaID != wantID {
				t.Fatalf("nonmatching firmware overrode the built-in default: %+v err=%v", compositions[entry.GameID], err)
			}
		})
	}
}

func TestBundledFirmwareDefaultTracksBundleUpdatesWithoutSavingSelection(t *testing.T) {
	ctx := context.Background()
	archive, _ := hardwareZX81ContractFixtureWithROMMapAndImage(t, true, true, "fes.computer", "fes.media.zx81-tape", strings.Repeat("6", 40), nil, bytes.Repeat([]byte{0x00}, 8192))
	s, _, entry, _ := newCoreEntryLaunchFixture(t, archive, "Workshop ZX81")
	first := bytes.Repeat([]byte{0x42}, zx81MachineROMBytes)
	s.LoadBundledFirmware(ctx, bundledFirmwareFixture(t, "fes.zx81", "machine-rom", first))
	firstID := fmt.Sprintf("%x", sha256.Sum256(first))
	compositions, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !compositions[entry.GameID].FirmwareReady || compositions[entry.GameID].ROMMediaID != firstID {
		t.Fatalf("first bundle default %+v err=%v", compositions[entry.GameID], err)
	}
	stored, err := s.catalog.(coreROMCatalog).CoreEntryROM(ctx, entry.GameID)
	if err != nil || stored.MediaID != "" {
		t.Fatalf("default was saved as a title selection: %+v err=%v", stored, err)
	}

	next := bytes.Repeat([]byte{0x43}, zx81MachineROMBytes)
	s.LoadBundledFirmware(ctx, bundledFirmwareFixture(t, "fes.zx81", "machine-rom", next))
	compositions, err = s.CoreCompositions(ctx, []string{entry.GameID})
	nextID := fmt.Sprintf("%x", sha256.Sum256(next))
	if err != nil || !compositions[entry.GameID].FirmwareReady || compositions[entry.GameID].ROMMediaID != nextID {
		t.Fatalf("updated bundle default %+v err=%v", compositions[entry.GameID], err)
	}
	effective, err := s.CoreEntryROM(ctx, entry.GameID)
	if err != nil || effective.MediaID != nextID {
		t.Fatalf("effective selection after bundle update %+v err=%v", effective, err)
	}
}

type countingCoreMediaStore struct {
	*catalog.Store
	opens int
}

func (s *countingCoreMediaStore) OpenCoreMedia(ctx context.Context, id string) (catalog.CoreMedia, io.ReadCloser, error) {
	s.opens++
	return s.Store.OpenCoreMedia(ctx, id)
}

func TestBundledFirmwareLibraryReadinessDoesNotRehashImagePerTitle(t *testing.T) {
	ctx := context.Background()
	archive, _ := hardwareZX81ContractFixtureWithROMMapAndImage(t, true, true, "fes.computer", "fes.media.zx81-tape", strings.Repeat("6", 40), nil, bytes.Repeat([]byte{0x00}, 8192))
	s, _, entry, inspection := newCoreEntryLaunchFixture(t, archive, "Workshop ZX81")
	image := bytes.Repeat([]byte{0x42}, zx81MachineROMBytes)
	s.LoadBundledFirmware(ctx, bundledFirmwareFixture(t, "fes.zx81", "machine-rom", image))
	counted := &countingCoreMediaStore{Store: s.catalog.(*catalog.Store)}
	s.catalog = counted

	compositions, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !compositions[entry.GameID].FirmwareReady || counted.opens != 0 {
		t.Fatalf("listing composition=%+v opens=%d err=%v", compositions[entry.GameID], counted.opens, err)
	}
	if _, _, err := s.readCoreEntryROM(ctx, entry, inspection.Descriptor); err != nil || counted.opens != 1 {
		t.Fatalf("launch validation opens=%d err=%v; launch should still verify the image", counted.opens, err)
	}
}

func TestServiceOpenLoadsBundledFirmwareFromConfiguredDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "isolated-home"))
	firmwareDir := bundledFirmwareFixture(t, "fes.zx81", "machine-rom", bytes.Repeat([]byte{0x42}, zx81MachineROMBytes))
	configPath := filepath.Join(dir, "config.toml")
	config := `base_url = "http://127.0.0.1:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[firmware]
bundled_dir = "` + firmwareDir + `"
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := Open(context.Background(), Paths{
		Config: configPath, Index: filepath.Join(dir, "state", "catalog.sqlite3"), Staging: filepath.Join(dir, "staging"),
	}, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer service.Close()
	list, err := service.BundledFirmware(context.Background())
	if err != nil || len(list.Firmware) != 1 || list.Firmware[0].CoreID != "fes.zx81" || list.Firmware[0].Size != zx81MachineROMBytes {
		t.Fatalf("startup bundled firmware %+v err=%v", list, err)
	}
}
