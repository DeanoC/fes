package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/bundledfirmware"
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
	archive, _ := hardwareZX81ContractFixtureWithROMMapAndImage(t, true, true, "fes.computer", "fes.media.zx81-tape", strings.Repeat("6", 40), nil, bytes.Repeat([]byte{0x00}, 8192))
	s, _, entry, _ := newCoreEntryLaunchFixture(t, archive, "Workshop ZX81")
	// Wrong size for the package's machine-rom, and another core's ROM.
	s.LoadBundledFirmware(ctx, bundledFirmwareFixture(t, "fes.zx81", "machine-rom", bytes.Repeat([]byte{1}, 4096)))
	compositions, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || compositions[entry.GameID].FirmwareReady {
		t.Fatalf("a different-size image was selected: %+v %v", compositions[entry.GameID], err)
	}
	s.LoadBundledFirmware(ctx, bundledFirmwareFixture(t, "fes.ts1000", "machine-rom", bytes.Repeat([]byte{1}, 8192)))
	compositions, err = s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || compositions[entry.GameID].FirmwareReady {
		t.Fatalf("another core's image was selected: %+v %v", compositions[entry.GameID], err)
	}
}
