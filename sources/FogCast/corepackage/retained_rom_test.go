package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

func retained(t *testing.T, data []byte) *Archive {
	t.Helper()
	a, err := RetainArchive(context.Background(), int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestRetainedROMLinksMatchStaging(t *testing.T) {
	for _, version := range []int{3, 4} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			ctx := context.Background()
			var input []byte
			var sources []ROMSource
			var staged Staged
			var err error
			if version == 3 {
				in := linkedROMFixture(t)
				input = in.Package
				a := retained(t, input)
				r := a.Inspection().Descriptor.ROM
				sources = []ROMSource{{r.ID, r.Role, in.ROM}}
				envelope, e := WriteROMInput(in)
				if e != nil {
					t.Fatal(e)
				}
				staged, err = StageROMInput(ctx, t.TempDir(), int64(len(envelope)), bytes.NewReader(envelope))
			} else {
				in := linkedTwoROMFixture(t)
				input = in.Package
				a := retained(t, input)
				r := a.Inspection().Descriptor.ROMs
				sources = []ROMSource{{r[0].ID, r[0].Role, in.BIOS}, {r[1].ID, r[1].Role, in.Cartridge}}
				envelope, e := WriteROMInputV2(in)
				if e != nil {
					t.Fatal(e)
				}
				staged, err = StageROMInputV2(ctx, t.TempDir(), int64(len(envelope)), bytes.NewReader(envelope))
			}
			if err != nil {
				t.Fatal(err)
			}
			defer staged.Cleanup()
			archive := retained(t, input)
			linked, err := archive.LinkROMs(ctx, sources)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(staged.ProgrammedPath)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(linked.Programmed())
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("existing staging disagrees: %v", err)
			}
			receipt := linked.Receipt()
			before, _ := json.Marshal(receipt)
			digest := linked.ReceiptSHA256()
			original := bytes.Clone(sources[0].Data)
			sources[0].Data[0] ^= 1
			input[0] ^= 1
			got[0] ^= 1
			if receipt.ROMLink != nil {
				receipt.ROMLink.SourceSHA256 = "changed"
			}
			if receipt.ROMLinks != nil {
				receipt.ROMLinks.Sources[0].ID = "changed"
			}
			after, _ := json.Marshal(linked.Receipt())
			if !bytes.Equal(before, after) || linked.ReceiptSHA256() != digest {
				t.Fatal("receipt escaped ownership")
			}
			reader, err := linked.Source(sources[0].ID, sources[0].Role)
			if err != nil {
				t.Fatal(err)
			}
			source, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(source, original) {
				t.Fatal("retained source changed")
			}
			reread, err := io.ReadAll(linked.Programmed())
			if err != nil || !bytes.Equal(reread, want) {
				t.Fatal("retained programming bytes changed")
			}
			// Same package, different source must produce a different activation receipt.
			different, err := archive.LinkROMs(ctx, sources)
			if err != nil {
				t.Fatal(err)
			}
			if different.ReceiptSHA256() == digest {
				t.Fatal("source identity not bound")
			}
			if _, err = linked.Source("missing", "firmware"); err == nil {
				t.Fatal("unknown source accepted")
			}
		})
	}
}
func TestRetainedROMRejectsBeforePublishing(t *testing.T) {
	in := linkedROMFixture(t)
	a := retained(t, in.Package)
	r := a.Inspection().Descriptor.ROM
	good := ROMSource{r.ID, r.Role, in.ROM}
	for _, sources := range [][]ROMSource{nil, {good, good}, {{"wrong", r.Role, in.ROM}}, {{r.ID, "cartridge", in.ROM}}, {{r.ID, r.Role, in.ROM[:10]}}} {
		if linked, err := a.LinkROMs(context.Background(), sources); err == nil || linked != nil {
			t.Fatal("invalid source published")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if linked, err := a.LinkROMs(ctx, []ROMSource{good}); err == nil || linked != nil {
		t.Fatal("cancelled link published")
	}
	if _, err := a.LinkROMs(nil, []ROMSource{good}); err == nil {
		t.Fatal("nil context accepted")
	}
	var empty Archive
	if _, err := empty.LinkROMs(context.Background(), []ROMSource{good}); err == nil {
		t.Fatal("empty archive accepted")
	}
}
