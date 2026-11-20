// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func launcherStaging(t *testing.T, f *programMenuFixture) map[string][]byte {
	t.Helper()
	export := stagingCatalogExport(t, f)
	menu := fstest.MapFS{}
	for name, data := range programExport(t, "menu", f.request, programUpload(f.r.f.files, f.request)) {
		menu[name] = &fstest.MapFile{Data: data}
	}
	payload, err := BuildLauncherStaging(context.Background(), export, menu, []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload[packagestore.PreferencesPath]) != packagestore.MaxPreferencesBytes {
		t.Fatal("preferences allocation is not the full bounded allowance")
	}
	return payload
}

func TestProgramStartupSelectsPersistenceOnlyForOwnedPairedWritableBoot(t *testing.T) {
	for _, mode := range []struct {
		name             string
		writable, paired bool
	}{
		{"paired-writable", true, true}, {"paired-read-only", false, true},
		{"serial-writable", true, false}, {"serial-read-only", false, false},
	} {
		t.Run(mode.name, func(t *testing.T) {
			var startup *ProgramStartup
			f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
				payload := launcherStaging(t, f)
				if !mode.writable {
					// Read-only boots preserve their nonempty bootstrap contract.
					export := stagingCatalogExport(t, f)
					payload["FES/CORESEL.JSN"] = export["FES/CORESEL.JSN"].Data
					payload["FES/UI.JSON"] = export["FES/UI.JSON"].Data
				}
				for name, data := range payload {
					f.r.f.files.files[name] = &fstest.MapFile{Data: data}
				}
				var err error
				startup, err = ReadProgramStartup(context.Background(), f.r.f.store, mode.writable, mode.paired)
				if err != nil {
					t.Fatal(err)
				}
				if mode.paired {
					f.request = startup.Menu
				}
			})
			if err := f.s.AttachPreferences(startup.Preferences); err != nil {
				t.Fatal(err)
			}
			err := startup.Preferences.WritePreferences([]byte(`{"layout":"list","home":"library"}`))
			persistent := mode.paired && mode.writable
			if errors.Is(err, tenfoot.ErrPreferencesPending) != persistent || err != nil && !persistent {
				t.Fatal("wrong save mode", err)
			}
			want := "ram"
			if persistent {
				want = "sd-checkpoint"
			}
			if startup.PreferencesMode != want || f.r.f.files.preferenceWrites != 0 {
				t.Fatal("startup granted writes outside a checkpoint")
			}
			if mode.writable && len(startup.Staged) != 2 || !mode.writable && len(startup.Entries) != 2 {
				t.Fatal("bootstrap discovery changed")
			}
		})
	}
}

func TestProgramStartupRejectsMissingOrCorruptRequiredFilesBeforeMenu(t *testing.T) {
	for _, fault := range []string{"preferences-absent", "preferences-corrupt", "menu-absent", "two-menus", "contained", "canceled", "optional-absent", "optional-corrupt"} {
		t.Run(fault, func(t *testing.T) {
			f := serviceTest(t)
			empty := programJSON(t, programChoices{1, "nano-program-selection-v1", []programChoice{}})
			f.files.files["FES/CORESEL.JSN"] = &fstest.MapFile{Data: empty}
			f.files.files["FES/UI.JSON"] = &fstest.MapFile{Data: []byte("{\"entries\":[],\"format\":1,\"scope\":\"nano-tenfoot-catalog-v1\"}\n")}
			choice := programChoice{Path: "FES/MENU.FCP", PackageID: string(bytes.Repeat([]byte{'1'}, 64)), MetadataPath: "FES/MENU.JSN", MetadataSHA256: string(bytes.Repeat([]byte{'2'}, 64))}
			f.files.files["FES/MENUSEL.JSN"] = &fstest.MapFile{Data: programJSON(t, programChoices{1, "nano-program-selection-v1", []programChoice{choice}})}
			f.files.files[packagestore.PreferencesPath] = &fstest.MapFile{Data: []byte(`{}`)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "preferences-absent":
				delete(f.files.files, packagestore.PreferencesPath)
			case "preferences-corrupt":
				f.files.files[packagestore.PreferencesPath].Data = []byte("broken")
			case "menu-absent":
				delete(f.files.files, "FES/MENUSEL.JSN")
			case "two-menus":
				f.files.files["FES/MENUSEL.JSN"].Data = programJSON(t, programChoices{1, "nano-program-selection-v1", []programChoice{choice, choice}})
			case "contained":
				f.lease.r.Active = false
			case "canceled":
				cancel()
			case "optional-corrupt":
				f.files.files["FES/INSTALL.JSN"] = &fstest.MapFile{Data: []byte("broken")}
			}
			startup, err := ReadProgramStartup(ctx, f.store, true, true)
			optional := fault == "optional-absent" || fault == "optional-corrupt"
			if optional {
				if err != nil || startup == nil || (startup.StagedError != nil) != (fault == "optional-corrupt") {
					t.Fatal(startup, err)
				}
			} else if err == nil || startup != nil {
				t.Fatal("bad required startup accepted", startup, err)
			}
			if f.cpu.calls != 0 || f.files.preferenceWrites != 0 {
				t.Fatal("startup performed hardware mutation")
			}
		})
	}
}

