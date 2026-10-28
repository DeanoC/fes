package rooms

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

func TestFesCoresRoomRendersTilesRoutesColecoAndSkipsBlockedLaunch(t *testing.T) {
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
		return strings.Contains(text, "FES Pong\n") && strings.Contains(text, "ColecoVision\n") && strings.Contains(text, "Browse Coleco titles\n") && strings.Contains(text, "ZX81\n") && strings.Contains(text, "Needs firmware\n")
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
	if got := r.Destination(); got.Label != "ColecoVision" || got.Kind != KindRoom || got.RoomID != "example.coleco-arcade" || got.Confirm() != ConfirmEnterRoom {
		t.Fatalf("coleco destination %+v confirm %v", got, got.Confirm())
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

const coreC64ShelfID = "2222222222222222222222222222222222222222222222222222222222222222"

type homeComputerServices struct {
	fakeServices
	computers     []hostclient.ShelfComputer
	hardwareReads int
}

func (s *homeComputerServices) Hardware(context.Context) (hostclient.HardwareSnapshot, error) {
	s.hardwareReads++
	return hostclient.HardwareSnapshot{Computers: append([]hostclient.ShelfComputer(nil), s.computers...)}, nil
}

func (s *homeComputerServices) SelectCoreEntryExpansion(context.Context, string, string, string, string) (hostclient.CoreEntryExpansion, error) {
	return hostclient.CoreEntryExpansion{}, errors.New("not used")
}

func TestHomeRoutesColecoTileBesideHostComputers(t *testing.T) {
	pack := examplePack(t, "example.fes-cores")
	local := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: coreColecoID, CoreID: "fes.coleco", Name: "FES ColecoVision", Needs: "parts", Launchable: false, Block: "Needs a video part"},
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "firmware", Launchable: false, Block: "Needs machine ROM", Inputs: []string{"keyboard"}},
	}}
	svc := &homeComputerServices{computers: []hostclient.ShelfComputer{
		{GameID: "fpga-c64", Title: "Commodore 64", CoreID: "fes.c64", PackageID: coreC64ShelfID, Ready: true},
		{GameID: "fpga-zx81", Title: "ZX81", CoreID: "fes.zx81", PackageID: coreZXID, Ready: true},
	}}
	r := newRoom(t, pack, Options{Local: local, Services: svc, Width: 1280, Height: 720, Budget: DefaultBudget()})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "FES ColecoVision\n") && strings.Contains(text, "Browse Coleco titles\n") && strings.Contains(text, "Commodore 64\n") && strings.Contains(text, "ZX81\n")
	})
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if strings.Contains(frameText(f), "Keyboard") {
		t.Fatalf("host computer drew a keyboard motif:\n%s", frameText(f))
	}
	if got := r.Destination(); got.Label != "FES ColecoVision" || got.Kind != KindRoom || got.RoomID != "example.coleco-arcade" || got.Confirm() != ConfirmEnterRoom {
		t.Fatalf("coleco dest %+v confirm %v", got, got.Confirm())
	}
	if !r.Input("right") {
		t.Fatal("right to zx81")
	}
	if got := r.Destination(); got.Kind != KindGame || got.Label != "ZX81" || got.GameID != "fpga-zx81" || got.Confirm() != ConfirmLaunch || len(got.Inputs) != 0 {
		t.Fatalf("zx81 dest %+v confirm %v", got, got.Confirm())
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.Input("right") {
		t.Fatal("right to c64")
	}
	if got := r.Destination(); got.Kind != KindGame || got.Label != "Commodore 64" || got.GameID != "fpga-c64" || got.Confirm() != ConfirmLaunch {
		t.Fatalf("c64 dest %+v confirm %v", got, got.Confirm())
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := local.launchedIDs(); len(got) != 0 {
		t.Fatalf("host cards used local launch %v", got)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}

	kept := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true, Inputs: []string{"keyboard"}},
	}}
	keptRoom := newRoom(t, pack, Options{Local: kept, Services: svc, Width: 1280, Height: 720, Budget: DefaultBudget()})
	if err := keptRoom.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, keptRoom, func(f Frame) bool {
		return strings.Contains(frameText(f), "ZX81\n") && strings.Contains(frameText(f), "Keyboard\n")
	})
	if got := keptRoom.Destination(); got.Kind != KindCore || got.Confirm() != ConfirmLaunchCore || got.PackageID != coreZXID {
		t.Fatalf("launchable local zx81 was replaced: %+v confirm %v", got, got.Confirm())
	}
	if keptRoom.Err() != nil {
		t.Fatal(keptRoom.Err())
	}
}

