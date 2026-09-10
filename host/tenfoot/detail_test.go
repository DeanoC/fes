package tenfoot

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScreenshotHandlesClampSkipAndDedupe(t *testing.T) {
	t.Parallel()
	valid := []string{
		strings.Repeat("aa", 32),
		strings.Repeat("bb", 32),
		strings.Repeat("cc", 32),
	}
	ids := []string{
		valid[0],
		"not-a-handle",
		"",
		strings.ToUpper(valid[0]),
		valid[1],
		valid[2],
		strings.Repeat("dd", 32),
		strings.Repeat("ee", 32),
		strings.Repeat("ff", 32),
		strings.Repeat("11", 32),
		strings.Repeat("22", 32),
		strings.Repeat("33", 32),
	}
	got := screenshotHandles(ids)
	if len(got) != maxScreenshotHandles {
		t.Fatalf("len = %d want %d (%#v)", len(got), maxScreenshotHandles, got)
	}
	if got[0] != valid[0] || got[1] != valid[1] {
		t.Fatalf("got = %#v", got)
	}
	if screenshotHandles(nil) != nil {
		t.Fatal("empty should omit")
	}
	if screenshotHandles([]string{"nope", ""}) != nil {
		t.Fatal("invalid-only should omit")
	}
}

func TestCloseDetailDropsScreenshotInflight(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	app := NewApp(nil, 800, 600, 10)
	app.games = []Game{availableGame("snes-mario", "Mario", "snes")}
	app.grid.SetCount(1)
	app.details["snes-mario"] = FocusDetail{ScreenshotIDs: []string{handle}}
	app.detailOpen = true
	shotCtx, shotCancel := context.WithCancel(context.Background())
	app.shotCtx = shotCtx
	app.shotCancel = shotCancel
	key := screenshotWorkKey(handle)
	app.inflight[key] = workScreenshot
	gen := app.shotGen
	app.closeDetailLocked()
	if app.detailOpen {
		t.Fatal("detail still open")
	}
	if app.shotGen == gen {
		t.Fatal("shot gen should advance")
	}
	if _, busy := app.inflight[key]; busy {
		t.Fatal("closed pane should release screenshot inflight")
	}
	if shotCtx.Err() == nil {
		t.Fatal("closed pane should cancel screenshot context")
	}
	if app.shotCtx != nil || app.shotCancel != nil {
		t.Fatal("closed pane should drop screenshot context")
	}
}

