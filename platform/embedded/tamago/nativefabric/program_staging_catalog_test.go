// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func stagingCatalogExport(t *testing.T, f *programMenuFixture) fstest.MapFS {
	t.Helper()
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	first := programExport(t, "first", request, programUpload(f.r.f.files, request))
	request, _, _ = addROMProgramChanged(t, f.r, 0, 8192, func(raw string) string {
		return strings.Replace(raw, `version = "0.1.0"`, `version = "0.2.0"`, 1)
	})
	files := programUpload(f.r.f.files, request)
	for _, pair := range [][2]string{{request.Path, "FES/ALT.FCP"}, {request.MetadataPath, "FES/ALT.JSN"}, {request.Sources[0].Path, "FES/ALT.BIN"}} {
		files[pair[1]] = files[pair[0]]
		delete(files, pair[0])
	}
	request.Path, request.MetadataPath, request.Sources[0].Path = "FES/ALT.FCP", "FES/ALT.JSN", "FES/ALT.BIN"
	second := programExport(t, "second", request, files)
	var a, b programChoices
	if err := json.Unmarshal(first["FES/CORESEL.JSN"], &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second["FES/CORESEL.JSN"], &b); err != nil {
		t.Fatal(err)
	}
	a.Programs = append(a.Programs, b.Programs...)
	first["FES/CORESEL.JSN"] = programJSON(t, a)
	first["FES/UI.JSON"] = []byte("{\"entries\":[{\"id\":\"first\",\"program\":0,\"title\":\"First\"},{\"id\":\"second\",\"program\":1,\"title\":\"Second\"}],\"format\":1,\"scope\":\"nano-tenfoot-catalog-v1\"}\n")
	for name, data := range second {
		if name != "FES/CORESEL.JSN" && name != "FES/UI.JSON" {
			first[name] = data
		}
	}
	result := fstest.MapFS{}
	for name, data := range first {
		result[name] = &fstest.MapFile{Data: data}
	}
	return result
}

