package hostapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

// Compares hostclient.NewClient and tenfoot.NewClient on the current public
// library GET contract. Caller HTTP transport (auth/target headers, timeout)
// is supplied to both. Feature API names match tenfoot: NewClient, GameListQuery,
// ListGames, GamePresentation, Artwork.

func TestSharedAndTenfootLibraryClientsShareHTTPContract(t *testing.T) {
	handle := strings.Repeat("a", 64)
	gamesBody := `{"games":[{"id":"megadrive-sonic-test","title":"Sonic","system":"megadrive","state":"available","root_online":true,"launchable":true}],"next_cursor":"page-2"}`
	presentationBody := `{"game_id":"megadrive-sonic-test","state":"ready","presentation":{"cover_artwork_id":"` + handle + `","summary":"Host-local presentation metadata for Sonic.","year":"1991","genre":"Platformer"}}`
	unavailableBody := `{"error":{"code":"TARGET_UNAVAILABLE","message":"target status is unavailable"}}`
	notFoundBody := `{"error":{"code":"NOT_FOUND","message":"presentation is unavailable"}}`

	var mu sync.Mutex
	type seen struct {
		Method, Path, RawQuery, Accept, Authorization, TargetID string
	}
	var requests []seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, seen{
			Method:        r.Method,
			Path:          r.URL.Path,
			RawQuery:      r.URL.RawQuery,
			Accept:        r.Header.Get("Accept"),
			Authorization: r.Header.Get("Authorization"),
			TargetID:      r.Header.Get("X-FogCast-Target-ID"),
		})
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			query := r.URL.Query()
			if query.Get("cursor") == "fail" {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, unavailableBody)
				return
			}
			_, _ = io.WriteString(w, gamesBody)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/games/megadrive-sonic-test":
			_, _ = io.WriteString(w, presentationBody)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/games/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, notFoundBody)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, "art")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: authTransport{base: server.Client().Transport},
		Timeout:   5 * time.Second,
	}
	shared := hostclient.NewClient(server.URL, httpClient)
	native := tenfoot.NewClient(server.URL, httpClient)
	ctx := context.Background()

	sharedQuery := hostclient.GameListQuery{
		Cursor: "page-1", Limit: 2, Platform: "megadrive", Sort: "system",
		Q: "sonic", Collection: "favorites",
	}
	nativeQuery := hostclient.GameListQuery{
		Cursor: "page-1", Limit: 2, Platform: "megadrive", Sort: "system",
		Q: "sonic", Collection: "favorites",
	}
	sharedGames, sharedNext, err := shared.ListGames(ctx, sharedQuery)
	if err != nil {
		t.Fatalf("hostclient ListGames: %v", err)
	}
	nativeGames, nativeNext, err := native.ListGames(ctx, nativeQuery)
	if err != nil {
		t.Fatalf("tenfoot ListGames: %v", err)
	}
	if sharedNext != "page-2" || nativeNext != sharedNext {
		t.Fatalf("next_cursor shared=%q tenfoot=%q", sharedNext, nativeNext)
	}
	if len(sharedGames) != 1 || len(nativeGames) != 1 || sharedGames[0].ID != "megadrive-sonic-test" || nativeGames[0].ID != sharedGames[0].ID {
		t.Fatalf("games shared=%v tenfoot=%v", sharedGames, nativeGames)
	}

	sharedPres, err := shared.GamePresentation(ctx, "megadrive-sonic-test")
	if err != nil {
		t.Fatalf("hostclient GamePresentation: %v", err)
	}
	nativePres, err := native.GamePresentation(ctx, "megadrive-sonic-test")
	if err != nil {
		t.Fatalf("tenfoot GamePresentation: %v", err)
	}
	if sharedPres.GameID != "megadrive-sonic-test" || nativePres.GameID != sharedPres.GameID || sharedPres.State != "ready" || nativePres.State != sharedPres.State {
		t.Fatalf("presentation shared=%#v tenfoot=%#v", sharedPres, nativePres)
	}
	if sharedPres.Presentation == nil || nativePres.Presentation == nil || sharedPres.Presentation.CoverArtworkID != handle || nativePres.Presentation.CoverArtworkID != handle {
		t.Fatalf("cover shared=%#v tenfoot=%#v", sharedPres.Presentation, nativePres.Presentation)
	}

	sharedArt, sharedType, err := shared.Artwork(ctx, handle)
	if err != nil {
		t.Fatalf("hostclient Artwork: %v", err)
	}
	nativeArt, nativeType, err := native.Artwork(ctx, handle)
	if err != nil {
		t.Fatalf("tenfoot Artwork: %v", err)
	}
	if string(sharedArt) != "art" || string(nativeArt) != "art" || sharedType != "image/png" || nativeType != sharedType {
		t.Fatalf("artwork shared=%q/%q tenfoot=%q/%q", sharedArt, sharedType, nativeArt, nativeType)
	}

	_, _, err = shared.ListGames(ctx, hostclient.GameListQuery{Cursor: "fail", Limit: 1})
	if err == nil || !strings.Contains(err.Error(), "TARGET_UNAVAILABLE") {
		t.Fatalf("hostclient structured games error = %v", err)
	}
	_, _, err = native.ListGames(ctx, hostclient.GameListQuery{Cursor: "fail", Limit: 1})
	if err == nil || !strings.Contains(err.Error(), "TARGET_UNAVAILABLE") {
		t.Fatalf("tenfoot structured games error = %v", err)
	}
	if _, err = shared.GamePresentation(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("hostclient structured presentation error = %v", err)
	}
	if _, err = native.GamePresentation(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("tenfoot structured presentation error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("no HTTP requests recorded")
	}
	for _, req := range requests {
		if req.Authorization != "Bearer caller" || req.TargetID != "target" {
			t.Fatalf("caller identity not preserved: %+v", req)
		}
		switch req.Path {
		case "/api/v1/games":
			if req.Method != http.MethodGet || req.Accept != "application/json" {
				t.Fatalf("games request %+v", req)
			}
			if !strings.Contains(req.RawQuery, "grouped=1") || !strings.Contains(req.RawQuery, "availability=ready") {
				t.Fatalf("games query defaults %+v", req)
			}
			if strings.Contains(req.RawQuery, "cursor=page-1") {
				if !strings.Contains(req.RawQuery, "q=sonic") || !strings.Contains(req.RawQuery, "platform=megadrive") || !strings.Contains(req.RawQuery, "collection=favorites") || !strings.Contains(req.RawQuery, "limit=2") || !strings.Contains(req.RawQuery, "sort=platform") {
					t.Fatalf("games filter query %+v", req)
				}
			}
		case "/api/v1/presentation/games/megadrive-sonic-test", "/api/v1/presentation/games/missing":
			if req.Method != http.MethodGet || req.Accept != "application/json" {
				t.Fatalf("presentation request %+v", req)
			}
		case "/api/v1/presentation/artwork/" + handle:
			if req.Method != http.MethodGet || req.Accept != "image/*" {
				t.Fatalf("artwork request %+v", req)
			}
		default:
			t.Fatalf("unexpected path %+v", req)
		}
	}
}

type authTransport struct {
	base http.RoundTripper
}

func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer caller")
	clone.Header.Set("X-FogCast-Target-ID", "target")
	return t.base.RoundTrip(clone)
}
