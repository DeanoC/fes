package spectrumsnapshots

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// ------------------------------------------------------------ restore model

// Model is a core's machine, as its build model numbers it.
type Model uint8

const (
	Model48K Model = iota
	Model128K
	ModelPlus3
	ModelPentagon
)

// machine runs a restore stream the way a core does: every memory read comes
// from the stream, writes go to RAM through the model's paging, and OUTs reach
// the ports. It executes only the instructions Stream writes.
type machine struct {
	model                          Model
	banks                          [8][bankBytes]byte
	p7ffd, p1ffd, border, ayIndex  uint8
	ay                             [16]uint8
	a, f, b, c, d, e, h, l         uint8
	a2, f2, b2, c2, d2, e2, h2, l2 uint8
	ix, iy, sp, pc                 uint16
	i, r, im                       uint8
	iff1                           bool
	stream                         []byte
	fetches                        int
}

func (m *machine) next() uint8 {
	v := m.stream[0]
	m.stream = m.stream[1:]
	return v
}
func (m *machine) m1() uint8 { m.r = m.r&0x80 | (m.r+1)&0x7f; m.fetches++; return m.next() }
func (m *machine) imm16() uint16 {
	lo := m.next()
	return uint16(lo) | uint16(m.next())<<8
}

func (m *machine) write(a uint16, v uint8) {
	slot := a >> 14
	if m.model == Model48K {
		if slot != 0 {
			m.banks[[]int{0, 5, 2, 0}[slot]][a&0x3fff] = v
		}
		return
	}
	if m.model == ModelPlus3 && m.p1ffd&1 != 0 {
		panic("write in an all-RAM mode")
	}
	switch slot {
	case 1:
		m.banks[5][a&0x3fff] = v
	case 2:
		m.banks[2][a&0x3fff] = v
	case 3:
		m.banks[m.p7ffd&7][a&0x3fff] = v
	}
}

func (m *machine) out(port uint16, v uint8) {
	locked := m.p7ffd&0x20 != 0
	switch {
	case port&1 == 0:
		m.border = v & 7
	case m.model == ModelPlus3 && port>>12 == 1 && port&2 == 0:
		if !locked {
			m.p1ffd = v & 7
		}
	case m.model == ModelPlus3 && port>>14 == 1 && port&2 == 0,
		m.model != Model48K && m.model != ModelPlus3 && port&0x8000 == 0 && port&2 == 0:
		if !locked {
			m.p7ffd = v
		}
	case m.model != Model48K && port>>14 == 3 && port&2 == 0:
		m.ayIndex = v & 15
	case m.model != Model48K && port>>14 == 2 && port&2 == 0:
		m.ay[m.ayIndex] = v
	}
}

func (m *machine) run(t *testing.T) {
	t.Helper()
	if !bytes.Equal(m.stream[:4], Magic[:]) || m.stream[4] != Version || m.stream[5] > byte(MachinePlus3) ||
		m.stream[6] != 0 || m.stream[7] != 0 {
		t.Fatalf("header % x", m.stream[:8])
	}
	m.stream = m.stream[HeaderBytes:]
	m.sp = 0xffff
	for len(m.stream) > 0 {
		switch op := m.m1(); op {
		case 0xf3:
			m.iff1 = false
		case 0xfb:
			m.iff1 = true
		case 0x01:
			v := m.imm16()
			m.b, m.c = uint8(v>>8), uint8(v)
		case 0x11:
			v := m.imm16()
			m.d, m.e = uint8(v>>8), uint8(v)
		case 0x21:
			v := m.imm16()
			m.h, m.l = uint8(v>>8), uint8(v)
		case 0x31:
			m.sp = m.imm16()
		case 0x06:
			m.b = m.next()
		case 0x3e:
			m.a = m.next()
		case 0x36:
			m.write(uint16(m.h)<<8|uint16(m.l), m.next())
		case 0x23:
			hl := uint16(m.h)<<8 | uint16(m.l) + 1
			m.h, m.l = uint8(hl>>8), uint8(hl)
		case 0xd3:
			m.out(uint16(m.a)<<8|uint16(m.next()), m.a)
		case 0xd9:
			m.b, m.b2, m.c, m.c2 = m.b2, m.b, m.c2, m.c
			m.d, m.d2, m.e, m.e2 = m.d2, m.d, m.e2, m.e
			m.h, m.h2, m.l, m.l2 = m.h2, m.h, m.l2, m.l
		case 0x08:
			m.a, m.a2, m.f, m.f2 = m.a2, m.a, m.f2, m.f
		case 0xf1:
			m.f = m.next()
			m.a = m.next()
			m.sp += 2
		case 0xc3:
			m.pc = m.imm16()
			if len(m.stream) != 0 {
				t.Fatal("stream continues after JP")
			}
		case 0xdd, 0xfd:
			if m.m1() != 0x21 {
				t.Fatal("unexpected index opcode")
			}
			if op == 0xdd {
				m.ix = m.imm16()
			} else {
				m.iy = m.imm16()
			}
		case 0xed:
			switch op2 := m.m1(); op2 {
			case 0x79:
				m.out(uint16(m.b)<<8|uint16(m.c), m.a)
			case 0x47:
				m.i = m.a
			case 0x4f:
				m.r = m.a
			case 0x46, 0x56, 0x5e:
				m.im = map[uint8]uint8{0x46: 0, 0x56: 1, 0x5e: 2}[op2]
			default:
				t.Fatalf("unexpected ED %02x", op2)
			}
		default:
			t.Fatalf("unexpected opcode %02x", op)
		}
	}
}

