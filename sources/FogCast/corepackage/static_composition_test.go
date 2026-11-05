package corepackage

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
	"github.com/pelletier/go-toml/v2"
)

func staticSelectedFixture(t *testing.T) ([]byte, expansion.Asset) {
	t.Helper()
	d, base, empty := staticPackageFixture(t)
	manifest, archive := staticArchive(t, d, base, empty)
	s := d.StaticExpansion
	body := []byte("different opaque vendor RAM persona")
	sum := sha256.Sum256(body)
	m := expansion.Manifest{Format: 2, Device: expansion.StaticDevice, Map: expansion.StaticMap, Slot: expansion.Slot, SlotMajor: 2, CartSize: int64(len(body)), CartSHA256: hex.EncodeToString(sum[:]), RecipeSHA256: strings.Repeat("c", 64), Revision: strings.Repeat("d", 40), ShellBuildID: d.Build.ID, ShellPackageID: packageIdentity(manifest, base, empty), ShellSHA256: d.Payload.SHA256, Partial: &expansion.StaticPersona{ControlBuildID: s.ControlBuildID, HardwareID: expansion.RAM16KPersonaID, RootQDBSHA256: s.RootQDBSHA256, InterfaceSHA256: s.InterfaceSHA256, ClockResetSHA256: s.ClockResetSHA256, POF0Hash: s.POF0Hash}}
	a, err := expansion.NewAsset(m, body)
	if err != nil {
		t.Fatal(err)
	}
	return archive, a
}
func TestStaticCompositionBundleDefaultAndSelectedRoundTrip(t *testing.T) {
	archive, asset := staticSelectedFixture(t)
	original := bytes.Clone(archive)
	var identities []string
	for _, selected := range []*expansion.Asset{nil, &asset} {
		b, err := ComposeStaticArchive(archive, selected)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := b.Write(&out); err != nil {
			t.Fatal(err)
		}
		read, err := ReadStaticCompositionBundle(bytes.NewReader(out.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if read.Composition != b.Composition || !bytes.Equal(read.Package, original) || (read.Expansion == nil) != (selected == nil) {
			t.Fatal("composition lost sealed inputs")
		}
		if selected != nil && (read.Expansion.ID != selected.ID || !bytes.Equal(read.Expansion.Cart, selected.Cart)) {
			t.Fatal("selected vendor image changed")
		}
		identities = append(identities, read.Composition.ID)
	}
	if identities[0] == identities[1] || !bytes.Equal(archive, original) {
		t.Fatal("selection identity or source preservation failed")
	}
}
func TestStaticCompositionBundleOwnsAndRevalidatesInputs(t *testing.T) {
	archive, asset := staticSelectedFixture(t)
	b, err := ComposeStaticArchive(archive, &asset)
	if err != nil {
		t.Fatal(err)
	}
	archive[0] ^= 1
	asset.Cart[0] ^= 1
	asset.Manifest.Partial.ControlBuildID = "wrong"
	var out bytes.Buffer
	if err := b.Write(&out); err != nil {
		t.Fatal("caller changed retained composition", err)
	}
	b.Composition.HardwareID = expansion.EmptyPersonaID
	var rejected bytes.Buffer
	if err := b.Write(&rejected); err == nil || rejected.Len() != 0 {
		t.Fatal("changed receipt published bytes")
	}
}
func rewriteStaticBundle(t *testing.T, source []byte, change func(map[string][]byte)) []byte {
	t.Helper()
	members := map[string][]byte{}
	r := tar.NewReader(bytes.NewReader(source))
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		members[h.Name], err = io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
	}
	change(members)
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	for _, name := range []string{"package.tar", "expansion.tar", "composition.json", "../extra"} {
		if body, ok := members[name]; ok {
			if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func TestStaticCompositionBundleRejectsSubstitutionAndUnknownMembers(t *testing.T) {
	archive, asset := staticSelectedFixture(t)
	b, err := ComposeStaticArchive(archive, &asset)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := b.Write(&out); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(map[string][]byte){
		"missing-selection": func(m map[string][]byte) { delete(m, "expansion.tar") },
		"missing-package":   func(m map[string][]byte) { delete(m, "package.tar") },
		"traversal":         func(m map[string][]byte) { m["../extra"] = []byte("x") },
		"changed-receipt":   func(m map[string][]byte) { m["composition.json"] = []byte(`{"composition_id":"bad"}`) },
		"unknown-field-receipt": func(m map[string][]byte) {
			m["composition.json"] = append([]byte(`{"extra":0,`), m["composition.json"][1:]...)
		},
		"changed-package": func(m map[string][]byte) { m["package.tar"] = bytes.Clone(m["package.tar"]); m["package.tar"][0] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := rewriteStaticBundle(t, out.Bytes(), change)
			if _, err := ReadStaticCompositionBundle(bytes.NewReader(bad)); err == nil {
				t.Fatal("accepted substituted static composition")
			}
		})
	}
	d, base, _ := staticPackageFixture(t)
	d.StaticExpansion = nil
	d.Format = 2
	d.Target = Target{Platform: "de10_nano", Device: "5CSEBA6U23I7", ProgrammingProfile: "fes-gp-v1"}
	manifest, err := toml.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ComposeStaticArchive(canonicalArchive(manifest, base), nil); err == nil {
		t.Fatal("ordinary package entered static composition")
	}
}

func TestStaticCompositionSharedGoldenVectors(t *testing.T) {
	root := "testdata/core-bundle-v5/"
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(root + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	manifest, base, empty := read("manifests/valid-basic.toml"), read("payloads/base.rbf"), read("payloads/persona.rbf")
	archive, err := writeCanonicalMembers([]canonicalMember{{name: "manifest.toml", data: manifest}, {name: "core.rbf", data: base}, {name: "persona.rbf", data: empty}}, MaxArchiveSize)
	if err != nil {
		t.Fatal(err)
	}
	var m expansion.Manifest
	if err := json.Unmarshal(read("expansions/ram16k/manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	asset, err := expansion.NewAsset(m, read("expansions/ram16k/cart.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(asset.ManifestBytes, read("expansions/ram16k/manifest.json")) {
		t.Fatal("Go asset canonical bytes differ from shared independent producer")
	}
	var vectors []struct {
		Name        string                      `json:"name"`
		Composition expansion.StaticComposition `json:"composition"`
	}
	if err := json.Unmarshal(read("compositions.json"), &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			var selected *expansion.Asset
			if v.Name == "ram16k" {
				selected = &asset
			}
			b, err := ComposeStaticArchive(archive, selected)
			if err != nil {
				t.Fatal(err)
			}
			if b.Composition != v.Composition {
				t.Fatalf("cross-reader identity disagreement: got %+v expected %+v", b.Composition, v.Composition)
			}
			var transport bytes.Buffer
			if err := b.Write(&transport); err != nil {
				t.Fatal(err)
			}
			again, err := ReadStaticCompositionBundle(&transport)
			if err != nil || again.Composition != v.Composition {
				t.Fatal("golden transport changed composition", err)
			}
		})
	}
}
