package tenfoot

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// Home fails on demand: "sort" arms it, and the next draw either spins past
// its deadline or raises a script error.
const failingHomeScript = `
function on_input(cmd)
  if cmd == "sort" then arm = true return true end
  if cmd == "filter_next" then rooms.open("nested") return true end
  return false
end
function draw()
  if arm then
    if mode == "error" then error("boom") end
    while true do end
  end
  gfx.rect(0, 0, 10, 10, "#fff")
end
`

const spinningRoomScript = `
function draw() while true do end end
`

type homeRecoverFixture struct {
	t   *testing.T
	app *App
	now time.Time
}

func newHomeRecoverFixture(t *testing.T, mode string) *homeRecoverFixture {
	t.Helper()
	script := failingHomeScript
	if mode != "" {
		script = "mode = \"" + mode + "\"\n" + script
	}
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "main-menu", script),
		testRoomPack(t, "nested", spinningRoomScript),
		testRoomPack(t, "other", spinningRoomScript),
	})
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(index, "")
	app.SetHomeRooms(true)
	app.SetHomeRoom("main-menu")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "home room root", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "main-menu" && s.Room.Err == "" && !s.RoomPicker.Open
	})
	return &homeRecoverFixture{t: t, app: app, now: time.Now()}
}

// tick advances the fake clock by d and runs one room tick at that time.
func (f *homeRecoverFixture) tick(d time.Duration) {
	f.now = f.now.Add(d)
	f.app.mu.Lock()
	f.app.tickRoomLocked(f.now)
	f.app.mu.Unlock()
}

func (f *homeRecoverFixture) room() *rooms.Instance {
	f.app.mu.Lock()
	defer f.app.mu.Unlock()
	return f.app.room
}

// fail arms the open Home and ticks once, so it fails at the fake now.
func (f *homeRecoverFixture) fail() *rooms.Instance {
	f.t.Helper()
	f.app.HandleCommand(CmdSortCycle, f.now)
	f.tick(0)
	inst := f.room()
	if inst == nil || inst.ID() != "main-menu" || inst.Err() == nil {
		f.t.Fatalf("home did not fail: %v", inst)
	}
	return inst
}

func (f *homeRecoverFixture) wantSame(inst *rooms.Instance, what string) {
	f.t.Helper()
	if got := f.room(); got != inst {
		f.t.Fatalf("%s: room changed to %v", what, got)
	}
}

func (f *homeRecoverFixture) wantFresh(old *rooms.Instance, what string) *rooms.Instance {
	f.t.Helper()
	got := f.room()
	if got == nil || got == old || got.ID() != "main-menu" || got.Err() != nil {
		f.t.Fatalf("%s: want a fresh healthy Home, got %v (old %v)", what, got, old)
	}
	snap := f.app.Snapshot()
	if snap.Room.Err != "" || snap.RoomPicker.Open || len(snap.Room.Parents) != 0 {
		f.t.Fatalf("%s: snapshot err=%q picker=%v parents=%v", what, snap.Room.Err, snap.RoomPicker.Open, snap.Room.Parents)
	}
	return got
}

