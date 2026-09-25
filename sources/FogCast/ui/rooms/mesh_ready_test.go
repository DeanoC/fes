package rooms

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/discovery"
)

func TestExecuteAdvertisementDoesNotFlipReadyForUnboundTitle(t *testing.T) {
	t.Parallel()
	frogger := readyGame("fpga-frogger", "Frogger", "fpga")
	frogger.FirmwareRequired = true
	state, matches := ClassifyGames([]hostclient.Game{frogger}, "Frogger")
	if state != AvailUnavailable || len(matches) != 1 || matches[0].LaunchBlock() != hostclient.LaunchMissingFirmware {
		t.Fatalf("unbound composition = %s %+v", state, matches)
	}
	foreign := discovery.ParseTXT(map[string]string{
		"protocol":  "1",
		"target_id": "01234567-89ab-cdef-0123-456789abcdef",
		"node_id":   "01234567-89ab-cdef-0123-456789abcdef",
		"mesh":      discovery.MeshProtocol,
		"cap":       "display_sink,execute:" + discovery.ExecuteFPGANative + ":fes.application/1",
		"hdmi":      "up",
	})
	if !foreign.Capabilities.DisplaySink || foreign.PictureUp() {
		t.Fatalf("display sink picture-up = %v", foreign.PictureUp())
	}
	if len(foreign.Capabilities.Execute) != 1 {
		t.Fatalf("execute = %#v", foreign.Capabilities.Execute)
	}
	if ready, _ := discovery.ReadyForBoundExecutor(state == AvailReady, foreign, nil); ready {
		t.Fatal("Execute advertisement made an unbound title Ready")
	}
	frogger.FirmwareReady = true
	state, matches = ClassifyGames([]hostclient.Game{frogger}, "Frogger")
	if state != AvailReady || len(matches) != 1 {
		t.Fatalf("bound composition = %s %+v", state, matches)
	}
	if ready, _ := discovery.ReadyForBoundExecutor(state == AvailReady, foreign, nil); !ready {
		t.Fatal("bound composition lost Ready")
	}
}

func TestReadyHereRoomsStayUnavailableUntilTheTitleIsHere(t *testing.T) {
	t.Parallel()
	coleco := readyGame("fpga-coleco-dk", "Donkey Kong", "coleco")
	coleco.FirmwareRequired = true
	coleco.FirmwareReady = true
	zx := readyGame("fpga-zx81-maze", "3D Monster Maze", "zx81")
	hostOnly := readyGame("snes-mario", "Super Mario World", "snes")
	hostOnly.Execution = hostclient.ExecutionHostOnly
	for _, game := range []hostclient.Game{coleco, zx, hostOnly} {
		state, matches := ClassifyGames([]hostclient.Game{game}, game.Title)
		dest := Destination{Kind: KindGame, Availability: state, Matches: matches, GameID: game.ID, Label: game.Title}
		dest.FillCopy()
		if state != AvailReady || dest.Confirm() != ConfirmLaunch || dest.Action != "Play" {
			t.Fatalf("seam off %s %+v confirm %v", game.ID, dest, dest.Confirm())
		}
	}

	cases := []struct {
		block   hostclient.LaunchBlock
		action  string
		state   Availability
		confirm ConfirmIntent
		status  string
	}{
		{hostclient.LaunchDistant, "fetch_here", AvailUnavailable, ConfirmExplain, "This title is not on this executor."},
		{hostclient.LaunchLeaseHeld, "wait_for_lease", AvailUnavailable, ConfirmExplain, "This executor is in use."},
		{hostclient.LaunchVersionSkew, "resolve_version", AvailUnavailable, ConfirmExplain, "Can't play here yet."},
		{hostclient.LaunchNoExecutor, "bind_executor", AvailUnavailable, ConfirmExplain, "This title cannot play on the current setup."},
		{hostclient.LaunchContentMissing, "supply_content", AvailUnavailable, ConfirmExplain, "A required part of this title is missing."},
		{hostclient.LaunchEnsureProgress, "wait", AvailChecking, ConfirmWait, "Still resolving whether this title can play here."},
	}
	for _, tc := range cases {
		game := coleco
		ready := false
		game.ReadyHere = &ready
		game.ReadyBlock = string(tc.block)
		game.NextAction = tc.action
		state, matches := ClassifyGames([]hostclient.Game{game}, game.Title)
		dest := Destination{Kind: KindGame, Availability: state, Matches: matches, Query: game.Title}
		dest.FillCopy()
		if state != tc.state || dest.Confirm() != tc.confirm || dest.NextAction != tc.action || dest.Status != tc.status || dest.Confirm() == ConfirmLaunch {
			t.Fatalf("%s state=%s confirm=%v dest=%+v", tc.block, state, dest.Confirm(), dest)
		}
	}

	ready := true
	coleco.ReadyHere = &ready
	state, matches := ClassifyGames([]hostclient.Game{coleco}, coleco.Title)
	dest := Destination{Kind: KindGame, Availability: state, Matches: matches, GameID: coleco.ID}
	dest.FillCopy()
	if state != AvailReady || dest.Confirm() != ConfirmLaunch || dest.Action != "Play" {
		t.Fatalf("ready here %+v", dest)
	}

	offline := hostOnly
	offline.ReadyHere = &ready
	offline.State = "missing"
	offline.RootOnline = false
	state, matches = ClassifyGames([]hostclient.Game{offline}, offline.Title)
	dest = Destination{Kind: KindGame, Availability: state, Matches: matches, GameID: offline.ID}
	dest.FillCopy()
	if state != AvailUnavailable || dest.Confirm() == ConfirmLaunch || matches[0].LaunchBlock() != hostclient.LaunchSourceOffline {
		t.Fatalf("offline host row %+v confirm %v", dest, dest.Confirm())
	}
}

