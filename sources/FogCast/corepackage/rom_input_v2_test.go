package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func linkedTwoROMFixture(t *testing.T) ROMInputV2 {
	t.Helper()
	old := linkedROMFixture(t)
	manifest, base, mapping, err := readArchive(old.Package)
	if err != nil {
		t.Fatal(err)
	}
	cut := strings.Index(string(manifest), "\n[rom]")
	if cut < 0 {
		t.Fatal("no ROM declaration")
	}
	manifest = []byte(strings.Replace(string(manifest[:cut]), "format = 3", "format = 4", 1) +
		fmt.Sprintf("\n[[roms]]\nid = \"coleco-bios\"\nrole = \"firmware\"\nsource_size = 1024\nsource_offset = 0\n\n[[roms]]\nid = \"coleco-cart\"\nrole = \"cartridge\"\nsource_size = %d\nsource_offset = 1024\n\n[rom_map]\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(old.ROM)-1024, len(mapping), sha256.Sum256(mapping)))
	archive := canonicalArchive(manifest, base)
	archive = archive[:len(archive)-1024]
	archive = append(archive, canonicalHeader("rom-map.json", int64(len(mapping)))...)
	archive = append(archive, mapping...)
	archive = append(archive, make([]byte, (512-len(mapping)%512)%512+1024)...)
	return ROMInputV2{Package: archive, BIOS: bytes.Clone(old.ROM[:1024]), Cartridge: bytes.Clone(old.ROM[1024:])}
}

func TestROMInputV2StageTwoSourceLink(t *testing.T) {
	in := linkedTwoROMFixture(t)
	data, err := WriteROMInputV2(in)
	if err != nil {
		t.Fatal(err)
	}
	if !IsROMInputV2(data) {
		t.Fatal("v2 transport not recognized")
	}
	root := t.TempDir()
	staged, err := StageROMInputV2(context.Background(), root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if staged.ROMLinks == nil || !staged.ROMLinks.ValidFor(staged.Descriptor) {
		t.Fatal("two-source identity missing")
	}
	if staged.ROMLinks.Sources[0].SourceSHA256 != romDigest(in.BIOS) || staged.ROMLinks.Sources[1].SourceSHA256 != romDigest(in.Cartridge) {
		t.Fatal("source digests lost")
	}
	manifest, base, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	d, err := decode(manifest, base, mapping)
	if err != nil {
		t.Fatal(err)
	}
	m, err := expansion.ParseROMMap(context.Background(), mapping, d.Payload.SHA256, len(in.BIOS)+len(in.Cartridge))
	if err != nil {
		t.Fatal(err)
	}
	want, err := expansion.LinkROM(context.Background(), base, m, append(bytes.Clone(in.BIOS), in.Cartridge...))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(staged.ProgrammedPath)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("linked output mismatch: %v", err)
	}
}

func TestROMInputV2AdoptionRechecksBothSources(t *testing.T) {
	in := linkedTwoROMFixture(t)
	data, err := WriteROMInputV2(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged, err := StageROMInputV2(context.Background(), root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || adopted[0].ROMLinks == nil {
		t.Fatalf("two-source adoption failed: %v", err)
	}
	if adopted[0].ROMLinks.Sources[0].SourceSHA256 != romDigest(in.BIOS) || adopted[0].ROMLinks.Sources[1].SourceSHA256 != romDigest(in.Cartridge) {
		t.Fatal("adoption lost source identities")
	}
	retained := filepath.Join(filepath.Dir(staged.ProgrammedPath), "input.tar")
	if err := os.Chmod(retained, 0600); err != nil {
		t.Fatal(err)
	}
	changed := bytes.Clone(data)
	changed[bytes.Index(changed, in.BIOS)] ^= 1
	if err := os.WriteFile(retained, changed, 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(retained, 0400); err != nil {
		t.Fatal(err)
	}
	if _, err := Adopt(root); err == nil {
		t.Fatal("changed retained BIOS was adopted")
	}
}

func TestStageROMInputV2RejectsIncompleteSources(t *testing.T) {
	in := linkedTwoROMFixture(t)
	in.BIOS = in.BIOS[:len(in.BIOS)-1]
	if _, err := WriteROMInputV2(in); err == nil {
		t.Fatal("short BIOS accepted")
	}
	in = linkedTwoROMFixture(t)
	data, err := WriteROMInputV2(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := StageROMInputV2(context.Background(), root, int64(len(data)), bytes.NewReader(data[:len(data)-1])); err == nil {
		t.Fatal("truncated cartridge accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed stage leaked: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := StageROMInputV2(ctx, root, int64(len(data)), bytes.NewReader(data)); err == nil {
		t.Fatal("canceled stage accepted")
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("canceled stage leaked")
	}
}

func TestROMInputV2ClosedTransport(t *testing.T) {
	in := linkedTwoROMFixture(t)
	data, err := WriteROMInputV2(in)
	if err != nil {
		t.Fatal(err)
	}
	_, offset, err := romInitMember(data, 0, "rom-link-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	_, offset, err = romInitMember(data, offset, "package.tar")
	if err != nil {
		t.Fatal(err)
	}
	bios, cartHeader, err := romInitMember(data, offset, "bios.bin")
	if err != nil {
		t.Fatal(err)
	}
	badBIOS := bytes.Clone(data)
	badBIOS[cartHeader-len(bios)] ^= 1
	badHeader := bytes.Clone(data)
	copy(badHeader[offset:offset+8], []byte("cart.bin"))
	extra := bytes.Clone(data[:len(data)-1024])
	extra = append(extra, canonicalHeader("extra.bin", 1)...)
	extra = append(extra, make([]byte, 512+1024)...)
	for name, bad := range map[string][]byte{
		"changed BIOS after receipt": badBIOS,
		"swapped source header":      badHeader,
		"extra archive member":       extra,
		"trailing bytes":             append(bytes.Clone(data), 0),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if _, err := StageROMInputV2(context.Background(), root, int64(len(bad)), bytes.NewReader(bad)); err == nil {
				t.Fatal("malformed input accepted")
			}
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Fatal("failed stage leaked")
			}
		})
	}
}

func TestROMInputV2RevalidatesAfterSuccessfulStage(t *testing.T) {
	in := linkedTwoROMFixture(t)
	data, err := WriteROMInputV2(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged, err := StageROMInputV2(context.Background(), root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := staged.Cleanup(); err != nil {
		t.Fatal(err)
	}
	_, offset, err := romInitMember(data, 0, "rom-link-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	_, offset, err = romInitMember(data, offset, "package.tar")
	if err != nil {
		t.Fatal(err)
	}
	data[offset+512] ^= 1 // Same package and receipt, changed private BIOS.
	if _, err := StageROMInputV2(context.Background(), root, int64(len(data)), bytes.NewReader(data)); err == nil {
		t.Fatal("successful prior stage bypassed source validation")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("rejected stage left a publication", entries, err)
	}
}

func TestROMInputV2OptionalExactShellExpansion(t *testing.T) {
	in := linkedTwoROMFixture(t)
	manifest, base, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	manifest = []byte(strings.ReplaceAll(string(manifest), "fes.simple-game", "fes.simple-computer") +
		"\n[[interfaces]]\nid = \"fes.expansion.zx81-bus\"\nmajor = 1\nminor = 0\nrequired = false\n")
	in.Package = canonicalArchive(manifest, base)
	in.Package = in.Package[:len(in.Package)-1024]
	in.Package = append(in.Package, canonicalHeader("rom-map.json", int64(len(mapping)))...)
	in.Package = append(in.Package, mapping...)
	in.Package = append(in.Package, make([]byte, (512-len(mapping)%512)%512+1024)...)
	d, err := decode(manifest, base, mapping)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: romDigest(base), CartSize: int64(len(base)), Device: expansion.Device, Format: 1, Map: expansion.Map, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: d.Build.ID, ShellPackageID: packageIdentity(manifest, base, mapping), ShellSHA256: romDigest(base), Slot: expansion.Slot, SlotMajor: 1}, base)
	if err != nil {
		t.Fatal(err)
	}
	in.Expansion = &asset
	data, err := WriteROMInputV2(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged, err := StageROMInputV2(context.Background(), root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if staged.Composition == nil || staged.ROMLinks == nil || staged.ExpansionDirectory == "" {
		t.Fatal("expansion identity missing")
	}
	m, err := expansion.ParseROMMap(context.Background(), mapping, d.Payload.SHA256, len(in.BIOS)+len(in.Cartridge))
	if err != nil {
		t.Fatal(err)
	}
	shell, err := compositionShell(Inspection{PackageID: packageIdentity(manifest, base, mapping), Descriptor: d}, base)
	if err != nil {
		t.Fatal(err)
	}
	_, _, want, err := expansion.ComposeROM(context.Background(), shell, asset, m, append(bytes.Clone(in.BIOS), in.Cartridge...))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(staged.ProgrammedPath)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("composed output mismatch: %v", err)
	}
	bad := asset
	bad.Manifest.ShellPackageID = strings.Repeat("0", 64)
	bad, err = expansion.NewAsset(bad.Manifest, bad.Cart)
	if err != nil {
		t.Fatal(err)
	}
	in.Expansion = &bad
	if _, err = WriteROMInputV2(in); err == nil {
		t.Fatal("wrong-shell expansion accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected retained exact-shell publication: %v", err)
	}
}
