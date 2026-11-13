package rooms

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/testwait"
)

// homeServices answers Home's admitted computer and library reads.
type homeServices struct {
	fakeServices
	computers      []hostclient.ShelfComputer
	hardwareErr    error
	recentsMu      sync.Mutex
	recents        []hostclient.Game
	recentsReads   int
	recentsQueries []hostclient.GameListQuery
	recentsLimits  []int
}

func (s *homeServices) Hardware(context.Context) (hostclient.HardwareSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return hostclient.HardwareSnapshot{Computers: append([]hostclient.ShelfComputer(nil), s.computers...)}, s.hardwareErr
}

func (*homeServices) SelectCoreEntryExpansion(context.Context, string, string, string, string) (hostclient.CoreEntryExpansion, error) {
	return hostclient.CoreEntryExpansion{}, errors.New("Home must not change hardware")
}

func computerEntry(id, coreID, reason string) hostclient.ShelfComputer {
	return hostclient.ShelfComputer{GameID: id, CoreID: coreID, PackageID: coreZXID, Ready: reason == "", UnavailableReason: reason}
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

func (s *homeServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, bool, error) {
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
		return games, false, nil
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

func (s *delayedFirstHomeRecentServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, bool, error) {
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
			return nil, false, ctx.Err()
		}
		close(s.returned)
		return append([]hostclient.Game(nil), s.staleGames...), false, s.staleErr
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

// machineEntry and titleEntry provide library rows for title counts, not machine admission.
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
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("zx81", "fes.zx81", ""), computerEntry("spectrum", "fes.spectrum", ""), computerEntry("st", "fes.atari-st", "")}, fakeServices: fakeServices{games: []hostclient.Game{
		machineEntry("zx81", "fes.zx81", ""),
		titleEntry("zx81-a", "fes.zx81"),
		titleEntry("zx81-b", "fes.ts1000"),
		machineEntry("spectrum", "fes.spectrum", ""),
		machineEntry("st", "fes.atari-st", ""),
		machineEntry("c64", "fes.c64", "rom"),
		machineEntry("pong", "fes.pong", ""),
	}}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Local: &fakeLocalCores{cores: []localcores.Core{{CoreID: "fes.c64", Needs: "firmware"}}}})
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
		d.Status != "Add your Commodore 64 ROM files to play" || d.Confirm() != ConfirmEnterRoom || d.CoreID != "fes.c64" || d.GameID != "" {
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
		computers:    []hostclient.ShelfComputer{computerEntry("zx81", "fes.zx81", "")},
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

func TestHomeFailedRefreshClearsStaleHostData(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{
		machineEntry("stale-machine", "fes.zx81", ""),
		titleEntry("old-title-a", "fes.zx81"),
		titleEntry("old-title-b", "fes.zx81"),
	}}}
	svc.computers = []hostclient.ShelfComputer{computerEntry("stale-machine", "fes.zx81", "")}
	local := &fakeLocalCores{cores: []localcores.Core{{
		PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true,
	}}}
	r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool {
		d := r.Destination()
		return homeSettled(r) && d.GameID == "stale-machine" && d.Status == "2 games"
	})

	svc.setLibraryError(errors.New("host unreachable"))
	svc.mu.Lock()
	svc.hardwareErr = errors.New("host unreachable")
	svc.mu.Unlock()
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
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("coleco", "fes.coleco", "Its library is offline")}, fakeServices: fakeServices{games: []hostclient.Game{machineEntry("coleco", "fes.coleco", "offline")}}}
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
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("spectrum", "fes.spectrum", "")}, fakeServices: fakeServices{games: []hostclient.Game{machineEntry("spectrum", "fes.spectrum", ""), grouped}}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	// Spectrum is the only Ready family, so Home opens on it.
	if d := r.Destination(); d.Label != "ZX Spectrum" || d.Status != "1 game" {
		t.Fatalf("Spectrum %+v", d)
	}
}

