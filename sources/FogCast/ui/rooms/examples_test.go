package rooms

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

const colecoFormat4HILPackageID = "506110a2554a80e0952d50ab3d40d2ea27e41ba014d4a91703e7de31bcddfa9c"

type coreLibraryEntrySpec struct {
	title     string
	coreID    string
	packageID string
}

// catalogCoreLibraryGames builds the same metadata shape that the host
// exposes for installed package entries: the catalog platform is "fpga", and
// the selected package/core identity lives on the core-entry row.
func catalogCoreLibraryGames(t *testing.T, specs ...coreLibraryEntrySpec) []hostclient.Game {
	t.Helper()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	games := make([]hostclient.Game, 0, len(specs))
	for _, spec := range specs {
		entry, err := store.CreateCoreEntry(context.Background(), spec.title, spec.coreID, spec.packageID)
		if err != nil {
			t.Fatalf("create installed core entry %q: %v", spec.title, err)
		}
		row, err := store.Game(context.Background(), entry.GameID)
		if err != nil {
			t.Fatalf("read installed catalog row %q: %v", spec.title, err)
		}
		metadata, err := store.CoreEntry(context.Background(), entry.GameID)
		if err != nil {
			t.Fatalf("read installed package metadata %q: %v", spec.title, err)
		}
		if row.System != catalog.CorePlatform || metadata.CoreID != spec.coreID || metadata.PackageID != spec.packageID {
			t.Fatalf("installed package metadata = row %+v entry %+v", row, metadata)
		}
		games = append(games, hostclient.Game{
			ID:         row.ID,
			Title:      row.Title,
			System:     string(row.System),
			CoreID:     metadata.CoreID,
			PackageID:  metadata.PackageID,
			State:      string(row.State),
			RootOnline: row.RootOnline,
			Launchable: true,
			Execution:  "fpga_native",
		})
	}
	return games
}