func TestHomeListsHostComputersWhenKitListingFails(t *testing.T) {
	pack := examplePack(t, "example.fes-cores")
	local := &fakeLocalCores{listErr: errors.New("invalid catalog response")}
	services := &homeComputerServices{computers: []hostclient.ShelfComputer{
		{GameID: "fpga-c64", Title: "Commodore 64", CoreID: "fes.c64", PackageID: coreC64ShelfID, Ready: true},
	}}
	r := newRoom(t, pack, Options{Local: local, Services: services, Width: 1280, Height: 720, Budget: DefaultBudget()})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Commodore 64\n")
	})
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if services.hardwareReads != 1 {
		t.Fatalf("host shelf read %d times after kit listing failure", services.hardwareReads)
	}
	if got := r.Destination(); got.Kind != KindGame || got.GameID != "fpga-c64" || got.Confirm() != ConfirmLaunch {
		t.Fatalf("host card unavailable after kit listing failure: %+v confirm=%v", got, got.Confirm())
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := local.launchedIDs(); len(got) != 0 {
		t.Fatalf("host card used kit-local launch: %v", got)
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

func TestFesCoresSmallLayoutKeepsPointerRoutes(t *testing.T) {
	if _, ok := HeroPanel(470, 350); ok {
		t.Fatal("470x350 used the hero layout")
	}
	pack := examplePack(t, "example.fes-cores")
	fake := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
		{PackageID: coreColecoID, CoreID: "fes.coleco", Name: "ColecoVision", Needs: "media", Launchable: false, Block: "Needs a cartridge"},
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "firmware", Launchable: false, Block: "Needs firmware"},
	}}
	r := newRoom(t, pack, Options{Local: fake, Width: 470, Height: 350})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := stepUntil(t, r, func(f Frame) bool {
		return hitByID(f, "core:2").ID != "" && hitByID(f, "menu:3").ID != "" && strings.Contains(frameText(f), "ColecoVision")
	})
	limit := compactStripTop(470, 350)
	accent, err := ParseHexColor("#235A46")
	if err != nil {
		t.Fatal(err)
	}
	var focusedBold, accentMark bool
	for _, op := range f.Ops {
		if op.Kind == OpText && op.Text == "FES Pong" && op.Bold {
			focusedBold = true
		}
		if op.Kind == OpRect && op.Color == accent {
			accentMark = true
		}
	}
	if !focusedBold || !accentMark {
		t.Fatal("compact list has no visible focus")
	}
	for _, id := range []string{"core:1", "core:2", "core:3", "menu:1", "menu:2", "menu:3"} {
		h := hitByID(f, id)
		if h.ID == "" || h.W < 1 || h.H < 1 {
			t.Fatalf("missing hit %s in %+v", id, f.Hits)
		}
		if h.X < 0 || h.Y < 0 || h.X+h.W > 471 || h.Y+h.H > float32(limit)+1 {
			t.Fatalf("hit %s %+v crosses the strip at %d", id, h, limit)
		}
	}
	r.Hover("core:2")
	if got := r.Destination(); got.Label != "ColecoVision" || got.Kind != KindRoom || got.RoomID != "example.coleco-arcade" {
		t.Fatalf("core hit %+v", got)
	}
	r.Hover("menu:1")
	if got := r.Destination(); got.Kind != KindLibrary {
		t.Fatalf("library hit %+v", got)
	}
	r.Hover("menu:2")
	if got := r.Destination(); got.Kind != KindRoom || got.RoomID == "" {
		t.Fatalf("rooms hit %+v", got)
	}
	r.Activate("menu:3")
	if got := r.Destination(); got.Kind != KindAction || got.LauncherAction != "settings" {
		t.Fatalf("settings hit %+v", got)
	}
	if got := fake.launchedIDs(); len(got) != 0 {
		t.Fatalf("pointer navigation launched %v", got)
	}
}

