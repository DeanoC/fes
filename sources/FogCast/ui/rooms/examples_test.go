package rooms

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

// Every embedded example must load and draw its first frame against a fake
// host without a script error.
func TestEmbeddedExamplesLoadAndDraw(t *testing.T) {
	packs := Examples()
	if len(packs) < 6 {
		t.Fatalf("expected the sample rooms, got %d", len(packs))
	}
	if !examplePack(t, "example.coleco-arcade").Valid() {
		t.Fatal("example.coleco-arcade missing")
	}
	if !examplePack(t, "example.fes-cores").Valid() {
		t.Fatal("example.fes-cores missing")
	}
	svc := &fakeServices{}
	index := NewIndex(packs)
	for _, p := range packs {
		if p.Err != nil {
			t.Fatalf("%s: %v", p.ID, p.Err)
		}
		r := newRoom(t, p, Options{Services: svc, Index: index, Width: 1280, Height: 720})
		if err := r.Load(); err != nil {
			t.Fatalf("%s load: %v", p.ID, err)
		}
		stepUntil(t, r, func(f Frame) bool { return len(f.Ops) > 0 })
		for _, cmd := range []string{"down", "right", "left", "up", "select"} {
			r.Input(cmd)
			if r.Err() != nil {
				t.Fatalf("%s input %s: %v", p.ID, cmd, r.Err())
			}
		}
		r.Step(time.Unix(9, 0))
		if r.Err() != nil {
			t.Fatalf("%s: %v", p.ID, r.Err())
		}
		r.Close()
	}
}

func frameText(f Frame) string {
	var b strings.Builder
	for _, op := range f.Ops {
		if op.Kind == OpText && op.Text != "" {
			b.WriteString(op.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func TestFesCoresRoomRendersTilesAndSkipsBlockedLaunch(t *testing.T) {
	pack := examplePack(t, "example.fes-cores")
	if pack.DestinationPanel != PanelHero {
		t.Fatalf("Home panel %q", pack.DestinationPanel)
	}
	fake := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
		{PackageID: coreColecoID, CoreID: "fes.coleco", Name: "ColecoVision", Needs: "media", Launchable: false, Block: "Needs a cartridge"},
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "firmware", Launchable: false, Block: "Needs firmware", Inputs: []string{"keyboard"}},
	}}
	r := newRoom(t, pack, Options{Local: fake, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "FES Pong\n") && strings.Contains(text, "ColecoVision\n") && strings.Contains(text, "ZX81\n") && strings.Contains(text, "Needs a cartridge\n") && strings.Contains(text, "Needs firmware\n")
	})
	if strings.Contains(frameText(f), "FES.PONG") || strings.Contains(frameText(f), "fes.pong") {
		t.Fatalf("tile renamed the manifest name:\n%s", frameText(f))
	}
	if strings.Contains(frameText(f), "Ready") {
		t.Fatalf("the shelf claimed readiness; the launcher owns it:\n%s", frameText(f))
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.launchedIDs(); len(got) != 1 || got[0] != corePongID {
		t.Fatalf("launch %v", got)
	}
	if !r.Input("right") {
		t.Fatal("right")
	}
	if got := r.Destination(); got.Label != "ColecoVision" || got.PackageID != coreColecoID || got.CoreLaunchable || got.Status != "Needs a cartridge" || got.CoreNeeds != "media" || got.Confirm() != ConfirmExplain {
		t.Fatalf("blocked dest %+v confirm %v", got, got.Confirm())
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.Input("right") {
		t.Fatal("right to firmware")
	}
	if got := r.Destination(); got.Label != "ZX81" || got.Status != "Needs firmware" || len(got.Inputs) != 1 || got.Inputs[0] != "keyboard" {
		t.Fatalf("firmware dest %+v", got)
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.launchedIDs(); len(got) != 1 || got[0] != corePongID {
		t.Fatalf("blocked tiles launched %v", got)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}

	bare := newRoom(t, pack, Options{Width: 1280, Height: 720})
	if err := bare.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, bare, func(f Frame) bool {
		return strings.Contains(frameText(f), "Installed machines appear here on an FES kit.")
	})
	// Without a kit the routes stay reachable: focus starts on Library.
	if got := bare.Destination(); got.Kind != KindLibrary || got.Confirm() != ConfirmOpenLibraryBrowse {
		t.Fatalf("host Home dest %+v", got)
	}
	if err := bare.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}

	empty := &fakeLocalCores{}
	quiet := newRoom(t, pack, Options{Local: empty, Width: 1280, Height: 720})
	if err := quiet.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, quiet, func(f Frame) bool {
		return strings.Contains(frameText(f), "Nothing is installed on this kit yet.")
	})
	if err := quiet.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := empty.launchedIDs(); len(got) != 0 {
		t.Fatalf("empty launched %v", got)
	}
	if got := quiet.Destination(); got.Kind != KindLibrary {
		t.Fatalf("empty Home did not offer the Library: %+v", got)
	}

	broken := &fakeLocalCores{listErr: errors.New("invalid catalog response")}
	failed := newRoom(t, pack, Options{Local: broken, Width: 1280, Height: 720})
	if err := failed.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, failed, func(f Frame) bool {
		return strings.Contains(frameText(f), "This kit's machines can't be listed right now.")
	})
	if failed.Err() != nil {
		t.Fatal(failed.Err())
	}
	if got := failed.Destination(); got.Kind == KindCore || got.Confirm() == ConfirmLaunchCore {
		t.Fatalf("refused socket offered a core %+v", got)
	}
}

