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

// Grid cover work queued before a room opens must not keep the workers busy
// behind the room. Opening the room drops those queued jobs and their inflight
// markers, keeps other queued work, and the grid asks again when it returns.
func TestReturnFromLibraryDropsQueuedGridCoverWork(t *testing.T) {
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
	app.suspendRoomsForLibraryLocked()
	app.mu.Unlock()
	t.Cleanup(func() {
		app.mu.Lock()
		app.closeAllRoomsLocked()
		app.mu.Unlock()
	})

	// The Library is showing: its grid queues the cover.
	app.queueVisibleWork(time.Now())
	app.mu.Lock()
	defer app.mu.Unlock()
	if _, busy := app.inflight[mario.ID]; !busy {
		t.Fatal("library grid did not queue its cover")
	}
	if !app.returnFromLibraryLocked() {
		t.Fatal("Back from the Library did not resume the room")
	}
	// The grid cover is gone before the resumed room's callbacks ran, not
	// one frame later.
	if _, busy := app.inflight[mario.ID]; busy {
		t.Fatal("resuming the room kept the hidden grid cover inflight")
	}
	select {
	case item := <-app.jobs:
		t.Fatalf("resumed room left hidden grid work %+v queued", item)
	default:
	}
}

func TestOpenRoomDropsQueuedGridCoverWork(t *testing.T) {
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
	app.mu.Unlock()

	app.queueVisibleWork(time.Now())
	app.mu.Lock()
	if len(app.jobs) != 1 {
		app.mu.Unlock()
		t.Fatalf("library grid queued %d jobs, want 1", len(app.jobs))
	}
	shot := workItem{kind: workScreenshot, handle: strings.Repeat("cd", 32), gen: app.loadGen, shotGen: app.shotGen}
	app.inflight[shot.key()] = workScreenshot
	app.jobs <- shot
	app.openRoomLocked("example.computers")
	if app.room == nil {
		app.mu.Unlock()
		t.Fatalf("room did not open: %s", app.roomErr)
	}
	// Opening the room drops the queued grid cover itself, before any frame.
	if _, busy := app.inflight[mario.ID]; busy {
		app.mu.Unlock()
		t.Fatal("opening the room kept the hidden grid cover inflight")
	}
	app.mu.Unlock()
	t.Cleanup(func() {
		app.mu.Lock()
		app.closeRoomLocked()
		app.mu.Unlock()
	})

	app.queueVisibleWork(time.Now())
	app.mu.Lock()
	if _, busy := app.inflight[mario.ID]; busy {
		app.mu.Unlock()
		t.Fatal("open room kept the hidden grid cover inflight")
	}
	if _, busy := app.inflight[shot.key()]; !busy {
		app.mu.Unlock()
		t.Fatal("open room dropped the screenshot inflight marker")
	}
	app.mu.Unlock()
	select {
	case item := <-app.jobs:
		if item != shot {
			t.Fatalf("open room left %+v queued, want only the screenshot", item)
		}
	default:
		t.Fatal("open room dropped the queued screenshot")
	}
	select {
	case item := <-app.jobs:
		t.Fatalf("open room left hidden grid work %+v queued", item)
	default:
	}

	app.mu.Lock()
	delete(app.inflight, shot.key())
	app.closeRoomLocked()
	app.mu.Unlock()
	app.queueVisibleWork(time.Now())
	select {
	case item := <-app.jobs:
		if item.gameID != mario.ID || item.kind != workArtwork {
			t.Fatalf("library grid queued %+v", item)
		}
	default:
		t.Fatal("library grid did not re-request the dropped cover")
	}
}