// Direction on a stacked compact column follows the rows on screen.
// Library, Rooms and Settings sit on top of each other, so Down walks
// that column and only then enters the shelf. The hero and a compact
// row that still fits keep Left and Right.
func TestFesCoresDirectionFollowsStackedRoutes(t *testing.T) {
	pack := examplePack(t, "example.fes-cores")
	cores := []localcores.Core{
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
		{PackageID: coreColecoID, CoreID: "fes.coleco", Name: "ColecoVision", Needs: "media", Launchable: false, Block: "Needs a cartridge"},
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "firmware", Launchable: false, Block: "Needs firmware"},
	}

	t.Run("stacked column", func(t *testing.T) {
		r := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: cores}, Width: 300, Height: 350})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		f := stepUntil(t, r, func(f Frame) bool {
			return hitByID(f, "core:1").ID != "" && hitByID(f, "menu:3").ID != "" && r.Destination().Label == "FES Pong"
		})
		library, rooms, settings := hitByID(f, "menu:1"), hitByID(f, "menu:2"), hitByID(f, "menu:3")
		shelf := hitByID(f, "core:1")
		if !(library.Y < rooms.Y && rooms.Y < settings.Y && settings.Y+settings.H <= shelf.Y) {
			t.Fatalf("routes are not stacked above the shelf: library %+v rooms %+v settings %+v shelf %+v", library, rooms, settings, shelf)
		}
		// Devin's 300×350 case: Down from Library must reach Rooms, not the core underneath the column.
		r.Hover("menu:1")
		if got := r.Destination(); got.Kind != KindLibrary {
			t.Fatalf("library %+v", got)
		}
		if !r.Input("right") || r.Destination().Kind != KindLibrary {
			t.Fatalf("right left the stacked column for %+v", r.Destination())
		}
		if !r.Input("down") {
			t.Fatal("down from library")
		}
		if got := r.Destination(); got.Kind != KindRoom || got.RoomID == "" {
			t.Fatalf("down from Library skipped to %+v", got)
		}
		if !r.Input("down") {
			t.Fatal("down to settings")
		}
		if got := r.Destination(); got.Kind != KindAction || got.LauncherAction != "settings" {
			t.Fatalf("settings %+v", got)
		}
		if !r.Input("left") || r.Destination().LauncherAction != "settings" {
			t.Fatalf("left left Settings for %+v", r.Destination())
		}
		if !r.Input("down") {
			t.Fatal("down into the shelf")
		}
		if got := r.Destination(); got.Label != "FES Pong" {
			t.Fatalf("shelf under the column %+v", got)
		}
		if !r.Input("down") {
			t.Fatal("down the shelf")
		}
		if got := r.Destination(); got.Label != "ColecoVision" {
			t.Fatalf("next core %+v", got)
		}
		if !r.Input("right") || r.Destination().Label != "ColecoVision" {
			t.Fatalf("right moved the stacked shelf to %+v", r.Destination())
		}
		if !r.Input("up") || r.Destination().Label != "FES Pong" {
			t.Fatalf("up the shelf %+v", r.Destination())
		}
		if !r.Input("up") {
			t.Fatal("up to the column")
		}
		if got := r.Destination(); got.Kind != KindAction || got.LauncherAction != "settings" {
			t.Fatalf("up from the first core %+v", got)
		}
		if !r.Input("up") || r.Destination().Kind != KindRoom {
			t.Fatalf("up the column %+v", r.Destination())
		}
	})

	// A taller stacked screen shows several cores. Down from Settings
	// enters the core directly under the column, not a later selection.
	t.Run("stacked shelf boundary", func(t *testing.T) {
		r := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: cores}, Width: 300, Height: 500})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		f := stepUntil(t, r, func(f Frame) bool {
			return hitByID(f, "core:1").ID != "" && hitByID(f, "core:3").ID != "" && hitByID(f, "menu:3").ID != ""
		})
		top, mid, last := hitByID(f, "core:1"), hitByID(f, "core:2"), hitByID(f, "core:3")
		settings := hitByID(f, "menu:3")
		if !(settings.Y+settings.H <= top.Y && top.Y < mid.Y && mid.Y < last.Y) {
			t.Fatalf("shelf is not under the column: settings %+v cores %+v %+v %+v", settings, top, mid, last)
		}
		r.Hover("core:3")
		r.Hover("menu:3")
		if got := r.Destination(); got.LauncherAction != "settings" {
			t.Fatalf("settings %+v", got)
		}
		if !r.Input("down") || r.Destination().Label != "FES Pong" {
			t.Fatalf("down from Settings skipped to %+v", r.Destination())
		}
		if !r.Input("down") || r.Destination().Label != "ColecoVision" {
			t.Fatalf("second core %+v", r.Destination())
		}
		if !r.Input("up") || r.Destination().Label != "FES Pong" {
			t.Fatalf("up skipped to %+v", r.Destination())
		}
	})

	t.Run("stacked while listing", func(t *testing.T) {
		held := blockingLocalCores{fakeLocalCores: &fakeLocalCores{cores: cores}, release: make(chan struct{})}
		r := newRoom(t, pack, Options{Local: held, Width: 300, Height: 350})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		r.Step(time.Unix(1, 0))
		if got := r.Destination(); got.Availability != AvailChecking {
			t.Fatalf("pending %+v", got)
		}
		if !r.Input("up") {
			t.Fatal("up")
		}
		if got := r.Destination(); got.Kind != KindAction || got.LauncherAction != "settings" {
			t.Fatalf("up from the checking shelf %+v", got)
		}
		if !r.Input("down") {
			t.Fatal("down")
		}
		if got := r.Destination(); got.Kind != KindUnresolved || got.Availability != AvailChecking {
			t.Fatalf("return to the checking shelf %+v", got)
		}
		close(held.release)
		stepUntil(t, r, func(Frame) bool { return r.Destination().Label == "FES Pong" })
	})

	t.Run("stacked empty kit stays on the column", func(t *testing.T) {
		r := newRoom(t, pack, Options{Width: 300, Height: 350})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, r, func(f Frame) bool {
			return strings.Contains(frameText(f), "Installed machines appear here on an FES kit.") && hitByID(f, "menu:3").ID != ""
		})
		if got := r.Destination(); got.Kind != KindLibrary {
			t.Fatalf("empty focus %+v", got)
		}
		r.Input("down")
		r.Input("down")
		if got := r.Destination(); got.Kind != KindAction || got.LauncherAction != "settings" {
			t.Fatalf("settings %+v", got)
		}
		r.Input("down")
		r.Input("right")
		if got := r.Destination(); got.Kind != KindAction || got.LauncherAction != "settings" {
			t.Fatalf("empty shelf took focus %+v", got)
		}
	})

	t.Run("unstacked row stays horizontal", func(t *testing.T) {
		r := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: cores}, Width: 470, Height: 350})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		f := stepUntil(t, r, func(f Frame) bool {
			return hitByID(f, "menu:3").ID != "" && r.Destination().Label == "FES Pong"
		})
		library, rooms, settings := hitByID(f, "menu:1"), hitByID(f, "menu:2"), hitByID(f, "menu:3")
		if library.Y != rooms.Y || rooms.Y != settings.Y || !(library.X < rooms.X && rooms.X < settings.X) {
			t.Fatalf("routes are not one row: %+v %+v %+v", library, rooms, settings)
		}
		r.Hover("menu:1")
		if !r.Input("down") || r.Destination().Label != "FES Pong" {
			t.Fatalf("down from the row %+v", r.Destination())
		}
		r.Hover("menu:1")
		if !r.Input("right") || r.Destination().Kind != KindRoom {
			t.Fatalf("right %+v", r.Destination())
		}
		if !r.Input("right") || r.Destination().LauncherAction != "settings" {
			t.Fatalf("settings %+v", r.Destination())
		}
		if !r.Input("left") || r.Destination().Kind != KindRoom {
			t.Fatalf("left %+v", r.Destination())
		}
		if !r.Input("down") || r.Destination().Label != "FES Pong" {
			t.Fatalf("down to the shelf %+v", r.Destination())
		}
		if !r.Input("right") || r.Destination().Label != "ColecoVision" {
			t.Fatalf("shelf right %+v", r.Destination())
		}
		if !r.Input("up") || r.Destination().Kind != KindRoom {
			t.Fatalf("up restored the route %+v", r.Destination())
		}
	})

	t.Run("hero stays horizontal", func(t *testing.T) {
		r := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: cores}, Width: 1280, Height: 720})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		f := stepUntil(t, r, func(f Frame) bool {
			return hitByID(f, "menu:2").ID != "" && r.Destination().Label == "FES Pong"
		})
		library, rooms := hitByID(f, "menu:1"), hitByID(f, "menu:2")
		if library.Y != rooms.Y || !(library.X < rooms.X) {
			t.Fatalf("hero routes are not one row: %+v %+v", library, rooms)
		}
		r.Hover("menu:1")
		if !r.Input("right") || r.Destination().Kind != KindRoom {
			t.Fatalf("hero right %+v", r.Destination())
		}
		if !r.Input("down") || r.Destination().Label != "FES Pong" {
			t.Fatalf("hero down %+v", r.Destination())
		}
		if !r.Input("right") || r.Destination().Label != "ColecoVision" {
			t.Fatalf("hero shelf %+v", r.Destination())
		}
		if !r.Input("up") || r.Destination().Kind != KindRoom {
			t.Fatalf("hero up %+v", r.Destination())
		}
	})
}

