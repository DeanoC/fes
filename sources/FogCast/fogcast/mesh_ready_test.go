package fogcast

import (
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

func TestGamesMeshReadyUsesReadyHere(t *testing.T) {
	bios := meshcontent.SumSHA256([]byte("bios"))
	cart := meshcontent.SumSHA256([]byte("cart"))
	pkg := strings.Repeat("ab", 32)
	fpga := meshcontent.Entry{
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
	hostOnly := meshcontent.Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
		Slots:      []meshcontent.Slot{meshcontent.PrimaryMediaSlot(cart)},
	}
	present := map[string]meshcontent.SlotState{
		bios.String(): meshcontent.StatePresent,
		cart.String(): meshcontent.StatePresent,
	}
	newExec := func(held map[string]meshcontent.SlotState, sources map[string]bool, abis []meshcontent.EligibleABI, packages []string) *readyExec {
		return &readyExec{
			meshLaunchExecutor: &meshLaunchExecutor{
				node:    "kit-a",
				held:    held,
				sources: sources,
				abis:    abis,
			},
			packages: packages,
		}
	}
	abi := []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}}
	service := func(exec *readyExec, mesh, bound string, entry meshcontent.Entry) *Service {
		s := &Service{
			targets:        []TargetConfig{{Name: "dev", Enabled: true, TargetID: "kit-a"}},
			selectedTarget: "dev",
			meshNodes: []MeshNode{
				{NodeID: "kit-a", TargetID: "kit-a", Mesh: mesh},
				{NodeID: "other", TargetID: "other", Mesh: discovery.MeshProtocol, Capabilities: discovery.Capabilities{
					Execute: []discovery.Execute{{Kind: discovery.ExecuteFPGANative}},
				}},
			},
		}
		s.targetClients = map[string]serviceClient{"dev": &acquiringMeshClient{}}
		s.SetMeshExecuteSession(MeshExecuteSession{
			BoundNode: bound,
			Executor:  exec,
			Entry: func(id string) (meshcontent.Entry, bool) {
				if id == entry.TitleID {
					return entry, true
				}
				if id == hostOnly.TitleID && entry.TitleID == fpga.TitleID {
					return hostOnly, true
				}
				return meshcontent.Entry{}, false
			},
		})
		return s
	}
	base := func() *Service {
		s := service(newExec(present, nil, abi, []string{pkg}), discovery.MeshProtocol, "kit-a", fpga)
		s.targetClients = map[string]serviceClient{"dev": &acquiringMeshClient{}}
		return s
	}

	t.Run("present claimable lease", func(t *testing.T) {
		got, on := base().GamesMeshReady(context.Background(), []string{fpga.TitleID})
		if !on || !got[fpga.TitleID].Ready || got[fpga.TitleID].Block != meshcontent.BlockNone || got[fpga.TitleID].NextAction != "" {
			t.Fatalf("ready %#v on %v", got[fpga.TitleID], on)
		}
	})
	t.Run("owned grant and generation", func(t *testing.T) {
		s := base()
		s.targetClients = map[string]serviceClient{"dev": &fakeServiceClient{meshLeaseGeneration: "gen-owned"}}
		s.connection = TargetConnection{State: "ready", TargetID: "kit-a", leaseSeen: true, leaseOwned: true, leaseGeneration: "gen-owned"}
		got, on := s.GamesMeshReady(context.Background(), []string{fpga.TitleID})
		if !on || !got[fpga.TitleID].Ready {
			t.Fatalf("owned grant %#v", got[fpga.TitleID])
		}
	})
	t.Run("generation mismatch", func(t *testing.T) {
		s := base()
		s.targetClients = map[string]serviceClient{"dev": &fakeServiceClient{meshLeaseGeneration: "gen-owned"}}
		s.connection = TargetConnection{State: "ready", TargetID: "kit-a", leaseSeen: true, leaseOwned: true, leaseGeneration: "other-gen"}
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockLeaseHeld, "wait_for_lease")
	})
	t.Run("foreign holder", func(t *testing.T) {
		s := base()
		s.connection = TargetConnection{State: "busy", TargetID: "kit-a"}
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockLeaseHeld, "wait_for_lease")
	})
	t.Run("no execute binding", func(t *testing.T) {
		s := service(newExec(present, nil, abi, []string{pkg}), discovery.MeshProtocol, "other", fpga)
		s.targets = append(s.targets, TargetConfig{Name: "other", Enabled: true, TargetID: "other"})
		s.targetClients = map[string]serviceClient{"other": &acquiringMeshClient{}}
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockNoExecutor, "bind_executor")
	})
	t.Run("package missing on executor", func(t *testing.T) {
		s := service(newExec(present, nil, abi, nil), discovery.MeshProtocol, "kit-a", fpga)
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockNoExecutor, "bind_executor")
	})
	t.Run("abi unlisted", func(t *testing.T) {
		s := service(newExec(present, nil, nil, []string{pkg}), discovery.MeshProtocol, "kit-a", fpga)
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockNoExecutor, "bind_executor")
	})
	t.Run("abi major skew", func(t *testing.T) {
		skew := []meshcontent.EligibleABI{{ID: "fes.application", Major: 2}}
		s := service(newExec(present, nil, skew, []string{pkg}), discovery.MeshProtocol, "kit-a", fpga)
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockVersionSkew, "resolve_version")
	})
	t.Run("mesh major skew", func(t *testing.T) {
		s := service(newExec(present, nil, abi, []string{pkg}), "2.0", "kit-a", fpga)
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockVersionSkew, "resolve_version")
	})
	t.Run("mesh major omitted", func(t *testing.T) {
		s := service(newExec(present, nil, abi, []string{pkg}), "", "kit-a", fpga)
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockVersionSkew, "resolve_version")
	})
	t.Run("content missing", func(t *testing.T) {
		held := map[string]meshcontent.SlotState{cart.String(): meshcontent.StatePresent}
		s := service(newExec(held, nil, abi, []string{pkg}), discovery.MeshProtocol, "kit-a", fpga)
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockContentMissing, "supply_content")
	})
	t.Run("distant only does not pull", func(t *testing.T) {
		exec := newExec(map[string]meshcontent.SlotState{cart.String(): meshcontent.StatePresent}, map[string]bool{bios.String(): true}, abi, []string{pkg})
		s := service(exec, discovery.MeshProtocol, "kit-a", fpga)
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockDistant, "fetch_here")
		if len(exec.pulls) != 0 {
			t.Fatalf("ready pulled %+v", exec.pulls)
		}
	})
	t.Run("checking", func(t *testing.T) {
		held := map[string]meshcontent.SlotState{
			bios.String(): meshcontent.StateChecking,
			cart.String(): meshcontent.StatePresent,
		}
		s := service(newExec(held, nil, abi, []string{pkg}), discovery.MeshProtocol, "kit-a", fpga)
		assertMeshBlock(t, s, fpga.TitleID, meshcontent.BlockEnsureProgress, "wait")
	})
	t.Run("host only stays ready while the kit is busy", func(t *testing.T) {
		exec := newExec(map[string]meshcontent.SlotState{cart.String(): meshcontent.StatePresent}, nil, nil, nil)
		s := service(exec, discovery.MeshProtocol, "kit-a", hostOnly)
		s.connection = TargetConnection{State: "busy", TargetID: "kit-a"}
		got, on := s.GamesMeshReady(context.Background(), []string{hostOnly.TitleID})
		if !on || !got[hostOnly.TitleID].Ready {
			t.Fatalf("host only %#v on %v", got[hostOnly.TitleID], on)
		}
	})
	t.Run("title outside the mesh contract is omitted", func(t *testing.T) {
		got, on := base().GamesMeshReady(context.Background(), []string{"not-a-title", fpga.TitleID})
		if !on || !got[fpga.TitleID].Ready {
			t.Fatalf("projected %#v", got)
		}
		if _, ok := got["not-a-title"]; ok {
			t.Fatal("unprojected title was annotated")
		}
	})
}

