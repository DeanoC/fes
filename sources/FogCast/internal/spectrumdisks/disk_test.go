package spectrumdisks

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// trd is a TR-DOS image of the given disk type whose sector bytes encode
// their logical track, sector and offset.
func trd(kind byte, tracks int) []byte {
	data := make([]byte, tracks*trdosTrackBytes)
	for i := range data {
		data[i] = byte(i/trdosTrackBytes*7 + i/trdosSectorBytes%trdosSectors*3 + i%trdosSectorBytes)
	}
	info := data[trdosInfo:]
	info[227], info[231] = kind, trdosID
	return data
}

// scl is a .scl image of three files, the second deleted.
func scl() []byte {
	data := append([]byte("SINCLAIR"), 3)
	for i, sectors := range []byte{2, 1, 17} {
		entry := []byte("FILE    B\x00\x00\x00\x00")
		entry[4] = '0' + byte(i)
		if i == 1 {
			entry[0] = 0x01
		}
		data = append(data, append(entry, sectors)...)
	}
	for i := 0; i < 20*trdosSectorBytes; i++ {
		data = append(data, byte(i*5))
	}
	return append(data, 0, 0, 0, 0) // checksum, read as file data as Fuse does
}

// sclOf is a .scl directory of files of the given sector count, without data.
func sclOf(files int, sectors byte) []byte {
	data := append([]byte("SINCLAIR"), byte(files))
	for i := 0; i < files; i++ {
		data = append(data, append([]byte("FILE    B\x00\x00\x00\x00"), sectors)...)
	}
	return data
}

// sector finds sector r in a raw track as a controller does: an ID mark
// after three A1 bytes with missing clocks, the ID's CRC, then the data
// mark's A1 sync and the data's CRC.
func sector(t *testing.T, track *Track, cylinder, head, r byte) []byte {
	t.Helper()
	b, c := track.Bytes, track.Clocks
	marked := func(i int) bool {
		return i >= 3 && c[i-3] && c[i-2] && c[i-1] && b[i-3] == syncMark && b[i-2] == syncMark && b[i-1] == syncMark
	}
	crc := func(from, to int) uint16 {
		v := uint16(0xffff)
		for _, x := range b[from:to] {
			v = crcFDC(v, x)
		}
		return v
	}
	for i := 3; i+7 < len(b); i++ {
		if !marked(i) || b[i] != idMark || b[i+3] != r {
			continue
		}
		if b[i+1] != cylinder || b[i+2] != head || crc(i-3, i+7) != 0 {
			t.Fatalf("sector %d: ID % x", r, b[i:i+7])
		}
		n := 128 << b[i+4]
		for j := i + 7; j+n+2 < len(b); j++ {
			if marked(j) && b[j] == dataMark {
				if crc(j-3, j+1+n+2) != 0 {
					t.Fatalf("sector %d: data CRC", r)
				}
				return b[j+1 : j+1+n]
			}
		}
	}
	t.Fatalf("cylinder %d head %d: no sector %d", cylinder, head, r)
	return nil
}

func TestTRDGeometryAndSectors(t *testing.T) {
	for _, tc := range []struct {
		kind             byte
		tracks           int
		cylinders, sides int
	}{
		{0x16, 160, 80, 2}, {0x17, 80, 40, 2}, {0x18, 80, 80, 1}, {0x19, 40, 40, 1},
		{0x16, 40, 80, 2},  // a short image is zero-filled
		{0x16, 166, 83, 2}, // a long image adds cylinders
	} {
		data := trd(tc.kind, tc.tracks)
		d, err := Read(data)
		if err != nil || d.Cylinders != tc.cylinders || d.Sides != tc.sides {
			t.Fatalf("type %#x %d tracks: %+v %v", tc.kind, tc.tracks, d, err)
		}
		for logical := 0; logical < tc.cylinders*tc.sides; logical++ {
			cylinder, side := logical/tc.sides, logical%tc.sides
			track := d.Tracks[cylinder][side]
			if len(track.Bytes) != ddBytes {
				t.Fatalf("track %d: %d bytes", logical, len(track.Bytes))
			}
			for r := 1; r <= trdosSectors; r++ {
				want := make([]byte, trdosSectorBytes)
				copy(want, data[min(len(data), (logical*trdosSectors+r-1)*trdosSectorBytes):])
				if got := sector(t, track, byte(cylinder), byte(side), byte(r)); !bytes.Equal(got, want) {
					t.Fatalf("type %#x track %d sector %d differs", tc.kind, logical, r)
				}
			}
		}
	}
}

