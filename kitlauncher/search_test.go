package kitlauncher

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestStartOpensSearchWithoutStealingYLayoutXPackSelectShelf(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	if action := pressNamed(&m, "start", now); action != "" || !m.SearchOpen {
		t.Fatalf("start action=%q open=%v", action, m.SearchOpen)
	}
	if m.Browse != fbgrid.BrowseGrid || m.Pack != "" || m.Shelf != ShelfAll {
		t.Fatalf("start stole browse=%s pack=%q shelf=%q", m.Browse, m.Pack, m.Shelf)
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseGrid || !m.SearchOpen {
		t.Fatalf("y stole browse=%s open=%v", m.Browse, m.SearchOpen)
	}
	pressNamed(&m, "x", now)
	if m.Pack != "" || !m.SearchOpen {
		t.Fatalf("x stole pack=%q", m.Pack)
	}
	pressNamed(&m, "select", now)
	if m.Shelf != ShelfAll || !m.SearchOpen {
		t.Fatalf("select stole shelf=%q", m.Shelf)
	}
	pressNamed(&m, "l", now)
	if m.Shelf != ShelfAll {
		t.Fatalf("L stole shelf %q", m.Shelf)
	}
	focus := m.Focus
	pressNamed(&m, "dpad-right", now)
	if m.Focus != focus {
		t.Fatalf("osk dpad stole catalog focus %d", m.Focus)
	}
}

func TestSearchFiltersNameAndClearLogoFallback(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(append(mixedCatalog(), tenfoot.Game{ID: "logo-md", Title: "", System: "megadrive", Launchable: true}))
	now := time.Unix(1, 0)
	pressNamed(&m, "start", now)
	typeOSK(&m, "sonic", now)
	if len(m.Games) != 1 || m.Games[0].ID != "sonic" {
		t.Fatalf("name filter %v", ids(m.Games))
	}
	pressNamed(&m, "b", now)
	typeOSK(&m, "megadrive", now)
	if len(m.Games) != 1 || m.Games[0].ID != "logo-md" {
		t.Fatalf("logo fallback %v", ids(m.Games))
	}
}

func TestEmptyQueryRestoresShelfAndExitRestoresFocus(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-right", now)
	if m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("setup %q", m.Games[m.Focus].ID)
	}
	pressNamed(&m, "start", now)
	typeOSK(&m, "zelda", now)
	if len(m.Games) != 1 || m.Games[0].ID != "zelda" {
		t.Fatalf("zelda %v", ids(m.Games))
	}
	pressNamed(&m, "b", now)
	if m.SearchQuery != "" || !m.SearchOpen || len(m.Games) != 6 {
		t.Fatalf("clear q=%q open=%v n=%d", m.SearchQuery, m.SearchOpen, len(m.Games))
	}
	if m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("clear focus %q", m.Games[m.Focus].ID)
	}
	pressNamed(&m, "b", now)
	if m.SearchOpen || m.SearchTag() != "" {
		t.Fatalf("exit open=%v tag=%q", m.SearchOpen, m.SearchTag())
	}
	if m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("exit focus %q", m.Games[m.Focus].ID)
	}
}

func TestNoMatchesIsEmptyAndADoesNotLaunch(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "start", now)
	typeOSK(&m, "zzzz", now)
	if len(m.Games) != 0 || m.SearchTag() != "SEARCH" {
		t.Fatalf("miss n=%d tag=%q", len(m.Games), m.SearchTag())
	}
	if !m.FocusSearchKey("done") {
		t.Fatal("done")
	}
	if action := pressNamed(&m, "a", now); action != "" {
		t.Fatalf("done launched %q", action)
	}
	if m.SearchOpen || len(m.Games) != 0 {
		t.Fatalf("done open=%v n=%d", m.SearchOpen, len(m.Games))
	}
	if action := pressNamed(&m, "a", now); action != "" {
		t.Fatalf("empty A launched %q", action)
	}
}

