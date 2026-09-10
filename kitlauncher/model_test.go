package kitlauncher

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestMenuAndGameControlsRemainSeparate(t *testing.T) {
	m := Model{Games: []tenfoot.Game{{ID: "pong", Launchable: true}, {ID: "sonic", Launchable: true}}, Connected: true, TargetReady: true}
	right, _ := remoteinput.NormalizeAxis("left-x", 32767)
	m.Input(right, time.Now())
	if m.Focus != 1 {
		t.Fatal("navigation lost")
	}
	m.Input(right, time.Now())
	if m.Focus != 1 {
		t.Fatal("held axis repeated")
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "launch" {
		t.Fatal(action)
	}
	m.Session.State = "active"
	b, _ := remoteinput.NormalizeGamepad("b", true)
	if action := m.Input(b, time.Now()); action != "" {
		t.Fatal("B stopped gameplay")
	}
	m.Connected = false
	m.Session.State = "idle"
	if m.Input(a, time.Now()) != "launch" {
		t.Fatal("offline cache-hit attempt")
	}
}

func TestSessionChromeDoesNotStealEastOrStart(t *testing.T) {
	m := Model{
		Games:     []tenfoot.Game{{ID: "sonic", Title: "Sonic 2", Launchable: true}},
		Catalog:   []tenfoot.Game{{ID: "sonic", Title: "Sonic 2", Launchable: true}},
		Connected: true, TargetReady: true,
	}
	m.Session.State = "active"
	m.Session.GameID = "sonic"
	chrome := m.SessionChrome()
	if chrome.State != "active" || chrome.Title != "Sonic 2" || chrome.Hint != fbgrid.SessionKitHint {
		t.Fatalf("chrome %+v", chrome)
	}
	now := time.Now()
	for _, name := range []string{"b", "start", "a", "y", "x"} {
		e, err := remoteinput.NormalizeGamepad(name, true)
		if err != nil {
			t.Fatal(err)
		}
		if action := m.Input(e, now); action != "" {
			t.Fatalf("%s stole %q while session active", name, action)
		}
	}
	m.Busy = true
	m.Message = "Stopping game"
	m.Session.State = "active"
	if m.SessionChrome().State != "stopping" {
		t.Fatalf("busy stop chrome %+v", m.SessionChrome())
	}
}

func TestCatalogGridNavigationClamps(t *testing.T) {
	m := Model{Games: makeGames(25), Connected: true, TargetReady: true}
	now := time.Now()
	press := func(name string) {
		e, err := remoteinput.NormalizeGamepad(name, true)
		if err != nil {
			t.Fatal(err)
		}
		m.Input(e, now)
	}
	press("dpad-right")
	if m.Focus != 1 {
		t.Fatalf("right %d", m.Focus)
	}
	for i := 0; i < 8; i++ {
		press("dpad-right")
	}
	if m.Focus != 3 {
		t.Fatalf("row clamp %d", m.Focus)
	}
	press("dpad-left")
	if m.Focus != 2 {
		t.Fatalf("left %d", m.Focus)
	}
	m.Focus = 0
	press("dpad-left")
	if m.Focus != 0 {
		t.Fatalf("left catalog clamp wrapped %d", m.Focus)
	}
	press("dpad-up")
	if m.Focus != 0 {
		t.Fatalf("up catalog clamp wrapped %d", m.Focus)
	}
	press("dpad-down")
	if m.Focus != 4 {
		t.Fatalf("down by columns %d", m.Focus)
	}
	m.Focus = 11
	press("dpad-down")
	if m.Focus != 15 {
		t.Fatalf("page boundary %d", m.Focus)
	}
	m.Focus = 24
	press("dpad-right")
	if m.Focus != 24 {
		t.Fatalf("last-row clamp %d", m.Focus)
	}
	press("dpad-down")
	if m.Focus != 24 {
		t.Fatalf("last-row down clamp %d", m.Focus)
	}
	if !m.DetailOpen {
		t.Fatal("last-row down should open detail")
	}
	press("dpad-up")
	if m.DetailOpen || m.Focus != 24 {
		t.Fatalf("up closed detail focus=%d open=%v", m.Focus, m.DetailOpen)
	}
	press("dpad-up")
	if m.Focus != 20 {
		t.Fatalf("up from last row %d", m.Focus)
	}
}

func TestAnalogStickRisingEdgeDoesNotSpam(t *testing.T) {
	m := Model{Games: makeGames(25), Connected: true, TargetReady: true}
	now := time.Now()
	right, _ := remoteinput.NormalizeAxis("left-x", 32767)
	down, _ := remoteinput.NormalizeAxis("left-y", 32767)
	idleX, _ := remoteinput.NormalizeAxis("left-x", 0)
	idleY, _ := remoteinput.NormalizeAxis("left-y", 0)
	m.Input(right, now)
	m.Input(right, now)
	m.Input(right, now)
	if m.Focus != 1 {
		t.Fatalf("held X %d", m.Focus)
	}
	m.Input(idleX, now)
	m.Input(down, now)
	m.Input(down, now)
	if m.Focus != 5 {
		t.Fatalf("held Y %d", m.Focus)
	}
	m.Input(idleY, now)
	left, _ := remoteinput.NormalizeAxis("left-x", -32767)
	m.Input(left, now)
	if m.Focus != 4 {
		t.Fatalf("left stick %d", m.Focus)
	}
}

func makeGames(n int) []tenfoot.Game {
	games := make([]tenfoot.Game, n)
	for i := range games {
		games[i] = tenfoot.Game{ID: "g" + string(rune('a'+i%26)), Launchable: true}
	}
	return games
}

func TestIdentityRemapStillLaunchesOnA(t *testing.T) {
	r := mustRemapper(t, "")
	m := Model{Games: []tenfoot.Game{{ID: "pong", Launchable: true}}, Connected: true, TargetReady: true}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(r.Apply(a), time.Now()); action != "launch" {
		t.Fatalf("identity launch %q", action)
	}
}

func TestSwapABRemapLaunchesOnB(t *testing.T) {
	r := mustRemapper(t, "swap-ab")
	m := Model{Games: []tenfoot.Game{{ID: "pong", Launchable: true}}, Connected: true, TargetReady: true}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(r.Apply(a), time.Now()); action != "" {
		t.Fatalf("A launched under swap-ab: %q", action)
	}
	b, _ := remoteinput.NormalizeGamepad("b", true)
	if action := m.Input(r.Apply(b), time.Now()); action != "launch" {
		t.Fatalf("B launch %q", action)
	}
}

func mustRemapper(t *testing.T, spec string) *inputmap.Remapper {
	t.Helper()
	p, err := inputmap.Resolve(spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := inputmap.NewRemapper(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mixedCatalog() []tenfoot.Game {
	return []tenfoot.Game{
		{ID: "pong", Title: "Pong", System: "pong", Launchable: true},
		{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true},
		{ID: "streets", Title: "Streets", System: "megadrive", Launchable: true},
		{ID: "blocked-md", Title: "Blocked", System: "megadrive", Launchable: false},
		{ID: "mario", Title: "Mario", System: "snes", Launchable: true},
		{ID: "zelda", Title: "Zelda", System: "snes", Launchable: true},
	}
}

func TestShelvesDeriveFromLoadedGames(t *testing.T) {
	m := Model{}
	m.SetCatalog(mixedCatalog())
	want := []string{ShelfAll, "pong", "megadrive", "snes"}
	if len(m.Shelves) != len(want) {
		t.Fatalf("shelves %v", m.Shelves)
	}
	for i, id := range want {
		if m.Shelves[i] != id {
			t.Fatalf("shelves %v want %v", m.Shelves, want)
		}
	}
	if m.Shelf != ShelfAll || len(m.Games) != 6 {
		t.Fatalf("all shelf=%q n=%d", m.Shelf, len(m.Games))
	}
	if chrome := m.ShelfChrome(); chrome != "ALL 6/6" {
		t.Fatalf("chrome %q", chrome)
	}
}

func TestCycleShelfFiltersVisibleGames(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Now()
	press := func(name string) {
		e, err := remoteinput.NormalizeGamepad(name, true)
		if err != nil {
			t.Fatal(err)
		}
		m.Input(e, now)
	}
	press("r")
	if m.Shelf != "pong" || len(m.Games) != 1 || m.Games[0].ID != "pong" {
		t.Fatalf("pong shelf=%q games=%v", m.Shelf, ids(m.Games))
	}
	if chrome := m.ShelfChrome(); chrome != "PONG 1/6" {
		t.Fatalf("pong chrome %q", chrome)
	}
	press("r")
	if m.Shelf != "megadrive" || len(m.Games) != 3 || m.Games[0].ID != "sonic" {
		t.Fatalf("md shelf=%q games=%v", m.Shelf, ids(m.Games))
	}
	if chrome := m.ShelfChrome(); chrome != "MEGADRIVE 3/6" {
		t.Fatalf("md chrome %q", chrome)
	}
	press("r")
	if m.Shelf != "snes" || len(m.Games) != 2 {
		t.Fatalf("snes shelf=%q n=%d", m.Shelf, len(m.Games))
	}
	press("r")
	if m.Shelf != ShelfAll || len(m.Games) != 6 {
		t.Fatalf("wrap all shelf=%q n=%d", m.Shelf, len(m.Games))
	}
	press("l")
	if m.Shelf != "snes" {
		t.Fatalf("left wrap %q", m.Shelf)
	}
	press("select")
	if m.Shelf != ShelfAll {
		t.Fatalf("select %q", m.Shelf)
	}
}

func TestCycleShelfKeepsVisibleFocus(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.Focus = 0
	now := time.Now()
	r, _ := remoteinput.NormalizeGamepad("r", true)
	m.Input(r, now)
	if m.Shelf != "pong" || m.Focus != 0 || m.Games[0].ID != "pong" {
		t.Fatalf("keep pong shelf=%q focus=%d games=%v", m.Shelf, m.Focus, ids(m.Games))
	}
	l, _ := remoteinput.NormalizeGamepad("l", true)
	m.Input(l, now)
	if m.Shelf != ShelfAll || m.Games[m.Focus].ID != "pong" {
		t.Fatalf("back to all focus=%s", m.Games[m.Focus].ID)
	}
}

func TestCycleShelfResetsWhenFocusLeaves(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.Focus = 1
	now := time.Now()
	r, _ := remoteinput.NormalizeGamepad("r", true)
	m.Input(r, now)
	if m.Shelf != "pong" || m.Games[m.Focus].ID != "pong" {
		t.Fatalf("left megadrive onto pong focus=%d games=%v", m.Focus, ids(m.Games))
	}
}

func TestCycleShelfLandsOnFirstLaunchable(t *testing.T) {
	games := []tenfoot.Game{
		{ID: "locked", System: "snes", Launchable: false},
		{ID: "mario", System: "snes", Launchable: true},
	}
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(append([]tenfoot.Game{{ID: "pong", System: "pong", Launchable: true}}, games...))
	now := time.Now()
	r, _ := remoteinput.NormalizeGamepad("r", true)
	m.Input(r, now)
	m.Input(r, now)
	if m.Shelf != "snes" || m.Games[m.Focus].ID != "mario" {
		t.Fatalf("shelf=%q focus=%d id=%s", m.Shelf, m.Focus, m.Games[m.Focus].ID)
	}
}

func TestShoulderDoesNotBreakGridNavOrLaunch(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Now()
	right, _ := remoteinput.NormalizeGamepad("dpad-right", true)
	m.Input(right, now)
	if m.Focus != 1 {
		t.Fatalf("grid right %d", m.Focus)
	}
	l, _ := remoteinput.NormalizeGamepad("l", true)
	m.Input(l, now)
	if m.Shelf != "snes" {
		t.Fatalf("l shelf %q", m.Shelf)
	}
	m.Input(right, now)
	if m.Focus != 1 || m.Games[m.Focus].ID != "zelda" {
		t.Fatalf("nav after shelf focus=%d games=%v", m.Focus, ids(m.Games))
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, now); action != "launch" {
		t.Fatalf("launch %q", action)
	}
}

func TestSelectDoesNotCycleDuringPlay(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Session: Session{State: "active"}}
	m.SetCatalog(mixedCatalog())
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	r, _ := remoteinput.NormalizeGamepad("r", true)
	now := time.Now()
	m.Input(selectPress, now)
	m.Input(r, now)
	if m.Shelf != ShelfAll {
		t.Fatalf("play shelf %q", m.Shelf)
	}
}

func TestInitialShelfFromConfig(t *testing.T) {
	m := Model{Shelf: "megadrive"}
	m.SetCatalog(mixedCatalog())
	if m.Shelf != "megadrive" || len(m.Games) != 3 || m.Games[0].ID != "sonic" {
		t.Fatalf("initial shelf=%q n=%d games=%v", m.Shelf, len(m.Games), ids(m.Games))
	}
}

func TestSetCatalogKeepsShelfAndFocus(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	r, _ := remoteinput.NormalizeGamepad("r", true)
	now := time.Now()
	m.Input(r, now)
	m.Input(r, now)
	m.Focus = 1
	if m.Shelf != "megadrive" || m.Games[m.Focus].ID != "streets" {
		t.Fatalf("setup shelf=%q focus=%s", m.Shelf, m.Games[m.Focus].ID)
	}
	m.SetCatalog(mixedCatalog())
	if m.Shelf != "megadrive" || m.Games[m.Focus].ID != "streets" || len(m.Games) != 3 {
		t.Fatalf("reload shelf=%q focus=%s n=%d", m.Shelf, m.Games[m.Focus].ID, len(m.Games))
	}
}

func TestSetCatalogDropsMissingShelf(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.Shelf = "nes"
	m.SetCatalog(mixedCatalog())
	if m.Shelf != ShelfAll || len(m.Games) != 6 {
		t.Fatalf("missing shelf=%q n=%d", m.Shelf, len(m.Games))
	}
}

func TestHeaderChromeEmptyCatalog(t *testing.T) {
	m := Model{}
	if got := m.HeaderChrome(); got != "FOGCAST" {
		t.Fatalf("empty %q", got)
	}
	m.SetCatalog(mixedCatalog())
	if got := m.HeaderChrome(); got != "FOGCAST  ALL 6/6" {
		t.Fatalf("all %q", got)
	}
}

func ids(games []tenfoot.Game) []string {
	out := make([]string, len(games))
	for i, game := range games {
		out[i] = game.ID
	}
	return out
}

func TestFailedSessionCanRequestRecoveryStop(t *testing.T) {
	now := time.Now()
	m := Model{Session: Session{State: "failed"}}
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	startPress, _ := remoteinput.NormalizeGamepad("start", true)
	m.Input(selectPress, now)
	m.Input(startPress, now)
	if action := m.Tick(now.Add(time.Second)); action != "stop" {
		t.Fatalf("failed-session recovery action = %q, want stop", action)
	}
}
