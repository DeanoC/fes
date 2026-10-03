package fogcast

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func misterossROMFixture(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("../../misteross/expansion/testdata/rom", name+".gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func tarMembers(members ...[2][]byte) []byte {
	var out bytes.Buffer
	for _, member := range members {
		name, data := string(member[0]), member[1]
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

// apple2LibraryPackageFixture is a synthetic fes.computer 1.0 Apple II shell:
// required video, HID keyboard, controller ports, audio and floppy, plus the
// optional slot bus. With rom it is format 3 with an apple2-firmware ROM.
func apple2LibraryPackageFixture(t *testing.T, rom bool, transforms ...func(string) string) (archive, firmware []byte) {
	t.Helper()
	base := misterossROMFixture(t, "blank.rbf")
	manifest, err := os.ReadFile("../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile("../corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.NewReplacer(`id = "fes.pong"`, `id = "fes.apple2"`, `id = "fes.simple-game"`, `id = "fes.computer"`, `id = "fes.gamepad"`, `id = "fes.gamepad.ports"`,
		fmt.Sprintf("%x", sha256.Sum256(old)), fmt.Sprintf("%x", sha256.Sum256(base)),
		fmt.Sprintf("size = %d", len(old)), fmt.Sprintf("size = %d", len(base))).Replace(string(manifest))
	for _, i := range []struct {
		id       string
		required bool
	}{{"fes.keyboard.hid", true}, {"fes.audio.pcm-s16-stereo-48k", true}, {"fes.media.apple2-floppy", true}, {"fes.expansion.apple2-bus", false}} {
		text += fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = %v\n", i.id, i.required)
	}
	members := [][2][]byte{{[]byte("manifest.toml"), nil}, {[]byte("core.rbf"), base}}
	if rom {
		mapping := misterossROMFixture(t, "map.json")
		firmware = misterossROMFixture(t, "ramp.rom")
		text = strings.Replace(text, "format = 2", "format = 3", 1)
		text += fmt.Sprintf("\n[rom]\nid = \"apple2-firmware\"\nrole = \"firmware\"\nsource_size = %d\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(firmware), len(mapping), sha256.Sum256(mapping))
		members = append(members, [2][]byte{[]byte("rom-map.json"), mapping})
	}
	for _, transform := range transforms {
		text = transform(text)
	}
	members[0][1] = []byte(text)
	return tarMembers(members...), firmware
}

func apple2SlotCard(t *testing.T, inspection corepackage.Inspection, slot int, changes ...func(*expansion.Manifest)) (expansion.Asset, []byte) {
	t.Helper()
	cart := misterossROMFixture(t, "blank.rbf")
	manifest := expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)), Device: expansion.Device, Format: 1,
		Map: expansion.Apple2Map, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: inspection.Descriptor.Build.ID,
		ShellPackageID: inspection.PackageID, ShellSHA256: inspection.Descriptor.Payload.SHA256, Slot: expansion.Apple2Slot, SlotIndex: slot, SlotMajor: 1}
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

func apple2ActiveStatus(inspection corepackage.Inspection, generation uint64) protocol.Status {
	core := inspection.Descriptor.Core.ID
	return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{
		PackageID: inspection.PackageID, Generation: generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
		BuildID: inspection.Descriptor.Build.ID, PersistenceMode: "volatile", Gamepad: true,
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.audio.pcm-s16-stereo-48k", Major: 1}, {ID: "fes.expansion.apple2-bus", Major: 1},
			{ID: "fes.gamepad.ports", Major: 1}, {ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.media.apple2-floppy", Major: 1}, {ID: "fes.video.fixed-720p60", Major: 1}},
		MediaUnits: []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.Apple2FloppyInterface(), MinBytes: 143360, MaxBytes: 143360, ChunkBytes: 512, State: "empty"}},
	}}
}

func TestSlotCardImportSelectionAndReadiness(t *testing.T) {
	ctx := context.Background()
	archive, _ := apple2LibraryPackageFixture(t, true)
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, archive, "Apple II", time.Minute)
	slot2, slot2Archive := apple2SlotCard(t, inspection, 2)
	slot4, slot4Archive := apple2SlotCard(t, inspection, 4)
	for _, data := range [][]byte{slot2Archive, slot4Archive} {
		imported, err := s.ImportCoreExpansion(ctx, int64(len(data)), bytes.NewReader(data))
		if err != nil || imported.PackageID != inspection.PackageID || imported.Slot == 0 {
			t.Fatalf("import %+v %v", imported, err)
		}
	}
	_, foreign := apple2SlotCard(t, inspection, 5, func(m *expansion.Manifest) { m.ShellSHA256 = strings.Repeat("0", 64) })
	var apiErr *protocol.APIError
	if _, err := s.ImportCoreExpansion(ctx, int64(len(foreign)), bytes.NewReader(foreign)); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("card for another shell imported: %v", err)
	}
	view, err := s.CoreEntrySlotExpansions(ctx, entry.GameID)
	if err != nil || !reflect.DeepEqual(view.Sockets, []int{2, 4, 5, 7}) || view.Bus != expansion.Apple2Slot || len(view.Expansions) != 0 || !view.Ready {
		t.Fatalf("empty view %+v %v", view, err)
	}
	view, err = s.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, 2, "", slot2.ID)
	if err != nil || len(view.Expansions) != 1 || view.Expansions[0] != (protocol.SlotExpansionStatus{Slot: 2, ExpansionID: slot2.ID, Ready: true}) {
		t.Fatalf("select %+v %v", view, err)
	}
	for name, call := range map[string]func() error{
		"card for another slot": func() error {
			_, err := s.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, 5, "", slot4.ID)
			return err
		},
		"logical-only slot": func() error {
			_, err := s.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, 3, "", "")
			return err
		},
	} {
		if err := call(); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := s.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, 2, "", slot2.ID); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeStaleRevision {
		t.Fatalf("stale selection: %v", err)
	}
	if _, err := s.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, 4, "", slot4.ID); err != nil {
		t.Fatal(err)
	}
	comps, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !reflect.DeepEqual(comps[entry.GameID].SlotExpansions, []protocol.SlotExpansionStatus{{Slot: 2, ExpansionID: slot2.ID, Ready: true}, {Slot: 4, ExpansionID: slot4.ID, Ready: true}}) {
		t.Fatalf("readiness %+v %v", comps, err)
	}
	if client.coreCalls != 0 || client.stopCalls != 0 {
		t.Fatal("selection contacted the target loader")
	}
}

