package fogcast

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/internal/meshpref"
	"github.com/DeanoC/FogCast/protocol"
)

func TestPlacementOnBoundExecutorRecordsRolesAndKeepsBind(t *testing.T) {
	t.Run("automatic selection", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-a", true)},
		})
		assertPlacementKeptBind(t, service, exec, MeshPlacement{
			Execute: "node-a", DisplaySink: "node-a", InputSource: "node-a",
		})
	})

	t.Run("preference selects the bound node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetDisplayPreference("node-a")
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("node-b", true),
				placeFPGACandidate("node-a", true),
			},
		})
		if service.PlaceOptions().DisplayPreference != "node-a" {
			t.Fatalf("options %+v", service.PlaceOptions())
		}
		assertPlacementKeptBind(t, service, exec, MeshPlacement{
			Execute: "node-a", DisplaySink: "node-a", InputSource: "node-a",
		})
	})

	t.Run("override selects the bound node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("node-b", true),
				placeFPGACandidate("node-a", true),
			},
			OverrideNodeID: " node-a ",
		})
		assertPlacementKeptBind(t, service, exec, MeshPlacement{
			Execute: "node-a", DisplaySink: "node-a", InputSource: "node-a",
		})
	})

	t.Run("omits input the node does not advertise", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-a", false)},
		})
		assertPlacementKeptBind(t, service, exec, MeshPlacement{
			Execute: "node-a", DisplaySink: "node-a",
		})
	})

	t.Run("phase 0 selected target", func(t *testing.T) {
		service := phase0PlacementService()
		entry, _ := fpgaMeshEntry("coleco-frogger")
		service.SetMeshExecuteSession(MeshExecuteSession{
			Entry: func(string) (meshcontent.Entry, bool) { return entry, true },
		})
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-a", true)},
		})
		boundName, err := launchAndObserveBind(t, service, entry.TitleID)
		if errors.Is(err, meshcontent.ErrUnboundNode) {
			t.Fatal(err)
		}
		if boundName != "dev" {
			t.Fatalf("bound %q err %v", boundName, err)
		}
		want := MeshPlacement{Execute: "node-a", DisplaySink: "node-a", InputSource: "node-a"}
		if got := service.MeshSessionPlacement(); got != want {
			t.Fatalf("placement %+v", got)
		}
		if service.meshExecute.Executor != nil || service.meshExecute.BoundNode != "" {
			t.Fatalf("phase 0 session gained bound %q exec %v", service.meshExecute.BoundNode, service.meshExecute.Executor)
		}
		if service.meshEnsure {
			t.Fatal("ensure flipped")
		}
	})
}

func TestPlacementRebindClaimsTheSelectedFPGAKit(t *testing.T) {
	want := MeshPlacement{Execute: "node-b", DisplaySink: "node-b", InputSource: "node-b"}

	t.Run("only other eligible node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.meshExecute.Placement = MeshPlacement{Execute: "node-a", DisplaySink: "node-a", InputSource: "node-a"}
		spare := &releasingMeshClient{}
		assertPlacementRebound(t, service, exec, spare, &MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
		}, want)
	})

	t.Run("preference selects another node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetDisplayPreference("node-b")
		spare := &releasingMeshClient{}
		assertPlacementRebound(t, service, exec, spare, &MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("node-a", true),
				placeFPGACandidate("node-b", true),
			},
		}, want)
	})

	t.Run("override selects another node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		spare := &releasingMeshClient{}
		assertPlacementRebound(t, service, exec, spare, &MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("node-a", true),
				placeFPGACandidate("node-b", true),
			},
			OverrideNodeID: "node-b",
		}, want)
	})

	t.Run("phase 0 selected target is not the selection", func(t *testing.T) {
		service := phase0PlacementService()
		service.targets = append(service.targets, TargetConfig{Name: "spare", Enabled: true, TargetID: "node-b", Address: "http://192.0.2.11:8182"})
		spare := &releasingMeshClient{}
		service.targetClients["spare"] = spare
		entry, _ := fpgaMeshEntry("coleco-frogger")
		service.SetMeshExecuteSession(MeshExecuteSession{
			Entry: func(string) (meshcontent.Entry, bool) { return entry, true },
		})
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
		})
		boundName, err := launchAndObserveBind(t, service, entry.TitleID)
		if errors.Is(err, meshcontent.ErrUnboundNode) {
			t.Fatal(err)
		}
		if boundName != "spare" || service.selectedTarget != "spare" {
			t.Fatalf("bound %q selected %q err %v", boundName, service.selectedTarget, err)
		}
		if service.meshExecute.Executor != nil {
			t.Fatal("phase 0 rebind installed an executor")
		}
		if got := service.MeshSessionPlacement(); got != want {
			t.Fatalf("placement %+v", got)
		}
		if service.meshEnsure || service.meshEnsureConfig {
			t.Fatal("ensure flipped")
		}
		if spare.claims != 1 || spare.releases != 0 {
			t.Fatalf("claims %d releases %d", spare.claims, spare.releases)
		}
		if err := spare.ReleaseContentPullLease(context.Background()); err != nil {
			t.Fatal(err)
		}
		owned, _ := spare.MeshKitLease()
		if owned || spare.releases != 1 {
			t.Fatalf("explicit release owned %v releases %d", owned, spare.releases)
		}
	})
}

