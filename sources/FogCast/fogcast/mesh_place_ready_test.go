package fogcast

import (
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/internal/meshpref"
)

func TestGamesPlacementPredicate(t *testing.T) {
	bios := meshcontent.SumSHA256([]byte("bios"))
	cart := meshcontent.SumSHA256([]byte("cart"))
	pkg := strings.Repeat("ab", 32)
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

	t.Run("selected stays ready without naming a node", func(t *testing.T) {
		s := placementReadyService(entry, pkg)
		s.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("kit-a", true)},
		})
		got := mustPlacementReady(t, s, entry.TitleID)
		if !got.Ready || got.Block != meshcontent.BlockNone || got.NextAction != "" || got.Placement != meshplace.OutcomeSelected {
			t.Fatalf("selected %#v", got)
		}
		if recorded := s.MeshSessionPlacement(); recorded != (MeshPlacement{}) {
			t.Fatalf("games read recorded %+v", recorded)
		}
		if s.meshEnsure {
			t.Fatal("ensure flipped")
		}
	})

	t.Run("preference selects one of several kits", func(t *testing.T) {
		s := placementReadyService(entry, pkg)
		s.SetDisplayPreference("kit-a")
		s.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("kit-b", true),
				placeFPGACandidate("kit-a", true),
			},
		})
		got := mustPlacementReady(t, s, entry.TitleID)
		if !got.Ready || got.Placement != meshplace.OutcomeSelected || got.Block != meshcontent.BlockNone {
			t.Fatalf("preference %#v", got)
		}
		if recorded := s.MeshSessionPlacement(); recorded != (MeshPlacement{}) {
			t.Fatalf("games read recorded %+v", recorded)
		}
	})

	t.Run("unresolved is not ready and names no node", func(t *testing.T) {
		s := placementReadyService(entry, pkg)
		s.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("kit-a", true),
				placeFPGACandidate("kit-b", true),
			},
		})
		got := mustPlacementReady(t, s, entry.TitleID)
		assertPlacementBlocked(t, got, meshplace.OutcomeUnresolved, BlockPlacementUnresolved)
		if got.Block == meshcontent.BlockVersionSkew || got.Block == meshcontent.BlockLeaseHeld || got.NextAction == "bind_executor" {
			t.Fatalf("unresolved collapsed %#v", got)
		}
		if recorded := s.MeshSessionPlacement(); recorded != (MeshPlacement{}) {
			t.Fatalf("unresolved recorded %+v", recorded)
		}
	})

	t.Run("fail closed is not ready and names no node", func(t *testing.T) {
		s := placementReadyService(entry, pkg)
		s.SetMeshPlacementAsk(&MeshPlacementAsk{MissingRequiredSlot: true, Candidates: []meshplace.Candidate{
			placeFPGACandidate("kit-a", true),
		}})
		got := mustPlacementReady(t, s, entry.TitleID)
		assertPlacementBlocked(t, got, meshplace.OutcomeFailClosed, BlockPlacementFailClosed)
		if got.Block == meshcontent.BlockVersionSkew || got.Block == meshcontent.BlockLeaseHeld || got.NextAction == "bind_executor" {
			t.Fatalf("fail closed collapsed %#v", got)
		}
		if recorded := s.MeshSessionPlacement(); recorded != (MeshPlacement{}) {
			t.Fatalf("fail closed recorded %+v", recorded)
		}
		if s.meshEnsure {
			t.Fatal("ensure flipped")
		}
	})

	t.Run("version skew stays version skew", func(t *testing.T) {
		s := placementReadyService(entry, pkg)
		s.meshNodes[0].Mesh = "2.0"
		s.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("kit-a", true)},
		})
		got := mustPlacementReady(t, s, entry.TitleID)
		if got.Ready || got.Block != meshcontent.BlockVersionSkew || got.NextAction != "resolve_version" || got.Placement != meshplace.OutcomeSelected {
			t.Fatalf("skew %#v", got)
		}
	})

	t.Run("in use stays lease held", func(t *testing.T) {
		s := placementReadyService(entry, pkg)
		s.connection = TargetConnection{State: "busy", TargetID: "kit-a"}
		s.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{
				placeFPGACandidate("kit-a", true),
				placeFPGACandidate("kit-b", true),
			},
		})
		got := mustPlacementReady(t, s, entry.TitleID)
		if got.Ready || got.Block != meshcontent.BlockLeaseHeld || got.NextAction != "wait_for_lease" || got.Placement != meshplace.OutcomeUnresolved {
			t.Fatalf("in use %#v", got)
		}
	})

	t.Run("no ask keeps phase 2 ready", func(t *testing.T) {
		s := placementReadyService(entry, pkg)
		got := mustPlacementReady(t, s, entry.TitleID)
		if !got.Ready || got.Placement != "" || got.Block != meshcontent.BlockNone {
			t.Fatalf("unasked %#v", got)
		}
	})

	t.Run("seam off ignores the ask", func(t *testing.T) {
		s := &Service{displayMemory: meshpref.New()}
		s.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("kit-a", true)},
		})
		got, on := s.GamesMeshReady(context.Background(), []string{entry.TitleID})
		if on || len(got) != 0 {
			t.Fatalf("seam off %#v on %v", got, on)
		}
		if s.meshEnsure {
			t.Fatal("ensure flipped")
		}
	})

	t.Run("execute advertisement does not become ready", func(t *testing.T) {
		s := placementReadyService(entry, pkg)
		s.meshExecute.Executor.(*readyExec).packages = nil
		s.SetMeshPlacementAsk(&MeshPlacementAsk{
			Candidates: []meshplace.Candidate{placeFPGACandidate("kit-a", true)},
		})
		got := mustPlacementReady(t, s, entry.TitleID)
		if got.Ready || got.Block != meshcontent.BlockNoExecutor || got.Placement != meshplace.OutcomeSelected {
			t.Fatalf("advertisement %#v", got)
		}
	})
}

