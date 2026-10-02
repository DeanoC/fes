package tenfoot

import (
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
)

func shelfQueryUnfiltered(query hostclient.GameListQuery) bool {
	return strings.TrimSpace(query.Platform) == "" && strings.TrimSpace(query.Q) == "" && strings.TrimSpace(query.Collection) == "" && strings.TrimSpace(query.Genre) == "" && strings.TrimSpace(query.Year) == "" && strings.TrimSpace(query.Region) == "" && !query.HidePrerelease && !query.HideHacks
}

// librarySavedOffline reports a shelf whose saved rows are not on this machine.
// A title that is present but not playable here is not offline.
func librarySavedOffline(games []hostclient.Game) bool {
	if len(games) == 0 {
		return false
	}
	for _, game := range games {
		if game.RootOnline && game.State == "available" {
			return false
		}
	}
	return true
}