func TestPlacementRebindEnsureOnUsesTheSelectedExecutor(t *testing.T) {
	service, old := placementBoundService(t, true)
	_, cart := fpgaMeshEntry("coleco-frogger")
	next := &meshLaunchExecutor{
		node:    "node-b",
		sources: map[string]bool{cart.String(): true},
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service.meshEnsure = true
	service.meshInstalled = meshTargetIdentityOf(service.selectedTarget, targetByName(service.targets, service.selectedTarget))
	service.meshPlacementExecutors = map[string]meshcontent.Executor{"node-b": next}
	spare := &releasingMeshClient{}
	service.targetClients["spare"] = spare
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
	})
	boundName, err := launchAndObserveBind(t, service, "coleco-frogger")
	if errors.Is(err, meshcontent.ErrUnboundNode) || errors.Is(err, meshcontent.ErrLeaseNotFree) {
		t.Fatalf("err %v", err)
	}
	if boundName != "spare" || service.selectedTarget != "spare" {
		t.Fatalf("bound %q selected %q err %v", boundName, service.selectedTarget, err)
	}
	if service.meshExecute.BoundNode != "node-b" || service.meshExecute.Executor != next {
		t.Fatalf("session bound %q exec %v", service.meshExecute.BoundNode, service.meshExecute.Executor)
	}
	if len(next.pulls) != 1 || next.pulls[0] != cart || len(old.pulls) != 0 {
		t.Fatalf("next %+v old %+v", next.pulls, old.pulls)
	}
	if got := service.MeshSessionPlacement(); got != (MeshPlacement{Execute: "node-b", DisplaySink: "node-b", InputSource: "node-b"}) {
		t.Fatalf("placement %+v", got)
	}
	if !service.meshEnsure || service.meshEnsureConfig {
		t.Fatalf("ensure %v config %v", service.meshEnsure, service.meshEnsureConfig)
	}
	if spare.claims != 1 || spare.releases != 0 {
		t.Fatalf("claims %d releases %d", spare.claims, spare.releases)
	}
}

func TestPlacementSameNodeDoesNotRebind(t *testing.T) {
	service, exec := placementBoundService(t, true)
	spare := &acquiringMeshClient{}
	service.targetClients["spare"] = spare
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{
			placeFPGACandidate("node-b", true),
			placeFPGACandidate("node-a", true),
		},
		OverrideNodeID: "node-a",
	})
	assertPlacementKeptBind(t, service, exec, MeshPlacement{
		Execute: "node-a", DisplaySink: "node-a", InputSource: "node-a",
	})
	if spare.claims != 0 {
		t.Fatalf("same-node claimed the other kit %d", spare.claims)
	}
	if service.selectedTarget != "dev" || service.meshExecute.Executor != exec {
		t.Fatalf("session moved target %q exec %v", service.selectedTarget, service.meshExecute.Executor)
	}
}

