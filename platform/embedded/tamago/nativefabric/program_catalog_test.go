// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func TestProgramCatalogDownloadsSelectExactPackageAndAlternateLaunches(t *testing.T) {
	f, b, _, _ := newProgramInstallFixture(t)
	// Supply exports from original source paths rather than install destinations.
	request := b.entries[0].Request
	firstExport := programExport(t, "first", request, programUpload(f.r.f.files, request))
	secondRequest, _, _ := addROMProgramChanged(t, f.r, 0, 8192, func(manifest string) string {
		return strings.Replace(manifest, `version = "0.1.0"`, `version = "0.2.0"`, 1)
	})
	secondExport := programExport(t, "second", secondRequest, programUpload(f.r.f.files, secondRequest))
	exports := map[string]map[string][]byte{"first": firstExport, "second": secondExport}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		if len(parts) == 2 {
			if data, ok := exports[parts[0]][parts[1]]; ok {
				w.Write(data)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	for _, id := range []string{"first", "second"} {
		source, err := NewProgramSource(server.Client(), server.URL+"/"+id, "")
		if err != nil {
			t.Fatal(err)
		}
		plan, err := source.Download(context.Background(), b, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = b.AttachProgramInstall(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
	}
	server.Close()
	if len(f.r.f.files.installOperations) != 0 {
		t.Fatal("catalog publication installed content")
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	rows, err := client.AvailableCores(context.Background())
	if err != nil || len(rows) != 2 || rows[0].PackageID == rows[1].PackageID || rows[0].CoreID != rows[1].CoreID {
		t.Fatal("two versions of one core were not independently advertised", rows, err)
	}
	for _, row := range rows {
		if row.ArtifactState != "available" {
			t.Fatal(row)
		}
	}
	// A known package with the wrong core identity cannot select either plan.
	foreign := rows[1].CoreReference
	foreign.CoreID = "fes.foreign"
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	if err = client.InstallAvailableCore(context.Background(), foreign); err == nil {
		t.Fatal("crossed reference installed a package")
	}
	if calls != f.r.cpu.calls || opens != f.r.f.files.opens || len(f.r.f.files.installOperations) != 0 {
		t.Fatal("crossed reference touched devices")
	}
	for _, index := range []int{1, 0} {
		row := rows[index]
		if _, err = client.CoreSetup(context.Background(), row.SourceID, row.CoreID, row.PackageID); err == nil {
			t.Fatal("uninstalled package offered setup")
		}
		if err = client.InstallAvailableCore(context.Background(), row.CoreReference); err != nil {
			t.Fatal(err)
		}
		setup, err := client.CoreSetup(context.Background(), row.SourceID, row.CoreID, row.PackageID)
		want := []string{"first", "second"}[index]
		if err != nil || len(setup.Entries) != 1 || setup.Entries[0].GameID != want {
			t.Fatal("setup selected the wrong package", setup, err)
		}
		fresh, err := client.AvailableCores(context.Background())
		if err != nil || fresh[index].ArtifactState != "installed" || (index == 1 && fresh[0].ArtifactState != "available") {
			t.Fatal("installation changed another package's state", fresh, err)
		}
	}
	for _, index := range []int{0, 1, 0} {
		id := []string{"first", "second"}[index]
		play, err := client.Launch(context.Background(), id)
		if err != nil || play.CorePackage.PackageID != rows[index].PackageID {
			t.Fatal("launch selected another catalog package", err)
		}
		if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
	}
	if f.s.Status().State != ProgramReady || f.r.f.lease.r.Active {
		t.Fatal("alternating catalog launches did not restore the menu")
	}
}

func TestProgramCatalogRefusesAliasesAndContentBudgetWithoutMutation(t *testing.T) {
	f, b, first, _ := newProgramInstallFixture(t)
	if err := b.AttachProgramInstall(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	alias, err := b.prepareInstallContent(context.Background(), "alias", "Alias", first.content)
	if err != nil {
		t.Fatal(err)
	}
	calls, opens := f.r.cpu.calls, f.r.f.files.opens
	if err = b.AttachProgramInstall(context.Background(), alias); err == nil {
		t.Fatal("ambiguous package reference accepted")
	}
	request, _, _ := addROMProgramChanged(t, f.r, 0, 8192, func(manifest string) string {
		return strings.Replace(manifest, `version = "0.1.0"`, `version = "0.2.0"`, 1)
	})
	second, err := b.PrepareInstall(context.Background(), "second", "Second", request, programUpload(f.r.f.files, request))
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the accounting boundary with real admitted content, without
	// allocating 64 MiB of fixture archives just to exhaust the budget.
	b.catalogBytes = packagestore.MaxInstallBytes - second.content.ContentBytes() + 1
	if err = b.AttachProgramInstall(context.Background(), second); err == nil {
		t.Fatal("aggregate content budget exceeded")
	}
	if len(b.catalog) != 1 || b.catalog[0] != first || calls != f.r.cpu.calls || opens != f.r.f.files.opens || len(f.r.f.files.installOperations) != 0 {
		t.Fatal("rejected attachment changed catalog or devices")
	}
	b.catalogBytes--
	if err = b.AttachProgramInstall(context.Background(), second); err != nil || b.catalogBytes != packagestore.MaxInstallBytes {
		t.Fatal("exact budget boundary rejected", err)
	}
}

func TestProgramCatalogRecordLimitRefusesWithoutReplacingRows(t *testing.T) {
	f, b, _, _ := newProgramInstallFixture(t)
	for i := 0; i <= packagestore.MaxInstallRecords; i++ {
		request, _, _ := addROMProgramChanged(t, f.r, 0, 8192, func(manifest string) string {
			return strings.Replace(manifest, `version = "0.1.0"`, fmt.Sprintf(`version = "0.2.%d"`, i), 1)
		})
		plan, err := b.PrepareInstall(context.Background(), fmt.Sprintf("core-%d", i), "Core", request, programUpload(f.r.f.files, request))
		if err != nil {
			t.Fatal(err)
		}
		err = b.AttachProgramInstall(context.Background(), plan)
		if (i < packagestore.MaxInstallRecords && err != nil) || (i == packagestore.MaxInstallRecords && err == nil) {
			t.Fatal("catalog record limit was not enforced", i, err)
		}
	}
	if len(b.catalog) != packagestore.MaxInstallRecords || b.catalog[0].entry.ID != "core-0" || b.catalog[packagestore.MaxInstallRecords-1].entry.ID != "core-31" || len(f.r.f.files.installOperations) != 0 {
		t.Fatal("capacity refusal replaced catalog rows or wrote storage")
	}
}

func TestProgramCatalogInstalledStateRequiresExactRetainedRequest(t *testing.T) {
	f, b, first, _ := newProgramInstallFixture(t)
	if err := b.AttachProgramInstall(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	request, _, _ := addROMProgramChanged(t, f.r, 0, 8192, func(manifest string) string {
		return strings.Replace(manifest, `version = "0.1.0"`, `version = "0.2.0"`, 1)
	})
	other, err := b.PrepareInstall(context.Background(), first.entry.ID, first.entry.Title, request, programUpload(f.r.f.files, request))
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Install(context.Background(), 0, other); err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	rows, err := client.AvailableCores(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ArtifactState != "available" {
		t.Fatal("entry ID collision falsely advertised the retained package as installed", rows, err)
	}
	if _, err = client.CoreSetup(context.Background(), rows[0].SourceID, rows[0].CoreID, rows[0].PackageID); err == nil {
		t.Fatal("setup returned another installed package")
	}
	calls, writes := f.r.cpu.calls, len(f.r.f.files.installOperations)
	if err = client.InstallAvailableCore(context.Background(), rows[0].CoreReference); err == nil {
		t.Fatal("catalog overwrote a conflicting installed identity")
	}
	if calls != f.r.cpu.calls || writes != len(f.r.f.files.installOperations) {
		t.Fatal("identity collision changed devices")
	}
}
