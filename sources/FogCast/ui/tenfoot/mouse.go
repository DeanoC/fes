package tenfoot

import (
	"math"

	"github.com/DeanoC/FogCast/remoteinput"
)

type playMouse struct {
	x, y    float64
	buttons uint8
}

func (a *App) ForwardsPlayMouse() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.session.CoreMouse && a.forwardsPlayHIDLocked() && !a.playHIDInputBlockedLocked() && !a.playHIDFailClosedLocked()
}

// Releasing mouse holds does not require relative capture or launcher focus.
func (a *App) playMouseRouteReadyLocked() bool {
	ready := a.forwardsPlayHIDLocked()
	if a.playHIDInputBlockedLocked() {
		ready = a.playHIDRouteReadyLocked()
	}
	return a.session.CoreMouse && ready && !a.playHIDFailClosedLocked()
}
func (a *App) mouse(id int) *playMouse {
	if a.playMice == nil {
		a.playMice = map[int]*playMouse{}
	}
	if a.playMice[id] == nil {
		a.playMice[id] = &playMouse{}
	}
	return a.playMice[id]
}
func (a *App) mouseButtons() uint8 {
	var b uint8
	for _, m := range a.playMice {
		b |= m.buttons
	}
	return b
}
func (a *App) HandlePlayMouseMotion(id int, dx, dy float64) bool {
	if !a.ForwardsPlayMouse() {
		return false
	}
	if math.IsNaN(dx) || math.IsNaN(dy) || math.IsInf(dx, 0) || math.IsInf(dy, 0) {
		return true
	}
	a.mu.Lock()
	if a.playHIDInputBlockedLocked() {
		a.mu.Unlock()
		return false
	}
	m := a.mouse(id)
	m.x = math.Max(-32768, math.Min(32767, m.x+dx))
	m.y = math.Max(-32768, math.Min(32767, m.y+dy))
	x, y := math.Trunc(m.x), math.Trunc(m.y)
	// Keep fractional counts; a queued vector is bounded by the signed16 API.
	x = math.Max(-32768, math.Min(32767, x))
	y = math.Max(-32768, math.Min(32767, y))
	event := remoteinput.MouseEvent(int16(x), int16(y), a.mouseButtons())
	a.mu.Unlock()
	if x != 0 || y != 0 {
		if a.SendPlayHID(event) {
			a.mu.Lock()
			m.x -= x
			m.y -= y
			a.mu.Unlock()
		}
	}
	return true
}
func (a *App) HandlePlayMouseButton(id, button int, down bool) bool {
	a.mu.Lock()
	if !a.playMouseRouteReadyLocked() || a.playHIDInputBlockedLocked() && down {
		a.mu.Unlock()
		return false
	}
	if button != 1 && button != 3 {
		a.mu.Unlock()
		return true
	}
	m := a.mouse(id)
	mask := uint8(1)
	if button == 3 {
		mask = 2
	}
	if down {
		m.buttons |= mask
	} else {
		m.buttons &^= mask
	}
	event := remoteinput.MouseEvent(0, 0, a.mouseButtons())
	a.mu.Unlock()
	a.SendPlayHID(event)
	return true
}
func (a *App) ReleasePlayMouse(id int) {
	a.mu.Lock()
	delete(a.playMice, id)
	event := remoteinput.MouseEvent(0, 0, a.mouseButtons())
	a.mu.Unlock()
	a.SendPlayHID(event)
}

// HandlePlayMouseReport keeps one evdev SYN_REPORT vector and button snapshot atomic.
func (a *App) HandlePlayMouseReport(id int, dx, dy int16, buttons uint8) bool {
	a.mu.Lock()
	if !a.playMouseRouteReadyLocked() {
		a.mu.Unlock()
		return false
	}
	if a.playHIDInputBlockedLocked() {
		m := a.playMice[id]
		var released uint8
		if m != nil {
			released = m.buttons &^ buttons
			m.buttons &= buttons & 3
			m.x, m.y = 0, 0
		}
		if released == 0 && (dx != 0 || dy != 0 || buttons != 0) {
			a.mu.Unlock()
			return false
		}
		dx, dy = 0, 0
	} else {
		a.mouse(id).buttons = buttons & 3
	}
	event := remoteinput.MouseEvent(dx, dy, a.mouseButtons())
	a.mu.Unlock()
	a.SendPlayHID(event)
	return true
}

// ReleaseAllPlayMouse drops captured button holds and unsent fractional motion
// when the SDL window loses focus. A neutral snapshot contains no motion.
func (a *App) ReleaseAllPlayMouse() {
	a.mu.Lock()
	hadMouse := len(a.playMice) != 0
	a.playMice = nil
	a.mu.Unlock()
	if hadMouse {
		a.SendPlayHID(remoteinput.MouseEvent(0, 0, 0))
	}
}
