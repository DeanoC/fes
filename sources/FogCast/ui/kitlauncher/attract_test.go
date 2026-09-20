package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"testing"
	"time"
)

func stillItem(id, title, handle string) hostclient.AttractItem {
	return hostclient.AttractItem{
		GameID:     id,
		Title:      title,
		Platform:   "snes",
		Backdrop:   handle,
		Launchable: true,
	}
}

func handleAA() string { return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }
func handleBB() string { return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }

func TestAttractArmsAfterIdle(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetAttractPlaylist(hostclient.AttractPlaylist{
		IdleSeconds: 1,
		Items:       []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())},
	})
	m.SetAttractIdle(20 * time.Millisecond)
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	if m.AttractActive {
		t.Fatal("armed before idle")
	}
	m.Tick(t0.Add(10 * time.Millisecond))
	if m.AttractActive {
		t.Fatal("armed early")
	}
	m.Tick(t0.Add(25 * time.Millisecond))
	if !m.AttractActive {
		t.Fatal("idle did not arm attract")
	}
	view := m.AttractView(t0.Add(25 * time.Millisecond))
	if view.Title != "Mario" || view.Handle != handleAA() || view.Empty {
		t.Fatalf("view %+v", view)
	}
}

func TestAttractDoesNotArmWhileBusyOrSession(t *testing.T) {
	t0 := time.Unix(0, 0)
	for _, state := range []string{"active", "launching", "stopping", "failed"} {
		m := Model{Connected: true, TargetReady: true, Session: Session{State: state}}
		m.SetAttractIdle(time.Millisecond)
		m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
		m.Tick(t0)
		m.Tick(t0.Add(time.Second))
		if m.AttractActive {
			t.Fatalf("armed while session %s", state)
		}
	}
	m := Model{Connected: true, TargetReady: true, Busy: true}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
	m.Tick(t0)
	m.Tick(t0.Add(time.Second))
	if m.AttractActive {
		t.Fatal("armed while busy")
	}
	m = Model{Connected: false, TargetReady: true}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
	m.Tick(t0)
	m.Tick(t0.Add(time.Second))
	if m.AttractActive {
		t.Fatal("armed while disconnected")
	}
}

func TestAttractDismissPreservesFocusAndShelf(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	r, _ := remoteinput.NormalizeGamepad("r", true)
	right, _ := remoteinput.NormalizeGamepad("dpad-right", true)
	t0 := time.Unix(0, 0)
	m.Input(r, t0)
	m.Input(r, t0)
	m.Input(right, t0)
	if m.Shelf != "megadrive" || m.Focus != 1 || m.Games[m.Focus].ID != "streets" {
		t.Fatalf("setup shelf=%q focus=%d", m.Shelf, m.Focus)
	}
	m.SetAttractIdle(10 * time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
	m.Tick(t0)
	m.Tick(t0.Add(20 * time.Millisecond))
	if !m.AttractActive {
		t.Fatal("expected attract")
	}
	m.Input(right, t0.Add(30*time.Millisecond))
	if m.AttractActive {
		t.Fatal("dpad did not dismiss")
	}
	if m.Shelf != "megadrive" || m.Focus != 1 || m.Games[m.Focus].ID != "streets" {
		t.Fatalf("focus moved shelf=%q focus=%d id=%s", m.Shelf, m.Focus, m.Games[m.Focus].ID)
	}
	m.Input(right, t0.Add(40*time.Millisecond))
	if m.Focus != 2 {
		t.Fatalf("nav after dismiss %d", m.Focus)
	}
}

func TestAttractEmptyPlaylistShowsIdlePanel(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: nil})
	m.SetAttractIdle(10 * time.Millisecond)
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	m.Tick(t0.Add(20 * time.Millisecond))
	if !m.AttractActive {
		t.Fatal("empty playlist should still enter idle panel")
	}
	view := m.AttractView(t0.Add(20 * time.Millisecond))
	if !view.Empty || view.Title != "FOGCAST" {
		t.Fatalf("empty view %+v", view)
	}
	b, _ := remoteinput.NormalizeGamepad("b", true)
	m.Input(b, t0.Add(30*time.Millisecond))
	if m.AttractActive {
		t.Fatal("empty panel did not dismiss")
	}
}

