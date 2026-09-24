package fogcast

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func TestLaunchCheckingDoesNotExecute(t *testing.T) {
	bios := meshcontent.SumSHA256([]byte("bios"))
	cart := meshcontent.SumSHA256([]byte("source-rom"))
	entry := meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.application", Major: 1}),
			meshcontent.BIOSSlot(bios),
			meshcontent.PrimaryMediaSlot(cart),
		},
	}
	exec := &meshLaunchExecutor{
		node: "kit-a",
		held: map[string]meshcontent.SlotState{
			bios.String(): meshcontent.StatePresent,
			cart.String(): meshcontent.StateChecking,
		},
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := &Service{
		targets:        []TargetConfig{{Name: "dev", Enabled: true, TargetID: "kit-a"}},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": ownedMeshClient()},
	}
	service.SetMeshCheckingTimeout(40 * time.Millisecond)
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "kit-a",
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	if !errors.Is(err, meshcontent.ErrCheckingTimeout) || errors.Is(err, meshcontent.ErrExecuteBlocked) {
		t.Fatalf("launch err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("checking launch pulled %+v linked %+v", exec.pulls, exec.links)
	}
}

func TestLaunchMissingNoSourceAndUnboundNode(t *testing.T) {
	cart := meshcontent.SumSHA256([]byte("source-rom"))
	entry := meshcontent.Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
		Slots:      []meshcontent.Slot{meshcontent.PrimaryMediaSlot(cart)},
	}
	exec := &meshLaunchExecutor{node: "host-a"}
	service := &Service{}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "host-a",
		Executor:  exec,
		Entry:     func(string) (meshcontent.Entry, bool) { return entry, true },
	})
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	if !errors.Is(err, meshcontent.ErrContentMissingNoSource) {
		t.Fatalf("missing err %v", err)
	}
	if len(exec.pulls) != 0 {
		t.Fatalf("missing launch pulled %+v", exec.pulls)
	}

	other := &meshLaunchExecutor{node: "other", sources: map[string]bool{cart.String(): true}}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "host-a",
		Executor:  other,
		Entry:     func(string) (meshcontent.Entry, bool) { return entry, true },
	})
	_, err = service.Launch(context.Background(), entry.TitleID, nil)
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("unbound err %v", err)
	}
	if len(other.pulls) != 0 || len(other.links) != 0 {
		t.Fatal("unbound launch pulled onto another node")
	}
}

func TestLaunchPullThenPresentAllowsTheExistingPathOnlyAfterEnsure(t *testing.T) {
	cart := meshcontent.SumSHA256([]byte("source-rom"))
	entry := meshcontent.Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
		Slots:      []meshcontent.Slot{meshcontent.PrimaryMediaSlot(cart)},
	}
	exec := &meshLaunchExecutor{
		node:    "host-a",
		sources: map[string]bool{cart.String(): true},
	}
	service := &Service{}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "host-a",
		Executor:  exec,
		Entry:     func(string) (meshcontent.Entry, bool) { return entry, true },
	})
	snap, err := service.captureLaunchSnapshot(entry.TitleID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.meshEnsureBeforeExecute(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart || exec.Slot(cart) != meshcontent.StatePresent {
		t.Fatalf("pulls %+v state %s", exec.pulls, exec.Slot(cart))
	}
	// A second ensure sees the id Present and does not pull again.
	// Launch would continue into the existing path; this service has
	// no catalog, so the assertion stops at the seam.
	snap, err = service.captureLaunchSnapshot(entry.TitleID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.meshEnsureBeforeExecute(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if len(exec.pulls) != 1 {
		t.Fatalf("present id pulled again %+v", exec.pulls)
	}
}

func TestLaunchOnNamedTargetDoesNotEnsureAnotherNode(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "spare", nil)
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("mismatched target pulled %+v linked %+v", exec.pulls, exec.links)
	}

	// A named target with no node id still binds that kit. Ensure must
	// not fall back to the session node.
	unnamed := meshTargetService(exec, "node-a", entry)
	unnamed.targets = []TargetConfig{{Name: "dev", Enabled: true}, {Name: "spare", Enabled: true}}
	exec.pulls, exec.links = nil, nil
	_, err = unnamed.LaunchOn(context.Background(), entry.TitleID, "spare", nil)
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("unnamed target err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("unnamed target pulled %+v linked %+v", exec.pulls, exec.links)
	}
}

func TestLaunchOnSelectedTargetChangeDoesNotEnsureTheOldNode(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	service.selectedTarget = "spare"
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("moved selection pulled %+v linked %+v", exec.pulls, exec.links)
	}
}

