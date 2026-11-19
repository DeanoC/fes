package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

// coreScreenHost is a room host that also answers the hardware read, as a
// kit host does, with every v1 computer ready and no session.
func coreScreenHost(t *testing.T, games []hostclient.Game) *roomHost {
	t.Helper()
	h := newRoomHost(t)
	h.games = games
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
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/hardware" {
			h.mu.Lock()
			state := h.state
			h.mu.Unlock()
			snap := hostclient.HardwareSnapshot{Session: &hostclient.SessionResult{State: state}}
			for _, g := range games {
				if g.MediaID == "" {
					snap.Computers = append(snap.Computers, hostclient.ShelfComputer{GameID: g.ID, Title: g.Title, CoreID: g.CoreID, Ready: true})
				}
			}
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	h.server = server
	return h
}

// The whole route a person takes: Home's Spectrum tile, the core screen it
// opens with Home's own payload, and Start.
func TestHomeFamilyOpensCoreScreenAndStartsAGame(t *testing.T) {
	games := []hostclient.Game{
		homeMachineEntry("spectrum", "fes.spectrum"),
		homeTitleEntry("lunar", "Lunar Courier", "fes.spectrum"),
	}
	h := coreScreenHost(t, games)
	dir := filepath.Join(t.TempDir(), "launcher")
	if err := saveTenfootPrefs(filepath.Join(dir, "tenfoot.json"), tenfootPrefs{
		SafeAreaPct: DefaultSafeAreaPct, SetupDone: true, FirmwareInfoSeen: []string{"zx81", "spectrum"},
	}); err != nil {
		t.Fatal(err)
	}
	app := startHomeLoungeOn(t, h, dir, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720, false)
	waitFor(t, app, "first family", homeFamilySettled)
	app.HandleCommand(CmdRight, time.Now())
	waitFor(t, app, "Spectrum", func(s Snapshot) bool { return s.Room.Destination.Label == "ZX Spectrum" })
	app.HandleCommand(CmdSelect, time.Now())
	text := func(s Snapshot) string {
		var b strings.Builder
		for _, op := range s.Room.Frame.Ops {
			b.WriteString(op.Text)
			b.WriteByte('\n')
		}
		return b.String()
	}
	s := waitFor(t, app, "core screen with its games", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == coreRoomID && !s.FirmwareCard.Open && roomHit(s, "game:2").ID != "" &&
			strings.Contains(text(s), "Ready\n")
	})
	for _, want := range []string{"ZX Spectrum 48K\n", "Lunar Courier\n", "1 in your library\n", "Start\n"} {
		if !strings.Contains(text(s), want) {
			t.Fatalf("core screen is missing %q:\n%s", want, text(s))
		}
	}
	if h.launchCount() != 0 {
		t.Fatal("opening the core screen launched a game")
	}
	// Focus starts on the first game, so Confirm starts it.
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "launch", func(Snapshot) bool { return h.launchCount() == 1 })
	h.mu.Lock()
	launch := h.launches[0]
	h.mu.Unlock()
	if !strings.Contains(launch, `"lunar"`) {
		t.Fatalf("launched %s, want the first game", launch)
	}
}
