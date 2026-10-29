package protocol

import (
	"encoding/binary"
	"net/http"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

// mbr builds a synthetic master boot record with one partition entry.
func mbr(endHead, endSector byte, sectors uint32) []byte {
	data := make([]byte, 512)
	entry := data[446:462]
	entry[0] = 0x80
	entry[4] = 0x06 // FAT16
	entry[5], entry[6], entry[7] = endHead, endSector, 0xff
	binary.LittleEndian.PutUint32(entry[8:], 63)
	binary.LittleEndian.PutUint32(entry[12:], sectors)
	data[510], data[511] = 0x55, 0xaa
	return data
}

func TestInferDiskGeometry(t *testing.T) {
	for _, tc := range []struct {
		name string
		mbr  []byte
		size int64
		want DiskGeometry
	}{
		{"partition-end-chs", mbr(15, 63, 100000), 64 << 20, DiskGeometry{130, 16, 63}},
		{"dosbox-style-heads", mbr(63, 32, 100000), 128 << 20, DiskGeometry{128, 64 - 48, 32}},
		{"no-signature", make([]byte, 512), 32 << 20, DiskGeometry{65, 16, 63}},
		{"empty-table", mbr(0, 0, 0), 2 << 20, DiskGeometry{4, 16, 63}},
		{"small-geometry", mbr(3, 17, 2000), 1 << 20, DiskGeometry{30, 4, 17}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "dosbox-style-heads" {
				// 64 heads cannot be reported; the entry is skipped and the
				// default geometry applies.
				tc.want = DiskGeometry{260, 16, 63}
			}
			got, err := InferDiskGeometry(tc.mbr, tc.size)
			if err != nil || got != tc.want || !got.Valid(tc.size) {
				t.Fatalf("got %+v %v, want %+v", got, err, tc.want)
			}
		})
	}
	if _, err := InferDiskGeometry(nil, 1<<20+1); err == nil {
		t.Fatal("partial sector admitted")
	}
	if _, err := InferDiskGeometry(nil, 129<<20); err == nil {
		t.Fatal("oversize image admitted")
	}
	// The cylinder count is capped at 16383 and never exceeds the image.
	got, err := InferDiskGeometry(mbr(0, 1, 1), 128<<20)
	if err != nil || got != (DiskGeometry{16383, 1, 1}) {
		t.Fatalf("cap: %+v %v", got, err)
	}
}

