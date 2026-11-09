package hostapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type kitEntryService struct {
	fakeService
	packageID, gameID string
}

func (s *kitEntryService) KitHostEntry(_ context.Context, pkg, game string) (catalog.CoreEntry, error) {
	s.packageID, s.gameID = pkg, game
	return catalog.CoreEntry{GameID: "fpga-game", CoreID: "fes.sms", PackageID: pkg}, nil
}
func TestKitHostEntryAPIRejectsPathsAndPassesIdentities(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	service := &kitEntryService{}
	handler := hostapi.New(service)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"package_id":"","game_id":"sms-game"}`, http.StatusOK},
		{`{"package_id":"` + pkg + `","game_id":"sms-game"}`, http.StatusOK},
		{`{"package_id":"` + pkg + `","game_id":"../game"}`, http.StatusBadRequest},
		{`{"package_id":"` + pkg + `","game_id":"sms-game","path":"/tmp/game"}`, http.StatusBadRequest},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/library/local-core-entry", strings.NewReader(tc.body))
		r.Host = "127.0.0.1"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: status %d body %s", tc.body, w.Code, w.Body.String())
		}
	}
	if service.packageID != pkg || service.gameID != "sms-game" {
		t.Fatalf("resolved %q %q", service.packageID, service.gameID)
	}
}

type generatedEntryHTTPService struct {
	fakeService
	store    *catalog.Store
	launched string
}

func (s *generatedEntryHTTPService) Games(ctx context.Context) ([]catalog.Game, error) {
	return s.store.Games(ctx)
}
func (s *generatedEntryHTTPService) Game(ctx context.Context, id string) (catalog.Game, error) {
	return s.store.Game(ctx, id)
}
func (s *generatedEntryHTTPService) CoreEntries(ctx context.Context) ([]catalog.CoreEntry, error) {
	return s.store.CoreEntries(ctx)
}
func (s *generatedEntryHTTPService) CoreEntry(ctx context.Context, id string) (catalog.CoreEntry, error) {
	return s.store.CoreEntry(ctx, id)
}
func (s *generatedEntryHTTPService) Launch(ctx context.Context, id string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.launched = id
	return protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}}, nil
}
func (s *generatedEntryHTTPService) LaunchOn(ctx context.Context, id, target string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return s.Launch(ctx, id, progress)
}

func TestGeneratedKitEntryHiddenFromListButDetailAndLaunchWork(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pkg := strings.Repeat("a", 64)
	entry, err := store.CreateKitHostEntry(ctx, "Machine", "fes.pong", pkg, "", "", catalog.KitHostEntryIdentity{Identity: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	s := &generatedEntryHTTPService{store: store}
	s.status = protocol.Status{State: protocol.StateIdle}
	server := httptest.NewServer(hostapi.New(s))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	game, err := client.Game(ctx, entry.GameID)
	if err != nil || game.ID != entry.GameID || game.CoreID != entry.CoreID || game.PackageID != pkg {
		t.Fatalf("detail=%+v err=%v", game, err)
	}
	list := serve(t, hostapi.New(s), http.MethodGet, "/api/v1/games")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), entry.GameID) {
		t.Fatalf("list=%d %s", list.Code, list.Body.String())
	}
	_, err = client.Launch(ctx, entry.GameID)
	if err != nil || s.launched != entry.GameID {
		t.Fatalf("launch=%q err=%v", s.launched, err)
	}
}
