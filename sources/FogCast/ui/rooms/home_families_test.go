package rooms

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/testwait"
)

// homeServices answers Home's two library reads: the fpga catalog and the
// recents collection.
type homeServices struct {
	fakeServices
	recentsMu      sync.Mutex
	recents        []hostclient.Game
	recentsReads   int
	recentsQueries []hostclient.GameListQuery
	recentsLimits  []int
}

func (s *homeServices) setRecents(games ...hostclient.Game) {
	s.recentsMu.Lock()
	defer s.recentsMu.Unlock()
	s.recents = games
}

func (s *homeServices) recentsAnswered() int {
	s.recentsMu.Lock()
	defer s.recentsMu.Unlock()
	return s.recentsReads
}

func (s *homeServices) recordedRecentsQueries() []hostclient.GameListQuery {
	s.recentsMu.Lock()
	defer s.recentsMu.Unlock()
	return append([]hostclient.GameListQuery(nil), s.recentsQueries...)
}

func (s *homeServices) recordedRecentsLimits() []int {
	s.recentsMu.Lock()
	defer s.recentsMu.Unlock()
	return append([]int(nil), s.recentsLimits...)
}

func (s *homeServices) setLibraryError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *homeServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, error) {
	if q.Collection == "recents" {
		s.recentsMu.Lock()
		defer s.recentsMu.Unlock()
		s.recentsReads++
		s.recentsQueries = append(s.recentsQueries, q)
		s.recentsLimits = append(s.recentsLimits, max)
		games := make([]hostclient.Game, 0, len(s.recents))
		if len(q.CoreIDs) > 0 {
			wanted := make(map[string]struct{}, len(q.CoreIDs))
			for _, coreID := range q.CoreIDs {
				wanted[coreID] = struct{}{}
			}
			for _, game := range s.recents {
				if _, ok := wanted[game.CoreID]; ok {
					games = append(games, game)
				}
			}
		} else {
			games = append(games, s.recents...)
		}
		if max > 0 && len(games) > max {
			games = games[:max]
		}
		return games, nil
	}
	return s.fakeServices.QueryGames(ctx, q, max)
}

type delayedFirstHomeRecentServices struct {
	*homeServices
	started     chan struct{}
	release     chan struct{}
	returned    chan struct{}
	staleGames  []hostclient.Game
	staleErr    error
	mu          sync.Mutex
	recentCalls int
	releaseOnce sync.Once
}

func (s *delayedFirstHomeRecentServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, error) {
	if q.Collection != "recents" {
		return s.homeServices.QueryGames(ctx, q, max)
	}
	s.mu.Lock()
	s.recentCalls++
	first := s.recentCalls == 1
	s.mu.Unlock()
	if first {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			close(s.returned)
			return nil, ctx.Err()
		}
		close(s.returned)
		return append([]hostclient.Game(nil), s.staleGames...), s.staleErr
	}
	return s.homeServices.QueryGames(ctx, q, max)
}

func (s *delayedFirstHomeRecentServices) releaseFirst() {
	s.releaseOnce.Do(func() { close(s.release) })
}

type observedHomeCores struct {
	*fakeLocalCores
	firstRead chan struct{}
	readOnce  sync.Once
}

func (s *observedHomeCores) List(ctx context.Context) ([]localcores.Core, error) {
	cores, err := s.fakeLocalCores.List(ctx)
	s.readOnce.Do(func() { close(s.firstRead) })
	return cores, err
}

// homeSettled waits for both the data and the completed Home picture.
func homeSettled(r *Instance) bool {
	d := r.Destination()
	if (d.Kind != KindFamily && d.Kind != KindGame) || d.Availability == AvailChecking || d.Availability == "" {
		return false
	}
	want := 4
	if r.opts.Width < 480 || r.opts.Height < 360 {
		want = 1
	}
	families := 0
	for _, h := range r.frame.Hits {
		if strings.HasPrefix(h.ID, "family:") {
			families++
		}
	}
	return families >= want
}

// machineEntry is a model's catalog row without media; title adds media.
func machineEntry(id, coreID string, block string) hostclient.Game {
	g := hostclient.Game{ID: id, Title: id, System: "fpga", CoreID: coreID, State: "available", RootOnline: true, Launchable: true}
	switch block {
	case "rom":
		g.ROMRequired = true
	case "offline":
		g.RootOnline = false
	}
	return g
}

