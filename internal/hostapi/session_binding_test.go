package hostapi_test

import (
	"encoding/json"
	"net/http"
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
