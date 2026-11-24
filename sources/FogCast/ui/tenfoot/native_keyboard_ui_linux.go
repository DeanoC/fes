//go:build linux

package tenfoot

// nativeKeyboardUIState is local to one evdev open incarnation. Unknown
// states require a real, post-drain key-up before each code can be armed.
type nativeKeyboardUIState struct {
	unknown    bool
	armed      [256]bool
	suppressed [256]bool
	admitted   [256]bool
}

// reconcileNativeKeyboardUI is bounded, infallible local bookkeeping. The
// callback cancels a UI command without executing its release action and
// must not post guest input. An unknown snapshot is never treated as all-up.
// Commands still contributed by another keyboard must not be cancelled.
func reconcileNativeKeyboardUI(source *nativeInput, devices []*nativeInput, held []uint16, known bool, cancel func(Command)) {
	if source == nil || cancel == nil {
		return
	}
	for _, code := range held {
		if code == 0 || code >= 256 {
			known = false
			break
		}
	}
	oldCommands := source.commands
	source.commands = nil
	cancelled := make(map[Command]bool)
	for _, cmd := range oldCommands {
		if cmd == CmdNone || cancelled[cmd] {
			continue
		}
		otherHeld := false
		for _, other := range devices {
			if other == nil || other == source || other.kind != InputKeyboard {
				continue
			}
			for _, otherCmd := range other.commands {
				if otherCmd == cmd {
					otherHeld = true
					break
				}
			}
			if otherHeld {
				break
			}
		}
		if !otherHeld {
			cancel(cmd)
			cancelled[cmd] = true
		}
	}
	source.shifts = nil
	source.keysHeld = nil
	state := &nativeKeyboardUIState{unknown: !known}
	if known {
		for _, code := range held {
			state.suppressed[code] = true
			if source.keysHeld == nil {
				source.keysHeld = make(map[uint16]string)
			}
			source.keysHeld[code] = nativeKeyName(code)
		}
	}
	source.localKeyboardUI = state
}

type nativeKeyboardUIEdgeResult struct {
	dispatch bool // permitted to run the App's UI-only dispatcher
	cleanup  bool // retire a previously admitted native UI command, no click
}

// nativeKeyboardUIEdge applies release-to-arm independently of the guest
// disposition. uiAllowed is the App's UI-only decision; this helper never
// sends input or performs an action. Call it for every raw edge, including
// key-ups that the App otherwise consumes. The caller performs cleanup even
// when dispatch is false.
func nativeKeyboardUIEdge(source *nativeInput, code uint16, value int32, uiAllowed bool) nativeKeyboardUIEdgeResult {
	if source == nil || source.localKeyboardUI == nil || code == 0 || code >= 256 {
		return nativeKeyboardUIEdgeResult{}
	}
	s := source.localKeyboardUI
	switch value {
	case 0:
		wasAdmitted := s.admitted[code]
		s.admitted[code] = false
		s.suppressed[code] = false
		s.armed[code] = true
		if !wasAdmitted {
			delete(source.keysHeld, code)
			delete(source.shifts, code)
		}
		return nativeKeyboardUIEdgeResult{dispatch: uiAllowed && wasAdmitted, cleanup: wasAdmitted}
	case 1:
		if s.admitted[code] {
			return nativeKeyboardUIEdgeResult{}
		}
		if s.suppressed[code] || (s.unknown && !s.armed[code]) || !uiAllowed {
			s.suppressed[code] = true
			return nativeKeyboardUIEdgeResult{}
		}
		s.admitted[code] = true
		return nativeKeyboardUIEdgeResult{dispatch: true}
	case 2:
		return nativeKeyboardUIEdgeResult{dispatch: uiAllowed && s.admitted[code] && !s.suppressed[code]}
	default:
		return nativeKeyboardUIEdgeResult{}
	}
}
