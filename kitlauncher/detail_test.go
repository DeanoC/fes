package kitlauncher

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/remoteinput"
)

func pressNamed(m *Model, name string, now time.Time) string {
	e, err := remoteinput.NormalizeGamepad(name, true)
	if err != nil {
		panic(err)
	}
	return m.Input(e, now)
}

func TestEastOpensDetailWithoutStealingGridA(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-right", now)
	if m.Focus != 1 || m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("setup focus=%d", m.Focus)
	}
	if action := pressNamed(&m, "a", now); action != "launch" {
		t.Fatalf("grid A %q", action)
	}
	if m.DetailOpen {
		t.Fatal("A opened detail")
	}
	if action := pressNamed(&m, "b", now); action != "" {
		t.Fatalf("B launched %q", action)
	}
	if !m.DetailOpen {
		t.Fatal("B did not open detail")
	}
	if m.Focus != 1 || m.Games[m.Focus].ID != "sonic" || m.Shelf != ShelfAll {
		t.Fatalf("open moved focus shelf=%q focus=%d id=%s", m.Shelf, m.Focus, m.Games[m.Focus].ID)
	}
}

func TestDownOpensDetailOnlyWhenFocusCannotMove(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(25)}
	now := time.Unix(1, 0)
	if action := pressNamed(&m, "dpad-down", now); action != "" || m.DetailOpen || m.Focus != 4 {
		t.Fatalf("middle down focus=%d open=%v action=%q", m.Focus, m.DetailOpen, action)
	}
	m.Focus = 24
	if action := pressNamed(&m, "dpad-down", now); action != "" || !m.DetailOpen || m.Focus != 24 {
		t.Fatalf("last-row down focus=%d open=%v action=%q", m.Focus, m.DetailOpen, action)
	}
}

func TestDetailClosePreservesFocusAndShelf(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "r", now)
	pressNamed(&m, "r", now)
	pressNamed(&m, "dpad-right", now)
	if m.Shelf != "megadrive" || m.Focus != 1 || m.Games[m.Focus].ID != "streets" {
		t.Fatalf("setup shelf=%q focus=%d", m.Shelf, m.Focus)
	}
	pressNamed(&m, "b", now)
	if !m.DetailOpen {
		t.Fatal("expected detail")
	}
	pressNamed(&m, "b", now)
	if m.DetailOpen {
		t.Fatal("B did not close")
	}
	if m.Shelf != "megadrive" || m.Focus != 1 || m.Games[m.Focus].ID != "streets" {
		t.Fatalf("B close moved shelf=%q focus=%d id=%s", m.Shelf, m.Focus, m.Games[m.Focus].ID)
	}
	pressNamed(&m, "b", now)
	pressNamed(&m, "dpad-up", now)
	if m.DetailOpen {
		t.Fatal("Up did not close")
	}
	if m.Shelf != "megadrive" || m.Focus != 1 || m.Games[m.Focus].ID != "streets" {
		t.Fatalf("Up close moved shelf=%q focus=%d", m.Shelf, m.Focus)
	}
}

func TestDetailALaunchesFocusedTitle(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-right", now)
	pressNamed(&m, "b", now)
	if !m.DetailOpen {
		t.Fatal("expected detail")
	}
	if action := pressNamed(&m, "a", now); action != "launch" {
		t.Fatalf("detail A %q", action)
	}
	if id := m.consumeLaunchID(); id != "sonic" {
		t.Fatalf("launch id %q", id)
	}
	if m.Focus != 1 || m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("launch moved focus %d", m.Focus)
	}
}

func TestAttractDoesNotArmWhileDetailOpen(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetAttractIdle(10 * time.Millisecond)
	m.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{stillItem("mario", "Mario", handleAA())}})
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	if !m.DetailOpen {
		t.Fatal("expected detail")
	}
	m.Tick(now)
	m.Tick(now.Add(time.Second))
	if m.AttractActive {
		t.Fatal("attract armed while detail open")
	}
	pressNamed(&m, "b", now.Add(time.Second))
	m.Tick(now.Add(time.Second + 5*time.Millisecond))
	m.Tick(now.Add(time.Second + 20*time.Millisecond))
	if !m.AttractActive {
		t.Fatal("attract should arm after close")
	}
}

func TestOpeningDetailNotesActivity(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetAttractIdle(50 * time.Millisecond)
	m.SetAttractPlaylist(tenfoot.AttractPlaylist{Items: []tenfoot.AttractItem{stillItem("mario", "Mario", handleAA())}})
	t0 := time.Unix(1, 0)
	m.Tick(t0)
	pressNamed(&m, "b", t0.Add(40*time.Millisecond))
	m.Tick(t0.Add(60 * time.Millisecond))
	if m.AttractActive {
		t.Fatal("opening detail did not reset idle")
	}
	if !m.DetailOpen {
		t.Fatal("expected detail")
	}
}

func TestDetailShouldersCycleScreenshots(t *testing.T) {
	aa := handleAA()
	bb := handleBB()
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	m.ApplyPresentation("pong", tenfoot.Presentation{
		GameID: "pong",
		State:  "ready",
		Presentation: &tenfoot.PresentationInfo{
			Studio:        "Atari",
			Year:          "1972",
			ScreenshotIDs: []string{aa, bb},
		},
	})
	d := m.FocusDetail()
	if d.Studio != "Atari" || d.Year != "1972" || len(d.ScreenshotIDs) != 2 {
		t.Fatalf("detail %+v", d)
	}
	if m.ShotHandle() != aa {
		t.Fatalf("shot0 %q", m.ShotHandle())
	}
	pressNamed(&m, "r", now)
	if m.ShotHandle() != bb || m.ShotIndex() != 1 {
		t.Fatalf("R shot %q idx=%d", m.ShotHandle(), m.ShotIndex())
	}
	pressNamed(&m, "l", now)
	if m.ShotHandle() != aa {
		t.Fatalf("L shot %q", m.ShotHandle())
	}
	if m.Shelf != ShelfAll {
		t.Fatalf("shoulder cycled shelf %q", m.Shelf)
	}
	pressNamed(&m, "dpad-right", now)
	if m.ShotHandle() != bb {
		t.Fatalf("dpad-right shot %q", m.ShotHandle())
	}
}

func TestDetailHintMentionsShotsWhenPresent(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	if !strings.Contains(m.DetailHint(), "B back") || strings.Contains(m.DetailHint(), "shots") {
		t.Fatalf("empty hint %q", m.DetailHint())
	}
	m.ApplyPresentation("pong", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{ScreenshotIDs: []string{handleAA(), handleBB()}},
	})
	if !strings.Contains(m.DetailHint(), "L/R shots") {
		t.Fatalf("shot hint %q", m.DetailHint())
	}
}

func TestSetCatalogClosesDetailWhenFocusLeaves(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	if !m.DetailOpen {
		t.Fatal("expected detail")
	}
	m.SetCatalog([]tenfoot.Game{{ID: "other", Title: "Other", System: "snes", Launchable: true}})
	if m.DetailOpen {
		t.Fatal("reload kept detail for missing title")
	}
}