// Every embedded example must load and draw its first frame against a fake
// host without a script error.
func TestEmbeddedExamplesLoadAndDraw(t *testing.T) {
	packs := Examples()
	want := map[string]bool{
		"example.fes-cores":     true,
		"example.computers":     true,
		"example.system":        true,
		"example.hardware":      true,
		"example.coleco-arcade": true,
		"example.welcome":       true,
		"example.setup-ready":   true,
	}
	if len(packs) != len(want) {
		t.Fatalf("got %d built-in rooms, want %d", len(packs), len(want))
	}
	for _, pack := range packs {
		if !want[pack.ID] {
			t.Fatalf("unexpected built-in room %q", pack.ID)
		}
		delete(want, pack.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing built-in rooms: %v", want)
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

func TestColecoArcadeResolvesInstalledPackageEntriesAndSkipsMissing(t *testing.T) {
	pack := examplePack(t, "example.coleco-arcade")
	packageGames := catalogCoreLibraryGames(t,
		coreLibraryEntrySpec{title: "Donkey Kong", coreID: "fes.coleco", packageID: colecoFormat4HILPackageID},
		coreLibraryEntrySpec{title: "Zaxxon", coreID: "fes.coleco", packageID: colecoFormat4HILPackageID},
		coreLibraryEntrySpec{title: "Donkey Kong", coreID: "fes.c64", packageID: strings.Repeat("c", 64)},
	)
	svc := &fakeServices{
		games: append(packageGames, []hostclient.Game{
			{ID: "arcade-dk", Title: "Donkey Kong", System: "arcade", State: "available", RootOnline: true, Launchable: true},
			{ID: "snes-mario", Title: "Super Mario World", System: "snes", State: "available", RootOnline: true, Launchable: true},
		}...),
	}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool {
		d := r.Destination()
		return d.Kind == KindGame && d.GameID != "" && d.Availability != AvailChecking
	})
	if got := r.Destination(); got.GameID != packageGames[0].ID || got.System != "coleco" {
		t.Fatalf("installed Coleco entry was not projected into the room: %+v", got)
	}
	wantQ := map[string]bool{"Donkey Kong": true, "Carnival": true, "Zaxxon": true, "Congo Bongo": true, "Frogger": true}
	for _, q := range svc.recorded() {
		if q.Platform != string(catalog.CorePlatform) {
			t.Fatalf("query platform %q want %s", q.Platform, catalog.CorePlatform)
		}
		if len(q.CoreIDs) != 1 || q.CoreIDs[0] != "fes.coleco" {
			t.Fatalf("query core IDs %v want [fes.coleco]", q.CoreIDs)
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
	if got.Kind != KindGame || got.Label != "Donkey Kong" || got.GameID != packageGames[0].ID || got.Platform != "coleco" || got.Query != "Donkey Kong" {
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
	if got.Label != "Zaxxon" || got.GameID != packageGames[1].ID {
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

// This fixture can run unchanged on ARM under an operator's maintenance lease.
// It uses the real embedded Lua room and one or eight in-memory installed cores;
// there are no sockets, launch calls, rendering devices or physical transitions.
func TestInstalledCoreRoomDefaultBudgetDwell(t *testing.T) {
	if os.Getenv("FES_ROOMS_DIAGNOSTIC_DWELL") != "1" {
		t.Skip("manual wall-clock diagnostic: set FES_ROOMS_DIAGNOSTIC_DWELL=1 under the kit lease")
	}
	count := 1
	if raw := os.Getenv("FES_ROOMS_DIAGNOSTIC_CORE_COUNT"); raw != "" {
		var err error
		count, err = strconv.Atoi(raw)
		if err != nil || (count != 1 && count != 8) {
			t.Fatal("diagnostic core count must be 1 or 8")
		}
	}
	ids := []string{"fes.zx81", "fes.zx80", "fes.ts1000", "fes.ts1500", "fes.c64", "fes.spectrum", "fes.spectrum128", "fes.atari-st"}
	var cores []localcores.Core
	// Synthetic package identities: these rows exercise room rendering and do
	// not establish acceptance of a physical package.
	for i := 0; i < count; i++ {
		cores = append(cores, localcores.Core{PackageID: fmt.Sprintf("%064x", i+1), CoreID: ids[i], Name: ids[i], Launchable: true})
	}
	fake := &fakeLocalCores{cores: cores}
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{Local: fake, Width: 1280, Height: 720, Budget: DefaultBudget()})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Sinclair ZX81") })
	const frames = 10000
	var total, maximum time.Duration
	for i := 0; i < frames; i++ {
		start := time.Now()
		r.Step(start)
		elapsed := time.Since(start)
		total += elapsed
		if elapsed > maximum {
			maximum = elapsed
		}
		if err := r.Err(); err != nil {
			t.Fatalf("frame=%d elapsed=%s max=%s mean=%s: %v", i, elapsed, maximum, total/time.Duration(i+1), err)
		}
	}
	t.Logf("cores=%d viewport=1280x720 frames=%d max_step=%s mean_step=%s soft_budget=%s hard_budget=%s", count, frames, maximum, total/frames, r.budget.Frame, r.budget.Frame*callbackHardLimitFactor)
}

func BenchmarkInstalledCoreRoomOneInstalledCore(b *testing.B) {
	var pack Pack
	for _, candidate := range Examples() {
		if candidate.ID == "example.fes-cores" {
			pack = candidate
			break
		}
	}
	fake := &fakeLocalCores{cores: []localcores.Core{{PackageID: coreZXID, CoreID: "fes.zx81", Name: "Sinclair ZX81", Launchable: true}}}
	r, err := New(pack, Options{Local: fake, Width: 1280, Height: 720, Budget: DefaultBudget()})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	if err = r.Load(); err != nil {
		b.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for r.Destination().Kind != KindFamily || r.Destination().Availability != AvailReady {
		r.Step(time.Now())
		if r.Err() != nil {
			b.Fatal(r.Err())
		}
		if time.Now().After(deadline) {
			b.Fatal("fixture unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Step(time.Now())
		if r.Err() != nil {
			b.Fatal(r.Err())
		}
	}
}
