package rooms

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
)

type loadingSystemsServices struct {
	*fakeServices
	ready <-chan struct{}
}

func (s loadingSystemsServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, limit int) ([]hostclient.Game, error) {
	select {
	case <-s.ready:
		return s.fakeServices.QueryGames(ctx, q, limit)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestSystemsSetupWaitsForLoadedShelf(t *testing.T) {
	ready := make(chan struct{})
	zx := preparedComputer("zx80-loaded", "My ZX80", "fes.zx80")
	r := newRoom(t, examplePack(t, "example.computers"), Options{
		Services: loadingSystemsServices{fakeServices: &fakeServices{games: []hostclient.Game{zx}}, ready: ready},
		Width:    1280, Height: 720,
	})
	if err := r.Load(); err != nil {
		close(ready)
		t.Fatal(err)
	}
	frame := r.Step(time.Now())
	if !frameHas(frame, "Looking for prepared systems") || frameHas(frame, "Add system") {
		close(ready)
		t.Fatalf("loading shelf offered setup:\n%s", frameText(frame))
	}
	r.Input("right")
	r.Input("select")
	r.Activate("setup")
	acts := r.TakeActions()
	close(ready)
	if len(acts) != 0 {
		t.Fatalf("loading shelf navigated away: %+v", acts)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "My ZX80") })
	r.Input("right")
	r.Input("select")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.hardware" || acts[0].InitialGameID != zx.ID || acts[0].InitialCoreID != zx.CoreID {
		t.Fatalf("loaded shelf setup: %+v", acts)
	}
}

func preparedComputer(id, title, coreID string) hostclient.Game {
	return hostclient.Game{
		ID: id, Title: title, System: "fpga", CoreID: coreID,
		State: "available", RootOnline: true, Launchable: true,
	}
}

func TestSystemsRoomRefreshesAfterNestedSetup(t *testing.T) {
	svc := &fakeServices{}
	r := newRoom(t, examplePack(t, "example.computers"), Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool {
		return frameHas(f, "No prepared systems yet. Add a system to get started.")
	})
	added := preparedComputer("nested-setup", "Newly prepared ZX81", "fes.zx81")
	svc.mu.Lock()
	svc.games = []hostclient.Game{added}
	svc.mu.Unlock()
	r.Resume()
	frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, added.Title) })
	if !frameHas(frame, "Set up") || r.Err() != nil {
		t.Fatalf("resumed Systems shelf did not include the new setup:\n%s\nerror=%v", frameText(frame), r.Err())
	}
}

func TestSystemsRoomResumeKeepsSelectedGameAfterEarlierInsert(t *testing.T) {
	first := preparedComputer("alpha-system", "Alpha setup", "fes.zx81")
	selected := preparedComputer("charlie-system", "Charlie setup", "fes.zx81")
	earlier := preparedComputer("aardvark-system", "Aardvark setup", "fes.zx81")
	svc := &fakeServices{games: []hostclient.Game{first, selected}}
	r := newRoom(t, examplePack(t, "example.computers"), Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, selected.Title) })
	if !r.Input("down") {
		t.Fatal("could not select the second system")
	}

	svc.mu.Lock()
	svc.games = []hostclient.Game{earlier, first, selected}
	svc.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, earlier.Title) })
	if !r.Input("select") {
		t.Fatal("Confirm was not handled")
	}
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != selected.ID {
		t.Fatalf("Confirm launched %+v, want game %q", acts, selected.ID)
	}
}

func TestSystemsRoomFailedRefreshClearsSetupSelection(t *testing.T) {
	game := preparedComputer("old-system", "Old setup", "fes.zx81")
	svc := &fakeServices{games: []hostclient.Game{game}}
	r := newRoom(t, examplePack(t, "example.computers"), Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, game.Title) })

	svc.mu.Lock()
	svc.err = fmt.Errorf("refresh failed")
	svc.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "refresh failed") })
	r.Activate("setup")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.system" || acts[0].InitialGameID != "" {
		t.Fatalf("setup after a failed refresh opened %+v, want Add system", acts)
	}
}

