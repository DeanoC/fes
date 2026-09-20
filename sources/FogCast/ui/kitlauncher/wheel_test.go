package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"strings"
	"testing"
	"time"
)

func TestOfflineLocalCatalogBrowsesWithoutLaunch(t *testing.T) {
	m := Model{Connected: false, TargetReady: false, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	if action := pressNamed(&m, "dpad-right", now); action != "" {
		t.Fatalf("offline wheel nav launched %q", action)
	}
	if m.Shelf != "pong" || !m.WheelOpen {
		t.Fatalf("offline wheel shelf=%q wheel=%v", m.Shelf, m.WheelOpen)
	}
	if action := pressNamed(&m, "r", now); action != "" {
		t.Fatalf("offline shoulder launched %q", action)
	}
	if action := pressNamed(&m, "a", now); action != "" {
		t.Fatalf("offline wheel A launched %q", action)
	}
	if m.WheelOpen || !m.fromWheel || m.Shelf != "megadrive" {
		t.Fatalf("offline enter wheel=%v from=%v shelf=%q", m.WheelOpen, m.fromWheel, m.Shelf)
	}
	right, _ := remoteinput.NormalizeGamepad("dpad-right", true)
	if action := m.Input(right, now); action != "" {
		t.Fatalf("offline grid nav launched %q", action)
	}
	if m.Focus != 1 || m.Games[m.Focus].ID != "streets" {
		t.Fatalf("offline grid focus=%d games=%v", m.Focus, ids(m.Games))
	}
	if action := pressNamed(&m, "a", now); action != "" {
		t.Fatalf("offline grid A %q", action)
	}
	m.fromWheel = false
	if action := pressNamed(&m, "b", now); action != "" {
		t.Fatalf("offline detail open launched %q", action)
	}
	if !m.DetailOpen {
		t.Fatal("offline B did not open detail")
	}
	if action := pressNamed(&m, "a", now); action != "" {
		t.Fatalf("offline detail A %q", action)
	}
}

func TestWheelFocusCyclesPlatformsAndEnterOpensGrid(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	if m.Shelf != ShelfAll || !m.WheelOpen {
		t.Fatalf("origin shelf=%q wheel=%v", m.Shelf, m.WheelOpen)
	}
	if items := m.WheelItems(); len(items) != 4 || items[0].ID != ShelfAll || items[1].ID != "pong" {
		t.Fatalf("items %#v", items)
	}
	pressNamed(&m, "dpad-right", now)
	if m.Shelf != "pong" || !m.WheelOpen || m.WheelIndex() != 1 {
		t.Fatalf("right shelf=%q wheel=%v idx=%d", m.Shelf, m.WheelOpen, m.WheelIndex())
	}
	if m.WheelStats() != "1 game" || m.WheelFeaturedTitle() != "Pong" {
		t.Fatalf("pong stats=%q featured=%q", m.WheelStats(), m.WheelFeaturedTitle())
	}
	pressNamed(&m, "r", now)
	if m.Shelf != "megadrive" || len(m.Games) != 3 {
		t.Fatalf("shoulder shelf=%q n=%d", m.Shelf, len(m.Games))
	}
	if action := pressNamed(&m, "a", now); action != "" {
		t.Fatalf("enter launched %q", action)
	}
	if m.WheelOpen || !m.fromWheel || m.Shelf != "megadrive" {
		t.Fatalf("enter wheel=%v from=%v shelf=%q", m.WheelOpen, m.fromWheel, m.Shelf)
	}
	right, _ := remoteinput.NormalizeGamepad("dpad-right", true)
	m.Input(right, now)
	if m.Focus != 1 || m.Games[m.Focus].ID != "streets" {
		t.Fatalf("grid nav focus=%d games=%v", m.Focus, ids(m.Games))
	}
	if action := pressNamed(&m, "a", now); action != "launch" {
		t.Fatalf("grid A %q", action)
	}
	pressNamed(&m, "b", now)
	if !m.WheelOpen || m.fromWheel || m.Shelf != "megadrive" {
		t.Fatalf("back wheel=%v from=%v shelf=%q", m.WheelOpen, m.fromWheel, m.Shelf)
	}
	if m.DetailOpen {
		t.Fatal("back opened detail")
	}
}

func TestWheelBackDoesNotStealGridDetailWhenNotFromWheel(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	if m.WheelOpen {
		t.Fatal("grid tests start on wheel")
	}
	pressNamed(&m, "b", now)
	if !m.DetailOpen || m.WheelOpen {
		t.Fatalf("B detail open=%v wheel=%v", m.DetailOpen, m.WheelOpen)
	}
}

func TestWheelLastRowDownStillOpensDetailFromGrid(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "r", now)
	pressNamed(&m, "r", now)
	pressNamed(&m, "a", now)
	if m.WheelOpen || m.Shelf != "megadrive" {
		t.Fatalf("enter shelf=%q wheel=%v", m.Shelf, m.WheelOpen)
	}
	m.Focus = len(m.Games) - 1
	pressNamed(&m, "dpad-down", now)
	if !m.DetailOpen || m.WheelOpen {
		t.Fatalf("last-row down open=%v wheel=%v", m.DetailOpen, m.WheelOpen)
	}
	pressNamed(&m, "b", now)
	if m.DetailOpen || m.WheelOpen || m.Shelf != "megadrive" {
		t.Fatalf("detail B wheel=%v open=%v shelf=%q", m.WheelOpen, m.DetailOpen, m.Shelf)
	}
	pressNamed(&m, "b", now)
	if !m.WheelOpen || m.DetailOpen {
		t.Fatalf("grid B wheel=%v open=%v", m.WheelOpen, m.DetailOpen)
	}
}

