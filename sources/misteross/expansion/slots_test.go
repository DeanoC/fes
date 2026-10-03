package expansion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func apple2Shell(t *testing.T) Shell {
	t.Helper()
	shell, _ := fixtures()
	return Shell{PackageID: strings.Repeat("a", 64), BuildID: strings.Repeat("b", 32), Payload: shell,
		Slot: Apple2Slot, SlotMajor: 1}
}

// apple2Card flips the given CRAM bits of the shell and binds the result to a slot.
func apple2Card(t *testing.T, shell Shell, slot int, points ...cramCoordinate) Asset {
	t.Helper()
	decoded, err := loadRBF(shell.Payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range points {
		setCramBit(decoded.cram, p.x, p.y, cramBit(decoded.cram, p.x, p.y)^1)
	}
	cart := saveRBF(decoded)
	manifest := Manifest{CartSHA256: hash(cart), CartSize: int64(len(cart)), Device: Device,
		Format: 1, Map: Apple2Map, RecipeSHA256: strings.Repeat("c", 64), Revision: strings.Repeat("d", 40),
		ShellBuildID: shell.BuildID, ShellPackageID: shell.PackageID, ShellSHA256: hash(shell.Payload),
		Slot: Apple2Slot, SlotIndex: slot, SlotMajor: 1}
	asset, err := NewAsset(manifest, cart)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestSpectrumSocketsAreDisjoint(t *testing.T) {
	sockets := SlotSockets(SpectrumSlot, SpectrumMap)
	if len(sockets) != 4 || sockets[0] != 1 || sockets[1] != 2 || sockets[2] != 3 || sockets[3] != 4 {
		t.Fatalf("sockets = %v", sockets)
	}
	if mapping, ok := SlotMap(SpectrumSlot, 1); !ok || mapping != SpectrumMap {
		t.Fatalf("layout = %q %v", mapping, ok)
	}
	for _, a := range sockets {
		for _, b := range sockets {
			pa, pb := spectrumSockets[a], spectrumSockets[b]
			if a != b && pa.x0 < pb.x1 && pb.x0 < pa.x1 && pa.y0 < pb.y1 && pb.y0 < pa.y1 {
				t.Fatalf("socket %d and %d CRAM rectangles overlap", a, b)
			}
		}
	}
	shell := apple2Shell(t)
	shell.Slot = SpectrumSlot
	donor := apple2Card(t, shell, 2, cramCoordinate{2000, 100})
	manifest := donor.Manifest
	manifest.Slot = SpectrumSlot
	manifest.Map = SpectrumMap
	manifest.SlotIndex = 1
	rebuilt, err := NewAsset(manifest, donor.Cart)
	if err != nil {
		t.Fatal(err)
	}
	composition, linked, err := ComposeSlotsContext(context.Background(), shell, []Asset{rebuilt})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := loadRBF(linked)
	if err != nil {
		t.Fatal(err)
	}
	if cramBit(decoded.cram, 2000, 100) != 0 || composition.Expansions[0].Slot != 1 {
		t.Fatal("spectrum socket 1 did not link inside its rectangle")
	}
}

func TestSlotSocketsAreDisjoint(t *testing.T) {
	sockets := SlotSockets(Apple2Slot, Apple2Map)
	if len(sockets) != 4 || sockets[0] != 2 || sockets[1] != 4 || sockets[2] != 5 || sockets[3] != 7 {
		t.Fatalf("sockets = %v", sockets)
	}
	if SlotSockets(Slot, Map) != nil || SlotSockets(ColecoSlot, ColecoMapV2) != nil {
		t.Fatal("single-socket maps must not report slot sockets")
	}
	for _, a := range sockets {
		for _, b := range sockets {
			pa, pb := apple2Sockets[a], apple2Sockets[b]
			if a != b && pa.x0 < pb.x1 && pb.x0 < pa.x1 && pa.y0 < pb.y1 && pb.y0 < pa.y1 {
				t.Fatalf("slot %d and %d CRAM rectangles overlap", a, b)
			}
		}
	}
}

func TestSlotManifestIndexRules(t *testing.T) {
	shell := apple2Shell(t)
	asset := apple2Card(t, shell, 4, cramCoordinate{2000, 2000})
	if !bytes.Contains(asset.ManifestBytes, []byte(`"slot":"fes.expansion.apple2-bus","slot_index":4,"slot_major":1`)) {
		t.Fatalf("canonical manifest order: %s", asset.ManifestBytes)
	}
	for name, change := range map[string]func(*Manifest){
		"absent-index":       func(m *Manifest) { m.SlotIndex = 0 },
		"logical-only-slot":  func(m *Manifest) { m.SlotIndex = 3 },
		"built-in-slot":      func(m *Manifest) { m.SlotIndex = 6 },
		"zx81-with-index":    func(m *Manifest) { m.Slot, m.Map, m.SlotIndex = Slot, Map, 2 },
		"unsupported-major":  func(m *Manifest) { m.SlotMajor = 2 },
		"unsupported-layout": func(m *Manifest) { m.Map = "fes.apple2-bus.slots/2" },
	} {
		t.Run(name, func(t *testing.T) {
			m := asset.Manifest
			change(&m)
			if _, err := NewAsset(m, asset.Cart); err == nil {
				t.Fatal("accepted an invalid slot manifest")
			}
		})
	}
	if _, _, err := Compose(shell, asset); err == nil {
		t.Fatal("single-socket Compose accepted a multi-socket card")
	}
}

func TestComposeSlotsLinksEachCardInItsSocket(t *testing.T) {
	shell := apple2Shell(t)
	slot2 := apple2Card(t, shell, 2, cramCoordinate{2000, 100}, cramCoordinate{1800, 1700})
	slot7 := apple2Card(t, shell, 7, cramCoordinate{2500, 6000})
	composition, linked, err := ComposeSlotsContext(context.Background(), shell, []Asset{slot7, slot2})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := loadRBF(linked)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture shell sets (2000,100); slot 2 clears it and sets (1800,1700).
	if cramBit(decoded.cram, 2000, 100) != 0 || cramBit(decoded.cram, 1800, 1700) != 1 ||
		cramBit(decoded.cram, 2500, 6000) != 1 || cramBit(decoded.cram, 100, 64) != 1 ||
		cramBit(decoded.cram, 3500, 80) != 1 {
		t.Fatal("linked payload lost a card bit or a shell bit")
	}
	if len(composition.Expansions) != 2 || composition.Expansions[0].Slot != 2 ||
		composition.Expansions[1].Slot != 7 || composition.Expansions[0].ExpansionID != slot2.ID {
		t.Fatalf("expansions = %+v", composition.Expansions)
	}
	material := "fes-composition-v2\x00" + shell.PackageID + "\x00" + "2:" + slot2.ID + "\x00" +
		"7:" + slot7.ID + "\x00" + hash(linked)
	sum := sha256.Sum256([]byte(material))
	if composition.ID != hex.EncodeToString(sum[:]) || composition.PayloadSHA256 != hash(linked) ||
		composition.PayloadSize != int64(len(linked)) || composition.ShellSHA256 != hash(shell.Payload) {
		t.Fatalf("composition identity %+v", composition)
	}
	again, relinked, err := ComposeSlotsContext(context.Background(), shell, []Asset{slot2, slot7})
	if err != nil || again.ID != composition.ID || !bytes.Equal(relinked, linked) {
		t.Fatal("slot composition depends on argument order")
	}
	single, _, err := ComposeSlotsContext(context.Background(), shell, []Asset{slot7})
	if err != nil || single.ID == composition.ID {
		t.Fatal("single-card composition must have its own identity")
	}
}

func TestComposeSlotsRejects(t *testing.T) {
	shell := apple2Shell(t)
	inSlot2 := apple2Card(t, shell, 2, cramCoordinate{2000, 100})
	for name, assets := range map[string][]Asset{
		"none":                {},
		"duplicate-slot":      {inSlot2, apple2Card(t, shell, 2, cramCoordinate{2001, 200})},
		"outside-own-socket":  {apple2Card(t, shell, 4, cramCoordinate{2000, 100})},
		"neighbour-row":       {apple2Card(t, shell, 2, cramCoordinate{2000, 1722})},
		"outside-all-sockets": {apple2Card(t, shell, 5, cramCoordinate{100, 4000})},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ComposeSlotsContext(context.Background(), shell, assets); err == nil {
				t.Fatal("accepted an invalid slot composition")
			}
		})
	}
	other := shell
	other.Payload = bytes.Clone(shell.Payload)
	other.Payload[len(other.Payload)-1] ^= 1
	if err := AdmitSlots(other, []Asset{inSlot2}); err == nil {
		t.Fatal("accepted a card for a different shell")
	}
	wrongBus := shell
	wrongBus.Slot = ColecoSlot
	if err := AdmitSlots(wrongBus, nil); err == nil {
		t.Fatal("accepted a single-socket shell")
	}
}