func TestCloseDetailCancelsScreenshotHTTPAndUnblocksCoverWork(t *testing.T) {
	marioShots := make([]string, maxScreenshotHandles)
	for i := range marioShots {
		marioShots[i] = strings.Repeat(fmt.Sprintf("%02x", i+1), 32)
	}
	sonicCover := strings.Repeat("aa", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	coverImg, err := DecodeCover(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	var shotStarts atomic.Int64
	var coverGets atomic.Int64
	blocked := make(chan struct{})
	canceled := make(chan struct{}, maxInflight)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			mario := availableGame("snes-mario", "Mario", "snes")
			sonic := availableGame("snes-sonic", "Sonic", "snes")
			sonic.Cover = sonicCover
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{mario, sonic}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/presentation/games/")
			pres := Presentation{
				GameID: id,
				State:  "ready",
				Presentation: &PresentationInfo{
					Summary: id,
					Studio:  "Nintendo",
				},
				Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
			}
			if id == "snes-mario" {
				pres.Presentation.ScreenshotIDs = append([]string(nil), marioShots...)
			}
			if id == "snes-sonic" {
				pres.Presentation.CoverArtworkID = sonicCover
			}
			_ = json.NewEncoder(w).Encode(pres)
		case r.URL.Path == "/api/v1/presentation/artwork/"+sonicCover:
			coverGets.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/artwork/"):
			n := shotStarts.Add(1)
			if n == int64(coverWorkers) {
				select {
				case <-blocked:
				default:
					close(blocked)
				}
			}
			select {
			case <-r.Context().Done():
				select {
				case canceled <- struct{}{}:
				default:
				}
				return
			case <-time.After(8 * time.Second):
				http.Error(w, "stale screenshot GET was not canceled", http.StatusGatewayTimeout)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 2 && len(snap.FocusDetail.ScreenshotIDs) == len(marioShots)
	})
	app.mu.Lock()
	app.covers["snes-sonic"] = &coverSlot{phase: coverReady, handle: sonicCover, image: coverImg}
	app.openDetailLocked()
	app.mu.Unlock()
	if !app.Snapshot().Detail.Open {
		t.Fatal("detail should open")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		app.Tick(time.Now())
		select {
		case <-blocked:
			goto shotsStarted
		default:
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("screenshot GETs did not fill workers, started=%d", shotStarts.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
shotsStarted:
	for i := 0; i < 8; i++ {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	beforeClose := shotStarts.Load()
	app.HandleCommand(CmdBack, time.Now())
	if app.Snapshot().Detail.Open {
		t.Fatal("detail should close")
	}
	gotCancel := 0
	cancelDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(cancelDeadline) && gotCancel < coverWorkers {
		select {
		case <-canceled:
			gotCancel++
		default:
			app.Tick(time.Now())
			time.Sleep(5 * time.Millisecond)
		}
	}
	if gotCancel < coverWorkers {
		t.Fatalf("canceled %d screenshot GETs, want %d", gotCancel, coverWorkers)
	}
	if shotStarts.Load() > beforeClose {
		t.Fatalf("queued stale screenshot GETs ran after close: before=%d after=%d", beforeClose, shotStarts.Load())
	}
	app.mu.Lock()
	app.covers["snes-sonic"] = &coverSlot{phase: coverArtwork, handle: sonicCover}
	app.mu.Unlock()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Covers["snes-sonic"] != nil
	})
	if coverGets.Load() < 1 {
		t.Fatal("cover work should run after screenshot cancel")
	}
}

func TestWorkerRejectsStaleScreenshotJobBeforeIO(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	var shotGets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/artwork/"):
			shotGets.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1
	})
	app.mu.Lock()
	gen := app.loadGen
	app.shotGen++
	stale := app.shotGen - 1
	app.mu.Unlock()
	select {
	case app.jobs <- workItem{kind: workScreenshot, gameID: "snes-mario", handle: handle, gen: gen, shotGen: stale}:
	case <-time.After(time.Second):
		t.Fatal("jobs channel blocked")
	}
	hold := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(hold) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	if shotGets.Load() != 0 {
		t.Fatalf("stale screenshot job performed I/O, gets=%d", shotGets.Load())
	}
}

func TestPresentationResultClearsEmptyShotIDs(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	app := NewApp(nil, 800, 600, 10)
	app.games = []Game{availableGame("snes-mario", "Mario", "snes")}
	app.grid.SetCount(1)
	app.applyResult(workResult{
		kind:          workPresentation,
		gameID:        "snes-mario",
		state:         "offline",
		screenshotIDs: []string{handle},
		gen:           app.loadGen,
	})
	app.mu.Lock()
	if got := app.focusDetailLocked().ScreenshotIDs; len(got) != 1 || got[0] != handle {
		app.mu.Unlock()
		t.Fatalf("incomplete shots = %#v", got)
	}
	app.mu.Unlock()
	app.applyResult(workResult{
		kind:          workPresentation,
		gameID:        "snes-mario",
		state:         "ready",
		attribution:   "Data from IGDB.com",
		summary:       "Jump on turtles.",
		screenshotIDs: nil,
		gen:           app.loadGen,
	})
	app.mu.Lock()
	defer app.mu.Unlock()
	if ids, ok := app.shotIDs["snes-mario"]; ok {
		t.Fatalf("shotIDs still set: %#v", ids)
	}
	detail := app.focusDetailLocked()
	if len(detail.ScreenshotIDs) != 0 {
		t.Fatalf("stale overlay = %#v", detail.ScreenshotIDs)
	}
	if detail.Summary != "Jump on turtles." {
		t.Fatalf("summary = %q", detail.Summary)
	}
}

func TestScreenshotDeadlineIsTerminalButCancelIsNot(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	app := NewApp(nil, 800, 600, 10)
	app.games = []Game{availableGame("snes-mario", "Mario", "snes")}
	app.grid.SetCount(1)
	app.details["snes-mario"] = FocusDetail{ScreenshotIDs: []string{handle}}
	app.detailOpen = true
	app.applyResult(workResult{
		kind:   workScreenshot,
		handle: handle,
		gen:    app.loadGen,
		err:    fmt.Errorf("artwork: %w", context.Canceled),
	})
	app.mu.Lock()
	canceledFailed := app.shotFailedLocked(handle)
	app.mu.Unlock()
	if canceledFailed {
		t.Fatal("canceled screenshot should not be terminal")
	}
	app.applyResult(workResult{
		kind:   workScreenshot,
		handle: handle,
		gen:    app.loadGen,
		err:    fmt.Errorf("artwork: %w", context.DeadlineExceeded),
	})
	app.mu.Lock()
	timeoutFailed := app.shotFailedLocked(handle)
	app.mu.Unlock()
	if !timeoutFailed {
		t.Fatal("screenshot timeout should be terminal")
	}
}

func TestCarouselIndexClampAndFailedSkip(t *testing.T) {
	t.Parallel()
	ids := []string{"a", "b", "c", "d"}
	failed := map[string]bool{"b": true, "c": true}
	isFailed := func(id string) bool { return failed[id] }
	if got := clampCarouselIndex(-1, 4); got != 0 {
		t.Fatalf("neg = %d", got)
	}
	if got := clampCarouselIndex(9, 4); got != 3 {
		t.Fatalf("high = %d", got)
	}
	if got := clampCarouselIndex(0, 0); got != 0 {
		t.Fatalf("empty = %d", got)
	}
	if got := stepCarousel(nil, isFailed, 0, 1); got != 0 {
		t.Fatalf("empty step = %d", got)
	}
	if got := stepCarousel(ids, isFailed, 0, 1); got != 3 {
		t.Fatalf("skip failed right = %d", got)
	}
	if got := stepCarousel(ids, isFailed, 3, -1); got != 0 {
		t.Fatalf("skip failed left = %d", got)
	}
	if got := skipFailedCarousel(ids, isFailed, 1); got != 3 {
		t.Fatalf("skip current failed = %d", got)
	}
	allFailed := func(string) bool { return true }
	if got := stepCarousel(ids, allFailed, 2, 1); got != 2 {
		t.Fatalf("all failed stay = %d", got)
	}
}

func TestGameDetailMergesCatalogAndPresentation(t *testing.T) {
	t.Parallel()
	game := Game{ID: "snes-mario", Title: "Mario", System: "snes", Year: "1990", Genre: "Action", Favorite: true}
	d := GameDetail(game, Presentation{})
	if d.Title != "Mario" || d.Platform != "snes" || d.Year != "1990" || d.Genre != "Action" || !d.Favorite {
		t.Fatalf("catalog %+v", d)
	}
	if d.Studio != "" || d.Summary != "" || len(d.ScreenshotIDs) != 0 {
		t.Fatalf("empty presentation leaked %+v", d)
	}
	shot := strings.Repeat("ab", 32)
	d = GameDetail(game, Presentation{
		Presentation: &PresentationInfo{
			Year:          "1985",
			Genre:         "Platform",
			Studio:        "Nintendo",
			Players:       "1-2",
			Summary:       "Jump.",
			ScreenshotIDs: []string{shot, "nope", shot},
		},
		Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
	})
	if d.Year != "1985" || d.Genre != "Platform" || d.Studio != "Nintendo" || d.Players != "1-2" || d.Summary != "Jump." {
		t.Fatalf("merged %+v", d)
	}
	if d.Attribution != "Data from IGDB.com" || len(d.ScreenshotIDs) != 1 || d.ScreenshotIDs[0] != shot {
		t.Fatalf("shots/attr %+v", d)
	}
}

func TestFocusDetailOmitsEmptyStudioPlayersAndScreenshots(t *testing.T) {
	t.Parallel()
	d := FocusDetail{Platform: "Super NES", Year: "1985", Genre: "Platform"}
	if d.MetaFacts() != "Super NES  ·  1985  ·  Platform" {
		t.Fatalf("facts = %q", d.MetaFacts())
	}
	if d.studioLine() != "" || d.playersLine() != "" {
		t.Fatalf("empty lines studio=%q players=%q", d.studioLine(), d.playersLine())
	}
	d.Studio = "Nintendo"
	d.Players = "1-2"
	d.Region = "USA"
	if d.MetaFacts() != "Super NES  ·  1985  ·  Platform  ·  Nintendo  ·  1-2  ·  USA" {
		t.Fatalf("rich facts = %q", d.MetaFacts())
	}
	if d.studioLine() != "Nintendo" || d.playersLine() != "1-2" {
		t.Fatalf("lines studio=%q players=%q", d.studioLine(), d.playersLine())
	}
}

func TestGameDetailCopiesCatalogRegionAndOmitsMissingCopy(t *testing.T) {
	t.Parallel()
	d := GameDetail(Game{Title: "Sonic", System: "megadrive", Year: "1990", Genre: "Action", Region: "usa"}, Presentation{})
	if d.Year != "1990" || d.Genre != "Action" || d.Region != "USA" || d.Summary != "" || d.Players != "" {
		t.Fatalf("catalog-only %+v", d)
	}
	if d.MetaFacts() != "megadrive  ·  1990  ·  Action  ·  USA" {
		t.Fatalf("facts = %q", d.MetaFacts())
	}
	empty := GameDetail(Game{Title: "Pong", System: "pong"}, Presentation{})
	if empty.Region != "" || empty.Summary != "" || empty.Players != "" || empty.MetaFacts() != "pong" {
		t.Fatalf("empty %+v facts=%q", empty, empty.MetaFacts())
	}
	cached := true
	onKit := GameDetail(Game{Title: "Sonic", System: "megadrive", ROMCached: &cached}, Presentation{})
	if onKit.Cached != "ON KIT" || !strings.Contains(onKit.MetaFacts(), "ON KIT") {
		t.Fatalf("cached %+v facts=%q", onKit, onKit.MetaFacts())
	}
	missing := false
	needs := GameDetail(Game{Title: "Sonic", System: "megadrive", ROMCached: &missing}, Presentation{})
	if needs.Cached != "NEEDS ROM" {
		t.Fatalf("missing %#v", needs)
	}
	ready := GameDetail(Game{Title: "Sonic", System: "megadrive", Region: "japan"}, Presentation{
		Presentation: &PresentationInfo{Year: "1991", Genre: "Platform", Studio: "SEGA", Players: "1-2", Summary: "Jump.", VideoID: strings.Repeat("ab", 32)},
	})
	if ready.Year != "1991" || ready.Genre != "Platform" || ready.Studio != "SEGA" || ready.Players != "1-2" || ready.Region != "Japan" || ready.Summary != "Jump." {
		t.Fatalf("presentation %+v", ready)
	}
	if ready.VideoID != strings.Repeat("ab", 32) {
		t.Fatalf("video %q", ready.VideoID)
	}
	plain := GameDetail(Game{Title: "Pong"}, Presentation{Presentation: &PresentationInfo{Summary: "Ball."}})
	if plain.VideoID != "" {
		t.Fatalf("still-only grew video %q", plain.VideoID)
	}
}

func TestAppFocusDetailDecodesStudioPlayersAndScreenshots(t *testing.T) {
	cover := strings.Repeat("ab", 32)
	shotOK := strings.Repeat("cd", 32)
	shotBad := strings.Repeat("ef", 32)
	shotMiss := strings.Repeat("11", 32)
	pngBytes := mustPNG(t, 16, 10, color.RGBA{R: 10, G: 200, B: 20, A: 255})
	var shotGets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []Platform{{ID: "snes", Label: "Super NES"}}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &PresentationInfo{
					CoverArtworkID: cover,
					Summary:        "Jump on turtles.",
					Year:           "1985",
					Genre:          "Platform",
					Studio:         "Nintendo",
					Players:        "1-2",
					ScreenshotIDs:  []string{shotOK, "nope", shotBad, shotOK, shotMiss},
				},
				Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
			})
		case r.URL.Path == "/api/v1/presentation/artwork/"+cover:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case r.URL.Path == "/api/v1/presentation/artwork/"+shotOK:
			shotGets.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case r.URL.Path == "/api/v1/presentation/artwork/"+shotBad:
			shotGets.Add(1)
			http.Error(w, "corrupt", http.StatusInternalServerError)
		case r.URL.Path == "/api/v1/presentation/artwork/"+shotMiss:
			shotGets.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		d := snap.FocusDetail
		return d.Studio == "Nintendo" && d.Players == "1-2" && d.Summary == "Jump on turtles." &&
			len(d.ScreenshotIDs) == 3 && d.ScreenshotIDs[0] == shotOK
	})
	now := time.Now()
	app.HandleCommand(CmdDown, now)
	snap := app.Snapshot()
	if !snap.Detail.Open {
		t.Fatal("down from last row should enter detail")
	}
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Screenshots[shotOK] != nil
	})
	if snap.Detail.Count != 3 {
		t.Fatalf("count = %d ids=%#v", snap.Detail.Count, snap.FocusDetail.ScreenshotIDs)
	}
	waitSnapshot(t, app, 2*time.Second, func(Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return app.shotFailedLocked(shotBad) && app.shotFailedLocked(shotMiss)
	})
	app.HandleCommand(CmdRight, time.Now())
	snap = app.Snapshot()
	if snap.Detail.Index != 0 {
		t.Fatalf("right should skip failed shots, index=%d", snap.Detail.Index)
	}
	if snap.Screenshots[shotOK] == nil {
		t.Fatal("good screenshot missing")
	}
	if _, ok := snap.Screenshots[shotBad]; ok {
		t.Fatal("failed screenshot should not be presented")
	}
	after := shotGets.Load()
	hold := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(hold) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	if shotGets.Load() != after {
		t.Fatalf("failed screenshot handles were retried: before=%d after=%d", after, shotGets.Load())
	}
}

