package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func TestInstalledCoreSetupCreatesManifestROMAndMediaSelections(t *testing.T) {
	pkg, firmware, cartridge, tape := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)
	for _, tc := range []struct {
		name       string
		descriptor corepackage.Descriptor
		roms       map[string]string
		mediaRole  string
		mediaID    string
	}{
		{name: "romless", descriptor: corepackage.Descriptor{Core: corepackage.Core{ID: "fes.pong"}, ABI: corepackage.Contract{ID: "fes.simple-game", Major: 1}}},
		{name: "spectrum-cassette", descriptor: corepackage.Descriptor{Core: corepackage.Core{ID: "fes.spectrum128"}, ABI: corepackage.Contract{ID: "fes.computer", Major: 1}, ROM: &corepackage.ROM{ID: "spectrum-firmware", Role: "firmware", SourceSize: 32768}}, roms: map[string]string{"spectrum-firmware": firmware}, mediaRole: protocol.CassetteRole, mediaID: tape},
		{name: "coleco-bios-and-cartridge", descriptor: corepackage.Descriptor{Format: 4, Core: corepackage.Core{ID: "fes.coleco"}, ABI: corepackage.Contract{ID: "fes.application", Major: 1}, ROMs: []corepackage.ROMRequirement{{ID: "bios", Role: "firmware", SourceSize: 8192}, {ID: "cartridge", Role: "cartridge", SourceSize: 16384}}}, roms: map[string]string{"bios": firmware, "cartridge": cartridge}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			entry := CoreEntry{GameID: "my-machine", Title: "My machine", CoreID: tc.descriptor.Core.ID, PackageID: pkg, MediaRole: tc.mediaRole, MediaID: tc.mediaID, FirmwareRequired: len(tc.descriptor.ROMs) > 0}
			created, romWrites := false, 0
			binding := CoreEntryROMSelection{GameID: entry.GameID}
			capabilities := protocol.CoreMediaCapabilities{PackageID: pkg, Media: []protocol.CoreMediaCapability{{Role: tc.mediaRole, Format: "manifest-media", Extensions: []string{".tap", ".z80"}}}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/core-packages/" + pkg:
					json.NewEncoder(w).Encode(map[string]any{"package_id": pkg, "descriptor": tc.descriptor})
				case "/api/v1/core-packages/" + pkg + "/media-capabilities":
					json.NewEncoder(w).Encode(capabilities)
				case "/api/v1/core-packages":
					json.NewEncoder(w).Encode(map[string]any{"packages": []any{}})
				case "/api/v1/library/firmware":
					if r.Method != http.MethodGet {
						t.Error("setup changed household firmware")
						return
					}
					json.NewEncoder(w).Encode(CoreFirmware{Slot: "firmware", MediaID: firmware, Size: 8192})
				case "/api/v1/library/core-entries":
					if r.Method == http.MethodGet {
						rows := []CoreEntry{}
						if created {
							rows = append(rows, entry)
						}
						json.NewEncoder(w).Encode(map[string]any{"entries": rows})
						return
					}
					var request CoreEntry
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Title != entry.Title || request.PackageID != entry.PackageID || request.MediaID != entry.MediaID || request.MediaRole != entry.MediaRole || request.FirmwareRequired != entry.FirmwareRequired {
						t.Errorf("creation did not retain selections: %+v err=%v", request, err)
					}
					created = true
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(entry)
				case "/api/v1/library/core-entries/my-machine/rom":
					if r.Method == http.MethodPut {
						var request map[string]string
						json.NewDecoder(r.Body).Decode(&request)
						if request["expected_media_id"] != "" || request["media_id"] != tc.roms[request["rom_id"]] {
							t.Errorf("unsafe ROM replacement: %+v", request)
						}
						romWrites++
						binding = CoreEntryROMSelection{GameID: entry.GameID, PackageID: pkg, ROMID: request["rom_id"], MediaID: request["media_id"]}
					}
					json.NewEncoder(w).Encode(binding)
				default:
					t.Errorf("unexpected setup request %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusNotFound)
				}
			}))
			defer server.Close()
			client := NewClient(server.URL, server.Client())
			setup, err := client.InstalledCoreSetup(ctx, pkg)
			if err != nil || setup.CoreID != entry.CoreID || setup.Capabilities.Media[0].Extensions[1] != ".z80" {
				t.Fatalf("installed setup %+v err=%v", setup, err)
			}
			for i := 0; i < 2; i++ {
				got, err := client.CreateInstalledCoreEntry(ctx, setup, entry.Title, tc.roms, tc.mediaRole, tc.mediaID)
				if err != nil || got != entry {
					t.Fatalf("create/retry: %+v err=%v", got, err)
				}
			}
			wantWrites := 0
			if len(tc.roms) > 0 {
				wantWrites = 1
			}
			if romWrites != wantWrites {
				t.Fatalf("ROM writes %d; expected %d", romWrites, wantWrites)
			}
			if _, err := client.CreateInstalledCoreEntry(ctx, setup, entry.Title, tc.roms, "disk", strings.Repeat("e", 64)); err == nil {
				t.Fatal("overwrote existing media selection")
			}
		})
	}
}
