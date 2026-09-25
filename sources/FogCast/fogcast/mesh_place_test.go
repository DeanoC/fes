package fogcast

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/internal/meshpref"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
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
		owned, _ := spare.MeshKitLease()
		if spare.claims != 1 || spare.releases != 1 || owned {
			t.Fatalf("claims %d releases %d owned %v", spare.claims, spare.releases, owned)
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
	if spare.claims != 1 || spare.releases != 1 {
		t.Fatalf("claims %d releases %d", spare.claims, spare.releases)
	}
}

func TestPlacementSameNodeDoesNotRebind(t *testing.T) {
	service, exec := placementBoundService(t, true)
	spare := &acquiringMeshClient{}
	service.targetClients["spare"] = spare
	var origins []string
	service.SetTargetOrigin(func(cfg TargetConfig, _ *targetclient.KitLease) { origins = append(origins, cfg.Name) })
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
	if len(origins) != 0 {
		t.Fatalf("same-node notified origin %+v", origins)
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

func TestPlacementRebindPinsTheClaimedKitThroughBind(t *testing.T) {
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
	service.executionResolver = ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) {
		service.selectedTarget = "dev"
		return ExecutionHostOnly, nil
	})
	boundName, err := launchAndObserveBind(t, service, entry.TitleID)
	if boundName != "spare" {
		t.Fatalf("bound %q selected %q err %v", boundName, service.selectedTarget, err)
	}
	if service.selectedTarget != "dev" {
		t.Fatalf("selected %q", service.selectedTarget)
	}
	if service.meshEnsure || service.meshEnsureConfig {
		t.Fatal("ensure flipped")
	}
	owned, _ := spare.MeshKitLease()
	if spare.claims != 1 || spare.releases != 1 || owned {
		t.Fatalf("claims %d releases %d owned %v", spare.claims, spare.releases, owned)
	}
}

func TestPlacementRebindRejectsAReplacedClient(t *testing.T) {
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
	service.executionResolver = ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) {
		service.targetClients["spare"] = &fakeServiceClient{}
		return ExecutionHostOnly, nil
	})
	_, err := launchAndObserveBind(t, service, entry.TitleID)
	var drifted *LaunchSnapshotError
	if !errors.As(err, &drifted) || drifted.Reason != launchSnapshotClientChanged {
		t.Fatalf("err %v", err)
	}
	owned, _ := spare.MeshKitLease()
	if spare.claims != 1 || spare.releases != 1 || owned {
		t.Fatalf("claims %d releases %d owned %v", spare.claims, spare.releases, owned)
	}
}

func TestPlacementRebindNotifiesTargetOrigin(t *testing.T) {
	service, _ := placementBoundService(t, true)
	base, err := url.Parse("http://192.0.2.11:8182")
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(base, "token-b", nil, "host", "placement")
	spare := &leasingMeshClient{lease: lease}
	service.targetClients["spare"] = spare
	var origins []TargetConfig
	var originLease *targetclient.KitLease
	service.SetTargetOrigin(func(cfg TargetConfig, got *targetclient.KitLease) {
		origins = append(origins, cfg)
		originLease = got
	})
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
	})
	_, err = service.Launch(context.Background(), "coleco-frogger", nil)
	if errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatal(err)
	}
	if len(origins) != 1 || origins[0].Name != "spare" || origins[0].TargetID != "node-b" {
		t.Fatalf("origins %+v err %v", origins, err)
	}
	if originLease != lease {
		t.Fatalf("origin lease %p, kit lease %p", originLease, lease)
	}
	if service.meshEnsure || service.meshEnsureConfig {
		t.Fatal("ensure flipped")
	}
}

