package tenfoot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func pointerCatalog(n int) *App {
	app := NewApp(nil, 1280, 720, n)
	app.games = make([]Game, n)
	for i := 0; i < n; i++ {
		app.games[i] = availableGame("g"+strconv.Itoa(i), "Game "+strconv.Itoa(i), "snes")
	}
	app.grid.SetCount(n)
	return app
}

func cellCenter(t *testing.T, app *App, i int) (int, int) {
	t.Helper()
	x, y, w, h, ok := app.Snapshot().Grid.CellRect(i)
	if !ok {
		t.Fatalf("cell %d offscreen focus=%d cols=%d mode=%s", i, app.Snapshot().Grid.Focus, app.Snapshot().Grid.Columns, app.Snapshot().Grid.Mode)
	}
	return x + w/2, y + h/2
}

func TestPointerHoverMovesFocusOnGridShelfAndList(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, mode := range []LayoutKind{LayoutGrid, LayoutShelf, LayoutList} {
		app := pointerCatalog(12)
		app.SetLayout(mode)
		if app.Snapshot().Grid.Focus != 0 {
			t.Fatalf("%s start focus = %d", mode, app.Snapshot().Grid.Focus)
		}
		x, y := cellCenter(t, app, 2)
		app.PointerMove(x, y, now)
		if got := app.Snapshot().Grid.Focus; got != 2 {
			t.Fatalf("%s hover focus = %d want 2", mode, got)
		}
	}
}

func TestPointerClickLaunchesWithoutController(t *testing.T) {
	var launches []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{
				availableGame("snes-mario", "Mario", "snes"),
				availableGame("megadrive-sonic", "Sonic", "megadrive"),
			}})
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
	x, y := cellCenter(t, app, 1)
	app.PointerMove(x, y, now)
	selected, ok := app.Selected()
	if !ok || selected.ID != "megadrive-sonic" {
		t.Fatalf("hover selected = %#v ok=%v", selected, ok)
	}
	app.PointerClick(x, y, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok" || snap.Launch.HTTPStatus != 0 || snap.Session.State == "active"
	})
	if len(launches) != 1 || !strings.Contains(launches[0], "megadrive-sonic") {
		t.Fatalf("launches = %#v", launches)
	}
}

func TestPointerClickEmptyDoesNotLaunch(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(4)
	now := time.Now()
	app.PointerClick(4, 4, now)
	if app.Snapshot().Grid.Focus != 0 {
		t.Fatalf("focus = %d", app.Snapshot().Grid.Focus)
	}
	if phase := app.Snapshot().Launch.Phase; phase != "idle" {
		t.Fatalf("empty click launched: %s", phase)
	}
}

func TestPointerAndKeyboardStillWorkTogether(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(8)
	now := time.Now()
	x, y := cellCenter(t, app, 2)
	app.PointerMove(x, y, now)
	if app.Press(CommandFromKey("right"), now) != CmdRight {
		t.Fatal("keyboard right after mouse")
	}
	if got := app.Snapshot().Grid.Focus; got != 3 {
		t.Fatalf("keyboard after mouse focus = %d want 3", got)
	}
	x, y = cellCenter(t, app, 1)
	app.PointerMove(x, y, now)
	if got := app.Snapshot().Grid.Focus; got != 1 {
		t.Fatalf("mouse after keyboard focus = %d want 1", got)
	}
}

func TestPointerClickDetailLaunches(t *testing.T) {
	var launches []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{
				availableGame("snes-mario", "Mario", "snes"),
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			launches = append(launches, string(raw))
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1
	})
	now := time.Now()
	app.Press(CommandFromKey("down"), now)
	if !app.Snapshot().Detail.Open {
		t.Fatal("down should open detail")
	}
	geom, ok := detailGeomOf(app.Snapshot())
	if !ok {
		t.Fatal("detail geom")
	}
	app.PointerClick(geom.Pane.X+24, geom.Pane.Y+24, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok" || snap.Launch.HTTPStatus != 0 || snap.Session.State == "active"
	})
	if len(launches) != 1 || !strings.Contains(launches[0], "snes-mario") {
		t.Fatalf("launches = %#v", launches)
	}
}

