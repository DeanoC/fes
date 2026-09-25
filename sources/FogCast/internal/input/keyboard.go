package input

import (
	"context"
	"errors"
	"sync"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// KeyboardSink maps FogCast key frames onto the ZX81 ULA matrix and posts
// them through the native runtime set_keyboard operation. Each source keeps
// its own held keys; the matrix is the union, so one source releasing a key
// leaves another source's hold in place.
type KeyboardSink struct {
	mu      sync.Mutex
	pressed [sourceCount]map[remoteinput.Code]bool
	poster  func(context.Context, uint64) error
}

func NewKeyboardSink() *KeyboardSink {
	return &KeyboardSink{}
}

func (s *KeyboardSink) SetPoster(poster func(context.Context, uint64) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poster = poster
}

func (s *KeyboardSink) Apply(f protocol.InputFrame) error {
	return s.ApplyFrom(context.Background(), sourceRemote, f)
}

func (s *KeyboardSink) ApplyFrom(ctx context.Context, source inputSource, f protocol.InputFrame) error {
	if f.Kind != uint8(remoteinput.KindKey) && f.Device != uint8(remoteinput.DeviceKeyboard) {
		return errors.New("unsupported input frame")
	}
	if source >= sourceCount {
		source = sourceRemote
	}
	code := remoteinput.Code(f.Code)
	s.mu.Lock()
	if s.pressed[source] == nil {
		s.pressed[source] = map[remoteinput.Code]bool{}
	}
	if f.Action == uint8(remoteinput.ActionPress) {
		s.pressed[source][code] = true
	} else if f.Action == uint8(remoteinput.ActionRelease) {
		delete(s.pressed[source], code)
	}
	matrix := zx81keys.Matrix(s.unionLocked())
	poster := s.poster
	s.mu.Unlock()
	if poster == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return poster(ctx, matrix)
}

func (s *KeyboardSink) unionLocked() map[remoteinput.Code]bool {
	union := make(map[remoteinput.Code]bool)
	for source := range s.pressed {
		for code := range s.pressed[source] {
			union[code] = true
		}
	}
	return union
}

func (s *KeyboardSink) ReleaseSource(source inputSource) error {
	if source >= sourceCount {
		source = sourceRemote
	}
	s.mu.Lock()
	s.pressed[source] = map[remoteinput.Code]bool{}
	matrix := zx81keys.Matrix(s.unionLocked())
	poster := s.poster
	s.mu.Unlock()
	if poster == nil {
		return nil
	}
	// Source release is not a local frame under the lifecycle lock. Callers
	// that need a deadline pass one through ApplyFrom.
	_ = poster(context.Background(), matrix)
	return nil
}

func (s *KeyboardSink) ReleaseAll() error {
	s.mu.Lock()
	for source := range s.pressed {
		s.pressed[source] = map[remoteinput.Code]bool{}
	}
	poster := s.poster
	s.mu.Unlock()
	if poster == nil {
		return nil
	}
	// Neutralize is best-effort: no simple-computer core means the runtime
	// rejects set_keyboard, and kit-lease cleanup still has to succeed.
	// This path is host cleanup, not a kit-local frame, so it keeps the
	// poster's own deadline instead of the local write bound.
	_ = poster(context.Background(), zx81keys.Neutral)
	return nil
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
