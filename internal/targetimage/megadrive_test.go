package targetimage_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/targetimage"
	"github.com/pelletier/go-toml/v2"
)

const validMegaDriveUpstreamLock = `format = 1

[mister_runtime]
commit = '4398f41bf504329e5c9b21f916cb37952bfb4cc'
mount_path = '/runtime-source'

[idle_rbf]
repository = 'https://github.com/MiSTer-devel/Distribution_MiSTer'
commit = 'f7bde4becb452ca28f604ad9802bbed5c6b58e01'
path = 'menu.rbf'
sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
size = 1
install_path = '/usr/share/mister-runtime/idle.rbf'

[megadrive_rbf]
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
commit = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
path = 'releases/MegaDrive_20260603.rbf'
sha256 = '%s'
size = %d
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
`

func TestPrepareMegaDriveSelectionSourceBuilt(t *testing.T) {
	t.Parallel()
	payload := []byte("sealed source-built megadrive")
	bundle := writeMegaDriveBundle(t, payload)
	cache := filepath.Join(t.TempDir(), "cache")
	output := filepath.Join(cache, "megadrive.selection.toml")

	selection, err := targetimage.PrepareMegaDriveSelection(targetimage.MegaDriveSelectionRequest{
		Source: "source-built",
		Bundle: bundle,
		Cache:  cache,
		Output: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Origin != "source-built" || selection.ABI != "mister" || selection.System != "megadrive" ||
		selection.Artifact != "megadrive.rbf" || selection.InstallPath != "/usr/share/mister-runtime/cores/megadrive.rbf" ||
		selection.Repository != "https://github.com/MiSTer-devel/MegaDrive_MiSTer" ||
		selection.Recipe != "scripts/rebuild_core.py" || selection.Toolchain == "" {
		t.Fatalf("selection = %#v", selection)
	}
	if got, err := os.ReadFile(filepath.Join(cache, "megadrive.rbf")); err != nil || string(got) != string(payload) {
		t.Fatalf("cached artifact = %q, err = %v", got, err)
	}
	for _, path := range []string{filepath.Join(cache, "megadrive.rbf"), output} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o222 != 0 || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s mode = %v, want sealed regular file", path, info.Mode())
		}
	}
	var recorded targetimage.MegaDriveSelection
	selectionBytes, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := toml.Unmarshal(selectionBytes, &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != selection {
		t.Fatalf("recorded selection = %#v, returned = %#v", recorded, selection)
	}
}

func TestPrepareMegaDriveSelectionDefaultsToSourceBuilt(t *testing.T) {
	t.Parallel()
	payload := []byte("default source-built")
	_, err := targetimage.PrepareMegaDriveSelection(targetimage.MegaDriveSelectionRequest{
		Cache:  filepath.Join(t.TempDir(), "cache"),
		Output: filepath.Join(t.TempDir(), "selection.toml"),
	})
	if err == nil {
		t.Fatal("unset source accepted without the required source-built bundle")
	}
	_ = payload
}

func TestPrepareMegaDriveSelectionRejectsClosedManifestFailures(t *testing.T) {
	t.Parallel()
	tests := map[string]func(string) string{
		"unknown field": func(manifest string) string { return manifest + "unknown = true\n" },
		"missing field": func(manifest string) string {
			return strings.Replace(manifest, "toolchain = 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'\n", "", 1)
		},
		"wrong abi": func(manifest string) string { return strings.Replace(manifest, "abi = 'mister'", "abi = 'other'", 1) },
		"wrong system": func(manifest string) string {
			return strings.Replace(manifest, "system = 'megadrive'", "system = 'snes'", 1)
		},
		"absolute artifact": func(manifest string) string {
			return strings.Replace(manifest, "artifact = 'megadrive.rbf'", "artifact = '/tmp/megadrive.rbf'", 1)
		},
		"escaping artifact": func(manifest string) string {
			return strings.Replace(manifest, "artifact = 'megadrive.rbf'", "artifact = '../megadrive.rbf'", 1)
		},
		"invalid repository": func(manifest string) string {
			return strings.Replace(manifest, "https://github.com/MiSTer-devel/MegaDrive_MiSTer", "http://example.test/core", 1)
		},
		"invalid revision": func(manifest string) string {
			return strings.Replace(manifest, "7365a137cfd8fa6f041e964d8b953159c0ec42d9", "7365a13", 1)
		},
		"invalid digest": func(manifest string) string {
			return strings.Replace(manifest, "recipe_sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'", "recipe_sha256 = 'bad'", 1)
		},
		"invalid size": func(manifest string) string {
			lines := strings.Split(manifest, "\n")
			for index, line := range lines {
				if strings.HasPrefix(line, "size = ") {
					lines[index] = "size = 0"
					break
				}
			}
			return strings.Join(lines, "\n")
		},
		"control character": func(manifest string) string {
			return strings.Replace(manifest, "toolchain = 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'", "toolchain = 'bad\\u0007toolchain'", 1)
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			payload := []byte("manifest validation")
			bundle := writeMegaDriveBundle(t, payload)
			manifestPath := filepath.Join(bundle, "megadrive-rbf.toml")
			original, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			broken := mutate(string(original))
			makeBundleWritable(t, bundle)
			if err := os.Chmod(manifestPath, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, []byte(broken), 0o444); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(bundle, 0o555); err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(t.TempDir(), "cache")
			if _, err := targetimage.PrepareMegaDriveSelection(targetimage.MegaDriveSelectionRequest{
				Source: "source-built",
				Bundle: bundle,
				Cache:  cache,
				Output: filepath.Join(cache, "selection.toml"),
			}); err == nil {
				t.Fatal("invalid bundle accepted")
			}
		})
	}
}

