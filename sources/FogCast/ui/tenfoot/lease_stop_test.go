package tenfoot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
)

func assertStopReleasesCapturedPlay(t *testing.T, body string, prior hostclient.SessionResult) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("release stop body = %q: %v", body, err)
	}
	if string(raw["retain_lease"]) != "false" {
		t.Fatalf("retain_lease = %s, want false so idle cleanup releases the grant", raw["retain_lease"])
	}
	var expected struct {
		ID         string `json:"id"`
		GameID     string `json:"game_id"`
		Target     string `json:"target"`
		Generation uint64 `json:"generation"`
	}
	if err := json.Unmarshal(raw["expected_session"], &expected); err != nil {
		t.Fatal(err)
	}
	if expected.ID != prior.ID || expected.GameID != prior.GameID || expected.Target != prior.Target || expected.Generation != prior.CorePackage.Generation {
		t.Fatalf("expected session = %+v, want captured play %s/%s gen %d", expected, prior.ID, prior.GameID, prior.CorePackage.Generation)
	}
}

func assertStopRetainsLease(t *testing.T, body string) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("soft-stop body = %q: %v", body, err)
	}
	if string(raw["retain_lease"]) != "true" {
		t.Fatalf("retain_lease = %s, want true for sofa Soft-stop", raw["retain_lease"])
	}
}

func kitLeaseApp(t *testing.T, server *httptest.Server, session hostclient.SessionResult) *App {
	t.Helper()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.SetKitLocal(&fakeLocalCores{}, &fakePadFeed{})
	app.mu.Lock()
	app.session = session
	app.syncGPUParkLocked()
	app.mu.Unlock()
	return app
}

func holdSelectStart(app *App, now time.Time) {
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), now)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), now.Add(time.Millisecond))
	app.Tick(now.Add(time.Millisecond + localChordHold))
}

func releaseSelectStart(app *App, now time.Time) {
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, false), now)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, false), now.Add(time.Millisecond))
}

func TestSelectStartReleasesLibraryLeaseAndBackRetains(t *testing.T) {
	prior := kitDisplaySession()
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/stop" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(server.Close)

	t.Run("select-start releases and shell exit does not renew", func(t *testing.T) {
		app := kitLeaseApp(t, server, prior)
		holdSelectStart(app, time.Now())
		waitFor(t, app, "chord idle", func(s Snapshot) bool { return s.Session.State == "idle" })
		mu.Lock()
		got := append([]string(nil), bodies...)
		bodies = nil
		mu.Unlock()
		if len(got) != 1 {
			t.Fatalf("stops = %#v", got)
		}
		assertStopReleasesCapturedPlay(t, got[0], prior)
		app.Stop()
		mu.Lock()
		after := append([]string(nil), bodies...)
		bodies = nil
		mu.Unlock()
		if len(after) != 0 {
			t.Fatalf("shell exit renewed or released again: %#v", after)
		}
		app.mu.Lock()
		held := app.retainedIdleLease
		app.mu.Unlock()
		if held {
			t.Fatal("successful release still tracked a retained lease")
		}
	})

	t.Run("back soft-stop retains", func(t *testing.T) {
		app := kitLeaseApp(t, server, prior)
		t.Cleanup(app.Stop)
		app.HandleCommand(CmdBack, time.Now())
		waitFor(t, app, "soft-stop idle", func(s Snapshot) bool { return s.Session.State == "idle" })
		mu.Lock()
		got := append([]string(nil), bodies...)
		bodies = nil
		mu.Unlock()
		if len(got) != 1 {
			t.Fatalf("stops = %#v", got)
		}
		assertStopRetainsLease(t, got[0])
		app.mu.Lock()
		held := app.retainedIdleLease
		app.mu.Unlock()
		if !held {
			t.Fatal("sofa Soft-stop dropped the retained lease flag")
		}
	})
}

func TestSelectStartWithoutPackageIdentityPostsEmptyRelease(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/stop" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(server.Close)
	session := hostclient.SessionResult{ID: "host", State: "active", GameID: "nes-pong"}
	app := kitLeaseApp(t, server, session)
	t.Cleanup(app.Stop)
	holdSelectStart(app, time.Now())
	waitFor(t, app, "unbound chord idle", func(s Snapshot) bool { return s.Session.State == "idle" })
	if body != "" {
		t.Fatalf("stop body = %q, want empty explicit release", body)
	}
}

