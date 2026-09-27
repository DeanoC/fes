package hostapi_test

import (
	"encoding/json"
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
