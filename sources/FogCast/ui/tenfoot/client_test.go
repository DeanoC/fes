package tenfoot

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/shared"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestClientSessionPreservesKitFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"state":"active","game_id":"pong","execution":"fpga_development","input":{"state":"attached","ready":true,"session_id":"kit-session"},"core_package":{"generation":18446744073709551614,"gamepad":true,"active_interfaces":[{"id":"fes.keyboard","major":1,"minor":0}]}}`)
	}))
	defer server.Close()

	result, err := NewClient(server.URL, server.Client()).Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Input == nil || result.Input.SessionID != "kit-session" {
		t.Fatalf("input = %+v", result.Input)
	}
	if result.CorePackage == nil || result.CorePackage.Generation != ^uint64(0)-1 || !result.CorePackage.Gamepad || len(result.CorePackage.ActiveInterfaces) != 1 {
		t.Fatalf("core package = %+v", result.CorePackage)
	}
}

func TestClientLaunchRejectsOversizeSessionResponse(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader(`{"state":"active"}` + strings.Repeat("x", 16<<20))),
			ContentLength: -1,
			Request:       req,
		}, nil
	})
	client := NewClient("http://host", &http.Client{Transport: transport})
	if _, err := client.Launch(context.Background(), "pong"); err == nil {
		t.Fatal("accepted oversized mutation response")
	}
}

func TestSessionMutationReadFailureRetainsHTTPStatus(t *testing.T) {
	const status = http.StatusBadGateway

	failures := []struct {
		name          string
		contentLength int64
	}{
		{name: "oversize", contentLength: maxAPIResponse + 1},
		{name: "truncated", contentLength: 8},
	}
	operations := []struct {
		name string
		call func(*Client) (hostclient.SessionResult, error)
	}{
		{name: "launch", call: func(c *Client) (hostclient.SessionResult, error) {
			return c.LaunchStamped(context.Background(), "pong", ClientStamp{})
		}},
		{name: "stop", call: func(c *Client) (hostclient.SessionResult, error) {
			return c.StopStamped(context.Background(), ClientStamp{})
		}},
		{name: "development-rbf", call: func(c *Client) (hostclient.SessionResult, error) {
			return c.LoadDevelopmentRBF(context.Background(), 1, strings.NewReader("x"))
		}},
		{name: "input-attach", call: func(c *Client) (hostclient.SessionResult, error) {
			return c.AttachInput(context.Background())
		}},
	}

	for _, failure := range failures {
		for _, operation := range operations {
			t.Run(failure.name+"/"+operation.name, func(t *testing.T) {
				client := NewClient("http://host", &http.Client{
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode:    status,
							Header:        make(http.Header),
							Body:          io.NopCloser(strings.NewReader("{}")),
							ContentLength: failure.contentLength,
							Request:       req,
						}, nil
					}),
				})
				result, err := operation.call(client)
				if err == nil {
					t.Fatal("accepted session response read failure")
				}
				if result.HTTPStatus != status {
					t.Fatalf("HTTPStatus = %d, want %d (err %v)", result.HTTPStatus, status, err)
				}
			})
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestClientDecodesGameRegion(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []hostclient.Game{{
				ID:         "megadrive-sonic",
				Title:      "Sonic",
				System:     "megadrive",
				Year:       "1991",
				Genre:      "Platform",
				Region:     "usa",
				Launchable: true,
			}},
		})
	}))
	t.Cleanup(server.Close)
	games, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), hostclient.GameListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].Region != "usa" || games[0].Year != "1991" || games[0].Genre != "Platform" {
		t.Fatalf("games = %#v", games)
	}
}

func TestClientCoreLibraryAndAvailability(t *testing.T) {
	t.Parallel()
	packageIDs := map[string]string{
		"pong":   strings.Repeat("a", 64),
		"zx81":   strings.Repeat("b", 64),
		"coleco": strings.Repeat("c", 64),
		"bad":    strings.Repeat("d", 64),
	}
	paths := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		paths[r.URL.Path]++
		switch r.URL.Path {
		case "/api/v1/library/core-entries":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries": []map[string]string{
					{"game_id": "fpga-pong", "title": "Pong", "core_id": "fes.pong", "package_id": packageIDs["pong"]},
					{"game_id": "fpga-zx81", "title": "ZX81", "core_id": "fes.zx81", "package_id": packageIDs["zx81"]},
					{"game_id": "fpga-coleco", "title": "Coleco", "core_id": "fes.coleco", "package_id": packageIDs["coleco"]},
					{"game_id": "fpga-bad", "title": "Bad", "core_id": "fes.bad", "package_id": packageIDs["bad"]},
				},
			})
		case "/api/v1/core-packages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"packages": []map[string]any{
					{"package_id": packageIDs["pong"], "descriptor": map[string]any{"core": map[string]string{"id": "fes.pong", "name": "FES Pong", "version": "1.0.0"}}, "compatibility": "unknown"},
					{"package_id": packageIDs["coleco"], "descriptor": map[string]any{"core": map[string]string{"id": "different.core", "name": "Wrong", "version": "2.0.0"}}, "compatibility": "compatible"},
					{"package_id": packageIDs["bad"], "descriptor": map[string]any{"core": map[string]string{"id": "fes.bad", "name": "Bad", "version": "3.0.0"}}, "compatibility": "incompatible"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	library, err := NewClient(server.URL, server.Client()).CoreLibrary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if paths["/api/v1/library/core-entries"] != 1 || paths["/api/v1/core-packages"] != 1 {
		t.Fatalf("paths = %#v", paths)
	}
	if len(library.Entries) != 4 || len(library.Packages) != 3 {
		t.Fatalf("library = %#v", library)
	}

	got := library.Availability()
	byGame := make(map[string]hostclient.CoreAvailability, len(got))
	for _, status := range got {
		byGame[status.GameID] = status
	}
	tests := []struct {
		game, state, packageCore, compatibility string
	}{
		{"fpga-pong", "installed", "fes.pong", "unknown"},
		{"fpga-zx81", "missing", "", ""},
		{"fpga-coleco", "mismatch", "different.core", "compatible"},
		{"fpga-bad", "incompatible", "fes.bad", "incompatible"},
	}
	for _, test := range tests {
		status, ok := byGame[test.game]
		if !ok {
			t.Fatalf("%s missing from %#v", test.game, got)
		}
		if status.State != test.state || status.PackageCoreID != test.packageCore || status.Compatibility != test.compatibility {
			t.Errorf("%s = %#v", test.game, status)
		}
	}
	if label := byGame["fpga-pong"].Label(); !strings.Contains(label, "fes.pong") || !strings.Contains(label, "unknown") {
		t.Fatalf("installed label = %q", label)
	}
}

