package hostapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/bundledfirmware"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type noCatalogTargetTransport struct{ t *testing.T }

func (n noCatalogTargetTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	n.t.Errorf("setup contacted target %s", r.URL)
	return nil, fmt.Errorf("target prohibited")
}

func TestCoreCatalogHostOnlyInstallSetupRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	publication := filepath.Join(root, "published")
	if err := os.Mkdir(publication, 0700); err != nil {
		t.Fatal(err)
	}
	// Canonical synthetic format-3 fixture; no real BIOS/game bytes or kit.
	var archive bytes.Buffer
	for _, m := range []struct{ name, path string }{{"manifest.toml", "manifests/valid-cartridge.toml"}, {"core.rbf", "payloads/fes-fixture.rbf"}, {"rom-map.json", "maps/valid-basic.json"}} {
		b, err := os.ReadFile("../../corepackage/testdata/core-bundle-v3/" + m.path)
		if err != nil {
			t.Fatal(err)
		}
		h := make([]byte, 512)
		copy(h, m.name)
		copy(h[100:], "0000644\x00")
		copy(h[108:], "0000000\x00")
		copy(h[116:], "0000000\x00")
		copy(h[124:], fmt.Sprintf("%011o\x00", len(b)))
		copy(h[136:], "00000000000\x00")
		copy(h[148:], "        ")
		h[156] = '0'
		copy(h[257:], "ustar\x00")
		copy(h[263:], "00")
		sum := 0
		for _, v := range h {
			sum += int(v)
		}
		copy(h[148:], fmt.Sprintf("%06o\x00 ", sum))
		archive.Write(h)
		archive.Write(b)
		archive.Write(make([]byte, (512-len(b)%512)%512))
	}
	archive.Write(make([]byte, 1024))
	archivePath := filepath.Join(publication, "core.fcore")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := corepackage.InspectPackage(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"version": 1, "source_id": "fes-first-party", "entries": []any{map[string]any{"core_id": p.Descriptor.Core.ID, "label": "Synthetic ROM fixture", "system": "pong", "standing": "supported", "package_id": p.PackageID, "archive_path": "core.fcore", "archive_sha256": fmt.Sprintf("%x", sha256.Sum256(archive.Bytes())), "archive_size": archive.Len()}, map[string]any{"core_id": "fes.apple2", "label": "Apple II", "system": "apple2", "standing": "experimental"}}}
	b, _ := json.Marshal(body)
	body["catalog_sha256"] = fmt.Sprintf("%x", sha256.Sum256(b))
	b, _ = json.Marshal(body)
	index := filepath.Join(publication, "catalog.json")
	if err := os.WriteFile(index, b, 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "config.toml")
	text := fmt.Sprintf("selected_target = \"none\"\nrequest_timeout_seconds = 15\nupload_timeout_seconds = 120\n[[targets]]\nname = \"none\"\nenabled = false\naddress = \"\"\nagent = \"\"\n[core_catalog]\npath = %q\nlibrary_source_id = \"private-fixture-library\"\n", index)
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	paths := fogcast.Paths{Config: config, Index: filepath.Join(root, "library.sqlite3"), Staging: filepath.Join(root, "staging"), CorePackages: filepath.Join(root, "installed")}
	gameID, mediaID := "", ""
	for cycle := 0; cycle < 2; cycle++ {
		service, err := fogcast.Open(ctx, paths, &http.Client{Transport: noCatalogTargetTransport{t}})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(hostapi.New(service))
		client := hostclient.NewClient(server.URL, server.Client())
		func() {
			defer server.Close()
			defer service.Close()
			rows, err := client.AvailableCores(ctx)
			if err != nil || len(rows) != 2 || rows[1].ArtifactState != "unproduced" || rows[1].Standing != "experimental" {
				t.Fatal(rows, err)
			}
			ref := rows[0].CoreReference
			if err := client.InstallAvailableCore(ctx, ref); err != nil {
				t.Fatal(err)
			}
			setup, err := client.CoreSetup(ctx, ref.SourceID, ref.CoreID, ref.PackageID)
			if err != nil || len(setup.ROMs) != 1 {
				t.Fatal(setup, err)
			}
			media, err := client.ImportCoreMedia(ctx, 1024, bytes.NewReader(make([]byte, 1024)))
			if err != nil {
				t.Fatal(err)
			}
			req := hostclient.CoreSetupRequest{CoreReference: ref, Title: "Synthetic title", ROMs: map[string]string{setup.ROMs[0].ID: media.MediaID}}
			for i := 0; i < 2; i++ {
				result, err := client.CreateCoreSetupEntry(ctx, req)
				if err != nil {
					t.Fatal(err)
				}
				if result.SourceID != "private-fixture-library" || result.PublicationSourceID != ref.SourceID {
					t.Fatal(result)
				}
				if gameID != "" && (gameID != result.Entry.GameID || mediaID != media.MediaID) {
					t.Fatal("restart or retry changed identity")
				}
				gameID = result.Entry.GameID
				mediaID = media.MediaID
			}
			selected, err := service.CoreEntryROM(ctx, gameID, setup.ROMs[0].ID)
			if err != nil || selected.MediaID != mediaID {
				t.Fatal(selected, err)
			}
		}()
	}
}

