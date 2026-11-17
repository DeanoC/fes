package corepackage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func nanoROMFixture(t *testing.T, size int) (*Archive, []byte, []byte) {
	t.Helper()
	root := filepath.Join("testdata", "core-bundle-v3")
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	manifest := read(fmt.Sprintf("manifests/valid-nano-%d.toml", size))
	payload := read("payloads/fes-fixture.rbf")
	mapping := read(fmt.Sprintf("maps/valid-nano-%d.json", size))
	body := canonicalArchiveWithROMMap(manifest, payload, mapping)
	dir := writeDirectory(t, manifest, payload)
	if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), mapping, 0600); err != nil {
		t.Fatal(err)
	}
	listed, err := InspectListing(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	a := retained(t, body)
	if listed.PackageID != a.Inspection().PackageID {
		t.Fatal("streaming/retained identity differs")
	}
	return a, body, payload
}

func TestNanoHeldROMRetainsSourceAndUnchangedFPGA(t *testing.T) {
	for _, size := range []int{4096, 8192} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			a, body, payload := nanoROMFixture(t, size)
			d := a.Inspection().Descriptor
			image := make([]byte, size)
			for i := range image {
				image[i] = byte(i*17 + 3)
			}
			want := bytes.Clone(image)
			linked, err := a.LinkROMs(context.Background(), []ROMSource{{d.ROM.ID, "firmware", image}})
			if err != nil {
				t.Fatal(err)
			}
			receipt := linked.Receipt()
			if receipt.PackageID != a.Inspection().PackageID || receipt.ROMLink == nil ||
				!receipt.ROMLink.ValidFor(d) || receipt.ROMLink.SourceSHA256 != romDigest(want) ||
				receipt.ROMLink.ProgrammedSHA256 != romDigest(payload) {
				t.Fatal("held ROM receipt does not bind independent source/payload", receipt)
			}
			first := linked.ReceiptSHA256()
			clear(image)
			clear(body)
			receipt.ROMLink.SourceSHA256 = "changed"
			reader, err := linked.Source(d.ROM.ID, "firmware")
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("source escaped private ownership", err)
			}
			clear(got)
			programmed, err := io.ReadAll(linked.Programmed())
			if err != nil || !bytes.Equal(programmed, payload) {
				t.Fatal("held ROM delivery changed FPGA bytes", err)
			}
			clear(programmed)
			if first != linked.ReceiptSHA256() || linked.Receipt().ROMLink.SourceSHA256 != romDigest(want) {
				t.Fatal("public reader or receipt mutated retained evidence")
			}
			other, err := a.LinkROMs(context.Background(), []ROMSource{{d.ROM.ID, "firmware", image}})
			if err != nil || other.ReceiptSHA256() == first {
				t.Fatal("changed ROM did not change activation receipt", err)
			}
		})
	}
}

func TestNanoHeldROMRejectsWrongSourcesAndBitstreamStaging(t *testing.T) {
	a, body, _ := nanoROMFixture(t, 4096)
	d := a.Inspection().Descriptor
	for _, sources := range [][]ROMSource{nil, {{d.ROM.ID, "cartridge", make([]byte, 4096)}},
		{{"other", "firmware", make([]byte, 4096)}}, {{d.ROM.ID, "firmware", make([]byte, 8192)}}} {
		if result, err := a.LinkROMs(context.Background(), sources); err == nil || result != nil {
			t.Fatal("invalid Nano ROM source accepted")
		}
	}
	source := []ROMSource{{d.ROM.ID, "firmware", make([]byte, 4096)}}
	if result, err := a.LinkROMs(context.Background(), source, PartInput{ID: "other", Archive: []byte("not a part")}); err == nil || result != nil {
		t.Fatal("Nano delivery accepted Cyclone V composition")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := a.LinkROMs(ctx, source); err != context.Canceled || result != nil {
		t.Fatal("cancellation", err)
	}
	if _, err := prepareROMInput(context.Background(), ROMInput{Package: body, ROM: source[0].Data}); err == nil {
		t.Fatal("Nano was sent to the Cyclone V bitstream linker")
	}
	linked, err := a.LinkROMs(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ROMLinkIdentity){
		func(r *ROMLinkIdentity) { r.Role = "" }, func(r *ROMLinkIdentity) { r.ProgrammedSHA256 = romDigest([]byte("different")) },
		func(r *ROMLinkIdentity) { r.ProgrammedSize++ },
	} {
		r := *linked.Receipt().ROMLink
		mutate(&r)
		if r.ValidFor(d) {
			t.Fatal("Nano receipt allowed role or programmed-byte substitution")
		}
	}
}