func TestImplicitEmptyTargetIDDoesNotInheritBoundNode(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := &Service{
		targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		selectedTarget: "spare",
		targetClients:  map[string]serviceClient{"spare": &fakeServiceClient{}},
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "node-a",
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("unmatched empty id pulled %+v linked %+v", exec.pulls, exec.links)
	}
}

func TestImplicitEmptyTargetIDEnsuresWhenNameIsTheBoundNode(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "dev",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := &Service{
		targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": ownedMeshClient()},
		catalog:        &fakeServiceCatalog{gameErr: errors.New("stop after ensure")},
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "dev",
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeInternal {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart || exec.node != "dev" {
		t.Fatalf("pulls %+v node %s", exec.pulls, exec.node)
	}
}

func TestLaunchOnMatchingTargetEnsuresThatNode(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "stale-session-node", entry)
	service.catalog = &fakeServiceCatalog{gameErr: errors.New("stop after ensure")}
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "dev", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeInternal {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart || exec.node != "node-a" {
		t.Fatalf("pulls %+v node %s", exec.pulls, exec.node)
	}
}

func TestLaunchOnForeignKitDoesNotPull(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	service.connection = TargetConnection{State: "busy", Owner: "other-shell", TargetID: "node-a"}
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("foreign kit pulled %+v linked %+v", exec.pulls, exec.links)
	}
}

// A session that cannot claim still fails closed before any pull.
func TestFreeKitDoesNotPull(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{meshcontent.SumSHA256([]byte("source-rom")).String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	service.targetClients["dev"] = &fakeServiceClient{}
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	if !errors.Is(err, meshcontent.ErrLeaseNotFree) {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("free kit pulled %+v linked %+v", exec.pulls, exec.links)
	}
}

func TestObservedLeaseGenerationMismatchDoesNotPull(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	service.connection = TargetConnection{State: "ready", TargetID: "node-a", leaseSeen: true, leaseOwned: true, leaseGeneration: "other-gen"}
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	if !errors.Is(err, meshcontent.ErrLeaseNotFree) {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 {
		t.Fatalf("stale generation pulled %+v", exec.pulls)
	}

	service.connection.leaseGeneration = "gen-owned"
	service.catalog = &fakeServiceCatalog{gameErr: errors.New("stop after ensure")}
	_, err = service.LaunchOn(context.Background(), entry.TitleID, "", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeInternal {
		t.Fatalf("matching generation err %v", err)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("pulls %+v", exec.pulls)
	}
}

func TestLaunchOnHostEntryStaysOnTheSessionNode(t *testing.T) {
	cart := meshcontent.SumSHA256([]byte("source-rom"))
	entry := meshcontent.Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
		Slots:      []meshcontent.Slot{meshcontent.PrimaryMediaSlot(cart)},
	}
	exec := &meshLaunchExecutor{
		node:    "host-a",
		sources: map[string]bool{cart.String(): true},
	}
	service := meshTargetService(exec, "host-a", entry)
	service.catalog = &fakeServiceCatalog{gameErr: errors.New("stop after ensure")}
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "spare", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeInternal {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("host pulls %+v", exec.pulls)
	}
}

func fpgaMeshEntry(title string) (meshcontent.Entry, meshcontent.ContentID) {
	cart := meshcontent.SumSHA256([]byte("source-rom"))
	entry := meshcontent.Entry{
		TitleID:    title,
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.application", Major: 1}),
			meshcontent.PrimaryMediaSlot(cart),
		},
	}
	return entry, cart
}

func ownedMeshClient() *fakeServiceClient {
	return &fakeServiceClient{meshLeaseGeneration: "gen-owned"}
}

func meshTargetService(exec *meshLaunchExecutor, bound string, entry meshcontent.Entry) *Service {
	service := &Service{
		targets: []TargetConfig{
			{Name: "dev", Enabled: true, TargetID: "node-a"},
			{Name: "spare", Enabled: true, TargetID: "node-b"},
		},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": ownedMeshClient()},
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: bound,
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	return service
}

func TestImplicitLaunchRejectsSelectionChangeAfterEnsure(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	service.catalog = &flipSelectedCoreCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, service: service}
	var bound string
	launchPinnedTargetBoundHook = func(name string) { bound = name }
	t.Cleanup(func() { launchPinnedTargetBoundHook = nil })

	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	var drifted *LaunchSnapshotError
	if !errors.As(err, &drifted) || drifted.Reason != launchSnapshotSelectionChanged || !errors.Is(err, ErrLaunchSnapshot) {
		t.Fatalf("err %v", err)
	}
	if bound != "" {
		t.Fatalf("bound %q after selection changed", bound)
	}
	if service.selectedTarget != "spare" {
		t.Fatalf("selected target %q", service.selectedTarget)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("pulls %+v", exec.pulls)
	}
}

func TestLaunchRejectsCompositionChangeAfterEnsure(t *testing.T) {
	service, client, coreEntry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Coleco Graphics I")
	active := coreEntryActiveStatus(inspection, 7, true)
	client.mediaStatus = active
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}
	cartA := meshcontent.SumSHA256([]byte("composition-a"))
	cartB := meshcontent.SumSHA256([]byte("composition-b"))
	current := cartA
	exec := &meshLaunchExecutor{
		node:    "dev",
		sources: map[string]bool{cartA.String(): true, cartB.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.simple-computer", Major: 1}},
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "dev",
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			if gameID != coreEntry.GameID {
				return meshcontent.Entry{}, false
			}
			return meshcontent.Entry{
				TitleID:    coreEntry.GameID,
				System:     "coleco",
				Launchable: true,
				Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
				Slots: []meshcontent.Slot{
					meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.simple-computer", Major: 1}),
					meshcontent.PrimaryMediaSlot(current),
				},
			}, true
		},
	})
	meshEnsureFinishedHook = func() { current = cartB }
	t.Cleanup(func() { meshEnsureFinishedHook = nil })

	service.targets[0].Enabled = true
	client.meshLeaseGeneration = "gen-owned"
	_, err := service.Launch(context.Background(), coreEntry.GameID, nil)
	var drifted *LaunchSnapshotError
	if !errors.As(err, &drifted) || drifted.Reason != launchSnapshotCompositionChanged {
		t.Fatalf("err %v", err)
	}
	if client.coreCalls != 0 || client.mediaCalls != 0 {
		t.Fatalf("programmed core=%d media=%d", client.coreCalls, client.mediaCalls)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cartA {
		t.Fatalf("pulls %+v", exec.pulls)
	}
}

func TestHostLaunchRejectsCompositionChangeAfterEnsure(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	prepared := preparedServiceFixture(t, []byte("rom"), identity)
	t.Cleanup(func() { _ = prepared.Remove() })
	adapter := &fakeHostExecutor{}
	service := newTestServiceWithExecution(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{prepared: prepared}, &fakeServiceClient{}, ExecutionPolicy{
		Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
		Host:     adapter,
	})
	cartA := meshcontent.SumSHA256([]byte("composition-a"))
	cartB := meshcontent.SumSHA256([]byte("composition-b"))
	current := cartA
	exec := &meshLaunchExecutor{
		node:    "host-a",
		sources: map[string]bool{cartA.String(): true, cartB.String(): true},
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "host-a",
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			if gameID != game.ID {
				return meshcontent.Entry{}, false
			}
			return meshcontent.Entry{
				TitleID:    game.ID,
				System:     "snes",
				Launchable: true,
				Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
				Slots:      []meshcontent.Slot{meshcontent.PrimaryMediaSlot(current)},
			}, true
		},
	})
	meshEnsureFinishedHook = func() { current = cartB }
	t.Cleanup(func() { meshEnsureFinishedHook = nil })

	service.targets[0].Enabled = true
	_, err := service.Launch(context.Background(), game.ID, nil)
	var drifted *LaunchSnapshotError
	if !errors.As(err, &drifted) || drifted.Reason != launchSnapshotCompositionChanged {
		t.Fatalf("err %v", err)
	}
	if adapter.launchCalls != 0 {
		t.Fatalf("host launch calls %d", adapter.launchCalls)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cartA {
		t.Fatalf("pulls %+v", exec.pulls)
	}
}

