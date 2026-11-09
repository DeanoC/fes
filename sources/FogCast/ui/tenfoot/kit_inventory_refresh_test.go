package tenfoot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// The host may answer a valid empty list before the kit's durable inventory
// is available. Exercise the host HTTP client and the actual Home room, not
// just the room's transient-error retry.
func TestKitHomeRefreshesEmptyInventoryWhenTargetBecomesReady(t *testing.T) {
	t.Setenv("FOGCAST_HOME_ROOM", "")
	useKitStateDir(t, t.TempDir())
	var ready atomic.Bool
	var healthFail atomic.Bool
	var hasCores atomic.Bool
	hasCores.Store(true)
	var inventoryCalls, libraryCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/kit/local-cores":
			inventoryCalls.Add(1)
			cores := []localcores.Core{}
			if ready.Load() && hasCores.Load() {
				cores = []localcores.Core{
					{PackageID: strings.Repeat("a", 64), CoreID: "fes.zx81", Name: "ZX81", Launchable: true},
					{PackageID: strings.Repeat("b", 64), CoreID: "fes.spectrum", Name: "Spectrum", Launchable: true},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"cores": cores})
		case "/api/v1/health":
			if healthFail.Load() {
				http.Error(w, "health unavailable", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "target": map[string]bool{"reachable": ready.Load(), "ready": ready.Load()}})
		case "/api/v1/status":
			http.Error(w, "status unavailable", http.StatusServiceUnavailable)
		case "/api/v1/games":
			libraryCalls.Add(1)
			_, _ = w.Write([]byte(`{"games":[]}`))
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case "/api/v1/library/collections":
			_, _ = w.Write([]byte(`{"collections":[]}`))
		case "/api/v1/library/edition-preferences":
			_, _ = w.Write([]byte(`{"preferences":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	prefs := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(prefs, tenfootPrefs{SafeAreaPct: DefaultSafeAreaPct, SetupDone: true}); err != nil {
		t.Fatal(err)
	}
	opts := Options{GFX: "menu-display", KitHost: true, APIBase: server.URL, Home: "rooms", HomeSet: true, PrefsPath: prefs}.normalized()
	app, err := configuredApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	initial := waitFor(t, app, "empty Home inventory", func(s Snapshot) bool {
		return homeFamilySettled(s) && s.Room.Destination.Label == "Sinclair ZX81" && s.Room.Destination.Availability == rooms.AvailUnavailable && s.Health.Ready && !s.Health.TargetReachable
	})
	if initial.Room.Err != "" {
		t.Fatalf("initial Home error: %s", initial.Room.Err)
	}
	app.HandleCommand(CmdRight, time.Now())
	waitFor(t, app, "moved focus", func(s Snapshot) bool { return s.Room.Destination.Label == "ZX Spectrum" })
	app.mu.Lock()
	originalRoom := app.room
	app.mu.Unlock()

	ready.Store(true)
	app.fetchHealth(t.Context())
	waitFor(t, app, "updated Home inventory", func(s Snapshot) bool {
		app.mu.Lock()
		_, zx := app.localInstalled["fes.zx81"]
		_, spectrum := app.localInstalled["fes.spectrum"]
		app.mu.Unlock()
		return !s.Loading && homeFamilySettled(s) && zx && spectrum && s.Room.Destination.Label == "ZX Spectrum" && s.Room.Destination.Availability == rooms.AvailReady
	})
	app.mu.Lock()
	roomUnchanged := app.room == originalRoom
	_, zxInstalled := app.localInstalled["fes.zx81"]
	_, spectrumInstalled := app.localInstalled["fes.spectrum"]
	app.mu.Unlock()
	if !roomUnchanged {
		t.Fatal("target recovery replaced Home instead of refreshing its existing room")
	}
	if !zxInstalled || !spectrumInstalled {
		t.Fatalf("launcher inventory not refreshed: zx81=%v spectrum=%v", zxInstalled, spectrumInstalled)
	}

	// Once ready, further health observations must not continuously reload
	// inventory or the room's library queries.
	beforeInventory, beforeLibrary := inventoryCalls.Load(), libraryCalls.Load()
	for i := 0; i < 3; i++ {
		app.fetchHealth(t.Context())
		app.Tick(time.Now())
	}
	time.Sleep(100 * time.Millisecond) // bounded observation for asynchronous reloads
	if got := inventoryCalls.Load(); got != beforeInventory {
		t.Fatalf("stable ready health reloaded inventory: %d -> %d", beforeInventory, got)
	}
	if got := libraryCalls.Load(); got != beforeLibrary {
		t.Fatalf("stable ready health reloaded library: %d -> %d", beforeLibrary, got)
	}

	// An actually empty inventory on a ready target also settles, and the
	// health loop must not turn the empty result into an endless poll.
	ready.Store(false)
	app.fetchHealth(t.Context())
	hasCores.Store(false)
	ready.Store(true)
	app.fetchHealth(t.Context())
	waitFor(t, app, "ready target with empty inventory", func(s Snapshot) bool {
		app.mu.Lock()
		empty := app.localInstallKnown && len(app.localInstalled) == 0
		app.mu.Unlock()
		return !s.Loading && homeFamilySettled(s) && empty && s.Room.Destination.Label == "ZX Spectrum" && s.Room.Destination.Availability == rooms.AvailUnavailable
	})
	beforeInventory, beforeLibrary = inventoryCalls.Load(), libraryCalls.Load()
	for i := 0; i < 3; i++ {
		app.fetchHealth(t.Context())
		app.Tick(time.Now())
	}
	time.Sleep(100 * time.Millisecond) // bounded observation for asynchronous reloads
	if got := inventoryCalls.Load(); got != beforeInventory {
		t.Fatalf("stable empty inventory was polled again: %d -> %d", beforeInventory, got)
	}
	if got := libraryCalls.Load(); got != beforeLibrary {
		t.Fatalf("stable empty inventory reloaded library: %d -> %d", beforeLibrary, got)
	}

	// Both endpoints can fail with an HTTP response while the last cached
	// health is still ready. A successful observation after that failure must
	// refresh the inventory even though the old health snapshot was ready.
	healthFail.Store(true)
	app.fetchHealth(t.Context())
	app.mu.Lock()
	failedObservation := app.kitMenuHealthError != ""
	oldHealthReady := app.health.Ready && app.health.TargetReachable && app.health.TargetReady
	app.mu.Unlock()
	if !failedObservation || !oldHealthReady {
		t.Fatalf("failed health observation: error recorded=%v old health ready=%v", failedObservation, oldHealthReady)
	}
	beforeInventory = inventoryCalls.Load()
	hasCores.Store(true)
	healthFail.Store(false)
	app.fetchHealth(t.Context())
	waitFor(t, app, "inventory after health HTTP recovery", func(s Snapshot) bool {
		app.mu.Lock()
		_, zx := app.localInstalled["fes.zx81"]
		_, spectrum := app.localInstalled["fes.spectrum"]
		app.mu.Unlock()
		return !s.Loading && homeFamilySettled(s) && zx && spectrum && s.Room.Destination.Label == "ZX Spectrum" && s.Room.Destination.Availability == rooms.AvailReady
	})
	if got := inventoryCalls.Load(); got != beforeInventory+2 {
		t.Fatalf("health recovery should refresh launcher and Home once: %d -> %d", beforeInventory, got)
	}
	beforeLibrary = libraryCalls.Load()
	for i := 0; i < 3; i++ {
		app.fetchHealth(t.Context())
		app.Tick(time.Now())
	}
	time.Sleep(100 * time.Millisecond) // bounded observation for asynchronous reloads
	if got := inventoryCalls.Load(); got != beforeInventory+2 {
		t.Fatalf("recovered health reloaded inventory again: %d -> %d", beforeInventory+2, got)
	}
	if got := libraryCalls.Load(); got != beforeLibrary {
		t.Fatalf("recovered health reloaded library again: %d -> %d", beforeLibrary, got)
	}
}

// An older request may finish after a later readiness transition, including a
// client that does not promptly honor cancellation. It must not restore the
// earlier inventory over the new one.
type delayedInventoryCores struct {
	fakeLocalCores
	calls    atomic.Int32
	started  chan struct{}
	release  chan struct{}
	returned chan struct{}
}

func (f *delayedInventoryCores) List(context.Context) ([]localcores.Core, error) {
	switch f.calls.Add(1) {
	case 1:
		return []localcores.Core{}, nil
	case 2:
		close(f.started)
		<-f.release
		defer close(f.returned)
		return []localcores.Core{{PackageID: strings.Repeat("a", 64), CoreID: "fes.zx81"}}, nil
	default:
		return []localcores.Core{{PackageID: strings.Repeat("b", 64), CoreID: "fes.spectrum"}}, nil
	}
}

func TestKitInventoryIgnoresSupersededRead(t *testing.T) {
	var ready atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "target": map[string]bool{"reachable": ready.Load(), "ready": ready.Load()}})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.kitHost = true
	cores := &delayedInventoryCores{started: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}
	app.SetKitLocal(cores, &fakePadFeed{})
	t.Cleanup(app.Stop)
	ready.Store(true)
	app.fetchHealth(t.Context())
	select {
	case <-cores.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first refresh did not start")
	}
	ready.Store(false)
	app.fetchHealth(t.Context())
	ready.Store(true)
	app.fetchHealth(t.Context())
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.mu.Lock()
		_, installed := app.localInstalled["fes.spectrum"]
		app.mu.Unlock()
		if installed {
			break
		}
		if time.Now().After(deadline) {
			close(cores.release)
			t.Fatal("second refresh did not publish")
		}
		time.Sleep(time.Millisecond)
	}
	close(cores.release)
	<-cores.returned
	// A subsequent ready observation is a synchronization point for the
	// launcher mutex, and must not trigger another refresh.
	app.fetchHealth(t.Context())
	app.mu.Lock()
	_, stale := app.localInstalled["fes.zx81"]
	_, current := app.localInstalled["fes.spectrum"]
	app.mu.Unlock()
	if stale || !current {
		t.Fatalf("superseded inventory won: old=%v current=%v", stale, current)
	}
}
