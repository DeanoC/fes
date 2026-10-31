package tenfoot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestBrowseActionsRetainTheMissingTitleRemedy(t *testing.T) {
	d := rooms.Destination{Kind: rooms.KindGame, Availability: rooms.AvailMissing, Label: "Missing title"}
	primary, secondary := browseActionLabels(d)
	if primary != "Open library" || secondary {
		t.Fatalf("missing title remedy changed: primary=%q secondary=%v", primary, secondary)
	}
}

func TestLibraryActionsReachDetailsAndReturnToTheSameTitle(t *testing.T) {
	for _, size := range [][2]int{{1280, 720}, {1024, 600}} {
		for _, mode := range []LayoutKind{LayoutGrid, LayoutList, LayoutShelf} {
			t.Run(fmt.Sprintf("%s-%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				app := libraryRenderApp(t, size[0], size[1], true)
				app.SetLayout(mode)
				app.focusIndex(len(app.games) - 1)
				before := app.Snapshot()
				id := before.Games[before.Grid.Focus].ID
				app.HandleCommand(CmdDown, time.Now())
				snap := app.Snapshot()
				if !snap.BrowseActions.Focused || snap.Detail.Open || snap.BrowseActions.Index != 0 {
					t.Fatalf("Down should focus visible actions, without opening Details: %+v", snap.BrowseActions)
				}
				primary, secondary := libraryActionRects(snap)
				if primary.W == 0 || secondary.W == 0 || primary.X+primary.W >= secondary.X || secondary.X+secondary.W > snap.Grid.Width-snap.Grid.Safe.Right {
					t.Fatalf("action rectangles: %+v %+v", primary, secondary)
				}
				if hit := HitTest(snap, secondary.X+secondary.W/2, secondary.Y+secondary.H/2); hit.Kind != PointerBrowseActions || hit.Index != 1 {
					t.Fatalf("painted Details and pointer differ: %+v", hit)
				}
				app.HandleCommand(CmdRight, time.Now())
				if hint := app.Snapshot().HeaderHint(); !strings.Contains(hint, "details") || strings.Contains(hint, "play") {
					t.Fatalf("focused Details has the wrong Confirm hint: %q", hint)
				}
				app.HandleCommand(CmdSelect, time.Now())
				snap = app.Snapshot()
				if !snap.Detail.Open || snap.BrowseActions.Focused || snap.FocusDetail.Title != before.FocusDetail.Title {
					t.Fatalf("direction + Confirm did not open the selected title's Details: detail=%+v actions=%+v", snap.Detail, snap.BrowseActions)
				}
				app.HandleCommand(CmdBack, time.Now())
				snap = app.Snapshot()
				if snap.Detail.Open || snap.Games[snap.Grid.Focus].ID != id {
					t.Fatalf("Back lost the title %q", id)
				}
				app.HandleCommand(CmdDown, time.Now())
				app.HandleCommand(CmdBack, time.Now())
				if snap := app.Snapshot(); snap.BrowseActions.Focused || snap.Grid.Focus != before.Grid.Focus {
					t.Fatalf("Back did not return from actions to the same title: %+v", snap.BrowseActions)
				}
			})
		}
	}
}

func TestUnfocusedLibraryDetailsPromptMatchesItsShortcut(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   InputKind
		prompt string
		cmd    Command
	}{
		{name: "gamepad", kind: InputGamepad, prompt: "Y", cmd: CommandFromButton(ButtonNorth)},
		{name: "keyboard", kind: InputKeyboard, prompt: "i", cmd: CommandFromKey("i")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := libraryRenderApp(t, 1280, 720, false)
			app.NoteInput(tc.kind, 1)
			snap := app.Snapshot()
			if !snap.BrowseActions.Visible || snap.BrowseActions.Focused {
				t.Fatalf("expected visible, unfocused Library actions: %+v", snap.BrowseActions)
			}
			if got := libraryActionPrompt(snap, 1); got != tc.prompt {
				t.Fatalf("unfocused Details prompt = %q, want %q", got, tc.prompt)
			}
			hint := snap.HeaderHint()
			if !strings.Contains(strings.ToLower(hint), strings.ToLower(tc.prompt)+" details") {
				t.Fatalf("closed-row hint does not advertise %q Details: %q", tc.prompt, hint)
			}
			if tc.kind == InputGamepad && strings.Contains(hint, "Y search") {
				t.Fatalf("Y still advertises search while it opens Details: %q", hint)
			}
			app.HandleCommand(tc.cmd, time.Now())
			if opened := app.Snapshot(); !opened.Detail.Open || opened.BrowseActions.Focused {
				t.Fatalf("Details shortcut did not open the selected title: %+v", opened)
			}
			if tc.kind == InputGamepad {
				if hint := app.Snapshot().HeaderHint(); !strings.Contains(hint, "Y search") {
					t.Fatalf("Details does not advertise controller search: %q", hint)
				}
				app.HandleCommand(tc.cmd, time.Now())
				if searched := app.Snapshot(); !searched.SearchOpen || searched.Detail.Open {
					t.Fatalf("second Y did not reach Search from Details: detail=%+v search=%v", searched.Detail, searched.SearchOpen)
				}
			}
		})
	}
}

