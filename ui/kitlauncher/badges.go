package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/fbgrid"
	"github.com/DeanoC/FogCast/ui/shared"
)

// TitleBadges is the honest chip set for a catalog row plus optional presentation.
// Players, rating, and completion come from presentation when those fields exist.
// Portable uses presentation.portable or the catalog system identity.
func TitleBadges(game hostclient.Game, pres hostclient.Presentation) []fbgrid.Badge {
	players, rating, completion := "", "", ""
	portable := shared.PortableSystem(game.System)
	if pres.Presentation != nil {
		info := pres.Presentation
		players = info.Players
		rating = info.Rating
		completion = info.Completion
		if info.Portable {
			portable = true
		}
	}
	return fbgrid.ComposeBadges(players, rating, completion, portable)
}
