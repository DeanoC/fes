//go:build linux

package bridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/DeanoC/FogCast/protocol"
	"golang.org/x/sys/unix"
)

// UInputSink writes Linux input_event records to uinput. OpenUInput preserves
// the legacy already-configured-device path; CreateUInputGamepad owns native
// device creation and destruction.
type UInputSink struct {
	file    *os.File
	ioctl   uinputIoctl
	created bool
	native  bool
	write   func([]byte) (int, error)
	mu      sync.Mutex
	pressed map[uint16]bool
	axes    map[uint16]int32
}

func OpenUInput(path string) (Sink, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	return &UInputSink{file: f, write: f.Write, pressed: make(map[uint16]bool), axes: make(map[uint16]int32)}, nil
}

type uinputIoctl func(fd uintptr, request uintptr, value uintptr) error

func realUInputIoctl(fd uintptr, request uintptr, value uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, value)
	if errno != 0 {
		return errno
	}
	return nil
}

// CreateUInputGamepad creates the fixed native FogCast virtual device. Unlike
// OpenUInput, this function owns both UI_DEV_CREATE and UI_DEV_DESTROY.
func CreateUInputGamepad(path string) (Sink, error) {
	return createUInputGamepad(path, realUInputIoctl)
}

func createUInputGamepad(path string, ioctl uinputIoctl) (Sink, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	sink := &UInputSink{
		file:    f,
		ioctl:   ioctl,
		native:  true,
		write:   f.Write,
		pressed: make(map[uint16]bool),
		axes:    make(map[uint16]int32),
	}
	capabilities := []struct {
		request uintptr
		value   uintptr
	}{
		{uiSetEvBit, uintptr(evSyn)},
		{uiSetEvBit, uintptr(evKey)},
		{uiSetEvBit, uintptr(evAbs)},
		{uiSetKeyBit, uintptr(btnDPadUp)},
		{uiSetKeyBit, uintptr(btnDPadDn)},
		{uiSetKeyBit, uintptr(btnDPadL)},
		{uiSetKeyBit, uintptr(btnDPadR)},
		{uiSetKeyBit, uintptr(btnA)},
		{uiSetKeyBit, uintptr(btnB)},
		{uiSetKeyBit, uintptr(btnC)},
		{uiSetKeyBit, uintptr(btnStart)},
		{uiSetKeyBit, uintptr(btnX)},
		{uiSetKeyBit, uintptr(btnY)},
		{uiSetKeyBit, uintptr(btnL)},
		{uiSetKeyBit, uintptr(btnR)},
		{uiSetKeyBit, uintptr(btnSelect)},

		{uiSetAbsBit, uintptr(absX)},
		{uiSetAbsBit, uintptr(absY)},
	}
	for _, capability := range capabilities {
		if err := ioctl(f.Fd(), capability.request, capability.value); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("configure uinput capability: %w", err)
		}
	}
	if _, err := f.Write(gamepadDescriptor()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("configure uinput device: %w", err)
	}
	if err := ioctl(f.Fd(), uiDevCreate, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("create uinput device: %w", err)
	}
	sink.created = true
	return sink, nil
}

func gamepadDescriptor() []byte {
	const (
		nameBytes = 80
		absCount  = 64
		idOffset  = nameBytes
		absMax    = 92
		absMin    = absMax + absCount*4
		userDev   = absMin + 3*absCount*4
	)
	descriptor := make([]byte, userDev)
	copy(descriptor[:nameBytes], "FogCast Virtual Gamepad")
	binary.LittleEndian.PutUint16(descriptor[idOffset:], busVirtual)
	binary.LittleEndian.PutUint16(descriptor[idOffset+2:], 0x0000)
	binary.LittleEndian.PutUint16(descriptor[idOffset+4:], 0x0001)
	binary.LittleEndian.PutUint16(descriptor[idOffset+6:], 0x0001)
	for _, axis := range []int{int(absX), int(absY)} {
		binary.LittleEndian.PutUint32(descriptor[absMax+axis*4:], uint32(32767))
		binary.LittleEndian.PutUint32(descriptor[absMin+axis*4:], 0xffff8000)
	}
	return descriptor
}

func (s *UInputSink) Apply(f protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return errors.New("uinput sink is closed")
	}
	code, eventType, ok := s.mapCode(f)
	if !ok {
		return fmt.Errorf("%w: unsupported input code", errRejectedInputFrame)
	}
	value := int32(0)
	switch f.Action {
	case 0: // release
	case 1: // press
		if eventType != evKey {
			return fmt.Errorf("%w: non-key press", errRejectedInputFrame)
		}
		value = 1
	case 2: // absolute axis value
		if eventType != evAbs {
			return fmt.Errorf("%w: non-axis absolute event", errRejectedInputFrame)
		}
		value = f.Value
	default:
		return fmt.Errorf("%w: unsupported input action", errRejectedInputFrame)
	}
	// A write error does not prove the kernel rejected the event record. Keep
	// enough conservative state to neutralize any non-neutral record that may
	// have arrived before a failed or short SYN_REPORT write.
	if eventType == evKey && value != 0 {
		s.pressed[f.Code] = true
	}
	if eventType == evAbs && value != 0 {
		s.axes[f.Code] = value
	}
	if err := s.writeEvent(eventType, code, value); err != nil {
		return err
	}
	if eventType == evKey && value == 0 {
		delete(s.pressed, f.Code)
	}
	if eventType == evAbs && value == 0 {
		delete(s.axes, f.Code)
	}
	return nil
}

