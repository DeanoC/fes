package hostapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/google/uuid"
)

type sessionEventWire struct {
	Sequence uint64           `json:"sequence"`
	FlightID string           `json:"flight_id"`
	Event    string           `json:"event"`
	State    protocol.State   `json:"state"`
	GameID   *string          `json:"game_id"`
	System   *protocol.System `json:"system"`
	Media    string           `json:"media"`
}

func decodeSessionEvents(t *testing.T, handler http.Handler) (string, []sessionEventWire) {
	t.Helper()
	response := serve(t, handler, http.MethodGet, "/api/v1/session/events")
	if response.Code != http.StatusOK {
		t.Fatalf("session events = %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Events []sessionEventWire `json:"events"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return response.Body.String(), payload.Events
}

func requireFlightID(t *testing.T, id string) {
	t.Helper()
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id || parsed.Version() != 4 {
		t.Fatalf("flight_id = %q, want UUID v4", id)
	}
}

func eventsNamed(events []sessionEventWire, name string) []sessionEventWire {
	matched := make([]sessionEventWire, 0)
	for _, event := range events {
		if event.Event == name {
			matched = append(matched, event)
		}
	}
	return matched
}

func TestIdleSessionEventsOmitFlightID(t *testing.T) {
	handler := hostapi.New(&fakeService{status: protocol.Status{State: protocol.StateIdle}})
	serve(t, handler, http.MethodGet, "/api/v1/session")
	body, events := decodeSessionEvents(t, handler)
	if len(events) == 0 {
		t.Fatal("expected a session.status event")
	}
	if strings.Contains(body, `"flight_id"`) {
		t.Fatalf("idle events included flight_id: %s", body)
	}
	for _, event := range events {
		if event.FlightID != "" {
			t.Fatalf("idle event %#v included flight_id", event)
		}
	}
}

func TestLaunchStatusAndStopShareFlightIDOnSessionEvents(t *testing.T) {
	gameID := "megadrive-sonic-test"
	system := protocol.SystemMegaDrive
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}},
		status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)
	if response := launchSession(t, handler, gameID); response.Code != http.StatusOK {
		t.Fatalf("launch = %d %s", response.Code, response.Body.String())
	}
	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if status.Code != http.StatusOK {
		t.Fatalf("status = %d %s", status.Code, status.Body.String())
	}
	stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if stop.Code != http.StatusOK {
		t.Fatalf("stop = %d %s", stop.Code, stop.Body.String())
	}

	body, events := decodeSessionEvents(t, handler)
	launches := eventsNamed(events, "session.launch")
	statuses := eventsNamed(events, "session.status")
	stops := eventsNamed(events, "session.stop")
	if len(launches) != 1 || len(statuses) == 0 || len(stops) != 1 {
		t.Fatalf("events = %s", body)
	}
	requireFlightID(t, launches[0].FlightID)
	if launches[0].FlightID != statuses[0].FlightID || launches[0].FlightID != stops[0].FlightID {
		t.Fatalf("flight ids diverged launch=%q status=%q stop=%q body=%s", launches[0].FlightID, statuses[0].FlightID, stops[0].FlightID, body)
	}
	if !strings.Contains(body, `"flight_id":"`+launches[0].FlightID+`"`) {
		t.Fatalf("JSON omitted flight_id: %s", body)
	}
}

func TestSequentialLaunchesAllocateDistinctFlightIDs(t *testing.T) {
	firstID, secondID := "first", "second"
	system := protocol.SystemSNES
	service := &fakeService{
		execution: "host_only",
		launch:    protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &firstID, System: &system}},
		stopped:   protocol.Status{State: protocol.StateIdle},
	}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	if response := launchSession(t, handler, firstID); response.Code != http.StatusOK {
		t.Fatalf("first launch = %d %s", response.Code, response.Body.String())
	}
	service.launch = protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &secondID, System: &system}}
	if response := launchSession(t, handler, secondID); response.Code != http.StatusOK {
		t.Fatalf("second launch = %d %s", response.Code, response.Body.String())
	}

	body, events := decodeSessionEvents(t, handler)
	launches := eventsNamed(events, "session.launch")
	if len(launches) != 2 {
		t.Fatalf("launch events = %d, want 2: %s", len(launches), body)
	}
	requireFlightID(t, launches[0].FlightID)
	requireFlightID(t, launches[1].FlightID)
	if launches[0].FlightID == launches[1].FlightID {
		t.Fatalf("replacement reused flight_id %q", launches[0].FlightID)
	}
	stops := eventsNamed(events, "session.media.stop")
	if len(stops) == 0 {
		t.Fatalf("replacement media stop missing: %s", body)
	}
	if stops[0].FlightID != launches[0].FlightID {
		t.Fatalf("replacement stop flight_id = %q, want launch %q body=%s", stops[0].FlightID, launches[0].FlightID, body)
	}
	starts := eventsNamed(events, "session.media.start")
	if len(starts) < 2 {
		t.Fatalf("media starts = %d, want 2: %s", len(starts), body)
	}
	if starts[1].FlightID != launches[1].FlightID {
		t.Fatalf("second media start flight_id = %q, want %q", starts[1].FlightID, launches[1].FlightID)
	}
}

func TestOrphanedStopAllocatesFlightID(t *testing.T) {
	handler := hostapi.New(&fakeService{stopped: protocol.Status{State: protocol.StateIdle}})
	stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if stop.Code != http.StatusOK {
		t.Fatalf("stop = %d %s", stop.Code, stop.Body.String())
	}
	body, events := decodeSessionEvents(t, handler)
	stops := eventsNamed(events, "session.stop")
	if len(stops) != 1 {
		t.Fatalf("stop events = %s", body)
	}
	requireFlightID(t, stops[0].FlightID)
}

func TestDevelopmentRBFEventsIncludeFlightID(t *testing.T) {
	observed := "DEVCORE"
	service := &fakeService{
		development: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed},
		stopped:     protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", bytes.NewReader([]byte("development-rbf")))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("development-rbf = %d %s", response.Code, response.Body.String())
	}
	stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if stop.Code != http.StatusOK {
		t.Fatalf("stop = %d %s", stop.Code, stop.Body.String())
	}
	body, events := decodeSessionEvents(t, handler)
	loads := eventsNamed(events, "session.development_rbf")
	stops := eventsNamed(events, "session.stop")
	if len(loads) != 1 || len(stops) != 1 {
		t.Fatalf("events = %s", body)
	}
	requireFlightID(t, loads[0].FlightID)
	if loads[0].FlightID != stops[0].FlightID {
		t.Fatalf("development flight_id = %q stop = %q", loads[0].FlightID, stops[0].FlightID)
	}
}

func TestDescribedPackageLaunchEventsIncludeFlightID(t *testing.T) {
	core, game := "fes.pong", "core-pong"
	active := protocol.Status{
		State: protocol.StateActive, Development: true, ObservedCore: &core, GameID: &game,
		CorePackage: &protocol.CorePackageStatus{
			PackageID: strings.Repeat("a", 64), Generation: 9,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true,
		},
	}
	service := &fakeService{
		execution: fogcast.ExecutionFPGADevelopment,
		status:    active,
		launch:    protocol.CachedLaunchResponse{Status: active},
		stopped:   protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	if response := launchSession(t, handler, game); response.Code != http.StatusOK {
		t.Fatalf("package launch = %d %s", response.Code, response.Body.String())
	}
	body, events := decodeSessionEvents(t, handler)
	launches := eventsNamed(events, "session.launch")
	if len(launches) != 1 {
		t.Fatalf("package launch events = %s", body)
	}
	requireFlightID(t, launches[0].FlightID)
}