func TestBrowseActionsRejectChangedSubjectOrContext(t *testing.T) {
	for _, change := range []string{"new-item", "new-package", "new-media", "new-readiness", "new-target", "new-session"} {
		t.Run(change, func(t *testing.T) {
			app := libraryRenderApp(t, 1280, 720, false)
			app.focusIndex(len(app.games) - 1)
			app.HandleCommand(CmdDown, time.Now())
			if !app.Snapshot().BrowseActions.Focused {
				t.Fatal("actions did not focus")
			}
			switch change {
			case "new-item":
				app.games[app.grid.Focus].ID = "replacement"
			case "new-package":
				app.games[app.grid.Focus].PackageID = strings.Repeat("a", 64)
			case "new-media":
				app.games[app.grid.Focus].MediaID = strings.Repeat("b", 64)
			case "new-readiness":
				app.games[app.grid.Focus].Launchable = false
			case "new-target":
				app.client = NewClient("http://fixture.invalid", nil)
			case "new-session":
				app.sessionGen++
			}
			app.HandleCommand(CmdSelect, time.Now())
			if snap := app.Snapshot(); snap.Launch.Phase == "launching" || snap.Detail.Open || snap.BrowseActions.Focused || !strings.Contains(snap.Status, "selection changed") {
				t.Fatalf("stale action was retargeted: launch=%+v status=%q", snap.Launch, snap.Status)
			}
		})
	}
}

func TestHomeHeroActionsFollowSpatialDirectionsWithoutMovingSelection(t *testing.T) {
	for _, size := range [][2]int{{1280, 720}, {1024, 600}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			fake := &fakeLocalCores{cores: homeFixtureCores()}
			app := startHomeLounge(t, fake, size[0], size[1])
			before := waitFor(t, app, "Pong", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
			app.HandleCommand(CmdUp, time.Now())
			snap := app.Snapshot()
			if !snap.BrowseActions.Focused || snap.Room.Destination.PackageID != before.Room.Destination.PackageID {
				t.Fatalf("Up did not enter the hero actions above the retained shelf item: %+v", snap.BrowseActions)
			}
			app.HandleCommand(CmdRight, time.Now())
			app.HandleCommand(CmdSelect, time.Now())
			if snap := app.Snapshot(); !snap.Room.CoreDetail || fake.launchCount() != 0 {
				t.Fatalf("Details action launched or failed to open: %+v", snap.Room)
			}
			app.HandleCommand(CmdBack, time.Now())
			app.HandleCommand(CmdUp, time.Now())
			app.HandleCommand(CmdDown, time.Now())
			if snap := app.Snapshot(); snap.BrowseActions.Focused || snap.Room.Destination.PackageID != before.Room.Destination.PackageID {
				t.Fatal("Down did not return to the retained shelf item")
			}
			app.HandleCommand(CmdUp, time.Now())
			app.HandleCommand(CmdUp, time.Now())
			if snap := app.Snapshot(); snap.Room.Destination.Kind != rooms.KindLibrary || snap.BrowseActions.Focused {
				t.Fatalf("second Up did not reach the header: %+v", snap.Room.Destination)
			}
			app.HandleCommand(CmdDown, time.Now())
			if snap := app.Snapshot(); snap.Room.Destination.PackageID != before.Room.Destination.PackageID {
				t.Fatal("header Down changed the shelf selection")
			}
			// Ordinary Confirm on the shelf still launches directly.
			app.HandleCommand(CmdSelect, time.Now())
			waitFor(t, app, "direct launch", func(Snapshot) bool { return fake.launchCount() == 1 })
		})
	}
}

