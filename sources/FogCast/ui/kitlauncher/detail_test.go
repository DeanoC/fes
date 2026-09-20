package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"strings"
	"testing"
	"time"
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
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
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
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{stillItem("mario", "Mario", handleAA())}})
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
	m.ApplyPresentation("pong", hostclient.Presentation{
		GameID: "pong",
		State:  "ready",
		Presentation: &hostclient.PresentationInfo{
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

func TestFocusLogoHandleAndDetailPrefetchIncludesLogo(t *testing.T) {
	logo := strings.Repeat("ee", 32)
	cover := strings.Repeat("ff", 32)
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-right", now)
	if m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("focus %s", m.Games[m.Focus].ID)
	}
	m.ApplyPresentation("sonic", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{LogoID: logo, CoverArtworkID: cover},
	})
	if got := m.FocusLogoHandle(); got != logo {
		t.Fatalf("logo %q", got)
	}
	handles := m.DetailPrefetchHandles()
	foundLogo, foundCover := false, false
	for _, h := range handles {
		if h == logo {
			foundLogo = true
		}
		if h == cover {
			foundCover = true
		}
	}
	if !foundLogo || !foundCover {
		t.Fatalf("prefetch %#v", handles)
	}
}

func TestFocusMarqueeHandleAndDetailPrefetchIncludesMarquee(t *testing.T) {
	marquee := strings.Repeat("aa", 32)
	cover := strings.Repeat("bb", 32)
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-right", now)
	if m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("focus %s", m.Games[m.Focus].ID)
	}
	m.ApplyPresentation("sonic", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{MarqueeID: marquee, CoverArtworkID: cover},
	})
	if got := m.FocusMarqueeHandle(); got != marquee {
		t.Fatalf("marquee %q", got)
	}
	handles := m.DetailPrefetchHandles()
	foundMarquee, foundCover := false, false
	for _, h := range handles {
		if h == marquee {
			foundMarquee = true
		}
		if h == cover {
			foundCover = true
		}
	}
	if !foundMarquee || !foundCover {
		t.Fatalf("prefetch %#v", handles)
	}
	m.ApplyPresentation("sonic", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{CoverArtworkID: cover},
	})
	if got := m.FocusMarqueeHandle(); got != "" {
		t.Fatalf("absent marquee %q", got)
	}
}

func TestFocusBox3DHandleAndDetailPrefetchIncludesBox3D(t *testing.T) {
	box := strings.Repeat("aa", 32)
	cover := strings.Repeat("bb", 32)
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	now := time.Unix(1, 0)
	pressNamed(&m, "dpad-right", now)
	if m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("focus %s", m.Games[m.Focus].ID)
	}
	m.ApplyPresentation("sonic", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{Box3DID: box, CoverArtworkID: cover},
	})
	if got := m.FocusBox3DHandle(); got != box {
		t.Fatalf("box3d %q", got)
	}
	handles := m.DetailPrefetchHandles()
	foundBox, foundCover := false, false
	for _, h := range handles {
		if h == box {
			foundBox = true
		}
		if h == cover {
			foundCover = true
		}
	}
	if !foundBox || !foundCover {
		t.Fatalf("prefetch %#v", handles)
	}
	m.ApplyPresentation("sonic", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{CoverArtworkID: cover},
	})
	if got := m.FocusBox3DHandle(); got != "" {
		t.Fatalf("absent box3d %q", got)
	}
}

func TestFocusDetailSurfacesRegionPlayersAndSummary(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	games := mixedCatalog()
	games[1].Region = "usa"
	games[1].Year = "1990"
	games[1].Genre = "Action"
	m.SetCatalog(games)
	now := time.Now()
	pressNamed(&m, "dpad-right", now)
	if m.Games[m.Focus].ID != "sonic" {
		t.Fatalf("focus %s", m.Games[m.Focus].ID)
	}
	d := m.FocusDetail()
	if d.Region != "USA" || d.Year != "1990" || d.Genre != "Action" || d.Summary != "" || d.Players != "" {
		t.Fatalf("catalog-only %+v", d)
	}
	if !strings.Contains(d.MetaFacts(), "USA") || strings.Contains(d.MetaFacts(), "1-2") {
		t.Fatalf("catalog facts %q", d.MetaFacts())
	}
	m.ApplyPresentation("sonic", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{
			Year:    "1991",
			Genre:   "Platform",
			Studio:  "SEGA",
			Players: "1-2",
			Summary: "A blue hedgehog dashes through Green Hill Zone.",
		},
	})
	d = m.FocusDetail()
	if d.Year != "1991" || d.Genre != "Platform" || d.Studio != "SEGA" || d.Players != "1-2" || d.Region != "USA" {
		t.Fatalf("ready %+v", d)
	}
	if d.Summary != "A blue hedgehog dashes through Green Hill Zone." {
		t.Fatalf("summary %q", d.Summary)
	}
	facts := d.MetaFacts()
	for _, part := range []string{"1991", "Platform", "SEGA", "1-2", "USA"} {
		if !strings.Contains(facts, part) {
			t.Fatalf("facts missing %q: %q", part, facts)
		}
	}
}