func (s *UInputSink) ReleaseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	for code := range s.pressed {
		frame := protocol.InputFrame{Kind: 0, Code: code}
		if s.native {
			frame.Device = 1
			frame.Kind = 1
			frame.Action = 0
		}
		linux, _, ok := s.mapCode(frame)
		if ok {
			if err := s.writeEvent(evKey, linux, 0); err != nil {
				return err
			}
			delete(s.pressed, code)
		}
	}
	for code := range s.axes {
		frame := protocol.InputFrame{Kind: 2, Action: 2, Code: code}
		if s.native {
			frame.Device = 1
		}
		linux, _, ok := s.mapCode(frame)
		if ok {
			if err := s.writeEvent(evAbs, linux, 0); err != nil {
				return err
			}
			delete(s.axes, code)
		}
	}
	return nil
}

func (s *UInputSink) mapCode(frame protocol.InputFrame) (uint16, uint16, bool) {
	if s.native {
		return nativeLinuxCode(frame)
	}
	return linuxCode(frame)
}

func (s *UInputSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	var destroyErr error
	if s.created {
		destroyErr = s.ioctl(s.file.Fd(), uiDevDestroy, 0)
		s.created = false
	}
	closeErr := s.file.Close()
	s.file = nil
	return errors.Join(destroyErr, closeErr)
}

const (
	uiDevCreate  uintptr = 0x00005501
	uiDevDestroy uintptr = 0x00005502
	uiSetEvBit   uintptr = 0x40045564
	uiSetKeyBit  uintptr = 0x40045565
	uiSetAbsBit  uintptr = 0x40045567
	busVirtual   uint16  = 0x0006
	evSyn        uint16  = 0
	evKey        uint16  = 1
	evAbs        uint16  = 3
	synReport    uint16  = 0
	absX         uint16  = 0
	absY         uint16  = 1
	btnA         uint16  = 304
	btnB         uint16  = 305
	btnC         uint16  = 306
	btnX         uint16  = 307
	btnY         uint16  = 308
	btnL         uint16  = 310
	btnR         uint16  = 311
	btnDPadUp    uint16  = 544
	btnDPadDn    uint16  = 545
	btnDPadL     uint16  = 546
	btnDPadR     uint16  = 547
	btnSelect    uint16  = 314
	btnStart     uint16  = 315
)

func linuxCode(f protocol.InputFrame) (uint16, uint16, bool) {
	if f.Kind == 2 {
		switch f.Code {
		case 200:
			return absX, evAbs, true
		case 201:
			return absY, evAbs, true
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
		104: btnA, 105: btnB, 106: btnStart, 107: btnSelect, protocol.InputCodeButtonC: btnC,
		protocol.InputCodeButtonX: btnX,
		protocol.InputCodeButtonY: btnY,
		protocol.InputCodeButtonL: btnL,
		protocol.InputCodeButtonR: btnR,
	}
	code, ok := keys[f.Code]
	return code, evKey, ok
}

func nativeLinuxCode(f protocol.InputFrame) (uint16, uint16, bool) {
	if f.Player != 0 || f.Device != 1 {
		return 0, 0, false
	}
	if f.Kind == 2 {
		if f.Action != 2 {
			return 0, 0, false
		}
		switch f.Code {
		case 200:
			return absX, evAbs, true
		case 201:
			return absY, evAbs, true
		default:
			return 0, 0, false
		}
	}
	if f.Kind != 1 || (f.Action != 0 && f.Action != 1) {
		return 0, 0, false
	}
	keys := map[uint16]uint16{
		100: btnDPadUp, 101: btnDPadDn, 102: btnDPadL, 103: btnDPadR,
		104: btnA, 105: btnB, 106: btnStart, 107: btnSelect, protocol.InputCodeButtonC: btnC,
		protocol.InputCodeButtonX: btnX,
		protocol.InputCodeButtonY: btnY,
		protocol.InputCodeButtonL: btnL,
		protocol.InputCodeButtonR: btnR,
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
	if err := s.writeRecord(event[:]); err != nil {
		return err
	}
	binary.LittleEndian.PutUint16(event[8:], evSyn)
	binary.LittleEndian.PutUint16(event[10:], synReport)
	binary.LittleEndian.PutUint32(event[12:], 0)
	return s.writeRecord(event[:])
}

func (s *UInputSink) writeRecord(record []byte) error {
	write := s.write
	if write == nil {
		write = s.file.Write
	}
	written, err := write(record)
	if err != nil {
		return err
	}
	if written != len(record) {
		return io.ErrShortWrite
	}
	return nil
}

var _ Sink = (*UInputSink)(nil)
