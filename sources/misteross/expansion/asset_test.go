package expansion

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func assetFixture(t *testing.T) (Shell, Asset) {
	t.Helper()
	shell, cart := fixtures()
	base := Shell{PackageID: strings.Repeat("a", 64), BuildID: strings.Repeat("b", 32), Payload: shell, Slot: Slot, SlotMajor: 1}
	manifest := Manifest{CartSHA256: hash(cart), CartSize: int64(len(cart)), Device: Device, Format: 1, Map: Map, RecipeSHA256: strings.Repeat("c", 64), Revision: strings.Repeat("d", 40), ShellBuildID: base.BuildID, ShellPackageID: base.PackageID, ShellSHA256: hash(shell), Slot: Slot, SlotMajor: 1}
	asset, err := NewAsset(manifest, cart)
	if err != nil {
		t.Fatal(err)
	}
	return base, asset
}
func TestAssetRoundTripAndComposition(t *testing.T) {
	shell, asset := assetFixture(t)
	var buf bytes.Buffer
	if err := asset.Write(&buf); err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadAsset(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != asset.ID || !bytes.Equal(decoded.Cart, asset.Cart) {
		t.Fatal("roundtrip changed asset")
	}
	composition, payload, err := Compose(shell, decoded)
	if err != nil {
		t.Fatal(err)
	}
	if composition.PayloadSHA256 != hash(payload) || composition.PackageID != shell.PackageID || composition.ExpansionID != asset.ID {
		t.Fatal("composition lost identity")
	}
	t.Logf("manifest=%s\nexpansion_id=%s", asset.ManifestBytes, asset.ID)
	encoded, _ := json.Marshal(composition)
	t.Logf("composition=%s", encoded)
}
func TestAssetAdmissionRejectsChangedInputs(t *testing.T) {
	shell, asset := assetFixture(t)
	for name, change := range map[string]func(*Shell){
		"package": func(s *Shell) { s.PackageID = strings.Repeat("e", 64) },
		"build":   func(s *Shell) { s.BuildID = strings.Repeat("e", 32) },
		"payload": func(s *Shell) { s.Payload = bytes.Clone(s.Payload); s.Payload[0] ^= 1 },
		"slot":    func(s *Shell) { s.Slot = "other" },
		"version": func(s *Shell) { s.SlotMinor = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			modified := shell
			change(&modified)
			if Admit(modified, asset) == nil {
				t.Fatal("accepted changed shell")
			}
		})
	}
	asset.Cart[0] ^= 1
	if Admit(shell, asset) == nil {
		t.Fatal("accepted changed cart")
	}
}
func TestAssetArchiveRejectsMalformedInput(t *testing.T) {
	_, asset := assetFixture(t)
	archive := func(manifest []byte, duplicate, unknown bool) []byte {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		entries := []struct {
			name string
			data []byte
		}{{"manifest.json", manifest}, {"cart.rbf", asset.Cart}}
		if duplicate {
			entries = append(entries, entries[0])
		}
		if unknown {
			entries = append(entries, struct {
				name string
				data []byte
			}{"extra", []byte("x")})
		}
		for _, e := range entries {
			if err := tw.WriteHeader(&tar.Header{Name: e.name, Size: int64(len(e.data)), Mode: 0600}); err != nil {
				t.Fatal(err)
			}
			tw.Write(e.data)
		}
		tw.Close()
		return buf.Bytes()
	}
	canonical := asset.ManifestBytes
	wrongDigest := bytes.Replace(canonical, []byte(asset.Manifest.CartSHA256), []byte(strings.Repeat("0", 64)), 1)
	wrongSize := asset.Manifest
	wrongSize.CartSize++
	sizeJSON, _ := json.Marshal(wrongSize)
	unknownJSON := append([]byte(`{"unknown":1,`), canonical[1:]...)
	duplicateJSON := append([]byte(`{"format":1,`), canonical[1:]...)
	valid := archive(canonical, false, false)
	cases := map[string][]byte{
		"whitespace":       archive(append(bytes.Clone(canonical), '\n'), false, false),
		"malformed":        archive([]byte("{"), false, false),
		"unknown-json":     archive(unknownJSON, false, false),
		"duplicate-json":   archive(duplicateJSON, false, false),
		"duplicate-member": archive(canonical, true, false),
		"unknown-member":   archive(canonical, false, true),
		"digest":           archive(wrongDigest, false, false), "size": archive(sizeJSON, false, false),
		"truncated": valid[:1024], "trailing": append(bytes.Clone(valid), 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadAsset(bytes.NewReader(input)); err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
}
