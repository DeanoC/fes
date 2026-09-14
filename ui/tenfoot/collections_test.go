package tenfoot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppCyclesLibraryViewsAgainstHostAPI(t *testing.T) {
	var mu sync.Mutex
	var collections int
	var gameQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/library/collections":
			mu.Lock()
			collections++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"collections": []Collection{{ID: "weekend-queue", Name: "Weekend queue"}},
			})
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			gameQueries = append(gameQueries, r.URL.RawQuery)
			mu.Unlock()
			collection := r.URL.Query().Get("collection")
			games := []Game{availableGame("snes-mario", "Mario", "snes"), availableGame("megadrive-sonic", "Sonic", "megadrive")}
			switch collection {
			case "continue":
				games = []Game{availableGame("snes-mario", "Mario", "snes")}
			case "favorites", "recents", "unplayed":
				games = nil
			case "recently_added":
				games = []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			case "weekend-queue":
				games = []Game{availableGame("snes-mario", "Mario", "snes")}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []Platform{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 2 && snap.ViewLabel == "All" && len(snap.Views) == 7
	})

	want := []struct {
		id    string
		label string
		n     int
	}{
		{id: "continue", label: "Continue", n: 1},
		{id: "favorites", label: "Favorites", n: 0},
		{id: "recents", label: "Recent", n: 0},
		{id: "unplayed", label: "Unplayed", n: 0},
		{id: "recently_added", label: "Recently added", n: 1},
		{id: "weekend-queue", label: "Weekend queue", n: 1},
		{id: "", label: "All", n: 2},
	}
	for _, step := range want {
		app.Press(CmdViewNext, time.Now())
		waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
			return !snap.Loading && snap.Collection == step.id && snap.ViewLabel == step.label && len(snap.Games) == step.n && strings.Contains(snap.Status, step.label) && strings.Contains(snap.ChromeLine(), step.label)
		})
	}

	found := false
	mu.Lock()
	queries := append([]string(nil), gameQueries...)
	gotCollections := collections
	mu.Unlock()
	for _, query := range queries {
		if strings.Contains(query, "collection=weekend-queue") && strings.Contains(query, "grouped=1") && strings.Contains(query, "availability=ready") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queries = %#v", queries)
	}
	if gotCollections < 1 {
		t.Fatal("collections were not fetched")
	}
	for _, query := range queries {
		if strings.Contains(query, "collection=recents") && strings.Contains(query, "sort=title") {
			t.Fatalf("recents must not send sort=title: %s", query)
		}
	}
}

func TestAppViewPickerSelectsCollection(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/games" {
			mu.Lock()
			queries = append(queries, r.URL.RawQuery)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
			return
		}
		if r.URL.Path == "/api/v1/library/collections" {
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1
	})
	app.Press(CmdViewPicker, time.Now())
	if !app.ViewPickerOpen() {
		t.Fatal("picker should open")
	}
	app.Press(CmdDown, time.Now())
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && !snap.ViewPicker && snap.Collection == "continue" && snap.ViewLabel == "Continue"
	})
	found := false
	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	for _, query := range got {
		if strings.Contains(query, "collection=continue") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queries = %#v", got)
	}
	app.Press(CmdViewPicker, time.Now())
	app.Press(CmdBack, time.Now())
	if app.ViewPickerOpen() || app.Snapshot().Collection != "continue" {
		t.Fatalf("cancel changed view: picker=%v collection=%q", app.ViewPickerOpen(), app.Snapshot().Collection)
	}
}

func TestAppFavoriteToggleAndErrorRevert(t *testing.T) {
	var mu sync.Mutex
	fail := false
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			game := availableGame("snes-mario", "Mario", "snes")
			game.Favorite = false
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/library/favorites/"):
			mu.Lock()
			methods = append(methods, r.Method)
			shouldFail := fail
			mu.Unlock()
			if shouldFail {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":{"code":"INTERNAL","message":"nope"}}`)
				return
			}
			favorite := r.Method == http.MethodPut
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "favorite": favorite})
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
	app.Press(CmdFavorite, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		n := len(methods)
		mu.Unlock()
		return n >= 1 && len(snap.Games) == 1 && snap.Games[0].Favorite && snap.FocusDetail.Favorite && !strings.Contains(snap.Status, "favorite failed")
	})
	mu.Lock()
	fail = true
	mu.Unlock()
	app.Press(CmdFavorite, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		n := len(methods)
		mu.Unlock()
		return n >= 2 && len(snap.Games) == 1 && snap.Games[0].Favorite && strings.Contains(snap.Status, "favorite failed")
	})
	mu.Lock()
	got := append([]string(nil), methods...)
	mu.Unlock()
	if strings.Join(got, ",") != "PUT,DELETE" {
		t.Fatalf("methods = %#v", got)
	}
}