func TestAppDetailFocusEnterLeaveDoesNotStealBindings(t *testing.T) {
	var launches atomic.Int64
	var stops atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{
				availableGame("snes-mario", "Mario", "snes"),
				availableGame("megadrive-sonic", "Sonic", "megadrive"),
			}})
		case r.URL.Path == "/api/v1/session/launch" && r.Method == http.MethodPost:
			launches.Add(1)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","execution":"fpga_native"}`)
		case r.URL.Path == "/api/v1/session/stop" && r.Method == http.MethodPost:
			stops.Add(1)
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.URL.Path == "/api/v1/session":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 2
	})
	now := time.Now()
	if app.Snapshot().Detail.Open {
		t.Fatal("detail should start closed")
	}
	app.HandleCommand(CmdDown, now)
	if !app.Snapshot().Detail.Open {
		t.Fatal("down on last row should open detail")
	}
	app.HandleCommand(CmdUp, now)
	if app.Snapshot().Detail.Open {
		t.Fatal("up should leave detail")
	}
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Detail.Open {
		t.Fatal("east/back should leave detail")
	}

	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdLayoutCycle, now)
	snap := app.Snapshot()
	if !snap.Detail.Open {
		t.Fatal("select/layout must still cycle while detail is open")
	}
	if snap.Grid.Mode != LayoutShelf {
		t.Fatalf("layout = %s", snap.Grid.Mode)
	}
	app.HandleCommand(CmdLayoutCycle, now)
	app.HandleCommand(CmdLayoutCycle, now)
	if app.Snapshot().Grid.Mode != LayoutGrid {
		t.Fatalf("layout after cycle = %s", app.Snapshot().Grid.Mode)
	}

	app.HandleCommand(CmdSettings, now)
	if !app.Snapshot().Settings.Open {
		t.Fatal("guide/settings should open from detail")
	}
	if app.Snapshot().Detail.Open {
		t.Fatal("settings should close detail")
	}
	app.HandleCommand(CmdBack, now)

	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSearch, now)
	if !app.Snapshot().SearchOpen {
		t.Fatal("north/search OSK should open from detail")
	}
	if app.Snapshot().Detail.Open {
		t.Fatal("search should close detail")
	}
	app.HandleCommand(CmdBack, now)

	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return launches.Load() >= 1 && (snap.Session.State == "active" || snap.Launch.Phase == "ok" || snap.Launch.HTTPStatus != 0)
	})
	if launches.Load() < 1 {
		t.Fatal("south/A should still launch from detail")
	}

	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" || snap.GPUParked
	})
	app.HandleCommand(CmdDown, time.Now())
	if app.Snapshot().Detail.Open {
		t.Fatal("session must not enter detail")
	}
	app.HandleCommand(CmdBack, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return stops.Load() >= 1
	})
	if stops.Load() < 1 {
		t.Fatal("east/B should still stop the session")
	}
}

