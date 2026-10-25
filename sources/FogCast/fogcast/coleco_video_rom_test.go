package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// colecoNativeROMPackageFixture is the format-4 Coleco native-video shell:
// the 8 KiB BIOS and a 16 KiB cartridge linked through one 24-lane map.
func colecoNativeROMPackageFixture(t *testing.T) []byte {
	t.Helper()
	base := misterossROMFixture(t, "blank.rbf")
	const blocks, words, lanes = 24, 256, 40
	dest, err := expansion.BlankROMDestinations(base, blocks*words*lanes, 124, 32, 3906, 3442)
	if err != nil {
		t.Fatal(err)
	}
	m := expansion.ROMMap{Format: 1, Device: expansion.Device, Encoding: "m10k-1024x10-v1", BaseSHA256: fmt.Sprintf("%x", sha256.Sum256(base)), SourceSize: blocks * 1024}
	for i := 0; i < blocks; i++ {
		bits := make([][]uint32, words)
		for w := range bits {
			start := (i*words + w) * lanes
			bits[w] = append([]uint32(nil), dest[start:start+lanes]...)
		}
		m.Blocks = append(m.Blocks, expansion.ROMBlock{BEL: fmt.Sprintf("fixture-%d", i), SourceOffset: i * 1024, WordBits: bits})
	}
	mapping, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	fixture := "../corepackage/testdata/core-bundle-v2/"
	manifest, err := os.ReadFile(fixture + "manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(fixture + "payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(manifest), "format = 2", "format = 4", 1)
	text = strings.ReplaceAll(text, "fes.simple-game", "fes.application")
	text = strings.ReplaceAll(text, "fes.pong", "fes.coleco")
	text = strings.ReplaceAll(text, fmt.Sprintf("%x", sha256.Sum256(old)), fmt.Sprintf("%x", sha256.Sum256(base)))
	text = strings.ReplaceAll(text, fmt.Sprintf("size = %d", len(old)), fmt.Sprintf("size = %d", len(base)))
	text += fmt.Sprintf("\n[[roms]]\nid = \"coleco-bios\"\nrole = \"firmware\"\nsource_size = 8192\nsource_offset = 0\n\n[[roms]]\nid = \"coleco-cart\"\nrole = \"cartridge\"\nsource_size = 16384\nsource_offset = 8192\n\n[rom_map]\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(mapping), sha256.Sum256(mapping))
	text += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.native-pixels\"\nmajor = 1\nminor = 0\nrequired = false\n"
	return tarMembers([2][]byte{[]byte("manifest.toml"), []byte(text)}, [2][]byte{[]byte("core.rbf"), base}, [2][]byte{[]byte("rom-map.json"), mapping})
}

// A launch links the household BIOS, the title's cartridge and the selected
// native video part on the kit; a cartridge change relinks the same BIOS and
// part with the new cartridge.
func TestColecoNativeROMLaunchLinksBIOSCartridgeAndVideoPart(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoNativeROMPackageFixture(t), "Coleco native", 2*time.Minute)
	s.targetClients[s.selectedTarget] = client
	importMedia := func(fill byte, size int) string {
		data := bytes.Repeat([]byte{fill}, size)
		media, _, err := s.ImportCoreMedia(ctx, int64(len(data)), bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return media.MediaID
	}
	bios, cart, otherCart := importMedia(0x31, 8192), importMedia(0x55, 16384), importMedia(0x66, 16384)
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, bios); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "coleco-cart", "", cart); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || client.coreCalls != 0 {
		t.Fatalf("native shell launched without a video part: %v", err)
	}
	base := misterossROMFixture(t, "blank.rbf")
	part, err := expansion.NewAsset(expansion.Manifest{CartSHA256: inspection.Descriptor.Payload.SHA256, CartSize: int64(len(base)), Device: expansion.Device, Format: 1,
		Map: expansion.ColecoNativeVideoMap, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: inspection.Descriptor.Build.ID,
		ShellPackageID: entry.PackageID, ShellSHA256: inspection.Descriptor.Payload.SHA256, Slot: expansion.NativeVideoSlot, SlotMajor: 1}, base)
	if err != nil {
		t.Fatal(err)
	}
	importVideoFixture(t, s, part, "direct")
	video, err := s.CoreEntryVideo(ctx, entry.GameID)
	if err != nil || video.Builtin || video.PartID != part.ID {
		t.Fatalf("video selection %+v %v", video, err)
	}
	comps, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !comps[entry.GameID].ROMReady || !comps[entry.GameID].FirmwareReady || comps[entry.GameID].ROMSourceSize != 16384 {
		t.Fatalf("readiness: %+v %v", comps, err)
	}
	root := t.TempDir()
	var compositions []*expansion.PartsComposition
	var cartridges []string
	client.coreLoad = func(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
		data, err := io.ReadAll(body)
		if err != nil || int64(len(data)) != size || !corepackage.IsROMInputV2(data) || !bytes.Contains(data, []byte("part-video.tar")) {
			t.Fatal("launch dropped the two-source input or the video part", err)
		}
		staged, err := corepackage.StageROMInputV2(ctx, root, size, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		defer staged.Cleanup()
		if staged.ROMLinks == nil || staged.PartsComposition == nil || staged.PartsComposition.Layout != expansion.ColecoNativeVideoLayout || staged.PartsComposition.Parts[0].PartID != part.ID {
			t.Fatalf("kit staging lost identities: %+v", staged)
		}
		if staged.ROMLinks.Sources[0].SourceSHA256 != bios {
			t.Fatal("household BIOS not linked")
		}
		compositions = append(compositions, staged.PartsComposition)
		cartridges = append(cartridges, staged.ROMLinks.Sources[1].SourceSHA256)
		status := coreEntryActiveStatus(inspection, uint64(len(cartridges)), false)
		status.CorePackage.PersistenceMode = "volatile"
		status.CorePackage.ROMLinks = staged.ROMLinks
		status.CorePackage.PartsComposition = staged.PartsComposition
		client.statusResult = status
		return status, nil
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil || client.coreCalls != 1 {
		t.Fatalf("native ROM launch: %v calls=%d", err, client.coreCalls)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "coleco-cart", cart, otherCart); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil || client.coreCalls != 2 {
		t.Fatalf("cartridge relaunch: %v calls=%d", err, client.coreCalls)
	}
	if !reflect.DeepEqual(cartridges, []string{cart, otherCart}) || compositions[0].ID != compositions[1].ID {
		t.Fatalf("relink changed the part or kept the old cartridge: %v", cartridges)
	}

	// A different kit-side composition for the same request is refused.
	client.coreLoad = func(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
		status := client.statusResult
		pkg := *status.CorePackage
		tuple := *pkg.PartsComposition
		tuple.PayloadSHA256 = strings.Repeat("f", 64)
		pkg.PartsComposition = &tuple
		status.CorePackage = &pkg
		return status, nil
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil {
		t.Fatal("mismatched kit parts receipt accepted")
	}
}

// Only the two sealed Coleco format-4 shapes launch: BIOS with the 16 KiB
// cartridge or the 128 KiB MegaCart.
func TestColecoFormat4LaunchRejectsOtherCartridgeSizes(t *testing.T) {
	pkg := colecoNativeROMPackageFixture(t)
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, pkg, "Coleco native", 2*time.Minute)
	d := inspection.Descriptor
	d.ROMs = append([]corepackage.ROMRequirement(nil), d.ROMs...)
	d.ROMs[1].SourceSize = 32768
	if _, err := s.romLaunchSource(context.Background(), entry, d, pkg, nil); err == nil || client.coreCalls != 0 {
		t.Fatalf("32 KiB cartridge shape accepted: %v", err)
	}
}
