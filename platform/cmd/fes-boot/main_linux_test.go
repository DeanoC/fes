//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
)

func TestWriteTicketUsesLegacyStrictSchemaAndPersistsFallback(t *testing.T) {
	image := strings.Repeat("a", 64)
	fallback := strings.Repeat("b", 64)
	for _, tc := range []struct {
		name  string
		trial bool
	}{
		{name: "stable"},
		{name: "trial", trial: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "boot.json")
			choice := store.Selection{
				Manifest:            release.Manifest{ImageSHA256: image},
				Trial:               tc.trial,
				BootID:              "boot-a",
				FallbackImageSHA256: fallback,
			}
			if err := writeTicket(path, choice); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// This is the old target-agent schema: its decoder rejects every
			// field not present in this struct.
			var legacyTicket struct {
				BootID      *string `json:"boot_id"`
				ImageSHA256 *string `json:"image_sha256"`
				Trial       *bool   `json:"trial"`
			}
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			if err = decoder.Decode(&legacyTicket); err != nil {
				t.Fatalf("old strict parser rejected ticket %s: %v", body, err)
			}
			if err = decoder.Decode(&struct{}{}); err != io.EOF {
				t.Fatalf("old strict parser found trailing ticket data: %v", err)
			}
			if legacyTicket.BootID == nil || *legacyTicket.BootID != "boot-a" || legacyTicket.ImageSHA256 == nil || *legacyTicket.ImageSHA256 != image || legacyTicket.Trial == nil || *legacyTicket.Trial != tc.trial {
				t.Fatalf("legacy ticket = %+v", legacyTicket)
			}
			fallbackBody, err := os.ReadFile(filepath.Join(filepath.Dir(path), store.BootFallbackFilename))
			if err != nil {
				t.Fatal(err)
			}
			var record struct {
				BootID              string `json:"boot_id"`
				ImageSHA256         string `json:"image_sha256"`
				FallbackImageSHA256 string `json:"fallback_image_sha256"`
			}
			if err = json.Unmarshal(fallbackBody, &record); err != nil || record.BootID != "boot-a" || record.ImageSHA256 != image || record.FallbackImageSHA256 != fallback {
				t.Fatalf("fallback record = %+v from %s: %v", record, fallbackBody, err)
			}
		})
	}
}
