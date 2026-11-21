package protocol

import (
	"reflect"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func required(cs ...RuntimeContract) []corepackage.Interface {
	out := make([]corepackage.Interface, 0, len(cs))
	for _, c := range cs {
		out = append(out, corepackage.Interface{ID: c.ID, Major: int64(c.Major), Minor: int64(c.Minor), Required: true})
	}
	return out
}

func mediaSaves(d corepackage.Descriptor) map[string]bool {
	out := map[string]bool{}
	for _, c := range DeclaredCoreMediaCapabilities(d) {
		out[c.Role] = CoreMediaSaves(d, c)
	}
	return out
}

// The descriptors follow the producers' manifests: the ST and C64 OSS
// builds and the PC/XT Quartus build. A writable floppy needs its base
// interface and its write companion, both required, and the PC/XT also
// needs ABI 1.1.
func TestCoreMediaSavesFollowsTheDeclaredWriteContract(t *testing.T) {
	computer := func(minor int64, interfaces []corepackage.Interface) corepackage.Descriptor {
		return corepackage.Descriptor{ABI: corepackage.Contract{ID: ComputerABIID, Major: 1, Minor: minor}, Interfaces: interfaces}
	}
	stOptionalWrite := required(AtariStFloppyInterface())
	stOptionalWrite = append(stOptionalWrite, corepackage.Interface{ID: AtariStFloppyWriteInterface().ID, Major: 1, Minor: 0})
	cases := []struct {
		name string
		d    corepackage.Descriptor
		want map[string]bool
	}{
		{"ST writes", computer(0, required(AtariStFloppyInterface(), AtariStFloppyWriteInterface())), map[string]bool{DiskRole: true}},
		{"ST without write", computer(0, required(AtariStFloppyInterface())), map[string]bool{DiskRole: false}},
		{"ST write not required", computer(0, stOptionalWrite), map[string]bool{DiskRole: false}},
		{"C64 1541", computer(0, required(C64DiskInterface())), map[string]bool{DiskRole: false}},
		{"PC/XT 1.1 writes", computer(1, required(PcxtFloppyInterface(), PcxtFloppyWriteInterface())), map[string]bool{DiskRole: true}},
		{"PC/XT 1.1 without write", computer(1, required(PcxtFloppyInterface())), map[string]bool{DiskRole: false}},
		{"pc386 hard disk", computer(1, required(AtaDiskInterface())), map[string]bool{HardDiskRole: true}},
		{"Spectrum tape", computer(0, required(SpectrumTapeInterface())), map[string]bool{CassetteRole: false}},
		{"ZX81 SAVE makes a new cassette", computer(0, required(Zx81TapeInterface(), Zx81TapeWriteInterface())), map[string]bool{CassetteRole: false}},
	}
	for _, tc := range cases {
		if got := mediaSaves(tc.d); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: media saves %v, want %v", tc.name, got, tc.want)
		}
	}
	// PC/XT on ABI 1.0 declares no drive the host binds, and its floppy
	// does not write even with both interfaces.
	old := computer(0, required(PcxtFloppyInterface(), PcxtFloppyWriteInterface()))
	if DescriptorMediaWrite(old, PcxtFloppyInterface()) {
		t.Fatal("a PC/XT ABI 1.0 package was taken as writing its floppy")
	}
}
