package fogcast

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestPairedStatusKeepsCoreLaunchIdentityBeforePublicPoll(t *testing.T) {
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Live computer")
	active := coreEntryActiveStatus(inspection, 7, true)
	client.mediaStatus = active
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}
	s.pairedTargetConfigs = []TargetConfig{{Name: "dev", TargetID: "paired-dev", Enabled: true}}
	s.pairedTargetClients["dev"] = client
	response, err := s.Launch(context.Background(), entry.GameID, nil)
	if err != nil || response.Status.GameID == nil || *response.Status.GameID != entry.GameID {
		t.Fatalf("launch identity: status=%+v err=%v", response.Status, err)
	}
	if client.statusResult.GameID != nil || client.statusResult.System != nil {
		t.Fatal("target fixture already supplied host library identity")
	}
	// The first read after launch is the paired kit read, with no ordinary
	// browser/status poll available to populate the retained play record.
	status, err := s.Status(WithPairedTarget(context.Background(), "paired-dev"))
	if err != nil || status.GameID == nil || *status.GameID != entry.GameID || status.System == nil || *status.System != catalog.CorePlatform {
		t.Fatalf("paired read lost freshly launched identity: status=%+v err=%v", status, err)
	}
	if status.CorePackage.PackageID != response.Status.CorePackage.PackageID || status.CorePackage.Generation != response.Status.CorePackage.Generation {
		t.Fatal("paired read replaced the observed core binding")
	}
}

func TestPairedStatusEnrichesOnlyMatchingObservedCore(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	for _, foreground := range []bool{true, false} {
		for _, change := range []string{"matching", "package", "generation", "missing", "idle", "legacy"} {
			name := "retained/" + change
			if foreground {
				name = "foreground/" + change
			}
			t.Run(name, func(t *testing.T) {
				status := protocol.Status{State: protocol.StateActive, Development: true,
					CorePackage: &protocol.CorePackageStatus{PackageID: pkg, Generation: 9}}
				play := targetPlay{execution: ExecutionFPGANative, gameID: "kit-a-game", system: catalog.CorePlatform, packageID: pkg, packageGeneration: 9}
				switch change {
				case "package":
					status.CorePackage.PackageID = strings.Repeat("c", 64)
				case "generation":
					status.CorePackage.Generation++
				case "missing":
					status.CorePackage = nil
				case "idle":
					status.State = protocol.StateIdle
				case "legacy":
					status.CorePackage = nil
					play.packageID, play.packageGeneration = "", 0
				}
				client := &fakeServiceClient{statusResult: status}
				s := &Service{requestTimeout: time.Second,
					pairedTargetConfigs: []TargetConfig{{Name: "kit-a", TargetID: "a", Enabled: true}},
					pairedTargetClients: map[string]serviceClient{"kit-a": client},
					activeTarget:        "kit-b", activeExecution: ExecutionFPGANative, activeGameID: "kit-b-game",
					activeSystem: catalog.CorePlatform, activePackageID: strings.Repeat("b", 64), activePackageGeneration: 4,
					plays: map[string]targetPlay{"kit-a": play}}
				if foreground {
					s.activeTarget, s.activeGameID, s.activeSystem = "kit-a", play.gameID, play.system
					s.activePackageID, s.activePackageGeneration = play.packageID, play.packageGeneration
					// A stale retained entry cannot override the current fields,
					// even when it describes the newly observed replacement.
					stale := targetPlay{gameID: "stale-game", system: protocol.SystemNES}
					if status.CorePackage != nil {
						stale.packageID, stale.packageGeneration = status.CorePackage.PackageID, status.CorePackage.Generation
					}
					s.plays["kit-a"] = stale
				}
				got, err := s.Status(WithPairedTarget(context.Background(), "a"))
				if err != nil {
					t.Fatal(err)
				}
				if change == "matching" || change == "legacy" {
					if got.GameID == nil || *got.GameID != "kit-a-game" || got.System == nil || *got.System != catalog.CorePlatform {
						t.Fatalf("matching kit identity missing: %+v", got)
					}
				} else if got.GameID != nil || got.System != nil {
					t.Fatalf("stale identity attached to changed core: %+v", got)
				}
				if !foreground && (s.activeTarget != "kit-b" || s.activeGameID != "kit-b-game") {
					t.Fatal("paired read changed another kit's foreground identity")
				}
			})
		}
	}
}
