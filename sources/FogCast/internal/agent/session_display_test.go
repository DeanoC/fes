package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/protocol"
)

type displayRuntime struct {
	fakeRuntime
	calls int
}

func (r *displayRuntime) SetSessionDisplay(context.Context, protocol.SessionDisplay, protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.calls++
	return nil
}

func TestSessionDisplayCoordinatorAdmitsObservedComputerMedia(t *testing.T) {
	for _, tc := range []struct {
		contract           protocol.RuntimeContract
		minBytes, maxBytes uint32
	}{
		{protocol.C64DiskInterface(), 174848, 174848},
		{protocol.SpectrumTapeInterface(), uint32(protocol.SpectrumTapeMinBytes), uint32(protocol.SpectrumTapeMaxBytes)},
	} {
		t.Run(tc.contract.ID, func(t *testing.T) {
			b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}
			status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
				PackageID: b.PackageID, Generation: b.Generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
				ActiveInterfaces: []protocol.RuntimeInterface{{ID: tc.contract.ID, Major: tc.contract.Major, Minor: tc.contract.Minor}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}},
				MediaUnits:       []protocol.MediaUnitStatus{{Unit: 0, Interface: tc.contract, MinBytes: tc.minBytes, MaxBytes: tc.maxBytes, ChunkBytes: 512, State: "empty"}},
			}}
			runtime := &displayRuntime{fakeRuntime: fakeRuntime{reconciled: status}}
			coordinator := agent.New(runtime, time.Second, time.Second)
			coordinator.Initialize(context.Background())
			for _, display := range []protocol.SessionDisplay{protocol.SessionDisplayFocused, protocol.SessionDisplayClosed} {
				if _, err := coordinator.SetSessionDisplay(context.Background(), display, b); err != nil {
					t.Fatal(err)
				}
			}
			if runtime.calls != 2 {
				t.Fatalf("display did not reach runtime: %d", runtime.calls)
			}
			b.Generation++
			if _, err := coordinator.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b); err == nil || runtime.calls != 2 {
				t.Fatal("stale display reached runtime")
			}
		})
	}
}
