package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/DeanoC/misteross/expansion"
	"os"
	"reflect"
	"strings"
	"testing"
)

func stVideoROMFixture(t *testing.T) ROMInput {
	t.Helper()
	in := linkedROMFixture(t)
	manifest, base, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	old, err := decode(manifest, base, mapping)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(manifest), old.Core.ID, "fes.atari-st"), old.ABI.ID, "fes.computer")
	text += "\n[[interfaces]]\nid = \"fes.expansion.atari-st-bus\"\nmajor = 1\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n"
	in.Package = romArchive([]byte(text), base, mapping)
	descriptor, err := decode([]byte(text), base, mapping)
	if err != nil {
		t.Fatal(err)
	}
	id := packageIdentity([]byte(text), base, mapping)
	for _, role := range []string{expansion.PartRoleVideo, expansion.PartRoleExpansion} {
		m := expansion.Manifest{CartSHA256: romDigest(base), CartSize: int64(len(base)), Device: expansion.Device, Format: 1, Map: expansion.AtariStVideoMap, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: descriptor.Build.ID, ShellPackageID: id, ShellSHA256: romDigest(base), Slot: expansion.VideoSlot, SlotMajor: 1}
		if role == expansion.PartRoleExpansion {
			m.Map, m.Slot, m.SlotIndex = expansion.AtariStMap, expansion.AtariStSlot, 1
		}
		asset, err := expansion.NewAsset(m, base)
		if err != nil {
			t.Fatal(err)
		}
		in.Parts = append(in.Parts, asset)
	}
	return in
}
func romArchive(manifest, base, mapping []byte) []byte {
	out := canonicalArchive(manifest, base)
	out = out[:len(out)-1024]
	out = append(out, canonicalHeader("rom-map.json", int64(len(mapping)))...)
	out = append(out, mapping...)
	return append(out, make([]byte, (512-len(mapping)%512)%512+1024)...)
}
func TestSTVideoROMStageAdoptAndRetainTwoIdentities(t *testing.T) {
	ctx := context.Background()
	in := stVideoROMFixture(t)
	prepared, err := PrepareROMInput(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.PartsComposition == nil || prepared.SlotComposition != nil || prepared.PartsComposition.Layout != expansion.AtariStVideoLayout {
		t.Fatal("missing ST overlay identity")
	}
	in.Parts[0], in.Parts[1] = in.Parts[1], in.Parts[0]
	reordered, err := PrepareROMInput(ctx, in)
	if err != nil || !bytes.Equal(prepared.Data, reordered.Data) {
		t.Fatal("parts order changed canonical ROM transport", err)
	}
	root := t.TempDir()
	staged, err := StageROMInput(ctx, root, int64(len(prepared.Data)), bytes.NewReader(prepared.Data))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if staged.ROMLink == nil || !reflect.DeepEqual(staged.PartsComposition, prepared.PartsComposition) || staged.SlotComposition != nil || len(staged.PartDirectories) != 2 {
		t.Fatal("missing separate ROM/parts receipts")
	}
	overlay, err := os.ReadFile(staged.PayloadPath)
	if err != nil {
		t.Fatal(err)
	}
	programmed, err := os.ReadFile(staged.ProgrammedPath)
	if err != nil {
		t.Fatal(err)
	}
	if romDigest(overlay) != staged.PartsComposition.PayloadSHA256 || romDigest(programmed) != staged.ROMLink.ProgrammedSHA256 || bytes.Equal(overlay, programmed) {
		t.Fatal("overlay and programmed identities collapsed")
	}
	_, base, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	m, err := expansion.ParseROMMap(ctx, mapping, romDigest(base), len(in.ROM))
	if err != nil {
		t.Fatal(err)
	}
	shell, err := PartsShell(Inspection{PackageID: staged.PackageID, Descriptor: staged.Descriptor}, base)
	if err != nil {
		t.Fatal(err)
	}
	final, wantOverlay, wantProgrammed, err := expansion.ComposePartsROM(ctx, shell, in.Parts, m, in.ROM)
	if err != nil || !bytes.Equal(overlay, wantOverlay) || !bytes.Equal(programmed, wantProgrammed) || final.PayloadSHA256 != staged.ROMLink.ProgrammedSHA256 {
		t.Fatal("staged bytes differ from shared linker", err)
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || !reflect.DeepEqual(adopted[0].PartsComposition, staged.PartsComposition) || !reflect.DeepEqual(adopted[0].ROMLink, staged.ROMLink) {
		t.Fatal("adoption lost or failed to recheck paired receipts", err)
	}
	if err := os.Chmod(staged.ProgrammedPath, 0600); err != nil {
		t.Fatal(err)
	}
	programmed[len(programmed)/2] ^= 1
	if err := os.WriteFile(staged.ProgrammedPath, programmed, 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(staged.ProgrammedPath, 0400); err != nil {
		t.Fatal(err)
	}
	if _, err := Adopt(root); err == nil {
		t.Fatal("accepted tampered ROM+parts payload")
	}
}
func TestSTVideoROMRejectsAmbiguousRolesAndFalseEvidence(t *testing.T) {
	in := stVideoROMFixture(t)
	for name, bad := range map[string]ROMInput{"cpu-only": {Package: in.Package, ROM: in.ROM, Parts: in.Parts[1:]}, "duplicate": {Package: in.Package, ROM: in.ROM, Parts: []expansion.Asset{in.Parts[0], in.Parts[0]}}, "mixed": {Package: in.Package, ROM: in.ROM, Parts: in.Parts, SlotExpansions: in.Parts[1:]}} {
		t.Run(name, func(t *testing.T) {
			if _, err := PrepareROMInput(context.Background(), bad); err == nil {
				t.Fatal("accepted ambiguous ROM parts")
			}
		})
	}
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	reader := &canonicalReader{data: data}
	receiptBytes, err := reader.read("rom-link.json", 4096)
	if err != nil {
		t.Fatal(err)
	}
	var receipt romInputReceipt
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.PartsComposition.ID = strings.Repeat("e", 64)
	falseEvidence, err := writeROMInputReceipt(in, receipt)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := StageROMInput(context.Background(), root, int64(len(falseEvidence)), bytes.NewReader(falseEvidence)); err == nil {
		t.Fatal("accepted forged sender overlay identity")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("failed admission leaked publications")
	}
	// A declared empty video socket also remains excluded from the ROM map.
	manifest, base, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(mapping, &doc); err != nil {
		t.Fatal(err)
	}
	blocks := doc["blocks"].([]any)
	words := blocks[0].(map[string]any)["word_bits"].([]any)
	words[0].([]any)[0] = float64(3442*7605 + 1769)
	mapping, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(manifest), fmt.Sprintf("sha256 = \"%s\"", romDigest(inMapping(t, in.Package))), fmt.Sprintf("sha256 = \"%s\"", romDigest(mapping)))
	// Map length is sealed separately from its digest.
	text = strings.ReplaceAll(text, fmt.Sprintf("size = %d", len(inMapping(t, in.Package))), fmt.Sprintf("size = %d", len(mapping)))
	empty := ROMInput{Package: romArchive([]byte(text), base, mapping), ROM: in.ROM}
	if _, err := linkROMInput(context.Background(), empty, romInputReceipt{}); err == nil {
		t.Fatal("ROM admitted a destination in an unselected ST video socket")
	}
}
func inMapping(t *testing.T, pkg []byte) []byte {
	t.Helper()
	_, _, m, err := readArchive(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
