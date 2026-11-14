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
