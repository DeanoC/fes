package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

func TestFetchHealthClearsOnlyLeaseRefusals(t *testing.T) {
	for _, path := range []string{"status", "launch", "launch failure", "new identical status"} {
		t.Run(path, func(t *testing.T) {
			var free atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/launcher/kit-lease":
					state := "held"
					if free.Load() {
						state = "free"
					}
					_ = json.NewEncoder(w).Encode(KitLeaseStatus{State: state, Owner: "other-shell"})
				case "/api/v1/health":
					_ = json.NewEncoder(w).Encode(hostclient.HealthResult{Ready: true})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			client := NewClient(server.URL, server.Client())
			client.paired = true
			app := NewApp(client, 800, 600, 10)
			t.Cleanup(app.Stop)
			app.fetchHealth(t.Context())
			app.mu.Lock()
			if !app.foreignKitLeaseLocked() {
				t.Fatal("health did not report foreign lease")
			}
			switch path {
			case "status", "new identical status":
				if !app.kitMutationBlockedLocked() {
					t.Fatal("foreign lease did not refuse mutation")
				}
				if path == "new identical status" {
					app.failLocalCoreLocked(localcores.ErrInUse, "")
				}
			case "launch", "launch failure":
				app.startLaunchGameLocked(hostclient.Game{ID: "test-game", Title: "Test", State: "available", RootOnline: true})
				if path == "launch" {
					if app.launch.Message != localInUseCopy {
						t.Fatalf("launch refusal = %q", app.launch.Message)
					}
				} else {
					app.launch.Phase = "error"
					app.launch.Message = "unrelated launch failure"
					app.launchLeaseRefusal = false
				}
			}
			app.mu.Unlock()
			if path == "launch" {
				before := app.Snapshot()
				before.Room.Open = true
				if !launchOverlayVisible(before) {
					t.Fatal("launch refusal did not show its overlay before lease release")
				}
			}
			free.Store(true)
			app.fetchHealth(t.Context())
			snap := app.Snapshot()
			if path == "new identical status" {
				app.mu.Lock()
				status := app.status
				app.mu.Unlock()
				if status != localInUseCopy || snap.Status != localInUseCopy {
					t.Fatalf("new local-core error was cleared: status=%q snapshot=%q", status, snap.Status)
				}
			} else if snap.Status == localInUseCopy {
				t.Fatalf("lease refusal stayed visible after health transition: %q", snap.Status)
			}
			if path == "launch" && (snap.Launch.Message != "" || snap.Launch.Phase != "idle") {
				t.Fatalf("launch refusal remained: %+v", snap.Launch)
			}
			if path == "launch" {
				snap.Room.Open = true
				if launchOverlayVisible(snap) {
					t.Fatalf("launch refusal overlay remained visible: %+v", snap.Launch)
				}
			}
			if path == "launch failure" && (snap.Launch.Message != "unrelated launch failure" || snap.Launch.Phase != "error") {
				t.Fatalf("unrelated launch failure was cleared: %+v", snap.Launch)
			}
		})
	}
}
