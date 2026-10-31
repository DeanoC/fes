package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/zx81machine"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// The retained synthetic M10K fixture exercises the real archive/linker path;
// neither this package nor its no-op expansion is a working ZX81 bitstream.
func hardwareZX81Fixture(t *testing.T, rom, sessionDisplay bool, majors ...int) (archive, firmware []byte) {
	return hardwareZX81ContractFixture(t, rom, sessionDisplay, "fes.simple-computer", "fes.media.blob", strings.Repeat("1", 40), majors...)
}

func hardwareZX81ContractFixture(t *testing.T, rom, sessionDisplay bool, abi, media, revision string, majors ...int) (archive, firmware []byte) {
	return hardwareZX81ContractFixtureWithROMMap(t, rom, sessionDisplay, abi, media, revision, nil, majors...)
}

func hardwareZX81ContractFixtureWithROMMap(t *testing.T, rom, sessionDisplay bool, abi, media, revision string, romMap []byte, majors ...int) (archive, firmware []byte) {
	return hardwareZX81ContractFixtureWithROMMapAndImage(t, rom, sessionDisplay, abi, media, revision, romMap, nil, majors...)
}

func hardwareZX81ContractFixtureWithROMMapAndImage(t *testing.T, rom, sessionDisplay bool, abi, media, revision string, romMap, machineROM []byte, majors ...int) (archive, firmware []byte) {
	t.Helper()
	payload := misterossROMFixture(t, "blank.rbf")
	manifest, err := os.ReadFile("../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.NewReplacer(`id = "fes.pong"`, `id = "fes.zx81"`, `id = "fes.simple-game"`, `id = "`+abi+`"`,
		`id = "fes.gamepad"`, `id = "fes.keyboard"`, "size = 12", fmt.Sprintf("size = %d", len(payload)),
		"e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", fmt.Sprintf("%x", sha256.Sum256(payload)),
		`revision = "1111111111111111111111111111111111111111"`, `revision = "`+revision+`"`).Replace(string(manifest))
	text += "\n[[interfaces]]\nid = \"" + media + "\"\nmajor = 1\nminor = 0\nrequired = true\n"
	text += "\n[[interfaces]]\nid = \"fes.expansion.zx81-bus\"\nmajor = 1\nminor = 0\nrequired = false\n"
	if sessionDisplay {
		text += "\n[[interfaces]]\nid = \"fes.video.session-display\"\nmajor = 1\nminor = 0\nrequired = true\n"
		text += "\n[[interfaces]]\nid = \"fes.memory.hps-ddr\"\nmajor = 1\nminor = 0\nrequired = true\n"
	}
	if len(majors) != 0 && majors[0] == 2 {
		text = strings.Replace(text, `id = "fes.expansion.zx81-bus"`+"\nmajor = 1", `id = "fes.expansion.zx81-bus"`+"\nmajor = 2", 1)
	}
	members := [][2][]byte{{[]byte("manifest.toml"), nil}, {[]byte("core.rbf"), payload}}
	if rom {
		mapping := romMap
		if mapping == nil {
			mapping = misterossROMFixture(t, "map.json")
		}
		firmware = machineROM
		if firmware == nil {
			firmware = misterossROMFixture(t, "ramp.rom")
		}
		text = strings.Replace(text, "format = 2", "format = 3", 1)
		text += fmt.Sprintf("\n[rom]\nid = \"machine-rom\"\nrole = \"firmware\"\nsource_size = %d\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(firmware), len(mapping), sha256.Sum256(mapping))
		members = append(members, [2][]byte{[]byte("rom-map.json"), mapping})
	}
	members[0][1] = []byte(text)
	return tarMembers(members...), firmware
}

func hardwareExpansion(t *testing.T, inspection corepackage.Inspection, changes ...func(*expansion.Manifest)) (expansion.Asset, []byte) {
	t.Helper()
	cart := misterossROMFixture(t, "blank.rbf")
	manifest := expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)), Device: expansion.Device, Format: 1,
		Map: expansion.Map, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: inspection.Descriptor.Build.ID,
		ShellPackageID: inspection.PackageID, ShellSHA256: inspection.Descriptor.Payload.SHA256, Slot: expansion.Slot, SlotMajor: 1}
	for _, contract := range inspection.Descriptor.Interfaces {
		if contract.ID == expansion.Slot && contract.Major == 2 {
			manifest.SlotMajor, manifest.Map = 2, expansion.MapV2
		}
	}
	for _, change := range changes {
		change(&manifest)
	}
	asset, err := expansion.NewAsset(manifest, cart)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := asset.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	return asset, encoded.Bytes()
}

