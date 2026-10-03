package pack

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

func loadFesComputer(t *testing.T) *ABIFile {
	t.Helper()
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages", "abi", "fes_computer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return abi
}

func TestFesComputerContract(t *testing.T) {
	abi := loadFesComputer(t)
	if abi.ID != "fes.computer" || abi.Tag != 4 || abi.Major != 1 || abi.Minor != 0 {
		t.Fatalf("ABI identity %s tag %d %d.%d", abi.ID, abi.Tag, abi.Major, abi.Minor)
	}
	for name, want := range map[string]uint32{
		"FesComputerAbiTag": uint32(abi.Tag), "FesComputerAbiMajor": uint32(abi.Major),
		"FesComputerAbiMinor": uint32(abi.Minor), "FesComputerSignature": 0xf5000000,
		"FesComputerOpcodeKeyboardHid": 3, "FesComputerOpcodeControllerButtons": 4,
		"FesComputerOpcodeMediaInfo": 5, "FesComputerOpcodeMediaBegin": 6,
		"FesComputerOpcodeMediaChunk": 7, "FesComputerOpcodeMediaData": 8,
		"FesComputerOpcodeMediaCommit": 9, "FesComputerOpcodeMediaEject": 10,
		"FesComputerKeyboardRowCount": 9, "FesComputerKeyboardModifierRow": 8,
		"FesComputerMediaUnitCount": 8, "FesComputerMediaInfoStride": 8,
		"FesComputerMediaHeaderStride": 4, "FesComputerMediaChunkMaxBytes": 512,
		"FesComputerMediaMaxBytes": 33554432, "FesComputerMediaCRC32Polynomial": 0xedb88320,
		"FesComputerApple2FloppyUnit": 0, "FesComputerApple2FloppyBytes": 143360,
		"FesComputerSpectrumTapeUnit": 0, "FesComputerSpectrumTapeMinBytes": 1,
		"FesComputerSpectrumTapeMaxBytes": 65536,
		"FesComputerC64DiskUnit": 0, "FesComputerC64DiskBytes": 174848,
		"FesComputerAtariStFloppyUnit": 0, "FesComputerAtariStFloppyBytes": 737280,
	} {
		got, ok := abi.Constant(name)
		if !ok || got != want {
			t.Errorf("%s = %d (%v), want %d", name, got, ok, want)
		}
	}
	want := []struct {
		id  string
		bit uint8
	}{
		{"fes.video.fixed-720p60", 0}, {"fes.keyboard.hid", 1}, {"fes.gamepad.ports", 2},
		{"fes.audio.pcm-s16-stereo-48k", 3}, {"fes.media.apple2-floppy", 4},
		{"fes.media.spectrum-tape", 5}, {"fes.media.c64-disk", 6},
		{"fes.media.atari-st-floppy", 7},
	}
	if len(abi.Interfaces) != len(want) {
		t.Fatalf("interfaces = %+v", abi.Interfaces)
	}
	for i, w := range want {
		got := abi.Interfaces[i]
		if got.ID != w.id || got.CapabilityBit != w.bit || got.Major != 1 || got.Minor != 0 {
			t.Errorf("interface %d = %+v, want %s bit %d", i, got, w.id, w.bit)
		}
	}
}

// computerModel is an independent replay of docs/computer-io.md.
type computerModel struct {
	caps       uint16
	units      map[uint32][2]uint32
	state      map[uint32]uint16
	data       map[uint32][]byte
	held       bool
	rows       [9]uint16
	ports      [2]uint16
	beginUnit  int
	beginWords []uint32
	active     int
	total      uint32
	wantCRC    uint32
	received   uint32
	crc        uint32
	chunkWords []uint32
	chunkLen   uint32
	chunkRecv  uint32
	ordinal    uint32
}

func (m *computerModel) cancel() {
	m.beginUnit, m.active = -1, -1
	m.beginWords, m.chunkWords = nil, nil
	m.chunkLen, m.chunkRecv, m.ordinal = 0, 0, 0
}