func TestHomeUsesAdmittedComputerWithSelectedCassetteAndIndependentLocalShelf(t *testing.T) {
	const gameID = "fpga-sinclair-zx81-1b16a527befc"
	const packageID = "bef84629d9ab7a9e64521987484b5b90384e15a01e6f10fff3dba166913322b2"
	for _, tc := range []struct {
		name         string
		computers    []hostclient.ShelfComputer
		local        []localcores.Core
		want         Availability
		game, status string
	}{
		{name: "selected cassette", computers: []hostclient.ShelfComputer{{GameID: gameID, CoreID: "fes.zx81", PackageID: packageID, Ready: true}}, want: AvailReady, game: gameID, status: "1 game"},
		{name: "independent package block", computers: []hostclient.ShelfComputer{{GameID: gameID, CoreID: "fes.zx81", PackageID: packageID, UnavailableReason: "Needs a software update"}}, want: AvailUnavailable, game: gameID, status: "Needs a software update"},
		{name: "title alone is not a machine", want: AvailUnavailable, status: "Not installed on this kit"},
		{name: "independent local shelf", local: []localcores.Core{{CoreID: "fes.zx81", PackageID: coreZXID, Launchable: true, Needs: "none"}}, want: AvailReady, status: "1 game"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title := titleEntry(gameID, "fes.zx81")
			title.PackageID = packageID
			svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{title}}, computers: tc.computers}
			local := &fakeLocalCores{cores: tc.local}
			r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true})
			stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
			d := r.Destination()
			if d.Availability != tc.want || d.GameID != tc.game || d.Status != tc.status {
				t.Fatalf("Home %+v", d)
			}
			if !strings.Contains(strings.Join(d.Details, "\n"), "In your library: 1 game") {
				t.Fatalf("selected cassette title count lost: %v", d.Details)
			}
		})
	}
}

func TestHomeFamilyCapKeepsMachineAndReportsOnlyActualOverflow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		titleCount int
		want       string
	}{
		{name: "exact combined cap", titleCount: 1999, want: "1999 games"},
		{name: "titles exceed combined cap", titleCount: 2000, want: "1999+ games"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			games := make([]hostclient.Game, 0, tc.titleCount+1)
			for i := 0; i < tc.titleCount; i++ {
				games = append(games, titleEntry(fmt.Sprintf("a-%04d", i), "fes.zx81"))
			}
			// This machine sorts after every title in the unfiltered catalog.
			games = append(games, machineEntry("z-machine", "fes.zx81", ""))
			svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("z-machine", "fes.zx81", "")}, fakeServices: fakeServices{games: games}}
			r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Budget: DefaultBudget()})
			stepUntil(t, r, func(Frame) bool {
				d := r.Destination()
				return homeSettled(r) && d.GameID == "z-machine"
			})
			if d := r.Destination(); d.Availability != AvailReady || d.Status != tc.want {
				t.Fatalf("destination = %+v, want ready machine and %q", d, tc.want)
			}
		})
	}
}

func TestHomeFamilyBatchesDiscardUnfinishedReadOnReload(t *testing.T) {
	games := []hostclient.Game{machineEntry("old-machine", "fes.zx81", "")}
	for i := 0; i < 512; i++ {
		games = append(games, titleEntry(fmt.Sprintf("old-%04d", i), "fes.zx81"))
	}
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("old-machine", "fes.zx81", "")}, fakeServices: fakeServices{games: games}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Budget: DefaultBudget()})
	deadline := time.Now().Add(testwait.Timeout)
	for {
		r.pendingMu.Lock()
		ready := false
		for _, result := range r.pending {
			if len(result.games) == len(games) {
				ready = true
				break
			}
		}
		queued := len(r.pending)
		r.pendingMu.Unlock()
		// Four family queries and recents are queued. With
		// every input present, Checking after one Step can only be the large
		// answer still being processed, not a missing asynchronous reply.
		if ready && queued >= 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("large family response was not delivered")
		}
		time.Sleep(time.Millisecond)
	}
	r.Step(time.Now())
	// An admitted host machine may already be named, but its family must
	// remain Checking until the bounded title answer is complete.
	if d := r.Destination(); d.Availability != AvailChecking {
		t.Fatalf("partial family answer published: %+v", d)
	}
	svc.mu.Lock()
	svc.games = []hostclient.Game{machineEntry("new-machine", "fes.zx81", ""), titleEntry("new-title", "fes.zx81")}
	svc.computers = []hostclient.ShelfComputer{computerEntry("new-machine", "fes.zx81", "")}
	svc.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(Frame) bool {
		d := r.Destination()
		return homeSettled(r) && d.GameID == "new-machine" && d.Status == "1 game"
	})
	for i := 0; i < 20; i++ {
		r.Step(time.Now())
	}
	if d := r.Destination(); d.GameID != "new-machine" || d.Status != "1 game" {
		t.Fatalf("unfinished old answer replaced refreshed data: %+v", d)
	}
	if _, ok := r.games["old-machine"]; ok {
		t.Fatal("cancelled partial answer entered the game cache")
	}
}

