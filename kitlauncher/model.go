package kitlauncher

import (
	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/kitlauncher/controller"
	"github.com/DeanoC/FogCast/remoteinput"
	"time"
)

const axisDeadzone int32 = 8000

// Model is the kit/UI boundary. A renderer consumes it without owning network,
// framebuffer enablement, controller capture, or the target session lease.
type Model struct {
	Catalog                                           []tenfoot.Game
	Games                                             []tenfoot.Game
	Shelves                                           []string
	Shelf                                             string
	Focus                                             int
	Session                                           Session
	Connected, TargetReady, Busy, ControllerConnected bool
	Message                                           string
	chord                                             controller.Chord
	axisX, axisY                                      int
}

func (m *Model) ResetControls() { m.chord = controller.Chord{}; m.axisX = 0; m.axisY = 0 }
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
	dx, dy := 0, 0
	if e.Kind == remoteinput.KindAxis {
		switch e.Code {
		case remoteinput.AxisLeftX:
			dx = m.axisStep(&m.axisX, e.Value)
		case remoteinput.AxisLeftY:
			dy = m.axisStep(&m.axisY, e.Value)
		}
	}
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress {
		switch e.Code {
		case remoteinput.ButtonDPadUp:
			dy = -1
		case remoteinput.ButtonDPadDown:
			dy = 1
		case remoteinput.ButtonDPadLeft:
			dx = -1
		case remoteinput.ButtonDPadRight:
			dx = 1
		case remoteinput.ButtonL:
			m.CycleShelf(-1)
		case remoteinput.ButtonR, remoteinput.ButtonSelect:
			m.CycleShelf(1)
		case remoteinput.ButtonA:
			if len(m.Games) > 0 && m.Focus >= 0 && m.Focus < len(m.Games) && m.Games[m.Focus].Launchable {
				return "launch"
			}
		}
	}
	if len(m.Games) > 0 && (dx != 0 || dy != 0) {
		m.Focus = fbgrid.MoveFocus(m.Focus, len(m.Games), fbgrid.DefaultColumns, dx, dy)
	}
	return ""
}

func (m *Model) axisStep(hold *int, value int32) int {
	dir := 0
	if value > axisDeadzone {
		dir = 1
	}
	if value < -axisDeadzone {
		dir = -1
	}
	move := 0
	if dir != *hold {
		move = dir
	}
	*hold = dir
	return move
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
