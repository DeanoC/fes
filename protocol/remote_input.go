package protocol

import (
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	InputVersion     uint8 = 1
	InputTypeInput   uint8 = 1
	inputMagic             = "MSTR"
	inputHeaderSize        = 16
	inputPayloadSize       = 32
)

type InputHeader struct {
	Type    uint8
	Flags   uint16
	Session uint64
}
type InputFrame struct {
	Header       InputHeader
	Seq          uint32
	ClientMonoNS uint64
	ServerMonoNS uint64
	Player       uint8
	Device       uint8
	Kind         uint8
	Action       uint8
	Code         uint16
	Reserved     uint16
	Value        int32
}

func EncodeInputFrame(f InputFrame) ([]byte, error) {
	if f.Header.Type != InputTypeInput || f.Header.Session == 0 || f.Header.Flags != 0 || f.Player > 3 || f.Device > 1 || f.Kind > 2 || f.Action > 2 || f.Reserved != 0 {
		return nil, fmt.Errorf("invalid input frame")
	}
	b := make([]byte, 4+inputHeaderSize+inputPayloadSize)
	binary.LittleEndian.PutUint32(b, uint32(len(b)-4))
	copy(b[4:8], inputMagic)
	b[8] = InputVersion
	b[9] = f.Header.Type
	binary.LittleEndian.PutUint16(b[10:], f.Header.Flags)
	binary.LittleEndian.PutUint64(b[12:], f.Header.Session)
	p := b[20:]
	binary.LittleEndian.PutUint32(p, f.Seq)
	binary.LittleEndian.PutUint64(p[4:], f.ClientMonoNS)
	binary.LittleEndian.PutUint64(p[12:], f.ServerMonoNS)
	p[20] = f.Player
	p[21] = f.Device
	p[22] = f.Kind
	p[23] = f.Action
	binary.LittleEndian.PutUint16(p[24:], f.Code)
	binary.LittleEndian.PutUint16(p[26:], f.Reserved)
	binary.LittleEndian.PutUint32(p[28:], uint32(f.Value))
	return b, nil
}
func DecodeInputFrame(r io.Reader, max uint32) (InputFrame, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return InputFrame{}, err
	}
	if n > max || n != inputHeaderSize+inputPayloadSize {
		return InputFrame{}, fmt.Errorf("invalid frame length %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return InputFrame{}, err
	}
	if string(b[:4]) != inputMagic || b[4] != InputVersion {
		return InputFrame{}, fmt.Errorf("invalid frame header")
	}
	f := InputFrame{Header: InputHeader{Type: b[5], Flags: binary.LittleEndian.Uint16(b[6:]), Session: binary.LittleEndian.Uint64(b[8:])}}
	p := b[16:]
	f.Seq = binary.LittleEndian.Uint32(p)
	f.ClientMonoNS = binary.LittleEndian.Uint64(p[4:])
	f.ServerMonoNS = binary.LittleEndian.Uint64(p[12:])
	f.Player = p[20]
	f.Device = p[21]
	f.Kind = p[22]
	f.Action = p[23]
	f.Code = binary.LittleEndian.Uint16(p[24:])
	f.Reserved = binary.LittleEndian.Uint16(p[26:])
	f.Value = int32(binary.LittleEndian.Uint32(p[28:]))
	if f.Header.Type != InputTypeInput || f.Header.Session == 0 || f.Header.Flags != 0 || f.Player > 3 || f.Device > 1 || f.Kind > 2 || f.Action > 2 || f.Reserved != 0 {
		return InputFrame{}, fmt.Errorf("invalid input fields")
	}
	return f, nil
}

type SequenceTracker struct {
	last uint32
	seen bool
}

func (s *SequenceTracker) Accept(seq uint32) bool {
	accepted, _ := s.Observe(seq)
	return accepted
}

// Observe accepts monotonic sequence numbers and reports forward gaps.
func (s *SequenceTracker) Observe(seq uint32) (accepted, gap bool) {
	if !s.seen || seq >= s.last {
		if s.seen && seq > s.last && seq-s.last > 1 {
			gap = true
		}
		s.last = seq
		s.seen = true
		return true, gap
	}
	return false, false
}

type SessionHello struct {
	Version uint8
	Session uint64
	Token   []byte
}
type SessionGuard struct {
	Version uint8
	Session uint64
	Token   []byte
}

func (g SessionGuard) Validate(h SessionHello) error {
	if h.Version != g.Version || h.Session != g.Session || len(h.Token) != len(g.Token) || subtle.ConstantTimeCompare(h.Token, g.Token) != 1 {
		return fmt.Errorf("session validation failed")
	}
	return nil
}

type StateSnapshot struct {
	Pressed []uint16
	Axes    map[uint16]int16
}
type InputState struct {
	pressed map[uint16]bool
	axes    map[uint16]int16
}

func NewInputState() *InputState {
	return &InputState{pressed: map[uint16]bool{}, axes: map[uint16]int16{}}
}
func (s *InputState) Apply(f InputFrame) error {
	if f.Kind == 0 {
		if f.Action == 1 {
			s.pressed[f.Code] = true
		} else if f.Action == 0 {
			delete(s.pressed, f.Code)
		} else {
			return fmt.Errorf("invalid key action")
		}
		return nil
	}
	if f.Kind == 2 && f.Action == 2 {
		s.axes[f.Code] = int16(f.Value)
		return nil
	}
	return fmt.Errorf("unsupported state event")
}
func (s *InputState) Pressed(c uint16) bool { return s.pressed[c] }
func (s *InputState) ApplySnapshot(x StateSnapshot) {
	s.pressed = map[uint16]bool{}
	for _, c := range x.Pressed {
		s.pressed[c] = true
	}
	s.axes = map[uint16]int16{}
	for c, v := range x.Axes {
		s.axes[c] = v
	}
}
func (s *InputState) ReleaseAll() { s.pressed = map[uint16]bool{}; s.axes = map[uint16]int16{} }
