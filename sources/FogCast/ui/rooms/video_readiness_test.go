package rooms

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestMissingVideoPartExplainsBlockedPlay(t *testing.T) {
	game := hostclient.Game{ID: "fpga-coleco", State: "available", RootOnline: true, Launchable: true, VideoRequired: true}
	if game.LaunchEligible() || LaunchBlockCopy(game) != "Needs a matching video part" {
		t.Fatalf("video block=%q copy=%q", game.LaunchBlock(), LaunchBlockCopy(game))
	}
}
