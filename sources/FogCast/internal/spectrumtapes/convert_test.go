package spectrumtapes

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// ----------------------------------------------------------- image builders

type image struct{ b []byte }

func (i *image) u8(v ...int) {
	for _, x := range v {
		i.b = append(i.b, byte(x))
	}
}
func (i *image) u16(v ...int) {
	for _, x := range v {
		i.b = binary.LittleEndian.AppendUint16(i.b, uint16(x))
	}
}
func (i *image) u24(v int)    { i.u8(v, v>>8, v>>16) }
func (i *image) u32(v uint32) { i.b = binary.LittleEndian.AppendUint32(i.b, v) }
func (i *image) raw(b []byte) { i.b = append(i.b, b...) }
func (i *image) str(s string) { i.u8(len(s)); i.raw([]byte(s)) }

func newTZX() *image { return &image{b: append([]byte("ZXTape!\x1a"), 1, 20)} }

func (i *image) rom(pauseMS int, data []byte) { i.u8(0x10); i.u16(pauseMS, len(data)); i.raw(data) }
func (i *image) turbo(pilot, sync1, sync2, bit0, bit1, pilots, lastBits, pauseMS int, data []byte) {
	i.u8(0x11)
	i.u16(pilot, sync1, sync2, bit0, bit1, pilots)
	i.u8(lastBits)
	i.u16(pauseMS)
	i.u24(len(data))
	i.raw(data)
}
func (i *image) tone(length, count int) { i.u8(0x12); i.u16(length, count) }
func (i *image) pulses(lengths ...int) {
	i.u8(0x13, len(lengths))
	i.u16(lengths...)
}
func (i *image) pure(bit0, bit1, lastBits, pauseMS int, data []byte) {
	i.u8(0x14)
	i.u16(bit0, bit1)
	i.u8(lastBits)
	i.u16(pauseMS)
	i.u24(len(data))
	i.raw(data)
}
func (i *image) direct(sample, pauseMS, lastBits int, data []byte) {
	i.u8(0x15)
	i.u16(sample, pauseMS)
	i.u8(lastBits)
	i.u24(len(data))
	i.raw(data)
}

type sym struct {
	edge    int
	lengths []int
}

func (i *image) generalised(pauseMS int, pilot []sym, npp int, prle [][2]int,
	data []sym, npd, totd int, stream []byte) {
	var b image
	b.u16(pauseMS)
	b.u32(uint32(len(prle)))
	b.u8(npp, len(pilot)%256)
	b.u32(uint32(totd))
	b.u8(npd, len(data)%256)
	if len(prle) > 0 {
		for _, s := range pilot {
			b.u8(s.edge)
			b.u16(s.lengths...)
		}
		for _, r := range prle {
			b.u8(r[0])
			b.u16(r[1])
		}
	}
	if totd > 0 {
		for _, s := range data {
			b.u8(s.edge)
			b.u16(s.lengths...)
		}
	}
	b.raw(stream)
	i.u8(0x19)
	i.u32(uint32(len(b.b)))
	i.raw(b.b)
}

func (i *image) pause(ms int) { i.u8(0x20); i.u16(ms) }

func bytesOf(n int, seed int) []byte {
	b := make([]byte, n)
	x := uint32(seed)*2654435761 + 1
	for j := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[j] = byte(x)
	}
	return b
}

func romBlock(flag byte, n int, seed int) []byte {
	b := append([]byte{flag}, bytesOf(n, seed)...)
	check := byte(0)
	for _, x := range b {
		check ^= x
	}
	return append(b, check)
}

func tapImage() []byte {
	var t image
	for _, blk := range [][]byte{romBlock(0x00, 18, 1), romBlock(0xff, 64, 2), romBlock(0xff, 3, 3)} {
		t.u16(len(blk))
		t.raw(blk)
	}
	return t.b
}

func cswImage(version int, pulses []byte) []byte {
	var c image
	c.raw([]byte("Compressed Square Wave\x1a"))
	if version == 1 {
		c.u8(1, 1)
		c.u16(44100)
		c.u8(1, 0, 0, 0, 0)
		c.raw(pulses)
		return c.b
	}
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(pulses)
	w.Close()
	c.u8(2, 0)
	c.u32(22050)
	c.u32(0)
	c.u8(2, 0, 3)
	c.raw([]byte("fes-spectrum-tst"))
	c.raw([]byte{0xaa, 0xbb, 0xcc})
	c.raw(z.Bytes())
	return c.b
}

