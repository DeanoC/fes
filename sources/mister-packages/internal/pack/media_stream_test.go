package pack

import (
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMediaStreamFullChunksAndLengthAdmission(t *testing.T) {
	for _, total := range []uint32{0, 1, 16384, 16385, 32768, 32769, 65536, 33554432, 33554433, 0xffffffff} {
		s := streamState{Reset: true}
		for i, arg := range []uint32{total & 65535, total >> 16, 0, 0} {
			code, _ := s.exchange(8, uint32(i), arg)
			want := uint32(0)
			if i == 3 && (total == 0 || total > 32768) {
				want = 3
			}
			if code != want {
				t.Fatalf("total=%d index=%d error=%d want=%d", total, i, code, want)
			}
		}
	}
	// Compact generated payload exercises every ordinal, all 64 full chunks,
	// and u32 offsets without putting 32768 bytes in a golden JSON fixture.
	payload := make([]byte, 32768)
	for i := range payload {
		payload[i] = byte(i)
	}
	crc := crc32.ChecksumIEEE(payload)
	s := streamState{Reset: true}
	send := func(op, index, arg uint32) {
		t.Helper()
		if code, _ := s.exchange(op, index, arg); code != 0 {
			t.Fatalf("op=%d index=%d arg=%d error=%d", op, index, arg, code)
		}
	}
	for i, arg := range []uint32{32768, 0, crc & 65535, crc >> 16} {
		send(8, uint32(i), arg)
	}
	for offset := uint32(0); offset < 32768; offset += 512 {
		send(9, 0, offset&65535)
		send(9, 1, offset>>16)
		if code, _ := s.exchange(9, 2, 513); code != 3 {
			t.Fatal("oversized chunk accepted")
		}
		send(9, 2, 512)
		for word := uint32(0); word < 256; word++ {
			pos := offset + word*2
			send(10, word, uint32(payload[pos])|uint32(payload[pos+1])<<8)
		}
		if code, _ := s.exchange(10, 0, 0); code != 4 {
			t.Fatal("ordinal silently wrapped")
		}
	}
	send(11, 0, 0)
	if !s.Ready || !s.Reset || !reflect.DeepEqual(s.Payload, payload) {
		t.Fatal("full transfer differs")
	}
}

func streamJSON(t *testing.T, path string, dst any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatal(err)
	}
}

func TestMediaStreamConstantsAndLegacyOracle(t *testing.T) {
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages/abi/fes_simple_computer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct{ Legacy, Stream map[string]uint32 }
	streamJSON(t, "testdata/oracles/fes-simple-computer-stream.json", &oracle)
	if len(oracle.Legacy) != 45 || len(oracle.Stream) != 25 {
		t.Fatalf("incomplete oracle: %d/%d", len(oracle.Legacy), len(oracle.Stream))
	}
	for _, values := range []map[string]uint32{oracle.Legacy, oracle.Stream} {
		for name, want := range values {
			if got, ok := abi.Constant(name); !ok || got != want {
				t.Errorf("%s=%x present=%t want=%x", name, got, ok, want)
			}
		}
	}
	if len(abi.Constants) != len(oracle.Legacy)+len(oracle.Stream) {
		t.Fatal("unreviewed constant")
	}
	if abi.Major != 1 || abi.Minor != 0 || abi.Tag != 2 {
		t.Fatal("base ABI changed")
	}
	if len(abi.Interfaces) != 4 {
		t.Fatal("unexpected interfaces")
	}
	got := abi.Interfaces[3]
	if got.ID != "fes.media.blob-stream" || got.Major != 1 || got.Minor != 0 || got.CapabilityBit != 3 {
		t.Fatalf("stream interface: %+v", got)
	}
}

// This test-only interpretation checks fixture consistency, not runtime/RTL
// implementation. Consumers must run these vectors against their own endpoint.
type streamState struct {
	Reset, Ready, Legacy, Active        bool
	Begin                               [4]uint16
	BeginNext                           int
	Chunk                               [3]uint16
	ChunkNext                           int
	Total, ExpectedCRC, Received        uint32
	ChunkLength, ChunkReceived, Ordinal uint32
	Payload                             []byte
}

