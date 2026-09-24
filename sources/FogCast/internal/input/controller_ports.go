package input

import (
	"errors"
	"sync"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// ControllerBinding is observed from the runtime while the input lifecycle
// excludes core replacement. Every write retains this exact identity.
type ControllerBinding struct {
	PackageID  string
	Generation uint64
	Keypad     bool
}

type ControllerPoster func(string, uint64, uint8, uint8, uint16) error

type controllerPortsSink struct {
	mu       sync.Mutex
	fallback bridge.Sink
	poster   ControllerPoster
	binding  *ControllerBinding
	state    remoteinput.State
	dirty    [2]bool
}

func (s *controllerPortsSink) bind(binding *ControllerBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirty[0] || s.dirty[1] {
		return errors.New("controller state needs release")
	}
	if binding != nil {
		if binding.PackageID == "" || binding.Generation == 0 || s.poster == nil {
			return errors.New("invalid controller binding")
		}
		copy := *binding
		binding = &copy
	}
	s.binding = binding
	s.state.ReleaseAll()
	return nil
}

func (s *controllerPortsSink) Apply(f protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding == nil {
		return s.fallback.Apply(f)
	}
	if f.Player > 1 || f.Device != uint8(remoteinput.DeviceGamepad) {
		return bridge.RejectInput("unsupported controller event")
	}
	e := remoteinput.Event{Player: f.Player, Device: remoteinput.DeviceGamepad, Kind: remoteinput.Kind(f.Kind), Action: remoteinput.Action(f.Action), Code: remoteinput.Code(f.Code), Value: f.Value}
	if !validControllerEvent(e, s.binding.Keypad) {
		return bridge.RejectInput("unsupported controller control")
	}
	if err := s.state.Apply(e); err != nil {
		return err
	}
	buttons, keypad := controllerSnapshot(s.state.SnapshotForPlayer(f.Player), s.binding.Keypad)
	// A failed local request can have applied: release must still attempt zero.
	s.dirty[f.Player] = true
	if err := s.poster(s.binding.PackageID, s.binding.Generation, f.Player, buttons, keypad); err != nil {
		return err
	}
	s.dirty[f.Player] = buttons != 0 || keypad != 0
	return nil
}

func validControllerEvent(e remoteinput.Event, keypad bool) bool {
	if e.Kind == remoteinput.KindAxis {
		return e.Action == remoteinput.ActionAbsolute && (e.Code == remoteinput.AxisLeftX || e.Code == remoteinput.AxisLeftY) && e.Value >= -32768 && e.Value <= 32767
	}
	return e.Kind == remoteinput.KindButton && (e.Action == remoteinput.ActionPress || e.Action == remoteinput.ActionRelease) && e.Value == 0 &&
		((e.Code >= remoteinput.ButtonDPadUp && e.Code <= remoteinput.ButtonSelect) || (keypad && e.Code >= remoteinput.Keypad0 && e.Code <= remoteinput.KeypadHash))
}

// Keypad-port cores (Coleco) have no Start or Select on the original
// controller, and a modern pad has no keypad. On those cores Start also
// presses keypad 1 (the usual one-player game select) and Select also presses
// keypad * (the usual replay key). The Start/Select bitmap bits stay set.
const (
	keypadAliasStart  = uint16(1) << 1 // keypad 1
	keypadAliasSelect = uint16(1) << (remoteinput.KeypadStar - remoteinput.Keypad0)
)

func controllerSnapshot(snapshot remoteinput.Snapshot, keypadPorts bool) (buttons uint8, keypad uint16) {
	// Shared bitmap is Up,Down,Left,Right,A,B,Select,Start.
	for _, code := range snapshot.Pressed {
		if code >= remoteinput.ButtonDPadUp && code <= remoteinput.ButtonB {
			buttons |= 1 << (code - remoteinput.ButtonDPadUp)
		}
		if code == remoteinput.ButtonSelect {
			buttons |= 1 << 6
			if keypadPorts {
				keypad |= keypadAliasSelect
			}
		}
		if code == remoteinput.ButtonStart {
			buttons |= 1 << 7
			if keypadPorts {
				keypad |= keypadAliasStart
			}
		}
		if code >= remoteinput.Keypad0 && code <= remoteinput.KeypadHash {
			keypad |= 1 << (code - remoteinput.Keypad0)
		}
	}
	if x := snapshot.Axes[remoteinput.AxisLeftX]; x < -8000 {
		buttons |= 1 << 2
	} else if x > 8000 {
		buttons |= 1 << 3
	}
	if y := snapshot.Axes[remoteinput.AxisLeftY]; y < -8000 {
		buttons |= 1
	} else if y > 8000 {
		buttons |= 1 << 1
	}
	return
}

func (s *controllerPortsSink) ReleaseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding == nil {
		return s.fallback.ReleaseAll()
	}
	var result error
	for port := uint8(0); port < 2; port++ {
		if !s.dirty[port] {
			continue
		}
		if err := s.poster(s.binding.PackageID, s.binding.Generation, port, 0, 0); err != nil {
			result = errors.Join(result, err)
		} else {
			s.dirty[port] = false
		}
	}
	if result == nil {
		s.state.ReleaseAll()
	}
	return result
}

func (s *controllerPortsSink) Close() error {
	return errors.Join(s.ReleaseAll(), s.fallback.Close())
}
