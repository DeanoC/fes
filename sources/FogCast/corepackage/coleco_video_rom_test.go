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
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func colecoRasterROMInput(t *testing.T, base []byte, dest []uint32) ROMInput {
	t.Helper()
	const words, lanes = 256, 40
	if len(dest) != 16*words*lanes {
		t.Fatalf("destination count %d", len(dest))
	}
	blocks := make([]expansion.ROMBlock, 16)
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
		BaseSHA256: hex.EncodeToString(sum[:]), SourceSize: 16384, Blocks: blocks,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(manifest), "format = 2", "format = 3", 1)
	text = strings.ReplaceAll(text, "fes.simple-game", "fes.application")
	text = strings.ReplaceAll(text, "fes.pong", "fes.coleco")
	text = strings.ReplaceAll(text, "size = 12", fmt.Sprintf("size = %d", len(base)))
	text = strings.ReplaceAll(text, "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", hex.EncodeToString(sum[:]))
	text += fmt.Sprintf("\n[rom]\nid = \"cartridge-rom\"\nrole = \"cartridge\"\nsource_size = 16384\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(mapping), sha256.Sum256(mapping))
	text += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n"
	descriptor, err := decode([]byte(text), base, mapping)
	if err != nil {
		t.Fatal(err)
	}
	id := packageIdentity([]byte(text), base, mapping)
	asset, err := expansion.NewAsset(expansion.Manifest{
		CartSHA256: hex.EncodeToString(sum[:]), CartSize: int64(len(base)), Device: expansion.Device,
		Format: 1, Map: expansion.ColecoVideoMap, RecipeSHA256: strings.Repeat("b", 64),
		Revision: strings.Repeat("c", 40), ShellBuildID: descriptor.Build.ID, ShellPackageID: id,
		ShellSHA256: hex.EncodeToString(sum[:]), Slot: expansion.VideoSlot, SlotMajor: 1,
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	return ROMInput{Package: canonicalArchiveWithROMMap([]byte(text), base, mapping), ROM: make([]byte, 16384), Parts: []expansion.Asset{asset}}
}

func TestColecoRasterFormat3CartridgeAdmitsROMParts(t *testing.T) {
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
	dest, err := expansion.BlankROMDestinations(base, 16*256*40, 1769, 32, 2806, 3442)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := colecoRasterROMInput(t, base, dest)
	prepared, err := PrepareROMInput(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.PartsComposition == nil || prepared.PartsComposition.Layout != expansion.ColecoVideoLayout {
		t.Fatalf("raster cartridge layout: %+v", prepared.PartsComposition)
	}
	root := t.TempDir()
	staged, err := StageROMInput(ctx, root, int64(len(prepared.Data)), bytes.NewReader(prepared.Data))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if staged.ROMLink == nil || staged.PartsComposition == nil || staged.PartsComposition.Layout != expansion.ColecoVideoLayout {
		t.Fatal("staged raster cartridge lost the ROM or parts identity")
	}

	blocked := append([]uint32(nil), dest...)
	blocked[0] = uint32(1800*7605 + 1769)
	overlap := colecoRasterROMInput(t, base, blocked)
	if _, err = PrepareROMInput(ctx, overlap); err == nil || !strings.Contains(err.Error(), "socket") {
		t.Fatalf("socket overlap accepted: %v", err)
	}
}
