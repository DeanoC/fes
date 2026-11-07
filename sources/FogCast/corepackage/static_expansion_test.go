package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func staticPackageFixture(t *testing.T) (Descriptor, []byte, []byte) {
	t.Helper()
	manifest, base := fixtureBytes(t, loadCases(t)[0])
	d, err := decode(manifest, base)
	if err != nil {
		t.Fatal(err)
	}
	persona := []byte("synthetic-partial-persona\x00\xff")
	sum := sha256.Sum256(persona)
	d.Format = 5
	d.Target = Target{Platform: "de25_nano", Device: "A5EB013BB23BE4SCS", ProgrammingProfile: "de25-linux-static-computer-v1"}
	d.StaticExpansion = &StaticExpansion{File: "persona.rbf", Size: int64(len(persona)), SHA256: hex.EncodeToString(sum[:]), ControlBuildID: strings.Repeat("1", 32), HardwareID: 0xc001a001, RootQDBSHA256: strings.Repeat("2", 64), InterfaceSHA256: strings.Repeat("3", 64), ClockResetSHA256: strings.Repeat("4", 64), POF0Hash: strings.Repeat("5", 64)}
	return d, base, persona
}
func staticArchive(t *testing.T, d Descriptor, base, persona []byte) ([]byte, []byte) {
	t.Helper()
	manifest, err := toml.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := writeCanonicalMembers([]canonicalMember{{name: "manifest.toml", data: manifest}, {name: "core.rbf", data: base}, {name: "persona.rbf", data: persona}}, MaxArchiveSize)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, archive
}
func TestStaticPackageReadStageAndStreamBindBothOpaqueImages(t *testing.T) {
	d, base, persona := staticPackageFixture(t)
	manifest, archive := staticArchive(t, d, base, persona)
	dir := writeDirectory(t, manifest, base)
	if err := os.WriteFile(filepath.Join(dir, "persona.rbf"), persona, 0600); err != nil {
		t.Fatal(err)
	}
	expected := sha256.New()
	expected.Write([]byte("FES-CORE-PACKAGE-5\n"))
	for _, body := range [][]byte{manifest, base, persona} {
		var length [8]byte
		binary.LittleEndian.PutUint64(length[:], uint64(len(body)))
		expected.Write(length[:])
		expected.Write(body)
	}
	id := hex.EncodeToString(expected.Sum(nil))
	inspected, canonical, err := ReadPackage(dir)
	if err != nil || inspected.PackageID != id || !bytes.Equal(canonical, archive) {
		t.Fatalf("read/canonical identity: %v %+v", err, inspected)
	}
	listing, err := InspectListing(context.Background(), dir)
	if err != nil || listing.PackageID != id {
		t.Fatalf("stream identity: %v %+v", err, listing)
	}
	staged, err := StageBytes(context.Background(), t.TempDir(), archive)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if staged.PackageID != id || staged.Descriptor.StaticExpansion == nil {
		t.Fatalf("staged: %+v", staged)
	}
	reopened, err := InspectPackage(staged.Directory)
	if err != nil || reopened.PackageID != id {
		t.Fatalf("sealed staged read: %v %+v", err, reopened)
	}
	body, err := os.ReadFile(filepath.Join(staged.Directory, "persona.rbf"))
	if err != nil || !bytes.Equal(body, persona) {
		t.Fatal("persona staging changed", err)
	}
	storeRoot := filepath.Join(t.TempDir(), "installed")
	store, err := NewStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err := store.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || !created || stored.PackageID != id {
		t.Fatalf("store import: %v %+v", err, stored)
	}
	store, err = NewStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	stored, retained, err := store.Read(context.Background(), id)
	if err != nil || stored.Descriptor.StaticExpansion == nil || !bytes.Equal(retained, archive) {
		t.Fatal("reopened store lost default persona", err)
	}
	changed := append(bytes.Clone(persona), 1)
	sum := sha256.Sum256(changed)
	d.StaticExpansion.Size = int64(len(changed))
	d.StaticExpansion.SHA256 = hex.EncodeToString(sum[:])
	other, _ := staticArchive(t, d, base, changed)
	if packageIdentity(other, base, changed) == id {
		t.Fatal("persona change did not alter sealed package identity")
	}
}
func TestStaticPackageRejectsWrongBindingsAndMixedFormats(t *testing.T) {
	cases := map[string]func(*Descriptor){
		"wrong-platform":       func(d *Descriptor) { d.Target.Platform = "de10_nano" },
		"wrong-device":         func(d *Descriptor) { d.Target.Device = "5CSEBA6U23I7" },
		"ordinary-profile":     func(d *Descriptor) { d.Target.ProgrammingProfile = "de25-linux-computer-v1" },
		"wrong-filename":       func(d *Descriptor) { d.StaticExpansion.File = "../persona.rbf" },
		"missing-table":        func(d *Descriptor) { d.StaticExpansion = nil },
		"nonempty-default":     func(d *Descriptor) { d.StaticExpansion.HardwareID = 0xc002b002 },
		"zero-controller":      func(d *Descriptor) { d.StaticExpansion.ControlBuildID = strings.Repeat("0", 32) },
		"zero-root":            func(d *Descriptor) { d.StaticExpansion.RootQDBSHA256 = strings.Repeat("0", 64) },
		"zero-interface":       func(d *Descriptor) { d.StaticExpansion.InterfaceSHA256 = strings.Repeat("0", 64) },
		"zero-clock":           func(d *Descriptor) { d.StaticExpansion.ClockResetSHA256 = strings.Repeat("0", 64) },
		"zero-pof":             func(d *Descriptor) { d.StaticExpansion.POF0Hash = strings.Repeat("0", 64) },
		"oversized":            func(d *Descriptor) { d.StaticExpansion.Size = MaxPayloadSize + 1 },
		"digest-mismatch":      func(d *Descriptor) { d.StaticExpansion.SHA256 = strings.Repeat("a", 64) },
		"length-mismatch":      func(d *Descriptor) { d.StaticExpansion.Size++ },
		"format2-with-persona": func(d *Descriptor) { d.Format = 2 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			d, base, persona := staticPackageFixture(t)
			change(&d)
			_, archive := staticArchive(t, d, base, persona)
			if _, err := StageBytes(context.Background(), t.TempDir(), archive); err == nil {
				t.Fatal("accepted invalid static package")
			}
		})
	}
}
func TestStaticPackageRejectsExtraAndSubstitutedCompanion(t *testing.T) {
	d, base, persona := staticPackageFixture(t)
	manifest, archive := staticArchive(t, d, base, persona)
	for name, data := range map[string][]byte{"unknown-field": append(bytes.Clone(manifest), []byte("\n[static_expansion.extra]\nvalue = 1\n")...), "float-id": bytes.Replace(manifest, []byte("hardware_id = 3221331969"), []byte("hardware_id = 3221331969.0"), 1)} {
		if name == "float-id" && bytes.Equal(data, manifest) {
			t.Fatal("fixture did not change integer")
		}
		if _, err := decode(data, base, persona); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	bad := bytes.Clone(persona)
	bad[0] ^= 1
	_, badArchive := staticArchive(t, d, base, bad)
	for name, body := range map[string][]byte{"missing-persona": canonicalArchive(manifest, base), "corrupt-persona": badArchive, "trailing-member": append(bytes.Clone(archive), make([]byte, 512)...)} {
		if _, err := StageBytes(context.Background(), t.TempDir(), body); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	dir := writeDirectory(t, manifest, base)
	outside := filepath.Join(t.TempDir(), "persona.rbf")
	if err := os.WriteFile(outside, persona, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "persona.rbf")); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectListing(context.Background(), dir); err == nil {
		t.Fatal("stream accepted symlink persona")
	}
}

func TestStaticPackageSharedStructuralCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/core-bundle-v5/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		fixtureCase
		Persona string `json:"persona"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			root := "testdata/core-bundle-v5"
			manifest, err := os.ReadFile(filepath.Join(root, c.Manifest))
			if err != nil {
				t.Fatal(err)
			}
			base, err := os.ReadFile(filepath.Join(root, c.Payload))
			if err != nil {
				t.Fatal(err)
			}
			persona, err := os.ReadFile(filepath.Join(root, c.Persona))
			if err != nil {
				t.Fatal(err)
			}
			dir := writeDirectory(t, manifest, base)
			if err := os.WriteFile(filepath.Join(dir, "persona.rbf"), persona, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := InspectPackage(dir)
			if (err == nil) != c.Valid {
				t.Fatalf("directory valid=%v err=%v", c.Valid, err)
			}
			listing, streamErr := InspectListing(context.Background(), dir)
			if (streamErr == nil) != c.Valid {
				t.Fatalf("stream valid=%v err=%v", c.Valid, streamErr)
			}
			if c.Valid && (got.PackageID != c.PackageID || listing.PackageID != c.PackageID) {
				t.Fatalf("identity disagreement %s %s %s", got.PackageID, listing.PackageID, c.PackageID)
			}
		})
	}
}