func (s *streamState) exchange(op, index, arg uint32) (code, data uint32) {
	switch op {
	case 7:
		if index > 4 {
			return 2, 0
		}
		if arg != 0 {
			return 3, 0
		}
		return 0, []uint32{1, 0, 32768, 0, 512}[index]
	case 2:
		if index != 0 {
			return 2, 0
		}
		if arg > 1 {
			return 3, 0
		}
		if arg == 1 && (!s.Ready || s.Active || s.BeginNext != 0) {
			return 4, 0
		}
		s.Reset = arg == 0
		return 0, 0
	case 4, 5, 6:
		if s.Active || s.BeginNext != 0 {
			return 4, 0
		}
		panic("legacy exchanges belong to the unchanged legacy fixture")
	case 8:
		if !s.Reset || s.Legacy || s.Active {
			return 4, 0
		}
		if index > 3 || int(index) != s.BeginNext {
			return 2, 0
		}
		if index == 3 {
			total := uint32(s.Begin[0]) | uint32(s.Begin[1])<<16
			if total < 1 || total > 32768 {
				return 3, 0
			}
			s.Total = total
			s.ExpectedCRC = uint32(s.Begin[2]) | arg<<16
			s.Active = true
			s.BeginNext = 0
			s.Begin = [4]uint16{}
			s.Received = 0
			s.Payload = nil
		} else {
			s.Begin[index] = uint16(arg)
			s.BeginNext++
		}
		s.Ready = false
		return 0, 0
	case 9:
		if !s.Reset || !s.Active || s.ChunkLength != 0 {
			return 4, 0
		}
		if index > 2 || int(index) != s.ChunkNext {
			return 2, 0
		}
		if index == 2 {
			offset := uint32(s.Chunk[0]) | uint32(s.Chunk[1])<<16
			if offset != s.Received || arg < 1 || arg > 512 || s.Received > s.Total || arg > s.Total-s.Received {
				return 3, 0
			}
			s.ChunkLength = arg
			s.ChunkReceived = 0
			s.Ordinal = 0
			s.ChunkNext = 0
			s.Chunk = [3]uint16{}
		} else {
			s.Chunk[index] = uint16(arg)
			s.ChunkNext++
		}
		return 0, 0
	case 10:
		if !s.Reset || !s.Active || s.ChunkLength == 0 {
			return 4, 0
		}
		if index > 255 || index != s.Ordinal {
			return 2, 0
		}
		remaining := s.ChunkLength - s.ChunkReceived
		if remaining == 1 && arg > 255 {
			return 3, 0
		}
		s.Payload = append(s.Payload, byte(arg))
		s.Received++
		s.ChunkReceived++
		if remaining > 1 {
			s.Payload = append(s.Payload, byte(arg>>8))
			s.Received++
			s.ChunkReceived++
		}
		s.Ordinal++
		if s.ChunkReceived == s.ChunkLength {
			s.ChunkLength = 0
			s.ChunkReceived = 0
			s.Ordinal = 0
		}
		return 0, 0
	case 11:
		if index != 0 {
			return 2, 0
		}
		if arg != 0 {
			return 3, 0
		}
		if !s.Reset || !s.Active || s.ChunkNext != 0 || s.ChunkLength != 0 || s.Received != s.Total {
			return 4, 0
		}
		if crc32.ChecksumIEEE(s.Payload) != s.ExpectedCRC {
			return 3, 0
		}
		s.Active = false
		s.Ready = true
		return 0, 0
	case 12:
		if index != 0 {
			return 2, 0
		}
		if arg != 0 {
			return 3, 0
		}
		if !s.Reset || s.Legacy {
			return 4, 0
		}
		*s = streamState{Reset: true}
		return 0, 0
	default:
		return 1, 0
	}
}

