package fogcast

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

// leftKitReleaseClient is the kit a placement rebind leaves. It embeds
// the fixture's selected client so launch still uses that client's
// health, status, and core load. ReleaseKitGrant and Stop are counted
// here; Stop is not the embedded client's counter, because the success
// fixture shares that client with the newly placed kit.
type leftKitReleaseClient struct {
	serviceClient
	grantReleases int
	stops         int
	notHeld       bool
	grantErr      error
	token         string
	lease         *targetclient.KitLease
	forceOwned    bool
	generation    string
}

func (c *leftKitReleaseClient) ReleaseKitGrant(context.Context) (bool, error) {
	if c == nil {
		return false, errors.New("nil left kit")
	}
	c.grantReleases++
	return c.notHeld, c.grantErr
}

func (c *leftKitReleaseClient) Stop(ctx context.Context) (protocol.Status, error) {
	if c == nil || c.serviceClient == nil {
		return protocol.Status{}, errors.New("nil left kit")
	}
	c.stops++
	return c.serviceClient.Stop(ctx)
}

func (c *leftKitReleaseClient) MeshKitLease() (bool, string) {
	if c == nil {
		return false, ""
	}
	if c.forceOwned {
		generation := c.generation
		if generation == "" {
			generation = "gen-old"
		}
		return true, generation
	}
	if lease, ok := c.serviceClient.(meshKitLease); ok && lease != nil {
		return lease.MeshKitLease()
	}
	return false, ""
}

func (c *leftKitReleaseClient) KitLease() *targetclient.KitLease {
	if c == nil {
		return nil
	}
	return c.lease
}

func capturePlacementLeaseLog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	previous := placementLeaseLogf
	placementLeaseLogf = func(format string, args ...any) {
		fmt.Fprintf(&buf, format, args...)
		if !strings.HasSuffix(format, "\n") {
			buf.WriteByte('\n')
		}
	}
	t.Cleanup(func() { placementLeaseLogf = previous })
	return &buf
}

func prepareRebindLeftKit(t *testing.T, old *leftKitReleaseClient) (*Service, *placementKeepClient, string) {
	t.Helper()
	service, client, entry, inspection := newCoreEntryLaunchFixture(t, libraryPackageFixture(t, "0.1.0"), "Standalone Pong")
	coreID := inspection.Descriptor.Core.ID
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &coreID, CorePackage: &protocol.CorePackageStatus{
		PackageID: inspection.PackageID, Generation: 4,
		ABI: protocol.RuntimeContract{ID: inspection.Descriptor.ABI.ID, Major: 1}, BuildID: inspection.Descriptor.Build.ID, Gamepad: true,
	}}
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return active, nil
	}
	if old == nil {
		t.Fatal("nil left kit")
	}
	old.serviceClient = client
	old.forceOwned = true
	if old.generation == "" {
		old.generation = "gen-old"
	}
	if old.token == "" {
		old.token = "tok-secret-281"
	}
	service.targetClients[service.selectedTarget] = old
	keeper := &placementKeepClient{defaultMediaPackageClient: client}
	service.targets = append(service.targets, TargetConfig{Name: "spare", Enabled: true, TargetID: "node-b", Address: "http://192.0.2.11:8182"})
	service.targetClients["spare"] = keeper
	meshEntry, _ := fpgaMeshEntry(entry.GameID)
	meshEntry.Slots[0] = meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: inspection.PackageID, ABI: "fes.simple-game", Major: 1})
	service.SetMeshExecuteSession(MeshExecuteSession{
		Entry: func(gameID string) (meshcontent.Entry, bool) { return meshEntry, gameID == entry.GameID },
	})
	candidate := placeFPGACandidate("node-b", true)
	candidate.ABIs = []meshcontent.EligibleABI{{ID: "fes.simple-game", Major: 1}}
	service.SetMeshPlacementAsk(&MeshPlacementAsk{Candidates: []meshplace.Candidate{candidate}})
	return service, keeper, entry.GameID
}

