package spectrumsnapshots

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math/rand"
	"testing"
)

// szxChunk is one chunk: a 4-byte id and its data.
type szxChunk struct {
	id   string
	data []byte
}

// szx writes s as libspectrum writes a .szx: the header, CRTR, Z80R, SPCR,
// the RAM pages (zlib compressed when compress is set) and AY, then extra.
func szx(s *Snapshot, machine byte, compress bool, extra ...szxChunk) []byte {
	b := func(v bool) byte {
		if v {
			return 1
		}
		return 0
	}
	regs := []byte{s.F, s.A, s.C, s.B, s.E, s.D, s.L, s.H, s.F2, s.A2, s.C2, s.B2, s.E2, s.D2, s.L2, s.H2}
	for _, w := range []uint16{s.IX, s.IY, s.SP, s.PC} {
		regs = binary.LittleEndian.AppendUint16(regs, w)
	}
	regs = append(regs, s.I, s.R, b(s.IFF1), b(s.IFF2), s.IM, 0x34, 0x12, 0, 0, 0, 0, 0, 0)
	creator := append([]byte("FES test"), make([]byte, 28)...)
	chunks := []szxChunk{
		{"CRTR", creator},
		{"Z80R", regs},
		{"SPCR", []byte{s.Border, s.Port7FFD, s.Port1FFD, s.Border, 0, 0, 0, 0}},
	}
	for bank := 0; bank < 8; bank++ {
		page := s.Banks[bank]
		if page == nil {
			continue
		}
		data := page[:]
		flags := uint16(0)
		if compress {
			var z bytes.Buffer
			w := zlib.NewWriter(&z)
			w.Write(data)
			w.Close()
			data, flags = z.Bytes(), 1
		}
		chunks = append(chunks, szxChunk{"RAMP", append(binary.LittleEndian.AppendUint16(nil, flags), append([]byte{byte(bank)}, data...)...)})
	}
	if s.HasAY {
		chunks = append(chunks, szxChunk{"AY\x00\x00", append([]byte{2, s.AYSelect}, s.AY[:]...)})
	}
	chunks = append(chunks, extra...)
	out := []byte{'Z', 'X', 'S', 'T', 1, 4, machine, 0}
	for _, c := range chunks {
		out = append(out, c.id...)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(c.data)))
		out = append(out, c.data...)
	}
	return out
}

func TestSZX48KOnEveryModel(t *testing.T) {
	s := sampleState(Machine48K, rand.New(rand.NewSource(21)))
	for _, compress := range []bool{false, true} {
		for model := Model48K; model <= ModelPentagon; model++ {
			m := restore(t, szx(s, 1, compress), model, Machine48K)
			checkRegisters(t, m, s)
			checkBanks(t, m, s, 5, 2, 0)
		}
	}
}

func TestSZX128KClasses(t *testing.T) {
	s := sampleState(Machine128K, rand.New(rand.NewSource(22)))
	// 128K, +2 and Pentagon 128 snapshots all run as 128K ones.
	for _, machine := range []byte{2, 3, 7} {
		for _, model := range []Model{Model128K, ModelPlus3, ModelPentagon} {
			m := restore(t, szx(s, machine, true), model, Machine128K)
			checkRegisters(t, m, s)
			checkBanks(t, m, s, 0, 1, 2, 3, 4, 5, 6, 7)
			if m.p7ffd != s.Port7FFD || m.ay != s.AY || m.ayIndex != s.AYSelect {
				t.Errorf("machine %d model %d: $7FFD %02x AY % x select %d", machine, model, m.p7ffd, m.ay, m.ayIndex)
			}
		}
	}
}

func TestSZXPlus3(t *testing.T) {
	s := sampleState(MachinePlus3, rand.New(rand.NewSource(23)))
	s.Port7FFD, s.Port1FFD = 0x07, 0x05
	for _, machine := range []byte{4, 5} {
		m := restore(t, szx(s, machine, false), ModelPlus3, MachinePlus3)
		checkRegisters(t, m, s)
		checkBanks(t, m, s, 0, 1, 2, 3, 4, 5, 6, 7)
		if m.p1ffd != 0x05 || m.p7ffd != 0x07 {
			t.Errorf("machine %d: paging $1FFD %02x $7FFD %02x", machine, m.p1ffd, m.p7ffd)
		}
	}
}