func TestGamesMeshReadyLeaseAndBatch(t *testing.T) {
	bios := meshcontent.SumSHA256([]byte("bios"))
	cart := meshcontent.SumSHA256([]byte("cart"))
	other := meshcontent.SumSHA256([]byte("other-cart"))
	pkg := strings.Repeat("ab", 32)
	fpga := meshcontent.Entry{
		TitleID: "coleco-frogger", System: "coleco", Launchable: true,
		Execute: []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: pkg, ABI: "fes.application", Major: 1}),
			meshcontent.BIOSSlot(bios),
			meshcontent.PrimaryMediaSlot(cart),
		},
	}
	second := fpga
	second.TitleID = "coleco-dk"
	second.Slots = []meshcontent.Slot{
		meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: pkg, ABI: "fes.application", Major: 1}),
		meshcontent.BIOSSlot(bios),
		meshcontent.PrimaryMediaSlot(other),
	}
	abi := []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}}
	held := map[string]meshcontent.SlotState{
		bios.String():  meshcontent.StatePresent,
		cart.String():  meshcontent.StatePresent,
		other.String(): meshcontent.StatePresent,
	}
	exec := &countReadyExec{readyExec: &readyExec{meshLaunchExecutor: &meshLaunchExecutor{
		node: "kit-a", held: held, abis: abi,
	}, packages: []string{pkg}}}
	s := &Service{
		targets:        []TargetConfig{{Name: "dev", Enabled: true, TargetID: "kit-a"}, {Name: "other", Enabled: true, TargetID: "kit-b"}},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": &acquiringMeshClient{}},
		meshNodes:      []MeshNode{{NodeID: "kit-a", TargetID: "kit-a", Mesh: discovery.MeshProtocol}},
	}
	s.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "kit-a",
		Executor:  exec,
		Entry: func(id string) (meshcontent.Entry, bool) {
			switch id {
			case fpga.TitleID:
				return fpga, true
			case second.TitleID:
				return second, true
			default:
				return meshcontent.Entry{}, false
			}
		},
	})
	got, on := s.GamesMeshReady(context.Background(), []string{fpga.TitleID, second.TitleID})
	if !on || !got[fpga.TitleID].Ready || !got[second.TitleID].Ready {
		t.Fatalf("shared slots %#v", got)
	}
	if exec.slots != 3 {
		t.Fatalf("slot reads %d, want one per distinct content id", exec.slots)
	}

	s.targetClients = nil
	got, on = s.GamesMeshReady(context.Background(), []string{fpga.TitleID})
	if !on || got[fpga.TitleID].Ready || got[fpga.TitleID].Block != meshcontent.BlockLeaseHeld {
		t.Fatalf("unclaimable %#v", got[fpga.TitleID])
	}

	s.targetClients = map[string]serviceClient{"dev": &fakeServiceClient{meshLeaseAbandoned: true}}
	got, on = s.GamesMeshReady(context.Background(), []string{fpga.TitleID})
	if !on || got[fpga.TitleID].Ready || got[fpga.TitleID].NextAction != "wait_for_lease" {
		t.Fatalf("lost lease %#v", got[fpga.TitleID])
	}

	s.targets = []TargetConfig{{Name: "dev", Enabled: true, TargetID: "kit-a"}}
	s.targetClients = map[string]serviceClient{"dev": &acquiringMeshClient{}}
	s.meshExecute.BoundNode = "kit-b"
	exec.node = "kit-b"
	s.meshNodes = []MeshNode{{NodeID: "kit-b", TargetID: "kit-b", Mesh: discovery.MeshProtocol}}
	got, on = s.GamesMeshReady(context.Background(), []string{fpga.TitleID})
	if !on || got[fpga.TitleID].Ready || got[fpga.TitleID].Block != meshcontent.BlockLeaseHeld {
		t.Fatalf("sibling client %#v", got[fpga.TitleID])
	}

	s.targets = []TargetConfig{{Name: "dev", Enabled: true, TargetID: "kit-a"}, {Name: "other", Enabled: true, TargetID: "kit-b"}}
	s.targetClients = map[string]serviceClient{"other": &fakeServiceClient{meshLeaseGeneration: "gen-b"}}
	s.connection = TargetConnection{State: "ready", TargetID: "kit-a", leaseSeen: true, leaseOwned: true, leaseGeneration: "gen-a"}
	got, on = s.GamesMeshReady(context.Background(), []string{fpga.TitleID})
	if !on || !got[fpga.TitleID].Ready {
		t.Fatalf("bound grant %#v on %v", got[fpga.TitleID], on)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, on := s.GamesMeshReady(ctx, []string{fpga.TitleID}); on {
		t.Fatal("canceled context still reported ready")
	}
}

