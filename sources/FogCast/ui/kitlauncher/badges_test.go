package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/fbgrid"
	"github.com/DeanoC/FogCast/ui/shared"
	"testing"
)

func TestTitleBadgesHideEmptyAndUsePresentation(t *testing.T) {
	game := hostclient.Game{ID: "sonic", Title: "Sonic", System: "megadrive"}
	if got := TitleBadges(game, hostclient.Presentation{}); len(got) != 0 {
		t.Fatalf("empty %+v", got)
	}
	pres := hostclient.Presentation{Presentation: &hostclient.PresentationInfo{
		Players: "1-2", Rating: "4.5", Completion: "100%", Portable: true,
	}}
	got := TitleBadges(game, pres)
	if len(got) != 4 || got[0].Kind != fbgrid.BadgePlayers || got[0].Label != "1-2" || got[3].Label != "PORT" {
		t.Fatalf("full %+v", got)
	}
	players := TitleBadges(game, hostclient.Presentation{Presentation: &hostclient.PresentationInfo{Players: "2"}})
	if len(players) != 1 || players[0].Label != "2P" {
		t.Fatalf("players %+v", players)
	}
}

func TestTitleBadgesPortableFromHandheldSystem(t *testing.T) {
	got := TitleBadges(hostclient.Game{System: "gb"}, hostclient.Presentation{})
	if len(got) != 1 || got[0].Kind != fbgrid.BadgePortable || got[0].Label != "PORT" {
		t.Fatalf("gb %+v", got)
	}
	if got := TitleBadges(hostclient.Game{System: "snes"}, hostclient.Presentation{}); len(got) != 0 {
		t.Fatalf("snes %+v", got)
	}
}

func TestPortableSystemMatchesHandheldIDs(t *testing.T) {
	if !shared.PortableSystem("gba") || shared.PortableSystem("megadrive") || shared.PortableSystem("") {
		t.Fatal("portable system")
	}
}