func TestClientDecodesPlayStatsAndOptionalBadgeFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []hostclient.Game{{
					ID: "megadrive-sonic", Title: "Sonic", System: "megadrive",
					Launchable: true, PlayCount: 4, LastPlayedAt: 99,
				}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			_ = json.NewEncoder(w).Encode(hostclient.Presentation{
				GameID: "megadrive-sonic",
				State:  "ready",
				Presentation: &hostclient.PresentationInfo{
					Players: "2", Rating: "4.5", Completion: "100%", Portable: true,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	games, _, err := client.ListGames(context.Background(), hostclient.GameListQuery{Limit: 10})
	if err != nil || len(games) != 1 || games[0].PlayCount != 4 || games[0].LastPlayedAt != 99 {
		t.Fatalf("games = %#v err=%v", games, err)
	}
	pres, err := client.GamePresentation(context.Background(), "megadrive-sonic")
	if err != nil || pres.Presentation == nil || pres.Presentation.Players != "2" || pres.Presentation.Rating != "4.5" || pres.Presentation.Completion != "100%" || !pres.Presentation.Portable {
		t.Fatalf("presentation = %#v err=%v", pres, err)
	}
}

func TestClientListsGamesAndFollowsCursor(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.URL.Query().Get("cursor") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games":       []hostclient.Game{{ID: "snes-mario", Title: "Mario", System: "snes", Launchable: true}},
				"next_cursor": "page-2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []hostclient.Game{{ID: "megadrive-sonic", Title: "Sonic", System: "megadrive", Launchable: true}},
		})
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	games, err := client.FetchLibrary(context.Background(), hostclient.GameListQuery{Limit: 200, Sort: "title"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0].ID != "snes-mario" || games[1].ID != "megadrive-sonic" {
		t.Fatalf("games = %#v", games)
	}
	if len(paths) != 2 || !strings.Contains(paths[0], "grouped=1") || !strings.Contains(paths[0], "availability=ready") || !strings.Contains(paths[0], "limit=200") || !strings.Contains(paths[0], "sort=title") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestListGamesPrefersLaunchableVariant(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []hostclient.Game{{
				ID:         "snes-sonic-usa",
				Title:      "Sonic",
				System:     "snes",
				State:      "available",
				RootOnline: false,
				Launchable: true,
				Variants: []hostclient.Game{
					{ID: "snes-sonic-usa", Title: "Sonic", System: "snes", State: "available", RootOnline: false, Launchable: true},
					{ID: "snes-sonic-japan", Title: "Sonic", System: "snes", State: "available", RootOnline: true, Launchable: true},
				},
			}},
		})
	}))
	t.Cleanup(server.Close)
	games, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), hostclient.GameListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].ID != "snes-sonic-japan" {
		t.Fatalf("games = %#v", games)
	}
	if games[0].Variants != nil {
		t.Fatalf("variants leaked into catalog row: %#v", games[0].Variants)
	}
}

func TestClientPresentationArtworkAndLaunch(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	var launchBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/games/snes-mario":
			_ = json.NewEncoder(w).Encode(hostclient.Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &hostclient.PresentationInfo{
					CoverArtworkID: handle,
					VideoID:        strings.Repeat("ef", 32),
					Summary:        "jump",
					Year:           "1985",
					Genre:          "Platform",
					Studio:         "Nintendo",
					Players:        "1-2",
					ScreenshotIDs:  []string{handle, "bad", handle, strings.Repeat("cd", 32)},
				},
				Attribution: &hostclient.PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
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
	if err != nil || shared.CoverHandle(hostclient.Game{}, pres) != handle {
		t.Fatalf("presentation = %#v, %v", pres, err)
	}
	if got := pres.AttributionLabel(); got != "Data from IGDB.com" {
		t.Fatalf("attribution = %q", got)
	}
	if pres.Presentation == nil || pres.Presentation.Studio != "Nintendo" || pres.Presentation.Players != "1-2" {
		t.Fatalf("studio/players = %#v", pres.Presentation)
	}
	if got := shared.ScreenshotHandles(pres.Presentation.ScreenshotIDs); len(got) != 2 || got[0] != handle {
		t.Fatalf("screenshots = %#v", pres.Presentation.ScreenshotIDs)
	}
	if got := shared.VideoHandle(pres); got != strings.Repeat("ef", 32) {
		t.Fatalf("video = %q", got)
	}
	if got := shared.MarqueeHandle(pres); got != "" {
		t.Fatalf("unexpected marquee %q", got)
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

func TestClientSessionAndStop(t *testing.T) {
	t.Parallel()
	var stopPath, stopBody string
	var stopCT string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{
				"state":"active",
				"game_id":"snes-mario",
				"system":"snes",
				"execution":"fpga_native",
				"media":"active",
				"progress":{"stage":"core","message":"running"},
				"input":{"state":"attached","ready":true}
			}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			stopPath = r.URL.Path
			stopBody = string(raw)
			stopCT = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped","execution":"fpga_native"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	got, err := client.Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.HTTPStatus != 200 || got.State != "active" || got.GameID != "snes-mario" || got.System != "snes" {
		t.Fatalf("session = %#v", got)
	}
	if got.Execution != "fpga_native" || got.Media != "active" {
		t.Fatalf("session overlays = %#v", got)
	}
	if got.Progress == nil || got.Progress.Stage != "core" || got.Progress.Message != "running" {
		t.Fatalf("progress = %#v", got.Progress)
	}
	if got.Input == nil || got.Input.State != "attached" || !got.Input.Ready {
		t.Fatalf("input = %#v", got.Input)
	}

	idleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(idleServer.Close)
	idle, err := NewClient(idleServer.URL, idleServer.Client()).Session(context.Background())
	if err != nil || idle.State != "idle" || idle.GameID != "" || idle.System != "" {
		t.Fatalf("idle session = %#v, %v", idle, err)
	}

	stopped, err := client.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stopped.HTTPStatus != 200 || stopped.State != "idle" || stopped.Media != "stopped" || stopped.Execution != "fpga_native" {
		t.Fatalf("stop = %#v", stopped)
	}
	if stopPath != "/api/v1/session/stop" || stopBody != "" {
		t.Fatalf("stop request path=%q body=%q", stopPath, stopBody)
	}
	if stopCT != "" {
		t.Fatalf("stop Content-Type = %q", stopCT)
	}
}

func TestClientLoadDevelopmentRBFPostsOctetStream(t *testing.T) {
	t.Parallel()
	payload := []byte("development-rbf")
	var gotLen int64
	var gotCT, gotTE string
	var gotBody []byte
	var stopped bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/development-rbf":
			gotCT = r.Header.Get("Content-Type")
			gotTE = strings.Join(r.TransferEncoding, ",")
			gotLen = r.ContentLength
			raw, _ := io.ReadAll(r.Body)
			gotBody = raw
			_, _ = io.WriteString(w, `{"state":"active","execution":"fpga_development","development":true}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			stopped = true
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	got, err := client.LoadDevelopmentRBF(context.Background(), int64(len(payload)), strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "active" || got.Execution != "fpga_development" || !got.Development {
		t.Fatalf("result = %#v", got)
	}
	if gotCT != "application/octet-stream" || gotTE != "" || gotLen != int64(len(payload)) || string(gotBody) != string(payload) {
		t.Fatalf("ct=%q te=%q len=%d body=%q", gotCT, gotTE, gotLen, gotBody)
	}
	idle, err := client.Stop(context.Background())
	if err != nil || idle.State != "idle" || !stopped {
		t.Fatalf("stop = %#v err=%v stopped=%v", idle, err, stopped)
	}
}

func TestClientLoadDevelopmentRBFRejectsInvalidInputBeforeRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		http.Error(w, "no request expected", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	if _, err := client.LoadDevelopmentRBF(context.Background(), 0, strings.NewReader("")); err == nil {
		t.Fatal("empty accepted")
	}
	if _, err := client.LoadDevelopmentRBF(context.Background(), (32<<20)+1, strings.NewReader("rbf")); err == nil {
		t.Fatal("oversize accepted")
	}
	if _, err := client.LoadDevelopmentRBF(context.Background(), 3, nil); err == nil {
		t.Fatal("nil reader accepted")
	}
}

func TestClientLoadDevelopmentRBFHostContentTypeError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"BAD_REQUEST","message":"development RBF upload requires a bounded application/octet-stream body"}}`)
	}))
	t.Cleanup(server.Close)
	got, err := NewClient(server.URL, server.Client()).LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ErrorCode != "BAD_REQUEST" {
		t.Fatalf("result = %#v", got)
	}
}

func TestClientSessionDecodesDevelopmentFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"state":"active","development_active":true,"development_session_state":"fpga_development"}`)
	}))
	t.Cleanup(server.Close)
	got, err := NewClient(server.URL, server.Client()).Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Development || got.DevelopmentSessionState != "fpga_development" || got.Execution != "fpga_development" {
		t.Fatalf("session = %#v", got)
	}
}

func TestClientSessionEventsPollsAfterCursor(t *testing.T) {
	t.Parallel()
	var gotAfter []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/session/events" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		gotAfter = append(gotAfter, r.URL.Query().Get("after"))
		after := r.URL.Query().Get("after")
		if after == "" || after == "0" {
			gid := "snes-mario"
			sys := "snes"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"events": []map[string]any{
					{"sequence": 1, "event": "session.launch", "state": "active", "game_id": gid, "system": sys},
					{"sequence": 2, "event": "session.input.attach", "state": "active", "game_id": gid},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]any{
				{"sequence": 3, "event": "session.stop", "state": "idle", "media": "stopped"},
			},
		})
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	first, err := client.SessionEvents(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Sequence != 1 || first[0].Event != "session.launch" || first[0].GameID != "snes-mario" || first[0].System != "snes" {
		t.Fatalf("first events = %#v", first)
	}
	next, err := client.SessionEvents(context.Background(), first[len(first)-1].Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || next[0].Sequence != 3 || next[0].Event != "session.stop" || next[0].State != "idle" {
		t.Fatalf("next events = %#v", next)
	}
	if len(gotAfter) != 2 || gotAfter[0] != "0" || gotAfter[1] != "2" {
		t.Fatalf("after query = %#v", gotAfter)
	}
}

func TestClientKitLeaseStatusOnlyDecode(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/kit/lease" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state":         "held",
			"owner":         "fogcast@powerboat",
			"purpose":       "interactive game/development session",
			"generation":    "abc123def456",
			"expires_at":    "2026-09-06T12:00:00Z",
			"expires_in_ms": 72000,
		})
	}))
	t.Cleanup(server.Close)
	got, err := NewClient(server.URL, server.Client()).KitLease(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "held" || got.Owner != "fogcast@powerboat" || got.Purpose == "" || got.Generation != "abc123def456" || got.ExpiresInMS != 72000 {
		t.Fatalf("lease = %#v", got)
	}
	if got.Unavailable {
		t.Fatal("held lease marked unavailable")
	}

	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state":      "blocked",
			"reason":     "kit cleanup failed or agent shutting down; operator recovery required",
			"generation": "deadbeef",
		})
	}))
	t.Cleanup(blocked.Close)
	got, err = NewClient(blocked.URL, blocked.Client()).KitLease(context.Background(), blocked.URL)
	if err != nil || got.State != "blocked" || !strings.Contains(got.Reason, "cleanup failed") {
		t.Fatalf("blocked = %#v, %v", got, err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	down.Close()
	got, err = NewClient(down.URL, down.Client()).KitLease(context.Background(), down.URL)
	if err != nil || !got.Unavailable {
		t.Fatalf("down kit = %#v, %v", got, err)
	}

	empty, err := NewClient(server.URL, server.Client()).KitLease(context.Background(), "")
	if err != nil || empty.State != "" || empty.Unavailable {
		t.Fatalf("empty target = %#v, %v", empty, err)
	}

	unauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"UNAUTHORIZED","message":"missing or incorrect bearer token"}}`)
	}))
	t.Cleanup(unauth.Close)
	got, err = NewClient(unauth.URL, unauth.Client()).KitLease(context.Background(), unauth.URL)
	if err != nil || got.ErrorCode != "UNAUTHORIZED" || !strings.Contains(formatKitLeaseLine(got), "kit lease") {
		t.Fatalf("unauthorized lease = %#v, %v", got, err)
	}
}