// blockingLocalCores holds List until released, like a warming catalog.
type blockingLocalCores struct {
	*fakeLocalCores
	release chan struct{}
}

func (b blockingLocalCores) List(ctx context.Context) ([]localcores.Core, error) {
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.fakeLocalCores.List(ctx)
}

func TestHomeChecksThenRestoresSelectionAndReachesRoutes(t *testing.T) {
	pack := examplePack(t, "example.fes-cores")
	cores := []localcores.Core{
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true, Inputs: []string{"keyboard"}},
	}
	store := filepath.Join(t.TempDir(), "home.json")
	held := blockingLocalCores{fakeLocalCores: &fakeLocalCores{cores: cores}, release: make(chan struct{})}
	r := newRoom(t, pack, Options{Local: held, StorePath: store, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(1, 0))
	// While the list is pending, Home says Checking and Confirm waits.
	if got := r.Destination(); got.Availability != AvailChecking || got.Confirm() != ConfirmWait || got.Status == "" {
		t.Fatalf("pending Home %+v", got)
	}
	// Up reaches the routes during Checking, so Settings never waits on the kit.
	r.Input("up")
	r.Input("right")
	r.Input("right")
	if got := r.Destination(); got.Kind != KindAction || got.LauncherAction != "settings" {
		t.Fatalf("settings route %+v", got)
	}
	r.Input("left")
	if got := r.Destination(); got.Kind != KindRoom || got.RoomID == "" || got.Confirm() != ConfirmEnterRoom {
		t.Fatalf("rooms route %+v", got)
	}
	r.Input("down")
	close(held.release)
	stepUntil(t, r, func(Frame) bool { return r.Destination().Kind == KindCore })
	if got := r.Destination(); got.Label != "FES Pong" {
		t.Fatalf("first visit focus %+v", got)
	}
	r.Input("right")
	if got := r.Destination(); got.Label != "ZX81" {
		t.Fatalf("moved focus %+v", got)
	}
	r.Close()

	again := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: cores}, StorePath: store, Width: 1280, Height: 720})
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, again, func(Frame) bool { return again.Destination().Kind == KindCore })
	if got := again.Destination(); got.Label != "ZX81" {
		t.Fatalf("later visit did not restore the selection: %+v", got)
	}
}

func TestDestinationPanelManifest(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
		ok    bool
	}{{"", PanelStrip, true}, {"strip", PanelStrip, true}, {"hero", PanelHero, true}, {"sidebar", "", false}} {
		m, err := DecodeManifest([]byte("id = \"x\"\ndestination_panel = \"" + tc.value + "\"\n"))
		if (err == nil) != tc.ok || (tc.ok && m.DestinationPanel != tc.want) {
			t.Fatalf("%q: %+v %v", tc.value, m.DestinationPanel, err)
		}
	}
	if _, ok := HeroPanel(400, 300); ok {
		t.Fatal("a tiny room got the hero layout")
	}
	for _, size := range [][2]int{{1280, 720}, {1024, 600}, {1920, 1080}} {
		l, ok := HeroPanel(size[0], size[1])
		if !ok {
			t.Fatalf("%v: no hero layout", size)
		}
		inside := func(a, b PanelRect) bool {
			return a.X >= b.X && a.Y >= b.Y && a.X+a.W <= b.X+b.W && a.Y+a.H <= b.Y+b.H
		}
		room := PanelRect{W: size[0], H: size[1]}
		if !inside(l.Text, l.Card) || !inside(l.Art, l.Card) || !inside(l.Card, room) || !inside(l.Shelf, room) || l.Text.X+l.Text.W > l.Art.X || l.Card.Y+l.Card.H > l.Shelf.Y || l.Shelf.Y+l.Shelf.H > l.Footer.Y {
			t.Fatalf("%v: overlapping layout %+v", size, l)
		}
	}
}

