package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// A played ZX81 core entry can be removed from Home without entering its
// workbench, where an unacknowledged firmware card would intercept input.
func TestReleaseHomeRecentRemovesDisplayedCoreEntry(t *testing.T) {
	const targetID, targetTitle = "recent-target-zx81", "A Unique Recent Target"
	const otherID, otherTitle = "keep-other-zx81", "Z Keep Other Title"
	host := newRoomHost(t)
	catalog := []hostclient.Game{
		{ID: targetID, Title: targetTitle, System: "fpga", CoreID: "fes.zx81", PackageID: strings.Repeat("2", 64), State: "available", RootOnline: true, Launchable: true, PlayCount: 1, LastPlayedAt: 123},
		{ID: otherID, Title: otherTitle, System: "fpga", CoreID: "fes.zx81", PackageID: strings.Repeat("2", 64), State: "available", RootOnline: true, Launchable: true},
	}
	var mu sync.Mutex
	var deleted []string
	original, err := url.Parse(host.server.URL)
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
			games := make([]hostclient.Game, 0, len(catalog))
			for _, game := range catalog {
				if r.URL.Query().Get("collection") == "recents" && game.ID != targetID {
					continue
				}
				if platform := r.URL.Query().Get("platform"); platform != "" && game.System != platform {
					continue
				}
				games = append(games, game)
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
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
	host.server = server
	dir := filepath.Join(t.TempDir(), "launcher")
	prefs := filepath.Join(dir, "tenfoot.json")
	if err := saveTenfootPrefs(prefs, tenfootPrefs{SafeAreaPct: DefaultSafeAreaPct, SetupDone: true}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(prefs)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "rooms", installedCoresHomeID+".state.json")
	if err := os.MkdirAll(filepath.Dir(state), 0755); err != nil {
		t.Fatal(err)
	}
	roomBefore, err := json.MarshalIndent(map[string]string{"focus": "family:zx81", "unrelated": "preserve"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	roomBefore = append(roomBefore, '\n')
	if err := os.WriteFile(state, roomBefore, 0644); err != nil {
		t.Fatal(err)
	}
	cores := &fakeLocalCores{cores: homeFixtureCores()}
	app := startHomeLoungeOn(t, host, dir, cores, 1280, 720, false)
	waitFor(t, app, "ZX81 Home with recent", func(s Snapshot) bool {
		return homeFamilySettled(s) && s.Room.Destination.Kind == rooms.KindFamily && s.Room.Destination.Label == "Sinclair ZX81" && roomHit(s, "recent:1").ID != ""
	})
	app.HandleCommand(CommandFromKey("up"), time.Now())
	snap := waitFor(t, app, "exact recent target", func(s Snapshot) bool {
		return s.Room.Destination.Kind == rooms.KindGame && s.Room.Destination.GameID == targetID && s.Room.Destination.Label == targetTitle
	})
	if snap.FirmwareCard.Open {
		t.Fatal("recent navigation opened firmware card")
	}
	waitFor(t, app, "recent focus persisted", func(Snapshot) bool {
		data, err := os.ReadFile(state)
		var room map[string]any
		return err == nil && json.Unmarshal(data, &room) == nil && room["focus"] == "recent:"+targetID
	})
	app.HandleCommand(CommandFromKey("i"), time.Now())
	snap = waitFor(t, app, "target Details", func(s Snapshot) bool { return s.Detail.Open && s.FocusDetail.Title == targetTitle })
	if !snap.Detail.RemoveAvailable || snap.FirmwareCard.Open {
		t.Fatalf("target not removable or firmware card open: %+v", snap.Detail)
	}
	app.HandleCommand(CommandFromKey("x"), time.Now())
	snap = app.Snapshot()
	if !snap.Detail.RemoveConfirm || snap.FocusDetail.Title != targetTitle {
		t.Fatalf("wrong confirmation: %+v", snap.Detail)
	}
	mu.Lock()
	beforeConfirm := len(deleted)
	mu.Unlock()
	if beforeConfirm != 0 {
		t.Fatal("entry removed before confirmation")
	}
	if host.launchCount() != 0 || cores.launchCount() != 0 {
		t.Fatal("navigation launched a title")
	}
	app.HandleCommand(CommandFromKey("return"), time.Now())
	waitFor(t, app, "only target removed", func(Snapshot) bool {
		mu.Lock()
		defer mu.Unlock()
		return len(deleted) == 1 && deleted[0] == targetID && len(catalog) == 1 && catalog[0].ID == otherID
	})
	// Returning the selected family through normal navigation also restores
	// the per-room focus store, which is distinct from tenfoot preferences.
	waitFor(t, app, "refreshed family", func(s Snapshot) bool {
		return homeFamilySettled(s) && s.Room.Destination.Label == "Sinclair ZX81"
	})
	app.HandleCommand(CommandFromKey("right"), time.Now())
	waitFor(t, app, "Spectrum family", func(s Snapshot) bool { return s.Room.Destination.Label == "ZX Spectrum" })
	app.HandleCommand(CommandFromKey("left"), time.Now())
	waitFor(t, app, "restored ZX81 family", func(s Snapshot) bool { return s.Room.Destination.Label == "Sinclair ZX81" })
	waitFor(t, app, "original room store", func(Snapshot) bool {
		data, err := os.ReadFile(state)
		return err == nil && string(data) == string(roomBefore)
	})
	after, err := os.ReadFile(prefs)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("first-use firmware preferences changed")
	}
	if host.launchCount() != 0 || cores.launchCount() != 0 {
		t.Fatal("removal launched a title")
	}
}
