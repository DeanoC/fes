package tenfoot

import (
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/zx81keys"
)

func TestCommandFromKeyBrowseNav(t *testing.T) {
	t.Parallel()
	cases := map[string]Command{
		"up":        CmdUp,
		"down":      CmdDown,
		"left":      CmdLeft,
		"right":     CmdRight,
		"return":    CmdSelect,
		"escape":    CmdBack,
		"tab":       CmdTab,
		"shift-tab": CmdTabPrev,
		"/":         CmdSearch,
		"f":         CmdSearch,
		"o":         CmdSettings,
		"g":         CmdFilters,
		"l":         CmdLayoutCycle,
	}
	for name, want := range cases {
		if got := CommandFromKey(name); got != want {
			t.Fatalf("%s = %s want %s", name, got, want)
		}
	}
	if CommandFromKey("tab").String() != "tab" || CommandFromKey("shift-tab").String() != "tab-prev" {
		t.Fatal("tab command names")
	}
}

func TestKeyboardMappingIsNotZX81Matrix(t *testing.T) {
	t.Parallel()
	if CommandFromKey("return") == Command(zx81keys.KeyEnter) {
		t.Fatal("browse Enter must not be the ZX81 enter code")
	}
	if zx81keys.KeyEnter == 0 || zx81keys.Letter('A') == 0 {
		t.Fatal("zx81 matrix codes")
	}
}

func TestUSBKeyboardBrowseDetailSearchAndLaunch(t *testing.T) {
	var launches []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			q := r.URL.Query().Get("q")
			games := []hostclient.Game{
				availableGame("snes-mario", "Mario", "snes"),
				availableGame("megadrive-sonic", "Sonic", "megadrive"),
			}
			if q == "sonic" {
				games = []hostclient.Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			launches = append(launches, string(raw))
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"megadrive-sonic"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 2
	})

	now := time.Now()
	if app.Press(CommandFromKey("right"), now) != CmdRight {
		t.Fatal("arrow right")
	}
	selected, ok := app.Selected()
	if !ok || selected.ID != "megadrive-sonic" {
		t.Fatalf("selected = %#v ok=%v", selected, ok)
	}

	app.Press(CommandFromKey("down"), now)
	if !app.Snapshot().Detail.Open {
		t.Fatal("down on last row should open detail")
	}
	app.Press(CommandFromKey("escape"), now)
	if app.Snapshot().Detail.Open {
		t.Fatal("esc should close detail")
	}

	app.Press(CommandFromKey("tab"), now)
	if !app.SearchOpen() {
		t.Fatal("tab should open search")
	}
	app.TypeText("sonic", now)
	app.Press(CommandFromKey("tab"), now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.SearchOpen && snap.Query == "sonic" && len(snap.Games) == 1 && snap.Games[0].ID == "megadrive-sonic" && !snap.Loading
	})

	app.Press(CommandFromKey("return"), now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok" || snap.Launch.HTTPStatus != 0 || snap.Session.State == "active"
	})
	if len(launches) != 1 || !strings.Contains(launches[0], "megadrive-sonic") {
		t.Fatalf("launches = %#v", launches)
	}
}

func TestUSBKeyboardRepeatDoesNotSurviveZX81Forward(t *testing.T) {
	t.Parallel()
	app := catalogApp(20)
	now := time.Unix(0, 0)
	if app.Press(CmdRight, now) != CmdRight {
		t.Fatal("press right")
	}
	if app.repeat.held != CmdRight {
		t.Fatalf("held = %s", app.repeat.held)
	}
	app.session = hostclient.SessionResult{
		State:        "active",
		CoreKeyboard: true,
		Input:        &hostclient.SessionInput{State: "attached", Ready: true},
	}
	if !app.ForwardsCoreKeyboard() {
		t.Fatal("expected fes.keyboard forward")
	}
	if got := app.Tick(now.Add(repeatDelay + repeatEvery)); got != CmdNone {
		t.Fatalf("zx81 forward leaked sofa repeat %s", got)
	}
	if app.repeat.held != CmdNone {
		t.Fatalf("held after forward = %s", app.repeat.held)
	}
	app.session = hostclient.SessionResult{State: "idle"}
	if got := app.Tick(now.Add(2 * (repeatDelay + repeatEvery))); isHoldable(got) {
		t.Fatalf("ghost walk after zx81 stop: %s", got)
	}
}

func TestUSBKeyboardShiftTabOpensFilters(t *testing.T) {
	t.Parallel()
	app := catalogApp(4)
	now := time.Now()
	app.Press(CommandFromKey("shift-tab"), now)
	if !app.FiltersOpen() {
		t.Fatal("shift-tab should open filters")
	}
	app.Press(CommandFromKey("escape"), now)
	if app.FiltersOpen() {
		t.Fatal("esc should close filters")
	}
}

func TestUSBKeyboardTabCyclesSettingsRows(t *testing.T) {
	t.Parallel()
	app := catalogApp(4)
	app.settingsOpen = true
	app.settingsIndex = 0
	now := time.Now()
	app.Press(CommandFromKey("tab"), now)
	if got := app.Snapshot().Settings.Index; got != 1 {
		t.Fatalf("tab settings index = %d want 1", got)
	}
	app.Press(CommandFromKey("shift-tab"), now)
	if got := app.Snapshot().Settings.Index; got != 0 {
		t.Fatalf("shift-tab settings index = %d want 0", got)
	}
	app.Press(CommandFromKey("escape"), now)
	if app.Snapshot().Settings.Open {
		t.Fatal("esc should close settings")
	}
}
