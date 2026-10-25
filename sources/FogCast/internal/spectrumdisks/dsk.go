package spectrumdisks

import (
	"bytes"
	"fmt"
)

// +3 disks: standard ("MV - CPC") and extended ("EXTENDED") DSK images, read
// as Fuse 1.6.0's open_cpc reads them for the +3 (DISK_FLAG_PLUS3_CPC, no
// pre-index gap): each track's sectors from its Track-Info block, laid out
// with the uPD765 gap tables, every track the disk's common length, CRC
// errors and deleted marks from the sector status bytes, bytes of a long
// sector that fall in the gap, and weak sectors from their stored copies.
//
// A dump of a Speedlock disk keeps one copy of its weak sector: cylinder 0,
// head 0, sector 2, 512 bytes, recorded with a data CRC error. Fuse's
// Speedlock hack (plus3_detect_speedlock, on by default) corrupts every 29th
// byte when the sector is read again: only bytes 29 and 58 when its first 64
// bytes are E5 filler, else all of them. Those bytes are weak here, the first
// MaxWeakRanges of them.

var (
	dskMagic  = []byte("MV - CPC")
	edskMagic = []byte("EXTENDED")
	trackInfo = []byte("Track-Info")
	offsetInf = []byte("Offset-Info\r\n")
)

func isDSK(data []byte) bool {
	return len(data) >= 256 && (bytes.HasPrefix(data, dskMagic) || bytes.HasPrefix(data, edskMagic))
}

// fuseGap is one of disk.c's gap tables.
type fuseGap struct {
	gap, sync         byte
	syncLen           int
	fm                bool // no A1 sync; the mark byte itself is clocked
	postIndex, g2, g3 int
}

var (
	gapIBM3740 = fuseGap{gap: 0xff, sync: 0x00, syncLen: 6, fm: true, postIndex: 26, g2: 11, g3: 27}
	gapIBM34   = fuseGap{gap: 0x4e, sync: 0x00, syncLen: 12, postIndex: 50, g2: 22, g3: 54}
	gap4k765   = fuseGap{gap: 0xff, sync: 0x00, syncLen: 6, fm: true, postIndex: 8, g2: 11, g3: 10}
	gap8k765   = fuseGap{gap: 0x4e, sync: 0x00, syncLen: 12, postIndex: 16, g2: 22, g3: 24}
)

func (g fuseGap) marks() int {
	if g.fm {
		return 0
	}
	return 3
}

// sectorLen is disk.c's calc_sectorlen.
func (g fuseGap) sectorLen(n int) int {
	return g.syncLen + g.marks() + 7 + g.g2 + g.syncLen + g.marks() + 1 + n + 2 + g.g3
}

// disk.c's disk_bpt classes, chosen by the longest track.
func trackClass(bpt int) int {
	switch {
	case bpt > 12500:
		return 0
	case bpt > 10416:
		return 12500
	case bpt > 6500:
		return 10416
	case bpt > 6250:
		return 6500
	case bpt > 5208:
		return 6250
	case bpt > 3125:
		return 5208
	case bpt > 0:
		return 3125
	}
	return 0
}

// fuseTrack is disk.c's positional track: zeroed bytes, written from i.
type fuseTrack struct {
	b            []byte
	clocks, weak []bool
	i            int
	fm           bool
}

func newFuseTrack(bpt int) *fuseTrack {
	return &fuseTrack{b: make([]byte, bpt), clocks: make([]bool, bpt), weak: make([]bool, bpt)}
}

func (t *fuseTrack) fill(v byte, n int) {
	for k := 0; k < n; k++ {
		t.b[t.i] = v
		t.i++
	}
}

func (t *fuseTrack) gapAdd(g fuseGap, n int) bool {
	if t.i+n >= len(t.b) {
		return false
	}
	t.fill(g.gap, n)
	return true
}

// markAdd writes the sync run, the A1 marks (MFM) and the mark byte, and
// returns the CRC so far.
func (t *fuseTrack) markAdd(g fuseGap, m byte) uint16 {
	t.fill(g.sync, g.syncLen)
	crc := uint16(0xffff)
	for k := 0; k < g.marks(); k++ {
		t.b[t.i], t.clocks[t.i] = syncMark, true
		crc = crcFDC(crc, syncMark)
		t.i++
	}
	if g.fm {
		t.clocks[t.i] = true
	}
	t.b[t.i] = m
	t.i++
	return crcFDC(crc, m)
}

