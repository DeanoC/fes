package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFilterOverlaySelectionRefreshAndClearKeepsBrowseState(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/facets":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"genres": []string{"Action", "RPG"},
				"years":  []string{"1991", "1992"},
			})
		case "/api/v1/games":
			mu.Lock()
			queries = append(queries, r.URL.RawQuery)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []Platform{{ID: "snes", Label: "Super NES"}}})
		case "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && len(snap.Platforms) == 1
	})
	app.Press(CmdFilterNext, time.Now())
	app.Press(CmdViewNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.PlatformID == "snes" && snap.Collection == "continue"
	})

	app.Press(CmdFilters, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Filters.Open && !snap.Filters.Loading && len(snap.Filters.Rows) == filterRootRowCount
	})
	if app.Snapshot().Launch.Phase != "idle" {
		t.Fatalf("opening filters launched: %#v", app.Snapshot().Launch)
	}

	app.Press(CmdSelect, time.Now()) // Genre
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.Filters.Open && len(snap.Filters.Rows) >= 2 && snap.Filters.Rows[1].Label == "Action"
	})
	app.Press(CmdDown, time.Now())
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Genre == "Action" && snap.Filters.Open && snap.Collection == "continue" && snap.PlatformID == "snes"
	})

	app.Press(CmdDown, time.Now()) // Year
	app.Press(CmdSelect, time.Now())
	app.Press(CmdDown, time.Now())
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Year == "1991" && snap.Genre == "Action"
	})

	app.Press(CmdDown, time.Now()) // Region
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.Filters.Open && len(snap.Filters.Rows) > 2 && snap.Filters.Rows[1].Label == "USA"
	})
	app.Press(CmdDown, time.Now())
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Region == "usa" && strings.Contains(snap.Status, "USA")
	})

	app.Press(CmdDown, time.Now()) // Hide prerelease
	app.Press(CmdSelect, time.Now())
	app.Press(CmdDown, time.Now()) // Hide hacks
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.HidePrerelease && snap.HideHacks && snap.Collection == "continue" && snap.PlatformID == "snes"
	})

	found := false
	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	for _, query := range got {
		if strings.Contains(query, "genre=Action") && strings.Contains(query, "year=1991") && strings.Contains(query, "region=usa") && strings.Contains(query, "hide_prerelease=1") && strings.Contains(query, "hide_hacks=1") && strings.Contains(query, "collection=continue") && strings.Contains(query, "platform=snes") && strings.Contains(query, "grouped=1") && strings.Contains(query, "availability=ready") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("filtered query missing: %#v", got)
	}

	app.Press(CmdDown, time.Now()) // Clear
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Genre == "" && snap.Year == "" && snap.Region == "" && !snap.HidePrerelease && !snap.HideHacks && snap.Collection == "continue" && snap.PlatformID == "snes" && snap.Query == ""
	})
	mu.Lock()
	got = append([]string(nil), queries...)
	mu.Unlock()
	cleared := false
	for i := len(got) - 1; i >= 0; i-- {
		query := got[i]
		if strings.Contains(query, "collection=continue") && strings.Contains(query, "platform=snes") && !strings.Contains(query, "genre=") && !strings.Contains(query, "year=") && !strings.Contains(query, "region=") && !strings.Contains(query, "hide_prerelease") && !strings.Contains(query, "hide_hacks") {
			cleared = true
			break
		}
	}
	if !cleared {
		t.Fatalf("clear did not keep browse state: %#v", got)
	}

	app.Press(CmdBack, time.Now())
	if app.FiltersOpen() {
		t.Fatal("east should close filters")
	}
	if app.Snapshot().Launch.Phase != "idle" {
		t.Fatalf("confirm launched: %#v", app.Snapshot().Launch)
	}
}

func TestEmptyFacetsStillAllowClearAndAny(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/facets":
			_, _ = w.Write([]byte(`{"genres":null,"years":null}`))
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1
	})
	app.Press(CmdFilters, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Filters.Open && !snap.Filters.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.Filters.Open && len(snap.Filters.Rows) == 1 && snap.Filters.Rows[0].Label == "Any" && strings.Contains(snap.Filters.Hint, "no genres")
	})
	app.Press(CmdBack, time.Now())
	app.Press(CmdDown, time.Now())
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.Filters.Open && len(snap.Filters.Rows) == 1 && snap.Filters.Hint == "no years"
	})
	app.Press(CmdBack, time.Now())
	app.mu.Lock()
	app.filterGenre = "Action"
	app.mu.Unlock()
	app.Press(CmdDown, time.Now()) // Region
	app.Press(CmdDown, time.Now())
	app.Press(CmdDown, time.Now())
	app.Press(CmdDown, time.Now()) // Clear
	app.Press(CmdSelect, time.Now())
	if app.Snapshot().Genre != "" {
		t.Fatalf("clear left genre %q", app.Snapshot().Genre)
	}
}