// --------------------------------------------------------- snapshot builders

func randomBanks(rng *rand.Rand, banks ...int) map[int]*[bankBytes]byte {
	out := map[int]*[bankBytes]byte{}
	for _, b := range banks {
		out[b] = newBank()
		// Runs of equal bytes exercise the .z80 compression.
		for i := 0; i < bankBytes; {
			v, n := byte(rng.Intn(256)), 1+rng.Intn(12)
			if rng.Intn(4) == 0 {
				n = 40 + rng.Intn(300)
			}
			for ; n > 0 && i < bankBytes; n-- {
				out[b][i] = v
				i++
			}
		}
	}
	return out
}

func sampleState(machine Machine, rng *rand.Rand) *Snapshot {
	s := &Snapshot{Machine: machine,
		A: 0x12, F: 0xd7, B: 0x34, C: 0x56, D: 0x78, E: 0x9a, H: 0xbc, L: 0xde,
		A2: 0x21, F2: 0x7d, B2: 0x43, C2: 0x65, D2: 0x87, E2: 0xa9, H2: 0xcb, L2: 0xed,
		IX: 0x1357, IY: 0x2468, SP: 0xff40, PC: 0x8123, I: 0x3f, R: 0x85, IM: 1,
		IFF1: true, IFF2: true, Border: 5}
	if machine == Machine48K {
		s.Banks = randomBanks(rng, 5, 2, 0)
	} else {
		s.Banks = randomBanks(rng, 0, 1, 2, 3, 4, 5, 6, 7)
		s.Port7FFD = 0x1b // bank 3, shadow screen, ROM 1
		s.HasAY = true
		s.AYSelect = 7
		for i := range s.AY {
			s.AY[i] = uint8(0x10 + 7*i)
		}
	}
	return s
}

// deflate writes the .z80 run-length coding.
func deflate(in []byte) []byte {
	var out []byte
	for i := 0; i < len(in); {
		n := 1
		for i+n < len(in) && in[i+n] == in[i] && n < 255 {
			n++
		}
		if n >= 5 || in[i] == 0xed && n >= 2 {
			out = append(out, 0xed, 0xed, byte(n), in[i])
			i += n
			continue
		}
		out = append(out, in[i])
		// A single ED is followed by one literal byte.
		if in[i] == 0xed && i+1 < len(in) {
			out = append(out, in[i+1])
			i++
		}
		i++
	}
	return out
}

func z80Header(s *Snapshot, pc uint16, compressed bool) []byte {
	flags1 := s.R>>7 | s.Border<<1
	if compressed {
		flags1 |= 0x20
	}
	h := []byte{s.A, s.F, s.C, s.B, s.L, s.H, byte(pc), byte(pc >> 8), byte(s.SP), byte(s.SP >> 8),
		s.I, s.R & 0x7f, flags1, s.E, s.D, s.C2, s.B2, s.E2, s.D2, s.L2, s.H2, s.A2, s.F2,
		byte(s.IY), byte(s.IY >> 8), byte(s.IX), byte(s.IX >> 8), b2u(s.IFF1), b2u(s.IFF2), s.IM}
	return h
}