func TestDoneKeepsFilterAndDpadABMatchBrowse(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "start", now)
	typeOSK(&m, "s", now)
	// Sonic, Streets, plus blocked-md? "Blocked" contains no s... Streets, Sonic. SNES Mario? no. Pong no. Zelda no.
	// "s" matches Sonic, Streets. blocked-md Title "Blocked" has no s? "Blocked" has no s. Wait "Streets" and "Sonic".
	if !m.FocusSearchKey("done") {
		t.Fatal("done")
	}
	pressNamed(&m, "a", now)
	if m.SearchOpen || foldSearch(m.SearchQuery) != "s" {
		t.Fatalf("done open=%v q=%q", m.SearchOpen, m.SearchQuery)
	}
	if len(m.Games) < 2 {
		t.Fatalf("filter n=%d %v", len(m.Games), ids(m.Games))
	}
	origin := m.Focus
	pressNamed(&m, "dpad-right", now)
	if m.Focus != origin+1 && m.Focus != origin {
		t.Fatalf("dpad focus %d from %d", m.Focus, origin)
	}
	if action := pressNamed(&m, "a", now); action != "launch" {
		t.Fatalf("filtered A %q", action)
	}
	if action := pressNamed(&m, "b", now); action != "" || !m.DetailOpen {
		t.Fatalf("filtered B action=%q detail=%v", action, m.DetailOpen)
	}
}

func TestStartOnWheelEntersShelfSearch(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "r", now)
	pressNamed(&m, "r", now)
	if m.Shelf != "megadrive" || !m.WheelOpen {
		t.Fatalf("wheel shelf=%q open=%v", m.Shelf, m.WheelOpen)
	}
	pressNamed(&m, "start", now)
	if m.WheelOpen || !m.SearchOpen || m.Shelf != "megadrive" {
		t.Fatalf("start wheel=%v search=%v shelf=%q", m.WheelOpen, m.SearchOpen, m.Shelf)
	}
}

func TestWheelBackClearsCommittedSearch(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "r", now)
	pressNamed(&m, "r", now)
	pressNamed(&m, "start", now)
	typeOSK(&m, "sonic", now)
	if !m.FocusSearchKey("done") {
		t.Fatal("done")
	}
	pressNamed(&m, "a", now)
	if m.SearchOpen || foldSearch(m.SearchQuery) != "sonic" || len(m.Games) != 1 {
		t.Fatalf("committed open=%v q=%q n=%d", m.SearchOpen, m.SearchQuery, len(m.Games))
	}
	if action := pressNamed(&m, "b", now); action != "" || !m.WheelOpen {
		t.Fatalf("back action=%q wheel=%v", action, m.WheelOpen)
	}
	if m.SearchOpen || m.SearchQuery != "" || m.SearchTag() != "" {
		t.Fatalf("wheel kept search open=%v q=%q tag=%q", m.SearchOpen, m.SearchQuery, m.SearchTag())
	}
	if m.WheelStats() != "3 games" {
		t.Fatalf("wheel stats %q", m.WheelStats())
	}
	pressNamed(&m, "r", now)
	pressNamed(&m, "a", now)
	if m.WheelOpen || m.Shelf != "snes" || len(m.Games) != 2 || m.SearchTag() != "" {
		t.Fatalf("snes wheel=%v shelf=%q n=%d tag=%q", m.WheelOpen, m.Shelf, len(m.Games), m.SearchTag())
	}
}