func TestPlacementNativeSelectionDoesNotRebind(t *testing.T) {
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
	service.targets = []TargetConfig{
		{Name: "dev", Enabled: true, TargetID: "host-a"},
		{Name: "spare", Enabled: true, TargetID: "host-b"},
	}
	service.displayMemory = meshpref.New()
	service.catalog = &stableCoreCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, id: entry.TitleID}
	spare := &acquiringMeshClient{}
	service.targetClients["spare"] = spare
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		OverrideNodeID: "host-b",
		Candidates: []meshplace.Candidate{
			{NodeID: "host-a", MeshMajorOK: true, Execute: []string{meshcontent.ExecuteNativeEmu}},
			{NodeID: "host-b", MeshMajorOK: true, Execute: []string{meshcontent.ExecuteNativeEmu}, DisplaySink: true, InputSource: true},
		},
	})
	boundName, err := launchAndObserveBind(t, service, entry.TitleID)
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if boundName != "" || spare.claims != 0 {
		t.Fatalf("bound %q claims %d", boundName, spare.claims)
	}
	if service.meshExecute.BoundNode != "host-a" || service.meshExecute.Executor != exec || service.selectedTarget != "dev" {
		t.Fatalf("session moved bound %q target %q", service.meshExecute.BoundNode, service.selectedTarget)
	}
	if got := service.MeshSessionPlacement(); got != (MeshPlacement{}) {
		t.Fatalf("recorded %+v", got)
	}
	if service.meshEnsure {
		t.Fatal("ensure flipped")
	}
}

func TestPlacementRebindLeaseConflictDoesNotSteal(t *testing.T) {
	t.Run("claim rejected", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		spare := &busyMeshClient{}
		service.targetClients["spare"] = spare
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
		})
		boundName, err := launchAndObserveBind(t, service, "coleco-frogger")
		var api *protocol.APIError
		if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
			t.Fatalf("err %v", err)
		}
		if boundName != "" || spare.claims != 1 {
			t.Fatalf("bound %q claims %d", boundName, spare.claims)
		}
		if service.meshExecute.BoundNode != "node-a" || service.meshExecute.Executor != exec || service.selectedTarget != "dev" {
			t.Fatalf("session moved bound %q target %q", service.meshExecute.BoundNode, service.selectedTarget)
		}
		if got := service.MeshSessionPlacement(); got != (MeshPlacement{}) {
			t.Fatalf("recorded %+v", got)
		}
		if service.meshEnsure {
			t.Fatal("ensure flipped")
		}
	})

	t.Run("second shell sees in use", func(t *testing.T) {
		board := &leaseBoard{}
		first, firstExec := placementBoundService(t, true)
		holder := &boardMeshClient{id: "shell-a", board: board}
		first.targetClients["spare"] = holder
		first.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
		})
		if _, err := launchAndObserveBind(t, first, "coleco-frogger"); errors.Is(err, meshcontent.ErrUnboundNode) {
			t.Fatal(err)
		}
		if board.holderID() != "shell-a" || holder.claims != 1 {
			t.Fatalf("holder %q claims %d", board.holderID(), holder.claims)
		}

		second, secondExec := placementBoundService(t, true)
		other := &boardMeshClient{id: "shell-b", board: board}
		second.targetClients["spare"] = other
		second.connection = TargetConnection{State: "busy", TargetID: "node-b", Owner: "shell-a"}
		second.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
		})
		boundName, err := launchAndObserveBind(t, second, "coleco-frogger")
		var api *protocol.APIError
		if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied {
			t.Fatalf("err %v", err)
		}
		if boundName != "" || other.claims != 0 {
			t.Fatalf("bound %q claims %d", boundName, other.claims)
		}
		if board.holderID() != "shell-a" {
			t.Fatalf("holder %q", board.holderID())
		}
		if second.meshExecute.BoundNode != "node-a" || second.meshExecute.Executor != secondExec || second.selectedTarget != "dev" {
			t.Fatalf("second session moved bound %q target %q", second.meshExecute.BoundNode, second.selectedTarget)
		}
		if len(secondExec.pulls) != 0 || len(firstExec.pulls) != 0 {
			t.Fatalf("pulls first %+v second %+v", firstExec.pulls, secondExec.pulls)
		}
	})

	t.Run("generation mismatch does not take over", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		spare := &acquiringMeshClient{fakeServiceClient: fakeServiceClient{meshLeaseGeneration: "gen-owned"}}
		service.targetClients["spare"] = spare
		service.connection = TargetConnection{
			State: "ready", TargetID: "node-b",
			leaseSeen: true, leaseOwned: true, leaseGeneration: "other-gen",
		}
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
		})
		_, err := service.Launch(context.Background(), "coleco-frogger", nil)
		if !errors.Is(err, meshcontent.ErrLeaseNotFree) {
			t.Fatalf("err %v", err)
		}
		if spare.claims != 0 || service.meshExecute.Executor != exec || service.selectedTarget != "dev" {
			t.Fatalf("claims %d target %q exec %v", spare.claims, service.selectedTarget, service.meshExecute.Executor)
		}
	})
}