func b2u(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func z80v1(s *Snapshot, compressed bool) []byte {
	out := z80Header(s, s.PC, compressed)
	ram := append(append(append([]byte{}, s.Banks[5][:]...), s.Banks[2][:]...), s.Banks[0][:]...)
	if compressed {
		return append(append(out, deflate(ram)...), 0, 0xed, 0xed, 0)
	}
	return append(out, ram...)
}

func z80v3(s *Snapshot, mode byte, extra int) []byte {
	out := z80Header(s, 0, false)
	x := make([]byte, extra)
	binary.LittleEndian.PutUint16(x, s.PC)
	x[2] = mode
	x[3] = s.Port7FFD
	if s.HasAY {
		x[5] = 4
		x[6] = s.AYSelect
		copy(x[7:], s.AY[:])
	}
	if extra == 55 {
		x[54] = s.Port1FFD
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(extra))
	out = append(out, x...)
	pages := map[int]int{5: 8, 2: 4, 0: 5}
	if s.Machine != Machine48K {
		pages = map[int]int{}
		for b := 0; b < 8; b++ {
			pages[b] = b + 3
		}
	}
	for b := 0; b < 8; b++ {
		page, ok := pages[b]
		if !ok {
			continue
		}
		data := deflate(s.Banks[b][:])
		if b == 1 {
			// One uncompressed page.
			out = append(out, 0xff, 0xff, byte(page))
			out = append(out, s.Banks[b][:]...)
			continue
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(len(data)))
		out = append(out, byte(page))
		out = append(out, data...)
	}
	return out
}

func sna(s *Snapshot) []byte {
	iff := byte(0)
	if s.IFF2 {
		iff = 4
	}
	sp := s.SP
	if s.Machine == Machine48K {
		// A 48K .sna holds PC on the stack, which stays in RAM.
		sp -= 2
		for i, v := range []byte{byte(s.PC), byte(s.PC >> 8)} {
			a := sp + uint16(i)
			s.Banks[[]int{0, 5, 2, 0}[a>>14]][a&0x3fff] = v
		}
	}
	h := []byte{s.I, s.L2, s.H2, s.E2, s.D2, s.C2, s.B2, s.F2, s.A2, s.L, s.H, s.E, s.D, s.C, s.B,
		byte(s.IY), byte(s.IY >> 8), byte(s.IX), byte(s.IX >> 8), iff, s.R, s.F, s.A,
		byte(sp), byte(sp >> 8), s.IM, s.Border}
	paged := int(s.Port7FFD & 7)
	if s.Machine == Machine48K {
		paged = 0
	}
	out := append(h, s.Banks[5][:]...)
	out = append(out, s.Banks[2][:]...)
	out = append(out, s.Banks[paged][:]...)
	if s.Machine == Machine48K {
		return out
	}
	out = append(out, byte(s.PC), byte(s.PC>>8), s.Port7FFD, 0)
	for b := 0; b < 8; b++ {
		if b != 5 && b != 2 && b != paged {
			out = append(out, s.Banks[b][:]...)
		}
	}
	return out
}

// ------------------------------------------------------------------ checks

func restore(t *testing.T, data []byte, model Model, want Machine) *machine {
	t.Helper()
	got, stream, err := Convert(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || stream[5] != byte(want) {
		t.Fatalf("machine %d, class %d, want %d", got, stream[5], want)
	}
	m := &machine{model: model, stream: stream}
	m.run(t)
	return m
}

func checkRegisters(t *testing.T, m *machine, s *Snapshot) {
	t.Helper()
	got := []uint16{uint16(m.a)<<8 | uint16(m.f), uint16(m.b)<<8 | uint16(m.c), uint16(m.d)<<8 | uint16(m.e),
		uint16(m.h)<<8 | uint16(m.l), uint16(m.a2)<<8 | uint16(m.f2), uint16(m.b2)<<8 | uint16(m.c2),
		uint16(m.d2)<<8 | uint16(m.e2), uint16(m.h2)<<8 | uint16(m.l2), m.ix, m.iy, m.sp, m.pc,
		uint16(m.i), uint16(m.im), uint16(m.border)}
	want := []uint16{uint16(s.A)<<8 | uint16(s.F), uint16(s.B)<<8 | uint16(s.C), uint16(s.D)<<8 | uint16(s.E),
		uint16(s.H)<<8 | uint16(s.L), uint16(s.A2)<<8 | uint16(s.F2), uint16(s.B2)<<8 | uint16(s.C2),
		uint16(s.D2)<<8 | uint16(s.E2), uint16(s.H2)<<8 | uint16(s.L2), s.IX, s.IY, s.SP, s.PC,
		uint16(s.I), uint16(s.IM), uint16(s.Border)}
	names := strings.Fields("AF BC DE HL AF' BC' DE' HL' IX IY SP PC I IM border")
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %04x, want %04x", names[i], got[i], want[i])
		}
	}
	// The opcode fetch at PC increments R from the snapshot's value, as the
	// snapshot's next fetch would.
	if m.r != s.R {
		t.Errorf("R = %02x at the jump, want %02x", m.r, s.R)
	}
	if m.iff1 != s.IFF1 {
		t.Errorf("IFF1 = %v, want %v", m.iff1, s.IFF1)
	}
}