func TestPlacementSelectionStaysQuietAndBlockedRowsDoNotLaunch(t *testing.T) {
	t.Parallel()
	selected := placementReadyRow(true, hostclient.PlacementSelected, "")
	dest := placementDestination(selected)
	if dest.Availability != AvailReady || dest.Confirm() != ConfirmLaunch || dest.Action != "Play" || dest.Confirm() == ConfirmChoose || dest.Availability == AvailNeedsChoice {
		t.Fatalf("selected %+v confirm %v", dest, dest.Confirm())
	}
	if dest.GameID != selected.ID {
		t.Fatalf("selected game %q", dest.GameID)
	}

	unresolved := placementReadyRow(false, hostclient.PlacementUnresolved, hostclient.LaunchPlacementUnresolved)
	dest = placementDestination(unresolved)
	assertPlacementNotPlayable(t, dest, "unresolved")

	closed := placementReadyRow(false, hostclient.PlacementFailClosed, hostclient.LaunchPlacementFailClosed)
	dest = placementDestination(closed)
	assertPlacementNotPlayable(t, dest, "fail closed")
	if dest.Matches[0].LaunchBlock() == unresolved.LaunchBlock() {
		t.Fatalf("fail closed collapsed onto unresolved %s", dest.Matches[0].LaunchBlock())
	}

	usa := placementReadyRow(false, hostclient.PlacementUnresolved, hostclient.LaunchPlacementUnresolved)
	usa.ID = "nes-smb-usa"
	usa.Title = "Super Mario Bros."
	jp := usa
	jp.ID = "nes-smb-jp"
	state, matches := ClassifyGames([]hostclient.Game{usa, jp}, "Super Mario Bros.")
	choice := Destination{Kind: KindGame, Availability: state, Matches: matches, Query: "Super Mario Bros.", Label: "Super Mario Bros."}
	choice.FillCopy()
	if state != AvailNeedsChoice || choice.Confirm() != ConfirmChoose || choice.Status != "Several editions match. Choose one." {
		t.Fatalf("editions %+v confirm %v", choice, choice.Confirm())
	}
	applied := ApplyEditionPreference(choice, usa.ID)
	assertPlacementNotPlayable(t, applied, "chosen edition")

	skew := placementReadyRow(false, hostclient.PlacementFailClosed, hostclient.LaunchVersionSkew)
	skew.NextAction = "resolve_version"
	dest = placementDestination(skew)
	if dest.Availability != AvailUnavailable || dest.Confirm() != ConfirmExplain || dest.Status != "Can't play here yet." || dest.Confirm() == ConfirmLaunch {
		t.Fatalf("skew %+v confirm %v", dest, dest.Confirm())
	}

	busy := placementReadyRow(false, hostclient.PlacementUnresolved, hostclient.LaunchLeaseHeld)
	busy.NextAction = "wait_for_lease"
	dest = placementDestination(busy)
	if dest.Availability != AvailUnavailable || dest.Confirm() != ConfirmExplain || dest.Status != "This executor is in use." || dest.Action != "Do not take the lease." || dest.Confirm() == ConfirmLaunch {
		t.Fatalf("in use %+v confirm %v", dest, dest.Confirm())
	}
}