func TestSeamOffBindFollowsLiveTarget(t *testing.T) {
	original := &fakeServiceClient{}
	replacement := &fakeServiceClient{}
	service := &Service{
		targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": original},
		hostExecutor:   &fakeHostExecutor{},
		executionResolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) {
			return ExecutionHostOnly, nil
		}),
	}
	service.catalog = &seamOffTargetCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, service: service, replacement: replacement, next: "spare"}
	var bound string
	launchPinnedTargetBoundHook = func(name string) { bound = name }
	t.Cleanup(func() { launchPinnedTargetBoundHook = nil })

	_, err := service.Launch(context.Background(), "snes-mario", nil)
	if bound != "spare" {
		t.Fatalf("bound %q err %v selected %q", bound, err, service.selectedTarget)
	}
	if service.targetClients["dev"] != replacement {
		t.Fatalf("dev client restored to the capture, err %v", err)
	}
}

type seamOffTargetCatalog struct {
	*fakeServiceCatalog
	service     *Service
	replacement serviceClient
	next        string
}

func (c *seamOffTargetCatalog) Game(context.Context, string) (catalog.Game, error) {
	c.service.selectedTarget = c.next
	c.service.targetClients["dev"] = c.replacement
	return catalog.Game{ID: "snes-mario", System: protocol.SystemSNES, State: catalog.SourceStateAvailable, RootOnline: true}, nil
}

