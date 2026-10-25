package fogcast

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/spectrumsnapshots"
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

// tap is one 19-byte header block.
func tap() []byte {
	block := append([]byte{0x00}, make([]byte, 18)...)
	return append([]byte{byte(len(block)), 0}, block...)
}

func cassette(t *testing.T, d corepackage.Descriptor, data []byte) ([]byte, error) {
	t.Helper()
	return spectrumCassetteStream(d, io.NopCloser(bytes.NewReader(data)), int64(len(data)))
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