func TestAttractSkipsVideoOnlyRows(t *testing.T) {
	video := handleAA()
	cover := handleBB()
	m := Model{Connected: true, TargetReady: true}
	m.SetAttractPlaylist(hostclient.AttractPlaylist{
		Items: []hostclient.AttractItem{
			{GameID: "clip", Title: "Clip", Video: video, Launchable: true},
			stillItem("mario", "Mario", cover),
		},
	})
	m.SetAttractIdle(time.Millisecond)
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	m.Tick(t0.Add(5 * time.Millisecond))
	view := m.AttractView(t0.Add(5 * time.Millisecond))
	if view.Title != "Mario" || view.Handle != cover {
		t.Fatalf("video-only row leaked %+v", view)
	}
}

func TestAttractALaunchesCurrentItem(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.Focus = 0
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	m.Tick(t0.Add(5 * time.Millisecond))
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, t0.Add(10*time.Millisecond)); action != "launch" {
		t.Fatalf("A action %q", action)
	}
	if m.AttractActive {
		t.Fatal("A did not dismiss")
	}
	if m.Shelf != ShelfAll || m.Games[m.Focus].ID != "mario" {
		t.Fatalf("focus after A shelf=%q id=%s", m.Shelf, m.Games[m.Focus].ID)
	}
	if id := m.consumeLaunchID(); id != "mario" {
		t.Fatalf("launch id %q", id)
	}
}

func TestAttractAFromStripLaunchesAttractGame(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetStrip([]hostclient.Game{
		{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true},
	}, "Recent")
	m.Focus = len(m.Games) - 1
	now := time.Unix(1, 0)
	if action := pressNamed(&m, "dpad-down", now); action != "" || !m.StripActive {
		t.Fatalf("enter strip action=%q strip=%v", action, m.StripActive)
	}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
	t0 := time.Unix(2, 0)
	m.lastInput = t0
	m.Tick(t0)
	m.Tick(t0.Add(5 * time.Millisecond))
	if !m.AttractActive || !m.StripActive {
		t.Fatalf("attract=%v strip=%v", m.AttractActive, m.StripActive)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, t0.Add(10*time.Millisecond)); action != "launch" {
		t.Fatalf("A action %q", action)
	}
	if m.AttractActive || m.StripActive {
		t.Fatalf("after A attract=%v strip=%v", m.AttractActive, m.StripActive)
	}
	if m.Games[m.Focus].ID != "mario" {
		t.Fatalf("grid focus %s", m.Games[m.Focus].ID)
	}
	if id := m.consumeLaunchID(); id != "mario" {
		t.Fatalf("launch id %q", id)
	}
}

func TestAttractBFromStripReturnsToStrip(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetStrip([]hostclient.Game{
		{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true},
	}, "Recent")
	m.Focus = len(m.Games) - 1
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-down", now)
	if !m.StripActive {
		t.Fatal("expected strip")
	}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
	t0 := time.Unix(2, 0)
	m.lastInput = t0
	m.Tick(t0.Add(5 * time.Millisecond))
	if !m.AttractActive {
		t.Fatal("expected attract")
	}
	if action := pressNamed(&m, "b", t0.Add(10*time.Millisecond)); action != "" || m.AttractActive || !m.StripActive {
		t.Fatalf("B action=%q attract=%v strip=%v", action, m.AttractActive, m.StripActive)
	}
	if m.Games[m.Focus].ID != "zelda" {
		t.Fatalf("grid focus %s", m.Games[m.Focus].ID)
	}
}

func TestAttractADismissesUnknownGameID(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("missing", "Ghost", handleAA())}})
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	m.Tick(t0.Add(5 * time.Millisecond))
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, t0.Add(10*time.Millisecond)); action != "" {
		t.Fatalf("A action %q", action)
	}
	if m.AttractActive || m.launchID != "" {
		t.Fatalf("attract=%v pending launch=%q", m.AttractActive, m.launchID)
	}
	if m.Games[m.Focus].ID == "missing" {
		t.Fatal("unknown game mutated catalog focus")
	}
}

func TestAttractBDismissesWithoutLaunch(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	m.Tick(t0.Add(5 * time.Millisecond))
	b, _ := remoteinput.NormalizeGamepad("b", true)
	if action := m.Input(b, t0.Add(10*time.Millisecond)); action != "" {
		t.Fatalf("B launched %q", action)
	}
	if m.AttractActive {
		t.Fatal("B did not dismiss")
	}
	if m.Focus != 0 {
		t.Fatalf("focus %d", m.Focus)
	}
}