func TestShelfComputersIncludesAllPlayableLibraryCores(t *testing.T) {
	ctx := context.Background()
	archive, _ := hardwareZX81Fixture(t, true, false)
	s, _, entry, _ := newCoreEntryLaunchFixture(t, archive, "My ZX81", time.Minute)
	store := s.catalog.(*catalog.Store)
	if _, err := store.CreateCoreEntry(ctx, "Commodore 64", "fes.c64", entry.PackageID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCoreEntry(ctx, "Pong", "fes.pong", entry.PackageID); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ShelfComputers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 || listed[0].CoreID != "fes.c64" || listed[1].CoreID != "fes.pong" || listed[2].CoreID != "fes.zx81" || listed[2].GameID != entry.GameID {
		t.Fatalf("shelf %+v", listed)
	}
	if listed[0].Ready || listed[0].UnavailableReason == "" {
		t.Fatalf("mismatched c64 package was launchable: %+v", listed[0])
	}
	for _, card := range listed {
		if card.CoreID == "fes.pong" && card.Ready {
			t.Fatalf("mismatched pong package was launchable: %+v", listed)
		}
	}
}

type shelfReadyMachineROM struct{}

func (shelfReadyMachineROM) Link(context.Context, []byte) ([]byte, string, error) {
	return nil, "", nil
}

type invalidShelfMediaCatalog struct {
	*catalog.Store
}

func (c invalidShelfMediaCatalog) CoreMediaInfo(context.Context, string) (catalog.CoreMedia, error) {
	return catalog.CoreMedia{}, catalog.ErrInvalidCoreMedia
}

func TestShelfComputerRequiresZX81LinkerAndValidSelectedMedia(t *testing.T) {
	ctx := context.Background()
	archive, _ := hardwareZX81Fixture(t, false, false)
	s, _, _, _ := newCoreEntryLaunchFixture(t, archive, "ZX81 with tape", time.Minute)
	listed, err := s.ShelfComputers(ctx)
	if err != nil || len(listed) != 1 || listed[0].Ready {
		t.Fatalf("ZX81 without machine linker looked ready: %+v err=%v", listed, err)
	}

	s.SetMachineROMLinker(shelfReadyMachineROM{})
	listed, err = s.ShelfComputers(ctx)
	if err != nil || len(listed) != 1 || !listed[0].Ready {
		t.Fatalf("ZX81 with valid linker and tape not ready: %+v err=%v", listed, err)
	}

	store := s.catalog.(*catalog.Store)
	s.catalog = invalidShelfMediaCatalog{Store: store}
	listed, err = s.ShelfComputers(ctx)
	if err != nil || len(listed) != 1 || listed[0].Ready {
		t.Fatalf("ZX81 with invalid saved tape looked ready: %+v err=%v", listed, err)
	}
	if listed[0].UnavailableReason == "" {
		t.Fatalf("invalid tape did not explain the blocked card: %+v", listed[0])
	}
}

func TestShelfComputerRequiresNativeVideoAdmission(t *testing.T) {
	ctx := context.Background()
	archive, _ := apple2LibraryPackageFixture(t, false, func(text string) string {
		text = strings.ReplaceAll(text, `id = "fes.apple2"`, `id = "fes.c64"`)
		return text + fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = false\n", expansion.NativeVideoSlot)
	})
	s, _, entry, inspection := newCoreEntryLaunchFixture(t, archive, "C64 native video", time.Minute)
	media := bytes.Repeat([]byte{0}, 143360)
	asset, _, err := s.ImportCoreMedia(ctx, int64(len(media)), bytes.NewReader(media))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.DiskRole, asset.MediaID); err != nil {
		t.Fatal(err)
	}
	if !hasVideoSocket(inspection) || !requiresVideoPart(inspection) {
		t.Fatalf("fixture did not declare required native video: %+v", inspection.Descriptor.Interfaces)
	}
	listed, err := s.ShelfComputers(ctx)
	if err != nil || len(listed) != 1 || listed[0].Ready {
		t.Fatalf("native-video computer without an admitted video part looked ready: %+v err=%v", listed, err)
	}
}