func TestDiskGeometryHeaderAndLimits(t *testing.T) {
	g := DiskGeometry{Cylinders: 260, Heads: 16, SectorsPerTrack: 63}
	if parsed, ok := ParseDiskGeometry(g.String()); !ok || parsed != g || !g.Valid(128<<20) {
		t.Fatalf("round trip %v %v", parsed, ok)
	}
	for _, bad := range []string{"", "0/16/63", "520/16", "520/16/63/1", "0520/16/63", "520/16/x", "99999999/1/1"} {
		if parsed, ok := ParseDiskGeometry(bad); ok && parsed.Valid(128<<20) {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, bad := range []DiskGeometry{{16384, 1, 1}, {1, 17, 1}, {1, 1, 64}, {1000, 16, 63}} {
		if bad.Valid(64 << 20) {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestMachineSettings(t *testing.T) {
	if d := DefaultMachineSettings(); !d.Valid() || d.String() != "0,0,0,0,544,7,1" {
		t.Fatalf("defaults %+v", d)
	}
	good := MachineSettings{Preset: 3, Frequency: 250, CodeProfile: 1, Turbo: 1, SBBase: 0x240, SBIRQ: 5, SBDMA: 1}
	if parsed, ok := ParseMachineSettings(good.String()); !ok || parsed != good || !good.Valid() {
		t.Fatalf("round trip %+v %v", parsed, ok)
	}
	bad := []MachineSettings{
		{Preset: 8, SBBase: 0x220, SBIRQ: 7, SBDMA: 1},
		{Frequency: 250, SBBase: 0x220, SBIRQ: 7, SBDMA: 1},
		{Preset: 1, Frequency: 9, SBBase: 0x220, SBIRQ: 7, SBDMA: 1},
		{Preset: 1, Frequency: 1001, SBBase: 0x220, SBIRQ: 7, SBDMA: 1},
		{CodeProfile: 2, SBBase: 0x220, SBIRQ: 7, SBDMA: 1},
		{Turbo: 2, SBBase: 0x220, SBIRQ: 7, SBDMA: 1},
		{SBBase: 0x230, SBIRQ: 7, SBDMA: 1},
		{SBBase: 0x220, SBIRQ: 9, SBDMA: 1},
		{SBBase: 0x220, SBIRQ: 4, SBDMA: 1},
		{SBBase: 0x220, SBIRQ: 7, SBDMA: 3},
	}
	for _, m := range bad {
		if m.Valid() {
			t.Errorf("%+v accepted", m)
		}
	}
	for _, irq := range []uint16{2, 3, 5, 7} {
		if m := (MachineSettings{SBBase: 0x220, SBIRQ: irq, SBDMA: 1}); !m.Valid() {
			t.Errorf("IRQ %d refused", irq)
		}
	}
	for _, header := range []string{"0,0,0,0,544,7", "0,0,0,0,544,7,1,0", "0,0,0,0,0544,7,1", "0,0,0,0,544,7,-1", "0,0,0,0,544,7,70000"} {
		if _, ok := ParseMachineSettings(header); ok {
			t.Errorf("%q parsed", header)
		}
	}
}

func TestLibraryMachineHeaders(t *testing.T) {
	h := http.Header{}
	if b, ok := LibraryMachineHeaders(h); b != nil || !ok {
		t.Fatal("absent binding")
	}
	disk := &StagedDisk{SHA256: "ab" + "cd0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", Size: 64 << 20,
		Geometry: DiskGeometry{130, 16, 63}}
	LibraryMachineBinding{GameID: "ultima-vi", Disk: disk}.SetHeaders(h)
	b, ok := LibraryMachineHeaders(h)
	if !ok || b == nil || b.GameID != "ultima-vi" || b.Disk == nil || b.Disk.SHA256 != disk.SHA256 || b.Disk.Geometry != disk.Geometry || b.Disk.Size != 0 {
		t.Fatalf("round trip %+v %v", b, ok)
	}
	h.Del(LibraryGameHeader)
	if _, ok := LibraryMachineHeaders(h); ok {
		t.Fatal("disk without game accepted")
	}
	h = http.Header{}
	h.Set(LibraryGameHeader, "Bad Game")
	if _, ok := LibraryMachineHeaders(h); ok {
		t.Fatal("invalid game accepted")
	}
}

func TestComputer11Media(t *testing.T) {
	if !ComputerABI(ComputerABIID, 1, 0) || !ComputerABI(ComputerABIID, 1, 1) || ComputerABI(ComputerABIID, 1, 2) {
		t.Fatal("fes.computer minors")
	}
	descriptor := corepackage.Descriptor{ABI: corepackage.Contract{ID: ComputerABIID, Major: 1, Minor: 1}}
	for _, i := range []RuntimeContract{AtaDiskInterface(), PersistenceWordsInterface(), Pc386MachineInterface()} {
		descriptor.Interfaces = append(descriptor.Interfaces, corepackage.Interface{ID: i.ID, Major: int64(i.Major), Minor: int64(i.Minor), Required: true})
	}
	if !DeclaresAtaDisk(descriptor) || !DeclaresPc386Machine(descriptor) {
		t.Fatal("pc386 declarations")
	}
	media := DeclaredCoreMediaCapabilities(descriptor)
	if len(media) != 1 || media[0].Role != HardDiskRole || media[0].MaxBytes != 128<<20 || media[0].MinBytes != 1<<20 || *media[0].Unit != 0 {
		t.Fatalf("media %+v", media)
	}
	if DeclaresDiskMedia(descriptor) {
		t.Fatal("the hard disk is not a removable disk")
	}
	descriptor.ABI.Minor = 0
	if DeclaresAtaDisk(descriptor) {
		t.Fatal("1.0 cannot declare the disk")
	}
	saveable := false
	unit := MediaUnitStatus{Unit: 0, Interface: AtaDiskInterface(), MinBytes: 1 << 20, MaxBytes: 128 << 20, ChunkBytes: 65536,
		State: MediaUnitEmpty, Saveable: &saveable}
	if !unit.Valid() {
		t.Fatal("ata unit refused")
	}
	unit.ChunkBytes = 512
	if unit.Valid() {
		t.Fatal("512-byte ata chunks accepted")
	}
}
