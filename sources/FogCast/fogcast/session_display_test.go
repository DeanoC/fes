package fogcast

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type displayServiceClient struct {
	fakeServiceClient
	calls int
	err   error
}

func (c *displayServiceClient) SetSessionDisplay(_ context.Context, _ protocol.SessionDisplay, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	c.calls++
	return c.statusResult, c.err
}

func TestDisplayAndLiveIdentityStayOnCapturedKitWhenForegroundChanges(t *testing.T) {
	id := strings.Repeat("a", 64)
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 9,
		ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}}
	a := &displayServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status}}
	bClient := &displayServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status}}
	s := &Service{uploadTimeout: time.Second, selectedTarget: "kit-b", activeTarget: "kit-b", activeExecution: ExecutionHostOnly,
		targets:       []TargetConfig{{Name: "kit-a", TargetID: "target-a", Enabled: true}, {Name: "kit-b", TargetID: "target-b", Enabled: true}},
		targetClients: map[string]serviceClient{"kit-a": a, "kit-b": bClient},
		plays:         map[string]targetPlay{"kit-a": {execution: ExecutionFPGADevelopment, gameID: "rom-zx81", system: "zx81", packageID: id, packageGeneration: 9}}}
	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: "kit-a", TargetID: "target-a"}
	for _, display := range []protocol.SessionDisplay{protocol.SessionDisplayFocused, protocol.SessionDisplayClosed} {
		got, err := s.SetSessionDisplay(context.Background(), display, b)
		if err != nil || got.GameID == nil || *got.GameID != "rom-zx81" || !b.MatchesSessionDisplay(got) {
			t.Fatalf("display=%+v err=%v status=%+v", display, err, got)
		}
	}
	if _, err := s.SetSessionDisplay(context.Background(), protocol.SessionDisplay{Focus: protocol.SessionFocusLauncher}, b); err == nil {
		t.Fatal("closed display with launcher focus reached the kit")
	}
	if a.calls != 2 || bClient.calls != 0 || s.activeTarget != "kit-b" {
		t.Fatal("display followed or replaced foreground")
	}
	b.Generation++
	if _, err := s.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b); err == nil || a.calls != 2 {
		t.Fatal("stale core generation reached physical client")
	}
	b.Generation--
	a.err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "display"}
	if _, err := s.SetSessionDisplay(context.Background(), protocol.SessionDisplayClosed, b); err == nil || s.plays["kit-a"].packageGeneration != 9 {
		t.Fatal("display failure retired play")
	}
}

func TestDisplayServiceAdmitsObservedDiskAndSpectrumOnCapturedKit(t *testing.T) {
	for _, tc := range []struct {
		contract           protocol.RuntimeContract
		minBytes, maxBytes uint32
	}{
		{protocol.C64DiskInterface(), 174848, 174848}, {protocol.SpectrumTapeInterface(), uint32(protocol.SpectrumTapeMinBytes), uint32(protocol.SpectrumTapeMaxBytes)},
	} {
		t.Run(tc.contract.ID, func(t *testing.T) {
			id := strings.Repeat("a", 64)
			status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
				PackageID: id, Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
				ActiveInterfaces: []protocol.RuntimeInterface{{ID: tc.contract.ID, Major: tc.contract.Major, Minor: tc.contract.Minor}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}},
				MediaUnits:       []protocol.MediaUnitStatus{{Unit: 0, Interface: tc.contract, MinBytes: tc.minBytes, MaxBytes: tc.maxBytes, ChunkBytes: 512, State: "empty"}},
			}}
			target := &displayServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status}}
			s := &Service{uploadTimeout: time.Second, selectedTarget: "kit-a", activeTarget: "kit-a",
				targets: []TargetConfig{{Name: "kit-a", TargetID: "target-a", Enabled: true}}, targetClients: map[string]serviceClient{"kit-a": target},
				plays: map[string]targetPlay{"kit-a": {execution: ExecutionFPGANative, gameID: "computer", system: "c64", packageID: id, packageGeneration: 9}}}
			b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: "kit-a", TargetID: "target-a"}
			for _, display := range []protocol.SessionDisplay{protocol.SessionDisplayFocused, protocol.SessionDisplayClosed} {
				if _, err := s.SetSessionDisplay(context.Background(), display, b); err != nil {
					t.Fatal(err)
				}
			}
			if target.calls != 2 {
				t.Fatalf("display did not reach target: %d", target.calls)
			}
			b.Generation++
			if _, err := s.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b); err == nil || target.calls != 2 {
				t.Fatal("stale display reached target")
			}
		})
	}
}
