package spectrumdisks

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
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
		"trd 80x2": {trd(0x16, 160), "93c538efd803dab656dc1885ec320ef4857b57a6f644e6ae13e7eb61a52cd67a"},
		"trd 40x1": {trd(0x19, 40), "0d2158f59dbf0fbd743af67a6136dbb496582fad69df489e43dc1d4fe05f7e66"},
		"scl":      {scl(), "3b85939ec889c8e37b211fe4d94934a73434612c93b3c8fdb201647c6b4200e8"},
	} {
		image, err := Convert(tc.data, TRDOS, false)
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
	image, err := Convert(trd(0x18, 80), TRDOS, true)
	if err != nil {
		t.Fatal(err)
	}
	// A single-sided disk ends at cylinder 79 side 0.
	if len(image) != (1+2*79+1)*SlotBytes || !bytes.HasPrefix(image, []byte{'Z', 'X', 'D', 'K', Version, 80, 1, 2, 13}) {
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
		if binary.LittleEndian.Uint16(slot) != ddBytes || !bytes.Equal(slot[SlotBytes/8:SlotBytes/8+ddBytes], track.Bytes) {
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
		if _, err := Convert(data, TRDOS, false); err == nil {
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
	launch, _ := Convert(data, TRDOS, true)
	swap, _ := Convert(data, TRDOS, false)
	if bytes.Equal(launch[SlotBytes:], swap[SlotBytes:]) || launch[7] != 2 || swap[7] != 0 {
		t.Fatal("launch and swapped images do not differ as expected")
	}
}

type dskSector struct {
	c, h, r, n, st1, st2 byte
	data                 []byte // stored bytes: copies of a weak sector, or data with its gap tail
}

type dskTrack struct {
	cylinder, side, mode byte // mode 2 MFM, 1 FM
	sectors              []dskSector
}

func fill(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*13) + seed
	}
	return b
}

// edsk writes an extended DSK image of the given tracks, in order.
func edsk(cylinders, sides int, tracks []dskTrack) []byte {
	hdr := make([]byte, 256)
	copy(hdr, "EXTENDED CPC DSK File\r\nDisk-Info\r\n")
	hdr[0x30], hdr[0x31] = byte(cylinders), byte(sides)
	var body []byte
	for i, t := range tracks {
		info := make([]byte, 256)
		copy(info, "Track-Info\r\n")
		info[0x10], info[0x11], info[0x13] = t.cylinder, t.side, t.mode
		info[0x14], info[0x15], info[0x16], info[0x17] = 2, byte(len(t.sectors)), 0x4e, 0xe5
		var data []byte
		for j, s := range t.sectors {
			e := info[0x18+8*j:]
			e[0], e[1], e[2], e[3], e[4], e[5] = s.c, s.h, s.r, s.n, s.st1, s.st2
			e[6], e[7] = byte(len(s.data)), byte(len(s.data)>>8)
			data = append(data, s.data...)
		}
		track := append(info, data...)
		track = append(track, make([]byte, (256-len(track)%256)%256)...)
		hdr[0x34+i] = byte(len(track) / 256)
		body = append(body, track...)
	}
	return append(hdr, body...)
}

// plus3Format is a +3 data track: nine 512-byte sectors numbered from 1.
func plus3Format(cylinder byte) dskTrack {
	t := dskTrack{cylinder: cylinder, mode: 2}
	for r := byte(1); r <= 9; r++ {
		t.sectors = append(t.sectors, dskSector{c: cylinder, r: r, n: 2, data: fill(512, cylinder*9+r)})
	}
	return t
}

func dskCases() map[string][]byte {
	weak := append(fill(512, 1), fill(512, 1)...)
	for i := 100; i < 300; i++ {
		weak[512+i] ^= 0x5a // the second copy differs over 200 bytes
	}
	protected := dskTrack{cylinder: 1, mode: 2, sectors: []dskSector{
		{c: 1, r: 1, n: 2, data: fill(512, 9)},
		{c: 1, r: 2, n: 2, st1: 0x20, st2: 0x20, data: fill(512, 8)}, // data CRC error
		{c: 1, r: 3, n: 2, st1: 0x20, data: fill(512, 7)},            // ID CRC error
		{c: 1, r: 4, n: 2, st2: 0x40, data: fill(512, 6)},            // deleted data
		{c: 1, r: 5, n: 2, data: weak},                               // weak: two copies
		{c: 1, r: 6, n: 1, data: fill(300, 5)},                       // 44 bytes in the gap
	}}
	fm := dskTrack{cylinder: 2, mode: 1, sectors: []dskSector{{c: 2, r: 1, n: 1, data: fill(256, 4)}}}
	long := dskTrack{cylinder: 0, mode: 2}
	for r := byte(1); r <= 13; r++ {
		long.sectors = append(long.sectors, dskSector{c: 0, r: r, n: 2, data: fill(512, r)})
	}
	return map[string][]byte{
		"plus3":     edsk(3, 1, []dskTrack{plus3Format(0), plus3Format(1), plus3Format(2)}),
		"protected": edsk(3, 1, []dskTrack{plus3Format(0), protected, fm}),
		"long":      edsk(1, 1, []dskTrack{long}),
	}
}

// The .dsk cases below were compared track by track, with their clock and
// weak bitmaps, against the raw tracks Fuse 1.6.0's disk.c builds for a +3
// (open_cpc with DISK_FLAG_PLUS3_CPC); so were the 710 local .dsk images Fuse
// opens (the 14 it refuses are refused here too).
func TestDSKImagesMatchFuse(t *testing.T) {
	want := map[string]string{
		"plus3":     "f927495cf2f55b0a75069e0c314a87590daff5fed1ed64839e0502b174335264",
		"protected": "f9467f5c3eefd4db512ef1a51683e4fab36e281d31737a6e2e43acc77d241ec2",
		"long":      "89e67895b8b3a3127ad7ab0fb0c6cc5c5a3a3419aa6b45202579749d9c499e52",
	}
	for name, data := range dskCases() {
		d, err := Read(data)
		if err != nil || d.System != Plus3 {
			t.Fatalf("%s: %v", name, err)
		}
		image, err := Convert(data, Plus3, false)
		if err != nil {
			t.Fatal(name, err)
		}
		sum := sha256.Sum256(image)
		if got := hex.EncodeToString(sum[:]); got != want[name] {
			t.Errorf("%s: image sha256 %s", name, got)
		}
	}
}

// A dump's single copy of a Speedlock weak sector reads weak where Fuse's
// Speedlock hack corrupts it: bytes 29 and 58 of an E5-filled sector, and
// every 29th byte of another, the first eight of them.
func TestSpeedlockSectorIsWeak(t *testing.T) {
	for name, tc := range map[string]struct {
		data []byte
		weak int
	}{
		"E5 filler": {bytes.Repeat([]byte{0xe5}, 512), 2},
		"data":      {fill(512, 3), MaxWeakRanges},
	} {
		track := plus3Format(0)
		track.sectors[1] = dskSector{c: 0, r: 2, n: 2, st1: 0x20, st2: 0x20, data: tc.data}
		d, err := Read(edsk(1, 1, []dskTrack{track}))
		if err != nil {
			t.Fatal(name, err)
		}
		track0 := d.Tracks[0][0]
		var weak []int
		for i, w := range track0.Weak {
			if w {
				weak = append(weak, i)
			}
		}
		if len(weak) != tc.weak {
			t.Fatalf("%s: %d weak bytes", name, len(weak))
		}
		for k, i := range weak {
			if k > 0 && i-weak[k-1] != 29 || track0.Bytes[i] != tc.data[28+29*k] {
				t.Fatalf("%s: weak byte %d at %d", name, k, i)
			}
		}
	}
	// Only cylinder 0 sector 2 with a data CRC error is Speedlock's.
	for _, tc := range []dskSector{{c: 0, r: 2, n: 2, data: fill(512, 3)}, {c: 0, r: 3, n: 2, st1: 0x20, st2: 0x20, data: fill(512, 3)}} {
		track := plus3Format(0)
		track.sectors[1] = tc
		d, _ := Read(edsk(1, 1, []dskTrack{track}))
		for _, w := range d.Tracks[0][0].Weak {
			if w {
				t.Fatalf("sector %d marked weak", tc.r)
			}
		}
	}
}

func TestWrongSystem(t *testing.T) {
	if _, err := Convert(trd(0x16, 160), Plus3, false); !errors.Is(err, ErrWrongSystem) {
		t.Errorf(".trd for the +3: %v", err)
	}
	if _, err := Convert(dskCases()["plus3"], TRDOS, false); !errors.Is(err, ErrWrongSystem) {
		t.Errorf(".dsk for TR-DOS: %v", err)
	}
	if !Plus3.Admit("GAME.DSK") || Plus3.Admit("game.trd") || !TRDOS.Admit("game.scl") || TRDOS.Admit("game.dsk") {
		t.Error("system extensions")
	}
}

func TestDSKSlotLayout(t *testing.T) {
	cases := dskCases()
	image, _ := Convert(cases["protected"], Plus3, true)
	if image[8] != 13 || image[7] != 2 {
		t.Fatalf("header % x", image[:9])
	}
	weak := image[3*SlotBytes:][:SlotBytes]
	if start, n := binary.LittleEndian.Uint16(weak[SlotBytes/8-144:]), binary.LittleEndian.Uint16(weak[SlotBytes/8-142:]); n != 200 ||
		binary.LittleEndian.Uint16(weak[SlotBytes/8-140:]) != 0 {
		t.Fatalf("weak range %d+%d", start, n)
	}
	if image[5*SlotBytes+2] != 1 || image[3*SlotBytes+2] != 0 {
		t.Fatal("FM track flag")
	}
	long, _ := Convert(cases["long"], Plus3, false)
	if long[8] != 14 || len(long) != 2*LargeSlotBytes || binary.LittleEndian.Uint16(long[LargeSlotBytes:]) != 10416 {
		t.Fatalf("long track image: shift %d, %d bytes", long[8], len(long))
	}
	// A track the image cannot hold, and one with too many weak ranges.
	d := newDisk(1, 1)
	d.Tracks[0][0] = &Track{Bytes: make([]byte, MaxTrackBytes+1), Clocks: make([]bool, MaxTrackBytes+1)}
	if _, err := d.Image(); err == nil {
		t.Error("oversized track accepted")
	}
	w := make([]bool, 6250)
	for i := 0; i < 2*(MaxWeakRanges+1); i += 2 {
		w[i] = true
	}
	d.Tracks[0][0] = &Track{Bytes: make([]byte, 6250), Clocks: make([]bool, 6250), Weak: w}
	if _, err := d.Image(); err == nil {
		t.Error("too many weak ranges accepted")
	}
}