func TestPlacementRebindPreviewIsNotTheDisplaySink(t *testing.T) {
	service, exec := placementBoundService(t, true)
	spare := &releasingMeshClient{}
	menu := meshplace.Candidate{
		NodeID:      "menu-host",
		MeshMajorOK: true,
		Execute:     []string{meshcontent.ExecuteNativeEmu},
		InputSource: true,
	}
	service.SetDisplayPreference("menu-host")
	assertPlacementRebound(t, service, exec, spare, &MeshPlacementAsk{
		Candidates: []meshplace.Candidate{menu, placeFPGACandidate("node-b", true)},
	}, MeshPlacement{Execute: "node-b", DisplaySink: "node-b", InputSource: "node-b"})
	got := service.MeshSessionPlacement()
	if got.DisplaySink == "menu-host" || got.InputSource == "menu-host" || got.Execute == "menu-host" {
		t.Fatalf("menu host became the sink %+v", got)
	}
}

func TestCrossKitSelectedReadyLaunchesOnThatKit(t *testing.T) {
	bios := meshcontent.SumSHA256([]byte("bios"))
	cart := meshcontent.SumSHA256([]byte("cart"))
	pkg := repeatHex(32)
	entry := meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: pkg, ABI: "fes.application", Major: 1}),
			meshcontent.BIOSSlot(bios),
			meshcontent.PrimaryMediaSlot(cart),
		},
	}
	service := placementReadyService(entry, pkg)
	service.targets = append(service.targets, TargetConfig{Name: "spare", Enabled: true, TargetID: "kit-b"})
	spare := &releasingMeshClient{}
	service.targetClients["spare"] = spare
	service.SetDisplayPreference("kit-b")
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{
			placeFPGACandidate("kit-b", true),
			placeFPGACandidate("kit-a", true),
		},
	})
	got := mustPlacementReady(t, service, entry.TitleID)
	if !got.Ready || got.Block != meshcontent.BlockNone || got.NextAction != "" || got.Placement != meshplace.OutcomeSelected {
		t.Fatalf("ready %#v", got)
	}
	sources := map[string]bool{}
	for _, id := range entry.ContentIDs() {
		sources[id.String()] = true
	}
	next := &meshLaunchExecutor{
		node:    "kit-b",
		sources: sources,
		abis:    []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service.meshEnsure = true
	service.meshInstalled = meshTargetIdentityOf(service.selectedTarget, targetByName(service.targets, service.selectedTarget))
	service.meshPlacementExecutors = map[string]meshcontent.Executor{"kit-b": next}
	service.catalog = &stableCoreCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, id: entry.TitleID}
	boundName, err := launchAndObserveBind(t, service, entry.TitleID)
	if errors.Is(err, meshcontent.ErrUnboundNode) || errors.Is(err, meshcontent.ErrLeaseNotFree) {
		t.Fatalf("err %v", err)
	}
	if boundName != "spare" || service.meshExecute.BoundNode != "kit-b" || service.meshExecute.Executor != next {
		t.Fatalf("bound %q session %q exec %v err %v", boundName, service.meshExecute.BoundNode, service.meshExecute.Executor, err)
	}
	if len(next.pulls) != len(entry.ContentIDs()) {
		t.Fatalf("pulls %+v", next.pulls)
	}
	if recorded := service.MeshSessionPlacement(); recorded.Execute != "kit-b" || recorded.DisplaySink != "kit-b" || recorded.InputSource != "kit-b" {
		t.Fatalf("placement %+v", recorded)
	}
	if !service.meshEnsure || service.meshEnsureConfig {
		t.Fatalf("ensure %v config %v", service.meshEnsure, service.meshEnsureConfig)
	}
}