func TestDecodeKitLeaseBlockedError(t *testing.T) {
	t.Parallel()
	got := decodeKitLeaseBody(503, []byte(`{"error":{"code":"KIT_LEASE_BLOCKED","message":"cleanup failed"}}`))
	if got.State != "blocked" || got.Unavailable || got.ErrorCode != "KIT_LEASE_BLOCKED" {
		t.Fatalf("blocked error = %#v", got)
	}
	down := decodeKitLeaseBody(503, []byte(`{"error":{"code":"MISTER_UNAVAILABLE","message":"target down"}}`))
	if !down.Unavailable || down.ErrorCode != "MISTER_UNAVAILABLE" {
		t.Fatalf("mister unavailable = %#v", down)
	}
}

func TestClientRejectsMalformedSessionResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code int
		body string
	}{
		{name: "empty", code: 200, body: ""},
		{name: "204", code: 204, body: ""},
		{name: "empty object", code: 200, body: `{}`},
		{name: "unknown state", code: 200, body: `{"state":"nope"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			client := NewClient(server.URL, server.Client())
			if _, err := client.Session(context.Background()); err == nil {
				t.Fatal("Session accepted malformed body")
			}
			if _, err := client.Launch(context.Background(), "snes-mario"); err == nil {
				t.Fatal("Launch accepted malformed body")
			}
			if _, err := client.Stop(context.Background()); err == nil {
				t.Fatal("Stop accepted malformed body")
			}
		})
	}
}

func TestClientLaunchRejectsNonActiveSuccess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(server.Close)
	if _, err := NewClient(server.URL, server.Client()).Launch(context.Background(), "snes-mario"); err == nil {
		t.Fatal("Launch accepted idle success")
	}
}

func TestClientStopRejectsNonIdleSuccess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
	}))
	t.Cleanup(server.Close)
	if _, err := NewClient(server.URL, server.Client()).Stop(context.Background()); err == nil {
		t.Fatal("Stop accepted active success")
	}
}

func TestClientHealthAndStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready":  true,
				"target": map[string]any{"reachable": false, "ready": false, "connection": map[string]any{"state": "connecting", "message": "looking for den", "target_id": "target-123"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/status":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"connection":{"state":"disconnected","message":"target not found","target_id":"target-123"},"error":{"code":"TARGET_UNAVAILABLE","message":"target status is unavailable"}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !health.Ready || health.TargetReachable || health.TargetReady || health.Connection.State != "connecting" || health.Connection.TargetID != "target-123" {
		t.Fatalf("health = %#v", health)
	}
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Unavailable || status.HTTPStatus != 503 || status.ErrorCode != "TARGET_UNAVAILABLE" || status.Connection.State != "disconnected" {
		t.Fatalf("status = %#v", status)
	}
}

func TestClientSessionInputAttachDetachEmptyBody(t *testing.T) {
	t.Parallel()
	var attachCT, detachCT, attachBody, detachBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session/input":
			_, _ = io.WriteString(w, `{"state":"detached","ready":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/input/attach":
			attachCT = r.Header.Get("Content-Type")
			attachBody = string(raw)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"attached","ready":true}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/input/detach":
			detachCT = r.Header.Get("Content-Type")
			detachBody = string(raw)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"detached","ready":false}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	got, err := client.SessionInput(context.Background())
	if err != nil || got.State != "detached" {
		t.Fatalf("session input = %#v, %v", got, err)
	}
	attached, err := client.AttachInput(context.Background())
	if err != nil || attached.Input == nil || attached.Input.State != "attached" {
		t.Fatalf("attach = %#v, %v", attached, err)
	}
	if attachBody != "" || attachCT != "" {
		t.Fatalf("attach request body=%q ct=%q", attachBody, attachCT)
	}
	detached, err := client.DetachInput(context.Background())
	if err != nil || detached.Input == nil || detached.Input.State != "detached" {
		t.Fatalf("detach = %#v, %v", detached, err)
	}
	if detachBody != "" || detachCT != "" {
		t.Fatalf("detach request body=%q ct=%q", detachBody, detachCT)
	}
}

func TestHostTransportErrorIncludesClientTimeout(t *testing.T) {
	t.Parallel()
	if isHostTransportError(context.Canceled) || isHostTransportError(context.Cause(context.Background())) {
		t.Fatal("canceled or nil should not be host transport")
	}
	timeout := &url.Error{Op: "Get", URL: "http://127.0.0.1:8787/api/v1/health", Err: context.DeadlineExceeded}
	if !isHostTransportError(timeout) {
		t.Fatal("http client timeout should be host unreachable")
	}
	if !isHostTransportError(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded without cancel should be host transport")
	}
	if isHostTransportError(errors.New("host API status 404")) {
		t.Fatal("HTTP status error is not transport")
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

func TestClientListsGamesWithPlatformSortAndSearch(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{}})
	}))
	t.Cleanup(server.Close)
	_, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), hostclient.GameListQuery{
		Limit:    50,
		Platform: "snes",
		Sort:     "system",
		Q:        "mario & luigi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %#v", paths)
	}
	for _, want := range []string{"grouped=1", "availability=ready", "platform=snes", "sort=platform", "q=mario"} {
		if !strings.Contains(paths[0], want) {
			t.Fatalf("missing %q in %s", want, paths[0])
		}
	}
}

