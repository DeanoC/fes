package kitlauncher

import "github.com/DeanoC/FogCast/ui/tenfoot"

// AtmosphereHandle prefers presentation fanart/backdrop, then an attract
// backdrop for the focused (or wheel representative) title. Empty means the
// painter may use a cover-wall of decoded covers, or no atmosphere.
func (m Model) AtmosphereHandle(pres tenfoot.Presentation) string {
	if handle := tenfoot.BackdropHandle(pres); handle != "" {
		return handle
	}
	game, ok := m.atmosphereGame()
	if !ok {
		return ""
	}
	for _, item := range m.attractItems {
		if item.GameID != game.ID {
			continue
		}
		if handle := item.BackdropHandle(); handle != "" {
			return handle
		}
	}
	return ""
}

func (m Model) atmosphereGame() (tenfoot.Game, bool) {
	if game, ok := m.FocusedGame(); ok {
		return game, true
	}
	if m.WheelOpen {
		return m.WheelGame(m.activeShelf())
	}
	return tenfoot.Game{}, false
}