func TestFailedHomeReopensAfterBackoffUpToCap(t *testing.T) {
	f := newHomeRecoverFixture(t, "")
	for i, backoff := range homeReopenBackoff {
		failed := f.fail()
		if !errors.Is(failed.Err(), rooms.ErrBudget) {
			t.Fatalf("home failure is not a budget failure: %v", failed.Err())
		}
		snap := f.app.Snapshot()
		if !snap.Room.ErrHome || snap.Room.HomeRetry != i+1 || !strings.Contains(snap.Room.Err, "budget") {
			t.Fatalf("reopen %d panel: errHome=%v retry=%d err=%q", i+1, snap.Room.ErrHome, snap.Room.HomeRetry, snap.Room.Err)
		}
		f.tick(backoff - time.Millisecond)
		f.wantSame(failed, "before backoff")
		f.tick(time.Millisecond)
		f.wantFresh(failed, "after backoff")
		f.tick(time.Second)
	}

	// The cap is used up: the next failure stays on the panel.
	failed := f.fail()
	snap := f.app.Snapshot()
	if !snap.Room.ErrHome || snap.Room.HomeRetry != 0 {
		t.Fatalf("capped panel: errHome=%v retry=%d", snap.Room.ErrHome, snap.Room.HomeRetry)
	}
	f.tick(time.Hour)
	f.wantSame(failed, "after the cap")

	// Back still reopens Home by hand, and that does not use the cap.
	f.app.HandleCommand(CmdBack, f.now)
	healthy := f.wantFresh(failed, "back after the cap")
	failed = f.fail()
	f.tick(time.Hour)
	f.wantSame(failed, "back does not restore automatic reopens")

	// Only a Home that stays up for homeHealthyReset returns the reopens.
	f.app.HandleCommand(CmdBack, f.now)
	healthy = f.wantFresh(failed, "back again")
	f.tick(0)
	f.tick(homeHealthyReset - time.Second)
	f.wantSame(healthy, "healthy home")
	failed = f.fail()
	if snap := f.app.Snapshot(); snap.Room.HomeRetry != 0 {
		t.Fatalf("reset before %s of health: retry=%d", homeHealthyReset, snap.Room.HomeRetry)
	}
	f.app.HandleCommand(CmdBack, f.now)
	healthy = f.wantFresh(failed, "back before reset")
	f.tick(0)
	f.tick(homeHealthyReset)
	f.wantSame(healthy, "healthy home for the reset window")
	failed = f.fail()
	if snap := f.app.Snapshot(); snap.Room.HomeRetry != 1 {
		t.Fatalf("reopens not reset after %s of health: retry=%d", homeHealthyReset, snap.Room.HomeRetry)
	}
	f.tick(homeReopenBackoff[0])
	f.wantFresh(failed, "reopen after reset")
}

func TestFailedHomeWaitsForPickerToClose(t *testing.T) {
	f := newHomeRecoverFixture(t, "")
	failed := f.fail()
	f.app.mu.Lock()
	f.app.openRoomPickerLocked()
	f.app.mu.Unlock()
	f.tick(homeReopenBackoff[0])
	f.wantSame(failed, "picker open")
	f.app.mu.Lock()
	f.app.closeRoomPickerLocked()
	f.app.mu.Unlock()
	f.tick(0)
	f.wantFresh(failed, "picker closed")
}

func TestHomeScriptErrorIsNeverRetried(t *testing.T) {
	f := newHomeRecoverFixture(t, "error")
	failed := f.fail()
	if errors.Is(failed.Err(), rooms.ErrBudget) || !strings.Contains(failed.Err().Error(), "boom") {
		t.Fatalf("want a script error, got %v", failed.Err())
	}
	snap := f.app.Snapshot()
	if !snap.Room.ErrHome || snap.Room.HomeRetry != 0 {
		t.Fatalf("script error panel: errHome=%v retry=%d", snap.Room.ErrHome, snap.Room.HomeRetry)
	}
	for _, d := range []time.Duration{homeReopenBackoff[0], time.Minute, time.Hour} {
		f.tick(d)
		f.wantSame(failed, "script error")
	}
}

func TestBackOnFailedRootHomeReopensHome(t *testing.T) {
	for _, cmd := range []Command{CmdBack, CmdSelect} {
		t.Run(roomCommandName(cmd), func(t *testing.T) {
			f := newHomeRecoverFixture(t, "error")
			failed := f.fail()
			f.app.HandleCommand(cmd, f.now)
			f.wantFresh(failed, "back on failed home")
		})
	}
}

