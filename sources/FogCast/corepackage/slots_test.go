package corepackage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

// apple2Fixture builds a synthetic fes.computer 1.0 shell with the optional
// Apple II slot bus over the misteross blank RBF. With rom it is format 3 and
// carries the synthetic ROM map, whose destinations lie outside every socket.
func apple2Fixture(t *testing.T, rom bool, transforms ...func(string) string) (archive, payload, firmware []byte, inspection Inspection) {
	t.Helper()
	read := func(name string) []byte {
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
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	payload = read("blank.rbf")
	manifest, old := fixtureBytes(t, loadCases(t)[0])
	text := strings.NewReplacer(`id = "fes.pong"`, `id = "fes.apple2"`, `id = "fes.simple-game"`, `id = "fes.computer"`,
		`id = "fes.gamepad"`, `id = "fes.gamepad.ports"`,
		fmt.Sprintf("%x", sha256.Sum256(old)), fmt.Sprintf("%x", sha256.Sum256(payload)),
		fmt.Sprintf("size = %d", len(old)), fmt.Sprintf("size = %d", len(payload))).Replace(string(manifest))
	for _, i := range []struct {
		id       string
		required bool
	}{{"fes.keyboard.hid", true}, {"fes.media.apple2-floppy", true}, {"fes.expansion.apple2-bus", false}} {
		text += fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = %v\n", i.id, i.required)
	}
	for _, transform := range transforms {
		text = transform(text)
	}
	if !rom {
		manifest = []byte(text)
		archive = canonicalArchive(manifest, payload)
		d, err := decode(manifest, payload)
		if err != nil {
			t.Fatal(err)
		}
		return archive, payload, nil, Inspection{PackageID: packageIdentity(manifest, payload), Descriptor: d}
	}
	mapping, firmware := read("map.json"), read("ramp.rom")
	text = strings.Replace(text, "format = 2", "format = 3", 1)
	text += fmt.Sprintf("\n[rom]\nid = \"apple2-firmware\"\nrole = \"firmware\"\nsource_size = %d\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(firmware), len(mapping), sha256.Sum256(mapping))
	manifest = []byte(text)
	archive = canonicalArchive(manifest, payload)
	archive = archive[:len(archive)-1024]
	archive = append(archive, canonicalHeader("rom-map.json", int64(len(mapping)))...)
	archive = append(archive, mapping...)
	archive = append(archive, make([]byte, (512-len(mapping)%512)%512+1024)...)
	d, err := decode(manifest, payload, mapping)
	if err != nil {
		t.Fatal(err)
	}
	return archive, payload, firmware, Inspection{PackageID: packageIdentity(manifest, payload, mapping), Descriptor: d}
}

// apple2Card binds an independently built card to one physical slot. The
// synthetic card carries the shell's own CRAM, so it changes no bit.
func apple2Card(t *testing.T, payload []byte, inspection Inspection, slot int, recipe byte) expansion.Asset {
	t.Helper()
	asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: romDigest(payload), CartSize: int64(len(payload)), Device: expansion.Device, Format: 1,
		Map: expansion.Apple2Map, RecipeSHA256: strings.Repeat(string("0123456789abcdef"[recipe%16]), 64), Revision: strings.Repeat("c", 40),
		ShellBuildID: inspection.Descriptor.Build.ID, ShellPackageID: inspection.PackageID, ShellSHA256: romDigest(payload),
		Slot: expansion.Apple2Slot, SlotIndex: slot, SlotMajor: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestSlotSocketsFollowTheDeclaredBus(t *testing.T) {
	_, _, _, inspection := apple2Fixture(t, false)
	if got := SlotSockets(inspection.Descriptor); !reflect.DeepEqual(got, []int{2, 4, 5, 7}) {
		t.Fatalf("sockets = %v", got)
	}
	for name, transform := range map[string]func(string) string{
		"required-bus": func(s string) string {
			return strings.Replace(s, "id = \"fes.expansion.apple2-bus\"\nmajor = 1\nminor = 0\nrequired = false", "id = \"fes.expansion.apple2-bus\"\nmajor = 1\nminor = 0\nrequired = true", 1)
		},
		"future-bus": func(s string) string {
			return strings.Replace(s, "id = \"fes.expansion.apple2-bus\"\nmajor = 1", "id = \"fes.expansion.apple2-bus\"\nmajor = 2", 1)
		},
		"other-abi": func(s string) string {
			return strings.Replace(s, `id = "fes.computer"`, `id = "fes.simple-computer"`, 1)
		},
		"no-bus": func(s string) string {
			return strings.Replace(s, "\n[[interfaces]]\nid = \"fes.expansion.apple2-bus\"\nmajor = 1\nminor = 0\nrequired = false\n", "", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, _, other := apple2Fixture(t, false, transform)
			if SlotSockets(other.Descriptor) != nil {
				t.Fatal("unsupported bus reported sockets")
			}
		})
	}
}

func TestAtariStSocketCompositionUsesTheSharedMap(t *testing.T) {
	ctx := context.Background()
	archive, payload, _, inspection := apple2Fixture(t, false, func(s string) string {
		return strings.NewReplacer("fes.apple2", "fes.atari-st", "fes.media.apple2-floppy", "fes.media.atari-st-floppy",
			"fes.expansion.apple2-bus", expansion.AtariStSlot).Replace(s)
	})
	bus, mapping, sockets, ok := SlotLayout(inspection.Descriptor)
	if !ok || bus != expansion.AtariStSlot || mapping != expansion.AtariStMap || !reflect.DeepEqual(sockets, []int{1}) {
		t.Fatalf("Atari ST layout %q %q %v %v", bus, mapping, sockets, ok)
	}
	manifest := expansion.Manifest{CartSHA256: romDigest(payload), CartSize: int64(len(payload)), Device: expansion.Device, Format: 1,
		Map: expansion.AtariStMap, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40),
		ShellBuildID: inspection.Descriptor.Build.ID, ShellPackageID: inspection.PackageID, ShellSHA256: romDigest(payload),
		Slot: expansion.AtariStSlot, SlotIndex: 1, SlotMajor: 1}
	card, err := expansion.NewAsset(manifest, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSlotCards(ctx, archive, []expansion.Asset{card}); err != nil {
		t.Fatal(err)
	}
	bundle, err := ComposeSlotArchive(ctx, archive, []expansion.Asset{card})
	if err != nil || len(bundle.Composition.Expansions) != 1 || bundle.Composition.Expansions[0].Slot != 1 || !bytes.Equal(bundle.Payload, payload) {
		t.Fatalf("Atari ST composition %+v %v", bundle, err)
	}
	transport, err := bundle.Write()
	if err != nil {
		t.Fatal(err)
	}
	staged, err := StageComposition(ctx, t.TempDir(), int64(len(transport)), bytes.NewReader(transport))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if staged.SlotComposition == nil || staged.SlotComposition.ID != bundle.Composition.ID || len(staged.SlotDirectories) != 1 || staged.SlotDirectories[0].Slot != 1 {
		t.Fatalf("Atari ST staged composition %+v", staged)
	}
	manifest.SlotIndex = 2
	if _, err := expansion.NewAsset(manifest, payload); err == nil {
		t.Fatal("nonexistent Atari ST socket accepted")
	}
	if ValidateSlotExpansions(archive, []expansion.Asset{card, card}) == nil {
		t.Fatal("two Atari ST cards accepted")
	}
	for _, invalid := range []func(*Descriptor){
		func(d *Descriptor) { d.Interfaces[len(d.Interfaces)-1].Required = true },
		func(d *Descriptor) { d.Interfaces[len(d.Interfaces)-1].Minor = 1 },
		func(d *Descriptor) { d.Interfaces = append(d.Interfaces, Interface{ID: expansion.C64Slot, Major: 1}) },
	} {
		descriptor := inspection.Descriptor
		descriptor.Interfaces = append([]Interface(nil), descriptor.Interfaces...)
		invalid(&descriptor)
		if SlotSockets(descriptor) != nil {
			t.Fatal("invalid Atari ST socket declaration accepted")
		}
	}
}

func TestSlotCompositionBundleStagesAdoptsAndCleansUp(t *testing.T) {
	ctx := context.Background()
	archive, payload, _, inspection := apple2Fixture(t, false)
	slot7, slot2 := apple2Card(t, payload, inspection, 7, 1), apple2Card(t, payload, inspection, 2, 2)
	if err := ValidateSlotCards(ctx, archive, []expansion.Asset{slot7, slot2}); err != nil {
		t.Fatal(err)
	}
	bundle, err := ComposeSlotArchive(ctx, archive, []expansion.Asset{slot7, slot2})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Composition.Expansions) != 2 || bundle.Composition.Expansions[0] != (expansion.SlotExpansion{Slot: 2, ExpansionID: slot2.ID}) ||
		bundle.Composition.PackageID != inspection.PackageID || bundle.Composition.ShellSHA256 != romDigest(payload) {
		t.Fatalf("composition %+v", bundle.Composition)
	}
	transport, err := bundle.Write()
	if err != nil {
		t.Fatal(err)
	}
	if !IsSlotCompositionBundle(transport) || IsROMInput(transport) {
		t.Fatal("slot transport not recognized")
	}
	root := t.TempDir()
	staged, err := StageComposition(ctx, root, int64(len(transport)), bytes.NewReader(transport))
	if err != nil {
		t.Fatal(err)
	}
	if staged.Composition != nil || staged.SlotComposition == nil || !equalSlotComposition(*staged.SlotComposition, bundle.Composition) ||
		len(staged.SlotDirectories) != 2 || staged.SlotDirectories[0].Slot != 2 || staged.SlotDirectories[1].Slot != 7 {
		t.Fatalf("staged %+v", staged)
	}
	for _, directory := range staged.SlotDirectories {
		entries, err := os.ReadDir(directory.Directory)
		if err != nil || len(entries) != 2 || entries[0].Name() != "cart.rbf" || entries[1].Name() != "manifest.json" {
			t.Fatalf("slot directory %s: %v %v", directory.Directory, entries, err)
		}
	}
	linked, err := os.ReadFile(staged.PayloadPath)
	if err != nil || !bytes.Equal(linked, bundle.Payload) {
		t.Fatalf("overlay: %v", err)
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || adopted[0].SlotComposition == nil ||
		!equalSlotComposition(*adopted[0].SlotComposition, bundle.Composition) || !reflect.DeepEqual(adopted[0].SlotDirectories, staged.SlotDirectories) {
		t.Fatalf("adopt %+v %v", adopted, err)
	}
	if err := adopted[0].Cleanup(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("cleanup retained %v", entries)
	}
}

func TestSlotCompositionRejections(t *testing.T) {
	ctx := context.Background()
	archive, payload, _, inspection := apple2Fixture(t, false)
	card := apple2Card(t, payload, inspection, 4, 1)
	bundle, err := ComposeSlotArchive(ctx, archive, []expansion.Asset{card})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := bundle.Write()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ComposeSlotArchive(ctx, archive, []expansion.Asset{card, apple2Card(t, payload, inspection, 4, 2)}); err == nil {
		t.Fatal("two cards in one slot accepted")
	}
	other := card.Manifest
	other.ShellPackageID = strings.Repeat("0", 64)
	foreign, err := expansion.NewAsset(other, card.Cart)
	if err != nil {
		t.Fatal(err)
	}
	if ValidateSlotExpansions(archive, []expansion.Asset{foreign}) == nil {
		t.Fatal("card for another shell accepted")
	}
	if ValidateSlotExpansions(canonicalArchive(fixtureBytes(t, loadCases(t)[0])), []expansion.Asset{card}) == nil {
		t.Fatal("slot card accepted by a shell without the slot bus")
	}
	romArchive, _, _, _ := apple2Fixture(t, true)
	if _, err := ComposeSlotArchive(ctx, romArchive, []expansion.Asset{card}); err == nil {
		t.Fatal("ROM shell composed without its ROM input")
	}
	changed := bundle
	changed.Payload = bytes.Clone(bundle.Payload)
	changed.Payload[len(changed.Payload)-1] ^= 1
	if _, err := changed.Write(); err == nil {
		t.Fatal("changed overlay written")
	}
	root := t.TempDir()
	tampered := bytes.Clone(transport)
	tampered[len(tampered)-1025] ^= 1
	for _, data := range [][]byte{tampered, transport[:len(transport)-512], append(bytes.Clone(transport), make([]byte, 512)...)} {
		if _, err := StageComposition(ctx, root, int64(len(data)), bytes.NewReader(data)); err == nil {
			t.Fatal("malformed slot transport staged")
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("rejection staged %v", entries)
	}
}

func TestROMInputSlotCardsCarryVerifiedEvidence(t *testing.T) {
	ctx := context.Background()
	archive, payload, firmware, inspection := apple2Fixture(t, true)
	plain, err := PrepareROMInput(ctx, ROMInput{Package: archive, ROM: firmware})
	if err != nil || plain.SlotComposition != nil || strings.Contains(string(plain.Data), "slot_expansions") {
		t.Fatalf("plain slot shell: %+v %v", plain.SlotComposition, err)
	}
	root := t.TempDir()
	staged, err := StageROMInput(ctx, root, int64(len(plain.Data)), bytes.NewReader(plain.Data))
	if err != nil || staged.SlotComposition != nil || staged.ROMLink == nil {
		t.Fatalf("plain staging %+v %v", staged, err)
	}
	if err := staged.Cleanup(); err != nil {
		t.Fatal(err)
	}
	cards := []expansion.Asset{apple2Card(t, payload, inspection, 5, 1), apple2Card(t, payload, inspection, 2, 2)}
	transport, err := PrepareROMInput(ctx, ROMInput{Package: archive, ROM: firmware, SlotExpansions: cards})
	if err != nil || transport.SlotComposition == nil || len(transport.SlotComposition.Expansions) != 2 || transport.SlotComposition.Expansions[0].Slot != 2 {
		t.Fatalf("slot transport %+v %v", transport.SlotComposition, err)
	}
	staged, err = StageROMInput(ctx, root, int64(len(transport.Data)), bytes.NewReader(transport.Data))
	if err != nil {
		t.Fatal(err)
	}
	if staged.SlotComposition == nil || !equalSlotComposition(*staged.SlotComposition, *transport.SlotComposition) ||
		staged.ROMLink == nil || staged.ProgrammedPath == "" || staged.PayloadPath == staged.ProgrammedPath || len(staged.SlotDirectories) != 2 {
		t.Fatalf("staged %+v", staged)
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || adopted[0].SlotComposition == nil || *adopted[0].ROMLink != *staged.ROMLink {
		t.Fatalf("adopt %+v %v", adopted, err)
	}
	if err := adopted[0].Cleanup(); err != nil {
		t.Fatal(err)
	}
	// A receipt whose evidence differs from the independent link is refused.
	var receipt romInputReceipt
	reader := &canonicalReader{data: transport.Data}
	encoded, err := reader.read("rom-link.json", 4096)
	if err != nil || json.Unmarshal(encoded, &receipt) != nil {
		t.Fatalf("receipt %v", err)
	}
	receipt.ProgrammedSHA256 = strings.Repeat("0", 64)
	in, _, err := readROMInput(transport.Data)
	if err != nil {
		t.Fatal(err)
	}
	forged, err := writeROMInputReceipt(in, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StageROMInput(ctx, root, int64(len(forged)), bytes.NewReader(forged)); err == nil {
		t.Fatal("forged link evidence staged")
	}
	receipt.Composition, receipt.ProgrammedSHA256, receipt.ProgrammedSize = nil, "", 0
	missing, err := writeROMInputReceipt(in, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StageROMInput(ctx, root, int64(len(missing)), bytes.NewReader(missing)); err == nil {
		t.Fatal("slot input without evidence staged")
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("rejections staged %v", entries)
	}
	if _, err := PrepareROMInput(ctx, ROMInput{Package: archive, ROM: firmware, SlotExpansions: []expansion.Asset{cards[0], apple2Card(t, payload, inspection, 5, 9)}}); err == nil {
		t.Fatal("two cards in one slot prepared")
	}
}