func TestProgramLauncherStagingRejectsInvalidMenuAndReservedPreferenceSources(t *testing.T) {
	f := newProgramMenuFixture(t, "")
	for _, fault := range []string{"missing-menu", "two-menus", "changed-package", "changed-metadata", "computer-instead", "preferences-collision", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			export := stagingCatalogExport(t, f)
			menu := fstest.MapFS{}
			request := f.request
			if fault == "computer-instead" {
				request, _, _ = addROMProgram(t, f.r, 0, 8192)
			}
			for name, data := range programExport(t, "menu", request, programUpload(f.r.f.files, request)) {
				menu[name] = &fstest.MapFile{Data: data}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "missing-menu":
				delete(menu, request.Path)
			case "two-menus":
				var choices programChoices
				if err := json.Unmarshal(menu["FES/CORESEL.JSN"].Data, &choices); err != nil {
					t.Fatal(err)
				}
				choices.Programs = append(choices.Programs, choices.Programs[0])
				menu["FES/CORESEL.JSN"].Data = programJSON(t, choices)
			case "changed-package":
				menu[request.Path].Data[0] ^= 1
			case "changed-metadata":
				menu[request.MetadataPath].Data = []byte(`{}`)
			case "preferences-collision":
				var choices programChoices
				if err := json.Unmarshal(export["FES/CORESEL.JSN"].Data, &choices); err != nil {
					t.Fatal(err)
				}
				old := choices.Programs[0].Path
				choices.Programs[0].Path = packagestore.PreferencesPath
				export[packagestore.PreferencesPath] = export[old]
				export["FES/CORESEL.JSN"].Data = programJSON(t, choices)
			case "canceled":
				cancel()
			}
			payload, err := BuildLauncherStaging(ctx, export, menu, []string{"first", "second"})
			if err == nil || payload != nil {
				t.Fatal("invalid complete staging published", err)
			}
		})
	}
}

func TestProgramLauncherStagingBoundsAllExportedFilesTogether(t *testing.T) {
	ctx := context.Background()
	fixture := newProgramMenuFixture(t, "")
	export := stagingCatalogExport(t, fixture)
	menu := fstest.MapFS{}
	for name, data := range programExport(t, "menu", fixture.request, programUpload(fixture.r.f.files, fixture.request)) {
		menu[name] = &fstest.MapFile{Data: data}
	}
	ids := []string{"first", "second"}
	staged, err := BuildLauncherStaging(ctx, export, menu, ids)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, data := range staged {
		total += len(data)
	}
	if _, err := buildLauncherStaging(ctx, export, menu, ids, total); err != nil {
		t.Fatal("exact aggregate allowance rejected", err)
	}
	if files, err := buildLauncherStaging(ctx, export, menu, ids, total-1); err == nil || files != nil {
		t.Fatal("oversized complete staging published", err)
	}
}
