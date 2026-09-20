package tenfoot

import (
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestConsumePlayHIDDoesNotStealAffinity(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(6)
	app.NoteInput(InputGamepad, 3)
	app.session = hostclient.SessionResult{
		State: "active",
		Input: &hostclient.SessionInput{State: "attached", Ready: true},
	}
	if !app.ForwardsPlayHID() {
		t.Fatal("expected play HID handoff")
	}
	if app.ForwardsCoreKeyboard() {
		t.Fatal("native play is not fes.keyboard")
	}
	if !app.ConsumePlayHID() {
		t.Fatal("play HID should consume sofa keys")
	}
	if got := app.Affinity(); got.Kind != InputGamepad || got.ID != 3 {
		t.Fatalf("play-session keyboard stole %#v", got)
	}
	if got := app.Snapshot().Grid.Focus; got != 0 {
		t.Fatalf("play-session keyboard stole focus %d", got)
	}
}

func TestConsumePlayHIDEscStopsWithoutStealingAffinity(t *testing.T) {
	t.Parallel()
	var stops, hidHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session/stop":
			stops.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/session/input/event":
			hidHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := pointerCatalog(6)
	app.client = NewClient(server.URL, server.Client())
	app.NoteInput(InputGamepad, 3)
	app.session = hostclient.SessionResult{
		State: "active",
		Input: &hostclient.SessionInput{State: "attached", Ready: true},
	}
	now := time.Now()
	if !app.HandlePlayHIDKey("escape", true, now) {
		t.Fatal("Esc must stay on the play HID chrome path")
	}
	if !app.Snapshot().Session.Stopping {
		t.Fatal("Esc did not stop the attached session")
	}
	if hidHits.Load() != 0 {
		t.Fatalf("Esc posted play HID %d times", hidHits.Load())
	}
	if got := app.Affinity(); got.Kind != InputGamepad || got.ID != 3 {
		t.Fatalf("Esc stole affinity %#v", got)
	}
}

func TestConsumePlayHIDLetterSStaysZX81NotStop(t *testing.T) {
	t.Parallel()
	var stops, hidHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session/stop":
			stops.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/session/input/event":
			hidHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := pointerCatalog(4)
	app.client = NewClient(server.URL, server.Client())
	app.NoteInput(InputGamepad, 1)
	app.session = hostclient.SessionResult{
		State:        "active",
		CoreKeyboard: true,
		Input:        &hostclient.SessionInput{State: "attached", Ready: true},
	}
	if !app.HandlePlayHIDKey("s", true, time.Now()) {
		t.Fatal("letter s must stay on the play HID path")
	}
	waitSnapshot(t, app, time.Second, func(Snapshot) bool { return hidHits.Load() == 1 })
	if stops.Load() != 0 {
		t.Fatalf("ZX81 letter s stopped the session, stops=%d", stops.Load())
	}
	if app.Snapshot().Session.Stopping {
		t.Fatal("ZX81 letter s must not chrome-stop")
	}
	if got := app.Affinity(); got.Kind != InputGamepad || got.ID != 1 {
		t.Fatalf("letter s stole affinity %#v", got)
	}
}

func TestHandlePlayHIDKeyNativeEncodesGamepadNotZX81(t *testing.T) {
	t.Parallel()
	var got atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/input/event" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Event *remoteinput.Event `json:"event"`
		}
		if json.Unmarshal(body, &payload) != nil || payload.Event == nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		got.Store(*payload.Event)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	app := pointerCatalog(4)
	app.client = NewClient(server.URL, server.Client())
	app.session = hostclient.SessionResult{
		State: "active",
		Input: &hostclient.SessionInput{State: "attached", Ready: true},
	}
	if app.ForwardsCoreKeyboard() {
		t.Fatal("native play is not fes.keyboard")
	}
	if !app.HandlePlayHIDKey("up", true, time.Now()) {
		t.Fatal("native up must be consumed")
	}
	waitSnapshot(t, app, time.Second, func(Snapshot) bool { return got.Load() != nil })
	event, _ := got.Load().(remoteinput.Event)
	if event.Device != remoteinput.DeviceGamepad || event.Kind != remoteinput.KindButton || event.Code != remoteinput.ButtonDPadUp {
		t.Fatalf("native up = %+v", event)
	}
	if event.Code == zx81keys.Letter('W') || event.Code >= 200 && event.Code < 256 {
		t.Fatalf("native up mis-routed as ZX81/axis %d", event.Code)
	}
	if !playhid.ChromeStop("backspace") {
		t.Fatal("Backspace remains chrome stop")
	}
}

func TestSendPlayHIDFailClosedOnForeignLease(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/input/event" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Event *remoteinput.Event `json:"event"`
		}
		if json.Unmarshal(body, &payload) != nil || payload.Event == nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	app := pointerCatalog(4)
	app.client = NewClient(server.URL, server.Client())
	app.session = hostclient.SessionResult{
		State:        "active",
		CoreKeyboard: true,
		Input:        &hostclient.SessionInput{State: "attached", Ready: true},
	}
	event := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: zx81keys.Letter('J')}
	if !app.SendPlayHID(event) {
		t.Fatal("ours should send")
	}
	waitSnapshot(t, app, time.Second, func(Snapshot) bool { return hits.Load() == 1 })

	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "held", Owner: "kit-hostless", Purpose: "offline-cache-hit-launch"}
	if app.SendPlayHID(event) {
		t.Fatal("hostless lease must fail closed")
	}
	app.kitLease = KitLeaseStatus{State: "busy", Owner: "caster"}
	if app.SendPlayHID(event) {
		t.Fatal("busy lease must fail closed")
	}
	app.kitLeaseHave = false
	app.healthHave = true
	app.health = hostclient.HealthResult{Connection: hostclient.TargetConnection{State: "recovery-required"}}
	if app.SendPlayHID(event) {
		t.Fatal("recovery-required connection must fail closed")
	}
	if hits.Load() != 1 {
		t.Fatalf("foreign HID leaked %d posts", hits.Load())
	}
}

func TestPlaySessionMouseDoesNotStealNativeAffinity(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(6)
	now := time.Now()
	app.NoteInput(InputGamepad, 9)
	app.session = hostclient.SessionResult{
		State: "active",
		Input: &hostclient.SessionInput{State: "attached", Ready: true},
	}
	x, y := cellCenter(t, app, 2)
	app.PointerMoveFrom(7, x, y, now)
	app.PointerClickFrom(7, x, y, now)
	if got := app.Affinity(); got.Kind != InputGamepad || got.ID != 9 {
		t.Fatalf("play-session mouse stole %#v", got)
	}
	if got := app.Snapshot().Grid.Focus; got != 0 {
		t.Fatalf("play-session mouse stole focus %d", got)
	}
}