func TestSZXOldLibspectrumSwapsAF(t *testing.T) {
	s := sampleState(Machine48K, rand.New(rand.NewSource(24)))
	data := szx(s, 1, true)
	got, err := Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.A != s.A || got.F != s.F {
		t.Fatalf("A %02x F %02x", got.A, got.F)
	}
	for creator, swapped := range map[string]bool{"libspectrum: 0.5.0": true, "libspectrum: 0.4.2": true,
		"libspectrum: 0.5.1": false, "libspectrum: 1.4.4": false} {
		crtr := append(append([]byte("Fuse"), make([]byte, 32)...), creator...)
		d := bytes.Replace(data, data[8:8+8+36], append([]byte("CRTR\x00\x00\x00\x00"), crtr...), 1)
		binary.LittleEndian.PutUint32(d[12:], uint32(len(crtr)))
		got, err := Read(d)
		if err != nil {
			t.Fatal(creator, err)
		}
		want := [4]uint8{s.A, s.F, s.A2, s.F2}
		if swapped {
			want = [4]uint8{s.F, s.A, s.F2, s.A2}
		}
		if [4]uint8{got.A, got.F, got.A2, got.F2} != want {
			t.Errorf("%s: AF %02x%02x AF' %02x%02x", creator, got.A, got.F, got.A2, got.F2)
		}
	}
}

func TestSZXIgnoresIdlePeripherals(t *testing.T) {
	s := sampleState(Machine128K, rand.New(rand.NewSource(25)))
	data := szx(s, 2, true,
		szxChunk{"B128", []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}}, // connected, not paged
		szxChunk{"IF1\x00", []byte{1, 0, 0, 0}},
		szxChunk{"MFCE", []byte{1, 0}},
		szxChunk{"PLTT", append([]byte{0, 0}, make([]byte, 64)...)}, // ULAplus off
		szxChunk{"JOY\x00", []byte{0, 0, 0, 0, 8, 8}},
		szxChunk{"KEYB", []byte{0, 0, 0, 0, 8}},
		szxChunk{"TAPE", make([]byte, 40)})
	m := restore(t, data, Model128K, Machine128K)
	checkRegisters(t, m, s)
}

func TestSZXRefused(t *testing.T) {
	s48 := sampleState(Machine48K, rand.New(rand.NewSource(26)))
	s128 := sampleState(Machine128K, rand.New(rand.NewSource(27)))
	noPage := func(bank int) *Snapshot {
		c := *s128
		c.Banks = map[int]*[bankBytes]byte{}
		for b, p := range s128.Banks {
			if b != bank {
				c.Banks[b] = p
			}
		}
		return &c
	}
	cases := map[string][]byte{
		"16K":           szx(s48, 0, false),
		"+3e":           szx(s128, 6, false),
		"Timex":         szx(s48, 8, false),
		"Scorpion":      szx(s128, 10, false),
		"Pentagon 1024": szx(s128, 14, false),
		"NTSC 48K":      szx(s48, 15, false),
		"unknown":       szx(s48, 99, false),
		"version 2":     func() []byte { d := szx(s48, 1, false); d[4] = 2; return d }(),
		"TR-DOS paged":  szx(s128, 2, false, szxChunk{"B128", []byte{5, 0, 0, 0, 0, 0, 0, 0, 0, 0}}),
		"IF1 paged":     szx(s48, 1, false, szxChunk{"IF1\x00", []byte{5, 0, 0, 0}}),
		"Multiface in":  szx(s48, 1, false, szxChunk{"MFCE", []byte{0, 1}}),
		"DivMMC paged":  szx(s48, 1, false, szxChunk{"DMMC", []byte{2, 0, 0}}),
		"ULAplus on":    szx(s48, 1, false, szxChunk{"PLTT", append([]byte{1, 0}, make([]byte, 64)...)}),
		"IF2 cartridge": szx(s48, 1, false, szxChunk{"IF2R", make([]byte, 8)}),
		"custom ROM":    szx(s48, 1, false, szxChunk{"ROM\x00", make([]byte, 8)}),
		"missing page":  szx(noPage(6), 2, true),
		"48K has page 1": szx(func() *Snapshot {
			c := *s48
			c.Banks = map[int]*[bankBytes]byte{5: s48.Banks[5], 2: s48.Banks[2], 0: s48.Banks[0], 1: s48.Banks[0]}
			return &c
		}(), 1, false),
		"bad zlib":  func() []byte { d := szx(s48, 1, true); i := bytes.Index(d, []byte("RAMP")); d[i+8+3] ^= 0xff; return d }(),
		"truncated": szx(s48, 1, false)[:5000],
		"no Z80R":   bytes.Replace(szx(s48, 1, false), []byte("Z80R"), []byte("Z80X"), 1),
	}
	for name, data := range cases {
		if Recognize(data) {
			t.Errorf("%s: recognized", name)
		}
	}
}
