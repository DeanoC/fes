// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func TestProgramCatalogStagesTwoROMReceiptsForOneFPGAPackage(t *testing.T) {
	var staged []*StagedProgram
	var requests []packagestore.Request
	var images [][]byte
	f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
		first, record, _ := addROMProgram(t, f.r, 0, 8192)
		firstImage := bytes.Clone(f.r.cpu.expectedROM)
		secondImage := bytes.Clone(firstImage)
		secondImage[0] ^= 1
		body := f.r.f.files.files[first.Path].Data
		archive, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		linked, err := archive.LinkROMs(context.Background(), []corepackage.ROMSource{{ID: "firmware", Role: "firmware", Data: secondImage}})
		if err != nil {
			t.Fatal(err)
		}
		second := first
		second.Sources = append([]packagestore.Source(nil), first.Sources...)
		second.ReceiptSHA256 = linked.ReceiptSHA256()
		second.MetadataPath = "FES/ROMALT.JSN"
		second.Sources[0].Path = "FES/ROMALT.BIN"
		second.Sources[0].SHA256 = fmt.Sprintf("%x", sha256.Sum256(secondImage))
		record.ROMReceiptSHA256 = second.ReceiptSHA256
		meta := programJSON(t, record)
		second.MetadataSHA256 = fmt.Sprintf("%x", sha256.Sum256(meta))
		f.r.f.files.files[second.MetadataPath] = &fstest.MapFile{Data: meta}
		f.r.f.files.files[second.Sources[0].Path] = &fstest.MapFile{Data: secondImage}
		a := programExport(t, "first", first, programUpload(f.r.f.files, first))
		b := programExport(t, "second", second, programUpload(f.r.f.files, second))
		choices := programChoices{1, "nano-program-selection-v1", []programChoice{}}
		for _, r := range []packagestore.Request{first, second} {
			choices.Programs = append(choices.Programs, programChoice{Path: r.Path, PackageID: r.PackageID, MetadataPath: r.MetadataPath, MetadataSHA256: r.MetadataSHA256, ROMReceiptSHA256: r.ReceiptSHA256, Sources: []programROMSource{{r.Sources[0].ID, r.Sources[0].Role, r.Sources[0].Path, r.Sources[0].SHA256}}})
		}
		a["FES/CORESEL.JSN"] = programJSON(t, choices)
		a["FES/UI.JSON"] = []byte("{\"entries\":[{\"id\":\"first\",\"program\":0,\"title\":\"ROM A\"},{\"id\":\"second\",\"program\":1,\"title\":\"ROM B\"}],\"format\":1,\"scope\":\"nano-tenfoot-catalog-v1\"}\n")
		for name, data := range b {
			if name != "FES/CORESEL.JSN" && name != "FES/UI.JSON" {
				a[name] = data
			}
		}
		export := fstest.MapFS{}
		for name, data := range a {
			export[name] = &fstest.MapFile{Data: data}
		}
		payload, err := BuildStagedCatalog(context.Background(), export, []string{"first", "second"})
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range payload {
			f.r.f.files.files[name] = &fstest.MapFile{Data: data}
		}
		staged, err = ReadStagedPrograms(context.Background(), f.r.f.store, "FES/INSTALL.JSN")
		if err != nil || len(staged) != 2 {
			t.Fatal(len(staged), err)
		}
		requests = []packagestore.Request{first, second}
		images = [][]byte{firstImage, secondImage}
	})
	b, err := NewProgramBackend(f.s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.AttachStagedPrograms(context.Background(), staged); err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	rows, err := client.AvailableCores(context.Background())
	if err != nil || len(rows) != 2 || rows[0].PackageID != rows[1].PackageID || rows[0].CoreID != rows[1].CoreID || rows[0].SourceID == rows[1].SourceID {
		t.Fatal("same-package ROM choices lost their exact references", rows, err)
	}
	for _, source := range []string{stagedSourceID, stagedSourceID + ":" + strings.Repeat("0", 64)} {
		ref := rows[0].CoreReference
		ref.SourceID = source
		calls, writes := f.r.cpu.calls, len(f.r.f.files.installOperations)
		if err = client.InstallAvailableCore(context.Background(), ref); err == nil {
			t.Fatal("unbound source reference accepted")
		}
		if calls != f.r.cpu.calls || writes != len(f.r.f.files.installOperations) {
			t.Fatal("rejected reference touched devices")
		}
	}
	for _, i := range []int{1, 0} {
		row := rows[i]
		if err = client.InstallAvailableCore(context.Background(), row.CoreReference); err != nil {
			t.Fatal(err)
		}
		setup, err := client.CoreSetup(context.Background(), row.SourceID, row.CoreID, row.PackageID)
		if err != nil || len(setup.Entries) != 1 || setup.Entries[0].GameID != []string{"first", "second"}[i] {
			t.Fatal("setup selected another ROM choice", setup, err)
		}
	}
	for _, i := range []int{0, 1, 0} {
		f.r.cpu.expectedROM = bytes.Clone(images[i])
		play, err := client.Launch(context.Background(), []string{"first", "second"}[i])
		if err != nil {
			t.Fatal(err)
		}
		view := f.s.Status()
		if play.CorePackage.PackageID != requests[i].PackageID || view.ROMReceiptSHA256 != requests[i].ReceiptSHA256 || view.Identity.ActivationReceiptSHA256 != requests[i].ReceiptSHA256 {
			t.Fatal("activation used another ROM receipt", view)
		}
		if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
	}
}
