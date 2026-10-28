package corepackage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

const (
	colecoBIOSBytes      = 8192
	colecoCartridgeBytes = 16384
	colecoROMBlocks      = (colecoBIOSBytes + colecoCartridgeBytes) / 1024
)

func colecoBlankShell(t *testing.T) []byte {
	t.Helper()
	packed, err := os.ReadFile("../../misteross/expansion/testdata/rom/blank.rbf.gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	base, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// colecoVideoROMShellDestinations returns blank lanes outside both video sockets.
func colecoVideoROMShellDestinations(t *testing.T, base []byte) []uint32 {
	t.Helper()
	dest, err := expansion.BlankROMDestinations(base, colecoROMBlocks*256*40, 124, 32, 3906, 3442)
	if err != nil {
		t.Fatal(err)
	}
	return dest
}

// colecoVideoROMInput is the format-4 Coleco video shell: the 8 KiB BIOS and
// 16 KiB cartridge through one map, with a video part for layout.
func colecoVideoROMInput(t *testing.T, base []byte, dest []uint32, layout string) ROMInputV2 {
	t.Helper()
	const words, lanes = 256, 40
	if len(dest) != colecoROMBlocks*words*lanes {
		t.Fatalf("destination count %d", len(dest))
	}
	blocks := make([]expansion.ROMBlock, colecoROMBlocks)
	for i := range blocks {
		wordBits := make([][]uint32, words)
		for w := range wordBits {
			start := (i*words + w) * lanes
			word := make([]uint32, lanes)
			copy(word, dest[start:start+lanes])
			wordBits[w] = word
		}
		blocks[i] = expansion.ROMBlock{BEL: fmt.Sprintf("fixture-%d", i), SourceOffset: i * 1024, WordBits: wordBits}
	}
	sum := sha256.Sum256(base)
	mapping, err := json.Marshal(expansion.ROMMap{
		Format: 1, Device: expansion.Device, Encoding: "m10k-1024x10-v1",
		BaseSHA256: hex.EncodeToString(sum[:]), SourceSize: colecoBIOSBytes + colecoCartridgeBytes, Blocks: blocks,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	slot, socket := expansion.VideoSlot, expansion.ColecoVideoMap
	if layout == expansion.ColecoNativeVideoLayout {
		slot, socket = expansion.NativeVideoSlot, expansion.ColecoNativeVideoMap
	}
	text := strings.Replace(string(manifest), "format = 2", "format = 4", 1)
	text = strings.ReplaceAll(text, "fes.simple-game", "fes.application")
	text = strings.ReplaceAll(text, "fes.pong", "fes.coleco")
	text = strings.ReplaceAll(text, "size = 12", fmt.Sprintf("size = %d", len(base)))
	text = strings.ReplaceAll(text, "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", hex.EncodeToString(sum[:]))
	text += fmt.Sprintf("\n[[roms]]\nid = \"coleco-bios\"\nrole = \"firmware\"\nsource_size = %d\nsource_offset = 0\n\n[[roms]]\nid = \"coleco-cart\"\nrole = \"cartridge\"\nsource_size = %d\nsource_offset = %d\n\n[rom_map]\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n",
		colecoBIOSBytes, colecoCartridgeBytes, colecoBIOSBytes, len(mapping), sha256.Sum256(mapping))
	text += fmt.Sprintf("\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = false\n", slot)
	descriptor, err := decode([]byte(text), base, mapping)
	if err != nil {
		t.Fatal(err)
	}
	id := packageIdentity([]byte(text), base, mapping)
	asset, err := expansion.NewAsset(expansion.Manifest{
		CartSHA256: hex.EncodeToString(sum[:]), CartSize: int64(len(base)), Device: expansion.Device,
		Format: 1, Map: socket, RecipeSHA256: strings.Repeat("b", 64),
		Revision: strings.Repeat("c", 40), ShellBuildID: descriptor.Build.ID, ShellPackageID: id,
		ShellSHA256: hex.EncodeToString(sum[:]), Slot: slot, SlotMajor: 1,
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	bios := bytes.Repeat([]byte{0x31}, colecoBIOSBytes)
	cart := bytes.Repeat([]byte{0x55, 0xaa}, colecoCartridgeBytes/2)
	return ROMInputV2{Package: canonicalArchiveWithROMMap([]byte(text), base, mapping), BIOS: bios, Cartridge: cart, Parts: []expansion.Asset{asset}}
}

func TestColecoFormat4VideoShellLinksBIOSCartridgeAndParts(t *testing.T) {
	base := colecoBlankShell(t)
	dest := colecoVideoROMShellDestinations(t, base)
	for _, layout := range []string{expansion.ColecoNativeVideoLayout, expansion.ColecoVideoLayout} {
		t.Run(layout, func(t *testing.T) {
			ctx := context.Background()
			in := colecoVideoROMInput(t, base, dest, layout)
			transport, err := PrepareROMInputV2(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if transport.PartsComposition == nil || transport.PartsComposition.Layout != layout || transport.SlotComposition != nil {
				t.Fatalf("parts layout: %+v", transport.PartsComposition)
			}
			if !IsROMInputV2(transport.Data) || IsROMInput(transport.Data) {
				t.Fatal("parts input is not a two-source transport")
			}
			prepared, err := readPreparedROMInputV2(transport.Data)
			if err != nil || len(prepared.input.Parts) != 1 || prepared.input.Parts[0].ID != in.Parts[0].ID || !bytes.Equal(prepared.input.BIOS, in.BIOS) || !bytes.Equal(prepared.input.Cartridge, in.Cartridge) {
				t.Fatalf("round trip: %v", err)
			}
			root := t.TempDir()
			staged, err := StageROMInputV2(ctx, root, int64(len(transport.Data)), bytes.NewReader(transport.Data))
			if err != nil {
				t.Fatal(err)
			}
			if staged.ROMLink != nil || staged.ROMLinks == nil || !staged.ROMLinks.ValidFor(staged.Descriptor) || !reflect.DeepEqual(staged.PartsComposition, transport.PartsComposition) || len(staged.PartDirectories) != 1 {
				t.Fatalf("staged identities: %+v", staged)
			}
			if staged.ROMLinks.Sources[0].ID != "coleco-bios" || staged.ROMLinks.Sources[0].SourceSHA256 != romDigest(in.BIOS) ||
				staged.ROMLinks.Sources[1].ID != "coleco-cart" || staged.ROMLinks.Sources[1].SourceSHA256 != romDigest(in.Cartridge) {
				t.Fatalf("source identities: %+v", staged.ROMLinks.Sources)
			}
			// The programmed bytes are the composed overlay with BIOS then cartridge linked.
			manifest, payload, mapping, err := readArchive(in.Package)
			if err != nil {
				t.Fatal(err)
			}
			d, m, err := decodeWithROMMap(manifest, payload, mapping)
			if err != nil {
				t.Fatal(err)
			}
			_, overlay, err := expansion.ComposePartsContext(ctx, mustPartsShell(t, Inspection{PackageID: staged.PackageID, Descriptor: d}, payload), in.Parts)
			if err != nil {
				t.Fatal(err)
			}
			want, err := expansion.LinkROM(ctx, overlay, rebasedROMMap(*m, overlay), append(bytes.Clone(in.BIOS), in.Cartridge...))
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(staged.ProgrammedPath)
			if err != nil || !bytes.Equal(got, want) || romDigest(got) != staged.ROMLinks.ProgrammedSHA256 {
				t.Fatalf("programmed bytes differ: %v", err)
			}
			if err := staged.Cleanup(); err != nil {
				t.Fatal(err)
			}

			staged, err = StageROMInputV2(ctx, root, int64(len(transport.Data)), bytes.NewReader(transport.Data))
			if err != nil {
				t.Fatal(err)
			}
			adopted, err := Adopt(root)
			if err != nil || len(adopted) != 1 || !reflect.DeepEqual(adopted[0].ROMLinks, staged.ROMLinks) || !reflect.DeepEqual(adopted[0].PartsComposition, staged.PartsComposition) || adopted[0].ProgrammedPath != staged.ProgrammedPath {
				t.Fatalf("adoption: %+v %v", adopted, err)
			}
			if err := adopted[0].Cleanup(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func mustPartsShell(t *testing.T, inspection Inspection, payload []byte) expansion.PartsShell {
	t.Helper()
	shell, err := PartsShell(inspection, payload)
	if err != nil {
		t.Fatal(err)
	}
	return shell
}

// rebasedROMMap binds the sealed map to the composed overlay so LinkROM can
// patch it; the lanes are unchanged.
func rebasedROMMap(m expansion.ROMMap, overlay []byte) expansion.ROMMap {
	sum := sha256.Sum256(overlay)
	m.BaseSHA256 = hex.EncodeToString(sum[:])
	return m
}

func TestColecoFormat4VideoShellRejectsBadInputs(t *testing.T) {
	base := colecoBlankShell(t)
	dest := colecoVideoROMShellDestinations(t, base)
	ctx := context.Background()
	layout := expansion.ColecoNativeVideoLayout

	blocked := append([]uint32(nil), dest...)
	blocked[0] = uint32(1800*7605 + 1769)
	if _, err := PrepareROMInputV2(ctx, colecoVideoROMInput(t, base, blocked, layout)); err == nil || !strings.Contains(err.Error(), "socket") {
		t.Fatalf("socket overlap accepted: %v", err)
	}
	// A ROM-only load of the same shell still keeps its lanes out of the sockets.
	overlap := colecoVideoROMInput(t, base, blocked, layout)
	overlap.Parts = nil
	data, err := PrepareROMInputV2(ctx, overlap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StageROMInputV2(ctx, t.TempDir(), int64(len(data.Data)), bytes.NewReader(data.Data)); err == nil || !strings.Contains(err.Error(), "socket") {
		t.Fatalf("socket overlap staged without parts: %v", err)
	}

	in := colecoVideoROMInput(t, base, dest, layout)
	both := in
	both.Expansion = &in.Parts[0]
	if _, err := PrepareROMInputV2(ctx, both); err == nil {
		t.Fatal("expansion and parts accepted together")
	}
	short := in
	short.Cartridge = in.Cartridge[:8192]
	if _, err := PrepareROMInputV2(ctx, short); err == nil {
		t.Fatal("8 KiB cartridge accepted for a 16 KiB source")
	}

	transport, err := PrepareROMInputV2(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	reader := &canonicalReader{data: transport.Data}
	receipt, err := reader.read("rom-link-v2.json", 4096)
	if err != nil {
		t.Fatal(err)
	}
	var sent romInputReceiptV2
	if err := json.Unmarshal(receipt, &sent); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*romInputReceiptV2){
		"missing evidence":  func(r *romInputReceiptV2) { r.PartsComposition, r.ProgrammedSHA256, r.ProgrammedSize = nil, "", 0 },
		"programmed digest": func(r *romInputReceiptV2) { r.ProgrammedSHA256 = strings.Repeat("0", 64) },
		"composition digest": func(r *romInputReceiptV2) {
			c := *r.PartsComposition
			c.PayloadSHA256 = strings.Repeat("0", 64)
			r.PartsComposition = &c
		},
		"missing selection": func(r *romInputReceiptV2) { r.Parts = nil },
	} {
		t.Run(name, func(t *testing.T) {
			bad := sent
			change(&bad)
			encoded, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}
			members := []canonicalMember{{"rom-link-v2.json", encoded}, {"package.tar", in.Package}, {"bios.bin", in.BIOS}, {"cartridge.bin", in.Cartridge}}
			parts, err := romPartMembers(in.Parts)
			if err != nil {
				t.Fatal(err)
			}
			forged, err := writeCanonicalMembers(append(members, parts...), MaxROMInputSize)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := StageROMInputV2(ctx, t.TempDir(), int64(len(forged)), bytes.NewReader(forged)); err == nil {
				t.Fatal("forged parts evidence staged")
			}
		})
	}
}

func TestColecoVideoShellRejectsFormat3Cartridge(t *testing.T) {
	base := colecoBlankShell(t)
	in := colecoVideoROMInput(t, base, colecoVideoROMShellDestinations(t, base), expansion.ColecoNativeVideoLayout)
	manifest, payload, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	d, err := decode(manifest, payload, mapping)
	if err != nil {
		t.Fatal(err)
	}
	inspection := Inspection{PackageID: packageIdentity(manifest, payload, mapping), Descriptor: d}
	if shell, err := PartsShell(inspection, payload); err != nil || shell.Layout != expansion.ColecoNativeVideoLayout {
		t.Fatalf("format-4 shell: %+v %v", shell, err)
	}
	for name, change := range map[string]func(*Descriptor){
		"format 3 cartridge": func(d *Descriptor) {
			d.Format, d.ROMs, d.ROMMap = 3, nil, nil
			d.ROM = &ROM{ID: "cartridge-rom", Role: "cartridge", SourceSize: 16384}
		},
		"megacart": func(d *Descriptor) {
			d.ROMs = append([]ROMRequirement(nil), d.ROMs...)
			d.ROMs[1].SourceSize = 131072
		},
		"short bios": func(d *Descriptor) {
			d.ROMs = append([]ROMRequirement(nil), d.ROMs...)
			d.ROMs[0].SourceSize = 4096
		},
		"other core": func(d *Descriptor) { d.Core.ID = "fes.sg1000" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := inspection
			change(&candidate.Descriptor)
			if _, err := PartsShell(candidate, payload); err == nil {
				t.Fatal("accepted a shell outside the format-4 Coleco video contract")
			}
		})
	}
}