func TestComputersRoomRecoversAfterFailedRefresh(t *testing.T) {
	old := preparedComputer("old-zx81-entry", "Old ZX81 entry", "fes.zx81")
	newer := preparedComputer("new-c64-entry", "New C64 entry", "fes.c64")
	svc := &fakeServices{games: []hostclient.Game{old}}
	r := newRoom(t, examplePack(t, "example.computers"), Options{
		Services: svc, Width: 1280, Height: 720,
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, old.Title) })

	refreshErr := fmt.Errorf("temporary shelf failure")
	svc.mu.Lock()
	svc.err = refreshErr
	svc.mu.Unlock()
	r.Resume()
	failed := stepUntil(t, r, func(f Frame) bool { return frameHas(f, refreshErr.Error()) })
	if frameHas(failed, old.Title) || !frameHas(failed, "Add system") {
		t.Fatalf("failed refresh left stale rows or hid Add system:\n%s", frameText(failed))
	}
	if !r.Input("right") || !r.Input("select") {
		t.Fatal("open Add system after a failed refresh")
	}
	actions := r.TakeActions()
	if len(actions) != 1 || actions[0].Kind != ActionOpenRoom || actions[0].RoomID != "example.system" {
		t.Fatalf("Add system opened a stale workbench: %+v", actions)
	}

	svc.mu.Lock()
	svc.games = []hostclient.Game{newer}
	svc.err = nil
	svc.mu.Unlock()
	r.Resume()
	recovered := stepUntil(t, r, func(f Frame) bool { return frameHas(f, newer.Title) })
	if frameHas(recovered, refreshErr.Error()) || frameHas(recovered, old.Title) || !frameHas(recovered, "Set up") {
		t.Fatalf("successful refresh kept the old error or rows:\n%s", frameText(recovered))
	}
	if !r.Input("select") {
		t.Fatal("open Set up after refresh recovery")
	}
	actions = r.TakeActions()
	if len(actions) != 1 || actions[0].Kind != ActionOpenRoom || actions[0].RoomID != "example.system" || actions[0].InitialGameID != newer.ID || actions[0].InitialCoreID != newer.CoreID {
		t.Fatalf("recovered shelf setup %+v", actions)
	}
}

type delayedFirstSystemsQueryServices struct {
	*fakeServices
	firstResult []hostclient.Game
	started     chan struct{}
	release     chan struct{}
	mu          sync.Mutex
	calls       int
	releaseOnce sync.Once
}

func (s *delayedFirstSystemsQueryServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, limit int) ([]hostclient.Game, error) {
	s.mu.Lock()
	s.calls++
	first := s.calls == 1
	s.mu.Unlock()
	if first {
		close(s.started)
		select {
		case <-s.release:
			return append([]hostclient.Game(nil), s.firstResult...), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.fakeServices.QueryGames(ctx, q, limit)
}

func (s *delayedFirstSystemsQueryServices) releaseFirst() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func TestSystemsRoomIgnoresStaleCatalogRefresh(t *testing.T) {
	old := preparedComputer("old-system", "Old system", "fes.zx81")
	newer := preparedComputer("new-system", "New system", "fes.zx81")
	svc := &delayedFirstSystemsQueryServices{
		fakeServices: &fakeServices{games: []hostclient.Game{newer}},
		firstResult:  []hostclient.Game{old},
		started:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	defer svc.releaseFirst()
	r := newRoom(t, examplePack(t, "example.computers"), Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	waitForSystemsSignal(t, r, svc.started, "initial Systems query")
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, newer.Title) })
	svc.releaseFirst()
	waitForSystemsPending(t, r, false)
	frame := r.Step(time.Now())
	if !frameHas(frame, newer.Title) || frameHas(frame, old.Title) {
		t.Fatalf("stale query replaced the resumed Systems shelf:\n%s", frameText(frame))
	}
}

type delayedSystemsGameServices struct {
	*fakeServices
	blockedID   string
	detail      hostclient.Game
	started     chan struct{}
	release     chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func (s *delayedSystemsGameServices) Game(ctx context.Context, id string) (hostclient.Game, error) {
	if id != s.blockedID {
		return s.fakeServices.Game(ctx, id)
	}
	s.startOnce.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return s.detail, nil
	case <-ctx.Done():
		return hostclient.Game{}, ctx.Err()
	}
}

func (s *delayedSystemsGameServices) releaseDetail() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func TestSystemsRoomIgnoresStaleEditionRefresh(t *testing.T) {
	old := preparedComputer("old-group", "Old grouped system", "fes.zx81")
	old.VariantCount = 2
	detail := old
	detail.Title = "Old detailed system"
	newer := preparedComputer("new-system", "New system after setup", "fes.zx81")
	svc := &delayedSystemsGameServices{
		fakeServices: &fakeServices{games: []hostclient.Game{old}},
		blockedID:    old.ID,
		detail:       detail,
		started:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	defer svc.releaseDetail()
	r := newRoom(t, examplePack(t, "example.computers"), Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	waitForSystemsSignal(t, r, svc.started, "grouped-edition lookup")
	svc.fakeServices.mu.Lock()
	svc.fakeServices.games = []hostclient.Game{newer}
	svc.fakeServices.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, newer.Title) })
	svc.releaseDetail()
	waitForSystemsPending(t, r, true)
	frame := r.Step(time.Now())
	if !frameHas(frame, newer.Title) || frameHas(frame, old.Title) || frameHas(frame, detail.Title) {
		t.Fatalf("stale edition callback replaced the resumed Systems shelf:\n%s", frameText(frame))
	}
}

