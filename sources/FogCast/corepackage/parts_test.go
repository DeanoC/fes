package corepackage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func developerPartsFixture(t *testing.T) ([]byte, []expansion.Asset) {
	t.Helper()
	packed, err := os.ReadFile("testdata/expansion-shell.rbf.gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(manifest), "fes.simple-game", "fes.application")
	text = strings.ReplaceAll(text, "fes.pong", "fes.coleco")
	text = strings.ReplaceAll(text, "size = 12", fmt.Sprintf("size = %d", len(payload)))
	sha := fmt.Sprintf("%x", sha256.Sum256(payload))
	text = strings.ReplaceAll(text, "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", sha)
	text += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n"
	descriptor, err := decode([]byte(text), payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := packageIdentity([]byte(text), payload, nil)
	var parts []expansion.Asset
	for _, video := range []bool{true, false} {
		slot, mapping, major := expansion.ColecoSlot, expansion.ColecoMapV2, 2
		if video {
			slot, mapping, major = expansion.VideoSlot, expansion.ColecoVideoMap, 1
		}
		asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: sha, CartSize: int64(len(payload)), Device: expansion.Device, Format: 1, Map: mapping, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: descriptor.Build.ID, ShellPackageID: id, ShellSHA256: sha, Slot: slot, SlotMajor: major}, payload)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, asset)
	}
	return canonicalArchive([]byte(text), payload), parts
}

func TestDeveloperPartsIndependentlyStageAdoptAndClean(t *testing.T) {
	ctx := context.Background()
	pkg, parts := developerPartsFixture(t)
	bundle, err := ComposePartsArchive(ctx, pkg, parts)
	if err != nil {
		t.Fatal(err)
	}
	data, err := bundle.Write(ctx)
	if err != nil {
		t.Fatal(err)
	}
	read, err := ReadPartsBundle(ctx, data)
	if err != nil || !reflect.DeepEqual(read.Composition, bundle.Composition) {
		t.Fatalf("roundtrip: %v", err)
	}
	root := t.TempDir()
	staged, err := StageParts(ctx, root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if staged.PartsComposition == nil || len(staged.PartDirectories) != 2 || staged.Composition != nil || staged.ROMLink != nil {
		t.Fatal("parts identities mixed with CPU or ROM composition")
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || !reflect.DeepEqual(adopted[0].PartsComposition, staged.PartsComposition) {
		t.Fatalf("adoption: %v", err)
	}
	if err := adopted[0].Cleanup(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("parts companions leaked")
	}
	changed := bytes.Clone(data)
	changed[len(changed)-1025] ^= 1
	if _, err := StageParts(ctx, root, int64(len(changed)), bytes.NewReader(changed)); err == nil {
		t.Fatal("target trusted changed linked bytes")
	}
	entries, _ = os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("rejection changed publications")
	}
	if _, err := StageComposition(ctx, root, int64(len(data)), bytes.NewReader(data)); err == nil {
		t.Fatal("library composition accepted developer parts")
	}
}

func TestDeveloperPartsRequireDeclaredShellAndRejectChangedEvidence(t *testing.T) {
	ctx := context.Background()
	pkg, parts := developerPartsFixture(t)
	manifest, payload, _, _ := readArchive(pkg)
	legacy := bytes.Replace(manifest, []byte("fes.fabric.video.raster-rgb888"), []byte("unknown.fabric.marker"), 1)
	if _, err := ComposePartsArchive(ctx, canonicalArchive(legacy, payload), parts); err == nil {
		t.Fatal("unmarked shell accepted")
	}
	bundle, err := ComposePartsArchive(ctx, pkg, parts[:1])
	if err != nil {
		t.Fatal(err)
	}
	bundle.Payload = bytes.Clone(bundle.Payload)
	bundle.Payload[0] ^= 1
	if _, err := bundle.Write(ctx); err == nil {
		t.Fatal("changed programmed evidence accepted")
	}
	if _, err := ComposeArchive(pkg, parts[0]); err == nil {
		t.Fatal("video accepted through CPU library composition")
	}
}
