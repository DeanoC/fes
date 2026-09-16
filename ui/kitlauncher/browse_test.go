package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/fbgrid"
	"testing"
	"time"
)

func TestYCyclesBrowseLayoutsWithoutStealingDpad(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(25)}
	now := time.Unix(1, 0)
	if m.Browse != fbgrid.BrowseGrid {
		t.Fatalf("origin %s", m.Browse)
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseCoverflow {
		t.Fatalf("y1 %s", m.Browse)
	}
	pressNamed(&m, "dpad-right", now)
	if m.Focus != 1 || m.Browse != fbgrid.BrowseCoverflow {
		t.Fatalf("dpad after y focus=%d browse=%s", m.Focus, m.Browse)
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseWall {
		t.Fatalf("y2 %s", m.Browse)
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseSplit {
		t.Fatalf("y3 %s", m.Browse)
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseGrid {
		t.Fatalf("y4 %s", m.Browse)
	}
	if m.Focus != 1 {
		t.Fatalf("focus lost %d", m.Focus)
	}
}

func TestYIgnoredOnWheelDetailAttract(t *testing.T) {
	now := time.Unix(1, 0)
	wheel := Model{Connected: true, TargetReady: true, WheelOpen: true}
	wheel.SetCatalog(mixedCatalog())
	pressNamed(&wheel, "y", now)
	if wheel.Browse != fbgrid.BrowseGrid || !wheel.WheelOpen {
		t.Fatalf("wheel y browse=%s open=%v", wheel.Browse, wheel.WheelOpen)
	}
	detail := Model{Connected: true, TargetReady: true, Games: makeGames(4), DetailOpen: true}
	pressNamed(&detail, "y", now)
	if detail.Browse != fbgrid.BrowseGrid || !detail.DetailOpen {
		t.Fatalf("detail y browse=%s open=%v", detail.Browse, detail.DetailOpen)
	}
	attract := Model{Connected: true, TargetReady: true, Games: makeGames(4), AttractActive: true}
	pressNamed(&attract, "y", now)
	if attract.Browse != fbgrid.BrowseGrid {
		t.Fatalf("attract y cycled browse=%s", attract.Browse)
	}
	if attract.AttractActive {
		t.Fatal("attract y should dismiss like any pad input")
	}
}

func TestCoverflowDownEntersStripOrDetail(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(8), Browse: fbgrid.BrowseCoverflow}
	now := time.Unix(1, 0)
	m.Focus = 3
	pressNamed(&m, "dpad-down", now)
	if !m.DetailOpen || m.Focus != 3 {
		t.Fatalf("coverflow down detail=%v focus=%d", m.DetailOpen, m.Focus)
	}
	m.DetailOpen = false
	m.SetStrip([]hostclient.Game{{ID: "recent", Title: "Recent", Launchable: true}}, "Recent")
	pressNamed(&m, "dpad-down", now)
	if !m.StripActive || m.DetailOpen {
		t.Fatalf("coverflow down strip=%v detail=%v", m.StripActive, m.DetailOpen)
	}
}

func TestCoverflowLeftRightWalksCatalog(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(8), Browse: fbgrid.BrowseCoverflow}
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-right", now)
	if m.Focus != 1 {
		t.Fatalf("right %d", m.Focus)
	}
	for i := 0; i < 12; i++ {
		pressNamed(&m, "dpad-right", now)
	}
	if m.Focus != 7 {
		t.Fatalf("end %d", m.Focus)
	}
	pressNamed(&m, "dpad-left", now)
	if m.Focus != 6 {
		t.Fatalf("left %d", m.Focus)
	}
}

func TestWallDownMovesBySixAndLastRowOpensDetail(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(20), Browse: fbgrid.BrowseWall}
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-down", now)
	if m.Focus != 6 {
		t.Fatalf("wall down %d", m.Focus)
	}
	m.Focus = 19
	pressNamed(&m, "dpad-down", now)
	if !m.DetailOpen || m.Focus != 19 {
		t.Fatalf("wall last-row detail=%v focus=%d", m.DetailOpen, m.Focus)
	}
}