func TestFilterBindingDoesNotCollideWithCoreCommands(t *testing.T) {
	t.Parallel()
	if CommandFromButton(ButtonSouth) != CmdSelect || CommandFromButton(ButtonEast) != CmdBack {
		t.Fatal("face launch/back")
	}
	if CommandFromButton(ButtonStart) != CmdQuit || CommandFromButton(ButtonBack) != CmdLayoutCycle {
		t.Fatal("start/select")
	}
	if CommandFromButton(ButtonNorth) != CmdSearch || CommandFromButton(ButtonWest) != CmdSortCycle {
		t.Fatal("north/west")
	}
	if CommandFromButton(ButtonLeftShoulder) != CmdFilterPrev || CommandFromButton(ButtonRightShoulder) != CmdFilterNext {
		t.Fatal("shoulders")
	}
	if CommandFromKey("o") != CmdSettings || CommandFromKey("l") != CmdLayoutCycle || CommandFromKey("/") != CmdSearch {
		t.Fatal("settings/layout/search keys")
	}
	if CommandFromKey("x") != CmdSortCycle || CommandFromKey("g") != CmdFilters {
		t.Fatal("sort/filter keys")
	}
	if longPressCommand(CmdSelect) != CmdViewPicker || longPressCommand(CmdSearch) != CmdFavorite {
		t.Fatal("existing holds")
	}
	if longPressCommand(CmdSortCycle) != CmdFilters {
		t.Fatal("hold west should open filters")
	}
	if longPressCommand(CmdFilters) != CmdNone || longPressCommand(CmdLayoutCycle) != CmdNone || longPressCommand(CmdBack) != CmdNone {
		t.Fatal("filters must not steal other holds")
	}
}

func TestHoldWestOpensFiltersWithoutSorting(t *testing.T) {
	app := catalogApp(5)
	app.sort = "title"
	held := map[Command]bool{}
	now := time.Unix(0, 0)
	if applyPressed(app, map[Command]bool{CmdSortCycle: true}, held, now) {
		t.Fatal("quit")
	}
	if app.Snapshot().Filters.Open {
		t.Fatal("filters opened on down")
	}
	if app.Snapshot().Sort != "title" {
		t.Fatal("sorted on down")
	}
	now = now.Add(40 * time.Millisecond)
	if applyPressed(app, map[Command]bool{}, held, now) {
		t.Fatal("quit")
	}
	if app.Snapshot().Sort != "recently_added" {
		t.Fatalf("short west should sort, sort=%s", app.Snapshot().Sort)
	}
	if app.Snapshot().Filters.Open {
		t.Fatal("short west opened filters")
	}

	app.sort = "title"
	held = map[Command]bool{}
	now = time.Unix(0, 0)
	if applyPressed(app, map[Command]bool{CmdSortCycle: true}, held, now) {
		t.Fatal("quit")
	}
	if got := app.Tick(now.Add(longPressMin + time.Millisecond)); got != CmdFilters {
		t.Fatalf("long = %s", got)
	}
	if !app.Snapshot().Filters.Open {
		t.Fatal("long west should open filters")
	}
	if app.Snapshot().Sort != "title" {
		t.Fatalf("long west sorted: %s", app.Snapshot().Sort)
	}
}

func TestHoldWestDoesNotOpenFiltersWhileViewPickerOpen(t *testing.T) {
	app := catalogApp(5)
	app.Press(CmdViewPicker, time.Now())
	if !app.ViewPickerOpen() {
		t.Fatal("picker")
	}
	held := map[Command]bool{}
	now := time.Unix(0, 0)
	if applyPressed(app, map[Command]bool{CmdSortCycle: true}, held, now) {
		t.Fatal("quit")
	}
	if app.Snapshot().Filters.Open {
		t.Fatal("filters opened from picker")
	}
	if got := app.Tick(now.Add(longPressMin + time.Millisecond)); got != CmdNone {
		t.Fatalf("long while picker = %s", got)
	}
	if !app.ViewPickerOpen() || app.Snapshot().Filters.Open {
		t.Fatal("picker should keep membership binding")
	}
}

func TestShouldersStayPlatformsWhileFiltersClosed(t *testing.T) {
	app := catalogApp(3)
	app.platforms = []Platform{{ID: "snes", Label: "Super NES"}, {ID: "nes", Label: "NES"}}
	app.Press(CmdFilterNext, time.Now())
	if app.Snapshot().PlatformID != "snes" {
		t.Fatalf("platform = %q", app.Snapshot().PlatformID)
	}
	app.Press(CmdFilters, time.Now())
	if !app.FiltersOpen() {
		t.Fatal("filters")
	}
	app.Press(CmdFilterNext, time.Now())
	if app.Snapshot().PlatformID != "snes" {
		t.Fatalf("shoulders remapped while overlay open: %q", app.Snapshot().PlatformID)
	}
	app.Press(CmdBack, time.Now())
	app.Press(CmdFilterNext, time.Now())
	if app.Snapshot().PlatformID != "nes" {
		t.Fatalf("shoulders after close = %q", app.Snapshot().PlatformID)
	}
}

func TestDumpRegionTokensMatchWebVocabulary(t *testing.T) {
	t.Parallel()
	want := []string{
		"usa", "japan", "europe", "world", "brazil",
		"korea", "asia", "australia",
		"france", "germany", "spain", "italy", "canada",
		"other",
	}
	if strings.Join(dumpRegionTokens, ",") != strings.Join(want, ",") {
		t.Fatalf("tokens = %#v", dumpRegionTokens)
	}
	if dumpRegionLabel("usa") != "USA" || dumpRegionLabel("japan") != "Japan" || dumpRegionLabel("other") != "Other" {
		t.Fatal("labels")
	}
}
