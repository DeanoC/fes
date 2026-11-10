// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func stagedRecord(t *testing.T, id string, request packagestore.Request, files map[string][]byte) []byte {
	t.Helper()
	exported := programExport(t, id, request, files)
	var choices programChoices
	if err := json.Unmarshal(exported["FES/CORESEL.JSN"], &choices); err != nil {
		t.Fatal(err)
	}
	sums := map[string]string{}
	for name, data := range files {
		sums[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	raw, err := canonicalProgramRecord(map[string]any{"format": 1, "scope": "nano-staged-program-install-v1", "id": id, "title": "Staged core", "program": choices.Programs[0], "files": sums})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type checkedInstallFiles struct {
	*programTestFiles
	calls, failAt int
}

func (f *checkedInstallFiles) Check(context.Context) error {
	r, valid := f.lease.LastReceipt()
	if !valid || !r.Active || !r.Prepared {
		return errors.New("integrity check without owned SD")
	}
	f.calls++
	if f.calls == f.failAt {
		return errors.New("protected boot file changed")
	}
	return nil
}

func TestStagedInstallerIntegrityFailureDoesNotAdvertiseOrRetry(t *testing.T) {
	for _, failAt := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			var checked *checkedInstallFiles
			var request packagestore.Request
			f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
				request, _, _ = addROMProgram(t, f.r, 0, 8192)
				checked = &checkedInstallFiles{programTestFiles: f.r.f.files, failAt: failAt}
				var err error
				f.r.f.store, err = packagestore.New(checked, f.r.f.lease)
				if err != nil {
					t.Fatal(err)
				}
			})
			b, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "existing", Title: "Existing", Request: request}})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := b.PrepareInstall(context.Background(), "guarded", "Guarded", request, programUpload(f.r.f.files, request))
			if err != nil {
				t.Fatal(err)
			}
			err = b.Install(context.Background(), 0, plan)
			if failAt == 0 {
				if err != nil || checked.calls != 2 || len(b.entries) != 2 || f.s.Status().State != ProgramReady {
					t.Fatal(err, checked.calls, f.s.Status())
				}
				return
			}
			if err == nil || checked.calls != failAt || len(b.entries) != 1 || f.s.Status().State != ProgramRebootRequired {
				t.Fatal(err, checked.calls, f.s.Status())
			}
			if _, err = f.r.f.store.ActiveReceipt(); !errors.Is(err, packagestore.ErrRetired) {
				t.Fatal("failed integrity retained usable store", err)
			}
			published := false
			for name := range checked.files {
				published = published || strings.HasPrefix(name, packagestore.InstallCatalogRoot+"/")
			}
			if published != (failAt == 2) {
				t.Fatal("catalogue publication crossed wrong integrity boundary", published)
			}
			calls, writes := f.r.cpu.calls, len(checked.installOperations)
			if err = b.Install(context.Background(), f.s.Status().Generation, plan); err == nil || calls != f.r.cpu.calls || writes != len(checked.installOperations) {
				t.Fatal("failed installer retried", err)
			}
		})
	}
}

func TestStagedCatalogRealFATInstallLaunchReturnAndFreshBootDiscovery(t *testing.T) {
	var staged *StagedProgram
	ownedInstallRealFATCycleWithStaging(t, func(b *ProgramBackend, _ packagestore.Request, _ map[string][]byte) (*ProgramInstall, error) {
		if err := b.AttachStagedProgram(context.Background(), staged); err != nil {
			return nil, err
		}
		return b.PrepareStagedInstall(context.Background(), staged)
	}, func(store *packagestore.Store, volume *fat.Volume, request packagestore.Request, files map[string][]byte) {
		if err := volume.CreateFile(context.Background(), "FES/INSTALL.JSN", stagedRecord(t, "persisted", request, files)); err != nil {
			t.Fatal(err)
		}
		var err error
		staged, err = ReadStagedProgram(context.Background(), store, "FES/INSTALL.JSN")
		if err != nil {
			t.Fatal(err)
		}
	}, func(b *ProgramBackend, _ *ProgramInstall) error {
		client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
		rows, err := client.AvailableCores(context.Background())
		if err != nil {
			return err
		}
		return client.InstallAvailableCore(context.Background(), rows[0].CoreReference)
	})
}

func stagedFixture(t *testing.T, change func(*programMenuFixture, packagestore.Request, []byte) []byte) (*programMenuFixture, *StagedProgram, error) {
	t.Helper()
	var staged *StagedProgram
	var stageErr error
	f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
		request, _, _ := addROMProgram(t, f.r, 0, 8192)
		files := programUpload(f.r.f.files, request)
		raw := stagedRecord(t, "staged", request, files)
		if change != nil {
			raw = change(f, request, raw)
		}
		f.r.f.files.files["FES/INSTALL.JSN"] = &fstest.MapFile{Data: raw}
		calls, writes := f.r.cpu.calls, len(f.r.f.files.installOperations)
		staged, stageErr = ReadStagedProgram(context.Background(), f.r.f.store, "FES/INSTALL.JSN")
		if f.r.cpu.calls != calls || len(f.r.f.files.installOperations) != writes {
			t.Fatal("staging accessed firmware or wrote files")
		}
	})
	return f, staged, stageErr
}