func titleEntry(id, coreID string) hostclient.Game {
	g := machineEntry(id, coreID, "")
	g.MediaID = strings.Repeat("a", 64)
	g.MediaRole = "cassette"
	return g
}

func loadHome(t *testing.T, opts Options) *Instance {
	t.Helper()
	if opts.Width == 0 {
		opts.Width, opts.Height = 1280, 720
	}
	r := newRoom(t, examplePack(t, "example.fes-cores"), opts)
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func TestHomeShowsTheFourReleaseFamiliesWithLibraryState(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{
		machineEntry("zx81", "fes.zx81", ""),
		titleEntry("zx81-a", "fes.zx81"),
		titleEntry("zx81-b", "fes.ts1000"),
		machineEntry("spectrum", "fes.spectrum", ""),
		machineEntry("st", "fes.atari-st", ""),
		machineEntry("c64", "fes.c64", "rom"),
		machineEntry("pong", "fes.pong", ""),
	}}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true})
	f := stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	text := frameText(f)
	for _, want := range []string{"Sinclair ZX81\n", "ZX Spectrum\n", "Atari ST\n", "Commodore 64\n", "Computers\n", "Settings\n", "2 games\n", "No games yet\n", "Add your ROM files\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Home is missing %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"Library\n", "Systems\n", "Pong", "Recently played"} {
		if strings.Contains(text, gone) {
			t.Fatalf("release Home shows %q:\n%s", gone, text)
		}
	}

	d := r.Destination()
	if d.Kind != KindFamily || d.Label != "Sinclair ZX81" || d.Availability != AvailReady || d.Status != "2 games" ||
		d.Models != "4 models: ZX81, ZX80, TS1000, TS1500" || d.RoomID != "example.hardware" || d.CoreID != "fes.zx81" || d.GameID != "zx81" ||
		d.Confirm() != ConfirmEnterRoom {
		t.Fatalf("opening family %+v confirm %v", d, d.Confirm())
	}
	if len(d.Details) != 3 || d.Details[0] != "Ready: ZX81" || d.Details[1] != "Not on this kit: ZX80, TS1000, TS1500" || d.Details[2] != "In your library: 2 games" {
		t.Fatalf("ZX81 details %q", d.Details)
	}

	r.Input("right")
	if d := r.Destination(); d.Label != "ZX Spectrum" || d.Status != "No games yet" || d.RoomID != "example.system" || d.CoreID != "fes.spectrum" {
		t.Fatalf("Spectrum %+v", d)
	}
	r.Input("right")
	r.Input("right")
	d = r.Destination()
	if d.Label != "Commodore 64" || d.Availability != AvailUnavailable || d.FamilyNeeds != "firmware" ||
		d.Status != "Add your Commodore 64 ROM files to play" || d.Confirm() != ConfirmEnterRoom || d.CoreID != "fes.c64" || d.GameID != "c64" {
		t.Fatalf("C64 %+v confirm %v", d, d.Confirm())
	}
	if r.Input("right") {
		t.Log("right past the last family is consumed")
	}
	if got := r.Destination().Label; got != "Commodore 64" {
		t.Fatalf("right moved past the last family to %q", got)
	}
	// Up from the top row is left to the launcher, which focuses the hero
	// actions and then sends header.
	if r.Input("up") {
		t.Fatal("Home consumed Up from its top row")
	}
	if !r.Input("header") {
		t.Fatal("header")
	}
	if d := r.Destination(); d.Kind != KindAction || d.LauncherAction != "settings" {
		t.Fatalf("header %+v", d)
	}
	if !r.Input("down") {
		t.Fatal("down from the header")
	}
	if d := r.Destination(); d.Label != "Commodore 64" {
		t.Fatalf("down from the header went to %+v", d)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestHomeRecentlyPlayedLeadsAndPlaysTheTitle(t *testing.T) {
	svc := &homeServices{
		fakeServices: fakeServices{games: []hostclient.Game{machineEntry("zx81", "fes.zx81", "")}},
		recents: []hostclient.Game{
			titleEntry("lunar", "fes.spectrum"),
			titleEntry("pong-title", "fes.pong"),
			titleEntry("bits", "fes.zx81"),
		},
	}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true})
	f := stepUntil(t, r, func(Frame) bool { return homeSettled(r) && r.Destination().Kind == KindGame })
	text := frameText(f)
	if !strings.Contains(text, "Recently played\n") || !strings.Contains(text, "lunar\n") || !strings.Contains(text, "bits\n") || strings.Contains(text, "pong-title") {
		t.Fatalf("recents row:\n%s", text)
	}
	d := r.Destination()
	if d.Kind != KindGame || d.GameID != "lunar" || d.Kicker != "Recently played" || d.Confirm() != ConfirmLaunch {
		t.Fatalf("opening recent %+v confirm %v", d, d.Confirm())
	}
	if r.Input("up") {
		t.Fatal("Home consumed Up from Recently played")
	}
	r.Input("right")
	if d := r.Destination(); d.GameID != "bits" {
		t.Fatalf("second recent %+v", d)
	}
	r.Input("down")
	if d := r.Destination(); d.Kind != KindFamily {
		t.Fatalf("down from recents %+v", d)
	}
	if !r.Input("up") {
		t.Fatal("Up from the families did not reach Recently played")
	}
	if d := r.Destination(); d.Kind != KindGame || d.GameID != "bits" {
		t.Fatalf("up from families %+v", d)
	}
}

