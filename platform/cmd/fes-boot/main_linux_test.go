//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
)

func TestBootStoreUsesSelectedBoard(t *testing.T) {
	for _, board := range []string{release.Board, release.BoardDE25} {
		root := t.TempDir()
		path := filepath.Join(root, "factory.json")
		m := release.Manifest{Format: 1, Board: board, BootABI: release.BootABI, Version: "test",
			KernelSHA256: strings.Repeat("a", 64), ImageSHA256: strings.Repeat("b", 64), ImageSize: 4096,
			FESRevision: strings.Repeat("c", 40), FogCastRevision: strings.Repeat("d", 40), RuntimeRevision: strings.Repeat("e", 40)}
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0444); err != nil {
			t.Fatal(err)
		}
		s, factory, err := openStoreAt(path, filepath.Join(root, "releases"), board)
		if err != nil || s.Board() != board || factory.Board != board {
			t.Fatalf("selected %s: %v", board, err)
		}
		other := release.Board
		if board == other {
			other = release.BoardDE25
		}
		unwritten := filepath.Join(root, "foreign")
		if _, _, err := openStoreAt(path, unwritten, other); err == nil {
			t.Fatal("accepted factory for another board")
		}
		if _, err := os.Stat(unwritten); !os.IsNotExist(err) {
			t.Fatal("wrong-board factory modified release storage")
		}
	}
}

func TestUnknownTrialBoardRefusedBeforeGuardStart(t *testing.T) {
	for _, board := range []string{"", "other"} {
		if err := (linuxPlatform{board: board}).Arm(context.Background(), store.Selection{}); err == nil || !strings.Contains(err.Error(), "qualified") {
			t.Fatalf("unqualified trial %s: %v", board, err)
		}
	}
	for _, board := range []string{release.Board, release.BoardDE25} {
		if err := trialSupported(board); err != nil {
			t.Fatal(err)
		}
	}
}

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
