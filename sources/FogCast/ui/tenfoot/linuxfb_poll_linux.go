//go:build linux

package tenfoot

import (
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/controller"
	"golang.org/x/sys/unix"
)

// yieldedDirection is a still-held hat/stick direction at the time the
// fixture is moved. Button is before user remapping, while code is the
// remapped core key used for suppression and per-player tracking.
type yieldedDirection struct {
	button     Button
	code       remoteinput.Code
	suppressed bool
	forwarded  bool
}

// Snapshot while the old player's held and suppression records still exist.
// Menu-only analog axes use in.axes, not in.hatHeld; their existing menu hold
// is unaffected by a port reassignment and must not be pressed a second time.
func (in *nativeInput) yieldedDirections(app *App) []yieldedDirection {
	if app == nil || in.player == noLocalPlayer || len(in.hatHeld) == 0 {
		return nil
	}
	var result []yieldedDirection
	seen := map[Button]bool{}
	app.mu.Lock()
	defer app.mu.Unlock()
	for _, button := range in.hatHeld {
		if button == ButtonNone || seen[button] {
			continue
		}
		seen[button] = true
		e := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: LogicalFromButton(button)}
		if in.remap != nil {
			e = in.remap.Apply(e)
		}
		key := padKey{uint8(in.player), e.Code}
		_, sent := in.padHeld[key]
		_, tracked := app.playPadHeld[key]
		// Never synthesize a Select/Start press or a swallowed transition
		// event on the new port. Swallow its eventual release there too.
		suppressed := app.playPadSuppressed[key] || app.sessionDisplayBusy || app.sessionDisplayUncertain ||
			app.stopPhase == "stopping" || app.retryStopLock || app.playHIDFailClosedLocked() ||
			e.Code == remoteinput.ButtonSelect || e.Code == remoteinput.ButtonStart
		coreOwns := app.needsSessionDisplayLocked() && app.session.State == "active" && !app.roomDuringPlay
		result = append(result, yieldedDirection{button: button, code: e.Code, suppressed: suppressed, forwarded: sent && tracked && coreOwns})
	}
	return result
}

// Restore only directions the active machine was already seeing. The
// release/departure of the old player is sent before this is called. A
// portless fixture cannot send to a machine, and ports do not auto-compact.
func (in *nativeInput) restoreYieldedDirections(app *App, directions []yieldedDirection) {
	if app == nil || in.player == noLocalPlayer {
		return
	}
	for _, direction := range directions {
		if direction.suppressed {
			app.mu.Lock()
			if app.needsSessionDisplayLocked() && app.session.State == "active" {
				if app.playPadSuppressed == nil {
					app.playPadSuppressed = make(map[padKey]bool)
				}
				app.playPadSuppressed[padKey{uint8(in.player), direction.code}] = true
			}
			app.mu.Unlock()
			continue
		}
		if direction.forwarded {
			in.button(app, direction.button, 1, time.Now())
		}
	}
}

// yieldingPad is the pad that gives up its controller port to arriving, or
// nil. A pad that yields gives way to one that does not, when it holds a
// lower port than arriving would take or arriving would take none. The
// lowest such port goes first.
func (ins *nativeInputs) yieldingPad(arriving *nativeInput) *nativeInput {
	if arriving.kind != InputGamepad || arriving.yields {
		return nil
	}
	var yielder *nativeInput
	for _, in := range ins.devices {
		if !in.yields || in.player == noLocalPlayer {
			continue
		}
		if arriving.player != noLocalPlayer && in.player > arriving.player {
			continue
		}
		if yielder == nil || in.player < yielder.player {
			yielder = in
		}
	}
	return yielder
}