func TestBindRejectsPinnedEndpointChange(t *testing.T) {
	for _, change := range []string{"target-id", "address"} {
		t.Run(change, func(t *testing.T) {
			entry, cart := fpgaMeshEntry("coleco-frogger")
			exec := &meshLaunchExecutor{
				node:    "node-a",
				sources: map[string]bool{cart.String(): true},
				abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
			}
			service := meshTargetService(exec, "node-a", entry)
			service.targets[0].Address = "http://192.0.2.10:8182"
			original := ownedMeshClient()
			replacement := &fakeServiceClient{}
			service.targetClients = map[string]serviceClient{"dev": original}
			service.catalog = &stableCoreCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, id: entry.TitleID}
			meshEnsureFinishedHook = func() {
				switch change {
				case "target-id":
					service.targets[0].TargetID = "node-b"
				case "address":
					service.targets[0].Address = "http://192.0.2.99:8182"
				}
				service.targetClients["dev"] = replacement
			}
			t.Cleanup(func() { meshEnsureFinishedHook = nil })

			_, err := service.Launch(context.Background(), entry.TitleID, nil)
			var drifted *LaunchSnapshotError
			want := launchSnapshotTargetIDChanged
			if change == "address" {
				want = launchSnapshotAddressChanged
			}
			if !errors.As(err, &drifted) || drifted.Reason != want {
				t.Fatalf("err %v", err)
			}
			if original.coreCalls != 0 || replacement.coreCalls != 0 {
				t.Fatalf("clients original=%d replacement=%d", original.coreCalls, replacement.coreCalls)
			}
			if len(exec.pulls) != 1 || exec.pulls[0] != cart {
				t.Fatalf("pulls %+v", exec.pulls)
			}
		})
	}
}

func TestBindKeepsPinnedClientWhenEndpointIsUnchanged(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	service.targets[0].Address = "http://192.0.2.10:8182"
	original := ownedMeshClient()
	replacement := &fakeServiceClient{}
	service.targetClients = map[string]serviceClient{"dev": original}
	service.catalog = &stableCoreCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, id: entry.TitleID}
	meshEnsureFinishedHook = func() { service.targetClients["dev"] = replacement }
	t.Cleanup(func() { meshEnsureFinishedHook = nil })

	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	var drifted *LaunchSnapshotError
	if !errors.As(err, &drifted) || drifted.Reason != launchSnapshotClientChanged {
		t.Fatalf("err %v", err)
	}
	if service.targetClients["dev"] != replacement {
		t.Fatal("overwrote the live client with the captured one")
	}
	if original.coreCalls != 0 || replacement.coreCalls != 0 {
		t.Fatalf("programmed original=%d replacement=%d", original.coreCalls, replacement.coreCalls)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("pulls %+v", exec.pulls)
	}
}