func TestListGamesEncodesFacetsAndHideFlags(t *testing.T) {
	t.Parallel()
	var raw string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{}})
	}))
	t.Cleanup(server.Close)
	_, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), hostclient.GameListQuery{
		Limit:          20,
		Genre:          "Action",
		Year:           "1991",
		Region:         "japan",
		HidePrerelease: true,
		HideHacks:      true,
		Collection:     "continue",
		Platform:       "snes",
	})
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("grouped") != "1" || values.Get("availability") != "ready" {
		t.Fatalf("defaults = %s", raw)
	}
	if values.Get("genre") != "Action" || values.Get("year") != "1991" || values.Get("region") != "japan" {
		t.Fatalf("facets = %s", raw)
	}
	if values.Get("hide_prerelease") != "1" || values.Get("hide_hacks") != "1" {
		t.Fatalf("hide = %s", raw)
	}
	if values.Get("collection") != "continue" || values.Get("platform") != "snes" {
		t.Fatalf("browse = %s", raw)
	}

	raw = ""
	_, _, err = NewClient(server.URL, server.Client()).ListGames(context.Background(), hostclient.GameListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	values, err = url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"genre", "year", "region", "hide_prerelease", "hide_hacks"} {
		if values.Get(key) != "" {
			t.Fatalf("omitted %s leaked in %s", key, raw)
		}
	}
}

func TestFacetsNilArraysDecodeEmpty(t *testing.T) {
	t.Parallel()
	cases := []string{`{}`, `{"genres":null,"years":null}`, `{"genres":[],"years":[]}`}
	for _, body := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/library/facets" {
				t.Errorf("path = %s", r.URL.Path)
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, body)
		}))
		got, err := NewClient(server.URL, server.Client()).Facets(context.Background())
		server.Close()
		if err != nil {
			t.Fatalf("body %s: %v", body, err)
		}
		if got.Genres == nil || got.Years == nil || len(got.Genres) != 0 || len(got.Years) != 0 {
			t.Fatalf("body %s: %#v", body, got)
		}
	}
}

func TestFacetsDecodesGenresAndYears(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"genres": []string{"Action", "RPG"}, "years": []string{"1991", "1992"}})
	}))
	t.Cleanup(server.Close)
	got, err := NewClient(server.URL, server.Client()).Facets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "Action" || len(got.Years) != 2 || got.Years[1] != "1992" {
		t.Fatalf("facets = %#v", got)
	}
}

func TestClientListsPlatforms(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/platforms" {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"platforms": []hostclient.Platform{{ID: "snes", Label: "Super NES", GameCount: 3, Launchable: true}},
		})
	}))
	t.Cleanup(server.Close)
	platforms, err := NewClient(server.URL, server.Client()).Platforms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(platforms) != 1 || platforms[0].ID != "snes" || platforms[0].Label != "Super NES" {
		t.Fatalf("platforms = %#v", platforms)
	}
}

func TestPresentationAttributionLabel(t *testing.T) {
	t.Parallel()
	igdb := hostclient.Presentation{Attribution: &hostclient.PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"}}
	if got := igdb.AttributionLabel(); got != "Data from IGDB.com" {
		t.Fatalf("igdb = %q", got)
	}
	launchbox := hostclient.Presentation{Attribution: &hostclient.PresentationAttribution{Provider: "launchbox", Label: "Data from LaunchBox Games Database"}}
	if got := launchbox.AttributionLabel(); got != "Data from LaunchBox Games Database" {
		t.Fatalf("launchbox = %q", got)
	}
	unknown := hostclient.Presentation{Attribution: &hostclient.PresentationAttribution{Provider: "steam", Label: "Steam"}}
	if got := unknown.AttributionLabel(); got != "" {
		t.Fatalf("unknown = %q", got)
	}
	if got := (hostclient.Presentation{}).AttributionLabel(); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestClientListsGamesWithCollection(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []hostclient.Game{{ID: "snes-mario", Title: "Mario", Favorite: true, Collections: []string{"weekend-queue"}}},
		})
	}))
	t.Cleanup(server.Close)
	games, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), hostclient.GameListQuery{
		Limit:      50,
		Collection: "favorites",
		Sort:       "title",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || !games[0].Favorite || len(games[0].Collections) != 1 || games[0].Collections[0] != "weekend-queue" {
		t.Fatalf("games = %#v", games)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %#v", paths)
	}
	for _, want := range []string{"grouped=1", "availability=ready", "collection=favorites", "sort=title"} {
		if !strings.Contains(paths[0], want) {
			t.Fatalf("missing %q in %s", want, paths[0])
		}
	}
}

