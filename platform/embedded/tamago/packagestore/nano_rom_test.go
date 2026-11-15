package packagestore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
)

func TestNanoHeldROMRetentionSurvivesStorageContainment(t *testing.T) {
	for _, size := range []int{4096, 8192} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			ctx := context.Background()
			base := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v3/"
			manifest := read(t, base+fmt.Sprintf("manifests/valid-nano-%d.toml", size))
			payload := read(t, base+"payloads/fes-fixture.rbf")
			mapping := read(t, base+fmt.Sprintf("maps/valid-nano-%d.json", size))
			body := canonical(t, manifest, payload, mapping)
			a, err := corepackage.RetainArchive(ctx, int64(len(body)), bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			d := a.Inspection().Descriptor
			image := make([]byte, size)
			for i := range image {
				image[i] = byte(i*29 + 7)
			}
			want := bytes.Clone(image)
			linked, err := a.LinkROMs(ctx, []corepackage.ROMSource{{ID: d.ROM.ID, Role: "firmware", Data: image}})
			if err != nil {
				t.Fatal(err)
			}
			req := Request{Path: "FES/CORE.FCP", PackageID: a.Inspection().PackageID, ReceiptSHA256: linked.ReceiptSHA256(),
				Sources: []Source{{ID: d.ROM.ID, Role: "firmware", Path: "FES/FIRMWARE.BIN", SHA256: hash(image)}}}
			files := fstest.MapFS{req.Path: &fstest.MapFile{Data: body}, req.Sources[0].Path: &fstest.MapFile{Data: image}}
			l := lease()
			store, tracked := open(t, files, l)
			for _, fault := range []string{"receipt", "digest", "role", "size", "package"} {
				bad := req
				bad.Sources = append([]Source(nil), req.Sources...)
				switch fault {
				case "receipt":
					bad.ReceiptSHA256 = strings.Repeat("0", 64)
				case "digest":
					bad.Sources[0].SHA256 = strings.Repeat("0", 64)
				case "role":
					bad.Sources[0].Role = "cartridge"
				case "size":
					files[req.Sources[0].Path].Data = image[:size-1]
				case "package":
					bad.PackageID = strings.Repeat("0", 64)
				}
				if selected, e := store.Retain(ctx, bad); e == nil || selected != nil {
					t.Fatal("bad retained ROM accepted", fault)
				}
				files[req.Sources[0].Path].Data = image
				if l.stops != 0 || tracked.handles.Load() != 0 {
					t.Fatal("content rejection contained storage or leaked a file", fault)
				}
			}
			selection, err := store.Retain(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			if selection.Linked() == nil || selection.Linked().ReceiptSHA256() != req.ReceiptSHA256 {
				t.Fatal("missing exact retained activation")
			}
			handoff, err := store.Contain(selection)
			if err != nil {
				t.Fatal(err)
			}
			reads := tracked.reads.Load()
			clear(image)
			clear(body)
			selected, err := handoff.Selection()
			if err != nil {
				t.Fatal(err)
			}
			reader, err := selected.Linked().Source(d.ROM.ID, "firmware")
			if err != nil {
				t.Fatal(err)
			}
			rom, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(rom, want) {
				t.Fatal("contained ROM changed", err)
			}
			programmed, err := io.ReadAll(selected.Linked().Programmed())
			if err != nil || !bytes.Equal(programmed, payload) {
				t.Fatal("FPGA payload was patched", err)
			}
			if tracked.reads.Load() != reads || tracked.handles.Load() != 0 || l.stops != 1 {
				t.Fatal("contained read touched storage")
			}
			if err = store.Resume(); err != nil {
				t.Fatal(err)
			}
			if _, err = handoff.Selection(); err == nil {
				t.Fatal("resumed storage preserved an old handoff")
			}
		})
	}
}