func TestSplitUpDownWalksListAndLastOpensDetail(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(8), Browse: fbgrid.BrowseSplit}
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-down", now)
	if m.Focus != 1 || m.DetailOpen {
		t.Fatalf("split down focus=%d detail=%v", m.Focus, m.DetailOpen)
	}
	pressNamed(&m, "dpad-right", now)
	if m.Focus != 1 {
		t.Fatalf("split right stole focus %d", m.Focus)
	}
	pressNamed(&m, "dpad-left", now)
	if m.Focus != 1 {
		t.Fatalf("split left stole focus %d", m.Focus)
	}
	pressNamed(&m, "dpad-up", now)
	if m.Focus != 0 {
		t.Fatalf("split up %d", m.Focus)
	}
	m.Focus = 7
	pressNamed(&m, "dpad-down", now)
	if !m.DetailOpen || m.Focus != 7 {
		t.Fatalf("split last-row detail=%v focus=%d", m.DetailOpen, m.Focus)
	}
}

func TestSplitALaunchesAndBOpensDetail(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(4), Browse: fbgrid.BrowseSplit}
	now := time.Unix(1, 0)
	if action := pressNamed(&m, "a", now); action != "launch" {
		t.Fatalf("split A %q", action)
	}
	if action := pressNamed(&m, "b", now); action != "" || !m.DetailOpen {
		t.Fatalf("split B action=%q detail=%v", action, m.DetailOpen)
	}
}

func TestSplitDownEntersStripWhenPresent(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(4), Browse: fbgrid.BrowseSplit}
	now := time.Unix(1, 0)
	m.Focus = 3
	m.SetStrip([]hostclient.Game{{ID: "recent", Title: "Recent", Launchable: true}}, "Recent")
	pressNamed(&m, "dpad-down", now)
	if !m.StripActive || m.DetailOpen || m.Focus != 3 {
		t.Fatalf("split down strip=%v detail=%v focus=%d", m.StripActive, m.DetailOpen, m.Focus)
	}
}

func TestEmptyCatalogYStillCyclesAndHidesTiles(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	now := time.Unix(1, 0)
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseCoverflow {
		t.Fatalf("empty y %s", m.Browse)
	}
	pressNamed(&m, "dpad-right", now)
	if m.Focus != 0 {
		t.Fatalf("empty dpad %d", m.Focus)
	}
}

func TestYDoesNotLaunchOrStop(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: []hostclient.Game{{ID: "pong", Launchable: true}}}
	now := time.Unix(1, 0)
	y, _ := remoteinput.NormalizeGamepad("y", true)
	if action := m.Input(y, now); action != "" {
		t.Fatalf("y launched %q", action)
	}
	m.Session.State = "active"
	if action := m.Input(y, now); action != "" {
		t.Fatalf("y stopped %q", action)
	}
}

func TestGridHintNamesNextLayout(t *testing.T) {
	m := Model{fromWheel: true, Browse: fbgrid.BrowseGrid}
	if got := m.GridHint(); got != "A play | B platforms | L/R | Y flow" {
		t.Fatalf("grid %q", got)
	}
	m.Browse = fbgrid.BrowseCoverflow
	if got := m.GridHint(); got != "A play | B platforms | L/R | Y wall" {
		t.Fatalf("flow %q", got)
	}
	m.Browse = fbgrid.BrowseWall
	if got := m.GridHint(); got != "A play | B platforms | L/R | Y split" {
		t.Fatalf("wall %q", got)
	}
	m.Browse = fbgrid.BrowseSplit
	if got := m.GridHint(); got != "A play | B platforms | L/R | Y grid" {
		t.Fatalf("split %q", got)
	}
}