func TestZX81RearSocketAdmitsComputerAndLegacyShells(t *testing.T) {
	bus := corepackage.Interface{ID: expansion.Slot, Major: 2}
	for _, abi := range []string{"fes.computer", "fes.simple-computer"} {
		if !zx81RearSocket(corepackage.Descriptor{ABI: corepackage.Contract{ID: abi, Major: 1}, Interfaces: []corepackage.Interface{bus}}) {
			t.Fatalf("%s rear socket rejected", abi)
		}
	}
	if zx81RearSocket(corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.application", Major: 1}, Interfaces: []corepackage.Interface{bus}}) {
		t.Fatal("application ABI accepted as a ZX81 rear socket")
	}
}

func TestHardwareOffersSelectorCapablePackageOnlyWithoutExpansion(t *testing.T) {
	ctx := context.Background()
	baseArchive, _ := hardwareZX81Fixture(t, false, false)
	s, _, entry, base := newCoreEntryLaunchFixture(t, baseArchive, "Workshop ZX81")
	s.SetMachineROMLinker(fakeROM{})
	selectorArchive, _ := hardwareZX81Fixture(t, false, true)
	selector, created, err := s.ImportCorePackage(ctx, int64(len(selectorArchive)), bytes.NewReader(selectorArchive))
	if err != nil || !created {
		t.Fatalf("import selector package: %v created=%v", err, created)
	}
	view, err := s.Hardware(ctx)
	if err != nil || len(view) != 1 || !view[0].Ready || view[0].LaunchPackageID != selector.PackageID {
		t.Fatalf("selector package not offered: %+v err=%v", view, err)
	}
	selected, err := s.SelectCoreEntry(ctx, entry.GameID, entry.PackageID, selector.PackageID)
	if err != nil || selected.PackageID != selector.PackageID {
		t.Fatalf("selector package selection: %+v err=%v", selected, err)
	}
	selected, err = s.SelectCoreEntry(ctx, entry.GameID, selector.PackageID, entry.PackageID)
	if err != nil || selected.PackageID != entry.PackageID {
		t.Fatalf("restore current package: %+v err=%v", selected, err)
	}

	asset, data := hardwareExpansion(t, base)
	if _, err = s.ImportCoreExpansion(ctx, int64(len(data)), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, "", asset.ID); err != nil {
		t.Fatal(err)
	}
	view, err = s.Hardware(ctx)
	if err != nil || len(view) != 1 || view[0].LaunchPackageID != "" {
		t.Fatalf("package switch offered with an exact-package expansion: %+v err=%v", view, err)
	}
}

func TestHardwareOffersSelectorPackageWithDifferentROMMapDigest(t *testing.T) {
	ctx := context.Background()
	currentArchive, firmware := hardwareZX81ContractFixture(t, true, false, "fes.simple-computer", "fes.media.blob", strings.Repeat("4", 40))
	s, _, entry, current := newCoreEntryLaunchFixture(t, currentArchive, "Workshop ZX81")
	s.SetMachineROMLinker(fakeROM{})
	firmwareMedia, _, err := s.ImportCoreMedia(ctx, int64(len(firmware)), bytes.NewReader(firmware))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "machine-rom", "", firmwareMedia.MediaID); err != nil {
		t.Fatal(err)
	}

	romMap := append([]byte(nil), misterossROMFixture(t, "map.json")...)
	romMap = append(romMap, '\n')
	selectorArchive, _ := hardwareZX81ContractFixtureWithROMMap(t, true, true, "fes.simple-computer", "fes.media.blob", strings.Repeat("5", 40), romMap)
	selector, created, err := s.ImportCorePackage(ctx, int64(len(selectorArchive)), bytes.NewReader(selectorArchive))
	if err != nil || !created {
		t.Fatalf("import selector package: %v created=%v", err, created)
	}
	if current.Descriptor.ROM.SHA256 == selector.Descriptor.ROM.SHA256 {
		t.Fatal("fixture packages should have different sealed ROM-map digests")
	}

	view, err := s.Hardware(ctx)
	if err != nil || len(view) != 1 || !view[0].Ready || view[0].LaunchPackageID != selector.PackageID {
		t.Fatalf("selector package with the same machine firmware was not offered: %+v err=%v", view, err)
	}
}

