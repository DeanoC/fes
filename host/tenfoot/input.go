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
)

const (
	repeatDelay = 280 * time.Millisecond
	repeatEvery = 90 * time.Millisecond
	stickGate   = 16000
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
	default:
		return CmdNone
	}
}

// CommandFromStick maps a left-stick axis sample to a d-pad command.
func CommandFromStick(axisX, axisY int) Command {
	ax, ay := axisX, axisY
	if ax < 0 {
		ax = -ax
	}
	if ay < 0 {
		ay = -ay
	}
	if ax < stickGate && ay < stickGate {
		return CmdNone
	}
	if ax >= ay {
		if axisX < 0 {
			return CmdLeft
		}
		return CmdRight
	}
	if axisY < 0 {
		return CmdUp
	}
	return CmdDown
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
	} else {
		r.held = CmdNone
	}
	return cmd
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
	default:
		return "none"
	}
}
