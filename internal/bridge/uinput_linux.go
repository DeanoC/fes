//go:build linux

package bridge

import (
	"encoding/binary"
	"errors"
	"os"
	"sync"

	"github.com/DeanoC/FogCast-POC/protocol"
)

// UInputSink writes Linux input_event records to an already configured uinput
// device. Device creation is intentionally kept behind OpenUInput so tests can
// use a fake sink; target provisioning must configure the device separately.
type UInputSink struct {
	file    *os.File
	mu      sync.Mutex
	pressed map[uint16]bool
	axes    map[uint16]int32
}

func OpenUInput(path string) (Sink, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	return &UInputSink{file: f, pressed: make(map[uint16]bool), axes: make(map[uint16]int32)}, nil
}

func (s *UInputSink) Apply(f protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return errors.New("uinput sink is closed")
	}
	code, eventType, ok := linuxCode(f)
	if !ok {
		return errors.New("unsupported input code")
	}
	value := int32(0)
	switch f.Action {
	case 0: // release
		if eventType == evKey {
			delete(s.pressed, f.Code)
		}
	case 1: // press
		if eventType != evKey {
			return errors.New("non-key press")
		}
		s.pressed[f.Code] = true
		value = 1
	case 2: // absolute axis value
		if eventType != evAbs {
			return errors.New("non-axis absolute event")
		}
		value = f.Value
		s.axes[f.Code] = value
	default:
		return errors.New("unsupported input action")
	}
	return s.writeEvent(eventType, code, value)
}

func (s *UInputSink) ReleaseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	for code := range s.pressed {
		linux, _, ok := linuxCode(protocol.InputFrame{Kind: 0, Code: code})
		if ok {
			if err := s.writeEvent(evKey, linux, 0); err != nil {
				return err
			}
		}
	}
	for code := range s.axes {
		linux, _, ok := linuxCode(protocol.InputFrame{Kind: 2, Code: code})
		if ok {
			if err := s.writeEvent(evAbs, linux, 0); err != nil {
				return err
			}
		}
	}
	s.pressed = make(map[uint16]bool)
	s.axes = make(map[uint16]int32)
	return nil
}

func (s *UInputSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

const (
	evSyn     uint16 = 0
	evKey     uint16 = 1
	evAbs     uint16 = 3
	synReport uint16 = 0
	btnA      uint16 = 304
	btnB      uint16 = 305
	btnDPadUp uint16 = 544
	btnDPadDn uint16 = 545
	btnDPadL  uint16 = 546
	btnDPadR  uint16 = 547
	btnSelect uint16 = 314
	btnStart  uint16 = 315
)

func linuxCode(f protocol.InputFrame) (uint16, uint16, bool) {
	if f.Kind == 2 {
		switch f.Code {
		case 200:
			return 0, evAbs, true
		case 201:
			return 1, evAbs, true
		default:
			return 0, 0, false
		}
	}
	if f.Kind != 0 && f.Kind != 1 {
		return 0, 0, false
	}
	keys := map[uint16]uint16{
		1: 1, 2: 30, 3: 105, 4: 106, 5: 103, 6: 108,
		100: btnDPadUp, 101: btnDPadDn, 102: btnDPadL, 103: btnDPadR,
		104: btnA, 105: btnB, 106: btnStart, 107: btnSelect,
	}
	code, ok := keys[f.Code]
	return code, evKey, ok
}

// MiSTer is an ARMv7 target: input_event is timeval(8), type(2), code(2),
// value(4), for a 16-byte record. A SYN_REPORT follows every event.
func (s *UInputSink) writeEvent(eventType, code uint16, value int32) error {
	var event [16]byte
	binary.LittleEndian.PutUint16(event[8:], eventType)
	binary.LittleEndian.PutUint16(event[10:], code)
	binary.LittleEndian.PutUint32(event[12:], uint32(value))
	if _, err := s.file.Write(event[:]); err != nil {
		return err
	}
	binary.LittleEndian.PutUint16(event[8:], evSyn)
	binary.LittleEndian.PutUint16(event[10:], synReport)
	binary.LittleEndian.PutUint32(event[12:], 0)
	_, err := s.file.Write(event[:])
	return err
}

var _ Sink = (*UInputSink)(nil)