func TestAppOfflinePresentationOmitsIncompleteDetail(t *testing.T) {
	var mu sync.Mutex
	ready := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			mu.Lock()
			ok := ready
			mu.Unlock()
			pres := Presentation{GameID: "snes-mario", State: "offline"}
			if ok {
				pres.State = "ready"
				pres.Presentation = &PresentationInfo{Summary: "Jump on turtles.", Studio: "Nintendo", Players: "1"}
				pres.Attribution = &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"}
			} else {
				pres.Presentation = &PresentationInfo{
					Summary:       "stale",
					Studio:        "hidden",
					Players:       "9",
					ScreenshotIDs: []string{strings.Repeat("ab", 32)},
				}
			}
			_ = json.NewEncoder(w).Encode(pres)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && snap.FocusDetail.Summary == "" && snap.FocusDetail.Studio == "" && snap.FocusDetail.Players == "" &&
			len(snap.FocusDetail.ScreenshotIDs) == 1
	})
	mu.Lock()
	ready = true
	mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.FocusDetail.Summary == "Jump on turtles." && snap.FocusDetail.Studio == "Nintendo" && snap.FocusDetail.Players == "1" &&
			len(snap.FocusDetail.ScreenshotIDs) == 0
	})
}

func TestAppShelfLastPageDownOpensDetail(t *testing.T) {
	t.Parallel()
	app := catalogApp(3)
	app.SetLayout(LayoutShelf)
	grid := app.Snapshot().Grid
	_, end := grid.VisibleRange()
	if end < 3 {
		t.Fatalf("expected partial last page visible, got end=%d cols=%d", end, grid.Columns)
	}
	if app.Snapshot().Grid.Focus != 0 {
		t.Fatalf("focus = %d", app.Snapshot().Grid.Focus)
	}
	app.HandleCommand(CmdDown, time.Now())
	snap := app.Snapshot()
	if !snap.Detail.Open {
		t.Fatalf("shelf last-page down should open detail, focus=%d", snap.Grid.Focus)
	}
	if snap.Grid.Focus != 0 {
		t.Fatalf("focus moved to %d, want stay on last page start", snap.Grid.Focus)
	}
}

func TestAppDetailOpensBeforeScreenshotsArrive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &PresentationInfo{
					Summary:       "Jump on turtles.",
					Studio:        "Nintendo",
					ScreenshotIDs: []string{strings.Repeat("cd", 32)},
				},
				Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/artwork/"):
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1
	})
	app.HandleCommand(CmdDown, time.Now())
	snap := app.Snapshot()
	if !snap.Detail.Open {
		t.Fatal("detail pane must open without waiting on screenshot bytes")
	}
	if snap.Screenshots[strings.Repeat("cd", 32)] != nil {
		t.Fatal("missing screenshot should not appear as a decoded still")
	}
}