func TestHardwareSkipsSelectorCandidatesThatRejectSavedMedia(t *testing.T) {
	ctx := context.Background()
	currentArchive, _ := hardwareZX81ContractFixture(t, false, false, "fes.computer", "fes.media.zx81-tape", strings.Repeat("2", 40))
	s, _, entry, _ := newCoreEntryLaunchFixture(t, currentArchive, "Tape ZX81")
	s.SetMachineROMLinker(fakeROM{})
	media, _, err := s.ImportCoreMedia(ctx, int64(len("saved tape")), strings.NewReader("saved tape"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.CassetteRole, media.MediaID); err != nil {
		t.Fatal(err)
	}

	selectorArchive, _ := hardwareZX81ContractFixture(t, false, true, "fes.computer", "fes.media.zx81-tape", strings.Repeat("3", 40))
	selector, created, err := s.ImportCorePackage(ctx, int64(len(selectorArchive)), bytes.NewReader(selectorArchive))
	if err != nil || !created {
		t.Fatalf("import tape selector package: %v created=%v", err, created)
	}
	invalidBeforeSelector := false
	for i := 0; i < 256 && !invalidBeforeSelector; i++ {
		revision := fmt.Sprintf("%040x", i+4)
		invalidArchive, _ := hardwareZX81ContractFixture(t, false, true, "fes.simple-computer", "fes.media.blob", revision)
		candidate, created, err := s.ImportCorePackage(ctx, int64(len(invalidArchive)), bytes.NewReader(invalidArchive))
		if err != nil || !created {
			t.Fatalf("import blob selector package %s: %v created=%v", revision, err, created)
		}
		invalidBeforeSelector = candidate.PackageID < selector.PackageID
	}
	if !invalidBeforeSelector {
		t.Fatal("could not create a media-incompatible selector candidate ordered before the usable package")
	}

	view, err := s.Hardware(ctx)
	if err != nil || len(view) != 1 || !view[0].Ready || view[0].LaunchPackageID != selector.PackageID {
		t.Fatalf("later media-compatible selector package was not offered: %+v err=%v", view, err)
	}
}

func TestZX81SessionDisplayDescriptorRequiresABICompatibleMedia(t *testing.T) {
	base := corepackage.Descriptor{
		Core: corepackage.Core{ID: "fes.zx81"},
		ABI:  corepackage.Contract{ID: "fes.simple-computer", Major: 1, Minor: 0},
		Interfaces: []corepackage.Interface{
			{ID: expansion.Slot, Major: 1, Minor: 0},
			{ID: "fes.video.session-display", Major: 1, Minor: 0},
			{ID: "fes.memory.hps-ddr", Major: 1, Minor: 0},
			{ID: "fes.media.blob", Major: 1, Minor: 0},
		},
	}
	required := base
	required.Interfaces = append([]corepackage.Interface(nil), base.Interfaces...)
	for i := range required.Interfaces {
		if required.Interfaces[i].ID != expansion.Slot {
			required.Interfaces[i].Required = true
		}
	}
	if !zx81SessionDisplayDescriptor(required, nil) {
		t.Fatal("required simple-computer selector interfaces should enable the selector")
	}

	activeOptional := base
	activeOptional.Interfaces = append([]corepackage.Interface(nil), base.Interfaces...)
	for i := range activeOptional.Interfaces {
		if activeOptional.Interfaces[i].ID != expansion.Slot {
			activeOptional.Interfaces[i].Required = false
		}
	}
	active := []protocol.RuntimeInterface{
		{ID: "fes.video.session-display", Major: 1, Minor: 0},
		{ID: "fes.memory.hps-ddr", Major: 1, Minor: 0},
		{ID: "fes.media.blob", Major: 1, Minor: 0},
	}
	if !zx81SessionDisplayDescriptor(activeOptional, active) {
		t.Fatal("optional selector interfaces confirmed active on the target should enable the selector")
	}
	if zx81SessionDisplayDescriptor(activeOptional, nil) {
		t.Fatal("optional selector interfaces without target confirmation must not enable the selector")
	}
	activeWithoutMedia := active[:2]
	if zx81SessionDisplayDescriptor(activeOptional, activeWithoutMedia) {
		t.Fatal("optional media interface absent from target status must not enable the selector")
	}

	computerBlob := required
	computerBlob.ABI.ID = "fes.computer"
	if zx81SessionDisplayDescriptor(computerBlob, nil) {
		t.Fatal("fes.computer with blob alone cannot open the ZX81 tape selector")
	}

	computerTape := computerBlob
	computerTape.Interfaces = append([]corepackage.Interface(nil), required.Interfaces...)
	computerTape.Interfaces[len(computerTape.Interfaces)-1].ID = "fes.media.zx81-tape"
	if !zx81SessionDisplayDescriptor(computerTape, nil) {
		t.Fatal("fes.computer with zx81-tape should support the selector")
	}

	simpleTape := required
	simpleTape.Interfaces = append([]corepackage.Interface(nil), computerTape.Interfaces...)
	simpleTape.ABI.ID = "fes.simple-computer"
	if zx81SessionDisplayDescriptor(simpleTape, nil) {
		t.Fatal("fes.simple-computer cannot use the fes.computer tape interface")
	}
}

func TestZX81SessionDisplayCandidatesMatchStableMachineROMIdentity(t *testing.T) {
	target := corepackage.Target{Platform: "de10_nano", Device: "5CSEBA6U23I7", ProgrammingProfile: "fes-gp-v1"}
	current := corepackage.Descriptor{
		Core:   corepackage.Core{ID: "fes.zx81"},
		Target: target,
		ROM:    &corepackage.ROM{ID: "machine-rom", Role: "firmware", SourceSize: 8192, File: "rom-map.json", Size: 742014, SHA256: strings.Repeat("a", 64)},
	}
	selector := corepackage.Descriptor{
		Core:   corepackage.Core{ID: "fes.zx81"},
		Target: target,
		ABI:    corepackage.Contract{ID: "fes.computer", Major: 1},
		ROM:    &corepackage.ROM{ID: "machine-rom", Role: "firmware", SourceSize: 8192, File: "rom-map.json", Size: 742014, SHA256: strings.Repeat("b", 64)},
		Interfaces: []corepackage.Interface{
			{ID: expansion.Slot, Major: 2, Minor: 0},
			{ID: "fes.video.session-display", Major: 1, Minor: 0, Required: true},
			{ID: "fes.memory.hps-ddr", Major: 1, Minor: 0, Required: true},
			{ID: "fes.media.zx81-tape", Major: 1, Minor: 0, Required: true},
		},
	}
	wrongID := selector
	wrongID.ROM = &corepackage.ROM{ID: "other-rom", Role: "firmware", SourceSize: 8192}
	wrongRole := selector
	wrongRole.ROM = &corepackage.ROM{ID: "machine-rom", Role: "cartridge", SourceSize: 8192}
	wrongSize := selector
	wrongSize.ROM = &corepackage.ROM{ID: "machine-rom", Role: "firmware", SourceSize: 4096}

	packages := []corepackage.Inspection{
		{PackageID: "wrong-id", Descriptor: wrongID},
		{PackageID: "wrong-role", Descriptor: wrongRole},
		{PackageID: "wrong-size", Descriptor: wrongSize},
		{PackageID: "selector", Descriptor: selector},
	}
	candidates := zx81SessionDisplayCandidates(packages, "selected", current)
	if len(candidates) != 1 || candidates[0].PackageID != "selector" {
		t.Fatalf("candidate with a package-specific ROM-map digest should match the same machine firmware: %+v", candidates)
	}
}

func TestHardwareSnapshotSurvivesInvalidUnrelatedPackage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	packageRoot := filepath.Join(root, "packages")
	catalogStore, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalogStore.Close() })
	packages, err := corepackage.NewStore(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, catalogStore,
		&fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	s.corePackages = packages
	archive, _ := hardwareZX81Fixture(t, false, false)
	inspection, created, err := s.ImportCorePackage(ctx, int64(len(archive)), bytes.NewReader(archive))
	if err != nil || !created {
		t.Fatalf("import ZX81 package: %v created=%v", err, created)
	}
	if _, err := catalogStore.CreateCoreEntry(ctx, "Saved ZX81", "fes.zx81", inspection.PackageID); err != nil {
		t.Fatal(err)
	}
	s.SetMachineROMLinker(fakeROM{})

	otherArchive := libraryPackageFixture(t, "0.2.0")
	other, created, err := s.ImportCorePackage(ctx, int64(len(otherArchive)), bytes.NewReader(otherArchive))
	if err != nil || !created {
		t.Fatalf("install unrelated package: %v created=%v", err, created)
	}
	if err := os.Chmod(filepath.Join(packageRoot, other.PackageID+".fcore"), 0o600); err != nil {
		t.Fatal(err)
	}

	view, err := s.Hardware(ctx)
	if err != nil || len(view) != 1 || !view[0].Ready || view[0].LaunchPackageID != "" {
		t.Fatalf("unrelated corrupt archive hid the selected ZX81 setup: %+v err=%v", view, err)
	}
}