func hitByID(f Frame, id string) Hit {
	for _, h := range f.Hits {
		if h.ID == id {
			return h
		}
	}
	return Hit{}
}

// compactStripTop mirrors roomDestGeom's reserved band. Hits have to stay
// above it or the strip takes the click.
func compactStripTop(w, h int) int {
	if w < 152 {
		return h
	}
	y := h - 118 - 8
	if y < 48 {
		y = 48
	}
	if h-8-y < 72 {
		return h
	}
	return y
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

func TestFesCoresRoomShowsHardwareUnavailable(t *testing.T) {
	const atariID = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	pack := examplePack(t, "example.fes-cores")
	fake := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
		{PackageID: atariID, CoreID: "fes.atari-st", Name: "Atari ST", Needs: "unavailable", Launchable: false, Block: "Hardware unavailable"},
	}}
	r := newRoom(t, pack, Options{Local: fake, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "Atari ST\n") && strings.Contains(text, "Hardware unavailable\n")
	})
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.Input("right") {
		t.Fatal("right")
	}
	if got := r.Destination(); got.Label != "Atari ST" || got.PackageID != atariID || got.CoreLaunchable || got.Status != "Hardware unavailable" {
		t.Fatalf("unavailable dest %+v", got)
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.launchedIDs(); len(got) != 1 || got[0] != corePongID {
		t.Fatalf("unavailable tile launched %v", got)
	}
}