// An installed Home that misses its deadline again right after a reopen must
// keep its selection: reopening is not an update, so it never rolls back.
func TestReopenKeepsInstalledHomeSelection(t *testing.T) {
	root := t.TempDir()
	installedTestRoom(t, root, "main-menu", "Previous", spinningRoomScript)
	selected := installedTestRoom(t, root, "main-menu", "Selected", spinningRoomScript)
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{selected}), root)
	app.SetHomeRooms(true)
	app.SetHomeRoom("main-menu")
	t.Cleanup(func() { app.mu.Lock(); defer app.mu.Unlock(); app.closeAllRoomsLocked() })
	f := &homeRecoverFixture{t: t, app: app, now: time.Now()}
	app.mu.Lock()
	app.openValidRoomLocked(selected, "")
	app.mu.Unlock()

	wantSelected := func(what string) {
		t.Helper()
		packs, err := rooms.LoadDir(root)
		if err != nil || len(packs) != 1 || packs[0].Revision != selected.Revision {
			t.Fatalf("%s: installed Home selection changed: %v %v", what, packs, err)
		}
		inst := f.room()
		if inst == nil || inst.ID() != "main-menu" || inst.Revision() != selected.Revision {
			t.Fatalf("%s: want the selected Home open, got %v", what, inst)
		}
	}
	failNow := func(what string) *rooms.Instance {
		t.Helper()
		f.tick(0)
		inst := f.room()
		if inst == nil || !errors.Is(inst.Err(), rooms.ErrBudget) {
			t.Fatalf("%s: home did not fail on budget: %v", what, inst)
		}
		return inst
	}

	failed := failNow("first failure")
	f.tick(homeReopenBackoff[0])
	f.wantFresh(failed, "automatic reopen")
	wantSelected("automatic reopen")

	failed = failNow("after automatic reopen")
	app.HandleCommand(CmdBack, f.now)
	f.wantFresh(failed, "back")
	wantSelected("back")
	failNow("after back")
	wantSelected("failed again")
}

func TestFailedNonRootRoomsAreNotRecovered(t *testing.T) {
	t.Run("nested", func(t *testing.T) {
		f := newHomeRecoverFixture(t, "")
		home := f.room()
		f.app.HandleCommand(CmdFilterNext, f.now)
		f.tick(0)
		nested := f.room()
		if nested == nil || nested.ID() != "nested" || !errors.Is(nested.Err(), rooms.ErrBudget) {
			t.Fatalf("nested room did not fail on budget: %v", nested)
		}
		snap := f.app.Snapshot()
		if snap.Room.ErrHome || snap.Room.HomeRetry != 0 {
			t.Fatalf("nested panel: errHome=%v retry=%d", snap.Room.ErrHome, snap.Room.HomeRetry)
		}
		f.tick(time.Hour)
		f.wantSame(nested, "nested failure")
		f.app.HandleCommand(CmdBack, f.now)
		f.wantSame(home, "back from failed nested room")
	})
	t.Run("root other room", func(t *testing.T) {
		f := newHomeRecoverFixture(t, "")
		f.app.mu.Lock()
		f.app.openRoomLocked("other")
		f.app.mu.Unlock()
		f.tick(0)
		other := f.room()
		if other == nil || other.ID() != "other" || !errors.Is(other.Err(), rooms.ErrBudget) {
			t.Fatalf("other room did not fail on budget: %v", other)
		}
		f.tick(time.Hour)
		f.wantSame(other, "other room failure")
		f.app.HandleCommand(CmdBack, f.now)
		snap := f.app.Snapshot()
		if snap.Room.Open || !snap.RoomPicker.Open {
			t.Fatalf("back on failed root room should open the picker: room=%v picker=%v", snap.Room.Open, snap.RoomPicker.Open)
		}
	})
}

func TestRoomErrorHint(t *testing.T) {
	for _, tc := range []struct {
		room RoomSnapshot
		want string
	}{
		{RoomSnapshot{ErrHome: true, HomeRetry: 2}, "Home reopens by itself shortly (try 2 of 3). B reopens it now."},
		{RoomSnapshot{ErrHome: true}, "B reopens Home."},
		{RoomSnapshot{Parents: []string{"main-menu"}}, "B returns to the previous room."},
		{RoomSnapshot{}, "B opens the menu."},
	} {
		if got := roomErrorHint(tc.room, InputGamepad); got != tc.want {
			t.Errorf("hint %+v = %q, want %q", tc.room, got, tc.want)
		}
	}
}