func TestOverlappingPlacementClaimsReleaseOnlyTheLastHolder(t *testing.T) {
	cfg := TargetConfig{Name: "spare", Enabled: true, TargetID: "node-b", Address: "http://192.0.2.11:8182"}
	service := phase0PlacementService()
	service.targets = append(service.targets, cfg)
	spare := &releasingMeshClient{}
	first, generation, err := service.claimPlacementKit(context.Background(), spare, cfg, "node-b")
	if err != nil || !first || generation == "" {
		t.Fatalf("first claimed %v generation %q err %v", first, generation, err)
	}
	second, adopted, err := service.claimPlacementKit(context.Background(), spare, cfg, "node-b")
	if err != nil || !second || adopted != generation {
		t.Fatalf("second claimed %v generation %q err %v", second, adopted, err)
	}
	if spare.claims != 1 {
		t.Fatalf("claims %d", spare.claims)
	}
	snap := launchSnapshot{client: spare, placementClaimed: true, placementGeneration: generation}
	if !service.releaseClaimedContentLease(snap) {
		t.Fatal("first failure did not settle")
	}
	owned, held := spare.MeshKitLease()
	if spare.releases != 0 || !owned || held != generation {
		t.Fatalf("first failure released the shared grant: releases %d owned %v generation %q", spare.releases, owned, held)
	}
	settled := false
	service.settlePlacementClaim(launchSnapshot{
		client: spare, placementClaimed: true, placementGeneration: generation, placementClaimSettled: &settled,
	})
	if !settled || spare.releases != 0 {
		t.Fatalf("execution start settled %v releases %d", settled, spare.releases)
	}
	if !service.releaseClaimedContentLease(snap) || spare.releases != 0 {
		t.Fatalf("later failure released a session grant: releases %d", spare.releases)
	}

	other := &releasingMeshClient{}
	left, leftGen, err := service.claimPlacementKit(context.Background(), other, cfg, "node-b")
	if err != nil || !left || leftGen == "" {
		t.Fatalf("left claimed %v generation %q err %v", left, leftGen, err)
	}
	right, rightGen, err := service.claimPlacementKit(context.Background(), other, cfg, "node-b")
	if err != nil || !right || rightGen != leftGen {
		t.Fatalf("right claimed %v generation %q err %v", right, rightGen, err)
	}
	leftSnap := launchSnapshot{client: other, placementClaimed: true, placementGeneration: leftGen}
	if !service.releaseClaimedContentLease(leftSnap) || other.releases != 0 {
		t.Fatalf("left release releases %d", other.releases)
	}
	if !service.releaseClaimedContentLease(leftSnap) {
		t.Fatal("right release did not settle")
	}
	owned, _ = other.MeshKitLease()
	if other.releases != 1 || owned {
		t.Fatalf("last failure releases %d owned %v", other.releases, owned)
	}
}

func TestPlacementRebindKeepsAGrantTheSessionAlreadyHeld(t *testing.T) {
	service := phase0PlacementService()
	service.targets = append(service.targets, TargetConfig{Name: "spare", Enabled: true, TargetID: "node-b", Address: "http://192.0.2.11:8182"})
	spare := &releasingMeshClient{}
	spare.meshLeaseGeneration = "gen-held"
	service.targetClients["spare"] = spare
	entry, _ := fpgaMeshEntry("coleco-frogger")
	service.SetMeshExecuteSession(MeshExecuteSession{
		Entry: func(string) (meshcontent.Entry, bool) { return entry, true },
	})
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
	})
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	if errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatal(err)
	}
	owned, generation := spare.MeshKitLease()
	if spare.claims != 0 || spare.releases != 0 || !owned || generation != "gen-held" {
		t.Fatalf("claims %d releases %d owned %v generation %q err %v", spare.claims, spare.releases, owned, generation, err)
	}
}

func TestPlacementRebindRejectsAMismatchedKitIdentity(t *testing.T) {
	service := phase0PlacementService()
	service.targets = append(service.targets, TargetConfig{Name: "spare", Enabled: true, TargetID: "node-b", Address: "http://192.0.2.11:8182"})
	spare := &identifiedMeshClient{targetID: "node-other"}
	service.targetClients["spare"] = spare
	entry, _ := fpgaMeshEntry("coleco-frogger")
	service.SetMeshExecuteSession(MeshExecuteSession{
		Entry: func(string) (meshcontent.Entry, bool) { return entry, true },
	})
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
	})
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if service.selectedTarget != "dev" {
		t.Fatalf("selected %q", service.selectedTarget)
	}
	if spare.claims != 0 || spare.releases != 0 {
		t.Fatalf("claims %d releases %d", spare.claims, spare.releases)
	}
}

func TestPlacementRebindClaimsTheDiscoveredKit(t *testing.T) {
	const nodeB = "84ed0a60-2b23-5ba6-b931-bac5f71187ab"
	var staleMutations atomic.Int32
	var liveClaims atomic.Int32
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			staleMutations.Add(1)
		}
		_ = json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: "other-node", Ready: true})
	}))
	defer stale.Close()
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/health":
			_ = json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: nodeB, Ready: true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/kit/lease":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "free"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/kit/claim":
			liveClaims.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "lease-b",
				"status": map[string]any{
					"state": "held", "generation": "gen-b", "expires_in_ms": 60000,
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "free"})
		}
	}))
	defer live.Close()
	staleURL, err := url.Parse(stale.URL)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := live.Client()
	lease := targetclient.NewKitLease(staleURL, "token-b", httpClient, "host", "placement")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(staleURL, "token-b", httpClient).WithKitLease(lease)
	service := phase0PlacementService()
	service.targets = append(service.targets, TargetConfig{Name: "spare", Enabled: true, TargetID: nodeB, Address: stale.URL, Agent: "token-b"})
	service.targetClients["spare"] = client
	service.resolveTarget = func(context.Context, string) ([]string, error) { return []string{live.URL}, nil }
	entry, _ := fpgaMeshEntry("coleco-frogger")
	service.SetMeshExecuteSession(MeshExecuteSession{
		Entry: func(string) (meshcontent.Entry, bool) { return entry, true },
	})
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate(nodeB, true)},
	})
	_, err = service.Launch(context.Background(), entry.TitleID, nil)
	if errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatal(err)
	}
	if service.selectedTarget != "spare" {
		t.Fatalf("selected %q err %v", service.selectedTarget, err)
	}
	if staleMutations.Load() != 0 || liveClaims.Load() != 1 {
		t.Fatalf("stale mutations %d live claims %d", staleMutations.Load(), liveClaims.Load())
	}
	if service.meshEnsure || service.meshEnsureConfig {
		t.Fatal("ensure flipped")
	}
}