func TestClientListsCollectionsAndSetFavorite(t *testing.T) {
	t.Parallel()
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"collections": []hostclient.Collection{{ID: "weekend-queue", Name: "Weekend queue"}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/favorites/snes-mario":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "favorite": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/library/favorites/snes-mario":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "favorite": false})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	collections, err := client.Collections(context.Background())
	if err != nil || len(collections) != 1 || collections[0].ID != "weekend-queue" || collections[0].Name != "Weekend queue" {
		t.Fatalf("collections = %#v, %v", collections, err)
	}
	if err := client.SetFavorite(context.Background(), "snes-mario", true); err != nil {
		t.Fatal(err)
	}
	if err := client.SetFavorite(context.Background(), "snes-mario", false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "GET /api/v1/library/collections,PUT /api/v1/library/favorites/snes-mario,DELETE /api/v1/library/favorites/snes-mario" {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestClientCollectionMembershipAndCRUD(t *testing.T) {
	t.Parallel()
	var methods []string
	var upsertName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/collections/weekend-queue/snes-mario":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "collection": "weekend-queue", "member": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/library/collections/weekend-queue/snes-mario":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "collection": "weekend-queue", "member": false})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/collections/weekend-queue":
			upsertName = r.URL.Query().Get("name")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "weekend-queue", "name": upsertName})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/library/collections/weekend-queue":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "weekend-queue"})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/collections/favorites":
			http.Error(w, `{"error":{"code":"BAD_REQUEST","message":"collection request is invalid"}}`, http.StatusBadRequest)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	if err := client.SetCollectionMember(context.Background(), "weekend-queue", "snes-mario", true); err != nil {
		t.Fatal(err)
	}
	if err := client.SetCollectionMember(context.Background(), "weekend-queue", "snes-mario", false); err != nil {
		t.Fatal(err)
	}
	got, err := client.UpsertCollection(context.Background(), "weekend-queue", "Weekend queue")
	if err != nil || got.ID != "weekend-queue" || got.Name != "Weekend queue" || upsertName != "Weekend queue" {
		t.Fatalf("upsert = %#v name=%q err=%v", got, upsertName, err)
	}
	if err := client.DeleteCollection(context.Background(), "weekend-queue"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpsertCollection(context.Background(), "favorites", "Favorites"); err == nil {
		t.Fatal("reserved upsert should fail")
	}
	want := "PUT /api/v1/library/collections/weekend-queue/snes-mario,DELETE /api/v1/library/collections/weekend-queue/snes-mario,PUT /api/v1/library/collections/weekend-queue,DELETE /api/v1/library/collections/weekend-queue,PUT /api/v1/library/collections/favorites"
	if strings.Join(methods, ",") != want {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestClientAttractPlaylist(t *testing.T) {
	t.Parallel()
	backdrop := strings.Repeat("ab", 32)
	cover := strings.Repeat("cd", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/attract" {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("limit") != "24" {
			t.Errorf("limit = %s", r.URL.Query().Get("limit"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"idle_seconds": 45,
			"items": []map[string]any{{
				"game_id":    "snes-mario",
				"title":      "Mario",
				"platform":   "snes",
				"backdrop":   backdrop,
				"cover":      cover,
				"launchable": true,
			}},
		})
	}))
	t.Cleanup(server.Close)
	playlist, err := NewClient(server.URL, server.Client()).Attract(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if playlist.IdleSeconds != 45 || len(playlist.Items) != 1 {
		t.Fatalf("playlist = %#v", playlist)
	}
	item := playlist.Items[0]
	if item.StillHandle() != backdrop {
		t.Fatalf("still handle = %q want backdrop", item.StillHandle())
	}
}

func TestClientFetchVideoFileStreamsAcceptVideo(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ee", 32)
	payload := make([]byte, 32)
	copy(payload[4:8], []byte("ftyp"))
	var accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/presentation/artwork/"+handle {
			http.NotFound(w, r)
			return
		}
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	path, err := NewClient(server.URL, server.Client()).FetchVideoFile(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if accept != "video/*" {
		t.Fatalf("Accept = %q", accept)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("file = %q", got)
	}
}

func TestClientFetchVideoFileRejectsImageMIME(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ff", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("not-a-video-file!!"))
	}))
	t.Cleanup(server.Close)
	path, err := NewClient(server.URL, server.Client()).FetchVideoFile(context.Background(), handle)
	if path != "" || err == nil {
		t.Fatalf("path=%q err=%v", path, err)
	}
}

func TestAttractStillHandlePrefersBackdropThenCoverThenMarquee(t *testing.T) {
	t.Parallel()
	backdrop := strings.Repeat("aa", 32)
	cover := strings.Repeat("bb", 32)
	marquee := strings.Repeat("cc", 32)
	video := strings.Repeat("dd", 32)
	item := hostclient.AttractItem{Video: video, Cover: cover, Backdrop: backdrop, Marquee: marquee}
	if item.StillHandle() != backdrop {
		t.Fatalf("got %q", item.StillHandle())
	}
	item.Backdrop = ""
	if item.StillHandle() != cover {
		t.Fatalf("cover fallback = %q", item.StillHandle())
	}
	item.Cover = ""
	if item.StillHandle() != marquee {
		t.Fatalf("marquee fallback = %q", item.StillHandle())
	}
	item.Marquee = ""
	if item.StillHandle() != "" {
		t.Fatalf("video must not be a still handle, got %q", item.StillHandle())
	}
	item = hostclient.AttractItem{Video: video, Cover: cover, Backdrop: backdrop, Marquee: marquee}
	handles := item.StillHandles()
	if len(handles) != 3 || handles[0] != backdrop || handles[1] != cover || handles[2] != marquee {
		t.Fatalf("still handles %v", handles)
	}
	if item.VideoHandle() != video {
		t.Fatalf("video handle %q", item.VideoHandle())
	}
}

func TestAttractPreviewHandlesMotionAndStillsFallback(t *testing.T) {
	t.Parallel()
	shot := strings.Repeat("aa", 32)
	backdrop := strings.Repeat("bb", 32)
	cover := strings.Repeat("cc", 32)
	marquee := strings.Repeat("dd", 32)
	video := strings.Repeat("ee", 32)
	item := hostclient.AttractItem{Video: video, Backdrop: backdrop, Cover: cover, Marquee: marquee}
	got := shared.AttractPreviewHandles(item, hostclient.Presentation{})
	if len(got) != 3 || got[0] != backdrop || got[1] != cover || got[2] != marquee {
		t.Fatalf("item stills %#v", got)
	}

	preview := shared.AttractPreviewHandles(item, hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{
			VideoID:           video,
			ScreenshotIDs:     []string{shot, "bad"},
			BackdropArtworkID: strings.Repeat("ff", 32),
		},
	})
	if len(preview) != 4 || preview[0] != shot || preview[1] != backdrop || preview[2] != cover || preview[3] != marquee {
		t.Fatalf("video preview %#v", preview)
	}

	stills := shared.AttractPreviewHandles(hostclient.AttractItem{Backdrop: backdrop, Cover: cover}, hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{ScreenshotIDs: []string{shot}, VideoID: ""},
	})
	if len(stills) != 2 || stills[0] != backdrop || stills[1] != cover {
		t.Fatalf("stills fallback %#v", stills)
	}

	empty := shared.AttractPreviewHandles(hostclient.AttractItem{Video: video}, hostclient.Presentation{})
	if empty != nil {
		t.Fatalf("video-only %#v", empty)
	}

	fromPres := shared.AttractPreviewHandles(hostclient.AttractItem{Video: video, Backdrop: backdrop}, hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{VideoID: video, MarqueeID: marquee},
	})
	if len(fromPres) != 2 || fromPres[0] != backdrop || fromPres[1] != marquee {
		t.Fatalf("presentation marquee %#v", fromPres)
	}
}