// idAdd is disk.c's id_add.
func (t *fuseTrack) idAdd(g fuseGap, c, h, r, n byte, crcError bool) bool {
	if t.i+g.syncLen+g.marks()+7 >= len(t.b) {
		return false
	}
	crc := t.markAdd(g, idMark)
	for _, v := range []byte{c, h, r, n} {
		t.b[t.i] = v
		crc = crcFDC(crc, v)
		t.i++
	}
	t.b[t.i] = byte(crc >> 8)
	t.i++
	if crcError {
		t.b[t.i] = ^byte(crc)
	} else {
		t.b[t.i] = byte(crc)
	}
	t.i++
	return t.gapAdd(g, g.g2)
}

// damAdd is disk.c's datamark_add.
func (t *fuseTrack) damAdd(g fuseGap, deleted bool) (uint16, bool) {
	if t.i+g.g2+g.syncLen+g.marks()+1 >= len(t.b) {
		return 0, false
	}
	m := byte(dataMark)
	if deleted {
		m = 0xf8
	}
	return t.markAdd(g, m), true
}

// dataAdd is disk.c's data_add from a source with autofill 0; it returns
// where the data starts and whether it fit.
func (t *fuseTrack) dataAdd(g fuseGap, src *dskSource, n int, deleted, crcError bool) (int, bool) {
	crc, ok := t.damAdd(g, deleted)
	if !ok {
		return 0, false
	}
	if t.i+n+2 >= len(t.b) {
		return 0, false
	}
	start := t.i
	got := src.read(t.b[t.i : t.i+n])
	for k := got; k < n; k++ {
		t.b[t.i+k] = 0x00
	}
	for k := 0; k < n; k++ {
		crc = crcFDC(crc, t.b[t.i])
		t.i++
	}
	if crcError {
		crc ^= 1
	}
	t.b[t.i], t.b[t.i+1] = byte(crc>>8), byte(crc)
	t.i += 2
	return start, t.gapAdd(g, g.g3)
}

func (t *fuseTrack) gap4(g fuseGap) {
	for t.i < len(t.b) {
		t.b[t.i] = g.gap
		t.i++
	}
}

// dskSource is disk.c's buffer_t: the image and a read position.
type dskSource struct {
	data []byte
	at   int
}

func (s *dskSource) avail() int { return max(0, len(s.data)-s.at) }

func (s *dskSource) read(p []byte) int {
	n := copy(p, s.data[min(s.at, len(s.data)):])
	s.at += n
	return n
}

// disk.c's CPC_ISSUE_* layout fixes for +3 images.
const (
	cpcNone = 0
	cpc1    = 1  // first sector 8K (6144 bytes)
	cpc2    = 2  // 6144, then 10 x 512
	cpc3    = 3  // 128, 256, ... 4096
	cpc3a   = 13 // as 3, numbered from 1
	cpc4    = 4  // N x 8192 (at most 6384 bytes)
	cpc5    = 5  // track 38: 512 256 512 ...
)