func TestPlacementHostGameReachesConfirm(t *testing.T) {
	ready := true
	selected := hostclient.Game{
		ID: "fpga-coleco-dk", Title: "Donkey Kong", System: "coleco",
		State: "available", RootOnline: true, Launchable: true,
		ReadyHere: &ready, Placement: hostclient.PlacementSelected,
	}
	svc := &fakeServices{games: []hostclient.Game{selected}}
	src := `
seen = nil
function load()
  library.query({ q = "Donkey Kong" }, function(games, err)
    seen = games
    destination.set{ kind = "game", label = "Donkey Kong", query = "Donkey Kong", matches = games }
  end)
end
function draw() end`
	r := newRoom(t, memPack(t, "place", src, nil), Options{Services: svc})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.Destination().Availability == AvailReady })
	if err := r.CheckGlobal(`seen[1].placement == "selected" and seen[1].launchable == true`); err != nil {
		t.Fatal(err)
	}
	d := r.Destination()
	if d.Confirm() != ConfirmLaunch || d.Action != "Play" || d.Availability == AvailNeedsChoice {
		t.Fatalf("selected room %+v confirm %v", d, d.Confirm())
	}

	blocked := false
	selected.ReadyHere = &blocked
	selected.Placement = hostclient.PlacementUnresolved
	selected.ReadyBlock = string(hostclient.LaunchPlacementUnresolved)
	selected.NextAction = "unavailable"
	r.RefreshCachedGames([]hostclient.Game{selected})
	got := r.Destination()
	assertPlacementNotPlayable(t, got, "refreshed")
	if got.Matches[0].LaunchBlock() != hostclient.LaunchPlacementUnresolved {
		t.Fatalf("block %s", got.Matches[0].LaunchBlock())
	}
}

func placementReadyRow(ready bool, placement string, block hostclient.LaunchBlock) hostclient.Game {
	g := readyGame("fpga-coleco-dk", "Donkey Kong", "coleco")
	g.FirmwareRequired = true
	g.FirmwareReady = true
	g.ReadyHere = &ready
	g.Placement = placement
	if !ready {
		g.ReadyBlock = string(block)
		g.NextAction = "unavailable"
	}
	return g
}

func placementDestination(g hostclient.Game) Destination {
	state, matches := ClassifyGames([]hostclient.Game{g}, g.Title)
	d := Destination{Kind: KindGame, Availability: state, Matches: matches, GameID: g.ID, Label: g.Title, Query: g.Title}
	d.FillCopy()
	return d
}

func assertPlacementNotPlayable(t *testing.T, dest Destination, name string) {
	t.Helper()
	if dest.Availability == AvailReady || dest.Availability == AvailNeedsChoice || dest.Confirm() == ConfirmLaunch || dest.Confirm() == ConfirmChoose {
		t.Fatalf("%s %+v confirm %v", name, dest, dest.Confirm())
	}
	if dest.Status == "Can't play here yet." || dest.Status == "This executor is in use." || dest.Status == "Several editions match. Choose one." {
		t.Fatalf("%s copy %q", name, dest.Status)
	}
	if dest.Confirm() != ConfirmExplain {
		t.Fatalf("%s confirm %v", name, dest.Confirm())
	}
}