func TestAppUnfavoriteReloadsFavoritesView(t *testing.T) {
	var mu sync.Mutex
	favorited := true
	var gameQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			gameQueries = append(gameQueries, r.URL.RawQuery)
			starred := favorited
			mu.Unlock()
			var games []Game
			if r.URL.Query().Get("collection") != "favorites" || starred {
				game := availableGame("snes-mario", "Mario", "snes")
				game.Favorite = starred
				games = []Game{game}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/library/favorites/"):
			mu.Lock()
			favorited = false
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "favorite": false})
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
	app.Press(CmdViewNext, time.Now())
	app.Press(CmdViewNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "favorites" && len(snap.Games) == 1 && snap.Games[0].Favorite
	})
	app.Press(CmdFavorite, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "favorites" && len(snap.Games) == 0 && strings.Contains(snap.Status, "Favorites")
	})
	found := false
	mu.Lock()
	got := append([]string(nil), gameQueries...)
	mu.Unlock()
	for _, query := range got {
		if strings.Contains(query, "collection=favorites") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queries = %#v", got)
	}
}

func TestAppFavoriteAddReloadsFavoritesView(t *testing.T) {
	var mu sync.Mutex
	favorited := false
	var gameQueries []string
	putStarted := make(chan struct{})
	releasePut := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releasePut:
		default:
			close(releasePut)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			gameQueries = append(gameQueries, r.URL.RawQuery)
			starred := favorited
			mu.Unlock()
			collection := r.URL.Query().Get("collection")
			var games []Game
			switch collection {
			case "favorites":
				if starred {
					game := availableGame("snes-mario", "Mario", "snes")
					game.Favorite = true
					games = []Game{game}
				}
			case "continue", "recents", "unplayed", "recently_added":
			default:
				game := availableGame("snes-mario", "Mario", "snes")
				game.Favorite = starred
				games = []Game{game}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/library/favorites/"):
			select {
			case <-putStarted:
			default:
				close(putStarted)
			}
			select {
			case <-releasePut:
			case <-r.Context().Done():
				return
			}
			mu.Lock()
			favorited = true
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "favorite": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && snap.Collection == "" && !snap.Games[0].Favorite
	})
	app.Press(CmdFavorite, time.Now())
	select {
	case <-putStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("favorite PUT did not start")
	}
	app.Press(CmdViewNext, time.Now())
	app.Press(CmdViewNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "favorites" && len(snap.Games) == 0
	})
	close(releasePut)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "favorites" && len(snap.Games) == 1 && snap.Games[0].ID == "snes-mario" && snap.Games[0].Favorite
	})
	favoritesGets := 0
	mu.Lock()
	got := append([]string(nil), gameQueries...)
	mu.Unlock()
	for _, query := range got {
		if strings.Contains(query, "collection=favorites") {
			favoritesGets++
		}
	}
	if favoritesGets < 2 {
		t.Fatalf("expected Favorites reload after add, queries = %#v", got)
	}
}

func TestAppKeepsFiltersInsideCollection(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"platforms": []Platform{{ID: "snes", Label: "Super NES"}},
			})
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			queries = append(queries, r.URL.RawQuery)
			mu.Unlock()
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
		return !snap.Loading && len(snap.Platforms) == 1 && len(snap.Games) == 1
	})
	app.Press(CmdViewNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "continue"
	})
	app.Press(CmdFilterNext, time.Now())
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "continue" && snap.PlatformID == "snes" && snap.Sort == "recently_added"
	})
	found := false
	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	for _, query := range got {
		if strings.Contains(query, "collection=continue") && strings.Contains(query, "platform=snes") && strings.Contains(query, "sort=recently_added") && strings.Contains(query, "grouped=1") && strings.Contains(query, "availability=ready") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queries = %#v", got)
	}
}