func (ins *nativeInputs) poll(app *App, now time.Time) (bool, error) {
	ins.maybeRescan(app, now)
	ins.syncProfiles(app)
	if ins.localKeyboardGate != nil {
		// Snapshot queries touch only fds already owned by this poll loop.
		// The App permit is false until a future reviewed activation grants it.
		ins.localKeyboardGate.snapshotUI(
			func(fd int) ([]byte, error) { return nativeKeyboardBitmap(fd, false, nativeKeyboardBitmapIOCTL) },
			func(fd int) ([]byte, error) { return nativeKeyboardBitmap(fd, true, nativeKeyboardBitmapIOCTL) },
			func(in *nativeInput, held []uint16) {
				reconcileNativeKeyboardUI(in, ins.devices, held, true, func(Command) {})
			},
			func(in *nativeInput) { reconcileNativeKeyboardUI(in, ins.devices, nil, false, func(Command) {}) },
		)
		_, _ = ins.localKeyboardGate.snapshot(
			func(fd int) ([]byte, error) { return nativeKeyboardBitmap(fd, false, nativeKeyboardBitmapIOCTL) },
			func(fd int) ([]byte, error) { return nativeKeyboardBitmap(fd, true, nativeKeyboardBitmapIOCTL) },
			func(in *nativeInput, held []uint16) {
				reconcileNativeKeyboardUI(in, ins.devices, held, true, func(Command) {})
			},
			func(in *nativeInput) { reconcileNativeKeyboardUI(in, ins.devices, nil, false, func(Command) {}) },
		)
	}
devices:
	for _, in := range append([]*nativeInput(nil), ins.devices...) {
		if in.info.GUID != "" {
			ins.syncLearner(app, in)
		}
		// Bound draining so a noisy device cannot starve rendering or cancellation.
		for batch := 0; batch < 8; batch++ {
			var b [nativeEventSize * 32]byte
			n, err := unix.Read(in.fd, b[:])
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				if ins.localKeyboardGate != nil && in.kind == InputKeyboard && in.localKeyboardID != (localhid.DeviceID{}) && len(in.pending) == 0 {
					_ = ins.localKeyboardGate.afterReadEAGAIN(in.localKeyboardID)
				}
				break
			}
			if err != nil {
				if ins.automatic {
					ins.drop(app, in)
					continue devices
				}
				return false, fmt.Errorf("%s input: %w", ins.label, err)
			}
			if n == 0 {
				if ins.automatic {
					ins.drop(app, in)
					continue devices
				}
				return false, fmt.Errorf("%s input: device closed", ins.label)
			}
			in.pending = append(in.pending, b[:n]...)
			for len(in.pending) >= nativeEventSize {
				typ, code, value := decodeNativeEvent(in.pending[:nativeEventSize])
				in.pending = in.pending[nativeEventSize:]
				if handled, quit, _ := ins.gatedKeyboardRecord(in, typ, code, value, now); handled {
					if quit {
						return true, nil
					}
					continue
				}
				if ins.localKeyboardGate != nil && in.kind == InputNone && typ == 1 && code < 256 {
					ins.localKeyboardGate.fault(nativeFaultCapability)
					continue
				}
				if typ == 0 && code == 3 {
					if ins.automatic {
						ins.drop(app, in)
						continue devices
					}
					return false, fmt.Errorf("%s input: evdev dropped events; restart to resynchronise", ins.label)
				}
				if in.info.GUID != "" {
					ins.syncLearner(app, in)
				}
				if in.learner != nil {
					if source, ok := in.learner.Feed(typ, code, value); ok {
						app.NoteInput(InputGamepad, in.fd)
						app.LearnPadSource(in.fd, source, now)
					}
					continue
				}
				if in.kind == InputMouse && in.mapper != nil {
					if event, ok := in.mapper.Map(typ, code, value); ok {
						x, y, buttons, _ := remoteinput.MouseVector(event)
						app.HandlePlayMouseReport(in.fd, x, y, buttons)
					}
					if in.mapper.MouseFault() {
						if ins.automatic {
							ins.drop(app, in)
							continue devices
						}
						return false, fmt.Errorf("%s input: mouse report exceeds signed16 range", ins.label)
					}
					continue
				}
				if typ == 1 {
					guide := code == 316
					if in.mapper != nil {
						guide = in.mapper.Guide(code)
					}
					if in.mapper != nil && !guide {
						if ins.padEvents(app, in, in.mapper.Events(typ, code, value), now) {
							return true, nil
						}
						continue
					}
					if guide {
						session := app.Snapshot().Session
						if value == 1 {
							if command := sessionGuideCommand(session); command != CmdNone {
								app.Press(command, now)
							} else if app.coreOwnsPadInput() {
								app.NoteInput(InputGamepad, in.fd)
								app.RequestRoomlessStop(now)
							} else {
								app.NoteInput(InputGamepad, in.fd)
								in.setButton(code, CmdSettings, true)
							}
						} else if app.coreOwnsPadInput() {
							if value != 0 {
								app.NoteInput(InputGamepad, in.fd)
							}
						} else if value == 0 {
							in.setButton(code, CmdSettings, false)
						}
					} else if button := nativeButton(code); button != ButtonNone {
						in.button(app, button, value, now)
					} else if code < 256 {
						if ins.key(app, in, code, value, now) {
							return true, nil
						}
					}
				} else if typ == 3 {
					if in.mapper != nil {
						if ins.padEvents(app, in, in.mapper.Events(typ, code, value), now) {
							return true, nil
						}
					} else if code == 16 || code == 17 {
						in.hat(app, code, value, now)
					}
				}
				// Preserve quick down/up pairs even when drained in one render frame.
				if (typ == 1 && code >= 256) || (typ == 3 && in.mapper == nil && (code == 16 || code == 17)) {
					if ins.applyButtons(app, now) {
						return true, nil
					}
				}
			}
		}
	}
	if app.coreOwnsPadInput() {
		if !ins.menuSuppressed {
			ins.dropMenuEdges(app)
			ins.menuSuppressed = true
		}
		return false, nil
	}
	if ins.menuSuppressed {
		ins.dropMenuEdges(app)
		ins.menuSuppressed = false
	}
	return ins.applyButtons(app, now), nil
}