func cswPulseData() []byte {
	p := []byte{10, 20, 30, 0}
	p = binary.LittleEndian.AppendUint32(p, 700)
	for j := 0; j < 200; j++ {
		p = append(p, byte(3+j%7))
	}
	return p
}

type pzxImage struct{ image }

func newPZX() *pzxImage {
	p := &pzxImage{}
	p.block("PZXT", []byte{1, 0, 'f', 'e', 's', 0})
	return p
}
func (p *pzxImage) block(tag string, body []byte) {
	p.raw([]byte(tag))
	p.u32(uint32(len(body)))
	p.raw(body)
}

// fixtures returns tape images covering every block libspectrum reads, and
// (extensions) the blocks only this package reads.
func fixtures() (oracle, extensions map[string][]byte) {
	oracle = map[string][]byte{}
	extensions = map[string][]byte{}
	oracle["rom.tap"] = tapImage()

	t := newTZX()
	t.rom(1000, romBlock(0x00, 18, 4))
	t.rom(0, romBlock(0xff, 40, 5))
	t.rom(2000, romBlock(0xff, 5, 6))
	oracle["standard.tzx"] = t.b

	t = newTZX()
	t.turbo(1000, 300, 400, 250, 500, 200, 5, 100, bytesOf(20, 7))
	t.turbo(1200, 0, 0, 0, 600, 0, 0, 0, bytesOf(9, 8))
	t.turbo(900, 350, 350, 300, 600, 31, 8, 0, bytesOf(4, 9))
	t.pause(30)
	oracle["turbo.tzx"] = t.b

	t = newTZX()
	t.tone(500, 51)
	t.pulses(100, 0, 200, 300)
	t.pause(50)
	t.tone(0, 3)
	t.tone(700, 2)
	t.pulses(0, 0, 40)
	oracle["tones.tzx"] = t.b

	t = newTZX()
	t.pure(300, 600, 3, 10, bytesOf(12, 10))
	t.pure(400, 800, 0, 0, bytesOf(6, 11))
	t.pure(500, 1000, 8, 15, bytesOf(1, 12))
	oracle["pure.tzx"] = t.b

	t = newTZX()
	t.direct(79, 20, 5, []byte{0xf0, 0x0f, 0xff, 0x00, 0xaa, 0x33})
	t.direct(158, 0, 8, []byte{0x00, 0x00, 0xff})
	t.direct(100, 5, 0, []byte{0xc3, 0x3c})
	oracle["direct.tzx"] = t.b

	t = newTZX()
	pilot := []sym{{0, []int{600, 0}}, {1, []int{300, 300}}, {2, []int{200, 100}}, {3, []int{150, 0}}}
	data2 := []sym{{0, []int{250, 250}}, {0, []int{500, 500}}}
	t.generalised(25, pilot, 2, [][2]int{{0, 40}, {1, 3}, {2, 2}, {3, 5}, {0, 1}}, data2, 2, 24, bytesOf(3, 13))
	data3 := []sym{{0, []int{200, 0, 0}}, {1, []int{300, 300, 100}}, {3, []int{400, 0, 0}}}
	t.generalised(0, nil, 0, nil, data3, 3, 7, []byte{0b00011000, 0b11010001})
	t.generalised(10, pilot[:1], 2, [][2]int{{0, 9}}, nil, 0, 0, nil)
	oracle["generalised.tzx"] = t.b

	t = newTZX()
	t.u8(0x21)
	t.str("group")
	t.tone(400, 7)
	t.u8(0x23)
	t.u16(2) // jump over the next tone
	t.tone(9999, 3)
	t.u8(0x24)
	t.u16(3) // loop three times
	t.tone(250, 5)
	t.pulses(60, 70)
	t.u8(0x25)
	t.u8(0x22)
	t.u8(0x28)
	t.u16(1 + 2 + 1 + 4)
	t.u8(1)
	t.u16(1)
	t.str("next")
	t.u8(0x30)
	t.str("comment")
	t.u8(0x31, 2)
	t.str("message")
	t.u8(0x32)
	t.u16(1 + 1 + 1 + 4)
	t.u8(1, 0x00)
	t.str("Fess")
	t.u8(0x33, 1, 0, 0x1a, 1)
	t.u8(0x35)
	t.raw([]byte("FES custom block"))
	t.u32(3)
	t.raw([]byte{1, 2, 3})
	t.raw([]byte("ZXTape!\x1a\x01\x14")) // glue
	t.u8(0x2b)
	t.u32(1)
	t.u8(1)
	t.tone(333, 4)
	t.u8(0x2a)
	t.u32(0)
	t.tone(444, 3)
	t.pause(0)
	t.tone(555, 2)
	t.u8(0x2b)
	t.u32(1)
	t.u8(0)
	t.pulses(80)
	oracle["flow.tzx"] = t.b

	oracle["v1.csw"] = cswImage(1, cswPulseData())
	oracle["v2.csw"] = cswImage(2, cswPulseData())

	p := newPZX()
	var puls image
	puls.u16(0x8000|5, 2168, 667, 735, 0, 0x8000, 0x9000, 0x8000|2, 0x8001, 0x0002, 0, 120, 0)
	p.block("PULS", puls.b)
	var data image
	data.u32(13 | 1<<31)
	data.u16(945)
	data.u8(2, 3)
	data.u16(855, 855, 0, 1710, 400)
	data.raw([]byte{0xa5, 0x38})
	p.block("DATA", data.b)
	p.block("BRWS", []byte("chapter"))
	var paus image
	paus.u32(5000 | 1<<31)
	p.block("PAUS", paus.b)
	data = image{}
	data.u32(16)
	data.u16(0)
	data.u8(2, 2)
	data.u16(79, 0, 0, 79)
	data.raw([]byte{0x0f, 0xf0})
	p.block("DATA", data.b)
	p.block("STOP", []byte{1, 0})
	puls = image{}
	puls.u16(300, 300)
	p.block("PULS", puls.b)
	paus = image{}
	paus.u32(700)
	p.block("PAUS", paus.b)
	p.block("ABCD", []byte{9, 9, 9})
	puls = image{}
	puls.u16(0, 0x8000|3, 450)
	p.block("PULS", puls.b)
	oracle["mixed.pzx"] = p.b

	// libspectrum 1.5 reads a general STOP as a pause whose length it never
	// sets, so Fuse waits for an arbitrary time; the format says it stops.
	p = newPZX()
	puls = image{}
	puls.u16(0x8000|4, 900, 300)
	p.block("PULS", puls.b)
	p.block("STOP", []byte{0, 0})
	puls = image{}
	puls.u16(500, 0, 0, 600)
	p.block("PULS", puls.b)
	p.block("STOP", []byte{2, 0})
	paus = image{}
	paus.u32(800 | 1<<31)
	p.block("PAUS", paus.b)
	extensions["stop.pzx"] = p.b

	t = newTZX()
	t.tone(500, 3)
	var csw image
	csw.u16(40)
	csw.u24(44100)
	csw.u8(1)
	csw.u32(0)
	csw.raw(cswPulseData())
	t.u8(0x18)
	t.u32(uint32(len(csw.b)))
	t.raw(csw.b)
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write([]byte{5, 6, 7, 8, 9})
	w.Close()
	csw = image{}
	csw.u16(0)
	csw.u24(22050)
	csw.u8(2)
	csw.u32(5)
	csw.raw(z.Bytes())
	t.u8(0x18)
	t.u32(uint32(len(csw.b)))
	t.raw(csw.b)
	t.tone(600, 2)
	extensions["csw.tzx"] = t.b

	t = newTZX()
	t.tone(100, 1)
	t.u8(0x26)
	t.u16(2)
	t.u16(3, 5) // the routines at blocks 4 and 6
	t.tone(900, 2)
	t.u8(0x23)
	t.u16(5) // jump past the routines to the final tone
	t.tone(200, 3)
	t.u8(0x27)
	t.tone(300, 5)
	t.u8(0x27)
	t.tone(400, 2)
	extensions["call.tzx"] = t.b

	t = newTZX()
	t.tone(100, 4)
	t.u8(0x4b)
	t.u32(5)
	t.raw([]byte{1, 2, 3, 4, 5})
	t.u8(0x34)
	t.raw(make([]byte, 8))
	t.u8(0x40, 0)
	t.u24(2)
	t.u8(7, 7)
	t.tone(200, 3)
	extensions["unknown.tzx"] = t.b
	return oracle, extensions
}

