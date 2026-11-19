package tenfoot

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// A library reload while a room is open (a Stop, a shelf change) keeps the
// room's own status line. A user action's notice still shows in the room,
// and the library's line returns when the library itself is shown.
func TestOpenRoomKeepsItsStatusAcrossLibraryReload(t *testing.T) {
	h := newRoomHost(t)
	app := newRoomApp(t, h, rooms.NewIndex(rooms.Examples()), false)
	waitFor(t, app, "initial library", func(s Snapshot) bool { return !s.Loading && len(s.Games) > 0 })

	app.mu.Lock()
	app.openRoomLocked("example.computers")
	app.mu.Unlock()
	opened := waitRoom(t, app, "example.computers")
	title := opened.Status
	if title == "" || strings.Contains(title, "titles") {
		t.Fatalf("room opened with status %q", title)
	}

	app.mu.Lock()
	app.reloadLocked()
	app.mu.Unlock()
	snap := waitFor(t, app, "reload in the room", func(s Snapshot) bool { return !s.Loading })
	if snap.Status != title {
		t.Fatalf("status after a library reload = %q, want the room's %q", snap.Status, title)
	}
	app.mu.Lock()
	app.reloadCatalogWithNoticeLocked("Saved.")
	app.mu.Unlock()
	waitFor(t, app, "notice in the room", func(s Snapshot) bool { return !s.Loading && s.Status == "Saved." })

	app.mu.Lock()
	app.suspendRoomsForLibraryLocked()
	app.reloadLocked()
	app.mu.Unlock()
	waitFor(t, app, "library line in the library", func(s Snapshot) bool {
		return !s.Room.Open && !s.Loading && strings.Contains(s.Status, "titles")
	})
}

// Back from the Library shows the room's own line, whether the Library's
// load has finished or is still running.
func TestRoomReturnFromLibraryShowsItsStatus(t *testing.T) {
	for _, finished := range []bool{true, false} {
		h := newRoomHost(t)
		app := newRoomApp(t, h, rooms.NewIndex(rooms.Examples()), false)
		waitFor(t, app, "initial library", func(s Snapshot) bool { return !s.Loading && len(s.Games) > 0 })
		app.mu.Lock()
		app.openRoomLocked("example.computers")
		app.mu.Unlock()
		title := waitRoom(t, app, "example.computers").Status

		gate := make(chan struct{})
		arrived := make(chan struct{}, 1)
		if !finished {
			h.mu.Lock()
			h.gamesGate, h.gamesArrived = gate, arrived
			h.mu.Unlock()
		}
		app.mu.Lock()
		app.suspendRoomsForLibraryLocked()
		app.reloadLocked()
		app.mu.Unlock()
		if finished {
			waitFor(t, app, "library loaded", func(s Snapshot) bool { return !s.Room.Open && !s.Loading })
		} else {
			<-arrived
		}
		app.mu.Lock()
		ok := app.returnFromLibraryLocked()
		app.mu.Unlock()
		if !ok {
			t.Fatal("Back did not return to the room")
		}
		close(gate)
		snap := waitFor(t, app, "room after Back", func(s Snapshot) bool { return s.Room.Open && !s.Loading })
		if snap.Status != title {
			t.Fatalf("finished=%v: status after Back = %q, want the room's %q", finished, snap.Status, title)
		}
	}
}