func TestHardwareSnapshotUsesExactPackageAdmissionAndFirmware(t *testing.T) {
	for _, major := range []int{1, 2} {
		t.Run(fmt.Sprintf("bus%d", major), func(t *testing.T) {
			testHardwareSnapshotUsesExactPackageAdmissionAndFirmware(t, major)
		})
	}
}

func TestZX81MissingMachineROMIsProvisionedWithoutReplacingUserSelection(t *testing.T) {
	ctx := context.Background()
	firmware := bytes.Repeat([]byte{0x00}, 8192)
	archive, _ := hardwareZX81ContractFixtureWithROMMapAndImage(t, true, true, "fes.computer", "fes.media.zx81-tape", strings.Repeat("6", 40), nil, firmware)
	s, _, entry, _ := newCoreEntryLaunchFixture(t, archive, "Workshop ZX81")
	before, err := s.CoreEntryROM(ctx, entry.GameID)
	if err != nil || before.MediaID != "" {
		t.Fatalf("fixture should start without a ROM selection: %+v err=%v", before, err)
	}

	view, err := s.Hardware(ctx)
	if err != nil || len(view) != 1 || !view[0].Ready || !view[0].FirmwareReady {
		t.Fatalf("missing-ROM setup was not auto-provisioned: %+v err=%v", view, err)
	}
	selected, err := s.CoreEntryROM(ctx, entry.GameID)
	if err != nil || selected.PackageID != entry.PackageID || selected.ROMID != "machine-rom" || selected.SourceSize != 8192 || selected.MediaID == "" {
		t.Fatalf("provisioned ROM selection = %+v err=%v", selected, err)
	}
	compositions, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !compositions[entry.GameID].FirmwareReady || compositions[entry.GameID].ROMMediaID != selected.MediaID {
		t.Fatalf("missing-ROM launch projection = %+v err=%v", compositions[entry.GameID], err)
	}
	object, reader, err := s.catalog.(coreMediaCatalog).OpenCoreMedia(ctx, selected.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	provisioned, err := io.ReadAll(reader)
	expected, expectedErr := zx81machine.MachineROM()
	if err != nil || expectedErr != nil || object.Size != 8192 || !bytes.Equal(provisioned, expected) {
		t.Fatalf("provisioned media size=%d bytes=%d err=%v", object.Size, len(provisioned), err)
	}

	custom := bytes.Repeat([]byte{0x5a}, 8192)
	customMedia, _, err := s.ImportCoreMedia(ctx, int64(len(custom)), bytes.NewReader(custom))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "machine-rom", selected.MediaID, customMedia.MediaID); err != nil {
		t.Fatal(err)
	}
	view, err = s.Hardware(ctx)
	if err != nil || len(view) != 1 || !view[0].Ready {
		t.Fatalf("user-selected firmware was changed or rejected: %+v err=%v", view, err)
	}
	selected, err = s.CoreEntryROM(ctx, entry.GameID)
	if err != nil || selected.MediaID != customMedia.MediaID {
		t.Fatalf("user-selected ROM was replaced: %+v err=%v", selected, err)
	}
}

