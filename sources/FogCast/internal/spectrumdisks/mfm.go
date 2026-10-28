package spectrumdisks

// MFM track building, as Fuse's disk.c lays tracks out.

// gapLayout is one of disk.c's gap tables: the gap byte, sync byte and run,
// the post-index gap, gap II after an ID and gap III after data.
type gapLayout struct {
	gap, sync             byte
	syncLen               int
	postIndex, gap2, gap3 int
}

// trdosGaps is disk.c's GAP_TRDOS: no pre-index gap or index mark.
var trdosGaps = gapLayout{gap: 0x4e, sync: 0x00, syncLen: 12, postIndex: 10, gap2: 22, gap3: 60}

const (
	idMark   = 0xfe
	dataMark = 0xfb
	syncMark = 0xa1
)

// crcFDC is the controller's CRC-CCITT step (polynomial 0x1021, MSB first).
func crcFDC(crc uint16, b byte) uint16 {
	crc ^= uint16(b) << 8
	for i := 0; i < 8; i++ {
		if crc&0x8000 != 0 {
			crc = crc<<1 ^ 0x1021
		} else {
			crc <<= 1
		}
	}
	return crc
}

type trackWriter struct {
	t    *Track
	gaps gapLayout
}

func newTrack(length int, gaps gapLayout) *trackWriter {
	return &trackWriter{t: &Track{Bytes: make([]byte, 0, length), Clocks: make([]bool, 0, length)}, gaps: gaps}
}

func (w *trackWriter) put(b byte, clock bool) {
	w.t.Bytes = append(w.t.Bytes, b)
	w.t.Clocks = append(w.t.Clocks, clock)
}

func (w *trackWriter) run(b byte, n int) {
	for i := 0; i < n; i++ {
		w.put(b, false)
	}
}

// mark writes the sync run, three A1 bytes with a missing clock and the mark
// byte, and returns the CRC so far.
func (w *trackWriter) mark(m byte) uint16 {
	w.run(w.gaps.sync, w.gaps.syncLen)
	crc := uint16(0xffff)
	for i := 0; i < 3; i++ {
		w.put(syncMark, true)
		crc = crcFDC(crc, syncMark)
	}
	w.put(m, false)
	return crcFDC(crc, m)
}

func (w *trackWriter) id(cylinder, head, sector, length byte) {
	crc := w.mark(idMark)
	for _, b := range []byte{cylinder, head, sector, length} {
		w.put(b, false)
		crc = crcFDC(crc, b)
	}
	w.put(byte(crc>>8), false)
	w.put(byte(crc), false)
	w.run(w.gaps.gap, w.gaps.gap2)
}

func (w *trackWriter) data(data []byte) {
	crc := w.mark(dataMark)
	for _, b := range data {
		w.put(b, false)
		crc = crcFDC(crc, b)
	}
	w.put(byte(crc>>8), false)
	w.put(byte(crc), false)
	w.run(w.gaps.gap, w.gaps.gap3)
}

// lengthCode is the ID's size code: 128 << code bytes.
func lengthCode(n int) byte {
	code := byte(0)
	for n > 128 {
		code++
		n >>= 1
	}
	return code
}

// interleave2 is disk.c's INTERLEAVE_2 order: sector s of a track of n
// sectors sits at position order[s] (1 9 2 10 ... for 16 sectors).
func interleave2(n int) []int {
	order := make([]int, n)
	pos, filled := 0, 0
	for s := 0; s < n; s++ {
		order[s] = pos
		pos += 2
		if pos >= n {
			pos -= n
			if pos <= filled {
				pos++
				filled++
			}
		}
	}
	return order
}

// formatTrack builds an MFM track of sectors numbered from 1, each n bytes
// of sector(s) laid out in interleave-2 order, then gap IV to the end of a
// double-density track.
func formatTrack(cylinder, head, sectors, n int, gaps gapLayout, sector func(int) []byte) *Track {
	order := interleave2(sectors)
	at := make([]int, sectors) // sector at each position
	for s, p := range order {
		at[p] = s
	}
	w := newTrack(ddBytes, gaps)
	w.run(gaps.gap, gaps.postIndex)
	for p := 0; p < sectors; p++ {
		s := at[p]
		w.id(byte(cylinder), byte(head), byte(s+1), lengthCode(n))
		w.data(sector(s))
	}
	w.run(gaps.gap, ddBytes-len(w.t.Bytes))
	return w.t
}
