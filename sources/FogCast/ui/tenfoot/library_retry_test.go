package tenfoot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/internal/testwait"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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

func TestLibraryRetriesATruncatedResponseThenRestoresFocus(t *testing.T) {
	var shelfFailures atomic.Int32
	var shelfAttempts atomic.Int32
	var launches atomic.Int32
	retryRelease := make(chan struct{})
	var releaseRetry sync.Once
	shelfFailures.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && !strings.Contains(r.URL.RawQuery, "collection=recents"):
			attempt := shelfAttempts.Add(1)
			if takeDrop(&shelfFailures) {
				writeTruncated(w, []byte(`{"games":[]}`))
				return
			}
			if attempt > 1 {
				select {
				case <-retryRelease:
				case <-r.Context().Done():
					return
				}
			}
			writeGames(w, availableGame("snes-mario", "Mario", "snes"), availableGame("megadrive-sonic", "Sonic", "megadrive"))
		case r.URL.Path == "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case r.URL.Path == "/api/v1/library/collections":
			_, _ = w.Write([]byte(`{"collections":[]}`))
		case r.URL.Path == "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case r.URL.Path == "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
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
	deadline := time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if strings.Contains(snap.Status, "Can't reach the library") {
			sawBanner = true
			releaseRetry.Do(func() { close(retryRelease) })
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

	shelfFailures.Store(1)
	before := shelfAttempts.Load()
	app.mu.Lock()
	app.reloadLocked()
	app.mu.Unlock()
	deadline = time.Now().Add(testwait.Timeout)
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
	retryRelease := make(chan struct{})
	var releaseRetry sync.Once
	release := func() { releaseRetry.Do(func() { close(retryRelease) }) }
	recentDrops.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && strings.Contains(r.URL.RawQuery, "collection=recents"):
			if takeDrop(&recentDrops) {
				dropConn(w)
				return
			}
			// Keep recovery behind the error observation instead of racing Tick.
			select {
			case <-retryRelease:
			case <-r.Context().Done():
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

	client := server.Client()
	// A reused connection lets net/http silently retry the injected disconnect.
	// Force a fresh connection so the application observes that failure.
	client.Transport.(*http.Transport).DisableKeepAlives = true
	app := NewApp(NewClient(server.URL, client), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	}), "")
	app.SetHomeRooms(true)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	t.Cleanup(release)

	saw := false
	deadline := time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if recentDetail(snap) == "couldn't load recently played" {
			saw = true
			release()
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
	deadline := time.Now().Add(testwait.Timeout)
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
	deadline := time.Now().Add(testwait.Timeout)
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
		deadline := time.Now().Add(testwait.Timeout)
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
	deadline := time.Now().Add(testwait.Timeout)
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
	deadline := time.Now().Add(testwait.Timeout)
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
	deadline = time.Now().Add(testwait.Timeout)
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

func TestApplyCatalogPageRestoresNavigatedTitleAfterPartialClamp(t *testing.T) {
	t.Parallel()
	app := NewApp(nil, 1280, 720, 10)
	full := make([]hostclient.Game, 6)
	for i := range full {
		id := fmt.Sprintf("snes-t%d", i)
		full[i] = availableGame(id, id, "snes")
	}
	app.games = append([]hostclient.Game(nil), full...)
	app.grid.SetCount(len(app.games))
	app.grid.Focus = 0
	pinned := true
	lastFocus := -1
	navID := ""
	app.applyCatalogPageLocked(full, "snes-t0", 0, &pinned, &lastFocus, &navID)
	if !pinned || app.grid.Focus != 0 || navID != "" {
		t.Fatalf("reload focus pinned=%v focus=%d nav=%q", pinned, app.grid.Focus, navID)
	}
	app.grid.Focus = 5
	app.navDirty = true
	app.applyCatalogPageLocked(full, "snes-t0", 0, &pinned, &lastFocus, &navID)
	if pinned || navID != "snes-t5" || app.grid.Focus != 5 {
		t.Fatalf("navigation pinned=%v focus=%d nav=%q", pinned, app.grid.Focus, navID)
	}
	app.applyCatalogPageLocked(full[:2], "snes-t0", 0, &pinned, &lastFocus, &navID)
	if navID != "snes-t5" || app.grid.Focus != 1 {
		t.Fatalf("partial clamp focus=%d nav=%q", app.grid.Focus, navID)
	}
	app.applyCatalogPageLocked(full, "snes-t0", 0, &pinned, &lastFocus, &navID)
	if app.grid.Focus != 5 || app.games[app.grid.Focus].ID != "snes-t5" {
		t.Fatalf("restored focus=%d id=%s", app.grid.Focus, app.games[app.grid.Focus].ID)
	}
}

func TestLibraryRetrySurvivesCachedLaunchAndGPUPark(t *testing.T) {
	var dropShelf atomic.Bool
	var shelfID atomic.Value
	var attempts atomic.Int32
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	shelfID.Store("snes-mario")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && !strings.Contains(r.URL.RawQuery, "collection=recents"):
			attempts.Add(1)
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario","system":"snes"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","system":"snes"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			mu.Lock()
			sessionJSON = `{"state":"idle"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.URL.Path == "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case r.URL.Path == "/api/v1/library/collections":
			_, _ = w.Write([]byte(`{"collections":[]}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitShelf := func(id, status string) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		var loading bool
		var gotStatus string
		var games []string
		var parked bool
		for time.Now().Before(deadline) {
			app.Tick(time.Now())
			app.mu.Lock()
			loading = app.loading
			gotStatus = app.status
			games = gameIDs(app.games)
			parked = app.gpuParked
			app.mu.Unlock()
			if !loading && !parked && len(games) == 1 && games[0] == id && (status == "" || gotStatus == status) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("shelf %s status %q: loading=%v parked=%v status=%q games=%v attempts=%d", id, status, loading, parked, gotStatus, games, attempts.Load())
	}
	waitShelf("snes-mario", "")

	dropShelf.Store(true)
	app.mu.Lock()
	app.reloadLocked()
	app.mu.Unlock()
	waitShelf("snes-mario", savedListOfflineCopy)

	beforePark := attempts.Load()
	app.Press(CmdSelect, time.Now())
	deadline := time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if snap.GPUParked && snap.Session.State == "active" && snap.Session.GameID == "snes-mario" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap := app.Snapshot(); !snap.GPUParked || snap.Session.GameID != "snes-mario" {
		t.Fatalf("launch did not park: %#v", snap.Session)
	}
	// One in-flight request can still land after cancel. A second request
	// means the parked generation restarted the retry.
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if attempts.Load() >= beforePark+2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if attempts.Load() < beforePark+2 {
		t.Fatalf("retry stopped while parked: before=%d now=%d", beforePark, attempts.Load())
	}

	app.Press(CmdBack, time.Now())
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if !snap.GPUParked && snap.Session.State != "active" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap := app.Snapshot(); snap.GPUParked || snap.Session.State == "active" {
		t.Fatalf("stop did not return from play: parked=%v session=%#v", snap.GPUParked, snap.Session)
	}
	app.mu.Lock()
	returnedGames := gameIDs(app.games)
	returnedRetry := app.libraryRetrying
	app.mu.Unlock()
	if !returnedRetry || len(returnedGames) != 1 || returnedGames[0] != "snes-mario" {
		t.Fatalf("return from play lost the held retry: retrying=%v games=%v", returnedRetry, returnedGames)
	}

	shelfID.Store("snes-zelda")
	dropShelf.Store(false)
	waitShelf("snes-zelda", "")
}

func TestSupersededFilterRetryStaysCancelledAcrossGPUPark(t *testing.T) {
	var dropShelf atomic.Bool
	var mu sync.Mutex
	var nonsms, sms int
	var count bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && !strings.Contains(r.URL.RawQuery, "collection=recents"):
			platform := r.URL.Query().Get("platform")
			mu.Lock()
			if count {
				if platform == "sms" {
					sms++
				} else {
					nonsms++
				}
			}
			mu.Unlock()
			if dropShelf.Load() {
				dropConn(w)
				return
			}
			if platform == "sms" {
				writeGames(w, availableGame("sms-sonic", "Sonic", "sms"))
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
	deadline := time.Now().Add(testwait.Timeout)
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
	app.reloadLocked()
	app.mu.Unlock()
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		app.mu.Lock()
		held := !app.loading && app.libraryRetrying && len(app.games) == 1 && app.games[0].ID == "snes-mario"
		app.mu.Unlock()
		if held {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.mu.Lock()
	held := !app.loading && app.libraryRetrying && len(app.games) == 1
	app.mu.Unlock()
	if !held {
		t.Fatalf("mario retry was not held: status=%q games=%v", app.Snapshot().Status, gameIDs(app.Snapshot().Games))
	}

	app.mu.Lock()
	app.platformID = "sms"
	app.reloadLocked()
	app.mu.Unlock()
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if strings.Contains(snap.Status, "Can't reach the library") && len(snap.Games) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap := app.Snapshot(); len(snap.Games) != 0 || !strings.Contains(snap.Status, "Can't reach the library") {
		t.Fatalf("sms retry status=%q games=%+v", snap.Status, snap.Games)
	}
	// Let the superseded generation's in-flight request finish before counting.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	count = true
	sms, nonsms = 0, 0
	mu.Unlock()

	app.mu.Lock()
	app.session.State = "active"
	app.session.GameID = "snes-mario"
	app.syncGPUParkLocked()
	parked := app.gpuParked
	app.mu.Unlock()
	if !parked {
		t.Fatal("session did not park")
	}
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		mu.Lock()
		got := sms
		mu.Unlock()
		if got >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	gotSMS, gotOther := sms, nonsms
	mu.Unlock()
	if gotSMS < 2 || gotOther != 0 {
		t.Fatalf("park restarted the old filter: sms=%d other=%d", gotSMS, gotOther)
	}

	app.mu.Lock()
	app.session.State = "idle"
	app.syncGPUParkLocked()
	app.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	gotOther = nonsms
	smsBeforeRecover := sms
	mu.Unlock()
	if gotOther != 0 {
		t.Fatalf("unpark resumed the superseded filter: other=%d", gotOther)
	}

	dropShelf.Store(false)
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if !snap.GPUParked && !snap.Loading && len(snap.Games) == 1 && snap.Games[0].ID == "sms-sonic" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	gotSMS, gotOther = sms, nonsms
	mu.Unlock()
	snap := app.Snapshot()
	if snap.GPUParked || len(snap.Games) != 1 || snap.Games[0].ID != "sms-sonic" || gotOther != 0 || gotSMS <= smsBeforeRecover {
		t.Fatalf("sms shelf after park = %+v status=%q sms=%d other=%d before=%d", gameIDs(snap.Games), snap.Status, gotSMS, gotOther, smsBeforeRecover)
	}
}

func TestHeldRetryKeepsNavigatedTitleAcrossPartialPage(t *testing.T) {
	games := make([]hostclient.Game, 6)
	for i := range games {
		id := fmt.Sprintf("snes-t%d", i)
		games[i] = availableGame(id, id, "snes")
	}
	var mode atomic.Value
	var partialDrops atomic.Int32
	mode.Store("full")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games" && !strings.Contains(r.URL.RawQuery, "collection=recents"):
			switch mode.Load() {
			case "drop":
				dropConn(w)
				return
			case "partial":
				if r.URL.Query().Get("cursor") == "" {
					writeGamePage(w, games[:2], "p2")
					return
				}
				partialDrops.Add(1)
				dropConn(w)
				return
			default:
				writeCatalogPage(w, games, r.URL.Query().Get("cursor"), 2)
			}
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
	app.pageLimit = 2
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	deadline := time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if !snap.Loading && len(snap.Games) == len(games) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := app.Snapshot().Games; len(got) != len(games) {
		t.Fatalf("initial shelf = %v", gameIDs(got))
	}

	mode.Store("drop")
	app.mu.Lock()
	app.reloadLocked()
	app.mu.Unlock()
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		app.mu.Lock()
		held := !app.loading && app.status == savedListOfflineCopy && len(app.games) == len(games)
		app.mu.Unlock()
		if held {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.mu.Lock()
	held := !app.loading && app.status == savedListOfflineCopy && len(app.games) == len(games)
	app.mu.Unlock()
	if !held {
		t.Fatalf("held shelf status=%q games=%v", app.Snapshot().Status, gameIDs(app.Snapshot().Games))
	}
	if !app.focusIndex(len(games) - 1) {
		t.Fatal("focus last title")
	}
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-t5" {
		t.Fatalf("selected before partial = %#v ok=%v", selected, ok)
	}

	mode.Store("partial")
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if len(snap.Games) != len(games) {
			t.Fatalf("partial retry published %d games, focus %d", len(snap.Games), snap.Grid.Focus)
		}
		if partialDrops.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if partialDrops.Load() < 2 {
		t.Fatalf("partial drops = %d", partialDrops.Load())
	}
	selected, ok = app.Selected()
	snap := app.Snapshot()
	if !ok || selected.ID != "snes-t5" || len(snap.Games) != len(games) || snap.Status != savedListOfflineCopy {
		t.Fatalf("focus after partial failure = %#v ok=%v status=%q games=%v focus=%d", selected, ok, snap.Status, gameIDs(snap.Games), snap.Grid.Focus)
	}

	mode.Store("full")
	deadline = time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap = app.Snapshot()
		selected, ok = app.Selected()
		if len(snap.Games) != len(games) {
			t.Fatalf("recovery published a partial shelf: %v focus=%d status=%q", gameIDs(snap.Games), snap.Grid.Focus, snap.Status)
		}
		if ok && selected.ID == "snes-t5" && !snap.Loading && snap.Status != savedListOfflineCopy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	selected, ok = app.Selected()
	snap = app.Snapshot()
	t.Fatalf("navigation lost on recovery: selected=%#v ok=%v status=%q games=%v focus=%d", selected, ok, snap.Status, gameIDs(snap.Games), snap.Grid.Focus)
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
	deadline := time.Now().Add(testwait.Timeout)
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
	}), "")
	app.SetHomeRooms(true)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	saw := false
	deadline := time.Now().Add(testwait.Timeout)
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
	writeGamePage(w, games, "")
}

func writeGamePage(w http.ResponseWriter, games []hostclient.Game, next string) {
	payload := map[string]any{"games": games}
	if next != "" {
		payload["next_cursor"] = next
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func writeCatalogPage(w http.ResponseWriter, games []hostclient.Game, cursor string, limit int) {
	idx := 0
	if cursor != "" {
		idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
	}
	if idx < 0 || idx >= len(games) {
		writeGamePage(w, []hostclient.Game{}, "")
		return
	}
	end := idx + limit
	if end > len(games) {
		end = len(games)
	}
	next := ""
	if end < len(games) {
		next = "p" + strconv.Itoa(end)
	}
	writeGamePage(w, games[idx:end], next)
}

func recentDetail(snap Snapshot) string {
	for _, row := range snap.RoomPicker.Rows {
		if row.Kind == HomeKindRecents {
			return row.Detail
		}
	}
	return ""
}
