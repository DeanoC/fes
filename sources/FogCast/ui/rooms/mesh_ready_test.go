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
}
