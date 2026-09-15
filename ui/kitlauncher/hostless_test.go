package kitlauncher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

const hostlessDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestHostlessEligibleRefusesPackagesAndROMless(t *testing.T) {
	t.Parallel()
	if err := hostlessEligible(tenfoot.Game{ID: "sonic", System: "megadrive", Launchable: true}); err != nil {
		t.Fatal(err)
	}
	if err := hostlessEligible(tenfoot.Game{ID: "pong", System: "pong", Launchable: true}); err == nil || err.Error() != RefuseNeedsHost {
		t.Fatalf("pong = %v", err)
	}
	if err := hostlessEligible(tenfoot.Game{ID: "pkg", System: "fpga", Launchable: true}); err == nil || err.Error() != RefuseNeedsHost {
		t.Fatalf("package = %v", err)
	}
	if err := hostlessEligible(tenfoot.Game{ID: "locked", System: "megadrive", Launchable: false}); err == nil || err.Error() != RefuseNeedsHost {
		t.Fatalf("not launchable = %v", err)
	}
}

func TestHostlessLaunchVerifiedHitAndFailClosed(t *testing.T) {
	system := protocol.SystemMegaDrive
	content := protocol.ContentIdentity{SHA256: hostlessDigest, Size: 4, Extension: "md"}
	coreName := "MegaDrive"
	gameID := "megadrive-sonic"
	var claimedOwner string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/kit/lease" && r.Method == http.MethodGet:
			st := kitlease.Status{State: "free", Generation: "g1"}
			if claimedOwner != "" {
				st = kitlease.Status{State: "held", Generation: "g2", Owner: claimedOwner, Purpose: kitlease.HostlessPurpose, ExpiresInMS: 60000}
			}
			_ = json.NewEncoder(w).Encode(st)
		case r.URL.Path == "/v1/kit/claim":
			claimedOwner = kitlease.HostlessOwner
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": map[string]any{"state": "held", "generation": "g2", "owner": kitlease.HostlessOwner, "purpose": kitlease.HostlessPurpose, "expires_in_ms": 60000},
				"token":  "lease-secret",
			})
		case r.URL.Path == "/v2/hostless/identity/"+gameID:
			_ = json.NewEncoder(w).Encode(protocol.CachedIdentityResponse{Present: true, GameID: gameID, System: &system, Content: &content})
		case r.URL.Path == "/v2/launch":
			if r.Header.Get(targetclient.KitLeaseHeader) != "lease-secret" {
				t.Error("launch missing lease")
			}
			_ = json.NewEncoder(w).Encode(protocol.CachedLaunchResponse{
				Status: protocol.Status{
					State: protocol.StateActive, GameID: &gameID, System: &system,
					ExpectedCore: &coreName, ObservedCore: &coreName,
				},
				Content: content,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	h := testHostless(t, server)
	game := tenfoot.Game{ID: gameID, System: "megadrive", Launchable: true, Title: "Sonic"}
	got, err := h.launch(context.Background(), game)
	if err != nil || got.Status.State != protocol.StateActive {
		t.Fatalf("launch = %#v err=%v", got, err)
	}

	miss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/lease":
			_ = json.NewEncoder(w).Encode(kitlease.Status{State: "free", Generation: "g1"})
		case "/v2/hostless/identity/" + gameID:
			_ = json.NewEncoder(w).Encode(protocol.CachedIdentityResponse{Present: false})
		default:
			http.NotFound(w, r)
		}
	}))
	defer miss.Close()
	if _, err := testHostless(t, miss).launch(context.Background(), game); err == nil || err.Error() != RefuseNeedsHost {
		t.Fatalf("cache miss = %v", err)
	}

	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(kitlease.Status{State: "held", Generation: "g9", Owner: "caster-hil", Purpose: "expand", ExpiresInMS: 60000})
	}))
	defer busy.Close()
	if _, err := testHostless(t, busy).launch(context.Background(), game); err == nil || err.Error() != RefuseKitInUse {
		t.Fatalf("foreign lease = %v", err)
	}
}

func TestHostlessStopReturnsIdleAndFailedReleaseStaysHeld(t *testing.T) {
	system := protocol.SystemMegaDrive
	content := protocol.ContentIdentity{SHA256: hostlessDigest, Size: 4, Extension: "md"}
	coreName := "MegaDrive"
	gameID := "megadrive-sonic"
	var claimed bool
	releases := 0
	agentState := "idle"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/kit/lease" && r.Method == http.MethodGet:
			st := kitlease.Status{State: "free", Generation: "g1"}
			if claimed {
				st = kitlease.Status{State: "held", Generation: "g2", Owner: kitlease.HostlessOwner, Purpose: kitlease.HostlessPurpose, ExpiresInMS: 60000}
			}
			_ = json.NewEncoder(w).Encode(st)
		case r.URL.Path == "/v1/kit/claim":
			claimed = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": map[string]any{"state": "held", "generation": "g2", "owner": kitlease.HostlessOwner, "purpose": kitlease.HostlessPurpose, "expires_in_ms": 60000},
				"token":  "lease-secret",
			})
		case r.URL.Path == "/v2/hostless/identity/"+gameID:
			_ = json.NewEncoder(w).Encode(protocol.CachedIdentityResponse{Present: true, GameID: gameID, System: &system, Content: &content})
		case r.URL.Path == "/v2/launch":
			agentState = "active"
			_ = json.NewEncoder(w).Encode(protocol.CachedLaunchResponse{
				Status: protocol.Status{
					State: protocol.StateActive, GameID: &gameID, System: &system,
					ExpectedCore: &coreName, ObservedCore: &coreName,
				},
				Content: content,
			})
		case r.URL.Path == "/v1/stop":
			if r.Header.Get(targetclient.KitLeaseHeader) != "lease-secret" {
				t.Error("stop missing lease")
			}
			agentState = "idle"
			_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
		case r.URL.Path == "/v1/status":
			_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.State(agentState)})
		case r.URL.Path == "/v1/kit/release":
			releases++
			http.Error(w, `{"error":{"code":"INTERNAL","message":"release failed"}}`, http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	h := testHostless(t, server)
	game := tenfoot.Game{ID: gameID, System: "megadrive", Launchable: true, Title: "Sonic"}
	if _, err := h.launch(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	session, err := h.stopAndRelease(context.Background())
	if err == nil {
		t.Fatal("expected release failure")
	}
	if session.State != "idle" {
		t.Fatalf("session after stop = %#v", session)
	}
	if !h.held() {
		t.Fatal("failed release dropped hostless grant")
	}
	if releases != 1 {
		t.Fatalf("releases=%d", releases)
	}
}

func testHostless(t *testing.T, server *httptest.Server) *hostlessRuntime {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(u, "token", server.Client(), kitlease.HostlessOwner, kitlease.HostlessPurpose)
	agent := targetclient.NewClient(u, "token", server.Client()).WithKitLease(lease)
	return &hostlessRuntime{agent: agent, lease: lease}
}
