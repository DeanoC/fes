package hostapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
			selected, err := service.CoreEntryROM(ctx, gameID)
			if err != nil || selected.MediaID != mediaID {
				t.Fatal(selected, err)
			}
		}()
	}
}