func TestGamesMeshReadySnapshotIsOneRead(t *testing.T) {
	cart := meshcontent.SumSHA256([]byte("cart"))
	pkg := strings.Repeat("ab", 32)
	entry := meshcontent.Entry{
		TitleID: "coleco-frogger", System: "coleco", Launchable: true,
		Execute: []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{PackageID: pkg, ABI: "fes.application", Major: 1}),
			meshcontent.PrimaryMediaSlot(cart),
		},
	}
	exec := &snapReadyExec{readyExec: &readyExec{meshLaunchExecutor: &meshLaunchExecutor{
		node: "kit-a",
		held: map[string]meshcontent.SlotState{cart.String(): meshcontent.StatePresent},
		abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}, packages: []string{pkg}}}
	s := &Service{
		targets:        []TargetConfig{{Name: "dev", Enabled: true, TargetID: "kit-a"}},
		selectedTarget: "dev",
		targetClients:  map[string]serviceClient{"dev": &acquiringMeshClient{}},
		meshNodes:      []MeshNode{{NodeID: "kit-a", TargetID: "kit-a", Mesh: discovery.MeshProtocol}},
	}
	s.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "kit-a", Executor: exec,
		Entry: func(id string) (meshcontent.Entry, bool) {
			return entry, id == entry.TitleID
		},
	})
	got, on := s.GamesMeshReady(context.Background(), []string{entry.TitleID, entry.TitleID})
	if !on || !got[entry.TitleID].Ready || exec.snaps != 1 || exec.slots != 0 {
		t.Fatalf("ready %#v snaps %d slots %d", got[entry.TitleID], exec.snaps, exec.slots)
	}
	exec.fail = true
	got, on = s.GamesMeshReady(context.Background(), []string{entry.TitleID})
	if !on || got[entry.TitleID].Ready || got[entry.TitleID].Block != meshcontent.BlockInvalid || got[entry.TitleID].NextAction != "unavailable" {
		t.Fatalf("unreachable %#v", got[entry.TitleID])
	}
}