func modes() map[string]bool { return map[string]bool{"48": true, "128": false} }

// The deck's playback of each converted image is Fuse's playback of the
// original, run for run, on both models.
func TestDeckPlaysWhatFusePlays(t *testing.T) {
	oracle, extensions := fixtures()
	for name, data := range merge(oracle, extensions) {
		blocks, err := read(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		pzx, err := Convert(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for mode, is48 := range modes() {
			want, err := fuseRuns(blocks, is48)
			if err != nil {
				t.Fatalf("%s %s: %v", name, mode, err)
			}
			got, err := deckRuns(pzx, is48)
			if err != nil {
				t.Fatalf("%s %s: %v", name, mode, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s: deck plays %d runs, Fuse %d; first difference %s", name, mode,
					len(got), len(want), firstDifference(got, want))
			}
		}
	}
}

func merge(a, b map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func firstDifference(a, b []string) string {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return "at " + strconv.Itoa(i) + ": " + x + " / " + y
		}
	}
	return "none"
}

type golden struct {
	Image string           `json:"image"`
	Runs  map[string]trace `json:"runs"`
}

type trace struct {
	Lines  int    `json:"lines"`
	SHA256 string `json:"sha256"`
}

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func traceOf(lines []string) trace {
	return trace{Lines: len(lines), SHA256: digest([]byte(strings.Join(lines, "\n") + "\n"))}
}

// testdata/oracle.json is libspectrum 1.5 (Fuse 1.6.0) playing each oracle
// fixture, recorded by tapedump (see testdata/README.md).
func TestEngineMatchesLibspectrum(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var goldens map[string]golden
	if err := json.Unmarshal(raw, &goldens); err != nil {
		t.Fatal(err)
	}
	oracle, _ := fixtures()
	if len(goldens) != len(oracle) {
		t.Fatalf("oracle records %d fixtures, the test builds %d", len(goldens), len(oracle))
	}
	for name, data := range oracle {
		g, ok := goldens[name]
		if !ok || g.Image != digest(data) {
			t.Fatalf("%s: fixture bytes differ from the recorded oracle input", name)
		}
		blocks, err := read(data)
		if err != nil {
			t.Fatal(err)
		}
		for mode, is48 := range modes() {
			lines, err := fuseRuns(blocks, is48)
			if err != nil {
				t.Fatal(err)
			}
			if got := traceOf(lines); got != g.Runs[mode] {
				t.Errorf("%s %s: %d lines %s, libspectrum %d lines %s", name, mode,
					got.Lines, got.SHA256, g.Runs[mode].Lines, g.Runs[mode].SHA256)
			}
		}
	}
}

// SPECTRUMTAPES_FIXTURES=dir writes the oracle fixtures for tapedump.
func TestWriteOracleFixtures(t *testing.T) {
	dir := os.Getenv("SPECTRUMTAPES_FIXTURES")
	if dir == "" {
		t.Skip("set SPECTRUMTAPES_FIXTURES to write the oracle fixtures")
	}
	oracle, _ := fixtures()
	for name, data := range oracle {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ROM bits travel as DATA blocks: a tape converts to about its own size.
func TestConvertKeepsDataCompact(t *testing.T) {
	data := tapImage()
	pzx, err := Convert(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pzx) > len(data)+300 {
		t.Fatalf("%d-byte tape became %d bytes of PZX", len(data), len(pzx))
	}
	if !bytes.HasPrefix(pzx, []byte("PZXT\x02\x00\x00\x00\x01\x00")) {
		t.Fatalf("PZX header %x", pzx[:10])
	}
}

// An edge-less symbol after a ROM block with no pause extends the block's
// last pulse, which leaves its DATA block for a PULS block.
func TestEdgelessPulseExtendsDataBlock(t *testing.T) {
	tz := newTZX()
	tz.rom(0, romBlock(0xff, 2, 14))
	tz.generalised(0, []sym{{1, []int{777}}}, 1, [][2]int{{0, 1}}, nil, 0, 0, nil)
	tz.tone(300, 2)
	blocks, err := read(tz.b)
	if err != nil {
		t.Fatal(err)
	}
	pzx, err := Convert(tz.b)
	if err != nil {
		t.Fatal(err)
	}
	for _, is48 := range modes() {
		want, _ := fuseRuns(blocks, is48)
		got, _ := deckRuns(pzx, is48)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("first difference %s", firstDifference(got, want))
		}
	}
	if !bytes.Contains(pzx, []byte("DATA")) {
		t.Fatal("the remaining bits left their DATA block")
	}
}

// A generalised block whose data alphabet has one symbol stores its symbols in
// zero bytes. After the pilot they still play: every symbol decodes as 0.
func TestOneSymbolDataPlaysAfterPilot(t *testing.T) {
	tz := newTZX()
	tz.generalised(0, []sym{{0, []int{1000}}}, 1, [][2]int{{0, 4}}, []sym{{0, []int{300, 300}}}, 2, 5, nil)
	blocks, err := read(tz.b)
	if err != nil {
		t.Fatal(err)
	}
	e, err := newEngine(blocks)
	if err != nil {
		t.Fatal(err)
	}
	var got []uint32
	for len(got) < 14 {
		tstates, _, err := e.next()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, tstates)
	}
	want := []uint32{1000, 1000, 1000, 1000, 300, 300, 300, 300, 300, 300, 300, 300, 300, 300}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edges %v, want %v", got, want)
	}
	pzx, err := Convert(tz.b)
	if err != nil {
		t.Fatal(err)
	}
	for _, is48 := range modes() {
		want, _ := fuseRuns(blocks, is48)
		got, _ := deckRuns(pzx, is48)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("first difference %s", firstDifference(got, want))
		}
	}
}