func checkBanks(t *testing.T, m *machine, s *Snapshot, banks ...int) {
	t.Helper()
	for _, b := range banks {
		if !bytes.Equal(m.banks[b][:], s.Banks[b][:]) {
			t.Errorf("bank %d differs", b)
		}
	}
}

func TestZ80Version1On48K(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		s := sampleState(Machine48K, rand.New(rand.NewSource(1)))
		m := restore(t, z80v1(s, compressed), Model48K, Machine48K)
		checkRegisters(t, m, s)
		checkBanks(t, m, s, 5, 2, 0)
	}
}

func TestZ80Version1RejectsSamRom(t *testing.T) {
	s := sampleState(Machine48K, rand.New(rand.NewSource(11)))
	data := z80v1(s, false)
	data[12] |= 0x10
	if Recognize(data) {
		t.Fatal("version-1 SamRom snapshot was recognized")
	}
}

func Test48KSnapshotOnEveryModel(t *testing.T) {
	s := sampleState(Machine48K, rand.New(rand.NewSource(2)))
	s.IFF1, s.IFF2, s.IM, s.R = false, false, 2, 0x02
	snaData := sna(s) // writes PC to the stack first
	for _, data := range [][]byte{z80v3(s, 0, 54), snaData} {
		for model := Model48K; model <= ModelPentagon; model++ {
			m := restore(t, data, model, Machine48K)
			checkRegisters(t, m, s)
			checkBanks(t, m, s, 5, 2, 0)
			if model != Model48K && m.p7ffd != 0x30 {
				t.Errorf("model %d: $7FFD = %02x, want 48 BASIC locked", model, m.p7ffd)
			}
			if model == ModelPlus3 && m.p1ffd != 0x04 {
				t.Errorf("+3: $1FFD = %02x, want ROM 3", m.p1ffd)
			}
		}
	}
}

func Test48KSnapshotWithAY(t *testing.T) {
	s := sampleState(Machine48K, rand.New(rand.NewSource(9)))
	s.HasAY, s.AYSelect = true, 11
	for i := range s.AY {
		s.AY[i] = uint8(0x30 + 5*i)
	}
	data := z80v3(s, 0, 54)
	for _, model := range []Model{Model128K, ModelPlus3, ModelPentagon} {
		m := restore(t, data, model, Machine48K)
		checkRegisters(t, m, s)
		if m.ay != s.AY || m.ayIndex != s.AYSelect {
			t.Errorf("model %d: AY = % x select %d, want % x select %d", model, m.ay, m.ayIndex, s.AY, s.AYSelect)
		}
	}
	// A Fuller box's AY sits on other ports; the cores have none.
	data[32+5] |= 0x40
	if got, err := Read(data); err != nil || got.HasAY {
		t.Errorf("Fuller box: AY restored (%v)", err)
	}
}

func Test128KSnapshots(t *testing.T) {
	s := sampleState(Machine128K, rand.New(rand.NewSource(3)))
	for _, data := range [][]byte{z80v3(s, 4, 54), z80v3(s, 3, 23), z80v3(s, 9, 54), sna(s)} {
		for _, model := range []Model{Model128K, ModelPlus3, ModelPentagon} {
			m := restore(t, data, model, Machine128K)
			checkRegisters(t, m, s)
			checkBanks(t, m, s, 0, 1, 2, 3, 4, 5, 6, 7)
			if m.p7ffd != s.Port7FFD {
				t.Errorf("$7FFD = %02x, want %02x", m.p7ffd, s.Port7FFD)
			}
			if model == ModelPlus3 && m.p1ffd != 0x04 {
				t.Errorf("+3: $1FFD = %02x, want ROM 3 for the 128K's ROM 1", m.p1ffd)
			}
			if bytes.Equal(data[:3], sna(s)[:3]) && len(data) == len(sna(s)) {
				continue // a .sna has no AY state
			}
			if m.ay != s.AY || m.ayIndex != s.AYSelect {
				t.Errorf("AY = % x select %d, want % x select %d", m.ay, m.ayIndex, s.AY, s.AYSelect)
			}
		}
	}
}

