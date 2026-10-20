package hostapi_test

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExpectedStopRejectsStalePlayBeforeDetach(t *testing.T) {
	for _, field := range []string{"id", "flight_id", "game_id", "target", "target_id", "package_id", "generation", "valid"} {
		t.Run(field, func(t *testing.T) {
			game := "core-zx81"
			svc := &fakeService{status: protocol.Status{State: protocol.StateActive, GameID: &game, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9}}, sessionTarget: "kit", sessionTargetID: "kit-id", stopped: protocol.Status{State: protocol.StateIdle}}
			input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
			handler := hostapi.New(svc, hostapi.WithRemoteInput(input))
			before := serve(t, handler, http.MethodGet, "/api/v1/session")
			var actual map[string]any
			if err := json.Unmarshal(before.Body.Bytes(), &actual); err != nil {
				t.Fatal(err)
			}
			input.detach = nil
			expected := map[string]any{"id": actual["id"], "flight_id": actual["flight_id"], "game_id": game, "target": "kit", "target_id": "kit-id", "package_id": strings.Repeat("a", 64), "generation": 9}
			if expected["flight_id"] == nil {
				expected["flight_id"] = ""
			}
			switch field {
			case "id":
				expected[field] = "old-session"
			case "flight_id":
				expected[field] = "11111111-1111-4111-8111-111111111111"
			case "game_id", "target", "target_id":
				expected[field] = "other"
			case "package_id":
				expected[field] = strings.Repeat("b", 64)
			case "generation":
				expected[field] = 10
			}
			raw, _ := json.Marshal(map[string]any{"expected_session": expected})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(string(raw)))
			req.Host = "127.0.0.1"
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if field == "valid" {
				if w.Code != 200 || input.status.State != host.RemoteInputDetached || len(svc.stopCtxErrs) != 1 {
					t.Fatalf("valid=%d %s detach=%v stops=%d", w.Code, w.Body, input.detach, len(svc.stopCtxErrs))
				}
			} else if w.Code != 409 || len(input.detach) != 0 || len(svc.stopCtxErrs) != 0 {
				t.Fatalf("stale=%d %s detach=%v stops=%d", w.Code, w.Body, input.detach, len(svc.stopCtxErrs))
			}
		})
	}
}

// The public observation can still describe the displayed play while service
// lifecycle admission has moved to another foreground. Neither Stop API may
// tear down the replacement play when that final binding check rejects.
type lifecycleRejectedStopService struct{ *fakeService }

func (s *lifecycleRejectedStopService) StopExpected(context.Context, fogcast.SessionStopBinding) (protocol.Status, error) {
	return protocol.Status{}, fogcast.ErrSessionChanged
}
func (s *lifecycleRejectedStopService) StopExpectedWithPreparation(context.Context, fogcast.SessionStopBinding, func(context.Context)) (protocol.Status, error) {
	return protocol.Status{}, fogcast.ErrSessionChanged
}
func TestPackageFreeExpectedStopRejectsReplacementBeforeStop(t *testing.T) {
	game := "nes-pong"
	system := protocol.SystemNES
	active := protocol.Status{State: protocol.StateActive, GameID: &game, System: &system}
	service := &fakeService{
		execution:       fogcast.ExecutionFPGANative,
		sessionTarget:   "kit",
		sessionTargetID: "kit-id",
		status:          active,
		launch:          protocol.CachedLaunchResponse{Status: active},
		stopped:         protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)
	if launch := launchSessionOn(t, handler, game, "kit"); launch.Code != http.StatusOK {
		t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
	}
	before := serve(t, handler, http.MethodGet, "/api/v1/session")
	var actual struct {
		ID       string          `json:"id"`
		FlightID string          `json:"flight_id"`
		Target   string          `json:"target"`
		TargetID string          `json:"target_id"`
		Core     json.RawMessage `json:"core_package"`
		GameID   string          `json:"game_id"`
	}
	if err := json.Unmarshal(before.Body.Bytes(), &actual); err != nil || actual.ID == "" || actual.FlightID == "" || actual.GameID != game || len(actual.Core) != 0 {
		t.Fatalf("session %d %s: %v", before.Code, before.Body.String(), err)
	}
	post := func(gameID, flight string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{
			"retain_lease": false,
			"expected_session": map[string]any{
				"id": actual.ID, "flight_id": flight, "game_id": gameID,
				"target": actual.Target, "target_id": actual.TargetID,
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(string(raw)))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	other := "snes-other"
	service.status.GameID = &other
	rejected := post(game, actual.FlightID)
	if rejected.Code != http.StatusConflict || len(service.stopCtxErrs) != 0 {
		t.Fatalf("replacement = %d %s stops=%d", rejected.Code, rejected.Body.String(), len(service.stopCtxErrs))
	}
	service.status.GameID = &game
	accepted := post(game, actual.FlightID)
	if accepted.Code != http.StatusOK || len(service.stopCtxErrs) != 1 {
		t.Fatalf("matching package-free stop = %d %s stops=%d", accepted.Code, accepted.Body.String(), len(service.stopCtxErrs))
	}
}

func TestExpectedStopLifecycleRejectionPreservesAttachedInput(t *testing.T) {
	game := "core-zx81"
	svc := &lifecycleRejectedStopService{&fakeService{status: protocol.Status{State: protocol.StateActive, GameID: &game, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9}}, sessionTarget: "kit", sessionTargetID: "kit-id"}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	handler := hostapi.New(svc, hostapi.WithRemoteInput(input))
	before := serve(t, handler, http.MethodGet, "/api/v1/session")
	var actual map[string]any
	if err := json.Unmarshal(before.Body.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	input.detach = nil
	input.status = host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}
	expected := map[string]any{"id": actual["id"], "flight_id": actual["flight_id"], "game_id": game, "target": "kit", "target_id": "kit-id", "package_id": strings.Repeat("a", 64), "generation": 9}
	if expected["flight_id"] == nil {
		expected["flight_id"] = ""
	}
	raw, _ := json.Marshal(map[string]any{"expected_session": expected})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(string(raw)))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 409 || len(input.detach) != 0 || input.status.State != host.RemoteInputAttached || len(svc.stopCtxErrs) != 0 {
		t.Fatalf("response=%d %s detach=%v input=%+v stops=%d", w.Code, w.Body, input.detach, input.status, len(svc.stopCtxErrs))
	}
}
