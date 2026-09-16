package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/shared"
)

// AtmosphereHandle prefers presentation fanart/backdrop, then an attract
// backdrop for the focused (or wheel representative) title. Empty means the
// painter may use a cover-wall of decoded covers, or no atmosphere.
func (m Model) AtmosphereHandle(pres hostclient.Presentation) string {
	if handle := shared.BackdropHandle(pres); handle != "" {
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

func (m Model) atmosphereGame() (hostclient.Game, bool) {
	if game, ok := m.FocusedGame(); ok {
		return game, true
	}
	if m.WheelOpen {
		return m.WheelGame(m.activeShelf())
	}
	return hostclient.Game{}, false
}
