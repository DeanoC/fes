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
