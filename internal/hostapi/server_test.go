package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/internal/hostapi"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type fakeService struct {
	games     []catalog.Game
	search    []catalog.Game
	query     string
	game      catalog.Game
	gamesErr  error
	gameErr   error
	health    protocol.Health
	healthErr error
	status    protocol.Status
	statusErr error
}

func (s *fakeService) Games(context.Context) ([]catalog.Game, error) {
	return append([]catalog.Game(nil), s.games...), s.gamesErr
}
func (s *fakeService) Search(_ context.Context, query string) ([]catalog.Game, error) {
	s.query = query
	return append([]catalog.Game(nil), s.search...), s.gamesErr
}
func (s *fakeService) Game(context.Context, string) (catalog.Game, error) { return s.game, s.gameErr }
func (s *fakeService) Health(context.Context) (protocol.Health, error)    { return s.health, s.healthErr }
func (s *fakeService) Status(context.Context) (protocol.Status, error)    { return s.status, s.statusErr }

func TestGamesReturnsStablePublicCatalogWithoutPrivatePathsOrDigests(t *testing.T) {
	service := &fakeService{games: []catalog.Game{{
		ID: "megadrive-sonic-test", Title: "Sonic", LibraryID: "private-root", RelativePath: "secret/Sonic.zip",
		System: protocol.SystemMegaDrive, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true,
		Content: &catalog.Content{SHA256: strings.Repeat("a", 64), Size: 123, Extension: "md"},
	}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, private := range []string{"private-root", "secret/", strings.Repeat("a", 64), "relative_path", "sha256"} {
		if strings.Contains(body, private) {
			t.Fatalf("response leaked %q: %s", private, body)
		}
	}
	var result struct {
		Games []struct {
			ID              string              `json:"id"`
			Title           string              `json:"title"`
			System          protocol.System     `json:"system"`
			State           catalog.SourceState `json:"state"`
			RootOnline      bool                `json:"root_online"`
			ContentPrepared bool                `json:"content_prepared"`
		} `json:"games"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Games) != 1 || result.Games[0].ID != "megadrive-sonic-test" || !result.Games[0].ContentPrepared {
		t.Fatalf("games = %#v", result.Games)
	}
	assertJSONHeaders(t, response)
}

func TestGamesSupportsSearchAndExecutionCapability(t *testing.T) {
	service := &fakeService{search: []catalog.Game{{ID: "snes-mario", Title: "Mario", System: protocol.SystemSNES, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games?q=mario")
	if response.Code != http.StatusOK || service.query != "mario" {
		t.Fatalf("status=%d query=%q body=%s", response.Code, service.query, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"execution":"fpga_native"`) {
		t.Fatalf("execution capability missing: %s", response.Body.String())
	}
}

func TestGameDetailUsesPathIDAndReturnsNotFound(t *testing.T) {
	service := &fakeService{gameErr: &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "not found"}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games/missing-game")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "GAME_NOT_FOUND" {
		t.Fatalf("error = %#v", envelope.Error)
	}
}

func TestGameDetailDoesNotMisreportCatalogFailureAsMissing(t *testing.T) {
	service := &fakeService{gameErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "private failure"}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games/known-game")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private failure") {
		t.Fatalf("private service error leaked: %s", response.Body.String())
	}
}

func TestHealthSeparatesHostReadinessFromTargetReadiness(t *testing.T) {
	service := &fakeService{health: protocol.Health{Ready: false}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/health")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Ready  bool `json:"ready"`
		Target struct {
			Ready bool `json:"ready"`
		} `json:"target"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Ready || result.Target.Ready {
		t.Fatalf("health = %#v", result)
	}
}

func TestStatusReturnsPublicControlState(t *testing.T) {
	gameID, core := "megadrive-sonic-test", "MegaDrive"
	service := &fakeService{status: protocol.Status{
		State: protocol.StateActive, GameID: &gameID, ExpectedCore: &core, ObservedCore: &core,
	}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/status")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"state":"active"`) || !strings.Contains(body, `"game_id":"megadrive-sonic-test"`) || !strings.Contains(body, `"core":"MegaDrive"`) {
		t.Fatalf("status body = %s", body)
	}
	if strings.Contains(body, "expected_core") || strings.Contains(body, "observed_core") {
		t.Fatalf("target implementation leaked: %s", body)
	}
}

func TestStatusRedactsTargetControlledErrorMessage(t *testing.T) {
	message := "/private/path Bearer secret-token"
	service := &fakeService{status: protocol.Status{
		State:     protocol.StateIdle,
		LastError: &protocol.APIError{Code: protocol.CodeInternal, Message: message},
	}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/status")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, message) || !strings.Contains(body, "FogCast operation failed internally") {
		t.Fatalf("status leaked or failed to canonicalize target error: %s", body)
	}
}

func TestGameDetailMapsInternalFailureToJSON500(t *testing.T) {
	service := &fakeService{gameErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "private failure"}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games/known-game")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private failure") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestRoutesRejectWrongMethodsAndMalformedGamePaths(t *testing.T) {
	handler := hostapi.New(&fakeService{})
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/v1/games", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/games/a/b", http.StatusNotFound},
		{http.MethodGet, "/api/v1/unknown", http.StatusNotFound},
	} {
		response := serve(t, handler, test.method, test.path)
		if response.Code != test.want {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.want)
		}
	}
}

func TestHostAPIRejectsUnexpectedBrowserHost(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/games", nil)
	request.Host = "evil.example"
	response := httptest.NewRecorder()
	hostapi.New(&fakeService{}).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func serve(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertJSONHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q", got)
	}
}
