package tenfoot

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestMissingVideoPartHasLibraryTag(t *testing.T) {
	game := hostclient.Game{State: "available", RootOnline: true, Launchable: true, VideoRequired: true}
	if got := libraryGameBlockTag(game); got != "Needs a video part" {
		t.Fatalf("video tag=%q", got)
	}
}
