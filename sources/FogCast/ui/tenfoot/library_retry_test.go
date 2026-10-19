package tenfoot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if !libraryNetRetry(fmt.Errorf("games: %w: read 4 of 10 bytes", hostclient.ErrResponseTruncated)) {
		t.Fatal("truncated body should retry")
	}
	if libraryNetRetry(fmt.Errorf("games: %w: content length 99 exceeds 4", hostclient.ErrResponseTooLarge)) {
		t.Fatal("oversize body should not retry")
	}
	var decoded struct{}
	decodeErr := json.Unmarshal([]byte(`{"games":`), &decoded)
	if decodeErr == nil || libraryNetRetry(fmt.Errorf("decode /api/v1/games: %w", decodeErr)) {
		t.Fatalf("json decode should not retry: %v", decodeErr)
	}
}

func TestSameFilterSavedShelfRefusesAnotherFilter(t *testing.T) {
	mario := []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}
	snes := hostclient.GameListQuery{Platform: "snes"}
	sms := hostclient.GameListQuery{Platform: "sms"}
	saved, ok := sameFilterSavedShelf(true, snes, mario, nil, snes)
	if !ok || len(saved) != 1 || saved[0].ID != "snes-mario" {
		t.Fatalf("same filter = %#v ok=%v", saved, ok)
	}
	if _, ok := sameFilterSavedShelf(true, snes, mario, mario, sms); ok {
		t.Fatal("sms refresh kept snes rows")
	}
	if _, ok := sameFilterSavedShelf(true, snes, nil, nil, snes); ok {
		t.Fatal("empty shelf was restored")
	}
	previous := []hostclient.Game{availableGame("sms-sonic", "Sonic", "sms")}
	saved, ok = sameFilterSavedShelf(false, hostclient.GameListQuery{}, nil, previous, sms)
	if !ok || saved[0].ID != "sms-sonic" {
		t.Fatalf("unstored shelf = %#v ok=%v", saved, ok)
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

func TestLibraryKeepsCachedShelfLaunchableDuringRetry(t *testing.T) {
	var dropShelf atomic.Bool
	var shelfID atomic.Value
	var launches atomic.Int32
	var launched atomic.Value
	shelfID.Store("snes-mario")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && !strings.Contains(r.URL.RawQuery, "collection=recents"):
			if dropShelf.Load() {
				dropConn(w)
				return
			}
			id, _ := shelfID.Load().(string)
			title := "Mario"
			if id == "snes-zelda" {
				title = "Zelda"
			}
			writeGames(w, availableGame(id, title, "snes"))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			launched.Store(string(raw))
			launches.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"SOURCE_UNAVAILABLE","message":"down"}}`))
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
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	// a.status, not Snapshot.Status: a launch error replaces the overlay
	// text while the shelf status stays on the app.
	waitShelf := func(id, status string, cached bool) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		var loading bool
		var loadErr, gotStatus string
		var games, saved []string
		for time.Now().Before(deadline) {
			app.Tick(time.Now())
			app.mu.Lock()
			loading = app.loading
			loadErr = app.loadErr
			gotStatus = app.status
			games = gameIDs(app.games)
			saved = gameIDs(app.shelfGames)
			app.mu.Unlock()
			cacheOK := !cached || (len(saved) == 1 && saved[0] == id)
			if !loading && loadErr == "" && len(games) == 1 && games[0] == id && cacheOK && (status == "" || gotStatus == status) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("shelf %s status %q cached=%v: loading=%v err=%q status=%q games=%v saved=%v", id, status, cached, loading, loadErr, gotStatus, games, saved)
	}
	waitShelf("snes-mario", "", true)

	dropShelf.Store(true)
	app.mu.Lock()
	app.reloadLocked()
	app.mu.Unlock()
	waitShelf("snes-mario", savedListOfflineCopy, true)
	if launches.Load() != 0 {
		t.Fatalf("launches before select = %d", launches.Load())
	}
	app.Press(CmdSelect, time.Now())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if launches.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, _ := launched.Load().(string)
	app.mu.Lock()
	heldGames := gameIDs(app.games)
	heldLoading := app.loading
	app.mu.Unlock()
	if launches.Load() != 1 || !strings.Contains(raw, "snes-mario") || heldLoading || len(heldGames) != 1 || heldGames[0] != "snes-mario" {
		t.Fatalf("launch during retry = %d body=%s loading=%v games=%v phase=%s", launches.Load(), raw, heldLoading, heldGames, app.Snapshot().Launch.Phase)
	}

	shelfID.Store("snes-zelda")
	dropShelf.Store(false)
	waitShelf("snes-zelda", "", true)

	dropShelf.Store(true)
	app.mu.Lock()
	app.reloadLocked()
	app.mu.Unlock()
	waitShelf("snes-zelda", savedListOfflineCopy, true)
}

func TestLibraryRetryDoesNotRestoreAnotherPlatform(t *testing.T) {
	var dropShelf atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && !strings.Contains(r.URL.RawQuery, "collection=recents"):
			if dropShelf.Load() {
				dropConn(w)
				return
			}
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
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if !snap.Loading && len(snap.Games) == 1 && snap.Games[0].ID == "snes-mario" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := app.Snapshot().Games; len(got) != 1 || got[0].ID != "snes-mario" {
		t.Fatalf("initial shelf = %+v", got)
	}

	dropShelf.Store(true)
	app.mu.Lock()
	app.platformID = "sms"
	app.reloadLocked()
	app.mu.Unlock()
	saw := false
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if strings.Contains(snap.Status, "Can't reach the library") {
			saw = true
			if len(snap.Games) != 0 {
				t.Fatalf("sms retry kept rows: %+v", snap.Games)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !saw || len(app.Snapshot().Games) != 0 {
		t.Fatalf("sms retry status=%q games=%+v", app.Snapshot().Status, app.Snapshot().Games)
	}
}

func TestTruncatedGameListIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTruncated(w, []byte(`{"games":[]}`))
	}))
	t.Cleanup(server.Close)
	_, _, err := NewClient(server.URL, server.Client()).ListGames(t.Context(), hostclient.GameListQuery{Limit: 1})
	if !errors.Is(err, hostclient.ErrResponseTruncated) {
		t.Fatalf("error = %v, want ErrResponseTruncated", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		t.Fatal("truncated body was classified as a net.Error")
	}
	if !libraryNetRetry(err) {
		t.Fatal("truncated game list should retry")
	}
}

func TestLibraryRecoversAfterATruncatedResponse(t *testing.T) {
	var drops atomic.Int32
	var attempts atomic.Int32
	drops.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && !strings.Contains(r.URL.RawQuery, "collection=recents"):
			attempts.Add(1)
			if takeDrop(&drops) {
				writeTruncated(w, []byte(`{"games":[{"id":"snes-mario"}]}`))
				return
			}
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
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	saw := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if strings.Contains(snap.Status, "Can't reach the library") || snap.LoadErr != "" {
			saw = true
		}
		if saw && !snap.Loading && snap.LoadErr == "" && len(snap.Games) == 1 && snap.Games[0].ID == "snes-mario" && attempts.Load() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap := app.Snapshot()
	t.Fatalf("truncated shelf did not recover: saw=%v status=%q err=%q games=%v attempts=%d", saw, snap.Status, snap.LoadErr, gameIDs(snap.Games), attempts.Load())
}

func TestRecentsRecoverAfterATruncatedResponse(t *testing.T) {
	var drops atomic.Int32
	var attempts atomic.Int32
	drops.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && strings.Contains(r.URL.RawQuery, "collection=recents"):
			attempts.Add(1)
			if takeDrop(&drops) {
				writeTruncated(w, []byte(`{"games":[{"id":"nes-pong"}]}`))
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
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if recentDetail(snap) == "couldn't load recently played" {
			saw = true
		}
		if saw && homeHasRow(snap, HomeKindRecent, "nes-pong") && recentDetail(snap) != "couldn't load recently played" && attempts.Load() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("truncated recents did not recover: saw=%v detail=%q attempts=%d rows=%+v", saw, recentDetail(app.Snapshot()), attempts.Load(), app.Snapshot().RoomPicker.Rows)
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

// writeTruncated advertises a longer body than it sends, which
// ReadResponseBody reports as ErrResponseTruncated.
func writeTruncated(w http.ResponseWriter, body []byte) {
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	if len(body) == 0 {
		body = []byte("{}")
	}
	_, _ = fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(body)+32)
	_, _ = rw.Write(body)
	_ = rw.Flush()
}

func gameIDs(games []hostclient.Game) []string {
	ids := make([]string, len(games))
	for i, game := range games {
		ids[i] = game.ID
	}
	return ids
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
