package hostclient

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCapturedSessionActionsNeverRereadForeground(t *testing.T) {
	captured := SessionResult{ID: "host-session", GameID: "core-zx81", State: "active", Target: "original-kit", TargetID: "original-id", FlightID: "11111111-1111-4111-8111-111111111111", CorePackage: &SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 1<<63 + 7, ABI: SessionCoreABI{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []SessionCoreInterface{{ID: "fes.media.blob", Major: 1}}}}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Errorf("reread foreground: %s %s", r.Method, r.URL.Path)
			http.Error(w, "replacement foreground", 500)
			return
		}
		if strings.Contains(r.URL.Path, "live-media") {
			want, _ := LiveMediaBinding(captured)
			headers := http.Header{}
			want.SetHeaders(headers)
			headers.Set(protocol.HostSessionIDHeader, captured.ID)
			for key, values := range headers {
				if r.Header.Get(key) != values[0] {
					t.Errorf("%s=%s want %s", key, r.Header.Get(key), values[0])
				}
			}
		} else {
			var raw map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Error(err)
			}
			var e map[string]any
			json.Unmarshal(raw["expected_session"], &e)
			if e["id"] != captured.ID || e["target"] != captured.Target || e["flight_id"] != captured.FlightID || e["game_id"] != captured.GameID || e["target_id"] != captured.TargetID || e["package_id"] != captured.CorePackage.PackageID {
				t.Errorf("expectation=%v", e)
			}
			var gen struct {
				Generation uint64 `json:"generation"`
			}
			json.Unmarshal(raw["expected_session"], &gen)
			if gen.Generation != captured.CorePackage.Generation {
				t.Errorf("generation lost: %d", gen.Generation)
			}
		}
		if strings.Contains(r.URL.Path, "live-media") {
			w.Write([]byte(`{"state":"active"}`))
		} else {
			w.Write([]byte(`{"state":"idle"}`))
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	if _, err := client.ReplaceLiveMediaForSession(context.Background(), captured, strings.Repeat("b", 64), "maze.p"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ClearLiveMediaForSession(context.Background(), captured); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StopExpectedStamped(context.Background(), captured, ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StopRetainLeaseExpected(context.Background(), captured, ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
}
