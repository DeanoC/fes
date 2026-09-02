package tenfoot

import "time"

// Command is one focus-graph action from a gamepad or debug keyboard.
type Command int

const (
	CmdNone Command = iota
	CmdUp
	CmdDown
	CmdLeft
	CmdRight
	CmdSelect
	CmdBack
	CmdQuit
	CmdFilterPrev
	CmdFilterNext
	CmdSortCycle
	CmdSearch
)

// Button is a gamepad-first control, independent of SDL.
type Button int

const (
	ButtonNone Button = iota
	ButtonDPadUp
	ButtonDPadDown
	ButtonDPadLeft
	ButtonDPadRight
	ButtonSouth
	ButtonEast
	ButtonStart
	ButtonBack
	ButtonWest
	ButtonNorth
	ButtonLeftShoulder
	ButtonRightShoulder
)

const (
	repeatDelay     = 280 * time.Millisecond
	repeatEvery     = 90 * time.Millisecond
	stickGate       = 16000
	stickHysteresis = 8000
)

// CommandFromButton maps a gamepad button to a focus command.
func CommandFromButton(button Button) Command {
	switch button {
	case ButtonDPadUp:
		return CmdUp
	case ButtonDPadDown:
		return CmdDown
	case ButtonDPadLeft:
		return CmdLeft
	case ButtonDPadRight:
		return CmdRight
	case ButtonSouth:
		return CmdSelect
	case ButtonEast, ButtonBack:
		return CmdBack
	case ButtonStart:
		return CmdQuit
	case ButtonWest:
		return CmdSortCycle
	case ButtonNorth:
		return CmdSearch
	case ButtonLeftShoulder:
		return CmdFilterPrev
	case ButtonRightShoulder:
		return CmdFilterNext
	default:
		return CmdNone
	}
}

// CommandFromKey maps debug keyboard keys. Names are SDL-style identifiers.
func CommandFromKey(name string) Command {
	switch name {
	case "up", "w":
		return CmdUp
	case "down", "s":
		return CmdDown
	case "left", "a":
		return CmdLeft
	case "right", "d":
		return CmdRight
	case "return", "space":
		return CmdSelect
	case "escape", "backspace":
		return CmdBack
	case "q":
		return CmdQuit
	case "leftbracket", "[":
		return CmdFilterPrev
	case "rightbracket", "]":
		return CmdFilterNext
	case "x":
		return CmdSortCycle
	case "/", "slash", "f":
		return CmdSearch
	default:
		return CmdNone
	}
}

// CommandFromStick maps a left-stick axis sample to a d-pad command.
func CommandFromStick(axisX, axisY int) Command {
	return CommandFromStickHeld(axisX, axisY, CmdNone)
}

// CommandFromStickHeld maps a left-stick sample, keeping the previous
// direction until the stick recenters or the other axis wins by stickHysteresis.
func CommandFromStickHeld(axisX, axisY int, held Command) Command {
	ax, ay := absAxis(axisX), absAxis(axisY)
	if ax < stickGate && ay < stickGate {
		return CmdNone
	}
	horizontal := ax >= ay
	switch held {
	case CmdLeft, CmdRight:
		if ay >= ax+stickHysteresis && ay >= stickGate {
			horizontal = false
		} else {
			horizontal = true
		}
	case CmdUp, CmdDown:
		if ax >= ay+stickHysteresis && ax >= stickGate {
			horizontal = true
		} else {
			horizontal = false
		}
	}
	if horizontal {
		if ax < stickGate {
			// Stay on the latched axis until recenter or a hysteresis switch.
			// Dropping the latch here made medium diagonals flip every frame.
			if held == CmdLeft || held == CmdRight {
				return held
			}
			return verticalStick(axisY)
		}
		if axisX < 0 {
			return CmdLeft
		}
		return CmdRight
	}
	if ay < stickGate {
		if held == CmdUp || held == CmdDown {
			return held
		}
		if axisX < 0 {
			return CmdLeft
		}
		return CmdRight
	}
	return verticalStick(axisY)
}

func verticalStick(axisY int) Command {
	if axisY < 0 {
		return CmdUp
	}
	return CmdDown
}

func absAxis(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Repeater emits held-direction repeats without blocking the frame loop.
type Repeater struct {
	held      Command
	heldSince time.Time
	lastFire  time.Time
}

// Down records a newly pressed command. Repeat only applies to movement.
func (r *Repeater) Down(cmd Command, now time.Time) Command {
	if cmd == CmdNone {
		return CmdNone
	}
	if isHoldable(cmd) {
		r.held = cmd
		r.heldSince = now
		r.lastFire = now
	}
	return cmd
}

// Arm starts hold-repeat for cmd without emitting a focus move.
func (r *Repeater) Arm(cmd Command, now time.Time) {
	if !isHoldable(cmd) {
		return
	}
	r.held = cmd
	r.heldSince = now
	r.lastFire = now
}

// Up clears a held movement command.
func (r *Repeater) Up(cmd Command) {
	if r.held == cmd {
		r.held = CmdNone
	}
}

// Tick returns a movement command when the hold repeat interval elapses.
func (r *Repeater) Tick(now time.Time) Command {
	if !isHoldable(r.held) {
		return CmdNone
	}
	if now.Sub(r.heldSince) < repeatDelay {
		return CmdNone
	}
	if now.Sub(r.lastFire) < repeatEvery {
		return CmdNone
	}
	r.lastFire = now
	return r.held
}

func isHoldable(cmd Command) bool {
	switch cmd {
	case CmdUp, CmdDown, CmdLeft, CmdRight:
		return true
	default:
		return false
	}
}

// applyPressed updates hold/repeat from this frame's pressed commands.
// After a dual-direction hold/release, a still-held direction is re-armed so
// navigation repeat continues (Repeater tracks only one command). Re-arm does
// not call Press: that would walk focus and restart repeat after South/East.
func applyPressed(app *App, pressed, held map[Command]bool, now time.Time) bool {
	for cmd := range held {
		if !pressed[cmd] {
			app.Release(cmd)
			delete(held, cmd)
		}
	}
	for cmd := range pressed {
		if cmd == CmdNone || held[cmd] {
			continue
		}
		if cmd == CmdQuit {
			return true
		}
		app.Press(cmd, now)
		held[cmd] = true
	}
	if !pressed[app.repeat.held] {
		for cmd := range held {
			if !isHoldable(cmd) {
				continue
			}
			app.repeat.Arm(cmd, now)
			break
		}
	}
	return false
}

func (c Command) String() string {
	switch c {
	case CmdUp:
		return "up"
	case CmdDown:
		return "down"
	case CmdLeft:
		return "left"
	case CmdRight:
		return "right"
	case CmdSelect:
		return "select"
	case CmdBack:
		return "back"
	case CmdQuit:
		return "quit"
	case CmdFilterPrev:
		return "filter-prev"
	case CmdFilterNext:
		return "filter-next"
	case CmdSortCycle:
		return "sort"
	case CmdSearch:
		return "search"
	default:
		return "none"
	}
}