func TestCatalogQuerySortForRecents(t *testing.T) {
	t.Parallel()
	if got := catalogQuerySort("recents", ""); got != "" {
		t.Fatalf("last played = %q", got)
	}
	if got := catalogQuerySort("recents", "title"); got != "title" {
		t.Fatalf("explicit title = %q", got)
	}
	if got := catalogQuerySort("recents", "recently_added"); got != "" {
		t.Fatalf("recently_added = %q", got)
	}
	if got := catalogQuerySort("recents", "platform"); got != "platform" {
		t.Fatalf("platform = %q", got)
	}
	if got := catalogQuerySort("favorites", "title"); got != "title" {
		t.Fatalf("favorites title = %q", got)
	}
	if got := catalogQuerySort("recently_added", "title"); got != "recently_added" {
		t.Fatalf("recently_added title = %q", got)
	}
	if got := catalogQuerySort("recently_added", ""); got != "recently_added" {
		t.Fatalf("recently_added empty = %q", got)
	}
	if got := catalogQuerySort("recently_added", "platform"); got != "platform" {
		t.Fatalf("recently_added platform = %q", got)
	}
}

func TestAppRecentsDefaultsToLastPlayedOrder(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			queries = append(queries, r.URL.RawQuery)
			mu.Unlock()
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
		return !snap.Loading && len(snap.Games) == 1 && snap.Collection == ""
	})
	app.Press(CmdViewNext, time.Now())
	app.Press(CmdViewNext, time.Now())
	app.Press(CmdViewNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "recents" && snap.Sort == "" && strings.Contains(snap.ChromeLine(), "Last played")
	})
	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	found := false
	for _, query := range got {
		if !strings.Contains(query, "collection=recents") {
			continue
		}
		found = true
		if strings.Contains(query, "sort=") {
			t.Fatalf("default recents sent sort: %s", query)
		}
	}
	if !found {
		t.Fatalf("no recents query in %#v", got)
	}

	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "recents" && snap.Sort == "title"
	})
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "recents" && snap.Sort == "platform"
	})
	mu.Lock()
	got = append([]string(nil), queries...)
	mu.Unlock()
	var sawTitle, sawPlatform bool
	for _, query := range got {
		if !strings.Contains(query, "collection=recents") {
			continue
		}
		if strings.Contains(query, "sort=title") {
			sawTitle = true
		}
		if strings.Contains(query, "sort=platform") {
			sawPlatform = true
		}
	}
	if !sawTitle || !sawPlatform {
		t.Fatalf("explicit recents sorts missing title=%v platform=%v queries=%#v", sawTitle, sawPlatform, got)
	}
}

func TestAppRecentlyAddedDefaultsToAddedSort(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			queries = append(queries, r.URL.RawQuery)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && snap.Sort == "title"
	})
	for i := 0; i < 8 && app.Snapshot().Collection != "recently_added"; i++ {
		app.Press(CmdViewNext, time.Now())
	}
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "recently_added" && snap.Sort == "recently_added" && strings.Contains(snap.ChromeLine(), "Recently added")
	})
	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	found := false
	for _, query := range got {
		if !strings.Contains(query, "collection=recently_added") {
			continue
		}
		found = true
		if !strings.Contains(query, "sort=recently_added") {
			t.Fatalf("recently_added default query = %s", query)
		}
	}
	if !found {
		t.Fatalf("no recently_added query in %#v", got)
	}
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "recently_added" && snap.Sort == "platform"
	})
}

func TestChromeLineIncludesActiveView(t *testing.T) {
	t.Parallel()
	snap := Snapshot{
		ViewLabel:  "Favorites",
		PlatformID: "snes",
		Platforms:  []Platform{{ID: "snes", Label: "Super NES"}},
		Sort:       "title",
		Status:     "1 titles · Favorites · Super NES · Title",
	}
	got := snap.ChromeLine()
	if !strings.Contains(got, "Favorites") || !strings.Contains(got, "Super NES") || !strings.Contains(got, "Title") {
		t.Fatalf("chrome = %q", got)
	}
	if !strings.HasPrefix(got, "Favorites") {
		t.Fatalf("view should lead chrome: %q", got)
	}
	recent := Snapshot{ViewLabel: "Recent", Collection: "recents", Sort: "", Status: "0 titles · Recent · All · Last played"}
	if chrome := recent.ChromeLine(); !strings.Contains(chrome, "Last played") {
		t.Fatalf("recents chrome = %q", chrome)
	}
	added := Snapshot{ViewLabel: "Recently added", Collection: "recently_added", Sort: "recently_added", Status: "1 titles · Recently added · All · Recently added"}
	if chrome := added.ChromeLine(); !strings.Contains(chrome, "Recently added") {
		t.Fatalf("recently added chrome = %q", chrome)
	}
}