func (m *computerModel) step(op, index, arg uint32, words []uint16) (code, data uint16) {
	const hasMedia = 1 << 4
	switch {
	case op == 1:
		if index >= 16 {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		}
		return 0, words[index]
	case op == 2:
		if index != 0 {
			return 2, 0
		} else if arg > 1 {
			return 3, 0
		}
		m.held = arg == 0
		if m.held {
			m.rows, m.ports = [9]uint16{}, [2]uint16{}
		}
		return 0, 0
	case op == 3:
		if m.caps&2 == 0 {
			return 1, 0
		} else if index > 8 {
			return 2, 0
		} else if index == 0 && arg&0xf != 0 || index == 8 && arg > 0xff {
			return 3, 0
		}
		m.rows[index] = uint16(arg)
		return 0, 0
	case op == 4:
		if m.caps&4 == 0 {
			return 1, 0
		} else if index > 1 {
			return 2, 0
		} else if arg > 0xff {
			return 3, 0
		}
		m.ports[index] = uint16(arg)
		return 0, 0
	case op >= 5 && op <= 10 && m.caps&hasMedia == 0:
		return 1, 0
	case op == 5:
		unit, field := index>>3, index&7
		if unit >= 8 || field > 5 {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		}
		limits, ok := m.units[unit]
		if !ok {
			return 0, 0
		}
		return 0, [6]uint16{uint16(limits[0]), uint16(limits[0] >> 16), uint16(limits[1]),
			uint16(limits[1] >> 16), 512, m.state[unit]}[field]
	case op == 6:
		unit, word := index>>2, index&3
		if _, ok := m.units[unit]; !ok {
			return 2, 0
		} else if m.active >= 0 {
			return 4, 0
		} else if m.beginUnit < 0 && word != 0 || m.beginUnit >= 0 && (int(unit) != m.beginUnit || int(word) != len(m.beginWords)) {
			return 2, 0
		}
		if word == 3 {
			total := m.beginWords[0] | m.beginWords[1]<<16
			if total < m.units[unit][0] || total > m.units[unit][1] {
				return 3, 0
			}
			m.active, m.total, m.wantCRC = int(unit), total, m.beginWords[2]|arg<<16
			m.received, m.crc, m.beginUnit, m.beginWords = 0, 0, -1, nil
			m.data[unit] = nil
			return 0, 0
		}
		if word == 0 {
			m.beginUnit = int(unit)
			m.state[unit] = 2
		}
		m.beginWords = append(m.beginWords, arg)
		return 0, 0
	case op == 7:
		unit, word := index>>2, index&3
		if m.active < 0 {
			return 4, 0
		} else if int(unit) != m.active || int(word) != len(m.chunkWords) || word > 2 {
			return 2, 0
		} else if m.chunkLen != 0 {
			return 4, 0
		}
		if word == 2 {
			offset := m.chunkWords[0] | m.chunkWords[1]<<16
			if offset != m.received || arg < 1 || arg > 512 || arg > m.total-m.received {
				return 3, 0
			}
			m.chunkWords, m.chunkLen, m.chunkRecv, m.ordinal = nil, arg, 0, 0
			return 0, 0
		}
		m.chunkWords = append(m.chunkWords, arg)
		return 0, 0
	case op == 8:
		if m.active < 0 || m.chunkLen == 0 {
			return 4, 0
		} else if index != m.ordinal {
			return 2, 0
		}
		payload := []byte{byte(arg), byte(arg >> 8)}
		if m.chunkLen-m.chunkRecv == 1 {
			if arg>>8 != 0 {
				return 3, 0
			}
			payload = payload[:1]
		}
		unit := uint32(m.active)
		m.data[unit] = append(m.data[unit], payload...)
		m.crc = crc32.Update(m.crc, crc32.IEEETable, payload)
		m.received += uint32(len(payload))
		m.chunkRecv += uint32(len(payload))
		m.ordinal++
		if m.chunkRecv == m.chunkLen {
			m.chunkLen = 0
		}
		return 0, 0
	case op == 9:
		if _, ok := m.units[index]; !ok {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		} else if m.active < 0 || m.beginUnit >= 0 {
			return 4, 0
		} else if int(index) != m.active {
			return 2, 0
		} else if m.chunkLen != 0 || m.received != m.total {
			return 4, 0
		} else if m.crc != m.wantCRC {
			return 3, 0
		}
		m.state[index] = 3
		m.cancel()
		return 0, 0
	case op == 10:
		if _, ok := m.units[index]; !ok {
			return 2, 0
		} else if arg != 0 {
			return 3, 0
		}
		if m.active == int(index) || m.beginUnit == int(index) {
			m.cancel()
		}
		m.state[index] = 1
		return 0, 0
	}
	return 1, 0
}