func TestHomeRecentsAndCompactResize(t *testing.T) {
	svc := &homeServices{
		fakeServices: fakeServices{games: []hostclient.Game{machineEntry("zx81", "fes.zx81", "")}},
		recents: []hostclient.Game{
			titleEntry("one", "fes.zx81"), titleEntry("two", "fes.spectrum"), titleEntry("three", "fes.c64"),
		},
	}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true})
	f := stepUntil(t, r, func(Frame) bool { return homeSettled(r) && r.Destination().Kind == KindGame })
	for _, title := range []string{"one\n", "two\n", "three\n"} {
		if !strings.Contains(frameText(f), title) {
			t.Fatalf("missing %q in %s", title, frameText(f))
		}
	}
	r.Resize(300, 350)
	if d := r.Destination(); d.Kind != KindFamily {
		t.Fatalf("compact resize retained recent destination: %+v", d)
	}
	r.Step(time.Now())
	r.Resize(1280, 720)
	f = r.Step(time.Now())
	if !strings.Contains(frameText(f), "Recently played\n") {
		t.Fatalf("hero resize lost recents: %s", frameText(f))
	}
}

func TestHomeRecentsPagePastCoresHiddenFromReleaseHome(t *testing.T) {
	recents := make([]hostclient.Game, 0, 13)
	for i := 0; i < 12; i++ {
		recents = append(recents, titleEntry("held-back", "fes.pong"))
	}
	recents = append(recents, titleEntry("older-visible", "fes.zx81"))
	svc := &homeServices{recents: recents}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })

	if d := r.Destination(); d.Kind != KindGame || d.GameID != "older-visible" {
		t.Fatalf("eligible title after held-back recents was not shown: %+v", d)
	}
	queries := svc.recordedRecentsQueries()
	limits := svc.recordedRecentsLimits()
	if len(queries) != 1 || len(limits) != 1 || limits[0] != 3 {
		t.Fatalf("recent queries %+v", queries)
	}
	var zx81, pong bool
	for _, coreID := range queries[0].CoreIDs {
		zx81 = zx81 || coreID == "fes.zx81"
		pong = pong || coreID == "fes.pong"
	}
	if !zx81 || pong {
		t.Fatalf("release Home recent core filter %v", queries[0].CoreIDs)
	}
}

func TestHomeFallsBackToTheKitWhenTheLibraryFails(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{err: errors.New("host unreachable")}}
	fake := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "firmware", Block: "Needs firmware"},
		{PackageID: corePongID, CoreID: "fes.spectrum", Name: "Spectrum", Needs: "none", Launchable: true},
	}}
	r := loadHome(t, Options{Services: svc, Local: fake, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	// The opening focus is the first Ready family.
	if d := r.Destination(); d.Label != "ZX Spectrum" || d.Availability != AvailReady || d.Status != "Ready to open" || d.GameID != "" {
		t.Fatalf("kit fallback opening %+v", d)
	}
	r.Input("left")
	if d := r.Destination(); d.Label != "Sinclair ZX81" || d.FamilyNeeds != "firmware" {
		t.Fatalf("kit fallback firmware %+v", d)
	}
	r.Input("right")
	r.Input("right")
	if d := r.Destination(); d.Label != "Atari ST" || d.Status != "Not installed on this kit" || d.Availability != AvailUnavailable {
		t.Fatalf("missing family %+v", d)
	}
}

func TestHomeFailedRefreshClearsStaleLibraryData(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{
		machineEntry("stale-machine", "fes.zx81", ""),
		titleEntry("old-title-a", "fes.zx81"),
		titleEntry("old-title-b", "fes.zx81"),
	}}}
	local := &fakeLocalCores{cores: []localcores.Core{{
		PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true,
	}}}
	r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool {
		d := r.Destination()
		return homeSettled(r) && d.GameID == "stale-machine" && d.Status == "2 games"
	})

	svc.setLibraryError(errors.New("host unreachable"))
	r.Resume()
	stepUntil(t, r, func(Frame) bool {
		d := r.Destination()
		return d.Kind == KindFamily && d.Label == "Sinclair ZX81" && d.GameID == "" && d.Status == "Ready to open"
	})
}

