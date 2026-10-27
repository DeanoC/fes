package hostapi_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/zx81tapes"
)

func TestDefaultZX81TapesImportWithoutSelectingOrLaunching(t *testing.T) {
	service := &coreMediaAPIService{}
	server := httptest.NewServer(hostapi.New(service))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	tapes, err := client.ZX81Tapes(context.Background())
	if err != nil || len(tapes) != len(zx81tapes.Entries()) || service.calls != 0 {
		t.Fatalf("read shelf: %+v %v calls=%d", tapes, err, service.calls)
	}
	for _, tape := range tapes {
		media, err := client.ImportZX81Tape(context.Background(), tape)
		if err != nil || media.MediaID != tape.SHA256 {
			t.Fatalf("import %s: %+v %v", tape.ID, media, err)
		}
	}
	if service.calls != len(tapes) || len(service.args) != 0 {
		t.Fatal("import changed library selection")
	}
	if response := mediaRequest(hostapi.New(service), "POST", "/api/v1/library/zx81-tapes/missing/import", "", ""); response.Code != 404 {
		t.Fatalf("unknown tape: %d", response.Code)
	}
}

type savedZX81Service struct {
	coreMediaAPIService
	saves   []catalog.ZX81Save
	coreIDs map[string]string
	err     error
}

func (s *savedZX81Service) CoreEntry(_ context.Context, gameID string) (catalog.CoreEntry, error) {
	coreID := s.coreIDs[gameID]
	if coreID == "" {
		coreID = "fes.zx81"
	}
	return catalog.CoreEntry{GameID: gameID, CoreID: coreID}, nil
}

func (s *savedZX81Service) ListZX81Saves(_ context.Context, gameID string) ([]catalog.ZX81Save, error) {
	var saves []catalog.ZX81Save
	for _, save := range s.saves {
		if save.GameID == gameID {
			saves = append(saves, save)
		}
	}
	return saves, s.err
}

func TestZX81TapeShelfScopesSavedProgramsToGame(t *testing.T) {
	mediaID := strings.Repeat("ab", 32)
	service := &savedZX81Service{saves: []catalog.ZX81Save{{
		GameID: "test-zx81", MediaID: mediaID, Name: "Saved program", Size: 917,
	}}}
	server := httptest.NewServer(hostapi.New(service))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	plain, err := client.ZX81Tapes(context.Background())
	if err != nil || len(plain) != len(zx81tapes.Entries()) || plain[0].MediaID != "" {
		t.Fatalf("starter shelf: %d %v", len(plain), err)
	}
	saved, err := client.ZX81TapesForGame(context.Background(), "test-zx81")
	if err != nil || len(saved) != len(plain)+1 {
		t.Fatalf("saved shelf: %d %v", len(saved), err)
	}
	otherGame, err := client.ZX81TapesForGame(context.Background(), "another-zx81")
	if err != nil || len(otherGame) != len(plain) {
		t.Fatalf("other game's shelf: %d %v", len(otherGame), err)
	}
	got := saved[len(saved)-1]
	if got.MediaID != mediaID || got.SHA256 != mediaID || got.Name != "Saved program" || got.RAMKB != 1 {
		t.Fatalf("saved tape: %+v", got)
	}
	if _, err := client.ZX81TapesForGame(context.Background(), "Bad Game"); err == nil {
		t.Fatal("invalid game was listed")
	}
}

func TestZX81TapeShelfOmitsStartersForZX80(t *testing.T) {
	service := &savedZX81Service{coreIDs: map[string]string{"my-zx80": "fes.zx80"}}
	server := httptest.NewServer(hostapi.New(service))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	tapes, err := client.ZX81TapesForGame(context.Background(), "my-zx80")
	if err != nil || len(tapes) != 0 {
		t.Fatalf("ZX80 starter shelf: %+v %v", tapes, err)
	}
}

func TestZX81TapeShelfKeepsTimexSavesWithoutStarters(t *testing.T) {
	mediaID := strings.Repeat("cd", 32)
	for _, coreID := range []string{"fes.ts1000", "fes.ts1500"} {
		t.Run(coreID, func(t *testing.T) {
			gameID := "my-" + strings.TrimPrefix(coreID, "fes.")
			service := &savedZX81Service{
				coreIDs: map[string]string{gameID: coreID},
				saves: []catalog.ZX81Save{{
					GameID: gameID, MediaID: mediaID, Name: "Saved program", Size: 917,
				}},
			}
			server := httptest.NewServer(hostapi.New(service))
			defer server.Close()
			client := hostclient.NewClient(server.URL, server.Client())
			tapes, err := client.ZX81TapesForGame(context.Background(), gameID)
			if err != nil || len(tapes) != 1 || tapes[0].MediaID != mediaID || tapes[0].Name != "Saved program" {
				t.Fatalf("%s shelf: %+v %v", coreID, tapes, err)
			}
		})
	}
}
