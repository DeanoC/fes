package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListsGamesAndFollowsCursor(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.URL.Query().Get("cursor") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games":       []Game{{ID: "snes-mario", Title: "Mario", System: "snes", Launchable: true}},
				"next_cursor": "page-2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []Game{{ID: "megadrive-sonic", Title: "Sonic", System: "megadrive", Launchable: true}},
		})
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	games, err := client.FetchLibrary(context.Background(), 200, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0].ID != "snes-mario" || games[1].ID != "megadrive-sonic" {
		t.Fatalf("games = %#v", games)
	}
	if len(paths) != 2 || !strings.Contains(paths[0], "grouped=1") || !strings.Contains(paths[0], "limit=200") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestClientPresentationArtworkAndLaunch(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	var launchBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/games/snes-mario":
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &struct {
					CoverArtworkID string `json:"cover_artwork_id"`
					Summary        string `json:"summary"`
					Year           string `json:"year"`
					Genre          string `json:"genre"`
					Studio         string `json:"studio"`
				}{CoverArtworkID: handle, Summary: "jump", Year: "1985", Genre: "Platform", Studio: "Nintendo"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png-bytes"))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			launchBody = string(raw)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	pres, err := client.GamePresentation(context.Background(), "snes-mario")
	if err != nil || CoverHandle(Game{}, pres) != handle {
		t.Fatalf("presentation = %#v, %v", pres, err)
	}
	data, ctype, err := client.Artwork(context.Background(), handle)
	if err != nil || ctype != "image/png" || string(data) != "png-bytes" {
		t.Fatalf("artwork = %q %q %v", data, ctype, err)
	}
	result, err := client.Launch(context.Background(), "snes-mario")
	if err != nil || result.HTTPStatus != 200 || result.State != "active" || result.GameID != "snes-mario" {
		t.Fatalf("launch = %#v, %v", result, err)
	}
	if launchBody != `{"game_id":"snes-mario"}` {
		t.Fatalf("launch body = %q", launchBody)
	}
}

func TestClientLaunchRecordsHostError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"SOURCE_UNAVAILABLE","message":"game source is unavailable"}}`)
	}))
	t.Cleanup(server.Close)
	result, err := NewClient(server.URL, server.Client()).Launch(context.Background(), "gba-test")
	if err != nil {
		t.Fatal(err)
	}
	if result.HTTPStatus != 500 || result.ErrorCode != "SOURCE_UNAVAILABLE" {
		t.Fatalf("result = %#v", result)
	}
}

func TestNormalizeHandleRejectsShortValues(t *testing.T) {
	t.Parallel()
	if got := normalizeHandle("abc"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := CoverHandle(Game{Cover: "not-a-handle"}, Presentation{}); got != "" {
		t.Fatalf("cover handle = %q", got)
	}
}