func TestInterleave(t *testing.T) {
	order := interleave2(16)
	at := make([]int, 16)
	for s, p := range order {
		at[p] = s + 1
	}
	want := []int{1, 9, 2, 10, 3, 11, 4, 12, 5, 13, 6, 14, 7, 15, 8, 16}
	for i := range want {
		if at[i] != want[i] {
			t.Fatalf("positions hold sectors %v", at)
		}
	}
}

func TestSCLDirectory(t *testing.T) {
	data := scl()
	d, err := Read(data)
	if err != nil || d.Cylinders != 80 || d.Sides != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	dir := sector(t, d.Tracks[0][0], 0, 0, 1)
	for i, want := range [][2]byte{{0, 1}, {2, 1}, {3, 1}} { // start sector, track
		if e := dir[16*i:]; e[14] != want[0] || e[15] != want[1] || e[13] != data[9+14*i+13] {
			t.Fatalf("entry %d: % x", i, e[:16])
		}
	}
	info := sector(t, d.Tracks[0][0], 0, 0, 9)
	free := 2544 - 20
	if info[225] != 4 || info[226] != 2 || info[227] != 0x16 || info[228] != 3 ||
		int(info[229])|int(info[230])<<8 != free || info[231] != trdosID || info[244] != 1 ||
		string(info[245:253]) != "FUSE-SCL" {
		t.Fatalf("descriptor % x", info[224:])
	}
	// Files start on track 1 (cylinder 0 side 1), sector 1.
	if got := sector(t, d.Tracks[0][1], 0, 1, 1); !bytes.Equal(got, data[9+14*3:][:trdosSectorBytes]) {
		t.Fatal("first file sector differs")
	}
}

// The images below were compared track by track with the raw tracks Fuse
// 1.6.0's disk.c builds for the same input (open_trd, open_scl). The .scl
// descriptor differs from Fuse only in byte 244, the deleted file count,
// which Fuse reads from the wrong buffer.
func TestImagesMatchFuse(t *testing.T) {
	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"trd 80x2": {trd(0x16, 160), "e5d21a5d29b2b1f1afea4dbf0069268b0634f4d42fabb50be45f49b4abcd1bc0"},
		"trd 40x1": {trd(0x19, 40), "a8c9d83b77daa0038d7dbdb62279468b7bfbf6046af0a8d9c26381d298e5b897"},
		"scl":      {scl(), "b5ba4d123024fa884d9967f1b93022bbf39796715d9b86521fb55037d222ffbd"},
	} {
		image, err := Convert(tc.data, false)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(image)
		if got := hex.EncodeToString(sum[:]); got != tc.want {
			t.Errorf("%s: image sha256 %s", name, got)
		}
	}
}

func TestImageLayout(t *testing.T) {
	image, err := Convert(trd(0x18, 80), true)
	if err != nil {
		t.Fatal(err)
	}
	// A single-sided disk ends at cylinder 79 side 0.
	if len(image) != (1+2*79+1)*SlotBytes || !bytes.HasPrefix(image, []byte{'Z', 'X', 'D', 'K', Version, 80, 1, 2}) {
		t.Fatalf("%d bytes, header % x", len(image), image[:8])
	}
	d, _ := Read(trd(0x18, 80))
	for c := 0; c < 80; c++ {
		side1 := image[(2+2*c)*SlotBytes:]
		if c < 79 && binary.LittleEndian.Uint16(side1) != 0 {
			t.Fatalf("cylinder %d side 1 is formatted", c)
		}
		slot := image[(1+2*c)*SlotBytes:][:SlotBytes]
		track := d.Tracks[c][0]
		if binary.LittleEndian.Uint16(slot) != ddBytes || !bytes.Equal(slot[trackData:trackData+ddBytes], track.Bytes) {
			t.Fatalf("cylinder %d slot differs", c)
		}
		for i, clock := range track.Clocks {
			if (slot[trackClocks+i/8]>>(i%8)&1 == 1) != clock {
				t.Fatalf("cylinder %d clock %d", c, i)
			}
		}
	}
}

