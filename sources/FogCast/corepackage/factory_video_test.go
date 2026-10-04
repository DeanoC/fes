package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func factoryVideoFixture(t *testing.T) (string, []byte, FactoryVideoIndex) {
	return factoryVideoFixtureLayout(t, expansion.ColecoVideoLayout)
}

func factoryVideoFixtureLayout(t *testing.T, layout string) (string, []byte, FactoryVideoIndex) {
	t.Helper()
	pkg, assets := developerPartsFixtureLayout(t, layout)
	root := filepath.Join(t.TempDir(), "core-video-parts")
	id := assets[0].Manifest.ShellPackageID
	if err := os.MkdirAll(filepath.Join(root, id), 0o755); err != nil {
		t.Fatal(err)
	}
	index := FactoryVideoIndex{Version: 1, Packages: []FactoryVideoPackage{{PackageID: id}}}
	for i, profile := range []string{"direct", "scanlines"} {
		manifest := assets[0].Manifest
		manifest.RecipeSHA256 = strings.Repeat(fmt.Sprintf("%x", i+1), 64)
		asset, err := expansion.NewAsset(manifest, assets[0].Cart)
		if err != nil {
			t.Fatal(err)
		}
		var archive bytes.Buffer
		if err := asset.Write(&archive); err != nil {
			t.Fatal(err)
		}
		ref := FactoryVideoReference{Profile: profile, PartID: asset.ID, ArchivePath: id + "/" + asset.ID + ".tar", ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(archive.Bytes())), ArchiveSize: int64(archive.Len())}
		index.Packages[0].Parts = append(index.Packages[0].Parts, ref)
		if err := os.WriteFile(filepath.Join(root, ref.ArchivePath), archive.Bytes(), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	encoded, _ := json.Marshal(index)
	if err := os.WriteFile(filepath.Join(root, "index.json"), append(encoded, '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, id), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755); _ = os.Chmod(filepath.Join(root, id), 0o755) })
	return root, pkg, index
}

func TestFactoryVideoTreeAdmitsExactBytesAndCatalogReference(t *testing.T) {
	for _, layout := range []string{expansion.ColecoVideoLayout, expansion.ColecoNativeVideoLayout} {
		t.Run(layout, func(t *testing.T) { testFactoryVideoTreeAdmitsExactBytesAndCatalogReference(t, layout) })
	}
}

func testFactoryVideoTreeAdmitsExactBytesAndCatalogReference(t *testing.T, layout string) {
	t.Helper()
	root, shell, index := factoryVideoFixtureLayout(t, layout)
	set, err := ReadFactoryVideoParts(context.Background(), root, func(ctx context.Context, id string) ([]byte, error) {
		if id != index.Packages[0].PackageID {
			t.Fatal("unexpected shell resolution")
		}
		return shell, nil
	})
	if err != nil || len(set.Parts) != 2 || set.SHA256 != fmt.Sprintf("%x", sha256.Sum256(set.IndexBytes)) {
		t.Fatalf("admit factory tree: %v", err)
	}
	part := set.Parts[0]
	ref := part.Reference
	ref.ArchivePath = "core-video-parts/" + ref.ArchivePath
	if _, err := AdmitFactoryVideoPart(context.Background(), part.PackageID, ref, part.Archive, shell); err != nil {
		t.Fatalf("catalog prefix: %v", err)
	}
	// Python's producer pads tar records beyond Go's writer. Preserve and bind
	// those exact bytes rather than rewriting a valid immutable producer archive.
	padded := append(bytes.Clone(part.Archive), make([]byte, 10240-len(part.Archive)%10240)...)
	paddedRef := ref
	paddedRef.ArchiveSize = int64(len(padded))
	paddedRef.ArchiveSHA256 = fmt.Sprintf("%x", sha256.Sum256(padded))
	if _, err := AdmitFactoryVideoPart(context.Background(), part.PackageID, paddedRef, padded, shell); err != nil {
		t.Fatalf("producer tar padding: %v", err)
	}
	changed := bytes.Clone(part.Archive)
	changed[0] ^= 1
	if _, err := AdmitFactoryVideoPart(context.Background(), part.PackageID, ref, changed, shell); err == nil {
		t.Fatal("changed archive admitted")
	}
	ref.PartID = strings.Repeat("a", 64)
	if _, err := AdmitFactoryVideoPart(context.Background(), part.PackageID, ref, part.Archive, shell); err == nil {
		t.Fatal("wrong part admitted")
	}
	manifest := part.Asset.Manifest
	manifest.Slot, manifest.Map = expansion.NativeVideoSlot, expansion.ColecoNativeVideoMap
	if layout == expansion.ColecoNativeVideoLayout {
		manifest.Slot, manifest.Map = expansion.VideoSlot, expansion.ColecoVideoMap
	}
	crossed, err := expansion.NewAsset(manifest, part.Asset.Cart)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := crossed.Write(&archive); err != nil {
		t.Fatal(err)
	}
	ref.PartID, ref.ArchiveSize, ref.ArchiveSHA256 = crossed.ID, int64(archive.Len()), fmt.Sprintf("%x", sha256.Sum256(archive.Bytes()))
	if _, err := AdmitFactoryVideoPart(context.Background(), part.PackageID, ref, archive.Bytes(), shell); err == nil {
		t.Fatal("crossed source contract admitted with valid shell identities")
	}
}

func TestFactoryVideoTreeRejectsUnclosedOrMutableObjects(t *testing.T) {
	for _, test := range []string{"root-mode", "index-mode", "part-mode", "extra-root", "extra-part", "missing", "symlink", "wrong-shell"} {
		t.Run(test, func(t *testing.T) {
			root, shell, index := factoryVideoFixture(t)
			part := filepath.Join(root, index.Packages[0].Parts[0].ArchivePath)
			_ = os.Chmod(root, 0o755)
			_ = os.Chmod(filepath.Dir(part), 0o755)
			switch test {
			case "index-mode":
				_ = os.Chmod(filepath.Join(root, "index.json"), 0o644)
			case "part-mode":
				_ = os.Chmod(part, 0o644)
			case "extra-root":
				_ = os.WriteFile(filepath.Join(root, "extra"), []byte("extra"), 0o444)
			case "extra-part":
				_ = os.WriteFile(filepath.Join(filepath.Dir(part), "extra"), []byte("extra"), 0o444)
			case "missing":
				_ = os.Remove(part)
			case "symlink":
				_ = os.Remove(part)
				_ = os.Symlink("../index.json", part)
			case "wrong-shell":
				shell = bytes.Clone(shell)
				shell[512] ^= 1
			}
			_ = os.Chmod(filepath.Dir(part), 0o555)
			if test != "root-mode" {
				_ = os.Chmod(root, 0o555)
			}
			if _, err := ReadFactoryVideoParts(context.Background(), root, func(context.Context, string) ([]byte, error) { return shell, nil }); err == nil {
				t.Fatal("invalid tree admitted")
			}
		})
	}
}

func TestFactoryVideoIndexRejectsAlternateAndAmbiguousDocuments(t *testing.T) {
	_, _, index := factoryVideoFixture(t)
	canonical, _ := json.Marshal(index)
	canonical = append(canonical, '\n')
	for _, changed := range [][]byte{
		bytes.TrimSpace(canonical),
		append(bytes.Clone(canonical), []byte("{}")...),
		bytes.Replace(canonical, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(canonical, []byte(`"version":1`), []byte(`"version":1,"unknown":1`), 1),
		bytes.Replace(canonical, []byte(`"scanlines"`), []byte(`"direct"`), 1),
		bytes.Replace(canonical, []byte(`"archive_path":"`), []byte(`"archive_path":"../`), 1),
	} {
		if _, err := DecodeFactoryVideoIndex(changed); err == nil {
			t.Fatal("alternate index admitted")
		}
	}
	duplicate := FactoryVideoPackage{PackageID: strings.Repeat("f", 64), Parts: append([]FactoryVideoReference(nil), index.Packages[0].Parts...)}
	for i := range duplicate.Parts {
		duplicate.Parts[i].ArchivePath = duplicate.PackageID + "/" + duplicate.Parts[i].PartID + ".tar"
	}
	index.Packages = append(index.Packages, duplicate)
	encoded, _ := json.Marshal(index)
	if _, err := DecodeFactoryVideoIndex(append(encoded, '\n')); err == nil {
		t.Fatal("duplicate part IDs across shells admitted")
	}
}