func TestSNAWithBank5Paged(t *testing.T) {
	s := sampleState(Machine128K, rand.New(rand.NewSource(4)))
	s.Port7FFD = 0x05
	// The file holds the six banks other than 5 and 2.
	data := sna(s)
	if len(data) != 27+3*bankBytes+4+6*bankBytes {
		t.Fatalf(".sna is %d bytes", len(data))
	}
	m := restore(t, data, Model128K, Machine128K)
	checkRegisters(t, m, s)
	checkBanks(t, m, s, 0, 1, 2, 3, 4, 5, 6, 7)
}

func TestSNARejectsInconsistentDuplicatePagedBank(t *testing.T) {
	for _, bank := range []int{2, 5} {
		t.Run(fmt.Sprintf("bank_%d", bank), func(t *testing.T) {
			s := sampleState(Machine128K, rand.New(rand.NewSource(int64(bank))))
			s.Port7FFD = byte(bank)
			data := sna(s)
			thirdPage := 27 + 2*bankBytes
			data[thirdPage] ^= 1
			if Recognize(data) {
				t.Fatalf("inconsistent duplicate bank %d was recognized", bank)
			}
		})
	}
}

func TestPlus3Snapshot(t *testing.T) {
	s := sampleState(MachinePlus3, rand.New(rand.NewSource(5)))
	s.Port7FFD = 0x07
	s.Port1FFD = 0x05 // all-RAM 4,5,6,7
	data := z80v3(s, 7, 55)
	m := restore(t, data, ModelPlus3, MachinePlus3)
	checkRegisters(t, m, s)
	checkBanks(t, m, s, 0, 1, 2, 3, 4, 5, 6, 7)
	if m.p1ffd != 0x05 || m.p7ffd != 0x07 {
		t.Errorf("paging $1FFD %02x $7FFD %02x", m.p1ffd, m.p7ffd)
	}
}

func TestRefusedSnapshots(t *testing.T) {
	s := sampleState(Machine48K, rand.New(rand.NewSource(6)))
	for name, data := range map[string][]byte{
		"16K":       func() []byte { d := z80v3(s, 0, 54); d[32+5] |= 0x80; return d }(),
		"SamRam":    z80v3(s, 2, 54),
		"MGT":       z80v3(s, 3, 54),
		"Scorpion":  z80v3(s, 10, 54),
		"Timex":     z80v3(s, 14, 54),
		"IF1 paged": func() []byte { d := z80v3(s, 1, 54); d[32+4] = 0xff; return d }(),
		"IF1 paged +3": func() []byte {
			d := z80v3(sampleState(MachinePlus3, rand.New(rand.NewSource(12))), 7, 55)
			d[32+4] = 0xff
			return d
		}(),
		"IF1 paged +3 alias mode": func() []byte {
			d := z80v3(sampleState(MachinePlus3, rand.New(rand.NewSource(13))), 8, 55)
			d[32+4] = 0xff
			return d
		}(),
		"IF1 paged +2A": func() []byte {
			d := z80v3(sampleState(MachinePlus3, rand.New(rand.NewSource(14))), 13, 55)
			d[32+4] = 0xff
			return d
		}(),
		"SamRom":     func() []byte { d := z80v1(s, false); d[12] |= 0x10; return d }(),
		"truncated":  z80v1(s, false)[:1000],
		"bad marker": func() []byte { d := z80v1(s, true); d[len(d)-2] = 0; return d }(),
		"TR-DOS": func() []byte {
			t := sampleState(Machine128K, rand.New(rand.NewSource(7)))
			d := sna(t)
			d[27+3*bankBytes+3] = 1
			return d
		}(),
	} {
		if Recognize(data) {
			t.Errorf("%s: recognized", name)
		}
	}
}

func TestInflateRejectsTruncatedRLEMarkers(t *testing.T) {
	for name, marker := range map[string][]byte{
		"missing count": {0xed, 0xed},
		"missing value": {0xed, 0xed, 1},
	} {
		t.Run(name, func(t *testing.T) {
			input := append(bytes.Repeat([]byte{0}, bankBytes-len(marker)), marker...)
			if _, err := inflate(input, bankBytes); err == nil {
				t.Fatal("truncated RLE marker was accepted")
			}
		})
	}
}

func TestAdmit(t *testing.T) {
	for name, want := range map[string]bool{"a.z80": true, "B.SNA": true, "c.tzx": false, "d": false} {
		if Admit(name) != want {
			t.Errorf("Admit(%q) = %v", name, !want)
		}
	}
}
