//go:build !tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func TestSpectrumSelectionUsesRetainedFirmwareReceipt(t *testing.T) {
	fixtures := "../../../../../sources/FogCast/corepackage/testdata/core-bundle-v3"
	rom := bytes.Repeat([]byte{0x15, 0x25}, 8192)
	romPath := filepath.Join(t.TempDir(), "firmware.rom")
	if err := os.WriteFile(romPath, rom, 0600); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{spectrum48Profile, "de25-linux-computer-v1"} {
		t.Run(profile, func(t *testing.T) {
			dir := t.TempDir()
			for dest, name := range map[string]string{
				"manifest.toml": "manifests/valid-nano-upload-16384.toml",
				"core.rbf":      "payloads/fes-fixture.rbf",
				"rom-map.json":  "maps/valid-nano-upload-16384.json",
			} {
				data, err := os.ReadFile(filepath.Join(fixtures, name))
				if err != nil {
					t.Fatal(err)
				}
				if dest == "manifest.toml" {
					data = []byte(strings.NewReplacer("fes.pong", "fes.spectrum", "fes-gp-v1", profile,
						"fes.simple-game", "fes.computer").Replace(string(data)))
				}
				if err := os.WriteFile(filepath.Join(dir, dest), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			payload, err := corepackage.CanonicalArchive(dir)
			if err != nil {
				t.Fatal(err)
			}
			archive, err := corepackage.RetainArchive(t.Context(), int64(len(payload)), bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "spectrum.fcore")
			if err := os.WriteFile(path, payload, 0600); err != nil {
				t.Fatal(err)
			}
			inspection := archive.Inspection()
			args := []string{path, romPath, inspection.PackageID}
			var output bytes.Buffer
			if err := run(t.Context(), args, &output); err == nil || output.Len() != 0 {
				t.Fatal("held-ROM mode accepted a mailbox firmware package")
			}
			spectrum := append([]string{"--spectrum48"}, args...)
			err = run(t.Context(), spectrum, &output)
			if profile != spectrum48Profile {
				if err == nil || output.Len() != 0 {
					t.Fatal("Spectrum mode accepted the Linux programming profile")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				SourceSHA256 string `json:"source_sha256"`
				Receipt      string `json:"rom_receipt_sha256"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			linked, err := archive.LinkROMs(t.Context(), []corepackage.ROMSource{{
				ID: inspection.Descriptor.ROM.ID, Role: "firmware", Data: rom,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if result.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(rom)) || result.Receipt != linked.ReceiptSHA256() {
				t.Fatal("host selection differs from the retained linker")
			}
			spectrum[3] = strings.Repeat("0", 64)
			output.Reset()
			if err := run(t.Context(), spectrum, &output); err == nil || output.Len() != 0 {
				t.Fatal("Spectrum accepted a different package ID")
			}
		})
	}
}
