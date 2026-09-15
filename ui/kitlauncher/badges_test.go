package kitlauncher

import (
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/FogCast/ui/tenfoot/fbgrid"
)

func TestTitleBadgesHideEmptyAndUsePresentation(t *testing.T) {
	game := tenfoot.Game{ID: "sonic", Title: "Sonic", System: "megadrive"}
	if got := TitleBadges(game, tenfoot.Presentation{}); len(got) != 0 {
		t.Fatalf("empty %+v", got)
	}
	pres := tenfoot.Presentation{Presentation: &tenfoot.PresentationInfo{
		Players: "1-2", Rating: "4.5", Completion: "100%", Portable: true,
	}}
	got := TitleBadges(game, pres)
	if len(got) != 4 || got[0].Kind != fbgrid.BadgePlayers || got[0].Label != "1-2" || got[3].Label != "PORT" {
		t.Fatalf("full %+v", got)
	}
	players := TitleBadges(game, tenfoot.Presentation{Presentation: &tenfoot.PresentationInfo{Players: "2"}})
	if len(players) != 1 || players[0].Label != "2P" {
		t.Fatalf("players %+v", players)
	}
}

func TestTitleBadgesPortableFromHandheldSystem(t *testing.T) {
	got := TitleBadges(tenfoot.Game{System: "gb"}, tenfoot.Presentation{})
	if len(got) != 1 || got[0].Kind != fbgrid.BadgePortable || got[0].Label != "PORT" {
		t.Fatalf("gb %+v", got)
	}
	if got := TitleBadges(tenfoot.Game{System: "snes"}, tenfoot.Presentation{}); len(got) != 0 {
		t.Fatalf("snes %+v", got)
	}
}

func TestPortableSystemMatchesHandheldIDs(t *testing.T) {
	if !tenfoot.PortableSystem("gba") || tenfoot.PortableSystem("megadrive") || tenfoot.PortableSystem("") {
		t.Fatal("portable system")
	}
}