func TestPlacementRebindRetriesReleaseWhenTheFirstAttemptFails(t *testing.T) {
	service, _ := placementBoundService(t, false)
	next := &meshLaunchExecutor{
		node: "node-b",
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
	service.meshEnsure = true
	service.meshInstalled = meshTargetIdentityOf(service.selectedTarget, targetByName(service.targets, service.selectedTarget))
	service.meshPlacementExecutors = map[string]meshcontent.Executor{"node-b": next}
	spare := &flakyReleaseClient{fails: 1}
	service.targetClients["spare"] = spare
	service.SetMeshPlacementAsk(&MeshPlacementAsk{
		Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
	})
	_, err := service.Launch(context.Background(), "coleco-frogger", nil)
	if !errors.Is(err, meshcontent.ErrContentMissingNoSource) {
		t.Fatalf("err %v", err)
	}
	owned, _ := spare.MeshKitLease()
	if spare.claims != 1 || spare.releases != 1 || owned || len(next.pulls) != 0 {
		t.Fatalf("claims %d releases %d owned %v pulls %+v", spare.claims, spare.releases, owned, next.pulls)
	}
}

func TestPlacementRebindKeepsClaimWhenExecutionStarts(t *testing.T) {
	service, client, entry, inspection := newCoreEntryLaunchFixture(t, libraryPackageFixture(t, "0.1.0"), "Standalone Pong")
	coreID := inspection.Descriptor.Core.ID
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &coreID, CorePackage: &protocol.CorePackageStatus{
		PackageID: inspection.PackageID, Generation: 4,
		ABI: protocol.RuntimeContract{ID: inspection.Descriptor.ABI.ID, Major: 1}, BuildID: inspection.Descriptor.Build.ID, Gamepad: true,
	}}
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return active, nil
	}
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
	boundName, err := launchAndObserveBind(t, service, entry.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if boundName != "spare" || service.activeExecution != ExecutionFPGANative {
		t.Fatalf("bound %q execution %q", boundName, service.activeExecution)
	}
	owned, _ := keeper.MeshKitLease()
	if keeper.claims != 1 || keeper.releases != 0 || !owned {
		t.Fatalf("claims %d releases %d owned %v", keeper.claims, keeper.releases, owned)
	}
	if service.meshEnsure || service.meshEnsureConfig {
		t.Fatal("ensure flipped")
	}
}

type leasingMeshClient struct {
	releasingMeshClient
	lease *targetclient.KitLease
}

func (c *leasingMeshClient) KitLease() *targetclient.KitLease {
	if c == nil {
		return nil
	}
	return c.lease
}

type identifiedMeshClient struct {
	releasingMeshClient
	targetID string
}

func (c *identifiedMeshClient) Health(context.Context) (protocol.Health, error) {
	return protocol.Health{TargetID: c.targetID, APIVersion: "v1", Ready: true}, nil
}

type flakyReleaseClient struct {
	releasingMeshClient
	fails int
}

func (c *flakyReleaseClient) ReleaseContentPullLease(ctx context.Context) error {
	if c.fails > 0 {
		c.fails--
		return errors.New("release failed")
	}
	return c.releasingMeshClient.ReleaseContentPullLease(ctx)
}

type placementKeepClient struct {
	*defaultMediaPackageClient
	claims   int
	releases int
}

func (c *placementKeepClient) AcquireContentPullLease(context.Context) error {
	c.claims++
	c.packageLibraryClient.fakeServiceClient.meshLeaseGeneration = "gen-keep"
	return nil
}

func (c *placementKeepClient) ReleaseContentPullLease(context.Context) error {
	c.releases++
	c.packageLibraryClient.fakeServiceClient.meshLeaseGeneration = ""
	return nil
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
	if spare.claims != 1 || spare.releases != 1 {
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
