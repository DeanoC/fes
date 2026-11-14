package rooms

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestMissingVideoPartExplainsBlockedPlay(t *testing.T) {
	game := hostclient.Game{ID: "fpga-coleco", State: "available", RootOnline: true, Launchable: true, VideoRequired: true}
	if game.LaunchEligible() || LaunchBlockCopy(game) != "Needs a matching video part" {
		t.Fatalf("video block=%q copy=%q", game.LaunchBlock(), LaunchBlockCopy(game))
	}
}

func TestVideoReadinessFlowsFromQueryToDestinationAndRefresh(t *testing.T) {
	svc := &fakeServices{games: []hostclient.Game{{
		ID: "fpga-coleco-cart", Title: "Coleco cartridge", System: "coleco",
		State: "available", RootOnline: true, Launchable: true,
		FirmwareRequired: true, FirmwareReady: true, ROMRequired: true, ROMReady: true,
		VideoRequired: true,
	}}}
	src := `
result = nil
function load()
  library.query({ q = "Coleco cartridge" }, function(games, err)
    result = games[1]
    destination.set{ kind = "game", label = "Coleco cartridge", query = "Coleco cartridge", matches = games }
  end)
end
function draw() end`
	r := newRoom(t, memPack(t, "video-readiness", src, nil), Options{Services: svc})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.Destination().Availability == AvailUnavailable })
	blocked := r.Destination()
	if blocked.Confirm() == ConfirmLaunch || blocked.Status != "Needs a matching video part" {
		t.Fatalf("missing video destination %+v, confirm=%v", blocked, blocked.Confirm())
	}
	if err := r.CheckGlobal("result.video_required == true and result.video_ready == false and result.launchable == false and result.launch_block == 'missing_video'"); err != nil {
		t.Fatal(err)
	}
	ready := svc.games[0]
	ready.VideoReady = true
	r.RefreshCachedGames([]hostclient.Game{ready})
	updated := r.Destination()
	if updated.Availability != AvailReady || updated.Confirm() != ConfirmLaunch || updated.Action != "Play" {
		t.Fatalf("ready video destination %+v, confirm=%v", updated, updated.Confirm())
	}
}

func TestVideoReadinessSurvivesUncachedLuaGame(t *testing.T) {
	src := `
result = nil
function load()
  result = destination.classify({{ id = "synthetic", title = "Synthetic", system = "coleco", state = "available", launchable = true, video_required = true, video_ready = false }})
end
function draw() end`
	r := newRoom(t, memPack(t, "video-uncached", src, nil), Options{Services: &fakeServices{}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal("result.state == 'unavailable' and result.status == 'Needs a matching video part'"); err != nil {
		t.Fatal(err)
	}
}

func TestEmittedBlockedRowsKeepReasonAfterCacheMiss(t *testing.T) {
	r := newRoom(t, memPack(t, "video-roundtrip", "function load() end\nfunction draw() end", nil), Options{Services: &fakeServices{}})
	for _, tc := range []struct {
		name string
		game hostclient.Game
		want hostclient.LaunchBlock
	}{
		{"missing video", hostclient.Game{ID: "video", Title: "Video", State: "available", RootOnline: true, Launchable: true, VideoRequired: true}, hostclient.LaunchMissingVideo},
		{"missing package", hostclient.Game{ID: "package", Title: "Package", State: "available", RootOnline: true, Launchable: true, PackageUnavailable: true}, hostclient.LaunchMissingPackage},
		{"offline", hostclient.Game{ID: "offline", Title: "Offline", State: "available", Launchable: true}, hostclient.LaunchSourceOffline},
		{"missing expansion", hostclient.Game{ID: "expansion", Title: "Expansion", State: "available", RootOnline: true, Launchable: true, ExpansionID: "card"}, hostclient.LaunchMissingExpansion},
		{"missing slot", hostclient.Game{ID: "slot", Title: "Slot", State: "available", RootOnline: true, Launchable: true, SlotExpansions: []hostclient.SlotExpansion{{Slot: 1, ExpansionID: "card"}}}, hostclient.LaunchMissingExpansion},
		{"missing firmware", hostclient.Game{ID: "firmware", Title: "Firmware", State: "available", RootOnline: true, Launchable: true, FirmwareRequired: true}, hostclient.LaunchMissingFirmware},
		{"missing ROM", hostclient.Game{ID: "rom", Title: "ROM", State: "available", RootOnline: true, Launchable: true, ROMRequired: true}, hostclient.LaunchMissingROM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := r.gameTable(tc.game)
			if optBool(row, "launchable") || !optBool(row, "catalog_launchable") {
				t.Fatalf("incorrect emitted gates: %v", row)
			}
			delete(r.games, tc.game.ID)
			list := r.L.NewTable()
			list.Append(row)
			games := r.gamesFromLua(list)
			if len(games) != 1 || games[0].LaunchBlock() != tc.want {
				t.Fatalf("reconstructed games=%+v, want block %s", games, tc.want)
			}
		})
	}
}

func TestLegacyLuaLaunchableWithoutCatalogGateIsUnchanged(t *testing.T) {
	src := `
result = nil
function load()
  result = destination.classify({{ id = "legacy", title = "Legacy", system = "coleco", state = "available", launchable = false }})
end
function draw() end`
	r := newRoom(t, memPack(t, "video-legacy", src, nil), Options{Services: &fakeServices{}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal("result.state == 'unavailable' and result.status == 'This platform is browse-only on this host.'"); err != nil {
		t.Fatal(err)
	}
}
