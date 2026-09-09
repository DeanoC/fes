package hostapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type stampedSessionEvent struct {
	Sequence     uint64         `json:"sequence"`
	FlightID     string         `json:"flight_id"`
	TSUTC        string         `json:"ts_utc"`
	MonoMS       int64          `json:"mono_ms"`
	ClientTSUTC  string         `json:"client_ts_utc"`
	ClientMonoMS *int64         `json:"client_mono_ms"`
	Event        string         `json:"event"`
	State        protocol.State `json:"state"`
	GameID       *string        `json:"game_id"`
}

func decodeStampedEvents(t *testing.T, handler http.Handler) []stampedSessionEvent {
	t.Helper()
	response := serve(t, handler, http.MethodGet, "/api/v1/session/events")
	if response.Code != http.StatusOK {
		t.Fatalf("session events = %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Events []stampedSessionEvent `json:"events"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Events
}

func TestLaunchAndStopRecordClientStampsAndHostClocks(t *testing.T) {
	gameID := "megadrive-sonic-test"
	system := protocol.SystemMegaDrive
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}},
		status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)
	clientTS := "2026-09-09T12:00:00.123456789Z"
	clientMono := int64(450)
	launch := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"megadrive-sonic-test"}`))
	launch.Host = "127.0.0.1"
	launch.Header.Set("X-FogCast-Client-Ts-Utc", clientTS)
	launch.Header.Set("X-FogCast-Client-Mono-Ms", "450")
	launchResponse := httptest.NewRecorder()
	handler.ServeHTTP(launchResponse, launch)
	if launchResponse.Code != http.StatusOK {
		t.Fatalf("launch = %d %s", launchResponse.Code, launchResponse.Body.String())
	}
	var launched struct {
		FlightID string `json:"flight_id"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(launchResponse.Body.Bytes(), &launched); err != nil {
		t.Fatal(err)
	}
	requireFlightID(t, launched.FlightID)

	stop := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", nil)
	stop.Host = "127.0.0.1"
	stop.Header.Set("X-FogCast-Client-Ts-Utc", "2026-09-09T12:00:01.000000000Z")
	stop.Header.Set("X-FogCast-Client-Mono-Ms", "1450")
	stop.Header.Set("X-FogCast-Flight-Id", launched.FlightID)
	stopResponse := httptest.NewRecorder()
	handler.ServeHTTP(stopResponse, stop)
	if stopResponse.Code != http.StatusOK {
		t.Fatalf("stop = %d %s", stopResponse.Code, stopResponse.Body.String())
	}

	events := decodeStampedEvents(t, handler)
	var launchEvent, stopEvent stampedSessionEvent
	for _, event := range events {
		switch event.Event {
		case "session.launch":
			launchEvent = event
		case "session.stop":
			stopEvent = event
		}
	}
	if launchEvent.Event == "" || stopEvent.Event == "" {
		t.Fatalf("missing launch/stop events: %#v", events)
	}
	if launchEvent.FlightID != launched.FlightID || stopEvent.FlightID != launched.FlightID {
		t.Fatalf("flight ids diverged launch=%q stop=%q want %q", launchEvent.FlightID, stopEvent.FlightID, launched.FlightID)
	}
	if _, err := time.Parse(time.RFC3339Nano, launchEvent.TSUTC); err != nil {
		t.Fatalf("host ts_utc = %q: %v", launchEvent.TSUTC, err)
	}
	if launchEvent.MonoMS < 0 {
		t.Fatalf("host mono_ms = %d", launchEvent.MonoMS)
	}
	if launchEvent.ClientTSUTC != clientTS {
		t.Fatalf("client_ts_utc = %q, want %q", launchEvent.ClientTSUTC, clientTS)
	}
	if launchEvent.ClientMonoMS == nil || *launchEvent.ClientMonoMS != clientMono {
		t.Fatalf("client_mono_ms = %v, want %d", launchEvent.ClientMonoMS, clientMono)
	}
	if stopEvent.ClientTSUTC != "2026-09-09T12:00:01Z" && stopEvent.ClientTSUTC != "2026-09-09T12:00:01.000000000Z" {
		t.Fatalf("stop client_ts_utc = %q", stopEvent.ClientTSUTC)
	}
}

