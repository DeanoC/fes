package corecatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, path string) string {
	t.Helper()
	root := t.TempDir()
	b := []byte("archive")
	sum := sha256.Sum256(b)
	if err := os.Mkdir(filepath.Join(root, "packages"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packages", "a.fcore"), b, 0600); err != nil {
		t.Fatal(err)
	}
	value := map[string]any{"version": 1, "source_id": "fes-first-party", "catalog_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "entries": []any{map[string]any{"core_id": "fes.sms", "label": "SMS", "system": "sms", "standing": "supported", "package_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "archive_path": path, "archive_sha256": hex.EncodeToString(sum[:]), "archive_size": len(b)}}}
	delete(value, "catalog_sha256")
	body, _ := json.Marshal(value)
	catalogSum := sha256.Sum256(body)
	value["catalog_sha256"] = hex.EncodeToString(catalogSum[:])
	data, _ := json.Marshal(value)
	file := filepath.Join(root, "catalog.json")
	os.WriteFile(file, data, 0600)
	return file
}
func TestCatalogVerifiedBytes(t *testing.T) {
	c, err := Load(fixture(t, "packages/a.fcore"))
	if err != nil {
		t.Fatal(err)
	}
	r, e, err := c.OpenPackage("fes.sms")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	if string(b) != "archive" || e.CoreID != "fes.sms" {
		t.Fatalf("%s %+v", b, e)
	}
}
func TestCatalogPathEscapeRejected(t *testing.T) {
	if _, err := Load(fixture(t, "../outside.fcore")); err == nil {
		t.Fatal("accepted path escape")
	}
}
func TestCatalogChangedArchiveRejected(t *testing.T) {
	file := fixture(t, "packages/a.fcore")
	c, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(filepath.Dir(file), "packages/a.fcore"), []byte("changed"), 0600)
	if _, _, err = c.OpenPackage("fes.sms"); err == nil {
		t.Fatal("accepted changed bytes")
	}
}
func TestCatalogDigestRejectsEditedMetadata(t *testing.T) {
	p := fixture(t, "packages/a.fcore")
	b, _ := os.ReadFile(p)
	b = bytes.Replace(b, []byte(`"SMS"`), []byte(`"Edited"`), 1)
	os.WriteFile(p, b, 0600)
	if _, err := Load(p); err == nil {
		t.Fatal("accepted changed catalog body")
	}
}
func TestCatalogReadsColecoAndPublishedAlias(t *testing.T) {
	for _, system := range []string{"coleco", "colecovision"} {
		c, err := Load(catalogSystem(t, system))
		if err != nil {
			t.Fatalf("system %q: %v", system, err)
		}
		if len(c.Entries) != 1 || c.Entries[0].CoreID != "fes.coleco" || c.Entries[0].System != "coleco" || c.Entries[0].Label != "ColecoVision" {
			t.Fatalf("system %q loaded as %+v", system, c.Entries)
		}
	}
}

func catalogSystem(t *testing.T, system string) string {
	t.Helper()
	root := t.TempDir()
	value := map[string]any{"version": 1, "source_id": "fes-first-party", "entries": []any{map[string]any{"core_id": "fes.coleco", "label": "ColecoVision", "system": system, "standing": "supported"}}}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	value["catalog_sha256"] = hex.EncodeToString(sum[:])
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "catalog.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestCatalogCanonicalUTF8MatchesPublisher(t *testing.T) {
	root := t.TempDir()
	value := map[string]any{"version": 1, "source_id": "fes-first-party", "entries": []any{map[string]any{"core_id": "fes.sms", "label": "é <&>\u2028 text", "system": "sms", "standing": "supported"}}, "catalog_sha256": "a9e528a7bde9a69169b0d8a11f3e768c6fa88600b7494daf39aa2c9915b933a9"}
	b, _ := json.Marshal(value)
	p := filepath.Join(root, "catalog.json")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
}
func TestCatalogSymlinkEscapeRejected(t *testing.T) {
	p := fixture(t, "packages/a.fcore")
	outside := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(outside, []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(filepath.Dir(p), "packages/a.fcore")
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, archive); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.OpenPackage("fes.sms"); err == nil {
		t.Fatal("followed escaping symlink")
	}
}
