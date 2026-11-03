package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestSoftwareHealthNegotiatesCompactOnlyForNewHosts(t *testing.T) {
	selected := strings.Repeat("a", 64)
	var calls atomic.Int32
	socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(softwarecontrol.Status{State: softwarecontrol.State{Version: 1, Generation: 9, Selected: selected, Receipts: map[string]softwarecontrol.Receipt{}}, ActiveBundle: selected, BaseIdentity: "base"})
	}))
	handler := httpapi.New(&fakeController{}, "secret", "test", nil, httpapi.WithKitSoftware(socket))
	for _, tc := range []struct {
		name, version string
		compact       bool
	}{{"old host", "", false}, {"unknown version", "2", false}, {"new host", "1", true}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/health", nil)
			req.Header.Set("Authorization", "Bearer secret")
			if tc.version != "" {
				req.Header.Set(protocol.SoftwareIdentityHeader, tc.version)
			}
			w := httptest.NewRecorder()
			before := calls.Load()
			handler.ServeHTTP(w, req)
			if calls.Load() != before+1 {
				t.Fatal("expected exactly one supervisor call")
			}
			var health protocol.Health
			if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
				t.Fatal(err)
			}
			if tc.compact {
				if health.Software != nil || health.SoftwareIdentityV1 == nil || health.SoftwareIdentityV1.Selected != selected || health.SoftwareIdentityV1.Generation != 9 {
					t.Fatalf("compact=%+v", health)
				}
			} else if health.Software == nil || health.SoftwareIdentityV1 != nil || health.Software.Selected != selected || health.Software.Receipts == nil {
				t.Fatalf("legacy=%+v", health)
			}
			if tc.version == "" {
				// Decode the exact response with independent legacy full-status
				// and #1445 four-field wire types, not today's Health type.
				var oldFull struct {
					Software softwarecontrol.Status `json:"software"`
				}
				var oldCompact struct {
					Software struct {
						Selected   string `json:"selected"`
						Generation uint64 `json:"generation"`
					} `json:"software"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &oldFull); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(w.Body.Bytes(), &oldCompact); err != nil {
					t.Fatal(err)
				}
				if oldFull.Software.Selected != selected || oldFull.Software.Generation != 9 || oldFull.Software.Receipts == nil || oldCompact.Software.Selected != selected || oldCompact.Software.Generation != 9 {
					t.Fatalf("old readers disagree: full=%+v compact=%+v", oldFull, oldCompact)
				}
			}
		})
	}
}