func TestAttractCycleAdvancesIndex(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractCycle(20 * time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{
		stillItem("mario", "Mario", handleAA()),
		stillItem("sonic", "Sonic", handleBB()),
	}})
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	enter := t0.Add(5 * time.Millisecond)
	m.Tick(enter)
	if m.AttractView(enter).Index != 0 || m.AttractView(enter).Title != "Mario" {
		t.Fatalf("origin %+v", m.AttractView(enter))
	}
	mid := enter.Add(15 * time.Millisecond)
	view := m.AttractView(mid)
	if view.FadeT <= 0 || view.NextHandle != handleBB() {
		t.Fatalf("expected fade toward next %+v", view)
	}
	m.Tick(enter.Add(25 * time.Millisecond))
	if m.AttractView(enter.Add(25*time.Millisecond)).Title != "Sonic" {
		t.Fatalf("cycle %+v", m.AttractView(enter.Add(25*time.Millisecond)))
	}
}

func TestAttractShoulderDoesNotCycleShelfWhileActive(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	m.Tick(t0.Add(5 * time.Millisecond))
	r, _ := remoteinput.NormalizeGamepad("r", true)
	m.Input(r, t0.Add(10*time.Millisecond))
	if m.AttractActive {
		t.Fatal("R did not dismiss")
	}
	if m.Shelf != ShelfAll {
		t.Fatalf("R cycled shelf while dismissing %q", m.Shelf)
	}
}

func TestAttractDoesNotBreakSelectStartStop(t *testing.T) {
	now := time.Now()
	m := Model{Session: Session{State: "active"}, Connected: true, TargetReady: true}
	m.SetAttractIdle(time.Millisecond)
	m.Tick(now)
	m.Tick(now.Add(time.Second))
	if m.AttractActive {
		t.Fatal("attract during play")
	}
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	startPress, _ := remoteinput.NormalizeGamepad("start", true)
	m.Input(selectPress, now)
	m.Input(startPress, now)
	if action := m.Tick(now.Add(time.Second)); action != "stop" {
		t.Fatalf("stop %q", action)
	}
}

func TestAttractPrefetchCurrentAndNext(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, AttractActive: true}
	m.attractItems = []hostclient.AttractItem{stillItem("a", "A", handleAA()), stillItem("b", "B", handleBB())}
	got := m.AttractPrefetchHandles()
	if len(got) != 2 || got[0] != handleAA() || got[1] != handleBB() {
		t.Fatalf("handles %v", got)
	}
}

func handleCC() string { return "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" }
func handleDD() string { return "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" }
func handleEE() string { return "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" }

func videoItem(id, title, video, backdrop, cover string) hostclient.AttractItem {
	return hostclient.AttractItem{
		GameID:     id,
		Title:      title,
		Platform:   "snes",
		Video:      video,
		Backdrop:   backdrop,
		Cover:      cover,
		Launchable: true,
	}
}

func TestAttractMotionCyclesStillsWhenVideoPresent(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractCycle(10 * time.Second)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{
		videoItem("mario", "Mario", handleEE(), handleAA(), handleBB()),
	}})
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	enter := t0.Add(5 * time.Millisecond)
	m.Tick(enter)
	view := m.AttractView(enter)
	if !view.Motion || view.Handle != handleAA() || view.Caption != "preview 1 / 2" {
		t.Fatalf("origin %+v", view)
	}
	m.Tick(enter.Add(2*time.Second + time.Millisecond))
	view = m.AttractView(enter.Add(2*time.Second + time.Millisecond))
	if view.Handle != handleBB() || view.ShotIndex != 1 || view.Caption != "preview 2 / 2" {
		t.Fatalf("cycle %+v", view)
	}
	if !view.Motion {
		t.Fatal("lost motion chrome")
	}
}

func TestAttractStillsFallbackHidesMotionChrome(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{
		stillItem("mario", "Mario", handleAA()),
	}})
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	m.Tick(t0.Add(5 * time.Millisecond))
	view := m.AttractView(t0.Add(5 * time.Millisecond))
	if view.Motion || view.Caption != "" || view.Handle != handleAA() || len(view.Wall) != 0 {
		t.Fatalf("stills grew motion chrome %+v", view)
	}
}

