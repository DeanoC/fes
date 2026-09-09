package kitlauncher

import (
	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
)

// TitleBadges is the honest chip set for a catalog row plus optional presentation.
// Players, rating, and completion come from presentation when those fields exist.
// Portable uses presentation.portable or the catalog system identity.
func TitleBadges(game tenfoot.Game, pres tenfoot.Presentation) []fbgrid.Badge {
	players, rating, completion := "", "", ""
	portable := tenfoot.PortableSystem(game.System)
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
