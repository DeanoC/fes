package tenfoot

import (
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
)

func shelfQueryUnfiltered(query hostclient.GameListQuery) bool {
	return strings.TrimSpace(query.Platform) == "" && strings.TrimSpace(query.Q) == "" && strings.TrimSpace(query.Collection) == "" && strings.TrimSpace(query.Genre) == "" && strings.TrimSpace(query.Year) == "" && strings.TrimSpace(query.Region) == "" && !query.HidePrerelease && !query.HideHacks
}

// shelfFilterEqual compares the fields that choose which rows belong on
// the shelf. Cursor, limit, and sort are not part of that filter.
func shelfFilterEqual(a, b hostclient.GameListQuery) bool {
	return strings.TrimSpace(a.Platform) == strings.TrimSpace(b.Platform) &&
		strings.TrimSpace(a.Q) == strings.TrimSpace(b.Q) &&
		strings.TrimSpace(a.Collection) == strings.TrimSpace(b.Collection) &&
		strings.TrimSpace(a.Genre) == strings.TrimSpace(b.Genre) &&
		strings.TrimSpace(a.Year) == strings.TrimSpace(b.Year) &&
		strings.TrimSpace(a.Region) == strings.TrimSpace(b.Region) &&
		strings.TrimSpace(a.Availability) == strings.TrimSpace(b.Availability) &&
		a.HidePrerelease == b.HidePrerelease &&
		a.HideHacks == b.HideHacks
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
