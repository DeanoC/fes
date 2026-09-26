package misterruntime_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/misteross/expansion"
)

func canonicalTar(t *testing.T, members ...[2][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, member := range members {
		name, data := string(member[0]), member[1]
		header := make([]byte, 512)
		copy(header, name)
		copy(header[100:108], "0000644\x00")
		copy(header[108:116], "0000000\x00")
		copy(header[116:124], "0000000\x00")
		copy(header[124:136], fmt.Sprintf("%011o\x00", len(data)))
		copy(header[136:148], "00000000000\x00")
		copy(header[148:156], "        ")
		header[156] = '0'
		copy(header[257:263], "ustar\x00")
		copy(header[263:265], "00")
		sum := 0
		for _, value := range header {
			sum += int(value)
		}
		copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(header)
		out.Write(data)
		out.Write(make([]byte, (512-len(data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

// apple2TargetFixture is a synthetic fes.computer shell with the optional
// Apple II slot bus, optionally format 3 with the misteross ROM map.
func apple2TargetFixture(t *testing.T, rom bool) (archive, firmware []byte, cards []expansion.Asset) {
	t.Helper()
	readGzip := func(name string) []byte {
		f, err := os.Open(filepath.Join("..", "..", "..", "misteross", "expansion", "testdata", "rom", name+".gz"))
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
	base := readGzip("blank.rbf")
	fixture := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(fixture, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(fixture, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.NewReplacer(`id = "fes.pong"`, `id = "fes.apple2"`, `id = "fes.simple-game"`, `id = "fes.computer"`, `id = "fes.gamepad"`, `id = "fes.gamepad.ports"`,
		fmt.Sprintf("%x", sha256.Sum256(old)), fmt.Sprintf("%x", sha256.Sum256(base)),
		fmt.Sprintf("size = %d", len(old)), fmt.Sprintf("size = %d", len(base))).Replace(string(manifest))
	text += "\n[[interfaces]]\nid = \"fes.keyboard.hid\"\nmajor = 1\nminor = 0\nrequired = true\n\n[[interfaces]]\nid = \"fes.expansion.apple2-bus\"\nmajor = 1\nminor = 0\nrequired = false\n"
	members := [][2][]byte{{[]byte("manifest.toml"), nil}, {[]byte("core.rbf"), base}}
	if rom {
		mapping := readGzip("map.json")
		firmware = readGzip("ramp.rom")
		text = strings.Replace(text, "format = 2", "format = 3", 1)
		text += fmt.Sprintf("\n[rom]\nid = \"apple2-firmware\"\nrole = \"firmware\"\nsource_size = %d\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(firmware), len(mapping), sha256.Sum256(mapping))
		members = append(members, [2][]byte{[]byte("rom-map.json"), mapping})
	}
	members[0][1] = []byte(text)
	archive = canonicalTar(t, members...)
	path := filepath.Join(t.TempDir(), "apple2.fcore")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range []int{4, 2} {
		card, err := expansion.NewAsset(expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(base)), CartSize: int64(len(base)), Device: expansion.Device,
			Format: 1, Map: expansion.Apple2Map, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40),
			ShellBuildID: inspection.Descriptor.Build.ID, ShellPackageID: inspection.PackageID, ShellSHA256: fmt.Sprintf("%x", sha256.Sum256(base)),
			Slot: expansion.Apple2Slot, SlotIndex: slot, SlotMajor: 1}, base)
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, card)
	}
	return archive, firmware, cards
}

type slotTargetControl struct {
	*romTargetControl
	slotCalls    int
	romSlotCalls int
	paths        []misterruntime.SlotExpansionPath
	composition  expansion.SlotComposition
}

func (c *slotTargetControl) checkSlots(paths []misterruntime.SlotExpansionPath, payloadPath string) {
	c.paths = paths
	for _, path := range paths {
		entries, err := os.ReadDir(path.Path)
		if err != nil || len(entries) != 2 || entries[0].Name() != "cart.rbf" || entries[1].Name() != "manifest.json" {
			c.t.Fatalf("slot %d directory %v %v", path.Slot, entries, err)
		}
	}
	if _, err := os.Stat(payloadPath); err != nil {
		c.t.Fatalf("overlay missing: %v", err)
	}
}

func (c *slotTargetControl) LoadSlotComposedCore(ctx context.Context, path, id string, paths []misterruntime.SlotExpansionPath, payloadPath string, composition expansion.SlotComposition) (misterruntime.Protocol2Response, error) {
	c.slotCalls++
	c.checkSlots(paths, payloadPath)
	c.composition = composition
	response, err := c.packageControl.LoadCore(ctx, path, id)
	if err == nil {
		response.ActivePackage.SlotComposition = &composition
		c.status2 = &response
	}
	return response, err
}

func (c *slotTargetControl) LoadROMSlotComposedCore(ctx context.Context, path, id string, paths []misterruntime.SlotExpansionPath, payloadPath string, composition expansion.SlotComposition, programmedPath string, link corepackage.ROMLinkIdentity) (misterruntime.Protocol2Response, error) {
	c.romSlotCalls++
	c.checkSlots(paths, payloadPath)
	c.composition = composition
	programmed, err := os.ReadFile(programmedPath)
	if err != nil || link.ProgrammedSHA256 != fmt.Sprintf("%x", sha256.Sum256(programmed)) {
		c.t.Fatalf("programmed identity: %v", err)
	}
	response, err := c.packageControl.LoadCore(ctx, path, id)
	if err == nil {
		response.Capabilities.ROMLinking = 1
		response.ActivePackage.ROMLink = &link
		response.ActivePackage.SlotComposition = &composition
		c.status2 = &response
	}
	return response, err
}

func TestSlotTargetRelinksROMCardsAndDispatchesV2(t *testing.T) {
	archive, firmware, cards := apple2TargetFixture(t, true)
	transport, err := corepackage.PrepareROMInput(context.Background(), corepackage.ROMInput{Package: archive, ROM: firmware, SlotExpansions: cards})
	if err != nil {
		t.Fatal(err)
	}
	control := &slotTargetControl{romTargetControl: newROMTargetControl(t, 1)}
	root := t.TempDir()
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
	active, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data))
	if apiErr != nil || !attempted || control.romSlotCalls != 1 || control.linkedCalls != 0 || active.SlotComposition == nil || active.ROMLink == nil {
		t.Fatalf("activation=%#v attempted=%v err=%v calls=%d/%d", active, attempted, apiErr, control.romSlotCalls, control.linkedCalls)
	}
	if !reflect.DeepEqual(*active.SlotComposition, *transport.SlotComposition) || len(control.paths) != 2 || control.paths[0].Slot != 2 || control.paths[1].Slot != 4 {
		t.Fatalf("composition %+v paths %+v", active.SlotComposition, control.paths)
	}
	adopted, err := corepackage.Adopt(root)
	if err != nil || len(adopted) != 1 || adopted[0].SlotComposition == nil || adopted[0].SlotComposition.ID != transport.SlotComposition.ID {
		t.Fatalf("restart adoption lost the slot composition: %+v %v", adopted, err)
	}
	if _, apiErr := runtime.Stop(context.Background()); apiErr != nil {
		t.Fatal(apiErr)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("stop retained %v", entries)
	}
}

func TestSlotTargetStagesROMLessCardsThroughComposedLoad(t *testing.T) {
	archive, _, cards := apple2TargetFixture(t, false)
	bundle, err := corepackage.ComposeSlotArchive(context.Background(), archive, cards)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := bundle.Write()
	if err != nil {
		t.Fatal(err)
	}
	control := &slotTargetControl{romTargetControl: newROMTargetControl(t, 0)}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
	active, attempted, apiErr := runtime.LoadComposedCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport)), bytes.NewReader(transport), bundle.Composition.PackageID)
	if apiErr != nil || !attempted || control.slotCalls != 1 || active.SlotComposition == nil || active.SlotComposition.ID != bundle.Composition.ID || active.PersistenceMode != "volatile" {
		t.Fatalf("activation=%#v attempted=%v err=%v calls=%d", active, attempted, apiErr, control.slotCalls)
	}
	if _, apiErr := runtime.Stop(context.Background()); apiErr != nil {
		t.Fatal(apiErr)
	}
}