func TestRefusals(t *testing.T) {
	bad := trd(0x16, 160)
	bad[trdosInfo+231] = 0
	for name, data := range map[string][]byte{
		"no TR-DOS ID":  bad,
		"short":         trd(0x16, 160)[:trdosInfo],
		"scl no files":  []byte("SINCLAIR\x00"),
		"scl truncated": []byte("SINCLAIR\x02FILE    B"),
		"scl too big":   sclOf(11, 255),
	} {
		if _, err := Convert(data, false); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A launch disk without a boot program gets Fuse's autoload loader: a BASIC
// "boot" that runs the first BASIC program, in the next free sector, with
// the directory and descriptor updated. Across the 907 local .trd and .scl
// images Convert accepts, the launch images match Fuse 1.6.0's autoload
// tracks except where Fuse rewrites the descriptor and clears its unused
// bytes (and, for two .scl images, its deleted-file count).
func TestLaunchDiskGetsABootLoader(t *testing.T) {
	data := scl()
	d, err := readTRDOS(data)
	if err != nil {
		t.Fatal(err)
	}
	d.addBootLoader()
	info := d.data[trdosInfo:]
	entry := d.data[16*3:][:16]
	if string(entry[:9]) != "boot    B" || entry[13] != 1 || entry[14] != 4 || entry[15] != 2 {
		t.Fatalf("boot entry % x", entry)
	}
	program := []byte{0x00, 0x01, 0x1c, 0x00, 0xf9, 0xc0, '1', '5', '6', '1', '9', 0x0e, 0x00, 0x00, 0x03, 0x3d,
		0x00, ':', 0xea, ':', 0xf7, '"', 'F', 'I', 'L', 'E', '0', ' ', ' ', ' ', '"', 0x0d}
	if int(entry[9])|int(entry[10])<<8 != len(program) || int(entry[11])|int(entry[12])<<8 != len(program) {
		t.Fatalf("boot lengths % x", entry[9:13])
	}
	file := d.data[(2*16+4)*trdosSectorBytes:][:trdosSectorBytes]
	if !bytes.Equal(file[:len(program)], program) || !bytes.Equal(file[len(program):len(program)+4], []byte{0x80, 0xaa, 1, 0}) ||
		!bytes.Equal(file[len(program)+4:], make([]byte, trdosSectorBytes-len(program)-4)) {
		t.Fatalf("boot file % x", file[:48])
	}
	if info[225] != 5 || info[226] != 2 || info[228] != 4 || int(info[229])|int(info[230])<<8 != 2544-21 {
		t.Fatalf("descriptor % x", info[224:232])
	}
	// A second pass, a disk with a boot program, and one without a BASIC
	// program are left alone.
	before := append([]byte(nil), d.data...)
	d.addBootLoader()
	if !bytes.Equal(before, d.data) {
		t.Fatal("a disk with a boot program changed")
	}
	code := sclOf(1, 1)
	code[9+8] = 'C'
	plain, err := readTRDOS(append(code, make([]byte, 256)...))
	if err != nil {
		t.Fatal(err)
	}
	before = append([]byte(nil), plain.data...)
	plain.addBootLoader()
	if !bytes.Equal(before, plain.data) {
		t.Fatal("a disk without a BASIC program changed")
	}
	// Convert adds it only for a launch.
	launch, _ := Convert(data, true)
	swap, _ := Convert(data, false)
	if bytes.Equal(launch[SlotBytes:], swap[SlotBytes:]) || launch[7] != 2 || swap[7] != 0 {
		t.Fatal("launch and swapped images do not differ as expected")
	}
}
