package main

import (
	"strings"
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
	g := modelGrid(m, 640, 480, nil)
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

func TestCatalogPageAndPrefetchWindow(t *testing.T) {
	start, end := catalogPage(0, 25)
	if start != 0 || end != 12 {
		t.Fatalf("page0 %d:%d", start, end)
	}
	start, end = catalogPage(12, 25)
	if start != 12 || end != 24 {
		t.Fatalf("page1 %d:%d", start, end)
	}
	start, end = catalogPage(24, 25)
	if start != 24 || end != 25 {
		t.Fatalf("page2 %d:%d", start, end)
	}
}

func TestGameTileLeavesCoverEmptyUntilCached(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	tile := gameTile(tenfoot.Game{Title: "Sonic", System: "megadrive", Cover: handle}, tenfoot.NewCoverCache())
	if tile.Cover != nil {
		t.Fatal("uncached cover should stay fallback")
	}
	flat := gameTile(tenfoot.Game{Title: "Pong", System: "pong"}, tenfoot.NewCoverCache())
	if flat.Cover != nil {
		t.Fatal("missing handle should stay fallback")
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
	tile := gameTile(tenfoot.Game{Title: "Márío", System: "snes"}, nil)
	if tile.Name != "M?r?o" {
		t.Fatalf("label %q", tile.Name)
	}
	if tile.Color != gfx.RGB(156, 52, 60) {
		t.Fatalf("color %+v", tile.Color)
	}
}
