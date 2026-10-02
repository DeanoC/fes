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

func TestWheelReadQueriesDoNotCopyCatalog(t *testing.T) {
	m := Model{Shelf: " NES "}
	m.SetCatalog([]hostclient.Game{
		{ID: "fallback", System: " NES ", PlayCount: -1},
		{ID: "other", System: "snes", Launchable: true, PlayCount: 99, LastPlayedAt: 99},
		{ID: "launch", System: "NeS", Launchable: true, PlayCount: 3, LastPlayedAt: 5},
		{ID: "latest", System: "nes", PlayCount: 2, LastPlayedAt: 7},
	})
	if game, ok := m.WheelGame(" NES "); !ok || game.ID != "launch" {
		t.Fatalf("representative = %q, %v", game.ID, ok)
	}
	if count := m.WheelPlayCount(); count != 5 {
		t.Fatalf("plays = %d", count)
	}
	if game, ok := m.WheelLastPlayed(); !ok || game.ID != "latest" {
		t.Fatalf("last played = %q, %v", game.ID, ok)
	}
	for _, item := range m.WheelItems() {
		want := len(filterGames(m.Catalog, item.ID))
		if item.Count != want {
			t.Fatalf("%s count = %d, want %d", item.ID, item.Count, want)
		}
	}
	if allocs := testing.AllocsPerRun(100, func() {
		m.WheelGame("nes")
		m.WheelPlayCount()
		m.WheelLastPlayed()
	}); allocs != 0 {
		t.Fatalf("read queries allocate catalog copies: %g", allocs)
	}
	next := append([]hostclient.Game(nil), m.Catalog...)
	next[2].Launchable = false
	m.ApplyCatalog(next)
	if game, _ := m.WheelGame("nes"); game.ID != "fallback" {
		t.Fatalf("fallback = %q", game.ID)
	}
}

func BenchmarkWheelLargeCatalog(b *testing.B) {
	m := Model{Shelf: "nes", Shelves: []string{ShelfAll, "nes", "snes"}}
	m.Catalog = make([]hostclient.Game, 3656)
	for i := range m.Catalog {
		m.Catalog[i] = hostclient.Game{System: "nes", PlayCount: 1, LastPlayedAt: int64(i)}
	}
	m.Catalog[len(m.Catalog)-1].Launchable = true
	m.SetCatalog(m.Catalog)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.WheelItems()
		m.WheelGame("nes")
		m.WheelPlayCount()
		m.WheelLastPlayed()
	}
}

func TestWheelSummariesRefreshWithCatalogFields(t *testing.T) {
	m := Model{Shelf: "nes"}
	games := []hostclient.Game{{ID: "one", Title: "One", System: "nes", Launchable: true}, {ID: "two", Title: "Two", System: "nes"}}
	m.SetCatalog(games)
	games[1].PlayCount, games[1].LastPlayedAt = 4, 100
	m.ApplyCatalog(games) // Browse-identical updates also rebuild play summaries.
	if game, ok := m.WheelLastPlayed(); !ok || game.ID != "two" || m.WheelPlayCount() != 4 {
		t.Fatalf("stale summaries: last=%q plays=%d", game.ID, m.WheelPlayCount())
	}
	games[0].Launchable, games[1].Launchable = false, true
	m.ApplyCatalog(games)
	if game, _ := m.WheelGame("nes"); game.ID != "two" {
		t.Fatalf("stale representative %q", game.ID)
	}
	m.ApplyCatalog(nil)
	if _, ok := m.WheelGame("all"); ok || m.WheelPlayCount() != 0 {
		t.Fatal("removed catalog remains in summaries")
	}
	// Directly constructed models retain the same query semantics.
	m = Model{Shelf: "nes", Catalog: games}
	if game, _ := m.WheelGame("nes"); game.ID != "two" || m.WheelPlayCount() != 4 {
		t.Fatalf("direct model: representative=%q plays=%d", game.ID, m.WheelPlayCount())
	}
}
