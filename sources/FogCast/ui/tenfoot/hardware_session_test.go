package tenfoot

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/internal/testwait"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func hardwareBoundSession() hostclient.SessionResult {
	return hostclient.SessionResult{ID: "same-host", State: "active", GameID: "fpga-zx81", Target: "kit", TargetID: "kit-id", FlightID: "flight", CorePackage: &hostclient.SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 9}}
}

func TestHardwareRoomRejectsChangedPlayIdentity(t *testing.T) {
	expected := hardwareBoundSession()
	action := rooms.Action{SessionID: expected.ID, GameID: expected.GameID, FlightID: expected.FlightID,
		MediaBinding: protocol.DevelopmentMediaBinding{Target: expected.Target, TargetID: expected.TargetID, PackageID: expected.CorePackage.PackageID, Generation: expected.CorePackage.Generation}}
	for name, change := range map[string]func(*hostclient.SessionResult){
		"session":    func(s *hostclient.SessionResult) { s.ID = "other" },
		"game":       func(s *hostclient.SessionResult) { s.GameID = "fpga-other" },
		"flight":     func(s *hostclient.SessionResult) { s.FlightID = "other" },
		"target":     func(s *hostclient.SessionResult) { s.Target = "other" },
		"target-id":  func(s *hostclient.SessionResult) { s.TargetID = "other" },
		"package":    func(s *hostclient.SessionResult) { s.CorePackage.PackageID = strings.Repeat("b", 64) },
		"generation": func(s *hostclient.SessionResult) { s.CorePackage.Generation++ },
		"idle":       func(s *hostclient.SessionResult) { s.State = "idle" },
	} {
		t.Run(name, func(t *testing.T) {
			app := NewApp(nil, 1280, 720, 20)
			app.session = hardwareBoundSession()
			if !app.roomSessionMatchesLocked(action) {
				t.Fatal("matching play rejected")
			}
			change(&app.session)
			before := app.session
			if app.roomSessionMatchesLocked(action) || !strings.Contains(app.status, "changed") {
				t.Fatal("stale room action accepted or missing notice")
			}
			if !samePlayHIDSession(before, app.session) || app.stopPhase == "stopping" || app.tapePickerOpen {
				t.Fatal("stale action changed lifecycle")
			}
		})
	}
}

func TestHardwareStopPreservesOrdinaryPackageFreeAndDevelopmentStop(t *testing.T) {
	for _, session := range []hostclient.SessionResult{
		{ID: "host", State: "active", GameID: "nes-game", Target: "host"},
		{ID: "host", State: "active", Target: "kit", CorePackage: &hostclient.SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 9}},
	} {
		t.Run(session.GameID, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]json.RawMessage
				if r.URL.Path != "/api/v1/session/stop" || json.NewDecoder(r.Body).Decode(&body) != nil || string(body["retain_lease"]) != "true" || body["expected_session"] != nil {
					t.Error("ordinary stop request changed")
				}
				_, _ = io.WriteString(w, `{"state":"idle"}`)
			}))
			defer server.Close()
			app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
			app.session, app.stopPhase = session, "stopping"
			app.doStop(context.Background(), ClientStampNow(), session, false)
			if calls != 1 || app.session.State != "idle" || app.retryStopLock {
				t.Fatalf("ordinary Stop failed: calls=%d session=%+v retry=%v", calls, app.session, app.retryStopLock)
			}
		})
	}
}

