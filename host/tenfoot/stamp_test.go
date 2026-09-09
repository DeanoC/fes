package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

func TestClientStampNowHasWallAndMono(t *testing.T) {
	first := ClientStampNow()
	time.Sleep(2 * time.Millisecond)
	second := ClientStampNow()
	if _, err := time.Parse(time.RFC3339Nano, first.TsUTC); err != nil {
		t.Fatalf("ts_utc = %q: %v", first.TsUTC, err)
	}
	if second.MonoMS < first.MonoMS {
		t.Fatalf("mono went backwards %d -> %d", first.MonoMS, second.MonoMS)
	}
}

func TestClientLaunchAndStopSendStampHeaders(t *testing.T) {
	t.Parallel()
	var launchHeaders http.Header
	var launchBody string
	var stopHeaders http.Header
	var stopBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			launchHeaders = r.Header.Clone()
			launchBody = string(raw)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","flight_id":"de305d54-75b4-431b-adb2-eb6b9e546014"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			stopHeaders = r.Header.Clone()
			stopBody = string(raw)
			_, _ = io.WriteString(w, `{"state":"idle","flight_id":"de305d54-75b4-431b-adb2-eb6b9e546014"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	stamp := ClientStamp{TsUTC: "2026-09-09T12:00:00.123456789Z", MonoMS: 44, FlightID: "de305d54-75b4-431b-adb2-eb6b9e546014"}
	got, err := client.LaunchStamped(context.Background(), "snes-mario", stamp)
	if err != nil {
		t.Fatal(err)
	}
	if got.FlightID != stamp.FlightID {
		t.Fatalf("launch flight_id = %q", got.FlightID)
	}
	if launchBody != `{"game_id":"snes-mario"}` {
		t.Fatalf("launch body = %q", launchBody)
	}
	if launchHeaders.Get(headerClientTsUTC) != stamp.TsUTC {
		t.Fatalf("launch ts header = %q", launchHeaders.Get(headerClientTsUTC))
	}
	if launchHeaders.Get(headerClientMonoMS) != "44" {
		t.Fatalf("launch mono header = %q", launchHeaders.Get(headerClientMonoMS))
	}
	stopped, err := client.StopStamped(context.Background(), stamp)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.FlightID != stamp.FlightID {
		t.Fatalf("stop flight_id = %q", stopped.FlightID)
	}
	if stopBody != "" {
		t.Fatalf("stop body = %q", stopBody)
	}
	if stopHeaders.Get(headerFlightID) != stamp.FlightID {
		t.Fatalf("stop flight header = %q", stopHeaders.Get(headerFlightID))
	}
}

func TestClientPostUIEvent(t *testing.T) {
	t.Parallel()
	var got UIEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/debug/ui-events" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = io.WriteString(w, `{"count":1}`)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	err := client.PostUIEvent(context.Background(), UIEvent{
		TSUTC:    "2026-09-09T12:00:00Z",
		MonoMS:   9,
		FlightID: "de305d54-75b4-431b-adb2-eb6b9e546014",
		Kind:     "ui.focus",
		Detail:   map[string]string{"game_id": "pong"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "ui.focus" || got.Layer != "ui" || got.Detail["game_id"] != "pong" {
		t.Fatalf("posted = %#v", got)
	}
}

func TestDebugHUDOffByDefaultAndPaintsWhenEnabled(t *testing.T) {
	app := NewApp(nil, 1280, 720, 4)
	if app.Snapshot().DebugHUD.Enabled {
		t.Fatal("debug HUD on by default")
	}
	app.SetDebugHUD(true)
	app.flightID = "de305d54-75b4-431b-adb2-eb6b9e546014"
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{Generation: "abcdef12deadbeef", ExpiresInMS: 72000}
	app.launch = LaunchSnapshot{Phase: "error", ErrorMessage: "host launch failed"}
	snap := app.Snapshot()
	if !snap.DebugHUD.Enabled || len(snap.DebugHUD.Lines) < 3 {
		t.Fatalf("hud = %#v", snap.DebugHUD)
	}
	joined := strings.Join(snap.DebugHUD.Lines, "\n")
	if !strings.Contains(joined, "flight de305d54") || !strings.Contains(joined, "gen abcdef12") || !strings.Contains(joined, "err host launch failed") {
		t.Fatalf("hud lines = %q", joined)
	}
	rec := gfx.NewRecorder()
	drawFrame(rec, snap, map[string]gpuTexture{}, map[string]gpuTexture{})
	found := false
	for _, call := range rec.Calls {
		if call.Op == "DrawText" && strings.Contains(call.Text, "flight de305d54") {
			found = true
			if call.Color != snap.Theme.Complete().Status {
				t.Fatalf("hud color = %#v", call.Color)
			}
		}
	}
	if !found {
		t.Fatalf("HUD DrawText missing: %#v", rec.Calls)
	}
}

func TestValidClientFlightID(t *testing.T) {
	if !validClientFlightID("de305d54-75b4-431b-adb2-eb6b9e546014") {
		t.Fatal("expected valid v4")
	}
	if validClientFlightID("not-a-uuid") || validClientFlightID("de305d54-75b4-331b-adb2-eb6b9e546014") {
		t.Fatal("accepted invalid id")
	}
}