// syncLearner starts reading a pad's raw controls once Settings ›
// Controllers maps it step by step and the pad has nothing held, so the
// press that started mapping comes up through the menus. When mapping ends
// it stops, and keys still down then come up unseen.
func (ins *nativeInputs) syncLearner(app *App, in *nativeInput) {
	learning := app.PadLearning(in.fd)
	switch {
	case learning && in.learner == nil:
		if len(in.padHeld) > 0 || len(in.controls) > 0 || len(in.hatHeld) > 0 || len(in.axes) > 0 {
			return
		}
		var rest map[uint16]int32
		if ins.kindOf == nil {
			rest = nativeAxisRest(in.fd, in.device)
		}
		in.learner = controller.NewLearner(in.device, rest)
	case !learning && in.learner != nil:
		if in.mapper != nil {
			for _, code := range in.learner.HeldKeys() {
				in.mapper.Suppress(code)
			}
		}
		in.learner = nil
	}
}

func sessionGuideCommand(session SessionSnapshot) Command {
	if !session.HardwareRoom {
		return CmdNone
	}
	if session.Coleco && !session.ControlsOpen {
		return CmdSearch
	}
	return CmdHome
}

func (ins *nativeInputs) dropMenuEdges(app *App) {
	if ins == nil {
		return
	}
	for _, in := range ins.devices {
		if in == nil {
			continue
		}
		in.controls = nil
		in.axes = nil
		in.menuSelectDown = false
		in.menuStartDown = false
		in.menuChordFired = false
	}
	for cmd := range ins.held {
		app.Release(cmd)
		delete(ins.held, cmd)
	}
}

func nativeButton(code uint16) Button {
	switch code {
	case 304:
		return ButtonSouth
	case 305:
		return ButtonEast
	case 307:
		return ButtonNorth
	case 308:
		return ButtonWest
	case 310:
		return ButtonLeftShoulder
	case 311:
		return ButtonRightShoulder
	case 314:
		return ButtonBack
	case 315:
		return ButtonStart
	case 544:
		return ButtonDPadUp
	case 545:
		return ButtonDPadDown
	case 546:
		return ButtonDPadLeft
	case 547:
		return ButtonDPadRight
	}
	return ButtonNone
}