func writeRoomPackage(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home.json")
	if err := os.WriteFile(path, []byte("{\n  \"package_id\": \""+id+"\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestHomeFocusesTheFirstInstalledCore is the fresh-image case: the omitted
// Atari ST sorts first, and Home must not open on it. A saved available
// package is still restored, including after the omitted tile is selected.
func TestHomeFocusesTheFirstInstalledCore(t *testing.T) {
	const atariID = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	pack := examplePack(t, "example.fes-cores")
	cores := []localcores.Core{
		{PackageID: atariID, CoreID: "fes.atari-st", Name: "Atari ST", Needs: "unavailable", Launchable: false, Block: "Hardware unavailable"},
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true},
	}
	open := func(t *testing.T, store string) *Instance {
		t.Helper()
		r := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: cores}, StorePath: store, Width: 1280, Height: 720})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		return r
	}
	fresh := open(t, "")
	f := stepUntil(t, fresh, func(Frame) bool {
		return strings.Contains(frameText(fresh.Frame()), "2 installed") && fresh.Destination().Label == "FES Pong"
	})
	if strings.Contains(frameText(f), "3 installed") || fresh.Destination().CoreID == "fes.atari-st" {
		t.Fatalf("fresh Home opened on the omitted core:\n%s\n%+v", frameText(f), fresh.Destination())
	}
	fresh.Close()

	saved := writeRoomPackage(t, coreZXID)
	again := open(t, saved)
	stepUntil(t, again, func(Frame) bool { return again.Destination().Label == "ZX81" })
	again.Activate("core:1")
	if got := again.Destination(); got.Label != "Atari ST" || got.CoreNeeds != "unavailable" {
		t.Fatalf("could not inspect the omitted core %+v", got)
	}
	again.Close()
	kept := open(t, saved)
	stepUntil(t, kept, func(Frame) bool { return kept.Destination().Kind == KindCore })
	if got := kept.Destination(); got.Label != "ZX81" || got.PackageID != coreZXID {
		t.Fatalf("selecting the omitted core replaced the saved package: %+v", got)
	}
	kept.Close()

	miss := open(t, writeRoomPackage(t, atariID))
	stepUntil(t, miss, func(Frame) bool { return miss.Destination().Kind == KindCore })
	if got := miss.Destination(); got.Label != "FES Pong" || strings.Contains(frameText(miss.Frame()), "3 installed") {
		t.Fatalf("saved omitted core stayed focused: %+v\n%s", got, frameText(miss.Frame()))
	}

	only := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: cores[:1]}, Width: 1280, Height: 720})
	if err := only.Load(); err != nil {
		t.Fatal(err)
	}
	of := stepUntil(t, only, func(f Frame) bool { return strings.Contains(frameText(f), "Atari ST\n") })
	if got := only.Destination(); got.Kind != KindLibrary || strings.Contains(frameText(of), "installed") {
		t.Fatalf("only omitted cores still opened as installed: %+v\n%s", got, frameText(of))
	}

	firmwareFirst := []localcores.Core{
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "firmware", Launchable: false, Block: "Needs firmware"},
		cores[0],
	}
	blocked := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: firmwareFirst}, Width: 1280, Height: 720})
	if err := blocked.Load(); err != nil {
		t.Fatal(err)
	}
	bf := stepUntil(t, blocked, func(Frame) bool { return blocked.Destination().Kind == KindCore })
	if got := blocked.Destination(); got.Label != "ZX81" || !strings.Contains(frameText(bf), "1 installed") {
		t.Fatalf("an installed blocked core was skipped: %+v\n%s", got, frameText(bf))
	}
}