func TestSlotCardLaunchLinksROMAndCardsOnBothSides(t *testing.T) {
	ctx := context.Background()
	archive, firmware := apple2LibraryPackageFixture(t, true)
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, archive, "Apple II", time.Minute)
	media, _, err := s.ImportCoreMedia(ctx, int64(len(firmware)), bytes.NewReader(firmware))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "apple2-firmware", "", media.MediaID); err != nil {
		t.Fatal(err)
	}
	var cards []expansion.Asset
	for _, slot := range []int{7, 2} {
		card, data := apple2SlotCard(t, inspection, slot)
		if _, err := s.ImportCoreExpansion(ctx, int64(len(data)), bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, slot, "", card.ID); err != nil {
			t.Fatal(err)
		}
		cards = append(cards, card)
	}
	root := t.TempDir()
	var targetComposition *expansion.SlotComposition
	client.coreLoad = func(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
		data, err := io.ReadAll(body)
		if err != nil || int64(len(data)) != size || !corepackage.IsROMInput(data) || !bytes.Contains(data, []byte("slot-2.tar")) || !bytes.Contains(data, []byte("slot-7.tar")) {
			t.Fatalf("transport: %v", err)
		}
		// The target relinks the source envelope independently.
		staged, err := corepackage.StageROMInput(ctx, root, size, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		defer staged.Cleanup()
		targetComposition = staged.SlotComposition
		status := apple2ActiveStatus(inspection, 3)
		status.CorePackage.ROMLink = staged.ROMLink
		status.CorePackage.SlotComposition = staged.SlotComposition
		client.statusResult = status
		return status, nil
	}
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || client.coreCalls != 1 {
		t.Fatalf("launch %+v %v calls=%d", response, err, client.coreCalls)
	}
	got := response.Status.CorePackage.SlotComposition
	if got == nil || targetComposition == nil || got.ID != targetComposition.ID || len(got.Expansions) != 2 ||
		got.Expansions[0] != (expansion.SlotExpansion{Slot: 2, ExpansionID: cards[1].ID}) || got.Expansions[1].Slot != 7 {
		t.Fatalf("session composition %+v", got)
	}
	if response.Status.GameID == nil || *response.Status.GameID != entry.GameID {
		t.Fatalf("library association lost: %+v", response.Status)
	}
}