func TestPrepareMegaDriveSelectionRejectsWritableBundleInputs(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"megadrive-rbf.toml", "megadrive.rbf"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bundle := writeMegaDriveBundle(t, []byte("writable input"))
			makeBundleWritable(t, bundle)
			if err := os.Chmod(filepath.Join(bundle, name), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(bundle, 0o555); err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(t.TempDir(), "cache")
			if _, err := targetimage.PrepareMegaDriveSelection(targetimage.MegaDriveSelectionRequest{
				Source: "source-built",
				Bundle: bundle,
				Cache:  cache,
				Output: filepath.Join(cache, "selection.toml"),
			}); err == nil {
				t.Fatal("writable bundle input accepted")
			}
		})
	}
}

func TestPrepareMegaDriveSelectionRejectsSymlinksAndPreservesCache(t *testing.T) {
	t.Parallel()
	payload := []byte("symlink and preserve")
	bundle := writeMegaDriveBundle(t, payload)
	cache := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	old := []byte("old complete cache")
	oldPath := filepath.Join(cache, "megadrive.rbf")
	if err := os.WriteFile(oldPath, old, 0o444); err != nil {
		t.Fatal(err)
	}
	makeBundleWritable(t, bundle)
	if err := os.Remove(filepath.Join(bundle, "megadrive.rbf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.rbf"), filepath.Join(bundle, "megadrive.rbf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bundle, 0o555); err != nil {
		t.Fatal(err)
	}
	if _, err := targetimage.PrepareMegaDriveSelection(targetimage.MegaDriveSelectionRequest{
		Source: "source-built",
		Bundle: bundle,
		Cache:  cache,
		Output: filepath.Join(cache, "selection.toml"),
	}); err == nil {
		t.Fatal("symlink artifact accepted")
	}
	got, err := os.ReadFile(oldPath)
	if err != nil || string(got) != string(old) {
		t.Fatalf("old cache = %q, err = %v", got, err)
	}
}

func TestPrepareMegaDriveSelectionPreservesArtifactWhenSelectionTargetIsInvalid(t *testing.T) {
	t.Parallel()
	payload := []byte("new selection payload")
	bundle := writeMegaDriveBundle(t, payload)
	cache := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	oldArtifact := []byte("old cached artifact")
	if err := os.WriteFile(filepath.Join(cache, "megadrive.rbf"), oldArtifact, 0o444); err != nil {
		t.Fatal(err)
	}
	selectionPath := filepath.Join(t.TempDir(), "selection-target")
	if err := os.Mkdir(selectionPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := targetimage.PrepareMegaDriveSelection(targetimage.MegaDriveSelectionRequest{
		Source: "source-built",
		Bundle: bundle,
		Cache:  cache,
		Output: selectionPath,
	}); err == nil {
		t.Fatal("selection install unexpectedly succeeded")
	}
	if got, err := os.ReadFile(filepath.Join(cache, "megadrive.rbf")); err != nil || string(got) != string(oldArtifact) {
		t.Fatalf("cached artifact after failed pair install = %q, err = %v", got, err)
	}
	if info, err := os.Stat(selectionPath); err != nil || !info.IsDir() {
		t.Fatalf("selection target after failed pair install = %#v, err = %v", info, err)
	}
}

func TestPrepareMegaDriveSelectionUpstreamUsesLockedArtifact(t *testing.T) {
	t.Parallel()
	payload := []byte("locked upstream")
	digest := sha256.Sum256(payload)
	lockPath := filepath.Join(t.TempDir(), "native-runtime.inputs.lock.toml")
	lock := fmt.Sprintf(validMegaDriveUpstreamLock, hex.EncodeToString(digest[:]), len(payload))
	if err := os.WriteFile(lockPath, []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(t.TempDir(), "downloaded.rbf")
	if err := os.WriteFile(artifact, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "cache")
	selection, err := targetimage.PrepareMegaDriveSelection(targetimage.MegaDriveSelectionRequest{
		Source:       "upstream",
		Artifact:     artifact,
		UpstreamLock: lockPath,
		Cache:        cache,
		Output:       filepath.Join(cache, "megadrive.selection.toml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Origin != "upstream" || selection.Artifact != "releases/MegaDrive_20260603.rbf" || selection.Recipe != "" || selection.Toolchain != "" {
		t.Fatalf("selection = %#v", selection)
	}
	if got, err := os.ReadFile(filepath.Join(cache, "megadrive.rbf")); err != nil || string(got) != string(payload) {
		t.Fatalf("cached artifact = %q, err = %v", got, err)
	}
}

func TestPrepareMegaDriveSelectionRejectsMixedSourceArguments(t *testing.T) {
	t.Parallel()
	bundle := writeMegaDriveBundle(t, []byte("mixed source"))
	artifact := filepath.Join(t.TempDir(), "upstream.rbf")
	if err := os.WriteFile(artifact, []byte("mixed source"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := targetimage.MegaDriveSelectionRequest{
		Source:   "source-built",
		Bundle:   bundle,
		Artifact: artifact,
		Cache:    filepath.Join(t.TempDir(), "cache"),
		Output:   filepath.Join(t.TempDir(), "selection.toml"),
	}
	if _, err := targetimage.PrepareMegaDriveSelection(base); err == nil {
		t.Fatal("source-built accepted upstream artifact")
	}
	base.Source = "upstream"
	if _, err := targetimage.PrepareMegaDriveSelection(base); err == nil {
		t.Fatal("upstream accepted source-built bundle")
	}
	base.Source = "unsupported"
	base.Bundle = ""
	base.Artifact = ""
	if _, err := targetimage.PrepareMegaDriveSelection(base); err == nil {
		t.Fatal("unsupported source accepted")
	}
}

func writeMegaDriveBundle(t *testing.T, payload []byte) string {
	t.Helper()
	digest := sha256.Sum256(payload)
	bundle := t.TempDir()
	manifest := fmt.Sprintf(`format = 1
abi = 'mister'
system = 'megadrive'
artifact = 'megadrive.rbf'
sha256 = '%s'
size = %d
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
revision = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
recipe = 'scripts/rebuild_core.py'
recipe_sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
toolchain = 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'
`, hex.EncodeToString(digest[:]), len(payload))
	if err := os.WriteFile(filepath.Join(bundle, "megadrive.rbf"), payload, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "megadrive-rbf.toml"), []byte(manifest), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bundle, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bundle, 0o755) })
	return bundle
}

func makeBundleWritable(t *testing.T, bundle string) {
	t.Helper()
	if err := os.Chmod(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
}