func testHardwareSnapshotUsesExactPackageAdmissionAndFirmware(t *testing.T, major int) {
	ctx := context.Background()
	archive, firmware := hardwareZX81Fixture(t, true, false, major)
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, archive, "My ZX81", time.Minute)
	asset, data := hardwareExpansion(t, inspection)
	if _, err := s.ImportCoreExpansion(ctx, int64(len(data)), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	wrongShell, _ := hardwareExpansion(t, inspection, func(m *expansion.Manifest) { m.ShellSHA256 = strings.Repeat("0", 64) })
	otherPackage, _ := hardwareExpansion(t, inspection, func(m *expansion.Manifest) { m.ShellPackageID = strings.Repeat("0", 64) })
	store := s.catalog.(*catalog.Store)
	for _, value := range []expansion.Asset{wrongShell, otherPackage} {
		if _, err := store.ImportCoreExpansion(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateCoreEntry(ctx, "Pong", "fes.pong", entry.PackageID); err != nil {
		t.Fatal(err)
	}
	view, err := s.Hardware(ctx)
	if err != nil || len(view) != 1 {
		t.Fatalf("snapshot %+v %v", view, err)
	}
	machine := view[0]
	if machine.GameID != entry.GameID || !machine.PackageReady || !machine.FirmwareReady || !machine.Ready || !machine.Socket.Supported || machine.Socket.ID != "rear" || len(machine.Choices) != 2 {
		t.Fatalf("default firmware readiness %+v", machine)
	}
	for _, choice := range machine.Choices {
		if choice.ExpansionID == otherPackage.ID || choice.Ready != (choice.ExpansionID == asset.ID) || choice.Label == "" || choice.Description == "" {
			t.Fatalf("incorrect admission or fallback %+v", choice)
		}
	}
	media, _, err := s.ImportCoreMedia(ctx, int64(len(firmware)), bytes.NewReader(firmware))
	if err != nil {
		t.Fatal(err)
	}
	selectedROM, err := s.CoreEntryROM(ctx, entry.GameID)
	if err != nil || selectedROM.MediaID == "" {
		t.Fatalf("missing provisioned default firmware binding: %+v %v", selectedROM, err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "machine-rom", selectedROM.MediaID, media.MediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreExpansionPresentation(ctx, asset.ID, "Workshop test card", "No-op linker fixture; adds no emulated hardware."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, "", asset.ID); err != nil {
		t.Fatal(err)
	}
	view, err = s.Hardware(ctx)
	if err != nil || !view[0].FirmwareReady || !view[0].Ready || view[0].DraftExpansionID != asset.ID {
		t.Fatalf("saved setup %+v %v", view, err)
	}
	for _, choice := range view[0].Choices {
		if choice.ExpansionID == asset.ID && (choice.Label != "Workshop test card" || choice.Description != "No-op linker fixture; adds no emulated hardware.") {
			t.Fatalf("presentation lost %+v", choice)
		}
	}
	for name, change := range map[string]struct {
		packageID, expected, selected string
		code                          protocol.ErrorCode
	}{
		"wrong shell":   {entry.PackageID, asset.ID, wrongShell.ID, protocol.CodeBadRequest},
		"wrong package": {entry.PackageID, asset.ID, otherPackage.ID, protocol.CodeBadRequest},
		"stale draft":   {entry.PackageID, "", "", protocol.CodeStaleRevision},
		"stale package": {otherPackage.Manifest.ShellPackageID, asset.ID, wrongShell.ID, protocol.CodeStaleRevision},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, change.packageID, change.expected, change.selected)
			var api *protocol.APIError
			if !errors.As(err, &api) || api.Code != change.code {
				t.Fatalf("selection error %v, want %s", err, change.code)
			}
		})
	}
	if client.coreCalls != 0 || client.stopCalls != 0 {
		t.Fatal("browsing or saving hardware mutated the target")
	}
	// A stale selection saved by an older host remains visible and removable.
	if _, err := store.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, asset.ID, wrongShell.ID); err != nil {
		t.Fatal(err)
	}
	view, err = s.Hardware(ctx)
	if err != nil || view[0].Ready || view[0].UnavailableReason == "" {
		t.Fatalf("wrong-shell saved selection looked ready: %+v %v", view, err)
	}
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, wrongShell.ID, ""); err != nil {
		t.Fatal(err)
	}
	zx80, err := store.CreateCoreEntry(ctx, "ZX80", "fes.zx80", entry.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	view, err = s.Hardware(ctx)
	if err != nil || len(view) != 2 {
		t.Fatalf("family snapshot %+v %v", view, err)
	}
	var sawZX80 bool
	for _, machine := range view {
		if machine.GameID != zx80.GameID {
			continue
		}
		sawZX80 = true
		if machine.CoreID != "fes.zx80" || machine.PackageReady || machine.PackageID != entry.PackageID {
			t.Fatalf("zx80 package admission %+v", machine)
		}
	}
	if !sawZX80 {
		t.Fatal("zx80 setup missing")
	}
}

