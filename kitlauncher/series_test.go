package kitlauncher

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
)

func seriesCatalog() []tenfoot.Game {
	return []tenfoot.Game{
		{ID: "sonic1", Title: "Sonic the Hedgehog", System: "megadrive", Launchable: true},
		{ID: "sonic2", Title: "Sonic the Hedgehog 2", System: "megadrive", Launchable: true},
		{ID: "sonic3", Title: "Sonic the Hedgehog 3", System: "snes", Launchable: true},
		{ID: "mario", Title: "Mario", System: "snes", Launchable: true},
	}
}

func TestSeriesHidesWithoutMates(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	if !m.DetailOpen {
		t.Fatal("expected detail")
	}
	m.ApplyPresentation("pong", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{Series: "Pong"},
	})
	if len(m.Series) != 0 || m.SeriesActive {
		t.Fatalf("alone series=%d active=%v", len(m.Series), m.SeriesActive)
	}
	pressNamed(&m, "dpad-down", now)
	if m.SeriesActive || !m.DetailOpen {
		t.Fatalf("down without mates active=%v open=%v", m.SeriesActive, m.DetailOpen)
	}
}

func TestDetailDownEntersSeriesAndAJumps(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(seriesCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	if m.Games[m.Focus].ID != "sonic1" {
		t.Fatalf("focus %s", m.Games[m.Focus].ID)
	}
	m.ApplyPresentation("sonic1", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{Series: "Sonic the Hedgehog"},
	})
	if len(m.Series) != 2 || m.Series[0].ID != "sonic2" {
		t.Fatalf("mates %v", ids(m.Series))
	}
	if action := pressNamed(&m, "dpad-down", now); action != "" || !m.SeriesActive || m.SeriesFocus != 0 {
		t.Fatalf("enter series action=%q active=%v focus=%d", action, m.SeriesActive, m.SeriesFocus)
	}
	if action := pressNamed(&m, "dpad-right", now); action != "" || m.SeriesFocus != 1 {
		t.Fatalf("series right action=%q focus=%d", action, m.SeriesFocus)
	}
	if action := pressNamed(&m, "a", now); action != "" || !m.DetailOpen {
		t.Fatalf("series A action=%q detail=%v", action, m.DetailOpen)
	}
	game, ok := m.FocusedGame()
	if !ok || game.ID != "sonic3" {
		t.Fatalf("jump game %+v ok=%v", game, ok)
	}
	if m.Shelf != "snes" {
		t.Fatalf("jump shelf %q", m.Shelf)
	}
	if m.SeriesActive {
		t.Fatal("jump left series focused")
	}
	if action := pressNamed(&m, "b", now); action != "" || m.DetailOpen {
		t.Fatalf("detail B action=%q open=%v", action, m.DetailOpen)
	}
	if m.Games[m.Focus].ID != "sonic3" || m.Shelf != "snes" {
		t.Fatalf("browse after jump shelf=%q id=%s", m.Shelf, m.Games[m.Focus].ID)
	}
}

func TestSeriesBReturnsToDetailWithoutClosing(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(seriesCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	m.ApplyPresentation("sonic1", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{Series: "Sonic the Hedgehog"},
	})
	pressNamed(&m, "dpad-down", now)
	if !m.SeriesActive {
		t.Fatal("expected series")
	}
	pressNamed(&m, "b", now)
	if !m.DetailOpen || m.SeriesActive {
		t.Fatalf("B should leave series open=%v active=%v", m.DetailOpen, m.SeriesActive)
	}
	if m.Games[m.Focus].ID != "sonic1" {
		t.Fatalf("focus moved %s", m.Games[m.Focus].ID)
	}
}

func TestSeriesDoesNotStealYXLaunchOrShots(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(seriesCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	aa := handleAA()
	bb := handleBB()
	m.ApplyPresentation("sonic1", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{
			Series:        "Sonic the Hedgehog",
			ScreenshotIDs: []string{aa, bb},
		},
	})
	if action := pressNamed(&m, "a", now); action != "launch" {
		t.Fatalf("detail A %q", action)
	}
	m.launchID = ""
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseGrid || !m.DetailOpen {
		t.Fatalf("Y stole detail browse=%s open=%v", m.Browse, m.DetailOpen)
	}
	pressNamed(&m, "r", now)
	if m.ShotHandle() != bb || m.SeriesActive {
		t.Fatalf("R shot %q series=%v", m.ShotHandle(), m.SeriesActive)
	}
	pressNamed(&m, "dpad-down", now)
	if !m.SeriesActive {
		t.Fatal("down series")
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseGrid || !m.DetailOpen || !m.SeriesActive {
		t.Fatalf("Y on series browse=%s open=%v series=%v", m.Browse, m.DetailOpen, m.SeriesActive)
	}
	pressNamed(&m, "x", now)
	if !m.DetailOpen || !m.SeriesActive {
		t.Fatalf("X stole series open=%v series=%v", m.DetailOpen, m.SeriesActive)
	}
}

func TestSplitRightEntersSeriesAndAOpensDetail(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Browse: fbgrid.BrowseSplit}
	m.SetCatalog(seriesCatalog())
	now := time.Unix(1, 0)
	m.ApplyPresentation("sonic1", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{Series: "Sonic the Hedgehog"},
	})
	if len(m.Series) < 1 {
		t.Fatal("expected mates")
	}
	if action := pressNamed(&m, "dpad-right", now); action != "" || !m.SeriesActive || m.Focus != 0 {
		t.Fatalf("split right action=%q series=%v focus=%d", action, m.SeriesActive, m.Focus)
	}
	pressNamed(&m, "dpad-right", now)
	if m.SeriesFocus != 1 {
		t.Fatalf("series right %d", m.SeriesFocus)
	}
	if action := pressNamed(&m, "a", now); action != "" || !m.DetailOpen {
		t.Fatalf("series A action=%q detail=%v", action, m.DetailOpen)
	}
	game, ok := m.FocusedGame()
	if !ok || game.ID != "sonic3" {
		t.Fatalf("split jump %+v ok=%v", game, ok)
	}
}

func TestSplitRightWithoutSeriesStillClamps(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Browse: fbgrid.BrowseSplit, Games: makeGames(8)}
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-down", now)
	pressNamed(&m, "dpad-right", now)
	if m.Focus != 1 || m.SeriesActive {
		t.Fatalf("split right stole focus=%d series=%v", m.Focus, m.SeriesActive)
	}
}

func TestDetailHintNamesSeriesChord(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(seriesCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	if got := m.DetailHint(); got != "A play | B back" {
		t.Fatalf("empty hint %q", got)
	}
	m.ApplyPresentation("sonic1", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{Series: "Sonic the Hedgehog"},
	})
	if got := m.DetailHint(); got != "A play | B back | Down series" {
		t.Fatalf("series hint %q", got)
	}
	pressNamed(&m, "dpad-down", now)
	if got := m.DetailHint(); got != "A open | B title | L/R" {
		t.Fatalf("focused hint %q", got)
	}
}