func TestFesCoresRoomOpensPastAnOmittedCore(t *testing.T) {
	const atariID = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	pack := examplePack(t, "example.fes-cores")
	fake := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: atariID, CoreID: "fes.atari-st", Name: "Atari ST", Needs: "unavailable", Launchable: false, Block: "Hardware unavailable"},
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
	}}
	r := newRoom(t, pack, Options{Local: fake, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "Atari ST\n") && strings.Contains(text, "1 installed")
	})
	if strings.Contains(frameText(f), "2 installed") {
		t.Fatalf("omitted core counted as installed:\n%s", frameText(f))
	}
	if got := r.Destination(); got.Label != "FES Pong" || got.PackageID != corePongID || !got.CoreLaunchable {
		t.Fatalf("opened on the omitted core: %+v", got)
	}
	if !r.Input("left") {
		t.Fatal("left")
	}
	if got := r.Destination(); got.Label != "Atari ST" || got.CoreNeeds != "unavailable" || got.CoreLaunchable || got.Status != "Hardware unavailable" {
		t.Fatalf("omitted dest %+v", got)
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.launchedIDs(); len(got) != 0 {
		t.Fatalf("omitted tile launched %v", got)
	}

	only := newRoom(t, pack, Options{Local: &fakeLocalCores{cores: []localcores.Core{
		{PackageID: atariID, CoreID: "fes.atari-st", Name: "Atari ST", Needs: "unavailable", Launchable: false, Block: "Hardware unavailable"},
	}}, Width: 1280, Height: 720})
	if err := only.Load(); err != nil {
		t.Fatal(err)
	}
	onlyFrame := stepUntil(t, only, func(f Frame) bool { return strings.Contains(frameText(f), "Atari ST\n") })
	if got := only.Destination(); got.Kind != KindLibrary {
		t.Fatalf("omission-only Home opened on %+v", got)
	}
	if strings.Contains(frameText(onlyFrame), "1 installed") {
		t.Fatal("an omission-only shelf counted an install")
	}
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
	cores := []localcores.Core{{PackageID: coreColecoID, CoreID: "fes.coleco", Name: "ColecoVision", Needs: "media", Block: "Needs a cartridge"}}
	// Synthetic identities: exercise eight rows/six visible tiles without
	// claiming hardware acceptance of any factory package.
	for i := 1; i < count; i++ {
		cores = append(cores, localcores.Core{PackageID: fmt.Sprintf("%064x", i), CoreID: fmt.Sprintf("fes.fixture%d", i), Name: fmt.Sprintf("Installed core fixture %d", i), Needs: "media", Block: "Needs a cartridge"})
	}
	fake := &fakeLocalCores{cores: cores}
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{Local: fake, Width: 1280, Height: 720, Budget: DefaultBudget()})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "ColecoVision") })
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
	if got := fake.launchedIDs(); len(got) != 0 {
		t.Fatalf("fixture unexpectedly launched %v", got)
	}
}

