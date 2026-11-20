package tenfoot

import (
	"net/http"
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
	// A notice set before the load survives the load's own status line, and
	// a failed refresh is still reported in the room.
	for _, c := range []struct {
		httpStatus int
		want       string
	}{
		{0, "Saved."},
		{http.StatusInternalServerError, "Saved. Couldn't refresh the library."},
	} {
		gate := make(chan struct{})
		arrived := make(chan struct{}, 1)
		h.mu.Lock()
		h.gamesGate, h.gamesArrived, h.gamesStatus = gate, arrived, c.httpStatus
		h.mu.Unlock()
		app.mu.Lock()
		app.reloadCatalogWithNoticeLocked("Saved.")
		app.mu.Unlock()
		<-arrived
		if s := app.Snapshot(); !s.Loading || s.Status != "Saved." {
			t.Fatalf("during the load: loading=%v status=%q", s.Loading, s.Status)
		}
		close(gate)
		snap := waitFor(t, app, "notice after the load", func(s Snapshot) bool { return !s.Loading && s.Status == c.want })
		if !snap.Room.Open {
			t.Fatal("room closed")
		}
		h.mu.Lock()
		h.gamesGate, h.gamesArrived, h.gamesStatus = nil, nil, 0
		h.mu.Unlock()
	}

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

// Back from a nested room shows the parent's own line, not the child's.
func TestParentRoomShowsItsStatusAfterNestedRoom(t *testing.T) {
	h := newRoomHost(t)
	app := newRoomApp(t, h, rooms.NewIndex(rooms.Examples()), false)
	waitFor(t, app, "initial library", func(s Snapshot) bool { return !s.Loading && len(s.Games) > 0 })
	app.mu.Lock()
	app.openRoomLocked("example.computers")
	app.mu.Unlock()
	parent := waitRoom(t, app, "example.computers").Status

	app.mu.Lock()
	app.openNestedRoomLocked("example.hardware", "")
	app.mu.Unlock()
	child := waitRoom(t, app, "example.hardware").Status
	if child == parent {
		t.Fatalf("nested room kept the parent's line %q", parent)
	}
	app.mu.Lock()
	app.leaveRoomLocked()
	app.mu.Unlock()
	snap := waitRoom(t, app, "example.computers")
	if snap.Status != parent {
		t.Fatalf("status after Back = %q, want the parent's %q", snap.Status, parent)
	}
}
