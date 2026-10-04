package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestStatusReportsKitLocalCoreWhileHostSessionIsIdle(t *testing.T) {
	t.Parallel()
	core := "fes.sms"
	tests := []struct {
		name  string
		phase string
		want  protocol.State
	}{
		{name: "launching", phase: "launching", want: protocol.StateLaunching},
		{name: "running", phase: "running", want: protocol.StateLocal},
		{name: "stopping", phase: "stopping", want: protocol.StateStopping},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := httpapi.New(&fakeController{status: protocol.Status{State: protocol.StateIdle}}, "test-token", "0.1.0", discardLogger(),
				httpapi.WithKitLocalRun(func() httpapi.KitLocalRun {
					return httpapi.KitLocalRun{Phase: tt.phase, CoreID: core}
				}))
			request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var status protocol.Status
			if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if status.State != tt.want || status.ObservedCore == nil || *status.ObservedCore != core || status.ExpectedCore == nil || *status.ExpectedCore != core || status.GameID != nil || status.System != nil || status.LastError != nil {
				t.Fatalf("status = %+v", status)
			}
		})
	}
}

func TestStatusKeepsHostSessionAheadOfKitLocalRun(t *testing.T) {
	t.Parallel()
	game := "sms-datastorm"
	handler := httpapi.New(&fakeController{status: protocol.Status{State: protocol.StateActive, GameID: &game}}, "test-token", "0.1.0", discardLogger(),
		httpapi.WithKitLocalRun(func() httpapi.KitLocalRun {
			return httpapi.KitLocalRun{Phase: "running", CoreID: "fes.sms"}
		}))
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var status protocol.Status
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateActive || status.GameID == nil || *status.GameID != game || status.ObservedCore != nil {
		t.Fatalf("status = %+v", status)
	}
}

func TestStatusStaysIdleWhenKitLocalRunIsIdle(t *testing.T) {
	t.Parallel()
	handler := httpapi.New(&fakeController{status: protocol.Status{State: protocol.StateIdle}}, "test-token", "0.1.0", discardLogger(),
		httpapi.WithKitLocalRun(func() httpapi.KitLocalRun {
			return httpapi.KitLocalRun{Phase: "idle", CoreID: "fes.sms"}
		}))
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var status protocol.Status
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateIdle || status.ObservedCore != nil || status.ExpectedCore != nil {
		t.Fatalf("status = %+v", status)
	}
}
