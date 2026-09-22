package corepackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestRomInitRoundTripKeepsTheSealedPackage(t *testing.T) {
	manifest := []byte("manifest = true\n")
	payload := bytes.Repeat([]byte{0x11}, 64)
	archive := testArchive(t, manifest, payload)
	programmed := bytes.Repeat([]byte{0x22}, 64)
	image := sha256.Sum256([]byte("basic"))
	body, err := WriteRomInit(RomInit{
		Package: archive, Programmed: programmed, ImageSHA256: hex.EncodeToString(image[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !IsRomInit(body) {
		t.Fatal("archive was not recognized")
	}
	got, err := ReadRomInit(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Package, archive) || !bytes.Equal(got.Programmed, programmed) || got.ImageSHA256 != hex.EncodeToString(image[:]) || len(got.Composition) != 0 {
		t.Fatalf("round trip = package %d programmed %d image %s composition %d", len(got.Package), len(got.Programmed), got.ImageSHA256, len(got.Composition))
	}
	if _, err := ReadRomInit(append(body, 1)); err == nil {
		t.Fatal("trailing byte was accepted")
	}
}

func testArchive(t *testing.T, manifest, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, member := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		out.Write(canonicalHeader(member.name, int64(len(member.data))))
		out.Write(member.data)
		out.Write(make([]byte, (512-len(member.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}
