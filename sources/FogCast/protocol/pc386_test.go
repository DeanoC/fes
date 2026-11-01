package protocol

import (
	"encoding/binary"
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

// chs encodes an LBA in the three-byte MBR form for heads/sectors, with a
// cylinder past 1023 written as 1023 the way partitioning tools do.
func chs(lba, heads, sectors uint32) [3]byte {
	c, h, s := lba/(heads*sectors), lba/sectors%heads, lba%sectors+1
	if c > 1023 {
		c, h, s = 1023, heads-1, sectors
	}
	return [3]byte{byte(h), byte(s) | byte(c>>8)<<6, byte(c)}
}

type partition struct{ start, end, heads, sectors uint32 }

// mbr builds a master boot record whose entries a tool wrote for each
// partition's geometry.
func mbr(partitions ...partition) []byte {
	data := make([]byte, 512)
	for i, p := range partitions {
		entry := data[446+16*i : 462+16*i]
		entry[4] = 0x06 // FAT16
		first, last := chs(p.start, p.heads, p.sectors), chs(p.end, p.heads, p.sectors)
		copy(entry[1:4], first[:])
		copy(entry[5:8], last[:])
		binary.LittleEndian.PutUint32(entry[8:], p.start)
		binary.LittleEndian.PutUint32(entry[12:], p.end-p.start+1)
	}
	data[510], data[511] = 0x55, 0xaa
	return data
}

func TestInferDiskGeometry(t *testing.T) {
	const mib = 1 << 20
	lbaOnly := mbr(partition{63, 64*mib/512 - 1, 16, 63})
	for _, at := range []int{447, 448, 449, 451, 452, 453} {
		lbaOnly[at] = 0
	}
	for _, tc := range []struct {
		name string
		mbr  []byte
		size int64
		want DiskGeometry
	}{
		{"dos-fdisk-cylinder-aligned", mbr(partition{63, 130*16*63 - 1, 16, 63}), 64 * mib, DiskGeometry{130, 16, 63}},
		// 204800 sectors are 203 cylinders and 176 sectors: the end is
		// mid-cylinder, and its end CHS alone would read 3 heads, 50 sectors.
		{"end-at-last-lba", mbr(partition{63, 100*mib/512 - 1, 16, 63}), 100 * mib, DiskGeometry{203, 16, 63}},
		// Its CHS fields also fit 18..28 heads; only 16 can be reported.
		{"mib-aligned-16-63", mbr(partition{2048, 32*mib/512 - 1, 16, 63}), 32 * mib, DiskGeometry{65, 16, 63}},
		{"small-geometry", mbr(partition{17, 30*4*17 - 1, 4, 17}), 1 * mib, DiskGeometry{30, 4, 17}},
		{"two-partitions", mbr(partition{63, 50*16*63 - 1, 16, 63}, partition{50 * 16 * 63, 130*16*63 - 1, 16, 63}), 64 * mib, DiskGeometry{130, 16, 63}},
		// Past cylinder 1023 every CHS reads 1023; 2..7 heads fit, and the
		// end's own heads and sectors pick 4/32.
		{"cylinder-overflow", mbr(partition{32, 128*mib/512 - 1, 4, 32}), 128 * mib, DiskGeometry{2048, 4, 32}},
		{"lba-only-entry", lbaOnly, 64 * mib, DiskGeometry{130, 16, 63}},
		{"no-signature", make([]byte, 512), 32 * mib, DiskGeometry{65, 16, 63}},
		{"empty-table", mbr(), 2 * mib, DiskGeometry{4, 16, 63}},
		// The cylinder count is capped at 16383 and never exceeds the image.
		{"cylinder-cap", mbr(partition{1, 128*mib/512 - 1, 1, 1}), 128 * mib, DiskGeometry{16383, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InferDiskGeometry(tc.mbr, tc.size)
			if err != nil || got != tc.want || !got.Valid(tc.size) {
				t.Fatalf("got %+v %v, want %+v", got, err, tc.want)
			}
		})
	}
	mismatched := mbr(partition{63, 64*mib/512 - 1, 16, 63})
	mismatched[453] = 3 // end cylinder 3 cannot be the last LBA
	misaligned := mbr(partition{63, 64*mib/512 - 1, 16, 63}, partition{0, 999, 16, 63})
	misaligned[462+5], misaligned[462+6], misaligned[462+7] = 15, 63, 0 // the old end-head reading
	for _, tc := range []struct {
		name, want string
		mbr        []byte
		size       int64
	}{
		{"util-linux-255-63", "written for 255 heads and 63 sectors per track", mbr(partition{2048, 16*mib/512 - 1, 255, 63}), 16 * mib},
		{"util-linux-255-63-64mib", "written for 255 heads and 63 sectors per track", mbr(partition{2048, 64*mib/512 - 1, 255, 63}), 64 * mib},
		// Inside cylinder 0, 33..255 heads all fit and none is named.
		{"util-linux-cylinder-0", "written for more than 16 heads", mbr(partition{63, 2047, 255, 63}), 1 * mib},
		{"chs-disagrees-with-lba", "match no geometry", mismatched, 64 * mib},
		{"entries-disagree", "match no geometry", misaligned, 64 * mib},
		// Wholly inside cylinder 0 with 63 sectors, any head count above 1
		// fits, and the end's own 2 heads and 38 sectors do not.
		{"ambiguous", "more than one geometry", mbr(partition{63, 100, 16, 63}), 1 * mib},
		{"partial-sector", "1 to 128 MiB", nil, 1*mib + 1},
		{"oversize", "1 to 128 MiB", nil, 129 * mib},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InferDiskGeometry(tc.mbr, tc.size)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %+v %v, want an error naming %q", got, err, tc.want)
			}
		})
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