func TestHardwareBoundStopSaveFailureKeepsPlayUntilRetry(t *testing.T) {
	prior := hardwareBoundSession()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Retain   bool `json:"retain_lease"`
			Expected struct {
				ID         string `json:"id"`
				GameID     string `json:"game_id"`
				Generation uint64 `json:"generation"`
			} `json:"expected_session"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !body.Retain || body.Expected.ID != prior.ID || body.Expected.GameID != prior.GameID || body.Expected.Generation != 9 {
			t.Error("retry lost captured Stop identity")
		}
		if calls == 1 {
			w.WriteHeader(500)
			_, _ = io.WriteString(w, `{"error":{"code":"SAVE_FAILED","message":"save failed"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.session, app.stopPhase = prior, "stopping"
	app.doStop(context.Background(), ClientStampNow(), prior, false)
	if !app.retryStopLock || !app.gpuParked || !samePlayHIDSession(prior, app.session) || app.retryStopCode != "SAVE_FAILED" {
		t.Fatalf("failed Stop lost active play or lock: %+v", app.Snapshot().Session)
	}
	app.stopPhase = "stopping"
	app.doStop(context.Background(), ClientStampNow(), prior, false)
	if calls != 2 || app.retryStopLock || app.gpuParked || app.session.State != "idle" {
		t.Fatal("successful retry did not restore idle navigation")
	}
}

func TestHardwareStopSessionChangedAllowsFreshExplicitStop(t *testing.T) {
	prior := hardwareBoundSession()
	current := hardwareBoundSession()
	current.CorePackage.Generation++
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/stop" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		calls++
		var body struct {
			Expected struct {
				Generation uint64 `json:"generation"`
			} `json:"expected_session"`
		}
		if r.URL.Path != "/api/v1/session/stop" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected request")
		}
		if calls == 1 {
			if body.Expected.Generation != prior.CorePackage.Generation {
				t.Error("initial Stop lost identity")
			}
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"SESSION_CHANGED","message":"foreground changed"}}`)
			return
		}
		if body.Expected.Generation != current.CorePackage.Generation {
			t.Error("explicit Stop reused stale identity")
		}
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.session, app.stopExpected, app.stopPhase = prior, prior, "stopping"
	app.retryStopLock = true
	app.sessionKick = make(chan struct{}, 1)
	app.doStop(context.Background(), ClientStampNow(), prior, false)
	if app.retryStopLock || app.stopExpected.ID != "" || calls != 1 || !strings.Contains(app.status, "changed") {
		t.Fatal("rejected Stop kept stale retry lock or replayed")
	}
	select {
	case <-app.sessionKick:
	default:
		t.Fatal("changed session did not request refresh")
	}
	app.mu.Lock()
	app.applySessionLocked(current)
	app.startStopLocked()
	done := app.stopWait
	app.mu.Unlock()
	<-done
	if calls != 2 || app.session.State != "idle" || app.retryStopLock {
		t.Fatal("new explicit Stop did not stop refreshed session")
	}
}

// A previous Sinclair visit must not choose the setup controls for another
// running core, or retain that core when a second session begins.
func TestPlayingSystemRoomReplacesStaleWorkshopSelection(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	app.mu.Lock()
	defer app.mu.Unlock()
	app.roomsIndex = rooms.NewIndex(rooms.Examples())
	app.openRoomLocked(hardwareRoomID)
	app.session = hardwareBoundSession()
	app.session.GameID = "coleco-game"
	app.rememberPlayingSetupLocked("fes.coleco")
	app.openPlayingHardwareRoomLocked()
	if app.room == nil || app.room.ID() != systemRoomID || !app.roomDuringPlay || app.session.GameID != "coleco-game" || app.session.CorePackage.Generation != 9 {
		t.Fatalf("wrong active room or changed session: room=%v session=%+v", app.room, app.session)
	}
	prior := app.room
	app.session.GameID = "spectrum-game"
	app.rememberPlayingSetupLocked("fes.spectrum")
	app.roomDuringPlay = false
	app.openPlayingHardwareRoomLocked()
	if app.room == nil || app.room.ID() != systemRoomID || app.room == prior {
		t.Fatal("reused the previous system selection")
	}
	app.closeAllRoomsLocked()
}

const activeSetupIdentityScript = `
function load() end
function draw()
  gfx.clear("#122738")
  gfx.text(room.initial_core_id or "missing core", 20, 20, {size=20})
  gfx.text(room.initial_game_id or "missing game", 20, 50, {size=20})
end
`

func TestActiveSinclairWorkbenchUsesCapturedLaunchOutsideFilteredShelf(t *testing.T) {
	for _, coreID := range []string{"fes.zx80", "fes.ts1000", "fes.ts1500"} {
		t.Run(coreID, func(t *testing.T) {
			app := NewApp(nil, 1280, 720, 20)
			app.roomsIndex = rooms.NewIndex([]rooms.Pack{
				testRoomPack(t, hardwareRoomID, activeSetupIdentityScript),
				testRoomPack(t, systemRoomID, activeSetupIdentityScript),
			})
			app.session = hardwareBoundSession()
			app.session.GameID = "active-sinclair"
			app.rememberPlayingSetupLocked(coreID)
			app.games = []hostclient.Game{{ID: "filtered-spectrum", CoreID: "fes.spectrum"}}
			app.loading = true
			app.openPlayingHardwareRoomLocked()
			if app.room == nil || app.room.ID() != hardwareRoomID || !app.roomDuringPlay {
				t.Fatalf("filtered shelf selected another workbench: %+v", app.Snapshot().Room)
			}
			app.Tick(time.Now())
			snap := app.Snapshot()
			foundCore, foundGame := false, false
			for _, op := range snap.Room.Frame.Ops {
				foundCore = foundCore || op.Text == coreID
				foundGame = foundGame || op.Text == app.session.GameID
			}
			if !foundCore || !foundGame || app.session.CorePackage.Generation != 9 {
				t.Fatalf("workbench lost exact active selection: %+v", snap.Room)
			}
			app.closeAllRoomsLocked()
		})
	}
}

func TestAdoptedSinclairWorkbenchReadsActivePackageOutsideShelf(t *testing.T) {
	for _, coreID := range []string{"fes.zx80", "fes.ts1000", "fes.ts1500"} {
		t.Run(coreID, func(t *testing.T) {
			prior := hardwareBoundSession()
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/debug/ui-events" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/core-packages/"+prior.CorePackage.PackageID {
					t.Errorf("unexpected lookup: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusNotFound)
					return
				}
				reads++
				json.NewEncoder(w).Encode(map[string]any{"package_id": prior.CorePackage.PackageID, "descriptor": map[string]any{"core": map[string]string{"id": coreID}}})
			}))
			defer server.Close()
			app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
			app.roomsIndex = rooms.NewIndex([]rooms.Pack{
				testRoomPack(t, hardwareRoomID, activeSetupIdentityScript),
				testRoomPack(t, systemRoomID, activeSetupIdentityScript),
			})
			app.session = prior
			app.games = []hostclient.Game{{ID: prior.GameID, CoreID: "fes.spectrum", PackageID: strings.Repeat("b", 64)}}
			app.mu.Lock()
			app.openPlayingHardwareRoomLocked()
			app.mu.Unlock()
			waitFor(t, app, "exact active Sinclair package", func(s Snapshot) bool { return s.Room.DuringPlay })
			app.mu.Lock()
			resolved, unchanged := app.playingSetupCoreLocked(), samePlayHIDSession(prior, app.session)
			app.mu.Unlock()
			snap := app.Snapshot()
			if snap.Room.ID != hardwareRoomID || resolved != coreID || !unchanged || reads != 1 {
				t.Fatalf("wrong adopted workbench: room=%s core=%s unchanged=%t reads=%d", snap.Room.ID, resolved, unchanged, reads)
			}
			app.closeAllRoomsLocked()
		})
	}
}

type playingSetupLookupTransport struct {
	base     http.RoundTripper
	contexts chan context.Context
}

func (tr playingSetupLookupTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	tr.contexts <- r.Context()
	return tr.base.RoundTrip(r)
}

func TestActiveWorkbenchDiscardsLookupAfterSessionReplacement(t *testing.T) {
	for _, change := range []string{"target", "game", "package", "generation", "flight"} {
		t.Run(change, func(t *testing.T) {
			prior := hardwareBoundSession()
			gate := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				<-gate
				json.NewEncoder(w).Encode(map[string]any{"package_id": prior.CorePackage.PackageID, "descriptor": map[string]any{"core": map[string]string{"id": "fes.zx80"}}})
			}))
			defer server.Close()
			contexts := make(chan context.Context, 1)
			httpClient := *server.Client()
			httpClient.Transport = playingSetupLookupTransport{base: http.DefaultTransport, contexts: contexts}
			app := NewApp(NewClient(server.URL, &httpClient), 1280, 720, 20)
			app.roomsIndex = rooms.NewIndex(rooms.Examples())
			app.session = prior
			app.mu.Lock()
			app.openPlayingHardwareRoomLocked()
			if app.room != nil || app.roomDuringPlay || !samePlayHIDSession(prior, app.session) {
				t.Fatal("lookup changed play before identifying the active package")
			}
			app.mu.Unlock()
			ctx := <-contexts
			next := hardwareBoundSession()
			switch change {
			case "target":
				next.Target = "other"
			case "game":
				next.GameID = "other"
			case "package":
				next.CorePackage.PackageID = strings.Repeat("b", 64)
			case "generation":
				next.CorePackage.Generation++
			case "flight":
				next.FlightID = "other"
			}
			app.mu.Lock()
			app.applySessionLocked(next)
			app.mu.Unlock()
			close(gate)
			select {
			case <-ctx.Done():
			case <-time.After(testwait.Timeout):
				t.Fatal("retired lookup did not finish")
			}
			app.mu.Lock()
			defer app.mu.Unlock()
			if app.room != nil || app.roomDuringPlay || app.playingSetupCoreLocked() != "" || !samePlayHIDSession(next, app.session) {
				t.Fatal("retired lookup opened another session's workshop")
			}
		})
	}
}

func TestActiveWorkbenchLookupDeadlineLeavesExplicitRetry(t *testing.T) {
	prior := hardwareBoundSession()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/debug/ui-events" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"package_id": prior.CorePackage.PackageID, "descriptor": map[string]any{"core": map[string]string{"id": "fes.ts1000"}}})
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.roomsIndex = rooms.NewIndex([]rooms.Pack{testRoomPack(t, hardwareRoomID, activeSetupIdentityScript)})
	app.session = prior
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	app.ctx = ctx
	app.mu.Lock()
	app.openPlayingHardwareRoomLocked()
	app.mu.Unlock()
	waitFor(t, app, "retryable package lookup", func(s Snapshot) bool { return strings.Contains(s.Status, "could not be read") })
	app.mu.Lock()
	if app.playingSetupPending || app.playingSetupCancel != nil || app.room != nil || !samePlayHIDSession(prior, app.session) {
		app.mu.Unlock()
		t.Fatal("failed lookup retained a busy request or changed play")
	}
	app.ctx = t.Context()
	app.openPlayingHardwareRoomLocked()
	app.mu.Unlock()
	waitFor(t, app, "retried exact package lookup", func(s Snapshot) bool { return s.Room.DuringPlay })
	if app.Snapshot().Room.ID != hardwareRoomID {
		t.Fatal("retry did not choose the Sinclair workbench")
	}
	app.closeAllRoomsLocked()
}

func TestCapturedWorkbenchIdentityRequiresReturnedLaunchPackage(t *testing.T) {
	for _, samePackage := range []bool{true, false} {
		name := "matching-package"
		if !samePackage {
			name = "different-active-package"
		}
		t.Run(name, func(t *testing.T) {
			active := hardwareBoundSession()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/session/launch" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				writeKitDisplaySession(w, active)
			}))
			defer server.Close()
			app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
			game := hostclient.Game{ID: active.GameID, CoreID: "fes.zx80", PackageID: active.CorePackage.PackageID}
			if !samePackage {
				game.PackageID = strings.Repeat("b", 64)
			}
			app.launch.GameID = game.ID
			app.launch.Phase = "launching"
			app.doLaunch(t.Context(), game, ClientStampNow(), "", "")
			app.mu.Lock()
			defer app.mu.Unlock()
			coreID := app.playingSetupCoreLocked()
			if samePackage && coreID != game.CoreID || !samePackage && coreID != "" {
				t.Fatalf("launch captured core %q from package %s while active package is %s", coreID, game.PackageID, active.CorePackage.PackageID)
			}
		})
	}
}