func (in *nativeInput) setButton(control uint16, cmd Command, down bool) {
	if in.controls == nil {
		in.controls = map[uint16]Command{}
	}
	if down {
		in.controls[control] = cmd
	} else {
		delete(in.controls, control)
	}
}

func (ins *nativeInputs) applyButtons(app *App, now time.Time) bool {
	if ins.held == nil {
		ins.held = map[Command]bool{}
	}
	pressed := map[Command]bool{}
	for _, in := range ins.devices {
		for _, cmd := range in.controls {
			pressed[cmd] = true
		}
	}
	return applyPressed(app, pressed, ins.held, now)
}

// padEvents takes what a pad's mapping reads from one evdev event: the
// stick or hat it moves and the buttons it presses or releases, including
// buttons a mapping puts on an axis or a hat direction.
func (ins *nativeInputs) padEvents(app *App, in *nativeInput, events []remoteinput.Event, now time.Time) bool {
	for _, e := range events {
		switch e.Kind {
		case remoteinput.KindButton:
			value := int32(0)
			if e.Action == remoteinput.ActionPress {
				value = 1
			}
			in.button(app, ButtonFromLogical(e.Code), value, now)
		case remoteinput.KindAxis:
			in.axis(app, e.Code, e.Value, now)
		}
	}
	return len(events) > 0 && ins.applyButtons(app, now)
}

// menuSettingsChordControl tracks an active Select+Start Settings chord in
// in.controls separately from single physical evdev button codes.
const menuSettingsChordControl uint16 = 0xffff

// handleMenuSettingsChord turns Select+Start into CmdSettings in menus when
// enabled on the pad (default on pads without a Guide button). Returns true when
// the event has been handled or suppressed by the chord state machine.
func (in *nativeInput) handleMenuSettingsChord(app *App, e remoteinput.Event, down bool) bool {
	if !in.settingsCombo || e.Kind != remoteinput.KindButton {
		return false
	}
	switch e.Code {
	case remoteinput.ButtonSelect:
		if down && app.localPadCommand(e) == CmdSelect {
			return false
		}
		in.menuSelectDown = down
	case remoteinput.ButtonStart:
		in.menuStartDown = down
	default:
		return false
	}
	if in.menuSelectDown && in.menuStartDown {
		for k, cmd := range in.controls {
			if cmd == CmdLayoutCycle || cmd == CmdQuit {
				delete(in.controls, k)
			}
		}
		in.menuChordFired = true
		in.setButton(menuSettingsChordControl, CmdSettings, true)
		app.NoteInput(InputGamepad, in.fd)
		return true
	}
	if in.menuChordFired {
		in.setButton(menuSettingsChordControl, CmdSettings, false)
		if !in.menuSelectDown && !in.menuStartDown {
			in.menuChordFired = false
		}
		return true
	}
	return e.Code == remoteinput.ButtonStart
}