func TestPlacementRebindDialsTheSelectedKit(t *testing.T) {
	const nodeB = "84ed0a60-2b23-5ba6-b931-bac5f71187ab"
	var hits atomic.Int32
	server := meshNodeServer(&hits, nodeB, "token-b")
	defer server.Close()
	service, old := placementBoundService(t, true)
	service.targets[1] = TargetConfig{Name: "spare", Enabled: true, Address: server.URL, Agent: "token-b", TargetID: nodeB}
	service.meshEnsure = true
	service.meshInstalled = meshTargetIdentityOf(service.selectedTarget, targetByName(service.targets, service.selectedTarget))
	spare := &releasingMeshClient{}
	service.targetClients["spare"] = spare
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate(nodeB, true)},
	})
	_, err := service.Launch(context.Background(), "coleco-frogger", nil)
	if errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatal(err)
	}
	if hits.Load() == 0 {
		t.Fatal("selected kit was not dialed")
	}
	if service.meshExecute.BoundNode != nodeB || service.meshExecute.Executor == nil || service.meshExecute.Executor.NodeID() != nodeB {
		t.Fatalf("session bound %q exec %v", service.meshExecute.BoundNode, service.meshExecute.Executor)
	}
	if service.meshExecute.Executor == old || len(old.pulls) != 0 {
		t.Fatalf("old executor was used pulls %+v", old.pulls)
	}
	if service.selectedTarget != "spare" || !service.meshEnsure || service.meshEnsureConfig {
		t.Fatalf("selected %q ensure %v config %v", service.selectedTarget, service.meshEnsure, service.meshEnsureConfig)
	}
}

func TestPlacementRebindReleasesClaimWhenEnsureDoesNotStart(t *testing.T) {
	service, _ := placementBoundService(t, false)
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
	if spare.claims != 1 || spare.releases != 1 || len(next.pulls) != 0 {
		t.Fatalf("claims %d releases %d pulls %+v", spare.claims, spare.releases, next.pulls)
	}
}

type busyMeshClient struct {
	fakeServiceClient
	claims int
}

func (c *busyMeshClient) AcquireContentPullLease(context.Context) error {
	c.claims++
	return &protocol.APIError{Code: "KIT_LEASE_BUSY", Message: "KIT_LEASE_BUSY"}
}

type leaseBoard struct {
	mu     sync.Mutex
	holder string
	gen    string
}

func (b *leaseBoard) holderID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.holder
}

type boardMeshClient struct {
	fakeServiceClient
	id     string
	board  *leaseBoard
	claims int
}

func (c *boardMeshClient) MeshKitLease() (bool, string) {
	if c == nil || c.board == nil {
		return false, ""
	}
	c.board.mu.Lock()
	defer c.board.mu.Unlock()
	if c.board.holder == c.id && c.board.gen != "" {
		return true, c.board.gen
	}
	return false, ""
}

func (c *boardMeshClient) AcquireContentPullLease(context.Context) error {
	c.claims++
	c.board.mu.Lock()
	defer c.board.mu.Unlock()
	if c.board.holder != "" && c.board.holder != c.id {
		return &protocol.APIError{Code: "KIT_LEASE_BUSY", Message: "KIT_LEASE_BUSY"}
	}
	c.board.holder = c.id
	c.board.gen = "gen-shared"
	return nil
}

