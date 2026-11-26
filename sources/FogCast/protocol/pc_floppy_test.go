package protocol

import (
	"net/http"
	"strings"
	"testing"
)

func TestPcFloppyDrivesAndSizes(t *testing.T) {
	for size, want := range map[int64]uint8{163840: 2, 184320: 2, 327680: 2, 368640: 2, 737280: 1, 1228800: 2, 1474560: 1, 1720320: 1} {
		if got, ok := PcFloppyDrive(size); !ok || got != want || !AdmitPcFloppySize(size) {
			t.Fatal(size, got, ok)
		}
	}
	for _, size := range []int64{0, 2949120, 1474560 - 512, 1474560 + 1, 1474560 * 2} {
		if _, ok := PcFloppyDrive(size); ok || AdmitPcFloppySize(size) {
			t.Fatal("admitted", size)
		}
	}
	if m := PcFloppySizeError(PcFloppyUnitA, 1228800).Message; !strings.Contains(m, "put it in B:") {
		t.Fatal(m)
	}
	if m := PcFloppySizeError(PcFloppyUnitB, 1474560).Message; !strings.Contains(m, "put it in A:") {
		t.Fatal(m)
	}
	if m := PcFloppySizeError(PcFloppyUnitA, 2949120).Message; !strings.Contains(m, "2.88M") {
		t.Fatal(m)
	}
}

func TestPcFloppyUnitStatus(t *testing.T) {
	no := false
	empty := MediaUnitStatus{Unit: PcFloppyUnitA, Interface: PcFloppyInterface(), MinBytes: uint32(PcFloppyMinBytes),
		MaxBytes: uint32(PcFloppyMaxBytes), ChunkBytes: 512, State: MediaUnitEmpty, Saveable: &no}
	protected := empty
	protected.Unit, protected.State = PcFloppyUnitB, MediaUnitReady
	writable := empty
	writable.State = MediaUnitReady
	writable.Persistence = &MediaDataStatus{Mode: "persistent", GameID: "freedos", BaseMediaID: strings.Repeat("c", 64), Revision: "absent"}
	for _, u := range []MediaUnitStatus{empty, protected, writable} {
		if !u.Valid() {
			t.Fatal(u)
		}
	}
	yes := true
	bad := []MediaUnitStatus{empty, empty, writable, writable, empty}
	bad[0].Unit = 3
	bad[1].Saveable = &yes
	bad[2].State = MediaUnitEmpty
	bad[3].Unit = 0
	bad[4].MaxBytes = uint32(PcFloppyMaxBytes) - 512
	for i, u := range bad {
		if u.Valid() {
			t.Fatal("accepted", i)
		}
	}
}

func TestPcFloppyRecordUnitIsPc386Only(t *testing.T) {
	id := MediaRecordIdentity{CoreID: Pc386CoreID, GameID: "freedos", BaseMediaID: strings.Repeat("d", 64), Unit: PcFloppyRecordUnit}
	if !id.Valid() {
		t.Fatal(id)
	}
	for _, change := range []func(*MediaRecordIdentity){
		func(i *MediaRecordIdentity) { i.Unit = PcFloppyUnitB },
		func(i *MediaRecordIdentity) { i.CoreID = "fes.atari-st" },
	} {
		bad := id
		change(&bad)
		if bad.Valid() {
			t.Fatal("accepted", bad)
		}
	}
}

func TestLibraryMachineFloppyHeaders(t *testing.T) {
	b := LibraryMachineBinding{GameID: "msdos-622", Floppies: []StagedFloppy{
		{Unit: PcFloppyUnitA, SHA256: strings.Repeat("e", 64), Size: 1474560},
		{Unit: PcFloppyUnitB, SHA256: strings.Repeat("f", 64), Size: 1228800, WriteProtected: true}}}
	if !b.Valid() {
		t.Fatal(b)
	}
	h := make(http.Header)
	b.SetHeaders(h)
	got, ok := LibraryMachineHeaders(h)
	if !ok || got == nil || len(got.Floppies) != 2 || got.Disk != nil {
		t.Fatal(got, ok)
	}
	for i := range got.Floppies {
		got.Floppies[i].Size = b.Floppies[i].Size // the kit fills it from the staged file
	}
	if !got.Valid() || got.Floppies[0] != b.Floppies[0] || got.Floppies[1] != b.Floppies[1] {
		t.Fatal(got)
	}
	// One disk per drive; a drive the size does not fit; bad header forms.
	twice := b
	twice.Floppies = []StagedFloppy{b.Floppies[0], b.Floppies[0]}
	wrong := b
	wrong.Floppies = []StagedFloppy{{Unit: PcFloppyUnitB, SHA256: b.Floppies[0].SHA256, Size: 1474560}}
	if twice.Valid() || wrong.Valid() {
		t.Fatal("bad binding accepted")
	}
	for _, values := range [][]string{
		{"1:" + strings.Repeat("e", 64), "1:" + strings.Repeat("f", 64)},
		{"3:" + strings.Repeat("e", 64)},
		{"1:" + strings.Repeat("e", 64) + ":ro"},
		{"1:" + strings.Repeat("E", 64)},
		{"1:a", "2:b", "1:c"},
	} {
		bad := make(http.Header)
		bad.Set(LibraryGameHeader, "msdos-622")
		for _, v := range values {
			bad.Add(FloppyImageHeader, v)
		}
		if _, ok := LibraryMachineHeaders(bad); ok {
			t.Fatal("accepted", values)
		}
	}
}
