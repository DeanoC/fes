package bundledfirmware

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testManifest(id string, image []byte) Manifest {
	return Manifest{
		Format: Format, ID: id, Name: "Test ROM", Version: "1.0",
		CoreID: "fes.test", ROMID: "test-firmware", File: "test.rom",
		Size: int64(len(image)), SHA256: fmt.Sprintf("%x", sha256.Sum256(image)),
		Licence: "GPL-2.0-only", LicenceName: "GNU General Public License v2",
		LicenceFile: "LICENSE.txt", Notice: "Test notice.",
		Origin: Location{URL: "https://example.invalid/test.zip", SHA256: strings.Repeat("a", 64)},
		Source: &Location{URL: "https://example.invalid/test-src.tar.gz", SHA256: strings.Repeat("b", 64)},
	}
}

func writeObject(t *testing.T, root string, m Manifest, image []byte) string {
	t.Helper()
	dir := filepath.Join(root, m.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{ManifestName: raw, m.File: image} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if m.LicenceFile != "" {
		if err := os.WriteFile(filepath.Join(dir, m.LicenceFile), []byte("licence text"), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadReadsValidObject(t *testing.T) {
	root := t.TempDir()
	image := bytes.Repeat([]byte{0x5a}, 256)
	writeObject(t, root, testManifest("test-rom", image), image)
	got, problems := Load(root)
	if len(problems) != 0 || len(got) != 1 {
		t.Fatalf("load: %+v %v", got, problems)
	}
	if !bytes.Equal(got[0].Image, image) || got[0].LicenceText != "licence text" || got[0].Source == nil {
		t.Fatalf("loaded object %+v", got[0].Manifest)
	}
}

func TestLoadMissingDirectoryHoldsNothing(t *testing.T) {
	got, problems := Load(filepath.Join(t.TempDir(), "absent"))
	if got != nil || problems != nil {
		t.Fatalf("missing dir: %v %v", got, problems)
	}
}

func TestLoadRefusesBadObjectsAndKeepsGoodOnes(t *testing.T) {
	image := bytes.Repeat([]byte{0x11}, 64)
	for _, tc := range []struct {
		name   string
		change func(*Manifest)
		bytes  []byte
		want   string
	}{
		{name: "digest", change: func(m *Manifest) { m.SHA256 = strings.Repeat("c", 64) }, want: "SHA-256"},
		{name: "size", change: func(m *Manifest) { m.Size = 65 }, want: "bytes, manifest says"},
		{name: "short image", bytes: image[:10], want: "bytes, manifest says"},
		{name: "notice", change: func(m *Manifest) { m.Notice = " " }, want: "missing notice"},
		{name: "licence", change: func(m *Manifest) { m.Licence = "" }, want: "missing licence"},
		{name: "format", change: func(m *Manifest) { m.Format = 2 }, want: "format"},
		{name: "file", change: func(m *Manifest) { m.File = "../escape.rom" }, want: "image file"},
		{name: "origin", change: func(m *Manifest) { m.Origin.URL = "http://example.invalid/x" }, want: "origin url"},
		{name: "source", change: func(m *Manifest) { m.Source.SHA256 = "nope" }, want: "source sha256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			good := testManifest("a-good", image)
			writeObject(t, root, good, image)
			bad := testManifest("b-bad", image)
			bad.CoreID = "fes.other"
			if tc.change != nil {
				tc.change(&bad)
			}
			stored := image
			if tc.bytes != nil {
				stored = tc.bytes
			}
			dir := writeObject(t, root, bad, image)
			if bad.File == testManifest("", nil).File {
				if err := os.Remove(filepath.Join(dir, bad.File)); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, bad.File), stored, 0o444); err != nil {
					t.Fatal(err)
				}
			}
			got, problems := Load(root)
			if len(got) != 1 || got[0].ID != "a-good" {
				t.Fatalf("good object not kept: %+v", got)
			}
			if len(problems) != 1 || !strings.Contains(problems[0].Error(), tc.want) {
				t.Fatalf("problems %v, want %q", problems, tc.want)
			}
		})
	}
}

func TestLoadRefusesUnknownFieldsAndMismatchedID(t *testing.T) {
	image := []byte{1, 2, 3}
	root := t.TempDir()
	dir := writeObject(t, root, testManifest("extra", image), image)
	raw, _ := os.ReadFile(filepath.Join(dir, ManifestName))
	raw = append(raw[:len(raw)-1], []byte(`,"preinstalled":true}`)...)
	if err := os.Remove(filepath.Join(dir, ManifestName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), raw, 0o444); err != nil {
		t.Fatal(err)
	}
	moved := testManifest("moved", image)
	movedDir := writeObject(t, root, moved, image)
	if err := os.Rename(movedDir, filepath.Join(root, "renamed")); err != nil {
		t.Fatal(err)
	}
	got, problems := Load(root)
	if len(got) != 0 || len(problems) != 2 {
		t.Fatalf("got %+v problems %v", got, problems)
	}
}

func TestLoadRefusesSecondObjectForSameROM(t *testing.T) {
	image := []byte{9, 9}
	root := t.TempDir()
	writeObject(t, root, testManifest("first", image), image)
	writeObject(t, root, testManifest("second", image), image)
	got, problems := Load(root)
	if len(got) != 1 || got[0].ID != "first" || len(problems) != 1 || !strings.Contains(problems[0].Error(), "already bundled by first") {
		t.Fatalf("got %+v problems %v", got, problems)
	}
}

func TestLoadRefusesSymlinkedImage(t *testing.T) {
	image := []byte{4, 5, 6}
	root := t.TempDir()
	m := testManifest("linked", image)
	dir := writeObject(t, root, m, image)
	target := filepath.Join(t.TempDir(), "elsewhere.rom")
	if err := os.WriteFile(target, image, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, m.File)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, m.File)); err != nil {
		t.Fatal(err)
	}
	if got, problems := Load(root); len(got) != 0 || len(problems) != 1 || !strings.Contains(problems[0].Error(), "not a regular file") {
		t.Fatalf("got %+v problems %v", got, problems)
	}
}
