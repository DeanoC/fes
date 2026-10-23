package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func displaySessionFixture() SessionResult {
	return SessionResult{ID: "host-session", State: "active", GameID: "fpga-zx81", Target: "kit-a", TargetID: "target-a", FlightID: "flight-a", CorePackage: &SessionCorePackage{
		PackageID: strings.Repeat("a", 64), Generation: 1<<63 + 7, ABI: SessionCoreABI{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []SessionCoreInterface{{ID: "fes.video.session-display", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}},
	}}
}

func displaySessionJSON(s SessionResult) any {
	return map[string]any{"id": s.ID, "state": s.State, "game_id": s.GameID, "target": s.Target, "target_id": s.TargetID, "flight_id": s.FlightID, "core_package": s.CorePackage}
}

func TestSessionDisplayCapabilityNeedsObservedVersionAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*SessionCorePackage)
	}{
		{"ABI", func(p *SessionCorePackage) { p.ABI.ID = "fes.simple-game" }},
		{"display version", func(p *SessionCorePackage) { p.ActiveInterfaces[0].Minor = 1 }},
		{"DDR version", func(p *SessionCorePackage) { p.ActiveInterfaces[1].Major = 2 }},
		{"missing DDR", func(p *SessionCorePackage) { p.ActiveInterfaces = p.ActiveInterfaces[:1] }},
		{"unidentified", func(p *SessionCorePackage) { p.Generation = 0 }},
		{"package identity", func(p *SessionCorePackage) { p.PackageID = "fes.zx81" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := displaySessionFixture().CorePackage
			if !SessionDisplayCapable(p) {
				t.Fatal("identified contract rejected")
			}
			tc.edit(p)
			if SessionDisplayCapable(p) {
				t.Fatal("unobserved or incompatible display accepted")
			}
		})
	}
}

func TestCapturedDisplayOpUsesOriginalTargetAndLosslessGeneration(t *testing.T) {
	prior := displaySessionFixture()
	var sent []protocol.SessionDisplayRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/session/display" {
			t.Error("display request reread foreground or changed endpoint")
		}
		binding, ok := protocol.DevelopmentMediaHeaders(r.Header)
		if !ok || binding.PackageID != prior.CorePackage.PackageID || binding.Generation != prior.CorePackage.Generation || binding.Target != prior.Target || binding.TargetID != prior.TargetID || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID {
			t.Errorf("captured display binding lost: %+v, %v", binding, ok)
		}
		var request protocol.SessionDisplayRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.Visible == nil {
			t.Error("display request did not carry an explicit visible boolean")
			return
		}
		// Only a surround names focus, so an older host still decodes the rest.
		if (request.Focus != "") != (*request.Visible && request.Focus == protocol.SessionFocusMachine) {
			t.Errorf("display request focus %q for visible %v", request.Focus, *request.Visible)
		}
		sent = append(sent, request)
		json.NewEncoder(w).Encode(displaySessionJSON(prior))
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	want := []protocol.SessionDisplay{protocol.SessionDisplayShown, protocol.SessionDisplayFocused, protocol.SessionDisplayClosed}
	for _, display := range want {
		if _, err := client.SetSessionDisplayForSession(context.Background(), prior, display); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.SetSessionDisplayForSession(context.Background(), prior, protocol.SessionDisplay{Focus: protocol.SessionFocusLauncher}); err == nil {
		t.Fatal("closed display with launcher focus was sent")
	}
	if len(sent) != len(want) {
		t.Fatalf("display mutations: %+v", sent)
	}
	for i, request := range sent {
		if got, ok := request.Display(); !ok || got != want[i] {
			t.Fatalf("display mutation %d: %+v", i, request)
		}
	}
}

func TestDisplayResponseCannotFollowReplacementOrOtherKit(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*SessionResult)
	}{
		{"same generation on another kit", func(s *SessionResult) { s.Target = "kit-b"; s.TargetID = "target-b" }},
		{"generation", func(s *SessionResult) { s.CorePackage.Generation++ }},
		{"session", func(s *SessionResult) { s.ID = "replacement" }},
		{"package", func(s *SessionResult) { s.CorePackage.PackageID = strings.Repeat("b", 64) }},
		{"stopped", func(s *SessionResult) { s.State = "idle" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior, next := displaySessionFixture(), displaySessionFixture()
			tc.edit(&next)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { json.NewEncoder(w).Encode(displaySessionJSON(next)) }))
			defer server.Close()
			if _, err := NewClient(server.URL, server.Client()).SetSessionDisplayForSession(context.Background(), prior, protocol.SessionDisplayFocused); err == nil {
				t.Fatal("display mutation accepted a replacement machine")
			}
		})
	}
}
