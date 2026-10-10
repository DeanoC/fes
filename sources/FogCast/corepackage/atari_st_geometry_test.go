package corepackage

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"
)

func geometryBase(g AtariStGeometry) []byte {
	data := make([]byte, g.Tracks*g.Heads*g.Sectors*512)
	binary.LittleEndian.PutUint16(data[11:13], 512)
	binary.LittleEndian.PutUint16(data[19:21], uint16(len(data)/512))
	binary.LittleEndian.PutUint16(data[24:26], uint16(g.Sectors))
	binary.LittleEndian.PutUint16(data[26:28], uint16(g.Heads))
	return data
}
func TestAtariStBaseGeometryAdmission(t *testing.T) {
	seen := map[int]bool{}
	for tracks := 80; tracks <= 82; tracks++ {
		for heads := 1; heads <= 2; heads++ {
			for sectors := 9; sectors <= 10; sectors++ {
				g := AtariStGeometry{tracks, heads, sectors}
				data := geometryBase(g)
				if seen[len(data)] {
					t.Fatal("ambiguous geometry")
				}
				seen[len(data)] = true
				got, ok := AtariStGeometryForSize(int64(len(data)))
				if !ok || got != g || !ValidAtariStBase(data, true) {
					t.Fatalf("valid shape refused: %+v", g)
				}
				if len(data) != InitialMediaBytes {
					if ValidAtariStBase(data, false) {
						t.Fatal("legacy admitted extension")
					}
					for _, offset := range []int{11, 19, 24, 26} {
						bad := bytes.Clone(data)
						bad[offset] ^= 1
						if ValidAtariStBase(bad, true) {
							t.Fatalf("mismatched BPB admitted at %d", offset)
						}
					}
				}
				if ValidAtariStBase(data[:len(data)-1], true) {
					t.Fatal("truncated geometry admitted")
				}
			}
		}
	}
	if len(seen) != 12 || !ValidAtariStBase(bytes.Repeat([]byte{0xa5}, InitialMediaBytes), false) {
		t.Fatal("legacy or bounded layout set changed")
	}
	for _, size := range []int64{-1, 0, 368641, 839681, 1024000} {
		if _, ok := AtariStGeometryForSize(size); ok {
			t.Fatal("invalid size admitted", size)
		}
	}
}
func TestExtendedInitialMediaRoundTrip(t *testing.T) {
	for _, g := range []AtariStGeometry{{80, 1, 10}, {82, 2, 10}} {
		t.Run(fmt.Sprint(g), func(t *testing.T) {
			in := initialSTInput(t)
			in.Parts = nil
			manifest, base, mapping, err := readArchive(in.Package)
			if err != nil {
				t.Fatal(err)
			}
			data := geometryBase(g)
			in.InitialMedia.Bytes = data
			in.InitialMedia.BaseMediaID = romDigest(data)
			if _, err := PrepareROMInput(context.Background(), in); err == nil {
				t.Fatal("non720 admitted without opt-in")
			}
			manifest = append(manifest, []byte("\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-geometry\"\nmajor = 1\nminor = 0\nrequired = true\n")...)
			in.Package = romArchive(manifest, base, mapping)
			prepared, err := PrepareROMInput(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			staged, err := StageROMInputBytes(context.Background(), t.TempDir(), prepared.Data)
			if err != nil {
				t.Fatal(err)
			}
			defer staged.Cleanup()
			if staged.InitialMedia == nil || staged.InitialMedia.Size != int64(len(data)) {
				t.Fatal("initial disk size lost", staged.InitialMedia)
			}
			adopted, err := Adopt(staged.root)
			if err != nil || len(adopted) != 1 || adopted[0].InitialMedia.Size != int64(len(data)) {
				t.Fatal("variable disk not adopted", err)
			}
			bad := in
			bad.InitialMedia = &InitialMedia{GameID: in.InitialMedia.GameID, BaseMediaID: in.InitialMedia.BaseMediaID, Bytes: bytes.Clone(data)}
			bad.InitialMedia.Bytes[24] ^= 1
			bad.InitialMedia.BaseMediaID = romDigest(bad.InitialMedia.Bytes)
			if _, err := PrepareROMInput(context.Background(), bad); err == nil {
				t.Fatal("mismatched BPB admitted")
			}
		})
	}
}