func examplePack(t *testing.T, id string) Pack {
	t.Helper()
	for _, p := range Examples() {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("%s missing", id)
	return Pack{}
}

func TestLobbyWorkbenchTMSPublishDestination(t *testing.T) {
	packs := Examples()
	index := NewIndex(packs)

	lobby := newRoom(t, examplePack(t, "example.lobby"), Options{Services: &fakeServices{}, Index: index, Width: 1280, Height: 720})
	if err := lobby.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, lobby, func(Frame) bool { return lobby.Destination().Kind == KindLibrary })
	if got := lobby.Destination(); got.Label != "Library" || got.Confirm() != ConfirmOpenLibraryBrowse {
		t.Fatalf("lobby library dest %+v", got)
	}
	if !lobby.Input("down") {
		t.Fatal("lobby down")
	}
	lobby.Step(time.Unix(2, 0))
	if got := lobby.Destination(); got.Kind != KindRoom || got.RoomID == "" || got.Action != "Enter room." {
		t.Fatalf("lobby room dest %+v", got)
	}

	svc := &fakeServices{
		games: []hostclient.Game{
			{ID: "coleco-dk", Title: "Donkey Kong", System: "coleco", Genre: "Action", State: "available", RootOnline: true, Launchable: true},
			{ID: "snes-wars", Title: "Wars", System: "snes", Genre: "Strategy", State: "available", RootOnline: true, Launchable: true},
		},
		platforms: []hostclient.Platform{
			{ID: "coleco", Label: "ColecoVision", Tags: []string{"vdp:tms9918-family", "cpu:z80"}},
			{ID: "snes", Label: "SNES", Tags: []string{"cpu:65c816"}},
		},
	}
	bench := newRoom(t, examplePack(t, "example.workbench"), Options{Services: svc, Index: index, Width: 1280, Height: 720})
	if err := bench.Load(); err != nil {
		t.Fatal(err)
	}
	if got := bench.Destination(); got.Kind != KindUnresolved || !got.Set() {
		t.Fatalf("workbench loading dest %+v", got)
	}
	stepUntil(t, bench, func(Frame) bool {
		d := bench.Destination()
		return d.Kind == KindGame && d.GameID != ""
	})
	if got := bench.Destination(); got.Label == "" || got.Availability == AvailChecking {
		t.Fatalf("workbench game dest %+v", got)
	}

	tms := newRoom(t, examplePack(t, "example.tms-vdp"), Options{Services: svc, Index: index, Width: 1280, Height: 720})
	if err := tms.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, tms, func(Frame) bool {
		d := tms.Destination()
		return d.Kind == KindUnresolved && d.Label == "ColecoVision" && d.Availability != AvailChecking
	})
	if !tms.Input("right") {
		t.Fatal("tms right")
	}
	tms.Step(time.Unix(3, 0))
	if got := tms.Destination(); got.Kind != KindGame || got.Label != "Donkey Kong" || got.GameID != "coleco-dk" {
		t.Fatalf("tms game dest %+v", got)
	}
}