func placementReadyService(entry meshcontent.Entry, pkg string) *Service {
	held := map[string]meshcontent.SlotState{}
	for _, id := range entry.ContentIDs() {
		held[id.String()] = meshcontent.StatePresent
	}
	exec := &readyExec{
		meshLaunchExecutor: &meshLaunchExecutor{
			node: "kit-a",
			held: held,
			abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
		},
		packages: []string{pkg},
	}
	s := &Service{
		targets:        []TargetConfig{{Name: "dev", Enabled: true, TargetID: "kit-a"}},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": &acquiringMeshClient{}},
		displayMemory:  meshpref.New(),
		meshNodes: []MeshNode{
			{NodeID: "kit-a", TargetID: "kit-a", Mesh: discovery.MeshProtocol},
			{NodeID: "other", TargetID: "other", Mesh: discovery.MeshProtocol, Capabilities: discovery.Capabilities{
				Execute: []discovery.Execute{{Kind: discovery.ExecuteFPGANative}},
			}},
		},
	}
	s.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "kit-a",
		Executor:  exec,
		Entry: func(id string) (meshcontent.Entry, bool) {
			if id == entry.TitleID {
				return entry, true
			}
			return meshcontent.Entry{}, false
		},
	})
	return s
}

func mustPlacementReady(t *testing.T, s *Service, id string) GameMeshReady {
	t.Helper()
	got, on := s.GamesMeshReady(context.Background(), []string{id})
	if !on {
		t.Fatal("seam off")
	}
	decision, ok := got[id]
	if !ok {
		t.Fatalf("missing %s in %#v", id, got)
	}
	return decision
}

func assertPlacementBlocked(t *testing.T, got GameMeshReady, outcome meshplace.Outcome, block meshcontent.Block) {
	t.Helper()
	if got.Ready || got.Placement != outcome || got.Block != block || got.NextAction != "unavailable" {
		t.Fatalf("got %#v want %s %s", got, outcome, block)
	}
}
