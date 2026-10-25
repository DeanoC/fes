package misterruntime_test

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

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/misteross/expansion"
)

// colecoVideoTargetInput is the format-4 Coleco native-video shell: the 8 KiB
// BIOS and 16 KiB cartridge through one map, with its Direct video part.
func colecoVideoTargetInput(t *testing.T) corepackage.ROMInputV2 {
	t.Helper()
	packed, err := os.ReadFile(filepath.Join("..", "..", "..", "misteross", "expansion", "testdata", "rom", "blank.rbf.gz"))
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
	const blocks, words, lanes = 24, 256, 40
	dest, err := expansion.BlankROMDestinations(base, blocks*words*lanes, 124, 32, 3906, 3442)
	if err != nil {
		t.Fatal(err)
	}
	m := expansion.ROMMap{Format: 1, Device: expansion.Device, Encoding: "m10k-1024x10-v1",
		BaseSHA256: fmt.Sprintf("%x", sha256.Sum256(base)), SourceSize: blocks * 1024}
	for i := 0; i < blocks; i++ {
		bits := make([][]uint32, words)
		for w := range bits {
			start := (i*words + w) * lanes
			bits[w] = append([]uint32(nil), dest[start:start+lanes]...)
		}
		m.Blocks = append(m.Blocks, expansion.ROMBlock{BEL: fmt.Sprintf("fixture-%d", i), SourceOffset: i * 1024, WordBits: bits})
	}
	mapping, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(fixture, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(fixture, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(manifest), "format = 2", "format = 4", 1)
	text = strings.ReplaceAll(text, "fes.simple-game", "fes.application")
	text = strings.ReplaceAll(text, "fes.pong", "fes.coleco")
	text = strings.ReplaceAll(text, fmt.Sprintf("%x", sha256.Sum256(old)), fmt.Sprintf("%x", sha256.Sum256(base)))
	text = strings.ReplaceAll(text, fmt.Sprintf("size = %d", len(old)), fmt.Sprintf("size = %d", len(base)))
	text += fmt.Sprintf("\n[[roms]]\nid = \"coleco-bios\"\nrole = \"firmware\"\nsource_size = 8192\nsource_offset = 0\n\n[[roms]]\nid = \"coleco-cart\"\nrole = \"cartridge\"\nsource_size = 16384\nsource_offset = 8192\n\n[rom_map]\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(mapping), sha256.Sum256(mapping))
	text += "\n[[interfaces]]\nid = \"fes.expansion.coleco-bus\"\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = \"fes.fabric.video.native-pixels\"\nmajor = 1\nminor = 0\nrequired = false\n"
	pkg := canonicalTar(t, [2][]byte{[]byte("manifest.toml"), []byte(text)}, [2][]byte{[]byte("core.rbf"), base}, [2][]byte{[]byte("rom-map.json"), mapping})
	staged, err := corepackage.Stage(context.Background(), t.TempDir(), int64(len(pkg)), bytes.NewReader(pkg))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	part, err := expansion.NewAsset(expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(base)), CartSize: int64(len(base)), Device: expansion.Device, Format: 1,
		Map: expansion.ColecoNativeVideoMap, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: staged.Descriptor.Build.ID,
		ShellPackageID: staged.PackageID, ShellSHA256: staged.Descriptor.Payload.SHA256, Slot: expansion.NativeVideoSlot, SlotMajor: 1}, base)
	if err != nil {
		t.Fatal(err)
	}
	return corepackage.ROMInputV2{Package: pkg, BIOS: bytes.Repeat([]byte{0x31}, 8192), Cartridge: bytes.Repeat([]byte{0x55}, 16384), Parts: []expansion.Asset{part}}
}

type colecoVideoTargetControl struct {
	*twoROMTargetControl
	partsCalls int
}