func TestProgramStagedCatalogProducerFATInstallAndReboot(t *testing.T) {
	image := filepath.Join(t.TempDir(), "catalog.img")
	file, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if raw, err := exec.Command("mformat", "-i", image, "-F", "::").CombinedOutput(); err != nil {
		t.Fatal(string(raw), err)
	}
	disk, err := block.OpenFile(image, true)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	var staged []*StagedProgram
	var entries []ProgramEntry
	f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
		export := stagingCatalogExport(t, f)
		payload, err := BuildStagedCatalog(context.Background(), export, []string{"first", "second"})
		if err != nil {
			t.Fatal(err)
		}
		if len(payload["FES/INSTALL.JSN"]) > 8192 {
			t.Fatal("control bound exceeded")
		}
		volume, err := fat.Mount(&ownedInstallDisk{disk, f.r.f.lease})
		if err != nil {
			t.Fatal(err)
		}
		merged := map[string][]byte{}
		for name, item := range f.r.f.files.files {
			if !item.Mode.IsDir() {
				merged[name] = item.Data
			}
		}
		for name, data := range payload {
			merged[name] = data
		}
		var names []string
		for name := range merged {
			names = append(names, name)
		}
		sort.Strings(names)
		dirs := map[string]bool{".": true}
		for _, name := range names {
			prefix := ""
			for _, part := range strings.Split(path.Dir(name), "/") {
				if part == "." {
					continue
				}
				if prefix != "" {
					prefix += "/"
				}
				prefix += part
				if !dirs[prefix] {
					if err = volume.Mkdir(context.Background(), prefix); err != nil {
						t.Fatal(err)
					}
					dirs[prefix] = true
				}
			}
			if err = volume.CreateFile(context.Background(), name, merged[name]); err != nil {
				t.Fatal(err)
			}
		}
		f.r.f.store, err = packagestore.New(volume, f.r.f.lease)
		if err != nil {
			t.Fatal(err)
		}
		entries, err = ReadInstallerBootstrap(context.Background(), f.r.f.store)
		if err != nil || len(entries) != 0 {
			t.Fatal(entries, err)
		}
		staged, err = ReadStagedPrograms(context.Background(), f.r.f.store, "FES/INSTALL.JSN")
		if err != nil || len(staged) != 2 {
			t.Fatal(len(staged), err)
		}
	})
	backend, err := NewProgramBackend(f.s, entries)
	if err != nil {
		t.Fatal(err)
	}
	if err = backend.AttachStagedPrograms(context.Background(), staged); err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: backend})
	rows, err := client.AvailableCores(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, i := range []int{1, 0} {
		if err = client.InstallAvailableCore(context.Background(), rows[i].CoreReference); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"first", "second", "first"} {
		play, err := client.Launch(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
	}
	// A separate store, loader and service discover both installed records with
	// no optional staging read and no catalog attachment or reinstall.
	restart := newProgramMenuFixtureContext(t, context.Background(), "", func(next *programMenuFixture) {
		volume, err := fat.Mount(&ownedInstallDisk{disk, next.r.f.lease})
		if err != nil {
			t.Fatal(err)
		}
		next.r.f.store, err = packagestore.New(volume, next.r.f.lease)
		if err != nil {
			t.Fatal(err)
		}
		entries, err = ReadInstallerBootstrap(context.Background(), next.r.f.store)
		if err != nil || len(entries) != 2 {
			t.Fatal(entries, err)
		}
	})
	next, err := NewProgramBackend(restart.s, entries)
	if err != nil {
		t.Fatal(err)
	}
	client = hostclient.NewClient("http://native.invalid", &http.Client{Transport: next})
	for _, id := range []string{"second", "first"} {
		play, err := client.Launch(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
	}
	if f.r.f.lease.r.Active || restart.r.f.lease.r.Active {
		t.Fatal("menu left SD active")
	}
	for _, plan := range backend.catalog {
		got, err := exec.Command("mtype", "-i", image, "::"+plan.entry.Request.Path).Output()
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(got)) != strings.TrimSuffix(path.Base(plan.entry.Request.Path), ".BIN") {
			t.Fatal("independent archive readback differs", err)
		}
	}
}

func TestProgramStagedCatalogRejectsWholeBatchWithoutPublication(t *testing.T) {
	for _, fault := range []string{"missing-member", "missing-catalog", "second-digest", "duplicate-id", "duplicate-package", "empty", "old-schema", "unknown", "oversize", "preflight"} {
		t.Run(fault, func(t *testing.T) {
			var sources []*StagedProgram
			var stageErr error
			f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
				export := stagingCatalogExport(t, f)
				payload, err := BuildStagedCatalog(context.Background(), export, []string{"first", "second"})
				if err != nil {
					t.Fatal(err)
				}
				var catalog stagedProgramCatalog
				if err = json.Unmarshal(payload["FES/INSTALL.JSN"], &catalog); err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "second-digest":
					catalog.Programs[1].Files[catalog.Programs[1].Program.Path] = strings.Repeat("0", 64)
				case "duplicate-id":
					catalog.Programs[1].ID = catalog.Programs[0].ID
				case "duplicate-package":
					catalog.Programs[1].Program.PackageID = catalog.Programs[0].Program.PackageID
				case "empty":
					catalog.Programs = []stagedProgramRecord{}
				case "old-schema":
					catalog.Scope = "nano-staged-program-install-v1"
				case "preflight":
					name := catalog.Programs[1].Program.MetadataPath
					var record selectedROMProgramRecord
					if err = json.Unmarshal(payload[name], &record); err != nil {
						t.Fatal(err)
					}
					record.Platform.Major++
					payload[name] = programJSON(t, record)
					sum := fmt.Sprintf("%x", sha256.Sum256(payload[name]))
					catalog.Programs[1].Program.MetadataSHA256 = sum
					catalog.Programs[1].Files[name] = sum
				}
				raw := programJSON(t, catalog)
				if fault == "unknown" {
					raw = []byte(strings.Replace(string(raw), `"format":1`, `"extra":true,"format":1`, 1))
				}
				if fault == "oversize" {
					raw = []byte(strings.Repeat(" ", 8193))
				}
				payload["FES/INSTALL.JSN"] = raw
				if fault == "missing-member" {
					delete(payload, catalog.Programs[1].Program.Path)
					delete(f.r.f.files.files, catalog.Programs[1].Program.Path)
				}
				if fault == "missing-catalog" {
					delete(payload, "FES/INSTALL.JSN")
				}
				for name, data := range payload {
					f.r.f.files.files[name] = &fstest.MapFile{Data: data}
				}
				calls, writes := f.r.cpu.calls, len(f.r.f.files.installOperations)
				sources, stageErr = ReadStagedPrograms(context.Background(), f.r.f.store, "FES/INSTALL.JSN")
				if calls != f.r.cpu.calls || writes != len(f.r.f.files.installOperations) {
					t.Fatal("staging mutated devices")
				}
				if _, err = f.r.f.store.ActiveReceipt(); err != nil {
					t.Fatal("content refusal lost healthy storage", err)
				}
			})
			b, err := NewProgramBackend(f.s, nil)
			if err != nil {
				t.Fatal(err)
			}
			if (fault == "missing-catalog") != errors.Is(stageErr, ErrStagedCatalogAbsent) {
				t.Fatal("catalog absence confused with missing member", stageErr)
			}
			if fault == "preflight" {
				if stageErr != nil || len(sources) != 2 {
					t.Fatal(stageErr)
				}
				if err = b.AttachStagedPrograms(context.Background(), sources); err == nil {
					t.Fatal("invalid later platform accepted")
				}
			} else if stageErr == nil || sources != nil {
				t.Fatal("invalid source returned partial catalog", len(sources), stageErr)
			}
			if len(b.catalog) != 0 || b.catalogBytes != 0 {
				t.Fatal("invalid batch advertised an earlier source")
			}
		})
	}
}

func TestProgramStagingProducerRefusesUnknownDuplicateAndCorruptSources(t *testing.T) {
	f := newProgramMenuFixture(t, "")
	for _, fault := range []string{"unknown", "duplicate-id", "duplicate-package", "corrupt", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			export := stagingCatalogExport(t, f)
			ids := []string{"first", "second"}
			ctx := context.Background()
			switch fault {
			case "unknown":
				ids[1] = "missing"
			case "duplicate-id":
				ids[1] = "first"
			case "duplicate-package":
				var choices programChoices
				if err := json.Unmarshal(export["FES/CORESEL.JSN"].Data, &choices); err != nil {
					t.Fatal(err)
				}
				choices.Programs[1] = choices.Programs[0]
				export["FES/CORESEL.JSN"].Data = programJSON(t, choices)
			case "corrupt":
				export["FES/ALT.FCP"].Data = []byte("bad archive")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if payload, err := BuildStagedCatalog(ctx, export, ids); err == nil || payload != nil {
				t.Fatal("invalid export produced installer input", err)
			}
		})
	}
}
