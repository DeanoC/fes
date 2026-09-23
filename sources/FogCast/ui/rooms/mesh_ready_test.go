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
	if discovery.ReadyForBoundExecutor(state == AvailReady, foreign) {
		t.Fatal("Execute advertisement made an unbound title Ready")
	}
	frogger.FirmwareReady = true
	state, matches = ClassifyGames([]hostclient.Game{frogger}, "Frogger")
	if state != AvailReady || len(matches) != 1 {
		t.Fatalf("bound composition = %s %+v", state, matches)
	}
	if !discovery.ReadyForBoundExecutor(state == AvailReady, foreign) {
		t.Fatal("bound composition lost Ready")
	}
}