func TestStagedFirstCoreInstallsIntoEmptyBootstrapLibrary(t *testing.T) {
	var entries []ProgramEntry
	f, staged, err := stagedFixture(t, func(f *programMenuFixture, _ packagestore.Request, raw []byte) []byte {
		f.r.f.files.files["FES/CORESEL.JSN"] = &fstest.MapFile{Data: programJSON(t, programChoices{1, "nano-program-selection-v1", []programChoice{}})}
		f.r.f.files.files["FES/UI.JSON"] = &fstest.MapFile{Data: []byte("{\"entries\":[],\"format\":1,\"scope\":\"nano-tenfoot-catalog-v1\"}\n")}
		var e error
		entries, e = ReadInstallerBootstrap(context.Background(), f.r.f.store)
		if e != nil || len(entries) != 0 {
			t.Fatal(entries, e)
		}
		return raw
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewProgramBackend(f.s, entries)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.AttachStagedProgram(context.Background(), staged); err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	rows, err := client.AvailableCores(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if err = client.InstallAvailableCore(context.Background(), rows[0].CoreReference); err != nil {
		t.Fatal(err)
	}
	if len(b.entries) != 1 || b.entries[0].ID != "staged" {
		t.Fatal("first installed core not advertised", b.entries)
	}
	play, err := client.Launch(context.Background(), "staged")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
}

func TestStagedCatalogUsesNormalInstallAPIThenLaunchAndReturn(t *testing.T) {
	f, staged, err := stagedFixture(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	b, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "existing", Title: "Existing", Request: request}})
	if err != nil {
		t.Fatal(err)
	}
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	if err = b.AttachStagedProgram(context.Background(), staged); err != nil {
		t.Fatal(err)
	}
	if calls != f.r.cpu.calls || opens != f.r.f.files.opens {
		t.Fatal("attached source reopened contained storage")
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	rows, err := client.AvailableCores(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ArtifactState != "available" {
		t.Fatal(rows, err)
	}
	if err = client.InstallAvailableCore(context.Background(), rows[0].CoreReference); err != nil {
		t.Fatal(err)
	}
	setup, err := client.CoreSetup(context.Background(), rows[0].SourceID, rows[0].CoreID, rows[0].PackageID)
	if err != nil || len(setup.Entries) != 1 || setup.Entries[0].GameID != "staged" {
		t.Fatal(setup, err)
	}
	rows, err = client.AvailableCores(context.Background())
	if err != nil || rows[0].ArtifactState != "installed" {
		t.Fatal(rows, err)
	}
	play, err := client.Launch(context.Background(), "staged")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	before := len(f.r.f.files.installOperations)
	if err = client.InstallAvailableCore(context.Background(), rows[0].CoreReference); err == nil {
		t.Fatal("duplicate install accepted")
	}
	if len(f.r.f.files.installOperations) != before {
		t.Fatal("duplicate rewrote content")
	}
	if f.s.Status().State != ProgramReady {
		t.Fatal(f.s.Status())
	}
}

func TestStagingRejectsChangedSchemaDigestAndUnknownFilesWithoutMutation(t *testing.T) {
	for _, fault := range []string{"duplicate", "unknown", "digest", "extra-file", "missing", "oversize", "bad-package"} {
		t.Run(fault, func(t *testing.T) {
			_, staged, err := stagedFixture(t, func(f *programMenuFixture, request packagestore.Request, raw []byte) []byte {
				if fault == "duplicate" {
					return bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1)
				}
				if fault == "oversize" {
					return []byte(strings.Repeat(" ", 8193))
				}
				var fields map[string]any
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				sums := fields["files"].(map[string]any)
				switch fault {
				case "unknown":
					fields["unexpected"] = true
				case "digest":
					sums[request.Path] = strings.Repeat("0", 64)
				case "extra-file":
					sums["FES/FOREIGN.BIN"] = strings.Repeat("a", 64)
				case "missing":
					delete(f.r.f.files.files, request.Path)
				case "bad-package":
					f.r.f.files.files[request.Path].Data = []byte("invalid archive")
					sums[request.Path] = fmt.Sprintf("%x", sha256.Sum256([]byte("invalid archive")))
				}
				out, e := canonicalProgramRecord(fields)
				if e != nil {
					t.Fatal(e)
				}
				return out
			})
			if err == nil || staged != nil {
				t.Fatal("invalid staged source accepted", staged, err)
			}
		})
	}
}

func TestStagedCatalogRejectsForeignReferencesMissingBodyAndRunningInstall(t *testing.T) {
	f, staged, err := stagedFixture(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	b, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "existing", Title: "Existing", Request: request}})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.AttachStagedProgram(context.Background(), staged); err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	rows, err := client.AvailableCores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ref := rows[0].CoreReference
	b.SetStagedSourceError(errors.New("invalid optional staged source"))
	if _, err = client.AvailableCores(context.Background()); err == nil {
		t.Fatal("invalid source still advertised")
	}
	if err = client.InstallAvailableCore(context.Background(), ref); err == nil {
		t.Fatal("quarantined source installed")
	}
	play, err := client.Launch(context.Background(), "existing")
	if err != nil {
		t.Fatal("source error broke healthy library", err)
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	b.SetStagedSourceError(nil)
	ref.PackageID = strings.Repeat("0", 64)
	before := len(f.r.f.files.installOperations)
	if err = client.InstallAvailableCore(context.Background(), ref); err == nil {
		t.Fatal("foreign package accepted")
	}
	r, _ := http.NewRequest(http.MethodPost, "http://native.invalid/api/v1/core-catalog/install", nil)
	response, err := b.RoundTrip(r)
	if err != nil || response.StatusCode != 400 {
		t.Fatal(response, err)
	}
	response.Body.Close()
	play, err = client.Launch(context.Background(), "existing")
	if err != nil {
		t.Fatal(err)
	}
	if err = client.InstallAvailableCore(context.Background(), rows[0].CoreReference); err == nil {
		t.Fatal("installed during running core")
	}
	if len(f.r.f.files.installOperations) != before {
		t.Fatal("refusal wrote content")
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
}