func TestColecoArcadeResolvesBySearchAndSkipsMissing(t *testing.T) {
	pack := examplePack(t, "example.coleco-arcade")
	svc := &fakeServices{
		games: []hostclient.Game{
			{ID: "coleco-dk", Title: "Donkey Kong", System: "coleco", State: "available", RootOnline: true, Launchable: true},
			{ID: "coleco-zaxxon", Title: "Zaxxon", System: "coleco", State: "available", RootOnline: true, Launchable: true},
			{ID: "arcade-dk", Title: "Donkey Kong", System: "arcade", State: "available", RootOnline: true, Launchable: true},
			{ID: "snes-mario", Title: "Super Mario World", System: "snes", State: "available", RootOnline: true, Launchable: true},
		},
	}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool {
		d := r.Destination()
		return d.Kind == KindGame && d.GameID != "" && d.Availability != AvailChecking
	})
	wantQ := map[string]bool{"Donkey Kong": true, "Carnival": true, "Zaxxon": true, "Congo Bongo": true, "Frogger": true}
	for _, q := range svc.recorded() {
		if q.Platform != "coleco" {
			t.Fatalf("query platform %q want coleco", q.Platform)
		}
		if q.Q == "" {
			t.Fatal("query missing q")
		}
		if !wantQ[q.Q] {
			t.Fatalf("unexpected q %q", q.Q)
		}
		delete(wantQ, q.Q)
	}
	if len(wantQ) != 0 {
		t.Fatalf("missing title searches %v", wantQ)
	}
	got := r.Destination()
	if got.Kind != KindGame || got.Label != "Donkey Kong" || got.GameID != "coleco-dk" || got.Platform != "coleco" || got.Query != "Donkey Kong" {
		t.Fatalf("focused dest %+v", got)
	}
	if got.Availability != AvailReady || got.Confirm() != ConfirmLaunch {
		t.Fatalf("ready dest %+v confirm %v", got, got.Confirm())
	}
	if !r.Input("down") {
		t.Fatal("down")
	}
	r.Step(time.Unix(3, 0))
	got = r.Destination()
	if got.Label != "Zaxxon" || got.GameID != "coleco-zaxxon" {
		t.Fatalf("skipped missing titles; next dest %+v", got)
	}

	empty := newRoom(t, pack, Options{Services: &fakeServices{}, Width: 1280, Height: 720})
	if err := empty.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, empty, func(Frame) bool {
		d := empty.Destination()
		return d.Kind == KindUnresolved && d.Availability != AvailChecking
	})
	if got := empty.Destination(); got.Label != "ColecoVision Arcade" || got.GameID != "" {
		t.Fatalf("empty library dest %+v", got)
	}
}

func TestMushroomKingdomPlayedIsNotCompleted(t *testing.T) {
	var pack Pack
	for _, p := range Examples() {
		if p.ID == "example.mario-world" {
			pack = p
			break
		}
	}
	if !pack.Valid() {
		t.Fatal("example.mario-world missing")
	}
	svc := &fakeServices{
		games: []hostclient.Game{
			{ID: "arcade-dk", Title: "Donkey Kong", System: "arcade", State: "available", RootOnline: true, Launchable: true, PlayCount: 2, LastPlayedAt: 88},
		},
	}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool {
		return r.Destination().History.Played
	})
	d := r.Destination()
	if d.Label != "Donkey Kong" || d.History.Completed || d.History.Line() != "Played" {
		t.Fatalf("first node %+v history %+v", d, d.History)
	}
	playedFill, err := ParseHexColor("#d4a017")
	if err != nil {
		t.Fatal(err)
	}
	doneFill, err := ParseHexColor("#2fb457")
	if err != nil {
		t.Fatal(err)
	}
	assertNodeFill := func(when string) {
		t.Helper()
		f := r.Step(time.Unix(20, 0))
		// Focused Donkey Kong sits near (128, 488) after the room's scale.
		var fill string
		for _, op := range f.Ops {
			if op.Kind != OpRect || op.W != 44 || op.H != 44 {
				continue
			}
			cx, cy := op.X+22, op.Y+22
			if cx < 100 || cx > 160 || cy < 450 || cy > 530 {
				continue
			}
			fill = FormatHexColor(op.Color)
			break
		}
		if fill == "" {
			t.Fatalf("%s: missing Donkey Kong node fill", when)
		}
		if fill != FormatHexColor(playedFill) {
			t.Fatalf("%s: Donkey Kong fill %s want Played %s", when, fill, FormatHexColor(playedFill))
		}
		if fill == FormatHexColor(doneFill) {
			t.Fatalf("%s: Played title used Completed fill", when)
		}
	}
	assertNodeFill("after match")
	r.Resume()
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	d = r.Destination()
	if d.History.Completed || d.History.Line() == "Completed" {
		t.Fatalf("resume after launch claimed Completed: %+v", d.History)
	}
	if !d.History.Played || d.History.Line() != "Played" {
		t.Fatalf("resume should stay Played: %+v", d.History)
	}
	assertNodeFill("after resume")
}