type flipSelectedCoreCatalog struct {
	*fakeServiceCatalog
	service *Service
	flipped bool
}

func (c *flipSelectedCoreCatalog) CoreEntry(context.Context, string) (catalog.CoreEntry, error) {
	if c != nil && !c.flipped {
		c.flipped = true
		c.service.selectedTarget = "spare"
	}
	return catalog.CoreEntry{GameID: "coleco-frogger"}, nil
}

func (c *flipSelectedCoreCatalog) CoreEntries(context.Context) ([]catalog.CoreEntry, error) {
	return nil, nil
}

func (c *flipSelectedCoreCatalog) CreateCoreEntry(context.Context, string, string, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, catalog.ErrCoreEntryNotFound
}

func (c *flipSelectedCoreCatalog) SelectCoreEntry(context.Context, string, string, string, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, catalog.ErrCoreEntryNotFound
}

func TestProjectedExpansionLinksSlotBytesOnTheExecutor(t *testing.T) {
	source := strings.Repeat("22", 32)
	slotBytes := strings.Repeat("33", 32)
	programmed := strings.Repeat("44", 32)
	title := meshCoreTitle("3D Monster Maze", "fes.zx81", strings.Repeat("cd", 32), source, false)
	title.ABI = corepackage.Contract{ID: "fes.simple-computer", Major: 1, Minor: 0}
	title.Expansions = []MeshExpansion{{Name: expansion.Slot, Digest: slotBytes}}
	entries, skipped := ProjectMeshLibrary(MeshLibrary{Titles: []MeshTitle{title}})
	if len(skipped) != 0 || len(entries) != 1 {
		t.Fatalf("entries %+v skipped %+v", entries, skipped)
	}
	entry := entries[0]
	primary, err := PrimarySourceID(source)
	if err != nil {
		t.Fatal(err)
	}
	expansionID, err := ExpansionSlotBytesID(slotBytes)
	if err != nil {
		t.Fatal(err)
	}
	if title.Core.MediaID != source {
		t.Fatal("primary fixture is not the source MediaID")
	}
	var sawPrimary, sawExpansion bool
	for _, slot := range entry.Slots {
		if slot.Content == nil {
			continue
		}
		if slot.Kind == meshcontent.SlotPrimaryMedia && *slot.Content == primary {
			sawPrimary = true
		}
		if slot.Kind == meshcontent.SlotExpansion && *slot.Content == expansionID && slot.Content.Digest == slotBytes {
			sawExpansion = true
		}
		if slot.Content.Digest == programmed {
			t.Fatal("projection used ProgrammedSHA256")
		}
	}
	if !sawPrimary || !sawExpansion {
		t.Fatalf("slots %+v", entry.Slots)
	}
	exec := &meshLaunchExecutor{
		node: "kit-a",
		sources: map[string]bool{
			primary.String():       true,
			expansionID.String():   true,
			"sha256:" + programmed: true,
		},
		abis: []meshcontent.EligibleABI{{ID: "fes.simple-computer", Major: 1}},
	}
	result, err := meshcontent.Ensure(context.Background(), entry, "kit-a", exec, meshcontent.EnsureOption{LeaseFree: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Execute || len(exec.links) != 1 || exec.links[0].id != expansionID || exec.links[0].name != expansion.Slot {
		t.Fatalf("result %+v links %+v", result, exec.links)
	}
	for _, id := range exec.pulls {
		if id.Digest == programmed {
			t.Fatal("pull asked for the programmed image")
		}
	}
	if exec.links[0].id.Digest != slotBytes {
		t.Fatal("executor linked something other than the slot-bytes digest")
	}
}

func TestLaunchRefusesTargetDisabledDuringEnsure(t *testing.T) {
	service, client, coreEntry, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "Coleco Graphics I")
	active := coreEntryActiveStatus(inspection, 7, true)
	client.mediaStatus = active
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}
	service.targets[0].Enabled = true
	service.targets[0].Address = "http://192.0.2.10:8182"
	service.targets[0].TargetID = "dev"
	client.meshLeaseGeneration = "gen-owned"
	cart := meshcontent.SumSHA256([]byte("source-rom"))
	exec := &meshLaunchExecutor{
		node:    "dev",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.simple-computer", Major: 1}},
		duringPull: func() {
			service.targets[0].Enabled = false
			delete(service.targetClients, "dev")
		},
	}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "dev",
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			if gameID != coreEntry.GameID {
				return meshcontent.Entry{}, false
			}
			return meshcontent.Entry{
				TitleID:    coreEntry.GameID,
				System:     "coleco",
				Launchable: true,
				Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
				Slots: []meshcontent.Slot{
					meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.simple-computer", Major: 1}),
					meshcontent.PrimaryMediaSlot(cart),
				},
			}, true
		},
	})
	_, err := service.Launch(context.Background(), coreEntry.GameID, nil)
	var drifted *LaunchSnapshotError
	if !errors.As(err, &drifted) || drifted.Reason != launchSnapshotTargetDisabled {
		t.Fatalf("err %v", err)
	}
	if client.coreCalls != 0 || client.mediaCalls != 0 {
		t.Fatalf("programmed core=%d media=%d", client.coreCalls, client.mediaCalls)
	}
	if service.targetClients["dev"] != nil {
		t.Fatal("restored the captured client onto the disabled target")
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("pulls %+v", exec.pulls)
	}
}