func TestSelectStartStaleIdentityDoesNotStopReplacement(t *testing.T) {
	prior := kitDisplaySession()
	original := prior.CorePackage.Generation
	var mu sync.Mutex
	var generations []uint64
	var retains []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/stop" {
			http.NotFound(w, r)
			return
		}
		var decoded struct {
			Retain   bool `json:"retain_lease"`
			Expected struct {
				Generation uint64 `json:"generation"`
			} `json:"expected_session"`
		}
		raw, _ := io.ReadAll(r.Body)
		if json.Unmarshal(raw, &decoded) != nil {
			t.Errorf("body %s", raw)
		}
		mu.Lock()
		generations = append(generations, decoded.Expected.Generation)
		if decoded.Retain {
			retains = append(retains, "true")
		} else {
			retains = append(retains, "false")
		}
		gen := decoded.Expected.Generation
		mu.Unlock()
		if gen == original {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"SESSION_CHANGED","message":"foreground changed"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(server.Close)
	app := kitLeaseApp(t, server, prior)
	t.Cleanup(app.Stop)
	now := time.Now()
	holdSelectStart(app, now)
	waitFor(t, app, "stale chord rejected", func(s Snapshot) bool {
		return strings.Contains(s.Status, "changed") && s.Session.State == "active"
	})
	app.mu.Lock()
	retry, release, held := app.retryStopLock, app.stopReleasesLease, app.retainedIdleLease
	gen := app.session.CorePackage.Generation
	if retry || release || held || gen != original {
		app.mu.Unlock()
		t.Fatalf("stale release replayed: retry=%v release=%v held=%v gen=%d", retry, release, held, gen)
	}
	app.session.CorePackage.Generation = original + 1
	app.mu.Unlock()
	releaseSelectStart(app, now.Add(2*time.Second))
	holdSelectStart(app, now.Add(3*time.Second))
	waitFor(t, app, "fresh chord idle", func(s Snapshot) bool { return s.Session.State == "idle" })
	mu.Lock()
	if len(generations) != 2 || generations[0] != original || generations[1] != original+1 {
		t.Fatalf("generations = %v", generations)
	}
	if len(retains) != 2 || retains[0] != "false" || retains[1] != "false" {
		t.Fatalf("retain flags = %v, both chords must release", retains)
	}
	mu.Unlock()
	app.mu.Lock()
	held = app.retainedIdleLease
	app.mu.Unlock()
	if held {
		t.Fatal("fresh release left the lease tracked as retained")
	}
}

func TestSelectStartReleaseFailureStaysRetryableWithoutBecomingSoftStop(t *testing.T) {
	prior := kitDisplaySession()
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/stop" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		n := len(bodies)
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"code":"MISTER_UNAVAILABLE","message":"lease release failed"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(server.Close)
	app := kitLeaseApp(t, server, prior)
	t.Cleanup(app.Stop)
	holdSelectStart(app, time.Now())
	waitFor(t, app, "release failure", func(s Snapshot) bool { return s.Session.RetryStop && s.Session.State == "active" })
	app.mu.Lock()
	failedHeld, failedRelease := app.retainedIdleLease, app.stopReleasesLease
	app.mu.Unlock()
	if !failedHeld || !failedRelease {
		t.Fatalf("failed release hid ownership: held=%v release=%v", failedHeld, failedRelease)
	}
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "retried release idle", func(s Snapshot) bool { return s.Session.State == "idle" && !s.Session.RetryStop })
	mu.Lock()
	got := append([]string(nil), bodies...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("stops = %#v", got)
	}
	assertStopReleasesCapturedPlay(t, got[0], prior)
	assertStopReleasesCapturedPlay(t, got[1], prior)
	app.mu.Lock()
	held := app.retainedIdleLease
	app.mu.Unlock()
	if held {
		t.Fatal("retried release still tracked a retained lease")
	}
}

func TestQueuedSelectStartKeepsTheRelease(t *testing.T) {
	prior := kitDisplaySession()
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/stop" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(server.Close)
	app := kitLeaseApp(t, server, prior)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.inputBusy = true
	app.mu.Unlock()
	holdSelectStart(app, time.Now())
	app.mu.Lock()
	if !app.stopQueued || !app.stopReleasesLease || app.stopPhase == "stopping" {
		t.Fatalf("queued chord = queued %v release %v phase %q", app.stopQueued, app.stopReleasesLease, app.stopPhase)
	}
	app.inputBusy = false
	app.flushQueuedStopLocked()
	done := app.stopWait
	app.mu.Unlock()
	if done == nil {
		t.Fatal("queued release did not start")
	}
	<-done
	assertStopReleasesCapturedPlay(t, body, prior)
}