func TestCommittedSearchHidesStripAndLastRowOpensDetail(t *testing.T) {
	now := time.Unix(1, 0)
	miss := Model{Connected: true, TargetReady: true}
	miss.SetCatalog(mixedCatalog())
	miss.SetStrip([]tenfoot.Game{{ID: "recent", Title: "Recent", Launchable: true}}, "Recent")
	pressNamed(&miss, "start", now)
	typeOSK(&miss, "zzzz", now)
	if !miss.FocusSearchKey("done") {
		t.Fatal("done")
	}
	pressNamed(&miss, "a", now)
	if miss.SearchOpen || len(miss.Games) != 0 || miss.SearchTag() != "SEARCH" {
		t.Fatalf("miss open=%v n=%d tag=%q", miss.SearchOpen, len(miss.Games), miss.SearchTag())
	}
	pressNamed(&miss, "dpad-down", now)
	if miss.StripActive || miss.DetailOpen {
		t.Fatalf("empty miss strip=%v detail=%v", miss.StripActive, miss.DetailOpen)
	}

	hit := Model{Connected: true, TargetReady: true}
	hit.SetCatalog(mixedCatalog())
	hit.SetStrip([]tenfoot.Game{{ID: "recent", Title: "Recent", Launchable: true}}, "Recent")
	pressNamed(&hit, "start", now)
	typeOSK(&hit, "s", now)
	if !hit.FocusSearchKey("done") {
		t.Fatal("done hit")
	}
	pressNamed(&hit, "a", now)
	if hit.SearchOpen || foldSearch(hit.SearchQuery) != "s" || len(hit.Games) < 2 {
		t.Fatalf("hit open=%v q=%q n=%d", hit.SearchOpen, hit.SearchQuery, len(hit.Games))
	}
	hit.Focus = len(hit.Games) - 1
	pressNamed(&hit, "dpad-down", now)
	if hit.StripActive || !hit.DetailOpen {
		t.Fatalf("filtered down strip=%v detail=%v", hit.StripActive, hit.DetailOpen)
	}
}

func TestSearchDoesNotArmAttractOrStealStop(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(10, 0)
	m.SetAttractIdle(time.Second)
	pressNamed(&m, "start", now)
	m.Tick(now.Add(2 * time.Second))
	if m.AttractActive {
		t.Fatal("attract armed while search open")
	}
	m.Session.State = "active"
	m.SearchOpen = false
	if action := pressNamed(&m, "start", now); action != "" || m.SearchOpen {
		t.Fatalf("start during game action=%q open=%v", action, m.SearchOpen)
	}
}

func TestSelectStartStopsWhileSearchOpen(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, SearchOpen: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(10, 0)
	m.Session.State = "active"
	pressNamed(&m, "select", now)
	pressNamed(&m, "start", now)
	if !m.SearchOpen {
		t.Fatal("start closed search during game")
	}
	if action := m.Tick(now.Add(time.Second)); action != "stop" {
		t.Fatalf("stop %q", action)
	}
}

func TestYAndXWorkAfterSearchCloses(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "start", now)
	pressNamed(&m, "b", now)
	if m.SearchOpen {
		t.Fatal("empty B should close")
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseCoverflow {
		t.Fatalf("y after search %s", m.Browse)
	}
	pressNamed(&m, "x", now)
	if m.Pack != theme.PackNeon {
		t.Fatalf("x after search %s", m.Pack)
	}
}

func TestSearchHaystackUsesTitleThenSystem(t *testing.T) {
	if got := SearchHaystack(tenfoot.Game{Title: "Sonic", System: "megadrive"}); got != "Sonic" {
		t.Fatalf("title %q", got)
	}
	if got := SearchHaystack(tenfoot.Game{Title: "  ", System: "snes"}); got != "snes" {
		t.Fatalf("system %q", got)
	}
	if got := SearchHaystack(tenfoot.Game{}); got != untitledSearchLabel {
		t.Fatalf("untitled %q", got)
	}
}

func TestGridHintUsesKitOSKWhileSearchOpen(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, fromWheel: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	if got := m.GridHint(); got != "A play | B platforms | L/R | Y flow" {
		t.Fatalf("browse %q", got)
	}
	pressNamed(&m, "start", now)
	if got := m.GridHint(); got != tenfoot.OSKKitHint(0) {
		t.Fatalf("osk %q", got)
	}
}

func typeOSK(m *Model, text string, now time.Time) {
	for _, r := range text {
		id := "char-" + string(r)
		if r == ' ' {
			id = "space"
		}
		if !m.FocusSearchKey(id) {
			panic("missing key " + id)
		}
		pressNamed(m, "a", now)
	}
}