func TestHardwareDraftChangePreservesLoadedExpansion(t *testing.T) {
	ctx := context.Background()
	archive, firmware := hardwareZX81Fixture(t, true, false)
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, archive, "Running ZX81", time.Minute)
	media, _, err := s.ImportCoreMedia(ctx, int64(len(firmware)), bytes.NewReader(firmware))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "machine-rom", "", media.MediaID); err != nil {
		t.Fatal(err)
	}
	asset, data := hardwareExpansion(t, inspection)
	if _, err := s.ImportCoreExpansion(ctx, int64(len(data)), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, "", asset.ID); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	client.coreLoad = func(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
		staged, err := corepackage.StageROMInput(ctx, root, size, body)
		if err != nil {
			return protocol.Status{}, err
		}
		defer staged.Cleanup()
		status := coreEntryActiveStatus(inspection, 7, true)
		status.CorePackage.ActiveInterfaces = append(status.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: "fes.keyboard", Major: 1})
		status.CorePackage.ROMLink = staged.ROMLink
		status.CorePackage.Composition = staged.Composition
		client.statusResult, client.mediaStatus = status, status
		return status, nil
	}
	before, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || before.Status.CorePackage == nil || before.Status.CorePackage.Composition == nil || before.Status.CorePackage.Composition.ExpansionID != asset.ID {
		t.Fatalf("launch %+v %v", before, err)
	}
	loads, stops, mediaCalls := client.coreCalls, client.stopCalls, client.mediaCalls
	if _, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, asset.ID, ""); err != nil {
		t.Fatal(err)
	}
	view, err := s.Hardware(ctx)
	if err != nil || len(view) != 1 || view[0].DraftExpansionID != "" || !view[0].Ready {
		t.Fatalf("empty next-launch draft %+v %v", view, err)
	}
	after, err := s.Status(ctx)
	if err != nil || after.State != protocol.StateActive || after.GameID == nil || *after.GameID != entry.GameID || after.CorePackage.Generation != 7 || after.CorePackage.PackageID != entry.PackageID || after.CorePackage.Composition == nil || after.CorePackage.Composition.ExpansionID != asset.ID {
		t.Fatalf("draft edit changed active hardware: %+v %v", after, err)
	}
	if client.coreCalls != loads || client.stopCalls != stops || client.mediaCalls != mediaCalls {
		t.Fatal("draft save triggered a physical or live-media transition")
	}
}

type blockedHardwareCatalog struct {
	*catalog.Store
	entered, resume chan struct{}
}

func (c *blockedHardwareCatalog) CoreEntries(ctx context.Context) ([]catalog.CoreEntry, error) {
	close(c.entered)
	select {
	case <-c.resume:
		return c.Store.CoreEntries(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestHardwareSnapshotSerializesSelection(t *testing.T) {
	archive, _ := hardwareZX81Fixture(t, false, false)
	s, _, entry, _ := newCoreEntryLaunchFixture(t, archive, "Snapshot ZX81", time.Minute)
	blocked := &blockedHardwareCatalog{Store: s.catalog.(*catalog.Store), entered: make(chan struct{}), resume: make(chan struct{})}
	s.catalog = blocked
	finished := make(chan error, 1)
	go func() { _, err := s.Hardware(context.Background()); finished <- err }()
	<-blocked.entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := s.SelectCoreEntryExpansion(ctx, entry.GameID, entry.PackageID, "", "")
	close(blocked.resume)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("selection interleaved snapshot: %v", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