func BenchmarkInstalledCoreRoomOneTile(b *testing.B) {
	var pack Pack
	for _, candidate := range Examples() {
		if candidate.ID == "example.fes-cores" {
			pack = candidate
			break
		}
	}
	fake := &fakeLocalCores{cores: []localcores.Core{{PackageID: coreColecoID, CoreID: "fes.coleco", Name: "ColecoVision", Needs: "media", Block: "Needs a cartridge"}}}
	r, err := New(pack, Options{Local: fake, Width: 1280, Height: 720, Budget: DefaultBudget()})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	if err = r.Load(); err != nil {
		b.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for r.Destination().Kind != KindCore {
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

// Filesystem-location tests save on shutdown. Keep the live Home debounce
// covered here with the room fixture's scheduling-tolerant callback budgets.
func TestHomeSavesSelectionWhileOpen(t *testing.T) {
	path := writeRoomPackage(t, corePongID)
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{
		Local: &fakeLocalCores{cores: []localcores.Core{
			{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
			{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true},
		}},
		StorePath: path,
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.Destination().Label == "FES Pong" })
	if !r.Input("right") || r.Destination().PackageID != coreZXID {
		t.Fatalf("selection did not move: %+v", r.Destination())
	}
	read := func() string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if got := read(); !strings.Contains(got, corePongID) || strings.Contains(got, coreZXID) {
		t.Fatalf("selection was saved before browsing settled: %s", got)
	}
	now := time.Now()
	for i := 0; i < 25; i++ {
		now = now.Add(100 * time.Millisecond)
		r.Step(now)
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(); !strings.Contains(got, coreZXID) {
		t.Fatalf("selection was not saved while Home remained open: %s", got)
	}
}
