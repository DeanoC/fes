package targetimage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

func factoryVideoSelectionFixture(t *testing.T) (string, string) {
	t.Helper()
	base, _, _ := packageSelectionFixtureForCore(t, "fes.coleco")
	packed, err := os.Open("../../corepackage/testdata/expansion-shell.rbf.gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(packed)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	reader.Close()
	packed.Close()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(base, "manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(manifest), "fes.simple-game", "fes.application")
	text = strings.ReplaceAll(text, "size = 12", fmt.Sprintf("size = %d", len(payload)))
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	text = strings.ReplaceAll(text, "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1", digest)
	text += "\n[[interfaces]]\nid = 'fes.expansion.coleco-bus'\nmajor = 2\nminor = 0\nrequired = false\n\n[[interfaces]]\nid = 'fes.fabric.video.raster-rgb888'\nmajor = 1\nminor = 0\nrequired = false\n"
	for name, data := range map[string][]byte{"manifest.toml": []byte(text), "core.rbf": payload} {
		_ = os.Chmod(filepath.Join(base, name), 0o644)
		if err := os.WriteFile(filepath.Join(base, name), data, 0o444); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(filepath.Join(base, name), 0o444)
	}
	inspection, err := corepackage.InspectPackage(base)
	if err != nil {
		t.Fatal(err)
	}
	packages := filepath.Join(t.TempDir(), "core-packages")
	if err := os.Mkdir(packages, 0o755); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(packages, inspection.PackageID)
	_ = os.Chmod(base, 0o755)
	if err := os.Rename(base, selected); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(selected, 0o555)
	root := filepath.Join(t.TempDir(), "core-video-parts")
	if err := os.MkdirAll(filepath.Join(root, inspection.PackageID), 0o755); err != nil {
		t.Fatal(err)
	}
	index := corepackage.FactoryVideoIndex{Version: 1, Packages: []corepackage.FactoryVideoPackage{{PackageID: inspection.PackageID}}}
	for i, profile := range []string{"direct", "scanlines"} {
		asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: digest, CartSize: int64(len(payload)), Device: expansion.Device, Format: 1, Map: expansion.ColecoVideoMap, RecipeSHA256: strings.Repeat(fmt.Sprintf("%x", i+1), 64), Revision: strings.Repeat("c", 40), ShellBuildID: inspection.Descriptor.Build.ID, ShellPackageID: inspection.PackageID, ShellSHA256: digest, Slot: expansion.VideoSlot, SlotMajor: 1}, payload)
		if err != nil {
			t.Fatal(err)
		}
		var archive bytes.Buffer
		if err := asset.Write(&archive); err != nil {
			t.Fatal(err)
		}
		ref := corepackage.FactoryVideoReference{Profile: profile, PartID: asset.ID, ArchivePath: inspection.PackageID + "/" + asset.ID + ".tar", ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(archive.Bytes())), ArchiveSize: int64(archive.Len())}
		index.Packages[0].Parts = append(index.Packages[0].Parts, ref)
		if err := os.WriteFile(filepath.Join(root, ref.ArchivePath), archive.Bytes(), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	encoded, _ := json.Marshal(index)
	if err := os.WriteFile(filepath.Join(root, "index.json"), append(encoded, '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(root, inspection.PackageID), 0o555)
	_ = os.Chmod(root, 0o555)
	t.Cleanup(func() { _ = removePath(root); _ = removePath(packages) })
	return root, packages
}

func TestFactoryVideoSelectionCoverageAndCachePair(t *testing.T) {
	ctx := context.Background()
	root, packages := factoryVideoSelectionFixture(t)
	if _, err := InspectFactoryVideoParts(ctx, "", packages); err == nil {
		t.Fatal("marked shell admitted without video parts")
	}
	cache := t.TempDir()
	if err := PrepareFactoryVideoParts(ctx, root, packages, cache); err != nil {
		t.Fatal(err)
	}
	set, err := InspectFactoryVideoParts(ctx, filepath.Join(cache, "core-video-parts"), packages)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFactoryVideoSelection(set, filepath.Join(cache, FactoryVideoSelectionName)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(FactoryVideoBuildInputs(set), "factory_video_1_profile=scanlines\n") {
		t.Fatal("missing part receipt")
	}
	t.Cleanup(func() { _ = removePath(filepath.Join(cache, "core-video-parts")) })
	// An invalid replacement cannot destroy either current cache component.
	_ = os.Chmod(filepath.Join(root, "index.json"), 0o644)
	if err := PrepareFactoryVideoParts(ctx, root, packages, cache); err == nil {
		t.Fatal("invalid source replaced cache")
	}
	if err := VerifyFactoryVideoSelection(set, filepath.Join(cache, FactoryVideoSelectionName)); err != nil {
		t.Fatal("cache changed after rejection", err)
	}
}

func TestFactoryVideoSelectionPublicationFailureRestoresPair(t *testing.T) {
	ctx := context.Background()
	root, packages := factoryVideoSelectionFixture(t)
	cache := t.TempDir()
	if err := PrepareFactoryVideoParts(ctx, root, packages, cache); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(cache, FactoryVideoSelectionName))
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareFactoryVideoParts(ctx, root, packages, cache, func(from, to string) error {
		if filepath.Base(from) == "record" {
			return errors.New("injected record publication failure")
		}
		return os.Rename(from, to)
	}); err == nil {
		t.Fatal("injected publication succeeded")
	}
	set, err := InspectFactoryVideoParts(ctx, filepath.Join(cache, "core-video-parts"), packages)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, set.IndexBytes) {
		t.Fatal("old tree was not retained")
	}
	if err := VerifyFactoryVideoSelection(set, filepath.Join(cache, FactoryVideoSelectionName)); err != nil {
		t.Fatal("old record was not restored", err)
	}
	t.Cleanup(func() { _ = removePath(filepath.Join(cache, "core-video-parts")) })
}

func TestFactoryVideoUnmarkedPackagesPermitAbsentTree(t *testing.T) {
	directory, record, selection := packageSelectionFixture(t)
	cache := t.TempDir()
	if _, err := PrepareCorePackageSelection(directory, record, cache, filepath.Join(cache, corePackageSelectionName)); err != nil {
		t.Fatal(err)
	}
	set, err := InspectFactoryVideoParts(context.Background(), "", filepath.Join(cache, "core-packages"))
	if err != nil || set != nil {
		t.Fatalf("legacy package-only fixture: %v", err)
	}
	t.Cleanup(func() { _ = removePath(filepath.Join(cache, "core-packages", selection.PackageID)) })
}
