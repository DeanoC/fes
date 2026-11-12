package tenfoot

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// An open room replaces the library grid on screen, so the hidden grid must
// not queue cover work that competes with the room's Lua thread.
func TestOpenRoomDoesNotPrefetchHiddenGridCovers(t *testing.T) {
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex(rooms.Examples()), "")
	app.ctx = t.Context()
	mario := availableGame("snes-mario", "Mario", "snes")
	app.mu.Lock()
	app.replaceLoadContextLocked()
	app.games = []hostclient.Game{mario}
	app.grid.SetCount(1)
	app.covers[mario.ID] = &coverSlot{phase: coverArtwork, handle: strings.Repeat("ab", 32)}
	app.openRoomLocked("example.computers")
	if app.room == nil {
		app.mu.Unlock()
		t.Fatalf("room did not open: %s", app.roomErr)
	}
	app.mu.Unlock()
	t.Cleanup(func() {
		app.mu.Lock()
		app.closeRoomLocked()
		app.mu.Unlock()
	})

	app.queueVisibleWork(time.Now())
	select {
	case item := <-app.jobs:
		t.Fatalf("open room queued hidden grid work %+v", item)
	default:
	}

	app.mu.Lock()
	app.closeRoomLocked()
	app.mu.Unlock()
	app.queueVisibleWork(time.Now())
	select {
	case item := <-app.jobs:
		if item.gameID != mario.ID || item.kind != workArtwork {
			t.Fatalf("library grid queued %+v", item)
		}
	default:
		t.Fatal("library grid queued no cover work")
	}
}
