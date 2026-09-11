package input

import (
	"sync"

	"errors"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// KeyboardSink maps FogCast key frames onto the ZX81 ULA matrix and posts
// them through the native runtime set_keyboard operation.
type KeyboardSink struct {
	mu      sync.Mutex
	pressed map[remoteinput.Code]bool
	poster  func(uint64) error
}

func NewKeyboardSink() *KeyboardSink {
	return &KeyboardSink{pressed: map[remoteinput.Code]bool{}}
}

func (s *KeyboardSink) SetPoster(poster func(uint64) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poster = poster
}

func (s *KeyboardSink) Apply(f protocol.InputFrame) error {
	if f.Kind != uint8(remoteinput.KindKey) && f.Device != uint8(remoteinput.DeviceKeyboard) {
		return errors.New("unsupported input frame")
	}
	code := remoteinput.Code(f.Code)
	s.mu.Lock()
	if f.Action == uint8(remoteinput.ActionPress) {
		s.pressed[code] = true
	} else if f.Action == uint8(remoteinput.ActionRelease) {
		delete(s.pressed, code)
	}
	matrix := zx81keys.Matrix(s.pressed)
	poster := s.poster
	s.mu.Unlock()
	if poster == nil {
		return nil
	}
	return poster(matrix)
}

func (s *KeyboardSink) ReleaseAll() error {
	s.mu.Lock()
	s.pressed = map[remoteinput.Code]bool{}
	poster := s.poster
	s.mu.Unlock()
	if poster == nil {
		return nil
	}
	return poster(zx81keys.Neutral)
}

func (s *KeyboardSink) Close() error { return s.ReleaseAll() }

type muxSink struct {
	keys *KeyboardSink
	pads bridge.Sink
}

func (m muxSink) Apply(f protocol.InputFrame) error {
	if keyboardFrame(f) {
		if m.keys != nil && f.Code >= uint16(zx81keys.KeyShift) {
			return m.keys.Apply(f)
		}
		return nil
	}
	if m.pads != nil {
		return m.pads.Apply(f)
	}
	return nil
}

func keyboardFrame(f protocol.InputFrame) bool {
	return f.Device == uint8(remoteinput.DeviceKeyboard) || f.Kind == uint8(remoteinput.KindKey)
}

func (m muxSink) ReleaseAll() error {
	var keyErr, padErr error
	if m.keys != nil {
		keyErr = m.keys.ReleaseAll()
	}
	if m.pads != nil {
		padErr = m.pads.ReleaseAll()
	}
	if keyErr != nil {
		return keyErr
	}
	return padErr
}

func (m muxSink) Close() error {
	var keyErr, padErr error
	if m.keys != nil {
		keyErr = m.keys.Close()
	}
	if m.pads != nil {
		padErr = m.pads.Close()
	}
	if keyErr != nil {
		return keyErr
	}
	return padErr
}