func TestAttractMarqueeStripWhenDistinctAndHidesWhenSame(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, AttractActive: true}
	m.attractItems = []hostclient.AttractItem{{
		GameID: "mario", Title: "Mario", Platform: "snes",
		Backdrop: handleAA(), Marquee: handleBB(), Launchable: true,
	}}
	view := m.AttractView(time.Unix(1, 0))
	if view.Handle != handleAA() || view.Marquee != handleBB() {
		t.Fatalf("distinct %+v", view)
	}
	got := m.AttractPrefetchHandles()
	found := false
	for _, h := range got {
		if h == handleBB() {
			found = true
		}
	}
	if !found {
		t.Fatalf("prefetch missing marquee %v", got)
	}

	m.attractItems[0].Marquee = handleAA()
	view = m.AttractView(time.Unix(1, 0))
	if view.Handle != handleAA() || view.Marquee != "" {
		t.Fatalf("same-handle strip %+v", view)
	}

	m.attractItems[0].Marquee = ""
	m.ApplyAttractPresentation("mario", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{MarqueeID: handleCC()},
	})
	view = m.AttractView(time.Unix(1, 0))
	if view.Marquee != handleCC() {
		t.Fatalf("presentation marquee %+v", view)
	}

	only := Model{Connected: true, TargetReady: true, AttractActive: true}
	only.attractItems = []hostclient.AttractItem{{
		GameID: "pong", Title: "Pong", Marquee: handleDD(), Launchable: true,
	}}
	view = only.AttractView(time.Unix(1, 0))
	if view.Handle != handleDD() || view.Marquee != "" {
		t.Fatalf("marquee-only still %+v", view)
	}
}

func TestAttractMotionUsesPresentationScreenshots(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, AttractActive: true}
	m.attractItems = []hostclient.AttractItem{videoItem("mario", "Mario", handleEE(), handleAA(), "")}
	shot := handleCC()
	m.ApplyAttractPresentation("mario", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{
			VideoID:       handleEE(),
			ScreenshotIDs: []string{shot, handleBB()},
		},
	})
	view := m.AttractView(time.Unix(1, 0))
	if !view.Motion || view.Handle != shot {
		t.Fatalf("presentation stills %+v", view)
	}
	got := m.AttractPrefetchHandles()
	if len(got) < 3 || got[0] != shot {
		t.Fatalf("prefetch %v", got)
	}
}

func TestAttractWallWhenFourTitlesIncludeVideo(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractCycle(10 * time.Second)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{
		videoItem("mario", "Mario", handleEE(), handleAA(), handleBB()),
		stillItem("sonic", "Sonic", handleCC()),
		stillItem("zelda", "Zelda", handleDD()),
		stillItem("pong", "Pong", handleAA()),
	}})
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	enter := t0.Add(5 * time.Millisecond)
	m.Tick(enter)
	view := m.AttractView(enter)
	if !view.Motion || len(view.Wall) != 4 {
		t.Fatalf("wall %+v", view)
	}
	if view.Wall[0].Handle != handleAA() || !view.Wall[0].Motion {
		t.Fatalf("tile0 %+v", view.Wall[0])
	}
	if view.Wall[1].Handle != handleCC() || view.Wall[1].Motion {
		t.Fatalf("tile1 claimed motion %+v", view.Wall[1])
	}
	if view.FadeT != 0 || view.NextHandle != "" {
		t.Fatalf("wall kept title fade %+v", view)
	}
	m.Tick(enter.Add(2*time.Second + time.Millisecond))
	view = m.AttractView(enter.Add(2*time.Second + time.Millisecond))
	if view.Wall[0].Handle != handleBB() {
		t.Fatalf("wall did not cycle current %+v", view.Wall[0])
	}
}

func TestAttractMotionALaunchesStagedGameFromStrip(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	m.SetStrip([]hostclient.Game{
		{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true},
	}, "Recent")
	m.Focus = len(m.Games) - 1
	now := time.Unix(1, 0)
	if action := pressNamed(&m, "dpad-down", now); action != "" || !m.StripActive {
		t.Fatalf("enter strip action=%q strip=%v", action, m.StripActive)
	}
	m.SetAttractIdle(time.Millisecond)
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{
		videoItem("mario", "Mario", handleEE(), handleAA(), handleBB()),
	}})
	t0 := time.Unix(2, 0)
	m.lastInput = t0
	m.Tick(t0)
	m.Tick(t0.Add(5 * time.Millisecond))
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, t0.Add(10*time.Millisecond)); action != "launch" {
		t.Fatalf("A action %q", action)
	}
	if m.AttractActive || m.StripActive {
		t.Fatalf("after A attract=%v strip=%v", m.AttractActive, m.StripActive)
	}
	if id := m.consumeLaunchID(); id != "mario" {
		t.Fatalf("launch id %q", id)
	}
}