func TestPlacementRebindReleasesTheLeftKitLeaseOnSuccess(t *testing.T) {
	logs := capturePlacementLeaseLog(t)
	old := &leftKitReleaseClient{}
	service, keeper, gameID := prepareRebindLeftKit(t, old)
	leftName := service.selectedTarget
	boundName, err := launchAndObserveBind(t, service, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if boundName != "spare" || service.activeExecution != ExecutionFPGANative {
		t.Fatalf("bound %q execution %q", boundName, service.activeExecution)
	}
	if old.grantReleases != 1 || old.stops != 0 {
		t.Fatalf("left releases %d stops %d", old.grantReleases, old.stops)
	}
	owned, _ := keeper.MeshKitLease()
	if keeper.claims != 1 || keeper.releases != 0 || !owned {
		t.Fatalf("claims %d releases %d owned %v", keeper.claims, keeper.releases, owned)
	}
	text := logs.String()
	if strings.Contains(text, old.token) {
		t.Fatal("placement rebind log included the lease token")
	}
	if !strings.Contains(text, fmt.Sprintf("released kit lease on %q", leftName)) {
		t.Fatalf("log missing release of %s", leftName)
	}
	if service.meshEnsure || service.meshEnsureConfig {
		t.Fatal("ensure flipped")
	}
}

func TestPlacementRebindKeepsTheLeftKitLeaseOnFailure(t *testing.T) {
	service, _ := placementBoundService(t, false)
	base := service.targetClients[service.selectedTarget].(*fakeServiceClient)
	old := &leftKitReleaseClient{
		serviceClient: base,
		forceOwned:    true,
		generation:    base.meshLeaseGeneration,
		token:         "tok-secret-281",
	}
	service.targetClients[service.selectedTarget] = old
	leftName := service.selectedTarget
	next := &meshLaunchExecutor{
		node: "node-b",
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service.meshEnsure = true
	service.meshInstalled = meshTargetIdentityOf(service.selectedTarget, targetByName(service.targets, service.selectedTarget))
	service.meshPlacementExecutors = map[string]meshcontent.Executor{"node-b": next}
	spare := &releasingMeshClient{}
	service.targetClients["spare"] = spare
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
	})
	_, err := service.Launch(context.Background(), "coleco-frogger", nil)
	if !errors.Is(err, meshcontent.ErrContentMissingNoSource) {
		t.Fatalf("err %v", err)
	}
	owned, _ := old.MeshKitLease()
	if old.grantReleases != 0 || !owned || service.selectedTarget != leftName {
		t.Fatalf("left releases %d owned %v selected %q", old.grantReleases, owned, service.selectedTarget)
	}
	if spare.claims != 1 || spare.releases != 1 || len(next.pulls) != 0 {
		t.Fatalf("claims %d releases %d pulls %+v", spare.claims, spare.releases, next.pulls)
	}
}

func TestPlacementRebindSameKitDoesNotReleaseTheLease(t *testing.T) {
	t.Run("same target id under two names", func(t *testing.T) {
		logs := capturePlacementLeaseLog(t)
		left := &leftKitReleaseClient{serviceClient: &fakeServiceClient{}, forceOwned: true, generation: "gen-old"}
		right := &leftKitReleaseClient{serviceClient: &fakeServiceClient{}, forceOwned: true, generation: "gen-new"}
		service := &Service{
			targets: []TargetConfig{
				{Name: "dev", Enabled: true, TargetID: "node-a"},
				{Name: "alias", Enabled: true, TargetID: "node-a"},
			},
			selectedTarget: "alias",
			targetClients:  map[string]serviceClient{"dev": left, "alias": right},
		}
		service.releaseLeftPlacementLease(placementSessionUndo{
			installedName: "alias",
			installedNode: "node-a",
			selectedName:  "dev",
			boundNode:     "node-a",
		})
		if left.grantReleases != 0 || right.grantReleases != 0 {
			t.Fatalf("left %d right %d", left.grantReleases, right.grantReleases)
		}
		if !strings.Contains(logs.String(), "same kit") {
			t.Fatal("log did not record the same-kit skip")
		}
	})

	t.Run("same client", func(t *testing.T) {
		logs := capturePlacementLeaseLog(t)
		shared := &leftKitReleaseClient{serviceClient: &fakeServiceClient{}, forceOwned: true, generation: "gen-old"}
		service := &Service{
			targets: []TargetConfig{
				{Name: "dev", Enabled: true, TargetID: "node-a"},
				{Name: "spare", Enabled: true, TargetID: "node-b"},
			},
			selectedTarget: "spare",
			targetClients:  map[string]serviceClient{"dev": shared, "spare": shared},
		}
		service.releaseLeftPlacementLease(placementSessionUndo{
			installedName: "spare",
			installedNode: "node-b",
			selectedName:  "dev",
			boundNode:     "node-a",
		})
		if shared.grantReleases != 0 {
			t.Fatalf("releases %d", shared.grantReleases)
		}
		if !strings.Contains(logs.String(), "same client") {
			t.Fatal("log did not record the same-client skip")
		}
	})
}