func TestHomeFamilyBatchesIncludeLateMachineEditions(t *testing.T) {
	group := machineEntry("blocked", "fes.zx81", "offline")
	for i := 0; i < 600; i++ {
		group.Variants = append(group.Variants, machineEntry(fmt.Sprintf("blocked-%03d", i), "fes.zx81", "offline"))
	}
	group.Variants = append(group.Variants, machineEntry("ready-edition", "fes.zx81", ""))
	group.VariantCount = len(group.Variants)
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("ready-edition", "fes.zx81", "")}, fakeServices: fakeServices{games: []hostclient.Game{group, titleEntry("game", "fes.zx81")}}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Budget: DefaultBudget()})
	deadline := time.Now().Add(testwait.Timeout)
	for {
		r.pendingMu.Lock()
		queued := len(r.pending)
		r.pendingMu.Unlock()
		if queued >= 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("machine editions were not delivered")
		}
		time.Sleep(time.Millisecond)
	}
	var converted int
	for step := 0; step < 5 && converted == 0; step++ {
		r.Step(time.Now())
		r.pendingMu.Lock()
		for _, result := range r.pending {
			if result.partialGame != nil {
				converted = result.variantIndex
			}
		}
		r.pendingMu.Unlock()
	}
	if converted == 0 || converted >= 128 {
		t.Fatalf("first frame converted %d of 601 editions; want fewer than 128", converted)
	}
	stepUntil(t, r, func(Frame) bool {
		d := r.Destination()
		return homeSettled(r) && d.GameID == "ready-edition" && d.Status == "1 game"
	})
}

func TestHomeFamilyBatchesKeepAdmissionSeparateFromEditionCache(t *testing.T) {
	group := machineEntry("blocked", "fes.zx81", "offline")
	group.Variants = []hostclient.Game{machineEntry("edition", "fes.zx81", "")}
	group.VariantCount = 1
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("edition", "fes.zx81", "Host rejected this edition")}, fakeServices: fakeServices{games: []hostclient.Game{group}}}
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{
		Services: svc, ReleaseOnly: true, Budget: DefaultBudget(),
	})
	r.RefreshCachedGames([]hostclient.Game{machineEntry("edition", "fes.zx81", "offline")})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool {
		d := r.Destination()
		return homeSettled(r) && d.GameID == "edition" && d.Availability == AvailUnavailable && d.Status == "Host rejected this edition"
	})
}

func TestHomeFamilyBatchesPreserveNewerCacheRefresh(t *testing.T) {
	for _, phase := range []string{"before_conversion", "during_conversion", "during_reduction"} {
		t.Run(phase, func(t *testing.T) {
			games := []hostclient.Game{machineEntry("machine", "fes.zx81", "")}
			for i := 0; i < 400; i++ {
				games = append(games, titleEntry(fmt.Sprintf("game-%04d", i), "fes.zx81"))
			}
			svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("machine", "fes.zx81", "Host rejected this machine")}, fakeServices: fakeServices{games: games}}
			r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Budget: DefaultBudget()})
			newer := machineEntry("machine", "fes.zx81", "offline")
			newer.Title = "newer refresh"
			if phase == "before_conversion" {
				r.RefreshCachedGames([]hostclient.Game{newer})
			}
			deadline := time.Now().Add(testwait.Timeout)
			for {
				r.pendingMu.Lock()
				queued := len(r.pending)
				r.pendingMu.Unlock()
				if queued >= 5 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("family responses were not delivered")
				}
				time.Sleep(time.Millisecond)
			}
			r.Step(time.Now())
			if phase == "during_reduction" {
				for i := 0; i < 15; i++ {
					if _, ok := r.CachedGame("machine"); ok {
						break
					}
					r.Step(time.Now())
				}
				if _, ok := r.CachedGame("machine"); !ok || homeSettled(r) {
					t.Fatalf("family reduction was not pending: %+v", r.Destination())
				}
			}
			if phase != "before_conversion" {
				r.RefreshCachedGames([]hostclient.Game{newer})
			}
			stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
			if d := r.Destination(); d.Availability == AvailReady || d.Status != "Host rejected this machine" {
				t.Fatalf("old family answer exposed stale readiness: %+v", d)
			}
			got, ok := r.CachedGame("machine")
			if !ok || got.Title != newer.Title || got.RootOnline != newer.RootOnline {
				t.Fatalf("old family answer replaced newer cache entry: %+v", got)
			}
		})
	}
}

