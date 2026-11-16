// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func downloadInstallerFixture(t *testing.T) (*programMenuFixture, *ProgramBackend, *ProgramInstall, map[string][]byte) {
	t.Helper()
	f, b, kept, _ := newProgramInstallFixture(t)
	payload, err := BuildStagedCatalog(context.Background(), stagingCatalogExport(t, f), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	// Installer delivery must work with no playable bootstrap catalog at all.
	delete(payload, "FES/CORESEL.JSN")
	delete(payload, "FES/UI.JSON")
	return f, b, kept, payload
}

func TestHTTPStagedCatalogInstallsBothRetainedCoresAndDiscoversRecords(t *testing.T) {
	f, b, _, payload := downloadInstallerFixture(t)
	var lock sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "installer.test" || r.Header.Get("Accept-Encoding") != "identity" {
			http.Error(w, "framing", 400)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/catalog/")
		lock.Lock()
		hits[name]++
		lock.Unlock()
		data, ok := payload[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	source, err := NewProgramSource(server.Client(), server.URL+"/catalog", "installer.test")
	if err != nil {
		t.Fatal(err)
	}
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	plans, err := source.DownloadStagedCatalog(context.Background(), b)
	if err != nil || len(plans) != 2 {
		t.Fatal(len(plans), err)
	}
	if calls != f.r.cpu.calls || opens != f.r.f.files.opens || len(f.r.f.files.installOperations) != 0 || len(b.catalog) != 0 {
		t.Fatal("download performed device IO or published a partial catalog")
	}
	server.Close()
	for _, data := range payload {
		if len(data) > 0 {
			data[0] ^= 1
		}
	} // Retained plans cannot depend on the export server or buffers.
	if err = b.AttachProgramInstalls(context.Background(), plans); err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	rows, err := client.AvailableCores(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if err = client.InstallAvailableCore(context.Background(), row.CoreReference); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"second", "first", "second"} {
		play, err := client.Launch(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
	}
	if f.s.Status().State != ProgramReady || f.r.f.lease.r.Active {
		t.Fatal("launch/return did not restore the menu")
	}
	restart := romProgramFixtureFor(t, NativeComputerProfile, false)
	for name, file := range f.r.f.files.files {
		copy := *file
		copy.Data = bytes.Clone(file.Data)
		restart.f.files.files[name] = &copy
	}
	entries, err := ReadInstalledPrograms(context.Background(), restart.f.store, nil)
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
	for _, entry := range entries {
		selection, err := restart.f.store.Retain(context.Background(), entry.Request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = restart.f.loader.Preflight(selection); err != nil {
			t.Fatal("fresh store rejected installed content", err)
		}
	}
	lock.Lock()
	defer lock.Unlock()
	if hits["FES/INSTALL.JSN"] != 1 || hits["FES/CORESEL.JSN"] != 0 || hits["FES/UI.JSON"] != 0 {
		t.Fatal("download depended on the playable bootstrap", hits)
	}
}

func TestHTTPStagedCatalogRejectsWholeBatchBeforePublicationOrDevices(t *testing.T) {
	for _, kind := range []string{"duplicate-id", "legacy-envelope", "checksum-map", "corrupt-second", "missing-second", "foreign-platform", "oversize", "redirect", "encoding", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f, b, kept, payload := downloadInstallerFixture(t)
			if err := b.AttachProgramInstall(context.Background(), kept); err != nil {
				t.Fatal(err)
			}
			var catalog stagedProgramCatalog
			if err := json.Unmarshal(payload["FES/INSTALL.JSN"], &catalog); err != nil {
				t.Fatal(err)
			}
			second := &catalog.Programs[1]
			switch kind {
			case "duplicate-id":
				second.ID = catalog.Programs[0].ID
			case "checksum-map":
				second.Files["FES/EXTRA.BIN"] = strings.Repeat("a", 64)
			case "foreign-platform":
				var metadata map[string]any
				if err := json.Unmarshal(payload[second.Program.MetadataPath], &metadata); err != nil {
					t.Fatal(err)
				}
				metadata["platform"].(map[string]any)["device"] = "foreign"
				raw, err := canonicalProgramRecord(metadata)
				if err != nil {
					t.Fatal(err)
				}
				payload[second.Program.MetadataPath] = raw
				sum := fmt.Sprintf("%x", sha256.Sum256(raw))
				second.Program.MetadataSHA256 = sum
				second.Files[second.Program.MetadataPath] = sum
			}
			raw, err := canonicalProgramRecord(catalog)
			if err != nil {
				t.Fatal(err)
			}
			payload["FES/INSTALL.JSN"] = raw
			if kind == "legacy-envelope" {
				payload["FES/INSTALL.JSN"] = []byte("{\"format\":1,\"scope\":\"nano-staged-program-install-v1\"}\n")
			}
			if kind == "corrupt-second" {
				payload[second.Program.Path] = bytes.Clone(payload[second.Program.Path])
				payload[second.Program.Path][0] ^= 1
			}
			if kind == "missing-second" {
				delete(payload, second.Program.MetadataPath)
			}
			if kind == "oversize" {
				payload["FES/INSTALL.JSN"] = bytes.Repeat([]byte("x"), 8193)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				name := strings.TrimPrefix(r.URL.Path, "/")
				if name == "FES/INSTALL.JSN" {
					if kind == "redirect" {
						http.Redirect(w, r, "http://127.0.0.1:1/other", 302)
						return
					}
					if kind == "encoding" {
						w.Header().Set("Content-Encoding", "gzip")
					}
				}
				if kind == "cancel" && name == second.Program.Path {
					cancel()
				}
				data, ok := payload[name]
				if !ok {
					http.NotFound(w, r)
					return
				}
				w.Write(data)
			}))
			defer server.Close()
			source, err := NewProgramSource(server.Client(), server.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			calls, opens := f.r.cpu.calls, f.r.f.files.opens
			plans, err := source.DownloadStagedCatalog(ctx, b)
			if err == nil || plans != nil {
				t.Fatal("invalid later content returned a partial batch", err)
			}
			if len(b.catalog) != 1 || b.catalog[0] != kept || len(b.entries) != 1 || calls != f.r.cpu.calls || opens != f.r.f.files.opens || len(f.r.f.files.installOperations) != 0 {
				t.Fatal("failed download published content or touched devices")
			}
		})
	}
}
