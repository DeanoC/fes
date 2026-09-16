package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"testing"
	"time"
)

func TestComposeStripLabelsAndDedupes(t *testing.T) {
	sonic := hostclient.Game{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true}
	mario := hostclient.Game{ID: "mario", Title: "Mario", System: "snes", Launchable: true, Favorite: true}
	pong := hostclient.Game{ID: "pong", Title: "Pong", System: "pong", Launchable: true, Favorite: true}
	games, label := ComposeStrip([]hostclient.Game{sonic, mario}, []hostclient.Game{mario, pong})
	if label != "Recent / Favorites" || len(games) != 3 {
		t.Fatalf("mixed %q n=%d", label, len(games))
	}
	if games[0].ID != "sonic" || games[1].ID != "mario" || games[2].ID != "pong" {
		t.Fatalf("order %v", ids(games))
	}
	games, label = ComposeStrip([]hostclient.Game{sonic}, nil)
	if label != "Recent" || len(games) != 1 {
		t.Fatalf("recent %q n=%d", label, len(games))
	}
	games, label = ComposeStrip(nil, []hostclient.Game{mario})
	if label != "Favorites" || len(games) != 1 {
		t.Fatalf("favorites %q n=%d", label, len(games))
	}
	games, label = ComposeStrip(nil, nil)
	if label != "" || len(games) != 0 {
		t.Fatalf("empty %q n=%d", label, len(games))
	}
	many := make([]hostclient.Game, stripMax+3)
	for i := range many {
		many[i] = hostclient.Game{ID: "g" + string(rune('a'+i))}
	}
	games, _ = ComposeStrip(many, many)
	if len(games) != stripMax {
		t.Fatalf("cap %d", len(games))
	}
}

func TestDownEntersStripInsteadOfDetail(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(25)}
	now := time.Unix(1, 0)
	m.SetStrip([]hostclient.Game{
		{ID: "recent-1", Title: "Recent 1", System: "snes", Launchable: true},
		{ID: "recent-2", Title: "Recent 2", System: "megadrive", Launchable: true},
	}, "Recent")
	m.Focus = 24
	if action := pressNamed(&m, "dpad-down", now); action != "" || m.DetailOpen || !m.StripActive || m.Focus != 24 {
		t.Fatalf("enter strip action=%q detail=%v strip=%v focus=%d", action, m.DetailOpen, m.StripActive, m.Focus)
	}
	if action := pressNamed(&m, "dpad-right", now); action != "" || m.StripFocus != 1 {
		t.Fatalf("strip right action=%q focus=%d", action, m.StripFocus)
	}
	if action := pressNamed(&m, "dpad-right", now); action != "" || m.StripFocus != 1 {
		t.Fatalf("strip clamp action=%q focus=%d", action, m.StripFocus)
	}
	if action := pressNamed(&m, "a", now); action != "" || !m.DetailOpen {
		t.Fatalf("strip A action=%q detail=%v", action, m.DetailOpen)
	}
	game, ok := m.FocusedGame()
	if !ok || game.ID != "recent-2" {
		t.Fatalf("detail game %+v ok=%v", game, ok)
	}
	if id := m.consumeLaunchID(); id != "recent-2" {
		t.Fatalf("launch id %q", id)
	}
	m.launchID = ""
	if action := pressNamed(&m, "b", now); action != "" || m.DetailOpen || !m.StripActive {
		t.Fatalf("detail B action=%q detail=%v strip=%v", action, m.DetailOpen, m.StripActive)
	}
	if action := pressNamed(&m, "b", now); action != "" || m.StripActive || m.Focus != 24 {
		t.Fatalf("strip B action=%q strip=%v focus=%d", action, m.StripActive, m.Focus)
	}
}

func TestStripUpReturnsToGridAndEmptyHides(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	m.SetStrip([]hostclient.Game{{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true}}, "Recent")
	m.Focus = len(m.Games) - 1
	pressNamed(&m, "dpad-down", now)
	if !m.StripActive {
		t.Fatal("expected strip")
	}
	if action := pressNamed(&m, "dpad-up", now); action != "" || m.StripActive {
		t.Fatalf("up action=%q strip=%v", action, m.StripActive)
	}
	m.SetStrip(nil, "Recent")
	if m.StripActive || len(m.Strip) != 0 || m.StripLabel != "" {
		t.Fatalf("hide active=%v n=%d label=%q", m.StripActive, len(m.Strip), m.StripLabel)
	}
	m.Focus = len(m.Games) - 1
	pressNamed(&m, "dpad-down", now)
	if !m.DetailOpen || m.StripActive {
		t.Fatalf("empty last-row down detail=%v strip=%v", m.DetailOpen, m.StripActive)
	}
}

func TestGridAStillLaunchesWhenStripVisible(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetStrip([]hostclient.Game{{ID: "recent", Title: "Recent", System: "snes", Launchable: true}}, "Recent")
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-right", now)
	if action := pressNamed(&m, "a", now); action != "launch" || m.StripActive || m.DetailOpen {
		t.Fatalf("grid A action=%q strip=%v detail=%v", action, m.StripActive, m.DetailOpen)
	}
}

func TestWheelBLeavesStrip(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, WheelOpen: true}
	m.SetCatalog(mixedCatalog())
	m.SetStrip([]hostclient.Game{{ID: "recent", Title: "Recent", System: "snes", Launchable: true}}, "Recent")
	now := time.Unix(1, 0)
	pressNamed(&m, "a", now)
	if m.WheelOpen {
		t.Fatal("did not enter grid")
	}
	m.Focus = len(m.Games) - 1
	pressNamed(&m, "dpad-down", now)
	if !m.StripActive {
		t.Fatal("expected strip")
	}
	pressNamed(&m, "b", now)
	if m.StripActive || m.WheelOpen {
		t.Fatalf("first B strip=%v wheel=%v", m.StripActive, m.WheelOpen)
	}
	pressNamed(&m, "b", now)
	if !m.WheelOpen || m.StripActive {
		t.Fatalf("second B wheel=%v strip=%v", m.WheelOpen, m.StripActive)
	}
}
