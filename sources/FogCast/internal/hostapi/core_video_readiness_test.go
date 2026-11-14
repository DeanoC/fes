package hostapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestVideoReadinessInLibraryAndVariantResponses(t *testing.T) {
	for _, ready := range []bool{false, true} {
		game := catalog.Game{ID: "fpga-coleco", Title: "Coleco", System: protocol.System("coleco"), Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true, GroupKey: "coleco-group"}
		variant := game
		variant.ID = "fpga-coleco-variant"
		service := &expansionReadinessService{groupedFake: groupedFake{fakeService: fakeService{games: []catalog.Game{game}, game: game}, variants: []catalog.Game{variant}}, compositions: map[string]protocol.CoreComposition{
			game.ID: {VideoRequired: true, VideoReady: ready}, variant.ID: {VideoRequired: true, VideoReady: ready},
		}}
		check := func(g hostclient.Game, admission bool) {
			t.Helper()
			if !g.VideoRequired || g.VideoReady != ready {
				t.Fatalf("ready=%v projection=%+v", ready, g)
			}
			if admission && (g.LaunchEligible() != ready || (!ready && g.LaunchBlock() != hostclient.LaunchMissingVideo)) {
				t.Fatalf("ready=%v block=%s", ready, g.LaunchBlock())
			}
		}
		for _, path := range []string{"/api/v1/games", "/api/v1/games?grouped=1&availability=ready", "/api/v1/games/" + game.ID} {
			response := serve(t, hostapi.New(service), http.MethodGet, path)
			if response.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
			}
			if path != "/api/v1/games/"+game.ID {
				var list struct {
					Games []hostclient.Game `json:"games"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil || len(list.Games) != 1 {
					t.Fatalf("list=%+v err=%v", list, err)
				}
				check(list.Games[0], true)
			} else {
				var detail hostclient.Game
				if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil || len(detail.Variants) != 1 {
					t.Fatalf("detail=%+v err=%v", detail, err)
				}
				check(detail, true)
				check(detail.Variants[0], false)
			}
		}
	}
}