func focusPickerRow(t *testing.T, app *App, create bool, id string) {
	t.Helper()
	app.Press(CmdViewPicker, time.Now())
	if !app.ViewPickerOpen() {
		t.Fatal("picker should open")
	}
	for i := 0; i < 16; i++ {
		snap := app.Snapshot()
		if snap.ViewPickerIndex >= 0 && snap.ViewPickerIndex < len(snap.PickerRows) {
			row := snap.PickerRows[snap.ViewPickerIndex]
			if create && row.Create {
				return
			}
			if !create && row.ID == id {
				return
			}
		}
		app.Press(CmdDown, time.Now())
	}
	t.Fatalf("picker row not found create=%v id=%q rows=%#v", create, id, app.Snapshot().PickerRows)
}

func focusNameKey(t *testing.T, app *App, id string) {
	t.Helper()
	app.mu.Lock()
	ok := app.nameField.OSK.SelectID(id)
	app.mu.Unlock()
	if !ok {
		t.Fatalf("name osk key %q not found", id)
	}
}

func oskTypeName(t *testing.T, app *App, text string, now time.Time) {
	t.Helper()
	for _, r := range text {
		if r == ' ' {
			focusNameKey(t, app, "space")
			app.Press(CmdSelect, now)
			continue
		}
		focusNameKey(t, app, "char-"+string(r))
		app.Press(CmdSelect, now)
	}
}

func TestAppCollectionMembershipToggleAndErrorRevert(t *testing.T) {
	var mu sync.Mutex
	fail := false
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			game := availableGame("snes-mario", "Mario", "snes")
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"collections": []Collection{{ID: "weekend-queue", Name: "Weekend queue"}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/library/collections/weekend-queue/"):
			mu.Lock()
			methods = append(methods, r.Method)
			shouldFail := fail
			mu.Unlock()
			if shouldFail {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":{"code":"INTERNAL","message":"nope"}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "snes-mario", "collection": "weekend-queue", "member": r.Method == http.MethodPut,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && len(snap.Views) == 7
	})
	focusPickerRow(t, app, false, "weekend-queue")
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		n := len(methods)
		mu.Unlock()
		return n >= 1 && len(snap.Games) == 1 && gameHasCollection(snap.Games[0], "weekend-queue") && !strings.Contains(snap.Status, "collection failed")
	})
	mu.Lock()
	fail = true
	mu.Unlock()
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		n := len(methods)
		mu.Unlock()
		return n >= 2 && len(snap.Games) == 1 && gameHasCollection(snap.Games[0], "weekend-queue") && strings.Contains(snap.Status, "collection failed")
	})
	mu.Lock()
	got := append([]string(nil), methods...)
	mu.Unlock()
	if strings.Join(got, ",") != "PUT,DELETE" {
		t.Fatalf("methods = %#v", got)
	}
}

func TestAppRemoveFromCustomCollectionReloadsView(t *testing.T) {
	var mu sync.Mutex
	member := true
	var gameQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			gameQueries = append(gameQueries, r.URL.RawQuery)
			in := member
			mu.Unlock()
			var games []Game
			if r.URL.Query().Get("collection") != "weekend-queue" || in {
				game := availableGame("snes-mario", "Mario", "snes")
				if in {
					game.Collections = []string{"weekend-queue"}
				}
				games = []Game{game}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"collections": []Collection{{ID: "weekend-queue", Name: "Weekend queue"}},
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/library/collections/weekend-queue/"):
			mu.Lock()
			member = false
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "collection": "weekend-queue", "member": false})
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
	for i := 0; i < 6; i++ {
		app.Press(CmdViewNext, time.Now())
	}
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "weekend-queue" && len(snap.Games) == 1
	})
	focusPickerRow(t, app, false, "weekend-queue")
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "weekend-queue" && len(snap.Games) == 0
	})
	found := false
	mu.Lock()
	got := append([]string(nil), gameQueries...)
	mu.Unlock()
	for _, query := range got {
		if strings.Contains(query, "collection=weekend-queue") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queries = %#v", got)
	}
}