func waitForSystemsSignal(t *testing.T, r *Instance, signal <-chan struct{}, description string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.Step(time.Now())
		select {
		case <-signal:
			return
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatalf("%s did not arrive", description)
}

func waitForSystemsPending(t *testing.T, r *Instance, game bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.pendingMu.Lock()
		found := false
		for _, result := range r.pending {
			if (game && result.game != nil) || (!game && result.games != nil) {
				found = true
				break
			}
		}
		r.pendingMu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Systems callback did not arrive")
}

func TestSystemsRoomListsPreparedWorkingFamilies(t *testing.T) {
	pack := examplePack(t, "example.computers")
	if !pack.Valid() || pack.Title != "Systems" {
		t.Fatalf("pack %+v", pack.Manifest)
	}
	expansion := strings.Repeat("ab", 32)
	slotID := strings.Repeat("cd", 32)
	zx := preparedComputer("fpga-zx81-maze", "3D Monster Maze", "fes.zx81")
	zx.ExpansionID = expansion
	zx.ExpansionReady = true
	zx.VariantCount = 2
	japan := preparedComputer("fpga-zx81-jp", "3D Monster Maze", "fes.zx81")
	japan.Region = "Japan"
	zx.Variants = []hostclient.Game{japan}
	bare := preparedComputer("fpga-zx81-bare", "Bare ZX81", "fes.zx81")
	bare.FirmwareRequired = true
	bare.FirmwareReady = false
	c64 := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	c64.SlotExpansions = []hostclient.SlotExpansion{{Slot: 1, ExpansionID: slotID, Ready: true}}
	pong := preparedComputer("fpga-pong", "FES Pong", "fes.pong")
	apple := preparedComputer("fpga-apple", "Apple II", "fes.apple2")
	coleco := preparedComputer("fpga-coleco", "Donkey Kong", "fes.coleco")

	games := make([]hostclient.Game, 0, 2007)
	for i := 0; i < 2001; i++ {
		games = append(games, preparedComputer(fmt.Sprintf("fpga-other-%04d", i), fmt.Sprintf("0000 unrelated %04d", i), "fes.other"))
	}
	games = append(games, zx, bare, c64, pong, apple, coleco)
	svc := &fakeServices{games: games}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "3D Monster Maze") && strings.Contains(text, "Commodore 64") && strings.Contains(text, "Japan")
	})
	text := frameText(frame)
	for _, want := range []string{"ZX81", "3D Monster Maze", "Japan", "Commodore 64", "Expansion " + expansion[:8], "Slot 1 " + slotID[:8]} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame missing %q\n%s", want, text)
		}
	}
	for _, absent := range []string{expansion, slotID, "Bare ZX81", "0000 unrelated", "16K", "REU", "Zon X"} {
		if strings.Contains(text, absent) {
			t.Fatalf("frame includes %q\n%s", absent, text)
		}
	}
	if len(frame.Ops) >= 80 {
		t.Fatalf("computers draw used %d ops", len(frame.Ops))
	}
	assertRowContrast(t, frame)
	queries := svc.recorded()
	if len(queries) != 1 || queries[0].Platform != "fpga" || queries[0].Sort != "title" || len(queries[0].CoreIDs) != 18 {
		t.Fatalf("queries %+v", queries)
	}
	if got := r.Destination(); got.Kind != "" || got.GameID != "" {
		t.Fatalf("computers published %+v", got)
	}

	wantIDs := []string{
		"fpga-zx81-maze",
		"fpga-zx81-jp",
		"fpga-c64-game",
	}
	for i, id := range wantIDs {
		if i > 0 {
			if !r.Input("down") {
				t.Fatalf("down %d", i)
			}
			if acts := r.TakeActions(); len(acts) != 0 {
				t.Fatalf("down launched %+v", acts)
			}
		}
		if !r.Input("select") {
			t.Fatalf("select %d", i)
		}
		acts := r.TakeActions()
		if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != id {
			t.Fatalf("row %d launch %+v, want %s", i, acts, id)
		}
	}
	r.Activate("computers:1")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-zx81-maze" {
		t.Fatalf("pointer launch %+v", acts)
	}
	r.Step(time.Unix(3, 0))
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func TestComputersRoomLoadsEditionsWhenTheListOmitsThem(t *testing.T) {
	pack := examplePack(t, "example.computers")
	list := preparedComputer("fpga-zx81-maze", "3D Monster Maze", "fes.zx81")
	list.VariantCount = 2
	detail := list
	japan := preparedComputer("fpga-zx81-jp", "3D Monster Maze", "fes.zx81")
	japan.Region = "Japan"
	detail.Variants = []hostclient.Game{japan}
	svc := &fakeServices{
		games:   []hostclient.Game{list},
		details: map[string]hostclient.Game{list.ID: detail},
	}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Japan")
	})
	text := frameText(frame)
	if !strings.Contains(text, "ZX81") || !strings.Contains(text, "3D Monster Maze") {
		t.Fatalf("frame\n%s", text)
	}
	if !r.Input("down") || !r.Input("select") {
		t.Fatal("variant select")
	}
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-zx81-jp" {
		t.Fatalf("edition launch %+v", acts)
	}
}

