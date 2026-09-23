package corepackage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DeanoC/misteross/expansion"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linkedROMFixture(t *testing.T) ROMInput {
	t.Helper()
	read := func(name string) []byte {
		f, e := os.Open(filepath.Join("../../misteross/expansion/testdata/rom", name+".gz"))
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		r, e := gzip.NewReader(f)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Close()
		b, e := io.ReadAll(r)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	base, mapping, rom := read("blank.rbf"), read("map.json"), read("ramp.rom")
	manifest, old := fixtureBytes(t, loadCases(t)[0])
	manifest = []byte(strings.ReplaceAll(strings.ReplaceAll(strings.Replace(string(manifest), "format = 2", "format = 3", 1), fmt.Sprintf("%x", sha256.Sum256(old)), fmt.Sprintf("%x", sha256.Sum256(base))), fmt.Sprintf("size = %d", len(old)), fmt.Sprintf("size = %d", len(base))))
	manifest = append(manifest, []byte(fmt.Sprintf("\n[rom]\nid = \"machine\"\nrole = \"firmware\"\nsource_size = %d\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(rom), len(mapping), sha256.Sum256(mapping)))...)
	archive := canonicalArchive(manifest, base)
	archive = archive[:len(archive)-1024]
	archive = append(archive, canonicalHeader("rom-map.json", int64(len(mapping)))...)
	archive = append(archive, mapping...)
	archive = append(archive, make([]byte, (512-len(mapping)%512)%512+1024)...)
	return ROMInput{Package: archive, ROM: rom}
}

func TestROMInputStageAndAdoptRelinks(t *testing.T) {
	in := linkedROMFixture(t)
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if !IsROMInput(data) {
		t.Fatal("not recognized")
	}
	root := t.TempDir()
	staged, err := StageROMInput(context.Background(), root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if staged.ROMLink == nil || !staged.ROMLink.ValidFor(staged.Descriptor) || staged.ProgrammedPath == "" {
		t.Fatal("missing identity")
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 {
		t.Fatalf("adopt: %v %v", adopted, err)
	}
	if *adopted[0].ROMLink != *staged.ROMLink {
		t.Fatal("identity lost")
	}
	body, err := os.ReadFile(staged.ProgrammedPath)
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)/2] ^= 1
	if err = os.Chmod(staged.ProgrammedPath, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(staged.ProgrammedPath, body, 0400); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(staged.ProgrammedPath, 0400); err != nil {
		t.Fatal(err)
	}
	if _, err = Adopt(root); err == nil {
		t.Fatal("tampered programmed bytes adopted")
	}
	if err = adopted[0].Cleanup(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("cleanup leaked")
	}
}
func TestROMInputRejectsAndCancels(t *testing.T) {
	in := linkedROMFixture(t)
	in.ROM = in.ROM[:len(in.ROM)-1]
	if _, err := WriteROMInput(in); err == nil {
		t.Fatal("short ROM accepted")
	}
	in = linkedROMFixture(t)
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := StageROMInput(ctx, root, int64(len(data)), bytes.NewReader(data)); err == nil {
		t.Fatal("cancel ignored")
	}
	data[len(data)-1] = 1
	if _, err := StageROMInput(context.Background(), root, int64(len(data)), bytes.NewReader(data)); err == nil {
		t.Fatal("trailing bytes accepted")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("failed staging leaked")
	}
}

func TestROMInputOptionalExpansion(t *testing.T) {
	in := linkedROMFixture(t)
	manifest, base, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	manifest = []byte(strings.ReplaceAll(string(manifest), "fes.simple-game", "fes.simple-computer") + "\n[[interfaces]]\nid = \"fes.expansion.zx81-bus\"\nmajor = 1\nminor = 0\nrequired = false\n")
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
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged, err := StageROMInput(context.Background(), root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if staged.Composition == nil || staged.ExpansionDirectory == "" || staged.PayloadPath == staged.ProgrammedPath {
		t.Fatal("missing separate composition")
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 {
		t.Fatalf("adopt expansion: %v", err)
	}
	if *adopted[0].Composition != *staged.Composition || *adopted[0].ROMLink != *staged.ROMLink {
		t.Fatal("adopt lost identity")
	}
	if err = adopted[0].Cleanup(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("expansion cleanup leaked")
	}
	bad := asset
	bad.Manifest.ShellPackageID = strings.Repeat("0", 64)
	bad, err = expansion.NewAsset(bad.Manifest, bad.Cart)
	if err != nil {
		t.Fatal(err)
	}
	in.Expansion = &bad
	if _, err = WriteROMInput(in); err == nil {
		t.Fatal("unbound expansion accepted")
	}
}

func TestROMInputDistinctSourceIdentity(t *testing.T) {
	in := linkedROMFixture(t)
	root := t.TempDir()
	var staged []Staged
	for i := 0; i < 2; i++ {
		in.ROM[0] = byte(i)
		data, err := WriteROMInput(in)
		if err != nil {
			t.Fatal(err)
		}
		s, err := StageROMInput(context.Background(), root, int64(len(data)), bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		staged = append(staged, s)
	}
	if staged[0].PackageID != staged[1].PackageID || *staged[0].ROMLink == *staged[1].ROMLink || staged[0].ROMLink.ProgrammedSHA256 == staged[1].ROMLink.ProgrammedSHA256 {
		t.Fatal("ROM launch identity does not distinguish source bytes")
	}
	for _, s := range staged {
		defer s.Cleanup()
	}
}

func TestROMInputClosedTransport(t *testing.T) {
	in := linkedROMFixture(t)
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	receipt, _, err := romInitMember(data, 0, "rom-link.json")
	if err != nil {
		t.Fatal(err)
	}
	rebuild := func(raw []byte, extraName string) []byte {
		out := append([]byte{}, canonicalHeader("rom-link.json", int64(len(raw)))...)
		out = append(out, raw...)
		out = append(out, make([]byte, (512-len(raw)%512)%512)...)
		offset := 512 + ((len(receipt) + 511) &^ 511)
		out = append(out, data[offset:len(data)-1024]...)
		if extraName != "" {
			out = append(out, canonicalHeader(extraName, 1)...)
			out = append(out, make([]byte, 512)...)
		}
		return append(out, make([]byte, 1024)...)
	}
	cases := map[string][]byte{
		"duplicate receipt key":     rebuild(append([]byte(`{"format":1,`), receipt[1:]...), ""),
		"unknown receipt key":       rebuild(append([]byte(`{"extra":1,`), receipt[1:]...), ""),
		"noncanonical receipt":      rebuild(append(receipt, ' '), ""),
		"external programmed bytes": rebuild(receipt, "programmed.rbf"),
		"external map":              rebuild(receipt, "rom-map.json"),
		"duplicate ROM":             rebuild(receipt, "rom.bin"),
		"truncated":                 data[:len(data)-1],
		"extra zeros":               append(bytes.Clone(data), make([]byte, 512)...),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if _, err := StageROMInput(context.Background(), root, int64(len(bad)), bytes.NewReader(bad)); err == nil {
				t.Fatal("accepted malformed transport")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("failure mutated staging")
			}
		})
	}
	// The manifest and map still admit structurally; framing must reject before staging.
	manifest, base, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	oldBaseDigest, oldMapSize := romDigest(base), len(mapping)
	base[len(base)/2] ^= 1
	manifest = []byte(strings.ReplaceAll(string(manifest), oldBaseDigest, romDigest(base)))
	var m expansion.ROMMap
	if err = json.Unmarshal(mapping, &m); err != nil {
		t.Fatal(err)
	}
	oldMap := romDigest(mapping)
	m.BaseSHA256 = romDigest(base)
	mapping, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	manifest = []byte(strings.ReplaceAll(string(manifest), oldMap, romDigest(mapping)))
	manifest = []byte(strings.ReplaceAll(string(manifest), fmt.Sprintf("size = %d", oldMapSize), fmt.Sprintf("size = %d", len(mapping))))
	in.Package = canonicalArchive(manifest, base)
	in.Package = in.Package[:len(in.Package)-1024]
	in.Package = append(in.Package, canonicalHeader("rom-map.json", int64(len(mapping)))...)
	in.Package = append(in.Package, mapping...)
	in.Package = append(in.Package, make([]byte, (512-len(mapping)%512)%512+1024)...)

	corrupted, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err = StageROMInput(context.Background(), root, int64(len(corrupted)), bytes.NewReader(corrupted)); err == nil {
		t.Fatal("malformed RBF accepted")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("invalid RBF published")
	}
}

type romPublicationCancelContext struct {
	context.Context
	cancel context.CancelFunc
	root   string
}

func (c romPublicationCancelContext) Err() error {
	entries, _ := os.ReadDir(c.root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "rom-link-") {
			c.cancel()
		}
	}
	return c.Context.Err()
}
func TestROMInputCancellationCleansPublishedCompanions(t *testing.T) {
	in := linkedROMFixture(t)
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = StageROMInput(romPublicationCancelContext{Context: ctx, cancel: cancel, root: root}, root, int64(len(data)), bytes.NewReader(data))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation: %v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("cancelled publication leaked")
	}
}

func TestROMLinkIdentityRejectsInvalidMetadata(t *testing.T) {
	in := linkedROMFixture(t)
	inspection, _, _, receipt, err := inspectROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	good := ROMLinkIdentity{ROMID: receipt.ROMID, MapSHA256: receipt.MapSHA256, SourceSHA256: receipt.SourceSHA256, SourceSize: receipt.SourceSize, ProgrammedSHA256: strings.Repeat("a", 64), ProgrammedSize: 100}
	if !good.ValidFor(inspection.Descriptor) {
		t.Fatal("valid identity rejected")
	}
	d := inspection.Descriptor
	r := *d.ROM
	r.ID = ""
	d.ROM = &r
	bad := good
	bad.ROMID = ""
	if bad.ValidFor(d) {
		t.Fatal("empty ROM ID accepted")
	}
	for name, alter := range map[string]func(*ROMLinkIdentity){"map": func(r *ROMLinkIdentity) { r.MapSHA256 = strings.Repeat("b", 64) }, "source digest": func(r *ROMLinkIdentity) { r.SourceSHA256 = strings.Repeat("A", 64) }, "size": func(r *ROMLinkIdentity) { r.SourceSize++ }, "programmed digest": func(r *ROMLinkIdentity) { r.ProgrammedSHA256 = "" }, "programmed size": func(r *ROMLinkIdentity) { r.ProgrammedSize = MaxPayloadSize + 1 }} {
		t.Run(name, func(t *testing.T) {
			bad := good
			alter(&bad)
			if bad.ValidFor(inspection.Descriptor) {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}

func TestROMAdoptionRequiresPrivateFiles(t *testing.T) {
	in := linkedROMFixture(t)
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	s, err := StageROMInput(context.Background(), root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cleanup()
	if err = os.Chmod(s.ProgrammedPath, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Adopt(root); err == nil {
		t.Fatal("writable retained ROM artifact adopted")
	}
}
