package pack

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// v11Model is an independent replay of the ABI 1.1 additions in
// docs/computer-io.md: identity word 16, the ATA disk, persistence words and
// the wall clock. Legacy opcodes go to computerModel.
type v11Model struct {
	legacy      *computerModel
	caps        uint32
	minor       uint16
	window      []byte
	verifyReads int

	disk, pending   uint16
	frozen, saved   bool
	writerBusy      bool
	pendingSector   uint32
	failed          bool
	epoch           uint16
	dirty           map[uint32]bool
	commitWords     []uint32
	geometry        []uint32
	verifyRemaining int

	words    [72]uint16
	snapshot *[72]uint16
	staging  map[uint32]uint16

	clockWords []uint32
	clock      []int
}

const (
	stateEmpty, stateLoading, stateReady, stateVerifying = 1, 2, 3, 4
	capPersistence                                       = 1 << 17
	capPC386Machine                                      = 1 << 19
)

func newV11Model(caps uint32, minor uint16, window []byte, verifyReads int) *v11Model {
	m := &v11Model{
		legacy: &computerModel{caps: uint16(caps), units: map[uint32][2]uint32{}, state: map[uint32]uint16{},
			data: map[uint32][]byte{}, held: true, beginUnit: -1, active: -1},
		caps: caps, minor: minor, window: window, verifyReads: verifyReads,
		disk: stateEmpty, dirty: map[uint32]bool{},
	}
	copy(m.words[:8], []uint16{0, 0, 0, 0, 0x220, 7, 1, 0})
	return m
}

func (m *v11Model) flags() uint16 {
	var f uint16
	if len(m.dirty) != 0 {
		f |= 1
	}
	if m.frozen {
		f |= 2
	}
	if m.writerBusy {
		f |= 4
	}
	if m.failed {
		f |= 8
	}
	return f
}

func (m *v11Model) reset(state uint16) {
	m.disk, m.frozen, m.saved, m.failed = state, false, false, false
	m.writerBusy, m.pendingSector = false, 0
	m.dirty, m.commitWords, m.geometry, m.verifyRemaining = map[uint32]bool{}, nil, nil, 0
}

func machineValid(w []uint16) bool {
	preset, freq := w[0], w[1]
	switch {
	case preset > 7 || w[2] > 1 || w[3] > 1 || w[7] != 0 || w[6] != 1:
		return false
	case freq != 0 && (preset == 0 || freq < 10 || freq > 1000):
		return false
	case w[4] != 0x220 && w[4] != 0x240:
		return false
	}
	return w[5] == 2 || w[5] == 3 || w[5] == 5 || w[5] == 7
}