func TestComputersRoomEmptyLibraryDoesNotLaunch(t *testing.T) {
	pack := examplePack(t, "example.computers")
	r := newRoom(t, pack, Options{Services: &fakeServices{}, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "No prepared systems yet. Add a system to get started.")
	})
	if len(frame.Ops) >= 80 {
		t.Fatalf("empty computers draw used %d ops", len(frame.Ops))
	}
	assertRowContrast(t, frame)
	r.Input("select")
	r.Input("right")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("empty select launched %+v", acts)
	}
	if !strings.Contains(frameText(frame), "Add system") {
		t.Fatal("empty shelf did not offer setup")
	}
	r.Input("select")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.system" {
		t.Fatalf("empty setup %+v", acts)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func TestComputersRoomSetupOpensTheWorkbench(t *testing.T) {
	pack := examplePack(t, "example.computers")
	zx := preparedComputer("fpga-zx81-maze", "3D Monster Maze", "fes.zx81")
	c64 := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	c64Second := preparedComputer("fpga-c64-second", "Second C64 title", "fes.c64")
	svc := &fakeServices{games: []hostclient.Game{zx, c64, c64Second}}
	r := newRoom(t, pack, Options{Services: svc, Index: NewIndex(Examples()), Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "Commodore 64") && strings.Contains(text, "Set up")
	})
	assertRowContrast(t, frame)
	if !r.Input("right") {
		t.Fatal("right did not focus Set up")
	}
	focused := r.Step(time.Unix(2, 0))
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	assertRowContrast(t, focused)
	if !r.Input("select") {
		t.Fatal("select on Set up")
	}
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.hardware" || acts[0].InitialGameID != "fpga-zx81-maze" {
		t.Fatalf("ZX81 setup %+v", acts)
	}
	if !r.Input("left") || !r.Input("down") || !r.Input("right") || !r.Input("select") {
		t.Fatal("move to Commodore 64 setup")
	}
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.system" || acts[0].InitialGameID != "fpga-c64-game" || acts[0].InitialCoreID != "fes.c64" {
		t.Fatalf("C64 setup %+v", acts)
	}
	if !r.Input("left") || !r.Input("down") || !r.Input("right") || !r.Input("select") {
		t.Fatal("move to the second Commodore 64 setup")
	}
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.system" || acts[0].InitialGameID != "fpga-c64-second" || acts[0].InitialCoreID != "fes.c64" {
		t.Fatalf("second C64 setup %+v", acts)
	}
	if !r.Input("left") || !r.Input("select") {
		t.Fatal("confirm still launches")
	}
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-c64-second" {
		t.Fatalf("launch %+v", acts)
	}
	r.Activate("setup")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.system" || acts[0].InitialGameID != "fpga-c64-second" || acts[0].InitialCoreID != "fes.c64" {
		t.Fatalf("pointer setup %+v", acts)
	}
}

// assertRowContrast checks every text run against the plate behind it.
// Unfocused Computers rows used the sofa's near-white label on a light page.
func assertRowContrast(t *testing.T, frame Frame) {
	t.Helper()
	var rows int
	for _, op := range frame.Ops {
		if op.Kind != OpText || op.Text == "" {
			continue
		}
		plate := frame.Clear
		for _, under := range frame.Ops {
			if under.Kind != OpRect {
				continue
			}
			if op.X >= under.X && op.Y >= under.Y && op.X < under.X+under.W && op.Y < under.Y+under.H {
				plate = under.Color
			}
		}
		ratio := contrastRatio(op.Color, plate)
		if ratio < 7 {
			t.Fatalf("text %q contrast %.2f against plate #%02x%02x%02x", op.Text, ratio, plate.R, plate.G, plate.B)
		}
		if op.Size >= 22 {
			rows++
		}
	}
	if rows == 0 && !strings.Contains(frameText(frame), "No prepared") {
		t.Fatal("no row text to check")
	}
}

func contrastRatio(a, b gfx.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relativeLuminance(c gfx.Color) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}
