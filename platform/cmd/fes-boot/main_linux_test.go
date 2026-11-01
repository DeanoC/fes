//go:build linux

package main

import (
	"context"
	"encoding/json"
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

func TestUnqualifiedDe25TrialRefusedBeforeGuardStart(t *testing.T) {
	for _, board := range []string{release.BoardDE25, "", "other"} {
		if err := (linuxPlatform{board: board}).Arm(context.Background(), store.Selection{}); err == nil || !strings.Contains(err.Error(), "not qualified") {
			t.Fatalf("unqualified trial %s: %v", board, err)
		}
	}
	if err := trialSupported(release.Board); err != nil {
		t.Fatal(err)
	}
}
