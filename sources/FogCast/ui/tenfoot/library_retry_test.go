package tenfoot

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestLibraryNetRetryClassifiesErrors(t *testing.T) {
	if !libraryNetRetry(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}) {
		t.Fatal("dial failure should retry")
	}
	if !libraryNetRetry(context.DeadlineExceeded) {
		t.Fatal("deadline should retry")
	}
	if libraryNetRetry(context.Canceled) || libraryNetRetry(errLibrarySuperseded) {
		t.Fatal("a cancelled or replaced load should not retry")
	}
	if libraryNetRetry(errors.New("host API 403 TARGET_MISMATCH: target")) {
		t.Fatal("a host API status should not retry")
	}
}

func TestLibraryRetriesARefusedConnectionThenRestoresFocus(t *testing.T) {
	var shelfDrops atomic.Int32
	var shelfAttempts atomic.Int32
	var launches atomic.Int32
	shelfDrops.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && !strings.Contains(r.URL.RawQuery, "collection=recents"):
			shelfAttempts.Add(1)
			if takeDrop(&shelfDrops) {
				dropConn(w)
				return
			}
			writeGames(w, availableGame("snes-mario", "Mario", "snes"), availableGame("megadrive-sonic", "Sonic", "megadrive"))
		case r.URL.Path == "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case r.URL.Path == "/api/v1/library/collections":
			_, _ = w.Write([]byte(`{"collections":[]}`))
		case r.URL.Path == "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			launches.Add(1)
			http.Error(w, "unexpected launch", http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	sawBanner := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if strings.Contains(snap.Status, "Can't reach the library") {
			sawBanner = true
		}
		if sawBanner && !snap.Loading && snap.LoadErr == "" && len(snap.Games) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap := app.Snapshot()
	if !sawBanner || snap.LoadErr != "" || len(snap.Games) != 2 || strings.Contains(snap.Status, "Can't reach the library") {
		t.Fatalf("retry did not clear the banner: saw=%v status=%q err=%q games=%d attempts=%d", sawBanner, snap.Status, snap.LoadErr, len(snap.Games), shelfAttempts.Load())
	}
	if shelfAttempts.Load() < 2 {
		t.Fatalf("shelf attempts = %d", shelfAttempts.Load())
	}
	if app.Press(CommandFromButton(ButtonDPadRight), time.Now()) != CmdRight {
		t.Fatal("expected right command")
	}
	selected, ok := app.Selected()
	if !ok || selected.ID != "megadrive-sonic" {
		t.Fatalf("selected = %#v ok=%v", selected, ok)
	}

	shelfDrops.Store(1)
	before := shelfAttempts.Load()
	app.mu.Lock()
	app.reloadLocked()
	app.mu.Unlock()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap = app.Snapshot()
		selected, ok = app.Selected()
		if !snap.Loading && snap.LoadErr == "" && ok && selected.ID == "megadrive-sonic" && shelfAttempts.Load() >= before+2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	selected, ok = app.Selected()
	if !ok || selected.ID != "megadrive-sonic" || app.Snapshot().LoadErr != "" {
		t.Fatalf("focus after retry = %#v ok=%v err=%q", selected, ok, app.Snapshot().LoadErr)
	}
	if launches.Load() != 0 {
		t.Fatalf("launches = %d", launches.Load())
	}
}

func TestHomeRecentsRetryAfterARefusedConnection(t *testing.T) {
	var recentDrops atomic.Int32
	recentDrops.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && strings.Contains(r.URL.RawQuery, "collection=recents"):
			if takeDrop(&recentDrops) {
				dropConn(w)
				return
			}
			writeGames(w, availableGame("nes-pong", "Pong", "nes"))
		case r.URL.Path == "/api/v1/games":
			writeGames(w, availableGame("snes-mario", "Mario", "snes"))
		case r.URL.Path == "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case r.URL.Path == "/api/v1/library/collections":
			_, _ = w.Write([]byte(`{"collections":[]}`))
		case r.URL.Path == "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	}), "/tmp/rooms")
	app.SetHomeRooms(true)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	saw := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if recentDetail(snap) == "couldn't load recently played" {
			saw = true
		}
		if saw && homeHasRow(snap, HomeKindRecent, "nes-pong") && recentDetail(snap) != "couldn't load recently played" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("recents did not recover: saw=%v detail=%q rows=%+v", saw, recentDetail(app.Snapshot()), app.Snapshot().RoomPicker.Rows)
}

func TestLibraryDoesNotRetryAHostStatus(t *testing.T) {
	var games atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/games":
			games.Add(1)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"TARGET_MISMATCH","message":"target"}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case "/api/v1/library/collections":
			_, _ = w.Write([]byte(`{"collections":[]}`))
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().LoadErr != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(presentationRetryMin + 150*time.Millisecond)
	if games.Load() != 1 || app.Snapshot().LoadErr == "" || len(app.Snapshot().Games) != 0 {
		t.Fatalf("games=%d err=%q titles=%d", games.Load(), app.Snapshot().LoadErr, len(app.Snapshot().Games))
	}
}

func TestEmptyLibraryIsNotUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/games":
			writeGames(w)
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case "/api/v1/library/collections":
			_, _ = w.Write([]byte(`{"collections":[]}`))
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if !snap.Loading && snap.Status == shelfEmptyCopy {
			if snap.LoadErr != "" {
				t.Fatalf("empty library set %q", snap.LoadErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status=%q err=%q", app.Snapshot().Status, app.Snapshot().LoadErr)
}

func takeDrop(n *atomic.Int32) bool {
	for {
		v := n.Load()
		if v <= 0 {
			return false
		}
		if n.CompareAndSwap(v, v-1) {
			return true
		}
	}
}

func dropConn(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	_ = conn.Close()
}

func writeGames(w http.ResponseWriter, games ...hostclient.Game) {
	if games == nil {
		games = []hostclient.Game{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
}

func recentDetail(snap Snapshot) string {
	for _, row := range snap.RoomPicker.Rows {
		if row.Kind == HomeKindRecents {
			return row.Detail
		}
	}
	return ""
}