func TestPlacementRebindLeftKitReleaseErrorIsLoggedAndNonFatal(t *testing.T) {
	logs := capturePlacementLeaseLog(t)
	base, err := url.Parse("http://192.0.2.10:8182")
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(base, "bearer", nil, "host", "placement")
	old := &leftKitReleaseClient{
		grantErr: errors.New("kit release failed"),
		token:    "tok-secret-281",
		lease:    lease,
	}
	service, keeper, gameID := prepareRebindLeftKit(t, old)
	service.stoppedKitLeases = []*targetclient.KitLease{lease}
	boundName, err := launchAndObserveBind(t, service, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if boundName != "spare" || service.activeExecution != ExecutionFPGANative {
		t.Fatalf("bound %q execution %q", boundName, service.activeExecution)
	}
	if old.grantReleases != 1 {
		t.Fatalf("left releases %d", old.grantReleases)
	}
	text := logs.String()
	if strings.Contains(text, old.token) {
		t.Fatal("placement rebind log included the lease token")
	}
	if !strings.Contains(text, "failed") {
		t.Fatal("log did not record the release failure")
	}
	if len(service.stoppedKitLeases) != 1 || service.stoppedKitLeases[0] != lease {
		t.Fatalf("retained %d grants", len(service.stoppedKitLeases))
	}
	owned, _ := keeper.MeshKitLease()
	if keeper.releases != 0 || !owned {
		t.Fatalf("spare releases %d owned %v", keeper.releases, owned)
	}
}

func TestPlacementRebindLeftKitNotHeldIsTreatedAsReleased(t *testing.T) {
	logs := capturePlacementLeaseLog(t)
	base, err := url.Parse("http://192.0.2.10:8182")
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(base, "bearer", nil, "host", "placement")
	old := &leftKitReleaseClient{notHeld: true, lease: lease, token: "tok-secret-281"}
	service, _, gameID := prepareRebindLeftKit(t, old)
	service.stoppedKitLeases = []*targetclient.KitLease{lease}
	_, err = launchAndObserveBind(t, service, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if old.grantReleases != 1 {
		t.Fatalf("left releases %d", old.grantReleases)
	}
	text := logs.String()
	if strings.Contains(text, old.token) {
		t.Fatal("placement rebind log included the lease token")
	}
	if !strings.Contains(text, "already released") {
		t.Fatal("log did not record the grant as already released")
	}
	if service.stoppedKitLeases != nil {
		t.Fatalf("retained %d grants", len(service.stoppedKitLeases))
	}
}

func TestPlacementRebindLeftKitReleaseSkipsAnInFlightHold(t *testing.T) {
	logs := capturePlacementLeaseLog(t)
	old := &leftKitReleaseClient{serviceClient: &fakeServiceClient{}, forceOwned: true, generation: "gen-old"}
	spare := &leftKitReleaseClient{serviceClient: &fakeServiceClient{}, forceOwned: true, generation: "gen-new"}
	service := &Service{
		targets: []TargetConfig{
			{Name: "dev", Enabled: true, TargetID: "node-a"},
			{Name: "spare", Enabled: true, TargetID: "node-b"},
		},
		selectedTarget: "spare",
		targetClients:  map[string]serviceClient{"dev": old, "spare": spare},
		placementHolds: map[serviceClient]*placementClaimRecord{
			old: {generation: "gen-old", inflight: 1},
		},
	}
	service.releaseLeftPlacementLease(placementSessionUndo{
		installedName: "spare",
		installedNode: "node-b",
		selectedName:  "dev",
		boundNode:     "node-a",
	})
	if old.grantReleases != 0 || spare.grantReleases != 0 {
		t.Fatalf("left %d spare %d", old.grantReleases, spare.grantReleases)
	}
	rec := service.placementHolds[old]
	if rec == nil || rec.inflight != 1 || rec.releasing {
		t.Fatalf("hold %#v", rec)
	}
	if !strings.Contains(logs.String(), "in flight") {
		t.Fatal("log did not record the in-flight skip")
	}
}

func TestPlacementRebindLeftKitReleaseSkipsWhenTheLeftKitStillNeedsTheGrant(t *testing.T) {
	undo := placementSessionUndo{installedName: "spare", installedNode: "node-b", selectedName: "dev", boundNode: "node-a"}
	newService := func(old, spare *leftKitReleaseClient) *Service {
		return &Service{
			targets: []TargetConfig{
				{Name: "dev", Enabled: true, TargetID: "node-a"},
				{Name: "spare", Enabled: true, TargetID: "node-b"},
			},
			selectedTarget: "spare",
			targetClients:  map[string]serviceClient{"dev": old, "spare": spare},
		}
	}
	cases := []struct {
		name   string
		setup  func(*Service, *leftKitReleaseClient)
		reason string
	}{
		{"left kit has a play", func(s *Service, _ *leftKitReleaseClient) {
			s.plays = map[string]targetPlay{"dev": {execution: ExecutionFPGANative, gameID: "coleco-frogger"}}
		}, "has a play"},
		{"session moved back to the left kit", func(s *Service, _ *leftKitReleaseClient) {
			s.selectedTarget = "dev"
		}, "back on the left kit"},
		{"left grant is not held", func(_ *Service, old *leftKitReleaseClient) {
			old.forceOwned = false
		}, "not held"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := capturePlacementLeaseLog(t)
			old := &leftKitReleaseClient{serviceClient: &fakeServiceClient{}, forceOwned: true, generation: "gen-old"}
			spare := &leftKitReleaseClient{serviceClient: &fakeServiceClient{}, forceOwned: true, generation: "gen-new"}
			service := newService(old, spare)
			tc.setup(service, old)
			service.releaseLeftPlacementLease(undo)
			if old.grantReleases != 0 || spare.grantReleases != 0 || old.stops != 0 {
				t.Fatalf("left releases %d spare releases %d left stops %d", old.grantReleases, spare.grantReleases, old.stops)
			}
			if !strings.Contains(logs.String(), tc.reason) {
				t.Fatalf("log %q missing %q", logs.String(), tc.reason)
			}
		})
	}
}
