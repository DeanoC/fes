package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			app.doStop(context.Background(), ClientStampNow(), session)
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
	app.doStop(context.Background(), ClientStampNow(), prior)
	if !app.retryStopLock || !app.gpuParked || !samePlayHIDSession(prior, app.session) || app.retryStopCode != "SAVE_FAILED" {
		t.Fatalf("failed Stop lost active play or lock: %+v", app.Snapshot().Session)
	}
	app.stopPhase = "stopping"
	app.doStop(context.Background(), ClientStampNow(), prior)
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
	app.doStop(context.Background(), ClientStampNow(), prior)
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