func TestSlotCompositionIDValidation(t *testing.T) {
	id := strings.Repeat("e", 64)
	for name, expansions := range map[string][]SlotExpansion{
		"empty":      nil,
		"unordered":  {{Slot: 4, ExpansionID: id}, {Slot: 2, ExpansionID: id}},
		"duplicate":  {{Slot: 4, ExpansionID: id}, {Slot: 4, ExpansionID: id}},
		"slot-zero":  {{Slot: 0, ExpansionID: id}},
		"bad-digest": {{Slot: 2, ExpansionID: "E"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := SlotCompositionID(strings.Repeat("a", 64), expansions, id); err == nil {
				t.Fatal("accepted invalid slot expansions")
			}
		})
	}
}

func TestComposeSlotsROM(t *testing.T) {
	base, m := romFixture()
	shell := Shell{PackageID: digest([]byte("package")), BuildID: "0123456789abcdef0123456789abcdef",
		Payload: base, Slot: Apple2Slot, SlotMajor: 1}
	rom := bytes.Repeat([]byte{0xa5}, 1024)
	composition, overlay, programmed, err := ComposeSlotsROM(context.Background(), shell, nil, m, rom)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := LinkROM(context.Background(), base, m, rom)
	if err != nil {
		t.Fatal(err)
	}
	if composition.ID != "" || !bytes.Equal(overlay, base) || !bytes.Equal(programmed, plain) {
		t.Fatal("ROM-only slot link differs from LinkROM")
	}
	card := apple2Card(t, shell, 5, cramCoordinate{2000, 4000})
	composition, overlay, programmed, err = ComposeSlotsROM(context.Background(), shell, []Asset{card}, m, rom)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := loadRBF(programmed)
	if err != nil {
		t.Fatal(err)
	}
	if composition.ID == "" || bytes.Equal(overlay, programmed) || cramBit(decoded.cram, 2000, 4000) != 1 {
		t.Fatal("ROM and slot card were not both linked")
	}
	m.Blocks[0].WordBits[0][0] = uint32(5000*cramWidth + 2000) // slot 5 socket
	if _, _, _, err := ComposeSlotsROM(context.Background(), shell, nil, m, rom); err == nil {
		t.Fatal("accepted a ROM destination inside an unselected socket")
	}
}

