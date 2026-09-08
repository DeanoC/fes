package targetimage

import (
	"crypto/sha256"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extraBundle(t *testing.T, system string, mutate func(*MegaDriveBundleManifest)) string {
	t.Helper()
	dir := t.TempDir()
	payload := []byte("synthetic " + system)
	recipe, repository, err := extraCoreRecipe(system)
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	if system == "snes" {
		revision = "93d359e6f23c734ae3928984e88bed1d9b53cbac"
	}
	if system == "nes" {
		revision = "9a63821173b6da4d6e95dcbe2e2a322ec8171144"
	}
	m := MegaDriveBundleManifest{Format: 1, ABI: "mister", System: system, Artifact: system + ".rbf", SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Size: int64(len(payload)), Repository: repository, Revision: revision, Recipe: recipe, RecipeSHA256: strings.Repeat("b", 64), Toolchain: "Quartus 17.0.2 Lite"}
	if mutate != nil {
		mutate(&m)
	}
	data, err := toml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, system+"-rbf.toml"), data, 0444); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, system+".rbf"), payload, 0444); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
	return dir
}
func TestAdditionalCoreSelectionAndInstalledVerification(t *testing.T) {
	for _, system := range []string{"pong", "snes", "nes"} {
		t.Run(system, func(t *testing.T) {
			bundle := extraBundle(t, system, nil)
			cache := t.TempDir()
			record := filepath.Join(cache, system+".selection.toml")
			selection, err := PrepareCoreSelection(system, bundle, cache, record)
			if err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(cache, system+".rbf")
			if selection.System != system || selection.InstallPath != "/usr/share/mister-runtime/cores/"+system+".rbf" {
				t.Fatal(selection)
			}
			if err := VerifyCoreSelection(system, artifact, record); err != nil {
				t.Fatal(err)
			}
			os.Chmod(artifact, 0644) // Installed files may use the image's regular mode.
			if err := VerifyCoreSelection(system, artifact, record); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(artifact, []byte("corrupted"), 0644)
			if VerifyCoreSelection(system, artifact, record) == nil {
				t.Fatal("corrupt bytes accepted")
			}
		})
	}
}
func TestAdditionalCoreSelectionRejectsWrongIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*MegaDriveBundleManifest){
		"system":     func(m *MegaDriveBundleManifest) { m.System = "megadrive" },
		"artifact":   func(m *MegaDriveBundleManifest) { m.Artifact = "../pong.rbf" },
		"repository": func(m *MegaDriveBundleManifest) { m.Repository = "https://example.com/core" },
		"revision":   func(m *MegaDriveBundleManifest) { m.Revision = strings.Repeat("a", 40) },
		"recipe":     func(m *MegaDriveBundleManifest) { m.Recipe = "scripts/build_pong.py" },
		"digest":     func(m *MegaDriveBundleManifest) { m.SHA256 = strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			bundle := extraBundle(t, "snes", mutate)
			cache := t.TempDir()
			if _, err := PrepareCoreSelection("snes", bundle, cache, filepath.Join(cache, "snes.selection.toml")); err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}
func TestAdditionalCoreSelectionRejectsCrossSystemAndWritableRecord(t *testing.T) {
	bundle := extraBundle(t, "pong", nil)
	cache := t.TempDir()
	record := filepath.Join(cache, "pong.selection.toml")
	if _, err := PrepareCoreSelection("pong", bundle, cache, record); err != nil {
		t.Fatal(err)
	}
	if VerifyCoreSelection("snes", filepath.Join(cache, "pong.rbf"), record) == nil {
		t.Fatal("cross-system record accepted")
	}
	os.Chmod(record, 0644)
	if VerifyCoreSelection("pong", filepath.Join(cache, "pong.rbf"), record) == nil {
		t.Fatal("writable record accepted")
	}
}