func TestInvalidClientStampsAreIgnoredWithoutFailingLaunch(t *testing.T) {
	gameID := "megadrive-sonic-test"
	system := protocol.SystemMegaDrive
	service := &fakeService{
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}},
	}
	handler := hostapi.New(service)
	launch := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"megadrive-sonic-test","client_ts_utc":"not-a-time","client_mono_ms":-3}`))
	launch.Host = "127.0.0.1"
	launch.Header.Set("X-FogCast-Client-Ts-Utc", "nope")
	launch.Header.Set("X-FogCast-Client-Mono-Ms", "nope")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, launch)
	if response.Code != http.StatusOK {
		t.Fatalf("launch = %d %s", response.Code, response.Body.String())
	}
	events := decodeStampedEvents(t, handler)
	var launchEvent stampedSessionEvent
	for _, event := range events {
		if event.Event == "session.launch" {
			launchEvent = event
		}
	}
	if launchEvent.Event == "" {
		t.Fatal("missing launch event")
	}
	if launchEvent.ClientTSUTC != "" || launchEvent.ClientMonoMS != nil {
		t.Fatalf("invalid stamp leaked: %#v", launchEvent)
	}
	if launchEvent.TSUTC == "" {
		t.Fatal("host clock missing")
	}
}

func TestEmptyStopBodyStillStops(t *testing.T) {
	handler := hostapi.New(&fakeService{stopped: protocol.Status{State: protocol.StateIdle}})
	stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if stop.Code != http.StatusOK {
		t.Fatalf("stop = %d %s", stop.Code, stop.Body.String())
	}
}

func TestUIEventIngestAndPoll(t *testing.T) {
	handler := hostapi.New(&fakeService{status: protocol.Status{State: protocol.StateIdle}})
	flight := "de305d54-75b4-431b-adb2-eb6b9e546014"
	body := `{"ts_utc":"2026-09-09T12:00:00.250Z","mono_ms":12,"flight_id":"` + flight + `","kind":"ui.focus","detail":{"game_id":"pong","token":"secret-should-drop"}}`
	post := httptest.NewRequest(http.MethodPost, "/api/v1/debug/ui-events", strings.NewReader(body))
	post.Host = "127.0.0.1"
	posted := httptest.NewRecorder()
	handler.ServeHTTP(posted, post)
	if posted.Code != http.StatusOK {
		t.Fatalf("ui ingest = %d %s", posted.Code, posted.Body.String())
	}
	get := serve(t, handler, http.MethodGet, "/api/v1/debug/ui-events?after=0")
	if get.Code != http.StatusOK {
		t.Fatalf("ui poll = %d %s", get.Code, get.Body.String())
	}
	var payload struct {
		Events []struct {
			Sequence uint64         `json:"sequence"`
			Kind     string         `json:"kind"`
			Layer    string         `json:"layer"`
			FlightID string         `json:"flight_id"`
			TSUTC    string         `json:"ts_utc"`
			MonoMS   int64          `json:"mono_ms"`
			Detail   map[string]any `json:"detail"`
		} `json:"events"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 1 {
		t.Fatalf("events = %#v", payload.Events)
	}
	event := payload.Events[0]
	if event.Sequence != 1 || event.Kind != "ui.focus" || event.Layer != "ui" || event.FlightID != flight {
		t.Fatalf("event = %#v", event)
	}
	if event.MonoMS != 12 {
		t.Fatalf("mono_ms = %d", event.MonoMS)
	}
	if event.Detail["game_id"] != "pong" {
		t.Fatalf("detail = %#v", event.Detail)
	}
	if _, ok := event.Detail["token"]; ok {
		t.Fatalf("secret leaked: %#v", event.Detail)
	}
}

func TestUIEventRejectsUnknownKind(t *testing.T) {
	handler := hostapi.New(&fakeService{status: protocol.Status{State: protocol.StateIdle}})
	post := httptest.NewRequest(http.MethodPost, "/api/v1/debug/ui-events", strings.NewReader(`{"ts_utc":"2026-09-09T12:00:00Z","kind":"ui.explode"}`))
	post.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, post)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
}