func TestAtariStSocketCompositionFence(t *testing.T) {
	sockets := SlotSockets(AtariStSlot, AtariStMap)
	if len(sockets) != 1 || sockets[0] != 1 {
		t.Fatalf("ST sockets = %v", sockets)
	}
	if mapping, ok := SlotMap(AtariStSlot, 1); !ok || mapping != AtariStMap {
		t.Fatalf("ST map = %q %v", mapping, ok)
	}
	shell := apple2Shell(t)
	shell.Slot = AtariStSlot
	donor := apple2Card(t, shell, 2, cramCoordinate{2000, 100})
	manifest := donor.Manifest
	manifest.Slot = AtariStSlot
	manifest.Map = AtariStMap
	manifest.SlotIndex = 1
	card, err := NewAsset(manifest, donor.Cart)
	if err != nil {
		t.Fatal(err)
	}
	composition, linked, err := ComposeSlotsContext(context.Background(), shell, []Asset{card})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := loadRBF(linked)
	if err != nil {
		t.Fatal(err)
	}
	if cramBit(decoded.cram, 2000, 100) != 0 || composition.Expansions[0].Slot != 1 {
		t.Fatal("ST card did not link inside its socket")
	}
	manifest.SlotIndex = 2
	if _, err := NewAsset(manifest, donor.Cart); err == nil {
		t.Fatal("ST accepted nonexistent socket 2")
	}
	manifest.SlotIndex = 1
	outside := apple2Card(t, shell, 4, cramCoordinate{2000, 2000})
	manifest.CartSHA256, manifest.CartSize = outside.Manifest.CartSHA256, outside.Manifest.CartSize
	outsideCard, err := NewAsset(manifest, outside.Cart)
	if err == nil {
		if _, _, err = ComposeSlotsContext(context.Background(), shell, []Asset{outsideCard}); err == nil {
			t.Fatal("ST card escaped its CRAM rectangle")
		}
	}
}
