package tenfoot

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/corenames"
)

func displayGameTitle(game hostclient.Game) string {
	if game.System == "fpga" {
		return corenames.EntryTitle(game.CoreID, game.Title)
	}
	return game.Title
}