func TestAttractMarqueeHandlePrefersPresentationThenItem(t *testing.T) {
	t.Parallel()
	item := strings.Repeat("aa", 32)
	pres := strings.Repeat("bb", 32)
	row := hostclient.AttractItem{Marquee: item, Backdrop: strings.Repeat("cc", 32)}
	if got := shared.AttractMarqueeHandle(row, hostclient.Presentation{}); got != item {
		t.Fatalf("item = %q", got)
	}
	if got := shared.AttractMarqueeHandle(row, hostclient.Presentation{Presentation: &hostclient.PresentationInfo{MarqueeID: pres}}); got != pres {
		t.Fatalf("presentation = %q", got)
	}
	if got := shared.AttractMarqueeHandle(hostclient.AttractItem{}, hostclient.Presentation{}); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := shared.MarqueeHandle(hostclient.Presentation{Presentation: &hostclient.PresentationInfo{MarqueeID: pres, LogoID: item}}); got != pres {
		t.Fatalf("presentation handle = %q", got)
	}
	if got := shared.MarqueeHandle(hostclient.Presentation{Presentation: &hostclient.PresentationInfo{LogoID: item}}); got != "" {
		t.Fatalf("logo-only = %q", got)
	}
}

func TestNormalizeHandleRejectsShortValues(t *testing.T) {
	t.Parallel()
	if got := hostclient.NormalizeHandle("abc"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := shared.CoverHandle(hostclient.Game{Cover: "not-a-handle"}, hostclient.Presentation{}); got != "" {
		t.Fatalf("cover handle = %q", got)
	}
}

func TestDetailPreviewHandlesSelectsShotsThenPoster(t *testing.T) {
	t.Parallel()
	shot := strings.Repeat("aa", 32)
	shot2 := strings.Repeat("bb", 32)
	backdrop := strings.Repeat("cc", 32)
	cover := strings.Repeat("dd", 32)
	video := strings.Repeat("ee", 32)
	dupShot := shot

	stills := shared.DetailPreviewHandles(hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{ScreenshotIDs: []string{shot, shot2, "nope"}},
	}, cover)
	if len(stills) != 2 || stills[0] != shot || stills[1] != shot2 {
		t.Fatalf("stills-only %#v", stills)
	}

	preview := shared.DetailPreviewHandles(hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{
			VideoID:           video,
			ScreenshotIDs:     []string{shot, dupShot, "bad"},
			BackdropArtworkID: backdrop,
			CoverArtworkID:    strings.Repeat("ff", 32),
		},
	}, cover)
	if len(preview) != 3 || preview[0] != shot || preview[1] != backdrop || preview[2] != cover {
		t.Fatalf("video preview %#v", preview)
	}

	poster := shared.DetailPreviewHandles(hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{VideoID: video, CoverArtworkID: cover},
	}, "")
	if len(poster) != 1 || poster[0] != cover {
		t.Fatalf("poster %#v", poster)
	}

	empty := shared.DetailPreviewHandles(hostclient.Presentation{
		Presentation: &hostclient.PresentationInfo{VideoID: video},
	}, "")
	if empty != nil {
		t.Fatalf("video without stills %#v", empty)
	}
	if shared.VideoHandle(hostclient.Presentation{}) != "" {
		t.Fatal("empty presentation grew a video handle")
	}
}

func TestClientLibrarySettingsGetAndPatch(t *testing.T) {
	t.Parallel()
	var patches []string
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/settings" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 90,
				"preferred_regions":    []string{"usa", "japan"},
				"selected_target":      "dev",
				"targets": []map[string]any{
					{"name": "dev", "address": "http://192.0.2.10:8182", "enabled": true, "agent_configured": true, "agent": "secret"},
					{"name": "spare", "address": "", "enabled": false, "agent_configured": false},
				},
				"libraries": []map[string]any{{"id": "snes", "system": "snes", "root": "/library/snes"}},
				"systems":   []map[string]any{{"id": "snes", "label": "SNES"}},
			})
		case http.MethodPatch:
			raw, _ := io.ReadAll(r.Body)
			patches = append(patches, string(raw))
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("patch json: %v", err)
			}
			if _, ok := body["libraries"]; ok {
				t.Errorf("patch included libraries: %s", raw)
			}
			if _, ok := body["targets"]; ok {
				t.Errorf("patch included targets: %s", raw)
			}
			idle := 90
			regions := []any{"usa", "japan"}
			selected := "dev"
			if v, ok := body["attract_idle_seconds"].(float64); ok {
				idle = int(v)
			}
			if v, ok := body["preferred_regions"].([]any); ok {
				regions = v
			}
			if v, ok := body["selected_target"].(string); ok {
				selected = v
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": idle,
				"preferred_regions":    regions,
				"selected_target":      selected,
				"targets":              []map[string]any{{"name": "dev", "enabled": true, "agent_configured": true}},
				"libraries":            []map[string]any{},
				"systems":              []map[string]any{},
			})
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	got, err := client.LibrarySettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.AttractIdleSeconds != 90 || got.SelectedTarget != "dev" {
		t.Fatalf("settings = %#v", got)
	}
	if len(got.PreferredRegions) != 2 || got.PreferredRegions[0] != "usa" {
		t.Fatalf("regions = %#v", got.PreferredRegions)
	}
	if len(got.Targets) != 2 || got.Targets[0].Name != "dev" || !got.Targets[0].AgentConfigured || got.Targets[0].Address == "" {
		t.Fatalf("targets = %#v", got.Targets)
	}
	encoded, err := json.Marshal(got.Targets[0])
	if err != nil {
		t.Fatal(err)
	}
	var targetJSON map[string]any
	if err := json.Unmarshal(encoded, &targetJSON); err != nil {
		t.Fatal(err)
	}
	if _, ok := targetJSON["agent"]; ok {
		t.Fatalf("GET target kept secret field: %s", encoded)
	}
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("GET target echoed secret: %s", encoded)
	}
	if len(got.Libraries) != 1 || len(got.Systems) != 1 {
		t.Fatalf("context = libs %#v systems %#v", got.Libraries, got.Systems)
	}

	idle := 75
	patched, err := client.PatchLibrarySettings(context.Background(), hostclient.LibrarySettingsPatch{AttractIdleSeconds: &idle})
	if err != nil {
		t.Fatal(err)
	}
	if patched.AttractIdleSeconds != 75 {
		t.Fatalf("idle patch = %#v", patched)
	}
	regions := []string{"japan"}
	if _, err := client.PatchLibrarySettings(context.Background(), hostclient.LibrarySettingsPatch{PreferredRegions: &regions}); err != nil {
		t.Fatal(err)
	}
	target := "spare"
	if _, err := client.PatchLibrarySettings(context.Background(), hostclient.LibrarySettingsPatch{SelectedTarget: &target}); err != nil {
		t.Fatal(err)
	}
	if len(patches) != 3 {
		t.Fatalf("patches = %#v", patches)
	}
	if patches[0] != `{"attract_idle_seconds":75}` {
		t.Fatalf("idle body = %q", patches[0])
	}
	if patches[1] != `{"preferred_regions":["japan"]}` {
		t.Fatalf("regions body = %q", patches[1])
	}
	if patches[2] != `{"selected_target":"spare"}` {
		t.Fatalf("target body = %q", patches[2])
	}
	if strings.Join(methods, ",") != "GET,PATCH,PATCH,PATCH" {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestClientLibrarySettingsPatchEmptyRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewClient("http://127.0.0.1:1", nil).PatchLibrarySettings(context.Background(), hostclient.LibrarySettingsPatch{}); err == nil {
		t.Fatal("empty patch accepted")
	}
}

