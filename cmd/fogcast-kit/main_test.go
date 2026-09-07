package main

import (
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/kitlauncher"
)

func TestModelGridUsesLiveGamesAndPages(t *testing.T) {
	games := make([]tenfoot.Game, 13)
	for i := range games {
		games[i] = tenfoot.Game{ID: "game-" + string(rune('a'+i)), Title: "Title " + string(rune('A'+i)), System: "snes", Launchable: true}
	}
	m := kitlauncher.Model{Games: games, Focus: 12, Connected: true, TargetReady: true, ControllerConnected: true}
	g := modelGrid(m, 640, 480)
	if len(g.Tiles) != 1 || g.Focus != 0 {
		t.Fatalf("page tiles=%d focus=%d", len(g.Tiles), g.Focus)
	}
	if g.Tiles[0].Name != "Title M" {
		t.Fatalf("tile %q", g.Tiles[0].Name)
	}
	if g.Header != "FOGCAST" || g.Footer == "" {
		t.Fatalf("header=%q footer=%q", g.Header, g.Footer)
	}
}

func TestModelFooterReportsConnectionBeforeController(t *testing.T) {
	m := kitlauncher.Model{Message: "Host unavailable - reconnecting", ControllerConnected: false}
	if got := modelFooter(m); got != "Host unavailable - reconnecting" {
		t.Fatalf("footer %q", got)
	}
	m = kitlauncher.Model{Connected: true, TargetReady: true, Message: "", ControllerConnected: false}
	if got := modelFooter(m); got != "Connect USB gamepad" {
		t.Fatalf("controller footer %q", got)
	}
}

func TestGameTileUsesSystemPaletteAndASCIILabel(t *testing.T) {
	tile := gameTile(tenfoot.Game{Title: "Márío", System: "snes"})
	if tile.Name != "M?r?o" {
		t.Fatalf("label %q", tile.Name)
	}
	if tile.Color != gfx.RGB(156, 52, 60) {
		t.Fatalf("color %+v", tile.Color)
	}
}