func TestInstalledSetupReplacesOnlyUnsavedBundledFirmware(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	bundleImage := bytes.Repeat([]byte{0x42}, 1024)
	bundleID := fmt.Sprintf("%x", sha256.Sum256(bundleImage))
	bundleDir := filepath.Join(root, "firmware", "spectrum-test")
	if err := os.MkdirAll(bundleDir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(bundledfirmware.Manifest{
		Format: bundledfirmware.Format, ID: "spectrum-test", Name: "Synthetic Spectrum default", Version: "1",
		CoreID: "fes.spectrum", ROMID: "spectrum-firmware", File: "firmware.bin", Size: int64(len(bundleImage)),
		SHA256: bundleID, Licence: "GPL-2.0-only", LicenceName: "Test licence", LicenceFile: "LICENSE.txt",
		Notice: "Synthetic test firmware.",
		Origin: bundledfirmware.Location{URL: "https://example.invalid/spectrum.zip", SHA256: strings.Repeat("a", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"manifest.json": manifest, "firmware.bin": bundleImage, "LICENSE.txt": []byte("test licence")} {
		if err := os.WriteFile(filepath.Join(bundleDir, name), data, 0400); err != nil {
			t.Fatal(err)
		}
	}

	archive := spectrumFirmwarePackage(t)
	paths := fogcast.Paths{
		Config: filepath.Join(root, "config.toml"), Index: filepath.Join(root, "library.sqlite3"),
		Staging: filepath.Join(root, "staging"), CorePackages: filepath.Join(root, "installed"),
	}
	packages, err := corepackage.NewStore(paths.CorePackages)
	if err != nil {
		t.Fatal(err)
	}
	inspection, _, err := packages.Import(ctx, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	targetID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-agent-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/health":
			_ = json.NewEncoder(w).Encode(protocol.Health{TargetID: targetID, APIVersion: "v1", AgentVersion: "test", Ready: true, BootID: strings.Repeat("b", 32)})
		case "GET /v1/kit/lease":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "free"})
		case "GET /v1/status":
			_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
		case "POST /v1/development/core/inspect":
			_ = json.NewEncoder(w).Encode(protocol.CoreInspection{PackageID: inspection.PackageID, Descriptor: inspection.Descriptor, Compatible: true})
		case "POST /v1/library/core/data/inspect":
			if r.Header.Get("X-FogCast-Package-ID") != inspection.PackageID {
				http.Error(w, "wrong package", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(protocol.CoreDataInspection{
				CoreData:   protocol.CoreData{PackageID: inspection.PackageID, CoreID: "fes.spectrum", Mode: "volatile", Revision: "absent", PaddleSpeed: protocol.PaddleNormal},
				Descriptor: inspection.Descriptor,
			})
		default:
			http.Error(w, "unexpected target request", http.StatusNotFound)
		}
	}))
	defer target.Close()
	config := fmt.Sprintf("selected_target = \"test\"\nrequest_timeout_seconds = 15\nupload_timeout_seconds = 120\n[[targets]]\nname = \"test\"\nenabled = true\naddress = %q\nagent = \"test-agent-token\"\ntarget_id = %q\n[firmware]\nbundled_dir = %q\n", target.URL, targetID, filepath.Join(root, "firmware"))
	if err := os.WriteFile(paths.Config, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fogcast.LoadConfig(paths.Config); err != nil {
		t.Fatalf("load test configuration: %v", err)
	}
	service, err := fogcast.Open(ctx, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(hostapi.New(service))
	defer func() {
		server.Close()
		_ = service.Close()
	}()
	client := hostclient.NewClient(server.URL, server.Client())
	setup, err := client.InstalledCoreSetup(ctx, inspection.PackageID)
	if err != nil || setup.CoreID != "fes.spectrum" || len(setup.ROMs) != 1 || setup.ROMs[0].ID != "spectrum-firmware" {
		t.Fatalf("installed setup %+v, err %v", setup, err)
	}
	userImage := bytes.Repeat([]byte{0x5a}, 1024)
	userROM, err := client.ImportCoreMedia(ctx, int64(len(userImage)), bytes.NewReader(userImage))
	if err != nil {
		t.Fatal(err)
	}
	legacyEntry, err := createEntryWithMediaIDOnlyClient(t, server.Client(), server.URL, setup, "Spectrum with media-id-only client", "spectrum-firmware", userROM.MediaID)
	if err != nil {
		t.Fatalf("client selecting a user ROM over the bundled default: %v", err)
	}
	entries := getCoreLibraryEntries(t, server.Client(), server.URL)
	if len(entries) != 1 || entries[0].GameID != legacyEntry.GameID || entries[0].Title != "Spectrum with media-id-only client" {
		t.Fatalf("ROM selection left extra library titles: %+v", entries)
	}
	legacyBinding := getInstalledROMBinding(t, server.Client(), server.URL, legacyEntry.GameID, "spectrum-firmware")
	if legacyBinding.MediaID != userROM.MediaID || legacyBinding.BundledDefault || legacyBinding.BundledMediaID != "" {
		t.Fatalf("legacy client saved ROM binding %+v", legacyBinding)
	}

	custom, err := client.CreateInstalledCoreEntry(ctx, setup, "Spectrum with own firmware", map[string]string{"spectrum-firmware": userROM.MediaID}, "", "")
	if err != nil {
		t.Fatalf("save user firmware over built-in default: %v", err)
	}
	customBinding := getInstalledROMBinding(t, server.Client(), server.URL, custom.GameID, "spectrum-firmware")
	if customBinding.MediaID != userROM.MediaID || customBinding.BundledDefault {
		t.Fatalf("user ROM binding %+v", customBinding)
	}
	if _, err := client.CreateInstalledCoreEntry(ctx, setup, "Spectrum with own firmware", map[string]string{"spectrum-firmware": strings.Repeat("c", 64)}, "", ""); err == nil {
		t.Fatal("overwrote a saved ROM choice")
	}

	withDefault, err := client.CreateInstalledCoreEntry(ctx, setup, "Spectrum with built-in firmware", map[string]string{"spectrum-firmware": bundleID}, "", "")
	if err != nil {
		t.Fatalf("keep built-in firmware: %v", err)
	}
	defaultBinding := getInstalledROMBinding(t, server.Client(), server.URL, withDefault.GameID, "spectrum-firmware")
	if defaultBinding.MediaID != "" || defaultBinding.BundledMediaID != bundleID || !defaultBinding.BundledDefault {
		t.Fatalf("built-in ROM was not returned as a derived default: %+v", defaultBinding)
	}

	server.Close()
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(paths.Index)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rawCustom, err := store.CoreEntryROM(ctx, custom.GameID, "spectrum-firmware")
	if err != nil || rawCustom.MediaID != userROM.MediaID {
		t.Fatalf("user ROM was not saved: %+v, err %v", rawCustom, err)
	}
	rawLegacy, err := store.CoreEntryROM(ctx, legacyEntry.GameID, "spectrum-firmware")
	if err != nil || rawLegacy.MediaID != userROM.MediaID {
		t.Fatalf("legacy client user ROM was not saved: %+v, err %v", rawLegacy, err)
	}
	rawDefault, err := store.CoreEntryROM(ctx, withDefault.GameID, "spectrum-firmware")
	if err != nil || rawDefault.MediaID != "" {
		t.Fatalf("built-in ROM was saved: %+v, err %v", rawDefault, err)
	}
}

// createEntryWithMediaIDOnlyClient checks a client that treats media_id as the
// saved ROM selection while requesting the named ROM explicitly.
func createEntryWithMediaIDOnlyClient(t *testing.T, client *http.Client, baseURL string, setup hostclient.CoreSetup, title, romID, mediaID string) (hostclient.CoreEntry, error) {
	t.Helper()
	requestBody, err := json.Marshal(map[string]any{
		"title": title, "package_id": setup.PackageID, "firmware_required": true,
	})
	if err != nil {
		return hostclient.CoreEntry{}, err
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/library/core-entries", bytes.NewReader(requestBody))
	if err != nil {
		return hostclient.CoreEntry{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return hostclient.CoreEntry{}, err
	}
	var entry hostclient.CoreEntry
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		return entry, fmt.Errorf("create title returned %s: %s", response.Status, body)
	}
	err = json.NewDecoder(response.Body).Decode(&entry)
	response.Body.Close()
	if err != nil {
		return entry, err
	}

	request, err = http.NewRequest(http.MethodGet, baseURL+"/api/v1/library/core-entries/"+entry.GameID+"/rom?rom_id="+url.QueryEscape(romID), nil)
	if err != nil {
		return entry, err
	}
	response, err = client.Do(request)
	if err != nil {
		return entry, err
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return entry, err
	}
	if response.StatusCode != http.StatusOK {
		return entry, fmt.Errorf("read ROM selection returned %s: %s", response.Status, body)
	}
	var shape struct {
		MediaID        *string `json:"media_id"`
		BundledMediaID string  `json:"bundled_media_id"`
		BundledDefault bool    `json:"bundled_default"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return entry, err
	}
	if shape.MediaID == nil || *shape.MediaID != "" || shape.BundledMediaID == "" || !shape.BundledDefault {
		return entry, fmt.Errorf("unexpected bundled ROM response: %s", body)
	}

	// Apply the saved-choice check to media_id, excluding bundled defaults.
	var binding struct {
		GameID    string `json:"game_id"`
		PackageID string `json:"package_id"`
		ROMID     string `json:"rom_id"`
		MediaID   string `json:"media_id"`
	}
	if err := json.Unmarshal(body, &binding); err != nil {
		return entry, err
	}
	if binding.GameID != entry.GameID || binding.PackageID != "" && binding.PackageID != entry.PackageID {
		return entry, fmt.Errorf("ROM selection identity changed")
	}
	if binding.MediaID != "" {
		if binding.ROMID != romID || binding.MediaID != mediaID {
			return entry, fmt.Errorf("the existing ROM differs; choose another title")
		}
		return entry, nil
	}

	selectionBody, err := json.Marshal(map[string]string{
		"package_id": entry.PackageID, "rom_id": romID,
		"expected_media_id": binding.MediaID, "media_id": mediaID,
	})
	if err != nil {
		return entry, err
	}
	request, err = http.NewRequest(http.MethodPut, baseURL+"/api/v1/library/core-entries/"+entry.GameID+"/rom", bytes.NewReader(selectionBody))
	if err != nil {
		return entry, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		return entry, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return entry, fmt.Errorf("save ROM returned %s: %s", response.Status, body)
	}
	return entry, nil
}

func getCoreLibraryEntries(t *testing.T, client *http.Client, baseURL string) []catalog.CoreEntry {
	t.Helper()
	response, err := client.Get(baseURL + "/api/v1/library/core-entries")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET core library returned %s", response.Status)
	}
	var result struct {
		Entries []catalog.CoreEntry `json:"entries"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result.Entries
}

func spectrumFirmwarePackage(t *testing.T) []byte {
	t.Helper()
	manifest, err := os.ReadFile("../../corepackage/testdata/core-bundle-v3/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte(`id = "fes.pong"`), []byte(`id = "fes.spectrum"`), 1)
	manifest = bytes.Replace(manifest, []byte(`id = "machine-rom"`), []byte(`id = "spectrum-firmware"`), 1)
	var members = []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}}
	for _, name := range []string{"payloads/fes-fixture.rbf", "maps/valid-basic.json"} {
		data, err := os.ReadFile("../../corepackage/testdata/core-bundle-v3/" + name)
		if err != nil {
			t.Fatal(err)
		}
		archiveName := filepath.Base(name)
		if strings.HasSuffix(name, ".rbf") {
			archiveName = "core.rbf"
		} else {
			archiveName = "rom-map.json"
		}
		members = append(members, struct {
			name string
			data []byte
		}{archiveName, data})
	}
	var out bytes.Buffer
	for _, member := range members {
		header := make([]byte, 512)
		copy(header, member.name)
		copy(header[100:], "0000644\x00")
		copy(header[108:], "0000000\x00")
		copy(header[116:], "0000000\x00")
		copy(header[124:], fmt.Sprintf("%011o\x00", len(member.data)))
		copy(header[136:], "00000000000\x00")
		copy(header[148:], "        ")
		header[156] = '0'
		copy(header[257:], "ustar\x00")
		copy(header[263:], "00")
		checksum := 0
		for _, value := range header {
			checksum += int(value)
		}
		copy(header[148:], fmt.Sprintf("%06o\x00 ", checksum))
		out.Write(header)
		out.Write(member.data)
		out.Write(make([]byte, (512-len(member.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

func getInstalledROMBinding(t *testing.T, client *http.Client, baseURL, gameID, romID string) hostclient.CoreEntryROMSelection {
	t.Helper()
	response, err := client.Get(baseURL + "/api/v1/library/core-entries/" + gameID + "/rom?rom_id=" + url.QueryEscape(romID))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET ROM binding returned %s", response.Status)
	}
	var binding hostclient.CoreEntryROMSelection
	if err := json.NewDecoder(response.Body).Decode(&binding); err != nil {
		t.Fatal(err)
	}
	return binding
}