func TestConvertRejects(t *testing.T) {
	loop := newTZX()
	loop.tone(100, 1)
	loop.u8(0x23)
	loop.u16(0)
	cases := map[string][]byte{
		"empty.tap": {},
		"short.tap": {5, 0, 1, 2},
		"bad.tzx":   []byte("ZXTape!\x1a\x01"),
		"cut.tzx":   append(newTZX().b, 0x10, 0, 0, 9),
		"empty.tzx": newTZX().b,
		"loop.tzx":  loop.b,
		"none.tzx":  append(newTZX().b, 0x13, 0),
		"bad.csw":   []byte("Compressed Square Wave\x1a\x03\x00"),
		"old.pzx":   []byte("PZXT\x02\x00\x00\x00\x02\x00"),
		"game.dsk":  []byte("MV - CPC"),
		"cut.pzx":   append(newPZX().b, 'P', 'U', 'L', 'S', 9, 0, 0, 0, 1),
	}
	for name, data := range cases {
		if _, err := Convert(data); err == nil {
			t.Errorf("%s converted", name)
		}
	}
}

func TestAdmit(t *testing.T) {
	for _, name := range []string{"a.tzx", "B.TAP", "c.pzx", "d.csw"} {
		if !Admit(name) {
			t.Errorf("%s refused", name)
		}
	}
	for _, name := range []string{"a.p", "b.dsk", "tap", "c.tzx.gz"} {
		if Admit(name) {
			t.Errorf("%s admitted", name)
		}
	}
}

func TestCheckedTAP(t *testing.T) {
	header := []byte{19, 0, 0x00, 3, 'n', 'a', 'm', 'e', ' ', ' ', ' ', ' ', ' ', ' ', 4, 0, 0, 0, 0, 0, 0}
	sum := byte(0)
	for _, v := range header[2:20] {
		sum ^= v
	}
	header[20] = sum
	if !CheckedTAP(header) || !CheckedTAP(append(append([]byte{}, header...), header...)) {
		t.Fatal("a checked .tap was not taken")
	}
	bad := append([]byte{}, header...)
	bad[20] ^= 1
	for name, data := range map[string][]byte{
		"bad checksum":   append(append([]byte{}, header...), bad...),
		"one-byte block": {1, 0, 0},
		"empty block":    {0, 0},
		"short":          header[:20],
		"TZX":            append([]byte("ZXTape!\x1a\x01\x14"), header...),
	} {
		if CheckedTAP(data) {
			t.Errorf("%s: taken as a checked .tap", name)
		}
	}
}