func TestAppAddToCustomCollectionReloadsView(t *testing.T) {
	var mu sync.Mutex
	member := false
	var gameQueries []string
	putStarted := make(chan struct{})
	putRelease := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-putRelease:
		default:
			close(putRelease)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			gameQueries = append(gameQueries, r.URL.RawQuery)
			in := member
			mu.Unlock()
			var games []Game
			collection := r.URL.Query().Get("collection")
			if collection == "" {
				game := availableGame("snes-mario", "Mario", "snes")
				if in {
					game.Collections = []string{"weekend-queue"}
				}
				games = []Game{game}
			} else if collection == "weekend-queue" && in {
				game := availableGame("snes-mario", "Mario", "snes")
				game.Collections = []string{"weekend-queue"}
				games = []Game{game}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"collections": []Collection{{ID: "weekend-queue", Name: "Weekend queue"}},
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/library/collections/weekend-queue/"):
			close(putStarted)
			<-putRelease
			mu.Lock()
			member = true
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "collection": "weekend-queue", "member": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && len(snap.Views) == 7
	})
	focusPickerRow(t, app, false, "weekend-queue")
	app.Press(CmdSortCycle, time.Now())
	select {
	case <-putStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("PUT did not start")
	}
	app.Press(CmdBack, time.Now())
	for i := 0; i < 6; i++ {
		app.Press(CmdViewNext, time.Now())
	}
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Collection == "weekend-queue"
	})
	close(putRelease)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && snap.Collection == "weekend-queue" && len(snap.Games) == 1 && snap.Games[0].ID == "snes-mario"
	})
	found := false
	mu.Lock()
	got := append([]string(nil), gameQueries...)
	mu.Unlock()
	for _, query := range got {
		if strings.Contains(query, "collection=weekend-queue") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queries = %#v", got)
	}
}

func TestAppHidesCreateUntilCollectionsLoad(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.URL.Path == "/api/v1/library/collections":
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{
				"collections": []Collection{{ID: "weekend-queue", Name: "Weekend queue"}},
			})
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
	app.Press(CmdViewPicker, time.Now())
	if !app.ViewPickerOpen() {
		t.Fatal("picker should open")
	}
	for _, row := range app.Snapshot().PickerRows {
		if row.Create {
			t.Fatal("create row should wait for a successful collection list")
		}
	}
	close(release)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		if !snap.ViewPicker {
			return false
		}
		for _, row := range snap.PickerRows {
			if row.Create {
				return true
			}
		}
		return false
	})
}

func TestAppHidesCreateWhenCollectionListFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.URL.Path == "/api/v1/library/collections":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":"INTERNAL","message":"nope"}}`)
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
	app.Press(CmdViewPicker, time.Now())
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, row := range app.Snapshot().PickerRows {
			if row.Create {
				t.Fatal("create row should stay hidden while the collection list is failing")
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAppCreateRenameDeleteCollectionWithOSK(t *testing.T) {
	var mu sync.Mutex
	collections := []Collection{}
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.URL.Path == "/api/v1/library/collections" && r.Method == http.MethodGet:
			mu.Lock()
			list := append([]Collection{}, collections...)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": list})
		case strings.HasPrefix(r.URL.Path, "/api/v1/library/collections/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/library/collections/")
			if strings.Contains(id, "/") {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			methods = append(methods, r.Method+" "+id+" "+r.URL.Query().Get("name"))
			switch r.Method {
			case http.MethodPut:
				if id == "favorites" {
					mu.Unlock()
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"code":"BAD_REQUEST","message":"collection request is invalid"}}`)
					return
				}
				name := r.URL.Query().Get("name")
				found := false
				for i := range collections {
					if collections[i].ID == id {
						collections[i].Name = name
						found = true
						break
					}
				}
				if !found {
					collections = append(collections, Collection{ID: id, Name: name})
				}
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "name": name})
				return
			case http.MethodDelete:
				next := collections[:0]
				for _, collection := range collections {
					if collection.ID != id {
						next = append(next, collection)
					}
				}
				collections = append([]Collection{}, next...)
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
				return
			}
			mu.Unlock()
			http.NotFound(w, r)
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

	focusPickerRow(t, app, true, "")
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.OSK.Open && snap.OSK.Prompt == "Collection name"
	})
	now := time.Now()
	oskTypeName(t, app, "weekend queue", now)
	focusNameKey(t, app, "done")
	app.Press(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.OSK.Open && !snap.ViewPicker && snap.Collection == "weekend-queue" && snap.ViewLabel == "weekend queue"
	})

	focusPickerRow(t, app, false, "weekend-queue")
	app.Press(CmdSearch, time.Now())
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.CollectionMenu.Open && !snap.CollectionMenu.Confirm
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.OSK.Open && snap.OSK.Buffer == "weekend queue"
	})
	focusNameKey(t, app, "clear")
	app.Press(CmdSelect, time.Now())
	oskTypeName(t, app, "saturday", time.Now())
	focusNameKey(t, app, "done")
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.OSK.Open && snap.ViewLabel == "saturday" && snap.Collection == "weekend-queue"
	})

	focusPickerRow(t, app, false, "weekend-queue")
	app.Press(CmdSearch, time.Now())
	app.Press(CmdDown, time.Now())
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, time.Second, func(snap Snapshot) bool {
		return snap.CollectionMenu.Confirm
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.ViewPicker && snap.Collection == "" && snap.ViewLabel == "All"
	})

	app.Press(CmdViewPicker, time.Now())
	app.Press(CmdDown, time.Now())
	app.Press(CmdDown, time.Now())
	app.Press(CmdSearch, time.Now())
	snap := app.Snapshot()
	if snap.CollectionMenu.Open {
		t.Fatal("favorites manage should be rejected")
	}
	if !strings.Contains(snap.Status, "cannot be renamed") {
		t.Fatalf("reserved status = %q", snap.Status)
	}

	mu.Lock()
	got := append([]string(nil), methods...)
	mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("methods = %#v", got)
	}
}

func TestAppNameOSKCancelKeepsPicker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []Collection{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading
	})
	focusPickerRow(t, app, true, "")
	app.Press(CmdSelect, time.Now())
	if !app.OSKOpen() {
		t.Fatal("name osk")
	}
	app.Press(CmdLayoutCycle, time.Now())
	app.Press(CmdSafeAreaIn, time.Now())
	if !app.OSKOpen() || !app.ViewPickerOpen() {
		t.Fatal("layout/safe-area should be ignored")
	}
	app.Press(CmdBack, time.Now())
	if app.OSKOpen() || !app.ViewPickerOpen() {
		t.Fatalf("empty B should cancel name and keep picker osk=%v picker=%v", app.OSKOpen(), app.ViewPickerOpen())
	}
}

func TestAppDeleteWhileBusyKeepsConfirm(t *testing.T) {
	t.Parallel()
	app := NewApp(nil, 800, 600, 10)
	app.mu.Lock()
	app.viewPickerOpen = true
	app.collectionManageOpen = true
	app.collectionConfirmOpen = true
	app.collectionManageID = "weekend-queue"
	app.collectionManageName = "Weekend"
	app.collections = []Collection{{ID: "weekend-queue", Name: "Weekend"}}
	app.collectionBusy = true
	app.handleCollectionConfirmLocked(CmdSelect)
	if !app.collectionConfirmOpen || !app.viewPickerOpen {
		t.Fatalf("confirm closed while busy picker=%v confirm=%v", app.viewPickerOpen, app.collectionConfirmOpen)
	}
	if !strings.Contains(app.status, "busy") {
		t.Fatalf("status = %q", app.status)
	}
	app.mu.Unlock()
}

func TestAppReservedCollectionRejectsRenameDelete(t *testing.T) {
	t.Parallel()
	app := NewApp(nil, 800, 600, 10)
	app.mu.Lock()
	app.deleteCollectionLocked("favorites")
	if !strings.Contains(app.status, "cannot be deleted") {
		t.Fatalf("delete status = %q", app.status)
	}
	app.nameEntry = nameEntryRename
	app.nameEntryID = "favorites"
	app.nameField.Buffer = "Favorites"
	app.submitNameEntryLocked()
	if !strings.Contains(app.status, "cannot be renamed") {
		t.Fatalf("rename status = %q", app.status)
	}
	app.mu.Unlock()
}