func (in *nativeInput) button(app *App, button Button, value int32, now time.Time) {
	if value == 2 || button == ButtonNone {
		return
	}
	app.NotePadButton(in.fd, LogicalFromButton(button), value != 0)
	e := remoteinput.Event{
		Device: remoteinput.DeviceGamepad,
		Kind:   remoteinput.KindButton,
		Code:   LogicalFromButton(button),
		Action: remoteinput.ActionRelease,
	}
	if value != 0 {
		e.Action = remoteinput.ActionPress
	}
	if in.remap != nil {
		e = in.remap.Apply(e)
	}
	if in.player == noLocalPlayer {
		// Both controller ports belong to other pads. This pad never reaches
		// a machine. It presses menu controls only where a pad with a port
		// would, and its releases always clear them. Keypad Select follows the
		// focused launcher's confirmation mapping too.
		if in.handleMenuSettingsChord(app, e, value != 0) {
			return
		}
		if value == 0 || app.portlessPadReachesMenu(e.Code) {
			in.setButton(uint16(button), app.localPadCommand(e), value != 0)
		}
		if value != 0 {
			app.NoteInput(InputGamepad, in.fd)
		}
		return
	}
	e.Player = uint8(in.player)
	key := padKey{e.Player, e.Code}
	if value != 0 {
		shared := in.holding(key)
		if in.padSent == nil {
			in.padSent = make(map[Button]padKey)
		}
		in.padSent[button] = key
		if shared {
			return
		}
	} else if sent, ok := in.padSent[button]; ok {
		// Release what this button pressed, even if the profile changed.
		key = sent
		e.Code = sent.code
		delete(in.padSent, button)
		if in.holding(key) {
			return
		}
	}
	if in.padHeld == nil {
		in.padHeld = make(map[padKey]remoteinput.Event)
	}
	if value != 0 {
		in.padHeld[key] = e
	} else {
		delete(in.padHeld, key)
	}
	if app.HandleLocalPad(e, now) {
		if value != 0 {
			app.NoteInput(InputGamepad, in.fd)
		}
		return
	}
	if in.handleMenuSettingsChord(app, e, value != 0) {
		return
	}
	in.setButton(uint16(button), app.localPadCommand(e), value != 0)
	if value != 0 {
		app.NoteInput(InputGamepad, in.fd)
	}
}

// holding reports whether a held button already holds key on the port.
func (in *nativeInput) holding(key padKey) bool {
	for _, sent := range in.padSent {
		if sent == key {
			return true
		}
	}
	return false
}

func (in *nativeInput) localHat(app *App, code uint16, value int32, now time.Time) {
	button := ButtonNone
	if code == 16 {
		if value < 0 {
			button = ButtonDPadLeft
		} else if value > 0 {
			button = ButtonDPadRight
		}
	} else if value < 0 {
		button = ButtonDPadUp
	} else if value > 0 {
		button = ButtonDPadDown
	}
	if in.hatHeld == nil {
		in.hatHeld = map[uint16]Button{}
	}
	prev := in.hatHeld[code]
	if prev == button {
		return
	}
	if prev != ButtonNone {
		in.button(app, prev, 0, now)
	}
	if button == ButtonNone {
		delete(in.hatHeld, code)
		return
	}
	in.hatHeld[code] = button
	in.button(app, button, 1, now)
}

func (in *nativeInput) localAxis(app *App, code remoteinput.Code, value int32, now time.Time) {
	axisCode := uint16(16)
	if code == remoteinput.AxisLeftY {
		axisCode = 17
	}
	direction := int32(0)
	if value < 0 {
		direction = -1
	} else if value > 0 {
		direction = 1
	}
	in.localHat(app, axisCode, direction, now)
}

func (in *nativeInput) hat(app *App, code uint16, value int32, now time.Time) {
	in.localHat(app, code, value, now)
}

func (in *nativeInput) axis(app *App, code remoteinput.Code, value int32, now time.Time) {
	if app.coreOwnsPadInput() {
		in.localAxis(app, code, value, now)
		return
	}
	if in.axes == nil {
		in.axes = map[uint16]Command{}
	}
	axisX, axisY := int(value), 0
	if code == remoteinput.AxisLeftY {
		axisX, axisY = 0, int(value)
	}
	old := in.axes[uint16(code)]
	cmd := CommandFromStickHeld(axisX, axisY, old)
	if cmd != old {
		in.button(app, nativeDirectionButton(old), 0, now)
		if cmd == CmdNone {
			// A stick at rest holds nothing.
			delete(in.axes, uint16(code))
		} else {
			in.axes[uint16(code)] = cmd
		}
		in.button(app, nativeDirectionButton(cmd), 1, now)
	}
}

func nativeDirectionButton(cmd Command) Button {
	switch cmd {
	case CmdUp:
		return ButtonDPadUp
	case CmdDown:
		return ButtonDPadDown
	case CmdLeft:
		return ButtonDPadLeft
	case CmdRight:
		return ButtonDPadRight
	default:
		return ButtonNone
	}
}
