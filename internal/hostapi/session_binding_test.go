package hostapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestSessionStatusReportsBoundTarget(t *testing.T) {
	service := &fakeService{
		status:          protocol.Status{State: protocol.StateIdle},
		sessionTarget:   "kit",
		sessionTargetID: "a3cdbf5f-1a12-4a95-a820-a9b4e600769a",
	}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK {
		t.Fatalf("session = %d %s", response.Code, response.Body.String())
	}
	var result struct {
		ID       string         `json:"id"`
		Target   string         `json:"target"`
		TargetID string         `json:"target_id"`
		State    protocol.State `json:"state"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID == "" || result.Target != "kit" || result.TargetID != "a3cdbf5f-1a12-4a95-a820-a9b4e600769a" || result.State != protocol.StateIdle {
		t.Fatalf("session binding = %+v", result)
	}
}

func TestSessionLaunchBindsOptionalTarget(t *testing.T) {
	gameID := "megadrive-sonic-test"
	system := protocol.SystemMegaDrive
	service := &fakeService{
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}},
		status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system},
	}
	handler := hostapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"`+gameID+`","target":"spare"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("launch = %d %s", response.Code, response.Body.String())
	}
	if service.launchTarget != "spare" {
		t.Fatalf("launch target = %q", service.launchTarget)
	}
	var result struct {
		Target string         `json:"target"`
		State  protocol.State `json:"state"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Target != "spare" || result.State != protocol.StateActive {
		t.Fatalf("launch session = %+v", result)
	}
}