func TestGamesMeshReadySeamOffLeavesComposition(t *testing.T) {
	got, on := (&Service{}).GamesMeshReady(context.Background(), []string{"coleco-frogger", "zx81-maze", "snes-mario"})
	if on || len(got) != 0 {
		t.Fatalf("seam off ready %#v on %v", got, on)
	}
	exec := &readyExec{meshLaunchExecutor: &meshLaunchExecutor{node: "kit-a"}}
	s := &Service{}
	s.SetMeshExecuteSession(MeshExecuteSession{BoundNode: "kit-a", Executor: exec})
	got, on = s.GamesMeshReady(context.Background(), []string{"coleco-frogger"})
	if on || len(got) != 0 {
		t.Fatalf("executor without a projection %#v on %v", got, on)
	}
}

func assertMeshBlock(t *testing.T, s *Service, id string, block meshcontent.Block, action string) {
	t.Helper()
	got, on := s.GamesMeshReady(context.Background(), []string{id})
	if !on || got[id].Ready || got[id].Block != block || got[id].NextAction != action {
		t.Fatalf("got %#v on %v want %s %s", got[id], on, block, action)
	}
}

type readyExec struct {
	*meshLaunchExecutor
	packages []string
}

func (e *readyExec) Packages() []string {
	if e == nil {
		return nil
	}
	return append([]string(nil), e.packages...)
}

type countReadyExec struct {
	*readyExec
	slots int
}

func (e *countReadyExec) Slot(id meshcontent.ContentID) meshcontent.SlotState {
	e.slots++
	return e.meshLaunchExecutor.Slot(id)
}

type snapReadyExec struct {
	*readyExec
	snaps int
	slots int
	fail  bool
}

func (e *snapReadyExec) Slot(id meshcontent.ContentID) meshcontent.SlotState {
	e.slots++
	return e.meshLaunchExecutor.Slot(id)
}

func (e *snapReadyExec) Snapshot(context.Context, []meshcontent.ContentID) (map[string]meshcontent.SlotFact, error) {
	e.snaps++
	if e.fail {
		return nil, meshcontent.ErrContentUnreachable
	}
	out := make(map[string]meshcontent.SlotFact, len(e.held))
	for id, state := range e.held {
		out[id] = meshcontent.SlotFact{State: state}
	}
	return out, nil
}