func TestLaunchSnapshotRejectsDriftDuringEnsure(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	cases := []struct {
		name   string
		reason string
		mutate func(*Service)
	}{
		{name: "removed", reason: launchSnapshotTargetRemoved, mutate: func(service *Service) {
			service.targets = []TargetConfig{{Name: "spare", Enabled: true, TargetID: "node-b"}}
		}},
		{name: "bound-node", reason: launchSnapshotBoundNodeChanged, mutate: func(service *Service) {
			service.SetMeshExecuteSession(MeshExecuteSession{
				BoundNode: "other",
				Executor:  service.meshExecute.Executor,
				Entry:     service.meshExecute.Entry,
			})
		}},
		{name: "package", reason: launchSnapshotCompositionChanged, mutate: func(service *Service) {
			service.meshExecute.Entry = func(gameID string) (meshcontent.Entry, bool) {
				changed := entry
				changed.Slots = append([]meshcontent.Slot(nil), entry.Slots...)
				pkg := *changed.Slots[0].Package
				pkg.PackageID = strings.Repeat("cd", 32)
				changed.Slots[0] = meshcontent.PackageSlot(pkg)
				return changed, gameID == entry.TitleID
			}
		}},
		{name: "abi", reason: launchSnapshotCompositionChanged, mutate: func(service *Service) {
			service.meshExecute.Entry = func(gameID string) (meshcontent.Entry, bool) {
				changed := entry
				changed.Slots = append([]meshcontent.Slot(nil), entry.Slots...)
				pkg := *changed.Slots[0].Package
				pkg.ABI = "fes.simple-computer"
				changed.Slots[0] = meshcontent.PackageSlot(pkg)
				return changed, gameID == entry.TitleID
			}
		}},
		{name: "firmware", reason: launchSnapshotCompositionChanged, mutate: func(service *Service) {
			bios := meshcontent.SumSHA256([]byte("bios"))
			service.meshExecute.Entry = func(gameID string) (meshcontent.Entry, bool) {
				changed := entry
				changed.Slots = append(append([]meshcontent.Slot(nil), entry.Slots...), meshcontent.BIOSSlot(bios))
				return changed, gameID == entry.TitleID
			}
		}},
		{name: "rom", reason: launchSnapshotCompositionChanged, mutate: func(service *Service) {
			other := meshcontent.SumSHA256([]byte("other-rom"))
			service.meshExecute.Entry = func(gameID string) (meshcontent.Entry, bool) {
				changed := entry
				changed.Slots = append([]meshcontent.Slot(nil), entry.Slots...)
				changed.Slots[1] = meshcontent.PrimaryMediaSlot(other)
				return changed, gameID == entry.TitleID
			}
		}},
		{name: "expansion", reason: launchSnapshotCompositionChanged, mutate: func(service *Service) {
			ram := meshcontent.SumSHA256([]byte("slot-bytes"))
			service.meshExecute.Entry = func(gameID string) (meshcontent.Entry, bool) {
				changed := entry
				changed.Slots = append(append([]meshcontent.Slot(nil), entry.Slots...), meshcontent.ExpansionSlot("port", ram))
				return changed, gameID == entry.TitleID
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &meshLaunchExecutor{
				node:    "node-a",
				sources: map[string]bool{cart.String(): true},
				abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
			}
			service := meshTargetService(exec, "node-a", entry)
			service.targets[0].Address = "http://192.0.2.10:8182"
			original := ownedMeshClient()
			service.targetClients = map[string]serviceClient{"dev": original}
			service.catalog = &stableCoreCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, id: entry.TitleID}
			exec.duringPull = func() { tc.mutate(service) }
			_, err := service.Launch(context.Background(), entry.TitleID, nil)
			var drifted *LaunchSnapshotError
			if !errors.As(err, &drifted) || drifted.Reason != tc.reason {
				t.Fatalf("err %v", err)
			}
			if original.coreCalls != 0 {
				t.Fatalf("core calls %d", original.coreCalls)
			}
			if service.targetClients["dev"] != original && tc.name != "removed" && tc.name != "bound-node" {
				t.Fatalf("client changed on %s", tc.name)
			}
			if len(exec.pulls) != 1 {
				t.Fatalf("pulls %+v", exec.pulls)
			}
		})
	}
}

