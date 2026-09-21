package corepackage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func TestCompositionBundleRejectsBeforeStaging(t *testing.T) {
	root := t.TempDir()
	for _, data := range [][]byte{nil, []byte("not tar"), make([]byte, 1024)} {
		if _, err := StageComposition(context.Background(), root, int64(len(data)), bytes.NewReader(data)); err == nil {
			t.Fatal("accepted malformed bundle")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejection changed staging: %v %v", entries, err)
	}
}

// A compact retained synthetic golden exercises staging in ordinary CI.
// FES_EXPANSION_SHELL optionally substitutes an exact producer artifact.
func TestCompositionStageProducerArtifact(t *testing.T) {
	source := os.Getenv("FES_EXPANSION_SHELL")
	if source == "" {
		packed, err := os.ReadFile("testdata/expansion-shell.rbf.gz")
		if err != nil {
			t.Fatal(err)
		}
		reader, err := gzip.NewReader(bytes.NewReader(packed))
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(io.LimitReader(reader, MaxPayloadSize+1))
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		sha := fmt.Sprintf("%x", sha256.Sum256(payload))
		if sha != "f38894e270e9e2771e157ebdf8767e92ad58d63de0764e7b03afdef0842ec5a0" {
			t.Fatal("synthetic Python/Go golden changed")
		}
		manifest, err := os.ReadFile("testdata/core-bundle-v2/manifests/valid-basic.toml")
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(manifest), "fes.simple-game", "fes.simple-computer")
		text = strings.ReplaceAll(text, "size = 12", fmt.Sprintf("size = %d", len(payload)))
		text = strings.ReplaceAll(text, "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", sha)
		text += "\n[[interfaces]]\nid = \"fes.expansion.zx81-ram\"\nmajor = 1\nminor = 0\nrequired = false\n"
		source = writeDirectory(t, []byte(text), payload)
	}
	manifest, err := os.ReadFile(filepath.Join(source, "manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(source, "core.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := decode(manifest, payload)
	if err != nil {
		t.Fatal(err)
	}
	archive := canonicalArchive(manifest, payload)
	sha := fmt.Sprintf("%x", sha256.Sum256(payload))
	asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: sha, CartSize: int64(len(payload)), Device: expansion.Device, Format: 1, Map: expansion.Map, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: descriptor.Build.ID, ShellPackageID: packageIdentity(manifest, payload), ShellSHA256: sha, Slot: expansion.Slot, SlotMajor: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ComposeArchive(archive, asset)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err = bundle.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged, err := StageComposition(context.Background(), root, int64(encoded.Len()), bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if staged.Composition == nil || *staged.Composition != bundle.Composition {
		t.Fatal("stage lost identity")
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 {
		t.Fatalf("adoption %v %v", adopted, err)
	}
	if adopted[0].Composition == nil || *adopted[0].Composition != bundle.Composition {
		t.Fatal("adoption lost identity")
	}
	if err = adopted[0].Cleanup(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cleanup retained companions: %v %v", entries, err)
	}
	if err = staged.Cleanup(); err != nil {
		t.Fatal(err)
	}
	changed := bundle
	changed.Payload = bytes.Clone(bundle.Payload)
	changed.Payload[0] ^= 1
	if changed.Write(&bytes.Buffer{}) == nil {
		t.Fatal("accepted changed composed payload")
	}
}

func TestCompositionShellAdmissionMatchesRuntime(t *testing.T) {
	base := Inspection{Descriptor: Descriptor{ABI: Contract{ID: "fes.simple-computer", Major: 1}, Interfaces: []Interface{{ID: expansion.Slot, Major: 1}}}}
	if _, err := compositionShell(base, nil); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Inspection){
		"abi":      func(i *Inspection) { i.Descriptor.ABI.ID = "fes.application" },
		"required": func(i *Inspection) { i.Descriptor.Interfaces[0].Required = true },
		"version":  func(i *Inspection) { i.Descriptor.Interfaces[0].Minor = 1 },
		"duplicate": func(i *Inspection) {
			i.Descriptor.Interfaces = append(i.Descriptor.Interfaces, i.Descriptor.Interfaces[0])
		},
		"missing": func(i *Inspection) { i.Descriptor.Interfaces = nil },
	} {
		t.Run(name, func(t *testing.T) {
			copy := base
			copy.Descriptor.Interfaces = append([]Interface(nil), base.Descriptor.Interfaces...)
			change(&copy)
			if _, err := compositionShell(copy, nil); err == nil {
				t.Fatal("accepted unsupported shell")
			}
		})
	}
}
