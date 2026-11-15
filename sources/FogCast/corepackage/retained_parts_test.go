package corepackage

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func retainedPartsFixture(t *testing.T) (ROMInputV2, []PartInput) {
	t.Helper()
	base := colecoBlankShell(t)
	in := colecoVideoROMInput(t, base, colecoVideoROMShellDestinations(t, base), expansion.ColecoVideoLayout)
	manifest := in.Parts[0].Manifest
	manifest.Slot, manifest.Map, manifest.SlotMajor = expansion.ColecoSlot, expansion.ColecoMapV2, 2
	cpu, err := expansion.NewAsset(manifest, base)
	if err != nil {
		t.Fatal(err)
	}
	in.Parts = append(in.Parts, cpu)
	inputs := []PartInput{}
	for _, asset := range in.Parts {
		var raw bytes.Buffer
		if err = asset.Write(&raw); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, PartInput{ID: asset.ID, Archive: raw.Bytes()})
	}
	return in, inputs
}
func TestRetainedPartsMatchIndependentStagingAndOwnInputs(t *testing.T) {
	in, parts := retainedPartsFixture(t)
	ctx := context.Background()
	archive := retained(t, in.Package)
	sources := []ROMSource{{ID: "coleco-bios", Role: "firmware", Data: in.BIOS}, {ID: "coleco-cart", Role: "cartridge", Data: in.Cartridge}}
	linked, err := archive.LinkROMs(ctx, sources, parts...)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := PrepareROMInputV2(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := StageROMInputV2(ctx, t.TempDir(), transport.Size(), transport.Reader())
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	programmed, err := os.ReadFile(staged.ProgrammedPath)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := os.ReadFile(staged.PayloadPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(linked.Programmed())
	if err != nil || !bytes.Equal(got, programmed) {
		t.Fatal("retained programmed bytes differ from normal staging", err)
	}
	got, err = io.ReadAll(linked.Overlay())
	if err != nil || !bytes.Equal(got, overlay) {
		t.Fatal("retained overlay differs from normal staging", err)
	}
	receipt := linked.Receipt()
	if receipt.PartsComposition == nil || !reflect.DeepEqual(receipt.PartsComposition, staged.PartsComposition) || !reflect.DeepEqual(receipt.ROMLinks, staged.ROMLinks) {
		t.Fatal("retained receipt differs from normal staging")
	}
	originalID := linked.ReceiptSHA256()
	original := bytes.Clone(parts[0].Archive)
	receipt.PartsComposition.Parts[0].PartID = "mutated"
	parts[0].Archive[0] ^= 1
	got[0] ^= 1
	if linked.ReceiptSHA256() != originalID || linked.Receipt().PartsComposition.Parts[0].PartID == "mutated" {
		t.Fatal("receipt aliases escaped")
	}
	reader, err := linked.Part(parts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(read, original) {
		t.Fatal("part archive changed after input mutation", err)
	}
	if _, err = linked.Part(strings.Repeat("0", 64)); err == nil {
		t.Fatal("unknown part returned")
	}
	reversed := []PartInput{parts[1], {ID: parts[0].ID, Archive: original}}
	reordered, err := archive.LinkROMs(ctx, sources, reversed...)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.ReceiptSHA256() != originalID {
		t.Fatal("caller order changed canonical part selection")
	}
	changed := in.Parts[0].Manifest
	changed.RecipeSHA256 = strings.Repeat("f", 64)
	asset, err := expansion.NewAsset(changed, in.Parts[0].Cart)
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	if err = asset.Write(&raw); err != nil {
		t.Fatal(err)
	}
	alternate, err := archive.LinkROMs(ctx, sources, PartInput{ID: asset.ID, Archive: raw.Bytes()}, parts[1])
	if err != nil {
		t.Fatal(err)
	}
	alteredBytes, err := io.ReadAll(alternate.Programmed())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(alteredBytes, programmed) || alternate.ReceiptSHA256() == originalID {
		t.Fatal("changed part identity was collapsed into identical programmed bytes")
	}

}
func TestRetainedPartsRejectBeforePublishing(t *testing.T) {
	in, parts := retainedPartsFixture(t)
	ctx := context.Background()
	archive := retained(t, in.Package)
	sources := []ROMSource{{ID: "coleco-bios", Role: "firmware", Data: in.BIOS}, {ID: "coleco-cart", Role: "cartridge", Data: in.Cartridge}}
	for _, invalid := range [][]PartInput{nil, parts[1:], {parts[0], parts[0]}, {{ID: strings.Repeat("f", 64), Archive: parts[0].Archive}}, {{ID: parts[0].ID, Archive: []byte("broken")}}, {parts[0], parts[1], parts[0]}} {
		if linked, err := archive.LinkROMs(ctx, sources, invalid...); err == nil || linked != nil {
			t.Fatal("invalid selection published")
		}
	}
	manifest := in.Parts[0].Manifest
	manifest.ShellPackageID = strings.Repeat("e", 64)
	other, err := expansion.NewAsset(manifest, in.Parts[0].Cart)
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	if err = other.Write(&raw); err != nil {
		t.Fatal(err)
	}
	if linked, err := archive.LinkROMs(ctx, sources, PartInput{ID: other.ID, Archive: raw.Bytes()}); err == nil || linked != nil {
		t.Fatal("incompatible shell part published")
	}
}