func TestReadyHomeActionsDoNotWaitForUnrelatedLibraryLoad(t *testing.T) {
	for _, size := range [][2]int{{1280, 720}, {1024, 600}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			index := rooms.NewIndex(rooms.Examples())
			pack, _ := index.Find(installedCoresHomeID)
			fake := &fakeLocalCores{cores: homeFixtureCores()}
			app := NewApp(nil, size[0], size[1], 50)
			app.roomsIndex = index
			app.localCores, app.localFeed = fake, &fakePadFeed{}
			w, h := app.roomContentSizeLocked(pack.ID)
			inst, err := rooms.New(pack, rooms.Options{Width: w, Height: h, Index: index, Local: fake, Services: hardwareRenderServices{}, Budget: hardwareRenderTestBudget()})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(inst.Close)
			app.room = inst
			if err := inst.Load(); err != nil {
				t.Fatal(err)
			}
			before := waitFor(t, app, "ready local Home core", func(s Snapshot) bool {
				return s.Room.Destination.Label == "Pong" && s.Room.Destination.Confirm() == rooms.ConfirmLaunchCore
			})
			// No host poller runs in this fixture: the unrelated library flags
			// stay pending while real Home Lua and command routing run.
			app.loading, app.searchPending = true, true
			app.HandleCommand(CmdUp, time.Now())
			snap := app.Snapshot()
			if !snap.BrowseActions.Visible || !snap.BrowseActions.Focused || snap.Room.Destination.PackageID != before.Room.Destination.PackageID || snap.Room.Destination.Confirm() != rooms.ConfirmLaunchCore {
				t.Fatalf("ready Home actions waited on the library: actions=%+v destination=%+v", snap.BrowseActions, snap.Room.Destination)
			}
			app.HandleCommand(CmdRight, time.Now())
			app.HandleCommand(CmdSelect, time.Now())
			if snap := app.Snapshot(); !snap.Room.CoreDetail || fake.launchCount() != 0 || !snap.Loading {
				t.Fatalf("pending library blocked Home Details or changed launch state: detail=%v launches=%d loading=%v", snap.Room.CoreDetail, fake.launchCount(), snap.Loading)
			}
		})
	}
}

func TestLibraryActionsStillWaitForTheirOwnPendingLoad(t *testing.T) {
	for _, flag := range []string{"loading", "search"} {
		t.Run(flag, func(t *testing.T) {
			app := libraryRenderApp(t, 1024, 600, false)
			app.focusIndex(len(app.games) - 1)
			app.loading, app.searchPending = flag == "loading", flag == "search"
			app.HandleCommand(CmdDown, time.Now())
			if snap := app.Snapshot(); snap.BrowseActions.Visible || snap.BrowseActions.Focused || snap.Detail.Open {
				t.Fatalf("pending Library offered stale actions: %+v", snap.BrowseActions)
			}
		})
	}
}

func TestHomeHeroActionsRejectAChangedDestination(t *testing.T) {
	fake := &fakeLocalCores{cores: homeFixtureCores()}
	app := startHomeLounge(t, fake, 1280, 720)
	waitFor(t, app, "Pong", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
	app.HandleCommand(CmdUp, time.Now())
	app.mu.Lock()
	app.room.Hover("core:2") // A refreshed/externally selected row replaces the captured subject.
	app.mu.Unlock()
	app.HandleCommand(CmdSelect, time.Now())
	snap := app.Snapshot()
	if fake.launchCount() != 0 || snap.Room.CoreDetail || snap.Status != staleBrowseActionStatus {
		t.Fatalf("stale Home action activated replacement: status=%q launches=%d", snap.Status, fake.launchCount())
	}
	if !snap.Room.Hero {
		t.Fatal("expected Home hero room")
	}
	dev, err := gfx.NewSoftware(snap.Grid.Width, snap.Grid.Height)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]gpuTexture{}
	presentFrame(dev, snap, map[string]gpuTexture{}, labels, false)
	key := roomTextKey(rooms.Op{Kind: rooms.OpText, Text: staleBrowseActionStatus, Size: heroFooterPx, Bold: true})
	if footer, ok := labels[key]; !ok || !labelHasInk(footer) {
		t.Fatal("stale-action message was not drawn in the Home hero footer")
	}
}