func repeatHex(n int) string {
	out := make([]byte, n*2)
	for i := range out {
		out[i] = 'a'
	}
	return string(out)
}

func TestLaunchWithoutPlacementAskKeepsBind(t *testing.T) {
	t.Run("phase 0 follows the live target", func(t *testing.T) {
		original := &fakeServiceClient{}
		replacement := &fakeServiceClient{}
		service := &Service{
			targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", TargetID: "node-a"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", TargetID: "node-b"},
			},
			selectedTarget: "dev",
			targetClients:  map[string]serviceClient{"dev": original},
			hostExecutor:   &fakeHostExecutor{},
			executionResolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) {
				return ExecutionHostOnly, nil
			}),
			displayMemory: meshpref.New(),
		}
		service.SetDisplayPreference("node-b")
		service.catalog = &seamOffTargetCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, service: service, replacement: replacement, next: "spare"}
		var bound string
		launchPinnedTargetBoundHook = func(name string) { bound = name }
		t.Cleanup(func() { launchPinnedTargetBoundHook = nil })

		_, err := service.Launch(context.Background(), "snes-mario", nil)
		if bound != "spare" {
			t.Fatalf("bound %q err %v selected %q", bound, err, service.selectedTarget)
		}
		if errors.Is(err, meshcontent.ErrUnboundNode) {
			t.Fatalf("placement refused a launch that did not ask: %v", err)
		}
		if got := service.MeshSessionPlacement(); got != (MeshPlacement{}) {
			t.Fatalf("recorded %+v", got)
		}
		if service.meshEnsure {
			t.Fatal("ensure flipped")
		}
	})

	t.Run("mesh session still ensures on the bound node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetDisplayPreference("node-b")
		assertUnaskedLaunchKeepsBind(t, service, exec)
	})

	t.Run("cleared ask is not placement", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates:     []meshplace.Candidate{placeFPGACandidate("node-b", true)},
			OverrideNodeID: "node-b",
		})
		service.SetMeshPlacementAsk(nil)
		assertUnaskedLaunchKeepsBind(t, service, exec)
	})

	t.Run("unresolved selection does not rebind", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("node-a", true),
				placeFPGACandidate("node-b", true),
			},
		})
		assertUnaskedLaunchKeepsBind(t, service, exec)
	})
}

func placementBoundService(t *testing.T, withSource bool) (*Service, *meshLaunchExecutor) {
	t.Helper()
	entry, cart := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{
		node: "node-a",
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	if withSource {
		exec.sources = map[string]bool{cart.String(): true}
	}
	service := meshTargetService(exec, "node-a", entry)
	service.displayMemory = meshpref.New()
	service.catalog = &stableCoreCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, id: entry.TitleID}
	return service, exec
}

func assertPlacementKeptBind(t *testing.T, service *Service, exec *meshLaunchExecutor, want MeshPlacement) {
	t.Helper()
	boundName, err := launchAndObserveBind(t, service, "coleco-frogger")
	if errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("matching placement refused: %v", err)
	}
	if boundName != "dev" {
		t.Fatalf("bound %q err %v", boundName, err)
	}
	if got := service.MeshSessionPlacement(); got != want {
		t.Fatalf("placement %+v want %+v", got, want)
	}
	if service.meshExecute.BoundNode != "node-a" || service.meshExecute.Executor != exec {
		t.Fatalf("session moved bound %q exec %v", service.meshExecute.BoundNode, service.meshExecute.Executor)
	}
	if service.selectedTarget != "dev" {
		t.Fatalf("selected %q", service.selectedTarget)
	}
	if len(exec.pulls) != 1 {
		t.Fatalf("pulls %+v", exec.pulls)
	}
	if exec.node != "node-a" {
		t.Fatalf("executor node %q", exec.node)
	}
	if service.meshEnsure {
		t.Fatal("ensure flipped")
	}
}