func TestSlotCardMismatchBlocksLaunchBeforeTarget(t *testing.T) {
	ctx := context.Background()
	archive, firmware := apple2LibraryPackageFixture(t, true)
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, archive, "Apple II", time.Minute)
	media, _, err := s.ImportCoreMedia(ctx, int64(len(firmware)), bytes.NewReader(firmware))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "apple2-firmware", "", media.MediaID); err != nil {
		t.Fatal(err)
	}
	// A card stored and selected outside the service (for example before the
	// shell was rebuilt) names the right package but a different shell payload.
	stale, _ := apple2SlotCard(t, inspection, 5, func(m *expansion.Manifest) { m.ShellSHA256 = strings.Repeat("0", 64) })
	store := s.catalog.(coreSlotExpansionCatalog)
	if _, err := store.ImportCoreExpansion(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, 5, "", stale.ID); err != nil {
		t.Fatal(err)
	}
	comps, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || len(comps[entry.GameID].SlotExpansions) != 1 || comps[entry.GameID].SlotExpansions[0].Ready {
		t.Fatalf("stale card reported ready: %+v %v", comps, err)
	}
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		t.Fatal("stale card reached the target")
		return protocol.Status{}, nil
	}
	var apiErr *protocol.APIError
	if _, err := s.Launch(ctx, entry.GameID, nil); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest || client.coreCalls != 0 || client.stopCalls != 0 {
		t.Fatalf("launch err=%v core=%d stop=%d", err, client.coreCalls, client.stopCalls)
	}
}

type slotComposedClient struct {
	*defaultMediaPackageClient
	root     string
	composed int
}

func (c *slotComposedClient) LoadComposedCore(ctx context.Context, size int64, body io.Reader, packageID string) (protocol.Status, error) {
	c.composed++
	staged, err := corepackage.StageComposition(ctx, c.root, size, body)
	if err != nil {
		return protocol.Status{}, err
	}
	defer staged.Cleanup()
	if staged.PackageID != packageID || staged.SlotComposition == nil || staged.Composition != nil {
		return protocol.Status{}, errors.New("target received a different slot composition")
	}
	status := c.statusResult
	status.CorePackage.SlotComposition = staged.SlotComposition
	c.statusResult = status
	return status, nil
}

func TestSlotCardsComposeROMLessShellAndKeepPlainLoadWithoutCards(t *testing.T) {
	ctx := context.Background()
	archive, _ := apple2LibraryPackageFixture(t, false)
	s, base, entry, inspection := newCoreEntryLaunchFixture(t, archive, "Apple II", time.Minute)
	client := &slotComposedClient{defaultMediaPackageClient: base, root: t.TempDir()}
	s.targetClients[s.selectedTarget] = client
	status := apple2ActiveStatus(inspection, 5)
	base.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		base.statusResult = status
		return status, nil
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil || base.coreCalls != 1 || client.composed != 0 {
		t.Fatalf("plain launch err=%v loads=%d composed=%d", err, base.coreCalls, client.composed)
	}
	card, data := apple2SlotCard(t, inspection, 4)
	if _, err := s.ImportCoreExpansion(ctx, int64(len(data)), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, 4, "", card.ID); err != nil {
		t.Fatal(err)
	}
	next := apple2ActiveStatus(inspection, 6)
	client.statusResult = next
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || client.composed != 1 || base.coreCalls != 1 {
		t.Fatalf("composed launch err=%v composed=%d loads=%d", err, client.composed, base.coreCalls)
	}
	if c := response.Status.CorePackage.SlotComposition; c == nil || len(c.Expansions) != 1 || c.Expansions[0].ExpansionID != card.ID {
		t.Fatalf("session composition %+v", response.Status.CorePackage)
	}
}