func TestDetailHintMentionsShotsWhenPresent(t *testing.T) {
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog(mixedCatalog())
	if !strings.Contains(m.DetailHint(), "B back") || strings.Contains(m.DetailHint(), "shots") {
		t.Fatalf("empty hint %q", m.DetailHint())
	}
	m.ApplyPresentation("pong", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{ScreenshotIDs: []string{handleAA(), handleBB()}},
	})
	if !strings.Contains(m.DetailHint(), "L/R shots") {
		t.Fatalf("shot hint %q", m.DetailHint())
	}
}

func TestDetailVideoPreviewCyclesStillsAndFallsBackToPoster(t *testing.T) {
	aa := handleAA()
	bb := handleBB()
	cover := strings.Repeat("cc", 32)
	backdrop := strings.Repeat("dd", 32)
	video := strings.Repeat("ee", 32)
	m := Model{Connected: true, TargetReady: true}
	games := mixedCatalog()
	games[0].Cover = cover
	m.SetCatalog(games)
	now := time.Unix(1, 0)
	pressNamed(&m, "b", now)
	if !m.DetailOpen {
		t.Fatal("expected detail")
	}

	m.ApplyPresentation("pong", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{ScreenshotIDs: []string{aa, bb}},
	})
	if m.HasVideoPreview() || m.ShotHandle() != aa {
		t.Fatalf("still-only video=%v shot=%q", m.HasVideoPreview(), m.ShotHandle())
	}
	m.Tick(now.Add(3 * time.Second))
	if m.ShotHandle() != aa {
		t.Fatalf("still-only auto-cycled to %q", m.ShotHandle())
	}
	if strings.Contains(m.DetailHint(), "preview") {
		t.Fatalf("still-only hint %q", m.DetailHint())
	}

	m.ApplyPresentation("pong", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{
			VideoID:           video,
			ScreenshotIDs:     []string{aa, bb},
			BackdropArtworkID: backdrop,
		},
	})
	if !m.HasVideoPreview() || m.FocusVideoHandle() != video {
		t.Fatalf("video handle %q", m.FocusVideoHandle())
	}
	if m.ShotHandle() != aa {
		t.Fatalf("video shot0 %q", m.ShotHandle())
	}
	if !strings.Contains(m.DetailHint(), "L/R preview") {
		t.Fatalf("video hint %q", m.DetailHint())
	}
	handles := m.PreviewHandles()
	if len(handles) != 4 || handles[0] != aa || handles[1] != bb || handles[2] != backdrop || handles[3] != cover {
		t.Fatalf("preview handles %#v", handles)
	}
	m.Tick(now)
	m.Tick(now.Add(defaultPreviewCycle + time.Millisecond))
	if m.ShotHandle() != bb {
		t.Fatalf("auto-cycle %q want %q", m.ShotHandle(), bb)
	}
	pressNamed(&m, "r", now.Add(2*defaultPreviewCycle))
	if m.ShotHandle() != backdrop {
		t.Fatalf("manual step %q", m.ShotHandle())
	}
	m.Tick(now.Add(2*defaultPreviewCycle + time.Millisecond))
	if m.ShotHandle() != backdrop {
		t.Fatalf("manual step was overwritten %q", m.ShotHandle())
	}

	m.ApplyPresentation("pong", hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{VideoID: video, CoverArtworkID: cover},
	})
	if m.ShotHandle() != cover {
		t.Fatalf("poster %q", m.ShotHandle())
	}
	m.Tick(now.Add(5 * time.Second))
	if m.ShotHandle() != cover {
		t.Fatalf("single poster cycled %q", m.ShotHandle())
	}

	prefetch := m.DetailPrefetchHandles()
	foundVideoStill := false
	for _, h := range prefetch {
		if h == cover {
			foundVideoStill = true
		}
	}
	if !foundVideoStill {
		t.Fatalf("prefetch %#v", prefetch)
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
	m.SetCatalog([]hostclient.Game{{ID: "other", Title: "Other", System: "snes", Launchable: true}})
	if m.DetailOpen {
		t.Fatal("reload kept detail for missing title")
	}
}