func daysIn(year, month uint32) uint32 {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

func (m *v11Model) step(op, index, arg uint32, identity []uint16) (uint16, uint16) {
	held := m.legacy.held
	switch op {
	case 1:
		if index >= uint32(len(identity)) {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		}
		return 0, identity[index]
	case 2:
		code, data := m.legacy.step(op, index, arg, identity)
		if code == 0 {
			if m.legacy.held {
				m.snapshot = nil
			}
			m.staging, m.clockWords = nil, nil
		}
		return code, data
	case 16, 17, 22, 23:
		if m.caps&(1<<16) == 0 {
			return 1, 0
		}
	case 24, 25, 26, 27:
		persistenceCaps := uint32(capPersistence | capPC386Machine)
		if m.caps&persistenceCaps != persistenceCaps {
			return 1, 0
		}
	case 28:
		if m.caps&(1<<18) == 0 {
			return 1, 0
		}
	default:
		return m.legacy.step(op, index, arg, identity)
	}
	switch op {
	case 16:
		if index>>3 >= 8 {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		} else if index>>3 != 0 {
			return 0, 0
		}
		state := m.disk
		if index&7 == 5 && m.disk == stateVerifying {
			if m.verifyRemaining--; m.verifyRemaining == 0 {
				m.disk = m.pending
				m.failed = m.pending != stateReady
			}
		}
		return 0, [8]uint16{0, 0x10, 0, 0x800, 128, state, m.flags(), m.epoch}[index&7]
	case 17:
		if index != 0 {
			return 2, 0
		} else if arg > 5 {
			return 3, 0
		}
		switch arg {
		case 0, 2:
			if !held || m.frozen && !m.saved {
				return 4, 0
			}
			if arg == 0 {
				m.reset(stateLoading)
			} else {
				m.reset(stateEmpty)
			}
		case 1:
			if !held || m.disk != stateLoading || len(m.commitWords) != 8 {
				return 4, 0
			}
			w := m.commitWords
			sectors := w[0] | w[1]<<16
			ok := crc32.ChecksumIEEE(m.window[:sectors*512]) == w[5]|w[6]<<16
			m.commitWords, m.disk, m.verifyRemaining = nil, stateVerifying, m.verifyReads
			m.failed = false
			if ok {
				m.pending, m.geometry = stateReady, []uint32{sectors, w[2], w[3], w[4]}
			} else {
				m.pending, m.geometry = stateLoading, nil
			}
		case 3:
			if m.disk != stateReady {
				return 4, 0
			}
			m.frozen, m.saved = true, false
			if m.writerBusy {
				if err := m.finishSectorWrite(); err != nil {
					return 4, 0
				}
			}
		case 4:
			m.frozen, m.saved = false, false
		case 5:
			if !m.frozen || m.writerBusy {
				return 4, 0
			}
			m.dirty, m.saved = map[uint32]bool{}, true
		}
		return 0, 0
	case 22:
		if index > 7 {
			return 2, 0
		} else if !held || m.disk != stateLoading {
			return 4, 0
		} else if index != uint32(len(m.commitWords)) {
			return 2, 0
		}
		w := m.commitWords
		bad := false
		switch index {
		case 1:
			size := w[0] | arg<<16
			bad = size < 2048 || size > 262144
		case 2:
			bad = arg < 1 || arg > 16383
		case 3:
			bad = arg < 1 || arg > 16
		case 4:
			bad = arg < 1 || arg > 63 || w[2]*w[3]*arg > w[0]|w[1]<<16
		case 7:
			bad = arg != 0
		}
		if bad {
			return 3, 0
		}
		m.commitWords = append(m.commitWords, arg)
		return 0, 0
	case 23:
		if index >= 128 {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		} else if !m.frozen {
			return 4, 0
		}
		var word uint16
		for bit := uint32(0); bit < 16; bit++ {
			if m.dirty[index*16+bit] {
				word |= 1 << bit
			}
		}
		return 0, word
	case 24:
		if index != 0 {
			return 2, 0
		}
		switch arg {
		case 0:
			if held {
				return 4, 0
			}
			if m.snapshot == nil {
				snap := m.words
				m.snapshot = &snap
			}
		case 1:
			if !held {
				return 4, 0
			}
			m.staging = map[uint32]uint16{}
		case 2:
			if !held || m.staging == nil || len(m.staging) != 72 {
				return 4, 0
			}
			staged := make([]uint16, 72)
			for i := range staged {
				staged[i] = m.staging[uint32(i)]
			}
			if !machineValid(staged) {
				return 3, 0
			}
			copy(m.words[:], staged)
			m.staging = nil
		case 3:
			if m.snapshot == nil {
				return 4, 0
			}
			m.snapshot = nil
		default:
			return 3, 0
		}
		return 0, 0
	case 25:
		if index >= 72 {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		} else if m.snapshot == nil {
			return 4, 0
		}
		return 0, m.snapshot[index]
	case 26:
		if index >= 72 {
			return 2, 0
		} else if !held || m.staging == nil {
			return 4, 0
		}
		m.staging[index] = uint16(arg)
		return 0, 0
	case 27:
		if index > 3 {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		}
		return 0, [4]uint16{72, 3, 1, 0}[index]
	}
	// 28: wall clock
	if index > 3 {
		return 2, 0
	} else if !held {
		return 4, 0
	} else if index != 0 && index != uint32(len(m.clockWords)) {
		return 2, 0
	}
	lo, hi := arg&0xff, arg>>8
	var ok bool
	switch index {
	case 0:
		ok = arg >= 1980 && arg <= 2099
	case 1:
		ok = lo >= 1 && lo <= 12 && hi >= 1 && hi <= daysIn(m.clockWords[0], lo)
	case 2:
		ok = lo <= 23 && hi <= 59
	case 3:
		ok = lo <= 59 && hi >= 1 && hi <= 7
	}
	if !ok {
		return 3, 0
	}
	if index == 0 {
		m.clockWords = nil
	}
	m.clockWords = append(m.clockWords, arg)
	if index == 3 {
		w := m.clockWords
		m.clock = []int{int(w[0]), int(w[1] & 0xff), int(w[1] >> 8), int(w[2] & 0xff), int(w[2] >> 8),
			int(w[3] & 0xff), int(w[3] >> 8)}
		m.clockWords = nil
	}
	return 0, 0
}

func (m *v11Model) beginSectorWrite(sector uint32) error {
	if m.disk != stateReady || m.legacy.held || m.frozen || m.writerBusy ||
		len(m.geometry) == 0 || sector >= m.geometry[0] {
		return fmt.Errorf("sector write is not accepted in the current machine state")
	}
	m.writerBusy, m.pendingSector = true, sector
	return nil
}

func (m *v11Model) finishSectorWrite() error {
	if !m.writerBusy {
		return fmt.Errorf("no accepted sector write to finish")
	}
	m.dirty[m.pendingSector/128] = true
	m.epoch++
	m.writerBusy, m.pendingSector = false, 0
	return nil
}

func (m *v11Model) event(kind string, sector, byteIndex, value uint32) error {
	switch kind {
	case "sector-write":
		if err := m.beginSectorWrite(sector); err != nil {
			return err
		}
		return m.finishSectorWrite()
	case "sector-write-begin":
		return m.beginSectorWrite(sector)
	case "sector-write-complete":
		return m.finishSectorWrite()
	case "cmos-write":
		if m.legacy.held || byteIndex >= 128 || value > 0xff {
			return fmt.Errorf("CMOS write is not accepted in the current machine state")
		}
		shift := 8 * (byteIndex & 1)
		w := &m.words[8+byteIndex/2]
		*w = *w&^(0xff<<shift) | uint16(value)<<shift
	default:
		return fmt.Errorf("unknown machine event %q", kind)
	}
	return nil
}

func TestFesComputer11GoldenExchanges(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "fes-computer-v1", "exchanges-1.1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ABI struct {
			ID                string
			Tag, Major, Minor uint16
		}
		Scenarios []struct {
			Name          string
			Capabilities  uint32
			AbiMinor      uint16 `json:"abi_minor"`
			IdentityWords int    `json:"identity_word_count"`
			VerifyReads   int    `json:"verify_reads"`
			BuildID       string `json:"build_id"`
			Window        struct{ Bytes int }
			Events        []struct {
				Before              int
				Kind                string
				Sector, Byte, Value uint32
			} `json:"machine_events"`
			Initial   bool `json:"initial_request_toggle"`
			Exchanges []struct {
				Name                    string
				Opcode, Index, Argument uint32
				Error, Data             uint16
				GPO                     []uint32
				GPI                     uint32
			}
			Final struct {
				Held       bool
				DiskState  uint16   `json:"disk_state"`
				DiskFlags  uint16   `json:"disk_flags"`
				DiskEpoch  uint16   `json:"disk_epoch"`
				DiskDirty  []uint32 `json:"disk_dirty_chunks"`
				DiskGeom   []uint32 `json:"disk_geometry"`
				Persistent []uint16 `json:"persistent_words"`
				WallClock  []int    `json:"wall_clock"`
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.ABI.ID != "fes.computer" || fixture.ABI.Tag != 4 || fixture.ABI.Major != 1 || fixture.ABI.Minor != 1 ||
		len(fixture.Scenarios) != 5 {
		t.Fatalf("fixture identity %+v", fixture.ABI)
	}
	for _, s := range fixture.Scenarios {
		t.Run(s.Name, func(t *testing.T) {
			build, err := hex.DecodeString(s.BuildID)
			if err != nil || len(build) != 16 {
				t.Fatal("build id")
			}
			identity := []uint16{0x4546, 0x3153, 1, 0, 4, 1, s.AbiMinor, uint16(s.Capabilities)}
			for i := 0; i < 16; i += 2 {
				identity = append(identity, uint16(build[i])|uint16(build[i+1])<<8)
			}
			if s.AbiMinor == 1 {
				identity = append(identity, uint16(s.Capabilities>>16))
			} else if s.Capabilities>>16 != 0 {
				t.Fatal("a 1.0 endpoint cannot advertise bits 16..31")
			}
			if len(identity) != s.IdentityWords {
				t.Fatalf("identity word count %d", s.IdentityWords)
			}
			window := make([]byte, s.Window.Bytes)
			for offset := range window {
				window[offset] = byte(offset*7 + offset>>11)
			}
			m := newV11Model(s.Capabilities, s.AbiMinor, window, s.VerifyReads)
			toggle, next := s.Initial, 0
			for n, x := range s.Exchanges {
				for ; next < len(s.Events) && s.Events[next].Before == n; next++ {
					e := s.Events[next]
					if err := m.event(e.Kind, e.Sector, e.Byte, e.Value); err != nil {
						t.Fatalf("machine event %s: %v", e.Kind, err)
					}
				}
				fields := x.Opcode<<24 | x.Index<<16 | x.Argument
				old := fields
				if toggle {
					old |= 0x80000000
				}
				toggle = !toggle
				if len(x.GPO) != 2 || x.GPO[0] != old || x.GPO[1] != old^0x80000000 {
					t.Fatalf("%s: request framing", x.Name)
				}
				if n < len(identity) && (x.Opcode != 1 || x.Index != uint32(n)) {
					t.Fatalf("%s: identity must precede activation", x.Name)
				}
				code, value := m.step(x.Opcode, x.Index, x.Argument, identity)
				response := value
				if code != 0 {
					response = code
				}
				want := uint32(0xf5000000) | uint32(response)
				if toggle {
					want |= 0x800000
				}
				if code != 0 {
					want |= 0x400000
				}
				if x.Error != code || x.Data != response || x.GPI != want {
					t.Fatalf("%s: fixture error %d data %#x gpi %#x, model %d %#x %#x",
						x.Name, x.Error, x.Data, x.GPI, code, response, want)
				}
			}
			if next != len(s.Events) {
				t.Fatal("machine events after the last exchange")
			}
			dirty := []uint32{}
			for chunk := uint32(0); chunk < 2048; chunk++ {
				if m.dirty[chunk] {
					dirty = append(dirty, chunk)
				}
			}
			geometry := m.geometry
			if geometry == nil {
				geometry = []uint32{}
			}
			clock := m.clock
			if clock == nil {
				clock = []int{}
			}
			f := s.Final
			if m.legacy.held != f.Held || m.disk != f.DiskState || m.flags() != f.DiskFlags || m.epoch != f.DiskEpoch ||
				!reflect.DeepEqual(dirty, f.DiskDirty) || !reflect.DeepEqual(geometry, f.DiskGeom) ||
				!reflect.DeepEqual(m.words[:], f.Persistent) || !reflect.DeepEqual(clock, f.WallClock) {
				t.Fatalf("final state differs: model disk %d flags %d epoch %d dirty %v geometry %v clock %v",
					m.disk, m.flags(), m.epoch, dirty, geometry, clock)
			}
		})
	}
}

func TestFesComputer11WriterBusyGuards(t *testing.T) {
	newReadyDisk := func() *v11Model {
		m := newV11Model(1<<16, 1, nil, 1)
		m.legacy.held = false
		m.disk = stateReady
		m.geometry = []uint32{2048, 1, 1, 1}
		return m
	}

	t.Run("freeze drains accepted write before ack", func(t *testing.T) {
		m := newReadyDisk()
		if err := m.event("sector-write-begin", 512, 0, 0); err != nil {
			t.Fatal(err)
		}
		if got := m.flags(); got != 4 {
			t.Fatalf("writer-busy flags %d, want 4", got)
		}
		if code, _ := m.step(17, 0, 3, nil); code != 0 {
			t.Fatalf("freeze error %d, want success", code)
		}
		if !m.frozen || m.writerBusy || !m.dirty[4] || m.epoch != 1 || m.flags() != 3 {
			t.Fatalf("freeze did not drain write: frozen=%t busy=%t dirty=%v epoch=%d flags=%d",
				m.frozen, m.writerBusy, m.dirty, m.epoch, m.flags())
		}
	})

	t.Run("saved rejects busy writer even when frozen", func(t *testing.T) {
		m := newReadyDisk()
		m.frozen, m.writerBusy, m.pendingSector = true, true, 512
		m.dirty[1] = true
		if code, _ := m.step(17, 0, 5, nil); code != 4 {
			t.Fatalf("Saved error %d, want state error", code)
		}
		if m.saved || !m.writerBusy || !m.dirty[1] {
			t.Fatalf("rejected Saved changed state: saved=%t busy=%t dirty=%v", m.saved, m.writerBusy, m.dirty)
		}
	})

	t.Run("freeze revokes saved eject authorization", func(t *testing.T) {
		m := newReadyDisk()
		m.legacy.held = true
		m.frozen, m.saved = true, true
		if code, _ := m.step(17, 0, 3, nil); code != 0 {
			t.Fatalf("repeat Freeze error %d, want success", code)
		}
		if !m.frozen || m.saved {
			t.Fatalf("Freeze did not clear Saved: frozen=%t saved=%t", m.frozen, m.saved)
		}
		if code, _ := m.step(17, 0, 2, nil); code != 4 {
			t.Fatalf("Eject after repeat Freeze error %d, want state error", code)
		}
	})

	t.Run("guest write event rejects frozen disk", func(t *testing.T) {
		m := newReadyDisk()
		m.frozen = true
		if err := m.event("sector-write", 0, 0, 0); err == nil {
			t.Fatal("frozen disk accepted guest sector write")
		}
	})
}