func TestWheelShouldersWrapAndSelectCycles(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "l", now)
	if m.Shelf != "snes" || !m.WheelOpen {
		t.Fatalf("left wrap %q", m.Shelf)
	}
	pressNamed(&m, "select", now)
	if m.Shelf != ShelfAll {
		t.Fatalf("select %q", m.Shelf)
	}
	pressNamed(&m, "dpad-down", now)
	if m.Shelf != "pong" {
		t.Fatalf("down %q", m.Shelf)
	}
}

func TestWheelADoesNotLaunch(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	if action := pressNamed(&m, "a", now); action != "" {
		t.Fatalf("wheel A %q", action)
	}
	if m.WheelOpen {
		t.Fatal("A did not enter")
	}
}

func TestWheelHeroPrefersAttractThenBackdropThenCover(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	m.CycleShelf(1)
	m.CycleShelf(1)
	if m.Shelf != "megadrive" {
		t.Fatalf("shelf %q", m.Shelf)
	}
	cover := strings.Repeat("11", 32)
	backdrop := strings.Repeat("22", 32)
	still := strings.Repeat("33", 32)
	pres := hostclient.Presentation{Presentation: &hostclient.PresentationInfo{
		CoverArtworkID:    cover,
		BackdropArtworkID: backdrop,
		LogoID:            strings.Repeat("44", 32),
	}}
	if got := m.WheelHeroHandle(pres); got != backdrop {
		t.Fatalf("backdrop hero %q", got)
	}
	if got := m.WheelLogoHandle(pres); got != strings.Repeat("44", 32) {
		t.Fatalf("logo %q", got)
	}
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{{
		GameID: "sonic", Title: "Sonic", Platform: "megadrive", Backdrop: still, Launchable: true,
	}}})
	if got := m.WheelHeroHandle(pres); got != still {
		t.Fatalf("attract hero %q", got)
	}
	m.Shelf = "snes"
	m.applyFilter("")
	if got := m.WheelHeroHandle(hostclient.Presentation{}); got != "" {
		t.Fatalf("other shelf used md still %q", got)
	}
}

func TestWheelPrefetchIDsFocusFirst(t *testing.T) {
	m := Model{}
	m.SetCatalog(mixedCatalog())
	m.Shelf = "snes"
	m.applyFilter("")
	ids := m.WheelPrefetchIDs()
	if len(ids) < 3 || ids[0] != "mario" {
		t.Fatalf("prefetch %v", ids)
	}
}

func TestWheelStatsRollsUpPlayCountAndLastPlayed(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	games := mixedCatalog()
	for i := range games {
		if games[i].ID == "sonic" {
			games[i].PlayCount = 4
			games[i].LastPlayedAt = 200
		}
		if games[i].ID == "streets" {
			games[i].PlayCount = 1
			games[i].LastPlayedAt = 50
		}
	}
	m.SetCatalog(games)
	m.Shelf = "megadrive"
	m.applyFilter("")
	if m.WheelStats() != "3 games  |  5 plays" {
		t.Fatalf("stats %q", m.WheelStats())
	}
	if m.WheelFeaturedTitle() != "Sonic" {
		t.Fatalf("featured %q", m.WheelFeaturedTitle())
	}
	m.Shelf = "snes"
	m.applyFilter("")
	if m.WheelStats() != "2 games" || m.WheelFeaturedTitle() != "Mario" {
		t.Fatalf("snes stats=%q featured=%q", m.WheelStats(), m.WheelFeaturedTitle())
	}
	m.Recents = []hostclient.Game{{ID: "zelda", Title: "Zelda", System: "snes"}}
	if m.WheelFeaturedTitle() != "Zelda" {
		t.Fatalf("recents featured %q", m.WheelFeaturedTitle())
	}
}

func TestWheelEmptyCatalogIsIdle(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	now := time.Unix(1, 0)
	if action := pressNamed(&m, "a", now); action != "" {
		t.Fatalf("empty A %q", action)
	}
	if !m.WheelOpen || m.WheelStats() != "0 games" {
		t.Fatalf("empty wheel=%v stats=%q", m.WheelOpen, m.WheelStats())
	}
}