func TestHomeFamilyFullPagesSettleAcrossBoundedFrames(t *testing.T) {
	cores := []string{"fes.zx81", "fes.spectrum", "fes.atari-st", "fes.c64"}
	games := make([]hostclient.Game, 0, len(cores)*2000)
	for _, core := range cores {
		games = append(games, machineEntry(core+"-machine", core, ""))
		for i := 0; i < 1999; i++ {
			games = append(games, titleEntry(fmt.Sprintf("%s-%04d", core, i), core))
		}
	}
	computers := make([]hostclient.ShelfComputer, 0, len(cores))
	for _, core := range cores {
		computers = append(computers, computerEntry(core+"-machine", core, ""))
	}
	svc := &homeServices{computers: computers, fakeServices: fakeServices{games: games}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Budget: DefaultBudget()})
	deadline := time.Now().Add(testwait.Timeout)
	for {
		r.pendingMu.Lock()
		queued := len(r.pending)
		r.pendingMu.Unlock()
		if queued >= len(cores)+1 { // families and recents; no kit is configured
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("full family answers were not delivered")
		}
		time.Sleep(time.Millisecond)
	}
	steps := 0
	now := time.Now()
	var first StepTiming
	var maxAsync, maxUpdate, maxTotal time.Duration
	// Conversion and reduction have independent per-frame work bounds. The
	// smaller Cortex-A9 reduction batch needs more frames, not a larger
	// callback deadline; retain a finite bound derived from the work volume.
	const countBatch = 64
	maxSteps := len(cores) * (2000/familyResultBatch + 2000/countBatch + 4)
	for steps < maxSteps && !homeSettled(r) {
		steps++
		now = now.Add(16 * time.Millisecond)
		r.Step(now)
		timing := r.LastStep()
		maxAsync = max(maxAsync, timing.Async)
		maxUpdate = max(maxUpdate, timing.Update)
		maxTotal = max(maxTotal, timing.Total)
		if steps == 1 {
			first = r.LastStep()
			r.pendingMu.Lock()
			converted := 0
			for _, result := range r.pending {
				converted += result.gameIndex
			}
			r.pendingMu.Unlock()
			if converted == 0 || converted > 128 {
				t.Fatalf("one frame converted %d rows, want 1..128", converted)
			}
		}
		if r.Err() != nil {
			t.Fatalf("room failed: %v", r.Err())
		}
	}
	if steps <= 1 || !homeSettled(r) {
		t.Fatalf("full pages settled after %d frames, destination %+v", steps, r.Destination())
	}
	t.Logf("four full family pages settled in %d update frames (first async %s, update %s; max async %s, update %s, total %s)",
		steps, first.Async, first.Update, maxAsync, maxUpdate, maxTotal)
	for i, core := range cores {
		if i > 0 {
			r.Input("right")
		}
		if d := r.Destination(); d.GameID != core+"-machine" || d.Status != "1999 games" {
			t.Fatalf("family %s lost or reordered results: %+v", core, d)
		}
	}
	// The module must release full response pages once all family summaries
	// have been published; otherwise Home retains thousands of Lua rows.
	module, ok := r.modules["families"].(*lua.LTable)
	if !ok {
		t.Fatal("families module missing")
	}
	update, ok := module.RawGetString("update").(*lua.LFunction)
	if !ok {
		t.Fatal("families update missing")
	}
	var requests *lua.LTable
	for _, upvalue := range update.Upvalues {
		table, ok := upvalue.Value().(*lua.LTable)
		if !ok {
			continue
		}
		first, ok := table.RawGetInt(1).(*lua.LTable)
		if ok && first.RawGetString("family") != lua.LNil {
			requests = table
			break
		}
	}
	if requests == nil {
		t.Fatal("family request state missing")
	}
	requests.ForEach(func(_, value lua.LValue) {
		request, ok := value.(*lua.LTable)
		if !ok || request.RawGetString("data") != lua.LNil || request.RawGetString("pending") != lua.LNil {
			t.Errorf("completed family retained a full response: %v", value)
		}
	})
}

func TestHomeFamilyMachineOverflowFailsClosed(t *testing.T) {
	games := make([]hostclient.Game, 0, 2001)
	for i := 0; i < 2001; i++ {
		games = append(games, machineEntry(fmt.Sprintf("machine-%04d", i), "fes.zx81", ""))
	}
	svc := &homeServices{fakeServices: fakeServices{games: games}}
	local := &fakeLocalCores{cores: []localcores.Core{{PackageID: coreZXID, CoreID: "fes.zx81", Launchable: true}}}
	r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true, Budget: DefaultBudget()})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	d := r.Destination()
	if d.Availability == AvailReady || d.GameID != "" || d.Status != "Too many systems to check" {
		t.Fatalf("overflow must not guess a launchable machine: %+v", d)
	}
}

func TestHomeFamilyMachineBudgetDoesNotHideTitleExistence(t *testing.T) {
	games := make([]hostclient.Game, 0, 2001)
	for i := 0; i < 2000; i++ {
		games = append(games, machineEntry(fmt.Sprintf("machine-%04d", i), "fes.zx81", ""))
	}
	games = append(games, titleEntry("a-title", "fes.zx81"))
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("machine-0000", "fes.zx81", "")}, fakeServices: fakeServices{games: games}}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Budget: DefaultBudget()})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) && r.Destination().GameID != "" })
	if d := r.Destination(); d.Status != "1+ games" || d.Availability != AvailReady {
		t.Fatalf("bounded result hid known title: %+v", d)
	}
}
