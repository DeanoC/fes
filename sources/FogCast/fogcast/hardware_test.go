package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// The retained synthetic M10K fixture exercises the real archive/linker path;
// neither this package nor its no-op expansion is a working ZX81 bitstream.
func hardwareZX81Fixture(t *testing.T, rom bool, majors ...int) (archive, firmware []byte) {
	t.Helper()
	payload := misterossROMFixture(t, "blank.rbf")
	manifest, err := os.ReadFile("../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.NewReplacer(`id = "fes.pong"`, `id = "fes.zx81"`, `id = "fes.simple-game"`, `id = "fes.simple-computer"`,
		`id = "fes.gamepad"`, `id = "fes.keyboard"`, "size = 12", fmt.Sprintf("size = %d", len(payload)),
		"e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", fmt.Sprintf("%x", sha256.Sum256(payload))).Replace(string(manifest))
	text += "\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n"
	text += "\n[[interfaces]]\nid = \"fes.expansion.zx81-bus\"\nmajor = 1\nminor = 0\nrequired = false\n"
	if len(majors) != 0 && majors[0] == 2 {
		text = strings.Replace(text, `id = "fes.expansion.zx81-bus"`+"\nmajor = 1", `id = "fes.expansion.zx81-bus"`+"\nmajor = 2", 1)
	}
	members := [][2][]byte{{[]byte("manifest.toml"), nil}, {[]byte("core.rbf"), payload}}
	if rom {
		mapping := misterossROMFixture(t, "map.json")
		firmware = misterossROMFixture(t, "ramp.rom")
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

func TestHardwareSnapshotUsesExactPackageAdmissionAndFirmware(t *testing.T) {
	for _, major := range []int{1, 2} {
		t.Run(fmt.Sprintf("bus%d", major), func(t *testing.T) {
			testHardwareSnapshotUsesExactPackageAdmissionAndFirmware(t, major)
		})
	}
}

func testHardwareSnapshotUsesExactPackageAdmissionAndFirmware(t *testing.T, major int) {
	ctx := context.Background()
	archive, firmware := hardwareZX81Fixture(t, true, major)
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
	if _, err := store.CreateCoreEntry(ctx, "Unrelated machine", "fes.pong", entry.PackageID); err != nil {
		t.Fatal(err)
	}
	view, err := s.Hardware(ctx)
	if err != nil || len(view) != 1 {
		t.Fatalf("snapshot %+v %v", view, err)
	}
	machine := view[0]
	if machine.GameID != entry.GameID || !machine.PackageReady || machine.FirmwareReady || machine.Ready || !machine.Socket.Supported || machine.Socket.ID != "rear" || len(machine.Choices) != 2 {
		t.Fatalf("unbound firmware readiness %+v", machine)
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
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "machine-rom", "", media.MediaID); err != nil {
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
}

func TestHardwareDraftChangePreservesLoadedExpansion(t *testing.T) {
	ctx := context.Background()
	archive, firmware := hardwareZX81Fixture(t, true)
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
	archive, _ := hardwareZX81Fixture(t, false)
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
