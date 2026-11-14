package fogcast

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func TestCreateCoreEntryWithCartridge(t *testing.T) {
	for _, tc := range []struct {
		name             string
		archive          func(*testing.T) []byte
		size             int
		romID            string
		firmwareRequired bool
	}{
		{"format3", func(t *testing.T) []byte { return libraryROMPackageFixture(t, "cartridge") }, 1024, "machine-rom", false},
		{"format4", twoROMLibraryPackageFixture, 131072, "coleco-cart", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, _, _, inspection := newCoreEntryLaunchFixture(t, tc.archive(t), "Existing title", 2*time.Minute)
			rom := bytes.Repeat([]byte{0x5a}, tc.size)
			media, _, err := s.ImportCoreMedia(ctx, int64(len(rom)), bytes.NewReader(rom))
			if err != nil {
				t.Fatal(err)
			}
			entry, err := s.CreateCoreMediaEntry(ctx, "Cartridge title", inspection.PackageID, "cartridge", media.MediaID)
			if err != nil {
				t.Fatal(err)
			}
			if entry.MediaID != "" || entry.MediaRole != "" || entry.FirmwareRequired != tc.firmwareRequired {
				t.Fatalf("cartridge was stored as live media or lost firmware requirement: %+v", entry)
			}
			binding, got, err := s.readCoreEntryROM(ctx, entry, inspection.Descriptor)
			if err != nil || binding.ROMID != tc.romID || binding.MediaID != media.MediaID || !bytes.Equal(got, rom) {
				t.Fatalf("named ROM binding = %+v, bytes=%d, err=%v", binding, len(got), err)
			}
			readiness, err := s.CoreCompositions(ctx, []string{entry.GameID})
			if err != nil || !readiness[entry.GameID].ROMReady {
				t.Fatalf("cartridge is not ready: %+v, %v", readiness[entry.GameID], err)
			}
			if tc.firmwareRequired && (!readiness[entry.GameID].FirmwareRequired || readiness[entry.GameID].FirmwareReady) {
				t.Fatalf("cartridge incorrectly satisfied the separate BIOS requirement: %+v", readiness[entry.GameID])
			}
			before, err := s.CoreEntries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			wrong, _, err := s.ImportCoreMedia(ctx, 1, strings.NewReader("x"))
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{wrong.MediaID, strings.Repeat("0", 64)} {
				if _, err := s.CreateCoreMediaEntry(ctx, "Rejected cartridge", inspection.PackageID, "cartridge", id); err == nil {
					t.Fatalf("accepted invalid cartridge %s", id)
				}
				after, err := s.CoreEntries(ctx)
				if err != nil || len(after) != len(before) {
					t.Fatalf("invalid cartridge left a title: %+v, %v", after, err)
				}
			}
		})
	}
}

func TestCreateCartridgeRejectsFirmwareROM(t *testing.T) {
	ctx := context.Background()
	s, _, _, inspection := newCoreEntryLaunchFixture(t, libraryROMPackageFixture(t, "firmware"), "Existing firmware", time.Minute)
	rom := bytes.Repeat([]byte{0x5a}, 1024)
	media, _, err := s.ImportCoreMedia(ctx, int64(len(rom)), bytes.NewReader(rom))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCoreMediaEntry(ctx, "Not a cartridge", inspection.PackageID, "cartridge", media.MediaID); err == nil {
		t.Fatal("accepted firmware as cartridge")
	}
}

func TestCreateCartridgeRejectsFirmwareRequirement(t *testing.T) {
	ctx := context.Background()
	s, _, _, inspection := newCoreEntryLaunchFixture(t, libraryROMPackageFixture(t, "cartridge"), "Existing cartridge", time.Minute)
	rom := bytes.Repeat([]byte{0x5a}, 1024)
	media, _, err := s.ImportCoreMedia(ctx, int64(len(rom)), bytes.NewReader(rom))
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.CoreEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateCoreEntryWithFirmware(ctx, "Unready cartridge", inspection.PackageID, "cartridge", media.MediaID, true)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("firmware-required cartridge error = %v, want BAD_REQUEST", err)
	}
	after, err := s.CoreEntries(ctx)
	if err != nil || len(after) != len(before) {
		t.Fatalf("firmware-required cartridge left a title: before=%+v after=%+v err=%v", before, after, err)
	}
}
