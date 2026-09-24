package fogcast

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
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
	service := &Service{}
	service.SetMeshExecuteSession(MeshExecuteSession{
		BoundNode: "kit-a",
		Executor:  exec,
		Entry: func(gameID string) (meshcontent.Entry, bool) {
			return entry, gameID == entry.TitleID
		},
	})
	_, err := service.Launch(context.Background(), entry.TitleID, nil)
	var blocked *meshcontent.ExecuteBlockedError
	if !errors.As(err, &blocked) || !errors.Is(err, meshcontent.ErrExecuteBlocked) || blocked.Block != meshcontent.BlockEnsureProgress {
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
	if err := service.meshEnsureBeforeExecute(entry.TitleID); err != nil {
		t.Fatal(err)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart || exec.Slot(cart) != meshcontent.StatePresent {
		t.Fatalf("pulls %+v state %s", exec.pulls, exec.Slot(cart))
	}
	// A second ensure sees the id Present and does not pull again.
	// Launch would continue into the existing path; this service has
	// no catalog, so the assertion stops at the seam.
	if err := service.meshEnsureBeforeExecute(entry.TitleID); err != nil {
		t.Fatal(err)
	}
	if len(exec.pulls) != 1 {
		t.Fatalf("present id pulled again %+v", exec.pulls)
	}
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
			primary.String():    true,
			expansionID.String(): true,
			"sha256:" + programmed: true,
		},
		abis: []meshcontent.EligibleABI{{ID: "fes.simple-computer", Major: 1}},
	}
	result, err := meshcontent.Ensure(entry, "kit-a", exec)
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
	node    string
	held    map[string]meshcontent.SlotState
	sources map[string]bool
	abis    []meshcontent.EligibleABI
	pulls   []meshcontent.ContentID
	links   []meshExpansionLink
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

func (f *meshLaunchExecutor) Pull(id meshcontent.ContentID) (meshcontent.SlotState, error) {
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
