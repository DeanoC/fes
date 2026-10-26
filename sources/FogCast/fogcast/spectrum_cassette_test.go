package fogcast

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/spectrumsnapshots"
	"github.com/DeanoC/FogCast/internal/spectrumtapes"
	"github.com/DeanoC/FogCast/protocol"
)

func spectrumDescriptor(snapshots ...string) corepackage.Descriptor {
	interfaces := []corepackage.Interface{{ID: "fes.media.spectrum-tape", Major: 2, Required: true}}
	for _, id := range snapshots {
		interfaces = append(interfaces, corepackage.Interface{ID: id, Major: 1})
	}
	return corepackage.Descriptor{Core: corepackage.Core{ID: "example.spectrum"},
		ABI: corepackage.Contract{ID: "fes.computer", Major: 1}, Interfaces: interfaces}
}

// sna48 is a 48K .sna whose stack (and PC) sits at $8000.
func sna48() []byte {
	data := make([]byte, 27+48<<10)
	data[23], data[24] = 0x00, 0x80 // SP
	data[25] = 1                    // IM 1
	return data
}

// sna128 is a 128K .sna with bank 0 paged.
func sna128() []byte {
	return append(sna48(), make([]byte, 4+5*16<<10)...)
}

// z80TapCollision is a valid uncompressed 48K version-1 .z80 file whose
// first RAM bytes make its full contents a valid sequence of TAP blocks.
func z80TapCollision() []byte {
	data := make([]byte, 30+48<<10)
	data[0], data[1] = 0x1c, 0x00   // TAP block length 28
	data[6] = 1                     // nonzero .z80 PC
	data[30], data[31] = 0xfe, 0xbf // TAP block length 49150
	return data
}

// tap is one 19-byte header block.
func tap() []byte {
	block := append([]byte{0x00}, make([]byte, 18)...)
	return append([]byte{byte(len(block)), 0}, block...)
}

// checkedTAP is one standard data block filling a file of size bytes: its
// flag, data and checksum XOR to zero. set writes header bytes that make it a
// valid snapshot of that size too.
func checkedTAP(size int, set func(data []byte)) []byte {
	data := make([]byte, size)
	data[0], data[1], data[2] = byte(size-2), byte((size-2)>>8), 0xff
	set(data)
	sum := byte(0)
	for _, v := range data[2 : size-1] {
		sum ^= v
	}
	data[size-1] = sum
	return data
}

func cassette(t *testing.T, d corepackage.Descriptor, data []byte) ([]byte, error) {
	t.Helper()
	return namedCassette(t, d, "", data)
}

func namedCassette(t *testing.T, d corepackage.Descriptor, name string, data []byte) ([]byte, error) {
	t.Helper()
	return spectrumCassetteStream(d, name, io.NopCloser(bytes.NewReader(data)), int64(len(data)))
}

func TestSpectrumCassetteStreamChoosesTapeOrSnapshot(t *testing.T) {
	all := spectrumDescriptor(protocol.SpectrumSnapshotInterfaceID, protocol.SpectrumSnapshot128InterfaceID)
	pzx, err := cassette(t, all, tap())
	if err != nil || !bytes.HasPrefix(pzx, []byte("PZXT")) {
		t.Fatalf("tape: % x, %v", pzx[:min(len(pzx), 8)], err)
	}
	for name, data := range map[string][]byte{"48K": sna48(), "128K": sna128()} {
		stream, err := cassette(t, all, data)
		if err != nil || !bytes.HasPrefix(stream, spectrumsnapshots.Magic[:]) {
			t.Fatalf("%s snapshot: %v", name, err)
		}
	}
}

func TestSpectrumCassetteStreamPrioritizesValidSnapshotOverTAP(t *testing.T) {
	data := z80TapCollision()
	if !spectrumsnapshots.Recognize(data) || !spectrumtapes.Recognize(data) {
		t.Fatal("fixture must be recognized as both a valid snapshot and TAP")
	}
	stream, err := cassette(t, spectrumDescriptor(protocol.SpectrumSnapshotInterfaceID), data)
	if err != nil || !bytes.HasPrefix(stream, spectrumsnapshots.Magic[:]) {
		t.Fatalf("ambiguous snapshot became a tape: %v", err)
	}
}

func TestSpectrumCassetteStreamKeepsCheckedTAPOfSnapshotSize(t *testing.T) {
	d := spectrumDescriptor(protocol.SpectrumSnapshotInterfaceID)
	for name, data := range map[string][]byte{
		// A 48K .sna: SP $8000 and IM 1.
		".sna": checkedTAP(27+48<<10, func(b []byte) { b[24], b[25] = 0x80, 1 }),
		// An uncompressed version-1 .z80: PC $0001 and IM 1.
		".z80": checkedTAP(30+48<<10, func(b []byte) { b[6], b[12], b[29] = 1, 0, 1 }),
	} {
		if !spectrumsnapshots.Recognize(data) || !spectrumtapes.CheckedTAP(data) {
			t.Fatalf("%s: fixture must be both a valid snapshot and a checked TAP", name)
		}
		for file, want := range map[string]string{"": "PZXT", "game.tap": "PZXT", "game" + name: "ZXRS"} {
			stream, err := namedCassette(t, d, file, data)
			if err != nil || !bytes.HasPrefix(stream, []byte(want)) {
				t.Errorf("%s named %q: %v, want %s", name, file, err, want)
			}
		}
	}
}

func TestSpectrumCassetteStreamNameDecides(t *testing.T) {
	d := spectrumDescriptor(protocol.SpectrumSnapshotInterfaceID)
	// The unchecked collision is a snapshot unnamed, but a tape named .tap.
	stream, err := namedCassette(t, d, "game.TAP", z80TapCollision())
	if err != nil || !bytes.HasPrefix(stream, []byte("PZXT")) {
		t.Fatalf("collision named .tap: %v", err)
	}
	// A named snapshot is never read as a tape, nor a named tape as a snapshot.
	var api *protocol.APIError
	if _, err := namedCassette(t, d, "game.z80", tap()); !errors.As(err, &api) || api.Code != protocol.CodeBadRequest {
		t.Errorf("tape named .z80: %v", err)
	}
	if _, err := namedCassette(t, d, "game.tzx", sna48()); !errors.As(err, &api) || api.Code != protocol.CodeBadRequest {
		t.Errorf("snapshot named .tzx: %v", err)
	}
}

func TestSpectrumCassetteStreamRefusesSnapshotsThePackageDoesNotRun(t *testing.T) {
	for name, tc := range map[string]struct {
		d    corepackage.Descriptor
		data []byte
		want string
	}{
		"no snapshots": {spectrumDescriptor(), sna48(), "48K snapshots do not run"},
		"48K only":     {spectrumDescriptor(protocol.SpectrumSnapshotInterfaceID), sna128(), "128K snapshots do not run"},
	} {
		_, err := cassette(t, tc.d, tc.data)
		var api *protocol.APIError
		if !errors.As(err, &api) || api.Code != protocol.CodeBadRequest || !strings.Contains(api.Message, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