func TestHomeChecksUntilTheLibraryAnswers(t *testing.T) {
	held := blockingLocalCores{fakeLocalCores: &fakeLocalCores{}, release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-held.release:
		default:
			close(held.release)
		}
	})
	// No library: the kit decides, and it has not answered yet.
	r := loadHome(t, Options{Local: held, ReleaseOnly: true})
	r.Step(time.Now())
	if d := r.Destination(); d.Kind != KindFamily || d.Availability != AvailChecking || d.Confirm() != ConfirmWait {
		t.Fatalf("Home before any answer %+v confirm %v", d, d.Confirm())
	}
	close(held.release)
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
}

func TestHomeRestoresTheSavedFamily(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "home.state.json")
	if err := os.WriteFile(storePath, []byte(`{"focus":"family:fes.atari-st"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{machineEntry("zx81", "fes.zx81", "")}}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true, StorePath: storePath})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	if d := r.Destination(); d.Label != "Atari ST" {
		t.Fatalf("saved family not restored: %+v", d)
	}
	r.Input("left")
	if err := r.CheckGlobal(`store.get("focus", "") == "family:spectrum"`); err != nil {
		t.Fatal(err)
	}
}

func TestHomeShowingAllCoresKeepsRoutesAndOtherFamilies(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{machineEntry("coleco", "fes.coleco", "offline")}}}
	r := loadHome(t, Options{Services: svc})
	f := stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	for _, want := range []string{"Library\n", "Systems\n", "Settings\n"} {
		if !strings.Contains(frameText(f), want) {
			t.Fatalf("all-cores Home is missing %q", want)
		}
	}
	// After the v1 families come the others in systems order: Apple II,
	// then ColecoVision.
	for i := 0; i < 5; i++ {
		r.Input("right")
	}
	d := r.Destination()
	if d.Label != "ColecoVision" || d.RoomID != "example.coleco-arcade" || d.Status != "Its library is offline" {
		t.Fatalf("sixth family %+v", d)
	}
	r.Input("header")
	if d := r.Destination(); d.Kind != KindLibrary {
		t.Fatalf("all-cores header starts on %+v", d)
	}
}

func TestHomeCompactLayoutKeepsFamiliesAndRoutes(t *testing.T) {
	r := loadHome(t, Options{Width: 470, Height: 350, ReleaseOnly: true})
	f := stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	var family, settings bool
	for _, h := range f.Hits {
		family = family || h.ID == "family:1"
		settings = settings || h.ID == "menu:1"
	}
	if !family || !settings {
		t.Fatalf("compact hits %+v", f.Hits)
	}
	r.Activate("family:3")
	if d := r.Destination(); d.Label != "Atari ST" {
		t.Fatalf("compact pointer %+v", d)
	}
}

func TestHomeCompactListWalksFamiliesAndReachesTheHeader(t *testing.T) {
	svc := &homeServices{recents: []hostclient.Game{titleEntry("lunar", "fes.spectrum")}}
	r := loadHome(t, Options{Services: svc, Width: 470, Height: 350, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	// Recently played isn't drawn here, so Home opens on a family.
	if d := r.Destination(); d.Kind != KindFamily || d.Label != "Sinclair ZX81" {
		t.Fatalf("compact opening %+v", d)
	}
	if !r.Input("down") || r.Destination().Label != "ZX Spectrum" {
		t.Fatalf("down %+v", r.Destination())
	}
	r.Input("up")
	if !r.Input("up") {
		t.Fatal("Up from the top family was not consumed")
	}
	if d := r.Destination(); d.Kind != KindAction {
		t.Fatalf("Up from the top family %+v", d)
	}
	r.Input("down")
	if d := r.Destination(); d.Label != "Sinclair ZX81" {
		t.Fatalf("down from the header %+v", d)
	}
}

func TestHomeKeepsTheSameRecentTitleAfterAResume(t *testing.T) {
	svc := &homeServices{recents: []hostclient.Game{titleEntry("a", "fes.spectrum"), titleEntry("b", "fes.zx81")}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) && r.Destination().GameID == "a" })
	r.Input("right")
	if d := r.Destination(); d.GameID != "b" {
		t.Fatalf("second recent %+v", d)
	}
	// Playing b moves it to the front.
	svc.setRecents(titleEntry("b", "fes.zx81"), titleEntry("a", "fes.spectrum"))
	r.Resume()
	stepUntil(t, r, func(Frame) bool { return svc.recentsAnswered() >= 2 })
	for i := 0; i < 3; i++ {
		r.Step(time.Now())
	}
	if d := r.Destination(); d.GameID != "b" {
		t.Fatalf("resume moved focus to %+v", d)
	}
}

func TestHomeIgnoresStaleRecentResponsesAfterReadinessRefresh(t *testing.T) {
	staleError := errors.New("older Recent read failed")
	for _, tc := range []struct {
		name       string
		staleGames []hostclient.Game
		staleErr   error
		wantStale  string
	}{
		{name: "older list", staleGames: []hostclient.Game{titleEntry("older", "fes.spectrum")}, wantStale: "older"},
		{name: "older error", staleErr: staleError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &homeServices{}
			svc := &delayedFirstHomeRecentServices{
				homeServices: base,
				started:      make(chan struct{}),
				release:      make(chan struct{}),
				returned:     make(chan struct{}),
				staleGames:   tc.staleGames,
				staleErr:     tc.staleErr,
			}
			defer svc.releaseFirst()

			local := &observedHomeCores{fakeLocalCores: &fakeLocalCores{}, firstRead: make(chan struct{})}
			r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true})
			select {
			case <-svc.started:
			case <-time.After(testwait.Timeout):
				t.Fatal("initial Recent read did not start")
			}
			select {
			case <-local.firstRead:
			case <-time.After(testwait.Timeout):
				t.Fatal("initial kit readiness read did not finish")
			}

			// The kit becomes Ready before Home resumes and requests fresh recents.
			local.mu.Lock()
			local.cores = []localcores.Core{{
				PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true,
			}}
			local.mu.Unlock()
			newer := titleEntry("newer", "fes.zx81")
			base.setRecents(newer)
			r.Resume()
			stepUntil(t, r, func(Frame) bool {
				d := r.Destination()
				return d.Kind == KindGame && d.GameID == newer.ID
			})

			svc.releaseFirst()
			select {
			case <-svc.returned:
			case <-time.After(testwait.Timeout):
				t.Fatal("older Recent read did not return")
			}
			deadline := time.Now().Add(testwait.Timeout)
			for {
				r.pendingMu.Lock()
				delivered := false
				for _, result := range r.pending {
					if tc.staleErr != nil && result.err != nil && result.err.Error() == tc.staleErr.Error() {
						delivered = true
					}
					for _, game := range result.games {
						delivered = delivered || tc.wantStale != "" && game.ID == tc.wantStale
					}
				}
				r.pendingMu.Unlock()
				if delivered {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("older Recent response was not delivered")
				}
				time.Sleep(time.Millisecond)
			}

			r.Step(time.Now())
			if d := r.Destination(); d.Kind != KindGame || d.GameID != newer.ID || d.Kicker != "Recently played" {
				t.Fatalf("older response replaced the newer Recent focus: %+v", d)
			}
		})
	}
}

func TestHomeCountsATitleOnceAcrossItsEditions(t *testing.T) {
	uk, us := titleEntry("game-uk", "fes.spectrum"), titleEntry("game-us", "fes.spectrum")
	grouped := uk
	grouped.VariantCount = 2
	grouped.Variants = []hostclient.Game{uk, us}
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{machineEntry("spectrum", "fes.spectrum", ""), grouped}}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	// Spectrum is the only Ready family, so Home opens on it.
	if d := r.Destination(); d.Label != "ZX Spectrum" || d.Status != "1 game" {
		t.Fatalf("Spectrum %+v", d)
	}
}