func (c *colecoVideoTargetControl) InspectPartsCore(ctx context.Context, path, id, payload string, parts []misterruntime.PartPath, receipt expansion.PartsComposition) (misterruntime.Protocol2Response, error) {
	return c.packageControl.InspectCore(ctx, path, id)
}
func (c *colecoVideoTargetControl) LoadPartsCore(context.Context, string, string, string, []misterruntime.PartPath, expansion.PartsComposition) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("Coleco BIOS and cartridge bypassed ROM dispatch")
	return misterruntime.Protocol2Response{}, nil
}
func (c *colecoVideoTargetControl) LoadROMLinksPartsComposedCore(ctx context.Context, path, id string, parts []misterruntime.PartPath, payload string, receipt expansion.PartsComposition, programmed string, links corepackage.ROMLinksIdentity) (misterruntime.Protocol2Response, error) {
	c.partsCalls++
	if len(parts) != 1 || parts[0].Role != "video" || receipt.Layout != expansion.ColecoNativeVideoLayout {
		c.t.Fatal("wrong Coleco native parts")
	}
	before, err := os.ReadFile(payload)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(before)) != receipt.PayloadSHA256 {
		c.t.Fatal("parts overlay identity lost", err)
	}
	after, err := os.ReadFile(programmed)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(after)) != links.ProgrammedSHA256 || bytes.Equal(before, after) {
		c.t.Fatal("ROM programmed identity lost", err)
	}
	response, err := c.packageControl.LoadCore(ctx, path, id)
	if err == nil {
		response.Capabilities.ROMLinking = 1
		response.ActivePackage.ROMLinks = &links
		response.ActivePackage.PartsComposition = &receipt
		c.status2 = &response
	}
	return response, err
}

func TestColecoVideoTargetLinksBIOSCartridgeAndPart(t *testing.T) {
	in := colecoVideoTargetInput(t)
	transport, err := corepackage.PrepareROMInputV2(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	control := &colecoVideoTargetControl{twoROMTargetControl: &twoROMTargetControl{romTargetControl: newROMTargetControl(t, 1)}}
	root := t.TempDir()
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
	active, attempted, failure := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data))
	if failure != nil || !attempted || control.partsCalls != 1 || control.calls != 0 || active.ROMLink != nil || active.ROMLinks == nil || !reflect.DeepEqual(active.PartsComposition, transport.PartsComposition) {
		t.Fatalf("activation=%+v attempted=%v failure=%v dispatch=%d/%d", active, attempted, failure, control.partsCalls, control.calls)
	}
	if active.ROMLinks.Sources[0].ID != "coleco-bios" || active.ROMLinks.Sources[0].SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(in.BIOS)) ||
		active.ROMLinks.Sources[1].ID != "coleco-cart" || active.ROMLinks.Sources[1].SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(in.Cartridge)) {
		t.Fatalf("source identities: %+v", active.ROMLinks.Sources)
	}
	adopted, err := corepackage.Adopt(root)
	if err != nil || len(adopted) != 1 || !reflect.DeepEqual(adopted[0].PartsComposition, active.PartsComposition) || !reflect.DeepEqual(adopted[0].ROMLinks, active.ROMLinks) {
		t.Fatal("restart lost paired identities", err)
	}
	if _, failure := runtime.Stop(context.Background()); failure != nil {
		t.Fatal(failure)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("retirement retained Coleco ROM/parts sources", entries)
	}
}

// A daemon client without the paired operation refuses the load before any
// target mutation.
func TestColecoVideoTargetRequiresPairedOperation(t *testing.T) {
	in := colecoVideoTargetInput(t)
	transport, err := corepackage.PrepareROMInputV2(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	control := &twoROMTargetControl{romTargetControl: newROMTargetControl(t, 1)}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
	_, attempted, failure := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data))
	if failure == nil || attempted || control.calls != 0 || control.loadCalls != 0 {
		t.Fatalf("attempted=%v failure=%v calls=%d/%d", attempted, failure, control.calls, control.loadCalls)
	}
}