func TestExplicitLaunchKeepsNamedTargetWhenSelectionChanges(t *testing.T) {
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	service.catalog = &stableCoreCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, id: entry.TitleID}
	service.targetClients = map[string]serviceClient{"dev": ownedMeshClient()}
	var bound string
	launchPinnedTargetBoundHook = func(name string) { bound = name }
	t.Cleanup(func() { launchPinnedTargetBoundHook = nil })
	exec.duringPull = func() { service.selectedTarget = "spare" }
	_, err := service.LaunchOn(context.Background(), entry.TitleID, "dev", nil)
	var drifted *LaunchSnapshotError
	if errors.As(err, &drifted) {
		t.Fatalf("explicit launch drifted: %v", err)
	}
	if bound != "dev" {
		t.Fatalf("bound %q err %v", bound, err)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("pulls %+v", exec.pulls)
	}
}

func TestCanceledOrMalformedLaunchDoesNotEnsure(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{meshcontent.SumSHA256([]byte("source-rom")).String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
		duringPull: func() {
			t.Error("pull started")
		},
	}
	service := meshTargetService(exec, "node-a", entry)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Launch(ctx, entry.TitleID, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err %v", err)
	}
	_, err = service.Launch(context.Background(), "Not A Game", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeBadRequest {
		t.Fatalf("malformed err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("admission pulled %+v linked %+v", exec.pulls, exec.links)
	}
}

func TestCheckingCancelDoesNotExecute(t *testing.T) {
	bios := meshcontent.SumSHA256([]byte("bios"))
	cart := meshcontent.SumSHA256([]byte("source-rom"))
	entry := meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.application", Major: 1}),
			meshcontent.BIOSSlot(bios),
			meshcontent.PrimaryMediaSlot(cart),
		},
	}
	exec := &meshLaunchExecutor{
		node: "kit-a",
		held: map[string]meshcontent.SlotState{
			bios.String(): meshcontent.StatePresent,
			cart.String(): meshcontent.StateChecking,
		},
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := &Service{
		targets:        []TargetConfig{{Name: "dev", Enabled: true, TargetID: "kit-a"}},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": ownedMeshClient()},
	}
	service.SetMeshCheckingTimeout(2 * time.Second)
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "kit-a",
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, err := service.Launch(ctx, entry.TitleID, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("canceled checking pulled %+v linked %+v", exec.pulls, exec.links)
	}
}

func TestDisabledTargetDoesNotEnsure(t *testing.T) {
	entry, _ := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node:    "node-a",
		sources: map[string]bool{meshcontent.SumSHA256([]byte("source-rom")).String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service := meshTargetService(exec, "node-a", entry)
	service.targets[0].Enabled = false
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	var drifted *LaunchSnapshotError
	if !errors.As(err, &drifted) || drifted.Reason != launchSnapshotTargetDisabled {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("disabled target pulled %+v", exec.pulls)
	}
}

func TestMeshCheckingTimeoutClamps(t *testing.T) {
	service := &Service{}
	service.SetMeshCheckingTimeout(meshcontent.MaxCheckingTimeout + time.Minute)
	if service.meshCheckingWait() != meshcontent.MaxCheckingTimeout {
		t.Fatalf("timeout %s", service.meshCheckingWait())
	}
}

func TestMeshExpansionDigestIsSlotBytesBeforeEnsure(t *testing.T) {
	slotBytes := strings.Repeat("ab", 32)
	id, err := ExpansionSlotBytesID(slotBytes)
	if err != nil || id.Digest != slotBytes || id.Algorithm != meshcontent.AlgorithmSHA256 {
		t.Fatalf("id %+v err %v", id, err)
	}
	assetID := strings.Repeat("cd", 32)
	archiveMedia := strings.Repeat("ef", 32)
	programmed := strings.Repeat("12", 32)
	if id.Digest == assetID || id.Digest == archiveMedia || id.Digest == programmed {
		t.Fatal("slot-bytes digest collided with another identity")
	}
	var expansionRow MeshExpansion
	expansionRow.Digest = slotBytes
	if expansionRow.Digest != id.Digest {
		t.Fatal("MeshExpansion.Digest is not the slot-bytes digest")
	}
}

type meshExpansionLink struct {
	name string
	id   meshcontent.ContentID
}

type meshLaunchExecutor struct {
	node       string
	held       map[string]meshcontent.SlotState
	sources    map[string]bool
	abis       []meshcontent.EligibleABI
	pulls      []meshcontent.ContentID
	links      []meshExpansionLink
	duringPull func()
}

type stableCoreCatalog struct {
	*fakeServiceCatalog
	id string
}

func (c *stableCoreCatalog) CoreEntry(context.Context, string) (catalog.CoreEntry, error) {
	id := "coleco-frogger"
	if c != nil && c.id != "" {
		id = c.id
	}
	return catalog.CoreEntry{GameID: id}, nil
}

func (c *stableCoreCatalog) CoreEntries(context.Context) ([]catalog.CoreEntry, error) {
	return nil, nil
}

func (c *stableCoreCatalog) CreateCoreEntry(context.Context, string, string, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, catalog.ErrCoreEntryNotFound
}

func (c *stableCoreCatalog) SelectCoreEntry(context.Context, string, string, string, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, catalog.ErrCoreEntryNotFound
}

func (f *meshLaunchExecutor) NodeID() string { return f.node }

func (f *meshLaunchExecutor) Slot(id meshcontent.ContentID) meshcontent.SlotState {
	if f.held == nil {
		return meshcontent.StateMissing
	}
	if state, ok := f.held[id.String()]; ok {
		return state
	}
	return meshcontent.StateMissing
}

func (f *meshLaunchExecutor) SourceAdvertises(id meshcontent.ContentID) bool {
	return f.sources[id.String()]
}

func (f *meshLaunchExecutor) Pull(ctx context.Context, id meshcontent.ContentID) (meshcontent.SlotState, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.duringPull != nil {
		f.duringPull()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.pulls = append(f.pulls, id)
	if f.held == nil {
		f.held = map[string]meshcontent.SlotState{}
	}
	f.held[id.String()] = meshcontent.StatePresent
	return meshcontent.StatePresent, nil
}

func (f *meshLaunchExecutor) LinkExpansion(name string, id meshcontent.ContentID) error {
	f.links = append(f.links, meshExpansionLink{name: name, id: id})
	return nil
}

func (f *meshLaunchExecutor) EligibleABIs() []meshcontent.EligibleABI { return f.abis }