func readDSK(data []byte) (*Disk, error) {
	extended := bytes.HasPrefix(data, edskMagic)
	hdr := data[:256]
	sides, cylinders := int(hdr[0x31]), int(hdr[0x30])
	if sides < 1 || sides > 2 || cylinders < 1 || cylinders > MaxCylinders {
		return nil, fmt.Errorf("spectrum disk: .dsk geometry %d cylinders x %d sides is outside 1..%d x 1..2", cylinders, sides, MaxCylinders)
	}
	data = append([]byte(nil), data...) // the scan marks MFM and fixes in track headers
	trackSize := func(i int) int {
		if extended {
			return int(data[0x34+i]) * 256
		}
		return int(data[0x32]) | int(data[0x33])<<8
	}
	// Bound the tracks to what the file holds.
	idx := 256
	for i := 0; i < sides*cylinders; i++ {
		if extended && 0x34+i >= 256 {
			return nil, fmt.Errorf("spectrum disk: .dsk track table overflows its header")
		}
		size := trackSize(i)
		if size == 0 {
			continue
		}
		if idx+0x14 > len(data) || idx+size > len(data) {
			cylinders = i/sides + i%sides
			break
		}
		if data[idx+0x13] == 0 {
			data[idx+0x13] = 2
		}
		idx += size
	}
	gapFor := func(t []byte) fuseGap {
		if t[0x1b] >= 6 {
			if t[0x13] == 2 {
				return gap8k765
			}
			return gap4k765
		}
		if t[0x13] == 2 {
			return gapIBM34
		}
		return gapIBM3740
	}
	sectorLength := func(t []byte, j int) (seclen, idlen int) {
		n := t[0x1b+8*j]
		if extended {
			seclen = int(t[0x1e+8*j]) | int(t[0x1f+8*j])<<8
		} else {
			seclen = 0x80 << (n & 31)
		}
		idlen = 0x80 << min(n, 8)
		return
	}
	// First pass: the longest track and each track's layout fix.
	at, maxBPT := 256, 0
	for i := 0; i < sides*cylinders; i++ {
		if at+len(offsetInf) <= len(data) && bytes.Equal(data[at:at+len(offsetInf)], offsetInf) {
			at = len(data)
		}
		if at >= len(data) {
			cylinders = i/sides + i%sides
			break
		}
		if extended && data[0x34+i] == 0 {
			continue
		}
		if len(data)-at < 256 || !bytes.HasPrefix(data[at:], trackInfo) {
			return nil, fmt.Errorf("spectrum disk: .dsk track %d has no Track-Info block", i)
		}
		t := data[at:]
		g := gapFor(t)
		if i != int(t[0x10])*sides+int(t[0x11]) {
			return nil, fmt.Errorf("spectrum disk: .dsk track %d is numbered %d/%d", i, t[0x10], t[0x11])
		}
		bpt := g.postIndex + 3
		if t[0x13] == 2 {
			bpt = g.postIndex + 6
		}
		fix := cpcNone
		for j := 0; j < int(t[0x15]); j++ {
			if 0x1f+8*j >= 256 {
				return nil, fmt.Errorf("spectrum disk: .dsk track %d lists too many sectors", i)
			}
			seclen, idlen := sectorLength(t, j)
			if !extended && ((seclen == 6144 && idlen != 8192) || (idlen > 4096 && seclen != 0) || seclen != idlen) {
				return nil, fmt.Errorf("spectrum disk: .dsk track %d sector %d length is inconsistent", i, j)
			}
			bpt += g.sectorLen(min(seclen, idlen))
			c, h, r, n := t[0x18+8*j], t[0x19+8*j], t[0x1a+8*j], t[0x1b+8*j]
			bj := byte(j)
			switch {
			case j == 0 && n == 6 && seclen > 6144:
				fix = cpc4
			case j == 0 && n == 6:
				fix = cpc1
			case j == 0 && c == bj && h == bj && r == bj && n == bj:
				fix = cpc3
			case j == 0 && c == 1 && h == 1 && r == 1 && n == 1:
				fix = cpc3a
			case j == 1 && fix == cpc1 && n == 2:
				fix = cpc2
			case i == 38 && j == 0 && n == 2:
				fix = cpc5
			case j > 1 && fix == cpc2 && n != 2:
				fix = cpcNone
			case j > 0 && fix == cpc3 && (c != bj || h != bj || r != bj || n != bj):
				fix = cpcNone
			case j > 0 && fix == cpc3a && (c != bj+1 || h != bj+1 || r != bj+1 || n != bj+1):
				fix = cpcNone
			case j > 10 && fix == cpc2:
				fix = cpcNone
			case i == 38 && j > 0 && fix == cpc5 && n != 2-bj&1:
				fix = cpcNone
			}
		}
		t[0x00] = byte(fix)
		if fix == cpc4 {
			bpt = 6500
		} else if fix != cpcNone {
			bpt = 6250
		}
		at += trackSize(i)
		maxBPT = max(maxBPT, bpt)
	}
	bpt := trackClass(maxBPT)
	if bpt == 0 || bpt > MaxTrackBytes {
		return nil, fmt.Errorf("spectrum disk: .dsk tracks of %d bytes are outside the image", maxBPT)
	}
	disk := newDisk(cylinders, sides)
	disk.System = Plus3
	src := &dskSource{data: data, at: 256}
	for i := 0; i < sides*cylinders; i++ {
		if extended && data[0x34+i] == 0 {
			continue
		}
		save := src.at
		hdr := data[src.at:]
		src.at += 256
		g := gapFor(hdr)
		if k := int(hdr[0x10])*sides + int(hdr[0x11]); k > i {
			i = k
		}
		if i >= sides*cylinders {
			break
		}
		t := newFuseTrack(bpt)
		t.fm = g.fm
		t.gapAdd(g, g.postIndex)
		fix := int(hdr[0x00])
		for j := 0; j < int(hdr[0x15]); j++ {
			seclen, idlen := sectorLength(hdr, j)
			st1, st2 := hdr[0x1c+8*j], hdr[0x1d+8*j]
			deleted := st2&0x40 != 0
			dataCRC := st1&0x20 != 0 && st2&0x20 != 0
			if fix == cpc2 && j == 0 {
				t.i = 8
			}
			t.idAdd(g, hdr[0x18+8*j], hdr[0x19+8*j], hdr[0x1a+8*j], hdr[0x1b+8*j],
				st1&0x20 != 0 && st2&0x20 == 0)
			switch {
			case fix == cpc1 && j == 0:
				t.dataAdd(g, src, seclen, deleted, dataCRC)
			case fix == cpc2 && j == 0:
				if _, ok := t.damAdd(g, deleted); ok {
					t.gapAdd(g, g.g2)
				}
				src.at += seclen
			case fix == cpc3:
				t.dataAdd(g, src, 128, deleted, dataCRC)
				src.at += seclen - 128
			case fix == cpc4:
				t.dataAdd(g, src, 6384, deleted, dataCRC)
				src.at += seclen - 6384
			case fix == cpc5:
				if idlen == 256 {
					t.dataAdd(g, &dskSource{data: data, at: src.at}, 512, deleted, dataCRC)
					src.at += idlen
				} else {
					t.dataAdd(g, src, idlen, deleted, dataCRC)
				}
			default:
				start, ok := t.dataAdd(g, src, min(seclen, idlen), deleted, dataCRC)
				if !ok {
					src.at += seclen
				}
				if seclen > idlen && seclen%idlen != 0 {
					// The rest of a long sector lies in the gap after it.
					if seclen-idlen > src.avail() {
						return nil, fmt.Errorf("spectrum disk: .dsk track %d sector %d runs past the image", i, j)
					}
					p := start + idlen
					for k := seclen - idlen; k > 0; k-- {
						if p == len(t.b) {
							p = 0
						}
						t.b[p] = data[src.at]
						p++
						src.at++
					}
				} else if seclen > idlen {
					// Copies of a weak sector: bytes that differ between
					// copies read at random.
					copies := seclen / idlen
					first, last := -1, -1
					for k := 0; k < idlen; k++ {
						for c := 0; c < copies-1; c++ {
							if src.at+c*idlen+k < len(data) && t.b[start+k] != data[src.at+c*idlen+k] {
								if first < 0 {
									first = start + k
								}
								last = start + k
							}
						}
					}
					for k := first; first >= 0 && k <= last; k++ {
						t.weak[k] = true
					}
					src.at += (copies - 1) * idlen
				} else if ok && i == 0 && dataCRC && idlen == 512 &&
					hdr[0x18+8*j] == 0 && hdr[0x19+8*j] == 0 && hdr[0x1a+8*j] == 2 {
					speedlock(t, start)
				}
			}
		}
		t.i = max(t.i, 0)
		t.gap4(g)
		disk.Tracks[i/sides][i%sides] = &Track{Bytes: t.b, Clocks: t.clocks, Weak: t.weak, FM: t.fm}
		src.at = save + trackSize(i)
	}
	return disk, nil
}

// speedlock marks the bytes of a Speedlock weak sector at start that Fuse's
// Speedlock hack corrupts: every 29th byte, counting the first as 1, in the
// first 64 bytes when they are E5 filler and in the whole sector otherwise.
func speedlock(t *fuseTrack, start int) {
	end := 512
	filler := true
	for k := 0; k < 63; k++ {
		filler = filler && t.b[(start+k)%len(t.b)] == 0xe5
	}
	if filler {
		end = 64
	}
	for k, n := 28, 0; k < end && n < MaxWeakRanges; k, n = k+29, n+1 {
		t.weak[(start+k)%len(t.b)] = true
	}
}
