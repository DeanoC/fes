package fogcast

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

// A pre-dispatch launch rejection never claims the kit. Stop must still be
// idempotent for clean idle, without acquiring ownership or stopping a peer.
func TestStopWithoutLeaseOnlyAcceptsObservedCleanIdle(t *testing.T) {
	game, core := "retained-game", "retained-core"
	system := protocol.System("coleco")
	for _, tc := range []struct {
		name     string
		status   protocol.Status
		failRead bool
		wantOK   bool
	}{
		{"idle", protocol.Status{State: protocol.StateIdle}, false, true},
		{"foreign active", protocol.Status{State: protocol.StateActive}, false, false},
		{"idle recovery", protocol.Status{State: protocol.StateIdle, Recovery: protocol.RecoveryRebootRequired}, false, false},
		{"idle error", protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable}}, false, false},
		{"development", protocol.Status{State: protocol.StateIdle, Development: true}, false, false},
		{"retained package", protocol.Status{State: protocol.StateIdle, CorePackage: &protocol.CorePackageStatus{}}, false, false},
		{"retained game", protocol.Status{State: protocol.StateIdle, GameID: &game}, false, false},
		{"retained system", protocol.Status{State: protocol.StateIdle, System: &system}, false, false},
		{"retained expected core", protocol.Status{State: protocol.StateIdle, ExpectedCore: &core}, false, false},
		{"retained observed core", protocol.Status{State: protocol.StateIdle, ObservedCore: &core}, false, false},
		{"unreachable", protocol.Status{}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutations := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					mutations++
					http.Error(w, "no mutation allowed", 500)
					return
				}
				switch r.URL.Path {
				case "/v1/health":
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
				case "/v1/status":
					if tc.failRead {
						http.Error(w, "unavailable", 503)
						return
					}
					json.NewEncoder(w).Encode(tc.status)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			lease := targetclient.NewKitLease(base, "test", server.Client(), "idle-test", "stop")
			defer lease.Close(context.Background())
			client := targetclient.NewClient(base, "test", server.Client()).WithKitLease(lease)
			service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
			status, err := service.Stop(context.Background())
			if tc.wantOK {
				if err != nil || status.State != protocol.StateIdle {
					t.Fatalf("Stop = %+v, %v", status, err)
				}
			} else if err == nil {
				t.Fatal("unsafe status accepted as stopped")
			}
			if mutations != 0 || lease.Held() {
				t.Fatalf("Stop mutated without ownership: %d, held=%v", mutations, lease.Held())
			}
		})
	}
}
