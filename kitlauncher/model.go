package kitlauncher

import (
	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/kitlauncher/controller"
	"github.com/DeanoC/FogCast/remoteinput"
	"time"
)

// Model is the kit/UI boundary. A renderer consumes it without owning network,
// framebuffer enablement, controller capture, or the target session lease.
type Model struct {
	Games                                             []tenfoot.Game
	Focus                                             int
	Session                                           Session
	Connected, TargetReady, Busy, ControllerConnected bool
	Message                                           string
	chord                                             controller.Chord
	axis                                              int
}

func (m *Model) ResetControls() { m.chord = controller.Chord{}; m.axis = 0 }
func (m *Model) Input(e remoteinput.Event, now time.Time) string {
	if sessionCanStop(m.Session.State) {
		if e.Kind == remoteinput.KindButton {
			m.chord.Update(e.Code, e.Action == remoteinput.ActionPress, now)
		}
		return ""
	}
	if m.Busy || !m.Connected || !m.TargetReady {
		return ""
	}
	move := 0
	if e.Kind == remoteinput.KindAxis && e.Code == remoteinput.AxisLeftY {
		dir := 0
		if e.Value > 8000 {
			dir = 1
		}
		if e.Value < -8000 {
			dir = -1
		}
		if dir != m.axis {
			move = dir
		}
		m.axis = dir
	}
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress {
		switch e.Code {
		case remoteinput.ButtonDPadUp:
			move = -1
		case remoteinput.ButtonDPadDown:
			move = 1
		case remoteinput.ButtonA:
			if len(m.Games) > 0 && m.Games[m.Focus].Launchable {
				return "launch"
			}
		}
	}
	if len(m.Games) > 0 {
		m.Focus = (m.Focus + move + len(m.Games)) % len(m.Games)
	}
	return ""
}
func (m *Model) Tick(now time.Time) string {
	if sessionCanStop(m.Session.State) && !m.Busy && m.chord.Ready(now) {
		return "stop"
	}
	return ""
}

// Failed sessions retain host-side cleanup state, so they use the same
// Select+Start recovery path as active sessions.
func sessionCanStop(state string) bool { return state == "active" || state == "failed" }