func TestMediaStreamGoldenExchanges(t *testing.T) {
	var f struct {
		Interface     string
		Major, Minor  uint32
		CapabilityBit uint32 `json:"capability_bit"`
		Endpoint      struct {
			Min, Max uint32
			ChunkMax uint32 `json:"chunk_max"`
		}
		CRCVectors []struct {
			Hex   string
			CRC32 uint32
		} `json:"crc_vectors"`
		SizeVectors []struct {
			Total, Lo, Hi   uint32
			SMSAccept       bool `json:"sms_accept"`
			TransportAccept bool `json:"transport_accept"`
		} `json:"size_vectors"`
		Scenarios []struct {
			Name          string
			InitialReset  *bool `json:"initial_reset"`
			Legacy        bool  `json:"legacy_active"`
			InitialToggle bool  `json:"initial_request_toggle"`
			Ready, Reset  bool
			Received      uint32
			Exchanges     []struct {
				Name                                      string
				Opcode, Index, Argument, Error, Data, GPI uint32
				GPO                                       []uint32
			}
		}
	}
	streamJSON(t, "testdata/fes-media-stream-v1/exchanges.json", &f)
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages/abi/fes_simple_computer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, iface := range abi.Interfaces {
		if iface.ID == f.Interface {
			found = true
			if uint32(iface.Major) != f.Major || uint32(iface.Minor) != f.Minor || uint32(iface.CapabilityBit) != f.CapabilityBit {
				t.Fatal("fixture interface metadata differs from YAML")
			}
		}
	}
	if !found || f.Interface != "fes.media.blob-stream" {
		t.Fatal("missing stream fixture interface")
	}
	constant := func(suffix string) uint32 {
		t.Helper()
		value, ok := abi.Constant("FesSimpleComputer" + suffix)
		if !ok {
			t.Fatalf("missing YAML constant %s", suffix)
		}
		return value
	}
	if f.Endpoint.Min != constant("MediaStreamMinBytes") || f.Endpoint.Max != constant("MediaStreamGuaranteedMaxBytes") || f.Endpoint.Max > constant("MediaStreamMaxBytes") || f.Endpoint.ChunkMax != constant("MediaStreamChunkMaxBytes") {
		t.Fatal("fixture endpoint differs from YAML SMS limits")
	}
	info := map[uint32]uint32{
		constant("MediaStreamInfoMinLoIndex"):    f.Endpoint.Min & 65535,
		constant("MediaStreamInfoMinHiIndex"):    f.Endpoint.Min >> 16,
		constant("MediaStreamInfoMaxLoIndex"):    f.Endpoint.Max & 65535,
		constant("MediaStreamInfoMaxHiIndex"):    f.Endpoint.Max >> 16,
		constant("MediaStreamInfoChunkMaxIndex"): f.Endpoint.ChunkMax,
	}
	infoSeen := make(map[uint32]bool)
	for _, scenario := range f.Scenarios {
		for _, e := range scenario.Exchanges {
			if e.Opcode == constant("OpcodeMediaStreamInfo") && e.Error == 0 {
				want, ok := info[e.Index]
				if !ok || e.Argument != 0 || e.Data != want || e.GPI&constant("ResponseMask") != want {
					t.Fatalf("%s Info differs from endpoint metadata", e.Name)
				}
				infoSeen[e.Index] = true
			}
		}
	}
	if len(infoSeen) != len(info) {
		t.Fatal("missing endpoint Info exchanges")
	}
	if len(f.CRCVectors) != 2 || len(f.SizeVectors) != 10 || len(f.Scenarios) != 7 {
		t.Fatal("missing contract vectors")
	}
	for _, v := range f.CRCVectors {
		b, err := hex.DecodeString(v.Hex)
		if err != nil {
			t.Fatal(err)
		}
		if crc32.ChecksumIEEE(b) != v.CRC32 {
			t.Fatal("CRC vector differs from IEEE")
		}
		// Independently check the specified reflected recurrence.
		crc := uint32(0xffffffff)
		for _, x := range b {
			crc ^= uint32(x)
			for bit := 0; bit < 8; bit++ {
				if crc&1 != 0 {
					crc = (crc >> 1) ^ 0xedb88320
				} else {
					crc >>= 1
				}
			}
		}
		if crc^0xffffffff != v.CRC32 {
			t.Fatal("CRC recurrence mismatch")
		}
	}
	for _, v := range f.SizeVectors {
		if v.Lo|v.Hi<<16 != v.Total || v.Lo > 65535 || v.Hi > 65535 ||
			v.SMSAccept != (v.Total >= 1 && v.Total <= 32768) || v.TransportAccept != (v.Total >= 1 && v.Total <= 33554432) {
			t.Fatalf("size vector %+v", v)
		}
	}
	for _, scenario := range f.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			s := streamState{Reset: true, Legacy: scenario.Legacy}
			if scenario.InitialReset != nil {
				s.Reset = *scenario.InitialReset
			}
			toggle := scenario.InitialToggle
			for _, e := range scenario.Exchanges {
				before := s
				before.Payload = append([]byte(nil), s.Payload...)
				code, data := s.exchange(e.Opcode, e.Index, e.Argument)
				if code != e.Error {
					t.Fatalf("%s error=%d want=%d", e.Name, code, e.Error)
				}
				if code != 0 {
					if !reflect.DeepEqual(s, before) {
						t.Fatalf("%s invalid request mutated state", e.Name)
					}
					data = code
				}
				if data != e.Data {
					t.Fatalf("%s data=%x want=%x", e.Name, data, e.Data)
				}
				word := e.Opcode<<24 | e.Index<<16 | e.Argument
				first := word
				if toggle {
					first |= 0x80000000
				}
				toggle = !toggle
				second := word
				gpi := uint32(0xf5000000) | data
				if toggle {
					second |= 0x80000000
					gpi |= 0x00800000
				}
				if code != 0 {
					gpi |= 0x00400000
				}
				if len(e.GPO) != 2 || e.GPO[0] != first || e.GPO[1] != second || e.GPI != gpi {
					t.Fatalf("%s wire mismatch", e.Name)
				}
			}
			if s.Ready != scenario.Ready || s.Reset != scenario.Reset || s.Received != scenario.Received {
				t.Fatalf("final state %+v", s)
			}
		})
	}
}