func TestFesComputerGoldenExchanges(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "fes-computer-v1", "exchanges.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ABI struct {
			ID                string
			Tag, Major, Minor uint16
		}
		CRCVectors []struct {
			Hex   string
			CRC32 uint32
		} `json:"crc_vectors"`
		Scenarios []struct {
			Name         string
			Capabilities uint16
			BuildID      string `json:"build_id"`
			Units        []struct{ Unit, Min, Max uint32 }
			Initial      bool `json:"initial_request_toggle"`
			Exchanges    []struct {
				Name                    string
				Opcode, Index, Argument uint32
				Error, Data             uint16
				GPO                     []uint32
				GPI                     uint32
			}
			Final struct {
				Held            bool
				KeyboardRows    [9]uint16         `json:"keyboard_rows"`
				ControllerPorts [2]uint16         `json:"controller_ports"`
				UnitStates      map[string]uint16 `json:"unit_states"`
				UnitCRC32       map[string]uint32 `json:"unit_sha_crc32"`
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.ABI.ID != "fes.computer" || fixture.ABI.Tag != 4 || len(fixture.Scenarios) != 2 {
		t.Fatalf("fixture identity %+v", fixture.ABI)
	}
	for _, v := range fixture.CRCVectors {
		raw, err := hex.DecodeString(v.Hex)
		if err != nil || crc32.ChecksumIEEE(raw) != v.CRC32 {
			t.Fatalf("crc vector %s", v.Hex)
		}
	}
	for _, s := range fixture.Scenarios {
		t.Run(s.Name, func(t *testing.T) {
			build, err := hex.DecodeString(s.BuildID)
			if err != nil || len(build) != 16 {
				t.Fatal("build id")
			}
			words := []uint16{0x4546, 0x3153, 1, 0, 4, 1, 0, s.Capabilities}
			for i := 0; i < 16; i += 2 {
				words = append(words, uint16(build[i])|uint16(build[i+1])<<8)
			}
			m := &computerModel{caps: s.Capabilities, units: map[uint32][2]uint32{},
				state: map[uint32]uint16{}, data: map[uint32][]byte{}, held: true, beginUnit: -1, active: -1}
			for _, u := range s.Units {
				m.units[u.Unit] = [2]uint32{u.Min, u.Max}
				m.state[u.Unit] = 1
			}
			toggle := s.Initial
			for n, x := range s.Exchanges {
				fields := x.Opcode<<24 | x.Index<<16 | x.Argument
				old := fields
				if toggle {
					old |= 0x80000000
				}
				toggle = !toggle
				if len(x.GPO) != 2 || x.GPO[0] != old || x.GPO[1] != old^0x80000000 {
					t.Fatalf("%s: request framing", x.Name)
				}
				if n < 16 && (x.Opcode != 1 || x.Index != uint32(n)) {
					t.Fatalf("%s: identity must precede activation", x.Name)
				}
				code, value := m.step(x.Opcode, x.Index, x.Argument, words)
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
					t.Fatalf("%s: got error %d data %#x gpi %#x, model %d %#x %#x",
						x.Name, x.Error, x.Data, x.GPI, code, response, want)
				}
			}
			if m.held != s.Final.Held || m.rows != s.Final.KeyboardRows || m.ports != s.Final.ControllerPorts {
				t.Fatalf("final input state %+v", s.Final)
			}
			for unit, state := range m.state {
				key := fmt.Sprint(unit)
				if s.Final.UnitStates[key] != state || s.Final.UnitCRC32[key] != crc32.ChecksumIEEE(m.data[unit]) {
					t.Fatalf("unit %d final state", unit)
				}
			}
		})
	}
}
