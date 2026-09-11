package tenfoot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestConsumePlayHIDDoesNotStealAffinity(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(6)
	app.NoteInput(InputGamepad, 3)
	app.session = SessionResult{
		State: "active",
		Input: &SessionInput{State: "attached", Ready: true},
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
	app.session = SessionResult{
		State:        "active",
		CoreKeyboard: true,
		Input:        &SessionInput{State: "attached", Ready: true},
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
	if hits.Load() != 1 {
		t.Fatalf("foreign HID leaked %d posts", hits.Load())
	}
}

func TestPlaySessionMouseDoesNotStealNativeAffinity(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(6)
	now := time.Now()
	app.NoteInput(InputGamepad, 9)
	app.session = SessionResult{
		State: "active",
		Input: &SessionInput{State: "attached", Ready: true},
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
