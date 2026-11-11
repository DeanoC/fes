package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// Exercise release Home with a fake kit-local host, Sinclair setup and Library against
// one catalog. The hardware room opens the selected ZX81 core shelf directly.
// No launch is needed to open or remove Details.
func TestReleaseHomeLibraryRemovesOnlyDisplayedCoreEntry(t *testing.T) {
	const targetID, targetTitle = "remove-target-zx81", "A Unique Remove Target"
	const otherID, otherTitle = "keep-other-zx81", "Z Keep Other Title"
	h := newRoomHost(t)
	catalog := []hostclient.Game{
		{ID: targetID, Title: targetTitle, System: "fpga", CoreID: "fes.zx81", PackageID: strings.Repeat("2", 64), State: "available", RootOnline: true, Launchable: true},
		{ID: otherID, Title: otherTitle, System: "fpga", CoreID: "fes.zx81", PackageID: strings.Repeat("2", 64), State: "available", RootOnline: true, Launchable: true},
	}
	var mu sync.Mutex
	var deleted []string
	original, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(original)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if r.Context().Err() == nil {
			http.Error(w, "fixture host unavailable", http.StatusBadGateway)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			mu.Lock()
			var filtered []hostclient.Game
			if r.URL.Query().Get("collection") != "recents" {
				platform := r.URL.Query().Get("platform")
				for _, game := range catalog {
					if platform == "" || game.System == platform {
						filtered = append(filtered, game)
					}
				}
			}
			mu.Unlock()
			if filtered == nil {
				filtered = []hostclient.Game{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": filtered})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []hostclient.Platform{{ID: "fpga", Label: "FPGA"}, {ID: "zx81", Label: "ZX81"}}})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/library/core-entries/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/library/core-entries/")
			mu.Lock()
			deleted = append(deleted, id)
			for i := range catalog {
				if catalog[i].ID == id {
					catalog = append(catalog[:i], catalog[i+1:]...)
					break
				}
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"game_id": id})
		default:
			proxy.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(server.Close)
	h.server = server
	cores := &fakeLocalCores{cores: homeFixtureCores()}
	// The device owner has already acknowledged Sinclair firmware. Without
	// this, the first-use card arrives asynchronously and can intercept the
	// navigation keys; its behavior has dedicated firmware-card tests.
	dir := filepath.Join(t.TempDir(), "launcher")
	if err := saveTenfootPrefs(filepath.Join(dir, "tenfoot.json"), tenfootPrefs{
		SafeAreaPct: DefaultSafeAreaPct, SetupDone: true, FirmwareInfoSeen: []string{"zx81"},
	}); err != nil {
		t.Fatal(err)
	}
	app := startHomeLoungeOn(t, h, dir, cores, 1280, 720, false)
	waitFor(t, app, "Sinclair Home", func(s Snapshot) bool {
		return homeFamilySettled(s) && s.Room.Destination.Label == "Sinclair ZX81"
	})
	app.HandleCommand(CmdSelect, time.Now())
	s := waitFor(t, app, "Sinclair setup", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == hardwareRoomID && roomHit(s, "library").ID != ""
	})
	s = waitFor(t, app, "setup Library control", func(s Snapshot) bool { return !s.FirmwareCard.Open && roomHit(s, "library").ID != "" })
	// Navigate the real hardware room with its directional inputs. Confirm
	// only once the Library label is visibly focused; never send Select to
	// an unidentified control, which could launch a machine.
	controlFocused := func(s Snapshot, id, label string) bool {
		hit := roomHit(s, id)
		if hit.ID == "" {
			return false
		}
		for _, op := range s.Room.Frame.Ops {
			if op.Kind == rooms.OpText && op.Text == label && op.Bold &&
				op.X >= hit.X && op.X < hit.X+hit.W && op.Y >= hit.Y && op.Y < hit.Y+hit.H {
				return true
			}
		}
		return false
	}
	for _, step := range []struct {
		button    Button
		id, label string
	}{
		{ButtonDPadRight, "fit", "Fit & save setup"},
		{ButtonDPadDown, "setup_library", "Set up ZX81"},
		{ButtonDPadRight, "firmware", "Firmware"},
		{ButtonDPadUp, "library", "Library"},
	} {
		app.HandleCommand(CommandFromButton(step.button), time.Now())
		waitFor(t, app, "focused "+step.label, func(s Snapshot) bool {
			return controlFocused(s, step.id, step.label)
		})
	}
	if h.launchCount() != 0 || cores.launchCount() != 0 {
		t.Fatal("navigating to Library launched a title")
	}
	app.HandleCommand(CommandFromButton(ButtonSouth), time.Now())
	s = waitFor(t, app, "ZX81 FPGA library", func(s Snapshot) bool {
		return !s.Room.Open && !s.Loading && s.PlatformID == "fpga" && len(s.Games) == 2
	})
	if s.Games[s.Grid.Focus].ID != targetID {
		t.Fatalf("focused unexpected title: %+v", s.Games[s.Grid.Focus])
	}
	app.HandleCommand(CommandFromKey("i"), time.Now())
	s = app.Snapshot()
	if !s.Detail.Open || s.FocusDetail.Title != targetTitle || !s.Detail.RemoveAvailable {
		t.Fatalf("wrong Details: %+v %+v", s.Detail, s.FocusDetail)
	}
	app.HandleCommand(CommandFromButton(ButtonWest), time.Now())
	s = app.Snapshot()
	if !s.Detail.RemoveConfirm || s.FocusDetail.Title != targetTitle {
		t.Fatalf("wrong confirmation: %+v %+v", s.Detail, s.FocusDetail)
	}
	if h.launchCount() != 0 || cores.launchCount() != 0 {
		t.Fatal("navigating to removal launched a title")
	}
	app.HandleCommand(CommandFromButton(ButtonSouth), time.Now())
	waitFor(t, app, "target removed", func(s Snapshot) bool {
		mu.Lock()
		defer mu.Unlock()
		return len(deleted) == 1 && len(catalog) == 1 && catalog[0].ID == otherID
	})
	mu.Lock()
	defer mu.Unlock()
	if len(deleted) != 1 || deleted[0] != targetID {
		t.Fatalf("deleted wrong entry: %v", deleted)
	}
	if h.launchCount() != 0 || cores.launchCount() != 0 {
		t.Fatal("removal launched a title")
	}
}