func TestClientLibrarySettingsPatchLibraries(t *testing.T) {
	t.Parallel()
	var patches []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/settings" || r.Method != http.MethodPatch {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		patches = append(patches, string(raw))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"attract_idle_seconds": 60,
			"preferred_regions":    []string{"usa"},
			"selected_target":      "dev",
			"targets":              []map[string]any{{"name": "dev"}},
			"libraries":            []map[string]any{{"id": "snes", "system": "snes", "root": "/library/snes"}},
			"systems":              []map[string]any{{"id": "snes", "label": "SNES"}},
		})
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	libraries := []hostclient.LibraryRoot{{ID: "snes", System: "snes", Root: "/library/snes"}}
	if _, err := client.PatchLibrarySettings(context.Background(), hostclient.LibrarySettingsPatch{Libraries: &libraries}); err != nil {
		t.Fatal(err)
	}
	empty := []hostclient.LibraryRoot{}
	if _, err := client.PatchLibrarySettings(context.Background(), hostclient.LibrarySettingsPatch{Libraries: &empty}); err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("patches = %#v", patches)
	}
	if patches[0] != `{"libraries":[{"id":"snes","system":"snes","root":"/library/snes"}]}` {
		t.Fatalf("full array = %q", patches[0])
	}
	if patches[1] != `{"libraries":[]}` {
		t.Fatalf("empty array = %q", patches[1])
	}
}

func TestClientLibrarySettingsPatchTargets(t *testing.T) {
	t.Parallel()
	var patches []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/settings" || r.Method != http.MethodPatch {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		patches = append(patches, string(raw))
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("patch json: %v", err)
		}
		if _, ok := body["libraries"]; ok {
			t.Errorf("targets patch included libraries: %s", raw)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"attract_idle_seconds": 60,
			"preferred_regions":    []string{"usa"},
			"selected_target":      "dev",
			"targets":              []map[string]any{{"name": "dev", "address": "http://192.0.2.10:8182", "enabled": true, "agent_configured": true}},
			"libraries":            []map[string]any{},
			"systems":              []map[string]any{},
		})
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	untouched := []hostclient.LibraryTargetWrite{{Name: "dev", Address: "http://192.0.2.10:8182", Enabled: true}}
	if _, err := client.PatchLibrarySettings(context.Background(), hostclient.LibrarySettingsPatch{Targets: &untouched}); err != nil {
		t.Fatal(err)
	}
	cleared := ""
	clear := []hostclient.LibraryTargetWrite{{Name: "dev", Address: "", Enabled: false, Agent: &cleared}}
	if _, err := client.PatchLibrarySettings(context.Background(), hostclient.LibrarySettingsPatch{Targets: &clear}); err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("patches = %#v", patches)
	}
	if patches[0] != `{"targets":[{"name":"dev","address":"http://192.0.2.10:8182","enabled":true}]}` {
		t.Fatalf("omit agent = %q", patches[0])
	}
	if patches[1] != `{"targets":[{"name":"dev","address":"","enabled":false,"agent":""}]}` {
		t.Fatalf("clear agent = %q", patches[1])
	}
}

func TestClientLibrarySettingsGetError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"SETTINGS_UNAVAILABLE","message":"library settings are unavailable"}}`)
	}))
	t.Cleanup(server.Close)
	if _, err := NewClient(server.URL, server.Client()).LibrarySettings(context.Background()); err == nil || !strings.Contains(err.Error(), "SETTINGS_UNAVAILABLE") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientPatchLibrarySettingsPreparesNamedTargetWithoutCredentials(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/library/settings" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != `{"prepare_target":"den"}` {
			t.Fatalf("body = %s", raw)
		}
		_, _ = io.WriteString(w, `{"selected_target":"den","targets":[{"name":"den","address":"mister.local:8182","enabled":true,"agent_configured":true,"target_id":"target-123"}]}`)
	}))
	defer server.Close()
	name := "den"
	settings, err := NewClient(server.URL, server.Client()).PatchLibrarySettings(t.Context(), hostclient.LibrarySettingsPatch{PrepareTarget: &name})
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Targets) != 1 || settings.Targets[0].TargetID != "target-123" {
		t.Fatalf("targets = %#v", settings.Targets)
	}
}

func TestClientWithAPIHostOverridesHostHeader(t *testing.T) {
	t.Parallel()
	var gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, server.Client()).withAPIHost("127.0.0.1:8787")
	if _, err := client.Session(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotHost != "127.0.0.1:8787" {
		t.Fatalf("Host = %q, want 127.0.0.1:8787", gotHost)
	}
}

func TestSmokeAPIHostRewritesNonLoopback(t *testing.T) {
	t.Parallel()
	if got := smokeAPIHost("http://host.docker.internal:8787", ""); got != "127.0.0.1:8787" {
		t.Fatalf("docker host = %q", got)
	}
	if got := smokeAPIHost("http://192.168.10.230:8787", ""); got != "127.0.0.1:8787" {
		t.Fatalf("lan host = %q", got)
	}
	if got := smokeAPIHost("http://127.0.0.1:8787", ""); got != "" {
		t.Fatalf("loopback rewrite = %q", got)
	}
	if got := smokeAPIHost("http://localhost:8787", ""); got != "" {
		t.Fatalf("localhost rewrite = %q", got)
	}
	if got := smokeAPIHost("http://host.docker.internal:8787", "localhost"); got != "localhost" {
		t.Fatalf("explicit = %q", got)
	}
}
