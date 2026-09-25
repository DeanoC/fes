package fogcast

import (
	"context"
	"errors"
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

func TestPlacementOnAnotherNodeRefusesBeforeBind(t *testing.T) {
	t.Run("only other eligible node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.meshExecute.Placement = MeshPlacement{Execute: "node-a", DisplaySink: "node-a", InputSource: "node-a"}
		refusePlacement(t, service, exec, &MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
		})
		if got := service.MeshSessionPlacement(); got.Execute != "node-a" || got.DisplaySink != "node-a" || got.InputSource != "node-a" {
			t.Fatalf("previous record changed %+v", got)
		}
	})

	t.Run("preference selects another node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		service.SetDisplayPreference("node-b")
		refusePlacement(t, service, exec, &MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("node-a", true),
				placeFPGACandidate("node-b", true),
			},
		})
		if got := service.MeshSessionPlacement(); got != (MeshPlacement{}) {
			t.Fatalf("recorded %+v", got)
		}
	})

	t.Run("override selects another node", func(t *testing.T) {
		service, exec := placementBoundService(t, true)
		refusePlacement(t, service, exec, &MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("node-a", true),
				placeFPGACandidate("node-b", true),
			},
			OverrideNodeID: "node-b",
		})
		if got := service.MeshSessionPlacement(); got != (MeshPlacement{}) {
			t.Fatalf("recorded %+v", got)
		}
	})

	t.Run("phase 0 selected target is not the selection", func(t *testing.T) {
		service := phase0PlacementService()
		entry, _ := fpgaMeshEntry("coleco-frogger")
		service.SetMeshExecuteSession(MeshExecuteSession{
			Entry: func(string) (meshcontent.Entry, bool) { return entry, true },
		})
		var bound string
		launchPinnedTargetBoundHook = func(name string) { bound = name }
		t.Cleanup(func() { launchPinnedTargetBoundHook = nil })
		service.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("node-b", true)},
		})
		_, err := service.Launch(context.Background(), entry.TitleID, nil)
		if !errors.Is(err, meshcontent.ErrUnboundNode) {
			t.Fatalf("err %v", err)
		}
		if bound != "" {
			t.Fatalf("bound %q", bound)
		}
		if service.selectedTarget != "dev" || service.meshExecute.Executor != nil || service.meshExecute.BoundNode != "" {
			t.Fatalf("session moved target %q bound %q exec %v", service.selectedTarget, service.meshExecute.BoundNode, service.meshExecute.Executor)
		}
		if got := service.MeshSessionPlacement(); got != (MeshPlacement{}) {
			t.Fatalf("recorded %+v", got)
		}
		if service.meshEnsure {
			t.Fatal("ensure flipped")
		}
	})
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

func refusePlacement(t *testing.T, service *Service, exec *meshLaunchExecutor, ask *MeshPlacementAsk) {
	t.Helper()
	service.SetMeshPlacementAsk(ask)
	boundName, err := launchAndObserveBind(t, service, "coleco-frogger")
	if !errors.Is(err, meshcontent.ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if boundName != "" {
		t.Fatalf("bound %q", boundName)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("refused launch pulled %+v linked %+v", exec.pulls, exec.links)
	}
	if service.meshExecute.BoundNode != "node-a" || service.meshExecute.Executor != exec {
		t.Fatalf("session moved bound %q exec %v", service.meshExecute.BoundNode, service.meshExecute.Executor)
	}
	if service.selectedTarget != "dev" {
		t.Fatalf("selected %q", service.selectedTarget)
	}
	if service.meshEnsure {
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