func assertPlacementRebound(t *testing.T, service *Service, old *meshLaunchExecutor, spare *releasingMeshClient, ask *MeshPlacementAsk, want MeshPlacement) {
	t.Helper()
	service.targetClients["spare"] = spare
	service.SetMeshPlacementAsk(ask)
	boundName, err := launchAndObserveBind(t, service, "coleco-frogger")
	if errors.Is(err, meshcontent.ErrUnboundNode) || errors.Is(err, meshcontent.ErrLeaseNotFree) {
		t.Fatalf("rebind refused: %v", err)
	}
	if boundName != "spare" || service.selectedTarget != "spare" {
		t.Fatalf("bound %q selected %q err %v", boundName, service.selectedTarget, err)
	}
	if got := service.MeshSessionPlacement(); got != want {
		t.Fatalf("placement %+v want %+v", got, want)
	}
	if service.meshExecute.BoundNode != "node-b" || service.meshExecute.Executor != nil {
		t.Fatalf("phase 0 session bound %q exec %v", service.meshExecute.BoundNode, service.meshExecute.Executor)
	}
	if len(old.pulls) != 0 || len(old.links) != 0 {
		t.Fatalf("ensure ran on the old executor pulls %+v links %+v", old.pulls, old.links)
	}
	if spare.claims != 1 || spare.releases != 0 {
		t.Fatalf("claims %d releases %d", spare.claims, spare.releases)
	}
	if service.meshEnsure || service.meshEnsureConfig {
		t.Fatal("ensure flipped")
	}
}

func assertUnaskedLaunchKeepsBind(t *testing.T, service *Service, exec *meshLaunchExecutor) {
	t.Helper()
	boundName, err := launchAndObserveBind(t, service, "coleco-frogger")
	if errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("unasked launch refused: %v", err)
	}
	if boundName != "dev" {
		t.Fatalf("bound %q err %v", boundName, err)
	}
	if got := service.MeshSessionPlacement(); got != (MeshPlacement{}) {
		t.Fatalf("recorded %+v", got)
	}
	if service.meshExecute.BoundNode != "node-a" || service.meshExecute.Executor != exec {
		t.Fatalf("session moved bound %q exec %v", service.meshExecute.BoundNode, service.meshExecute.Executor)
	}
	if len(exec.pulls) != 1 {
		t.Fatalf("pulls %+v", exec.pulls)
	}
	if service.meshEnsure {
		t.Fatal("ensure flipped")
	}
}

func launchAndObserveBind(t *testing.T, service *Service, gameID string) (string, error) {
	t.Helper()
	var bound string
	launchPinnedTargetBoundHook = func(name string) { bound = name }
	t.Cleanup(func() { launchPinnedTargetBoundHook = nil })
	_, err := service.Launch(context.Background(), gameID, nil)
	return bound, err
}

func phase0PlacementService() *Service {
	return &Service{
		targets:        []TargetConfig{{Name: "dev", Enabled: true, TargetID: "node-a", Address: "http://192.0.2.10:8182"}},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": &fakeServiceClient{}},
		hostExecutor:   &fakeHostExecutor{},
		executionResolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) {
			return ExecutionHostOnly, nil
		}),
		displayMemory: meshpref.New(),
		catalog: &fixedGameCatalog{game: catalog.Game{
			ID: "coleco-frogger", System: protocol.SystemSNES, State: catalog.SourceStateAvailable, RootOnline: true,
		}},
	}
}

type fixedGameCatalog struct {
	*fakeServiceCatalog
	game catalog.Game
}

func (c *fixedGameCatalog) Game(context.Context, string) (catalog.Game, error) {
	if c == nil {
		return catalog.Game{}, errors.New("missing catalog")
	}
	return c.game, nil
}

func placeFPGACandidate(id string, input bool) meshplace.Candidate {
	return meshplace.Candidate{
		NodeID:      id,
		MeshMajorOK: true,
		Execute:     []string{meshcontent.ExecuteFPGANative},
		DisplaySink: true,
		InputSource: input,
		ABIs:        []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
}