func TestPointerSearchHoverClickConfirms(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			q := r.URL.Query().Get("q")
			queries = append(queries, q)
			games := []Game{
				availableGame("snes-mario", "Mario", "snes"),
				availableGame("megadrive-sonic", "Sonic", "megadrive"),
			}
			if q == "s" {
				games = []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 2
	})
	now := time.Now()
	app.Press(CommandFromKey("tab"), now)
	if !app.SearchOpen() {
		t.Fatal("tab should open search")
	}
	_, keys, ok := oskLayout(app.Snapshot())
	if !ok {
		t.Fatal("osk layout")
	}
	var sKey, doneKey oskKeyRect
	for _, key := range keys {
		switch key.ID {
		case "char-s":
			sKey = key
		case "done":
			doneKey = key
		}
	}
	if sKey.ID == "" || doneKey.ID == "" {
		t.Fatalf("osk keys s=%q done=%q", sKey.ID, doneKey.ID)
	}
	app.PointerMove(sKey.X+sKey.W/2, sKey.Y+sKey.H/2, now)
	if app.Snapshot().OSK.FocusID != "char-s" {
		t.Fatalf("hover focus = %q", app.Snapshot().OSK.FocusID)
	}
	app.PointerClick(sKey.X+sKey.W/2, sKey.Y+sKey.H/2, now)
	if app.Snapshot().OSK.Buffer != "s" {
		t.Fatalf("typed = %q", app.Snapshot().OSK.Buffer)
	}
	app.PointerClick(doneKey.X+doneKey.W/2, doneKey.Y+doneKey.H/2, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.SearchOpen && snap.Query == "s" && !snap.Loading
	})
	found := false
	for _, q := range queries {
		if q == "s" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("search queries = %#v", queries)
	}
}

func TestPointerOSKPageClickReturnsToLetters(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(4)
	now := time.Now()
	app.Press(CommandFromKey("tab"), now)
	if !app.SearchOpen() {
		t.Fatal("tab should open search")
	}
	clickOSKKey(t, app, "page", now)
	if app.Snapshot().OSK.Page != oskPageSymbols {
		t.Fatalf("123 click page = %d", app.Snapshot().OSK.Page)
	}
	_, keys, ok := oskLayout(app.Snapshot())
	if !ok {
		t.Fatal("symbols osk")
	}
	var doneKey oskKeyRect
	for _, key := range keys {
		if key.ID == "done" {
			doneKey = key
			break
		}
	}
	if doneKey.ID == "" {
		t.Fatal("symbols done")
	}
	app.PointerMove(doneKey.X+doneKey.W/2, doneKey.Y+doneKey.H/2, now)
	if app.Snapshot().OSK.Page != oskPageSymbols {
		t.Fatalf("hover done jumped to page %d", app.Snapshot().OSK.Page)
	}
	clickOSKKey(t, app, "page", now)
	if app.Snapshot().OSK.Page != oskPageLetters {
		t.Fatalf("ABC click page = %d", app.Snapshot().OSK.Page)
	}
}

func clickOSKKey(t *testing.T, app *App, id string, now time.Time) {
	t.Helper()
	_, keys, ok := oskLayout(app.Snapshot())
	if !ok {
		t.Fatal("osk layout")
	}
	for _, key := range keys {
		if key.ID == id {
			app.PointerClick(key.X+key.W/2, key.Y+key.H/2, now)
			return
		}
	}
	t.Fatalf("osk key %q not found", id)
}

func TestPointerDoesNotWalkGridDuringPlaySession(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(6)
	now := time.Now()
	app.session = SessionResult{
		State:        "active",
		CoreKeyboard: true,
		Input:        &SessionInput{State: "attached", Ready: true},
	}
	if !app.ForwardsCoreKeyboard() {
		t.Fatal("expected fes.keyboard forward")
	}
	x, y := cellCenter(t, app, 2)
	app.PointerMove(x, y, now)
	app.PointerClick(x, y, now)
	if got := app.Snapshot().Grid.Focus; got != 0 {
		t.Fatalf("play-session mouse stole focus %d", got)
	}
	if phase := app.Snapshot().Launch.Phase; phase != "idle" {
		t.Fatalf("play-session mouse launched: %s", phase)
	}
}

func TestPointerMoveDismissesAttract(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(4)
	app.attractActive = true
	now := time.Now()
	app.PointerMove(400, 300, now)
	if app.AttractActive() {
		t.Fatal("mouse move should dismiss attract")
	}
}

func TestPointerClickFilterRowActivates(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(4)
	now := time.Now()
	app.Press(CommandFromKey("shift-tab"), now)
	if !app.FiltersOpen() {
		t.Fatal("shift-tab should open filters")
	}
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.Filters.Open && len(snap.Filters.Rows) > 0
	})
	panel, ok := filtersPanel(app.Snapshot())
	if !ok {
		t.Fatal("filters panel")
	}
	row := 3 // hide-prerelease
	app.PointerClick(panel.X+40, panel.rowY(row)+panel.RowH/2, now)
	if !app.Snapshot().HidePrerelease {
		t.Fatal("click should toggle hide prerelease")
	}
}

func TestHitTestCatalogMatchesCellRect(t *testing.T) {
	t.Parallel()
	app := pointerCatalog(10)
	snap := app.Snapshot()
	x, y, w, h, ok := snap.Grid.CellRect(1)
	if !ok {
		t.Fatal("cell 1")
	}
	hit := HitTest(snap, x+w/2, y+h/2)
	if hit.Kind != PointerCatalog || hit.Index != 1 {
		t.Fatalf("hit = %#v", hit)
	}
	if HitTest(snap, 2, 2).Kind != PointerNone {
		t.Fatalf("empty = %s", HitTest(snap, 2, 2).Kind)
	}
}
