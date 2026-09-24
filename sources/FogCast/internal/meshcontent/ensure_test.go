package meshcontent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ensureNow(entry Entry, node string, exec Executor) (Result, error) {
	return Ensure(context.Background(), entry, node, exec, EnsureOption{LeaseFree: true})
}

func TestEnsurePresentDoesNotPull(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("source-rom"))
	ram := SumSHA256([]byte("slot-bytes"))
	entry := colecoEntry(bios, cart, ram)
	exec := &fakeExecutor{
		node: "kit-a",
		held: map[string]SlotState{
			bios.String(): StatePresent,
			cart.String(): StatePresent,
			ram.String():  StatePresent,
		},
		abis: []EligibleABI{{ID: "fes.application", Major: 1}},
	}
	result, err := ensureNow(entry, "kit-a", exec)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Execute || result.Block != BlockNone {
		t.Fatalf("execute=%v block=%s", result.Execute, result.Block)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 1 {
		t.Fatalf("pulls %+v links %+v", exec.pulls, exec.links)
	}
	if exec.links[0].name != "port" || exec.links[0].id != ram {
		t.Fatalf("link %+v", exec.links[0])
	}
	assertSlotStates(t, result, StatePresent, StatePresent, StatePresent)
}

func TestEnsureMissingSourcePullsThenPresent(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("source-rom"))
	ram := SumSHA256([]byte("slot-bytes"))
	entry := colecoEntry(bios, cart, ram)
	exec := &fakeExecutor{
		node:    "kit-a",
		sources: map[string]bool{bios.String(): true, cart.String(): true, ram.String(): true},
		abis:    []EligibleABI{{ID: "fes.application", Major: 1}},
	}
	result, err := ensureNow(entry, "kit-a", exec)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Execute || result.Block != BlockNone {
		t.Fatalf("execute=%v block=%s", result.Execute, result.Block)
	}
	if len(exec.pulls) != 3 {
		t.Fatalf("pulls %+v", exec.pulls)
	}
	assertSlotStates(t, result, StatePresent, StatePresent, StatePresent)
	if exec.Slot(bios) != StatePresent || exec.Slot(cart) != StatePresent || exec.Slot(ram) != StatePresent {
		t.Fatal("pull did not land on the bound executor")
	}
}

func TestEnsureMissingWithoutSourceFailsClosed(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("source-rom"))
	ram := SumSHA256([]byte("slot-bytes"))
	entry := colecoEntry(bios, cart, ram)
	exec := &fakeExecutor{
		node: "kit-a",
		held: map[string]SlotState{cart.String(): StatePresent, ram.String(): StatePresent},
		abis: []EligibleABI{{ID: "fes.application", Major: 1}},
	}
	result, err := ensureNow(entry, "kit-a", exec)
	if result.Execute {
		t.Fatal("missing content allowed execute")
	}
	var missing *ContentMissingError
	if !errors.As(err, &missing) || !errors.Is(err, ErrContentMissingNoSource) {
		t.Fatalf("err %v", err)
	}
	if missing.Kind != SlotBIOS || missing.ID != bios {
		t.Fatalf("missing %+v", missing)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("pulls %+v links %+v", exec.pulls, exec.links)
	}
	if !errors.Is(err, ErrContentMissingNoSource) || errors.Is(err, ErrExecuteBlocked) || errors.Is(err, ErrUnboundNode) {
		t.Fatalf("class collapsed: %v", err)
	}
}

func TestEnsureCheckingStaysCheckingAndBlocksExecute(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("source-rom"))
	ram := SumSHA256([]byte("slot-bytes"))
	entry := colecoEntry(bios, cart, ram)
	exec := &fakeExecutor{
		node: "kit-a",
		held: map[string]SlotState{
			bios.String(): StatePresent,
			cart.String(): StateChecking,
			ram.String():  StatePresent,
		},
		sources: map[string]bool{cart.String(): true},
		abis:    []EligibleABI{{ID: "fes.application", Major: 1}},
	}
	result, err := ensureNow(entry, "kit-a", exec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Execute || result.Block != BlockEnsureProgress {
		t.Fatalf("execute=%v block=%s", result.Execute, result.Block)
	}
	if blocked := result.Blocked(); !errors.Is(blocked, ErrExecuteBlocked) {
		t.Fatalf("blocked %v", blocked)
	}
	var blocked *ExecuteBlockedError
	if !errors.As(result.Blocked(), &blocked) || blocked.Block != BlockEnsureProgress {
		t.Fatalf("blocked %#v", result.Blocked())
	}
	assertSlotStates(t, result, StatePresent, StateChecking, StatePresent)
	if len(exec.pulls) != 0 {
		t.Fatalf("mid-pull was pulled again: %+v", exec.pulls)
	}
	if len(exec.links) != 0 {
		t.Fatalf("linked while a sibling was checking: %+v", exec.links)
	}
}

func TestEnsureLinksExpansionOnExecutorNotProgrammedImage(t *testing.T) {
	source := SumSHA256([]byte("source-rom"))
	slotBytes := SumSHA256([]byte("slot-bytes"))
	programmed := SumSHA256([]byte("programmed-image"))
	if source == programmed || slotBytes == programmed || source == slotBytes {
		t.Fatal("fixture digests collided")
	}
	pkg := PackageABI{PackageID: strings.Repeat("ab", 32), ABI: "fes.application", Major: 1}
	entry := Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []Execute{{Kind: ExecuteFPGANative}},
		Slots: []Slot{
			PackageSlot(pkg),
			PrimaryMediaSlot(source),
			ExpansionSlot("port", slotBytes),
		},
	}
	exec := &fakeExecutor{
		node:    "kit-a",
		sources: map[string]bool{source.String(): true, slotBytes.String(): true, programmed.String(): true},
		abis:    []EligibleABI{{ID: pkg.ABI, Major: pkg.Major}},
	}
	result, err := ensureNow(entry, "kit-a", exec)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Execute {
		t.Fatalf("block %s", result.Block)
	}
	if len(exec.links) != 1 || exec.links[0].name != "port" || exec.links[0].id != slotBytes {
		t.Fatalf("links %+v", exec.links)
	}
	for _, id := range append(append([]ContentID{}, exec.pulls...), exec.lookups...) {
		if id == programmed {
			t.Fatalf("programmed image was used: %+v", id)
		}
	}
	for _, link := range exec.links {
		if link.id == programmed || link.id == source {
			t.Fatalf("link used the wrong bytes: %+v", link)
		}
	}
	if exec.links[0].id.Digest != slotBytes.Digest {
		t.Fatal("expansion was pre-linked with another slot")
	}
}

func TestEnsureABIIneligibleIsNotReady(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("source-rom"))
	ram := SumSHA256([]byte("slot-bytes"))
	entry := colecoEntry(bios, cart, ram)
	local := NewCache()
	for _, id := range entry.ContentIDs() {
		if err := local.Hold(id); err != nil {
			t.Fatal(err)
		}
	}
	pkg := *entry.Slots[0].Package
	bound := Bound{
		Execute: true, LeaseFree: true, MeshMajorOK: true,
		Local:    local,
		Packages: []string{pkg.PackageID},
	}
	ready, block := ReadyHere(entry, bound)
	if ready || block != BlockNoExecutor {
		t.Fatalf("unlisted abi ready=%v block=%s", ready, block)
	}
	bound.ABIs = []EligibleABI{{ID: pkg.ABI, Major: pkg.Major + 1}}
	ready, block = ReadyHere(entry, bound)
	if ready || block != BlockVersionSkew {
		t.Fatalf("wrong major ready=%v block=%s", ready, block)
	}
	bound.ABIs = []EligibleABI{{ID: "fes.simple-computer", Major: pkg.Major}}
	ready, block = ReadyHere(entry, bound)
	if ready || block != BlockNoExecutor {
		t.Fatalf("other abi ready=%v block=%s", ready, block)
	}

	exec := &fakeExecutor{
		node: "kit-a",
		held: map[string]SlotState{
			bios.String(): StatePresent,
			cart.String(): StatePresent,
			ram.String():  StatePresent,
		},
		abis: []EligibleABI{{ID: pkg.ABI, Major: pkg.Major + 1}},
	}
	result, err := ensureNow(entry, "kit-a", exec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Execute || result.Block != BlockVersionSkew {
		t.Fatalf("ensure execute=%v block=%s", result.Execute, result.Block)
	}
	ready, block = ReadyHere(entry, Bound{
		Execute: true, LeaseFree: true, MeshMajorOK: true,
		Local: local, Packages: []string{pkg.PackageID}, ABIs: exec.abis,
	})
	if ready || block != BlockVersionSkew {
		t.Fatalf("ready after ensure ready=%v block=%s", ready, block)
	}
}

func TestEnsureRefusesPullOntoUnboundNode(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("source-rom"))
	ram := SumSHA256([]byte("slot-bytes"))
	entry := colecoEntry(bios, cart, ram)
	exec := &fakeExecutor{
		node:    "other-kit",
		sources: map[string]bool{bios.String(): true, cart.String(): true, ram.String(): true},
		abis:    []EligibleABI{{ID: "fes.application", Major: 1}},
	}
	_, err := ensureNow(entry, "kit-a", exec)
	if !errors.Is(err, ErrUnboundNode) {
		t.Fatalf("err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 || len(exec.lookups) != 0 {
		t.Fatalf("unbound node was touched pulls=%d links=%d lookups=%d", len(exec.pulls), len(exec.links), len(exec.lookups))
	}
	if _, err := ensureNow(entry, "", exec); !errors.Is(err, ErrUnboundNode) {
		t.Fatalf("empty bind %v", err)
	}
}

func TestEnsureSlowPullStaysChecking(t *testing.T) {
	cart := SumSHA256([]byte("source-rom"))
	entry := Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []Execute{{Kind: ExecuteNativeEmu}},
		Slots:      []Slot{PrimaryMediaSlot(cart)},
	}
	exec := &fakeExecutor{
		node:    "host-a",
		sources: map[string]bool{cart.String(): true},
		slow:    true,
	}
	result, err := ensureNow(entry, "host-a", exec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Execute || result.Block != BlockEnsureProgress || result.Slots[0].State != StateChecking {
		t.Fatalf("%+v", result)
	}
	if len(exec.pulls) != 1 || exec.pulls[0] != cart {
		t.Fatalf("pulls %+v", exec.pulls)
	}
	if len(exec.links) != 0 {
		t.Fatal("native primary was linked as an expansion")
	}
}

type expansionLink struct {
	name string
	id   ContentID
}

type fakeExecutor struct {
	node    string
	held    map[string]SlotState
	sources map[string]bool
	abis    []EligibleABI
	slow    bool
	pulls   []ContentID
	lookups []ContentID
	links   []expansionLink
}

func (f *fakeExecutor) NodeID() string { return f.node }

func (f *fakeExecutor) Slot(id ContentID) SlotState {
	f.lookups = append(f.lookups, id)
	if f.held == nil {
		return StateMissing
	}
	if state, ok := f.held[id.String()]; ok {
		return state
	}
	return StateMissing
}

func (f *fakeExecutor) SourceAdvertises(id ContentID) bool {
	return f.sources[id.String()]
}

func (f *fakeExecutor) Pull(ctx context.Context, id ContentID) (SlotState, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.pulls = append(f.pulls, id)
	if f.held == nil {
		f.held = map[string]SlotState{}
	}
	if f.slow {
		f.held[id.String()] = StateChecking
		return StateChecking, nil
	}
	f.held[id.String()] = StatePresent
	return StatePresent, nil
}

func (f *fakeExecutor) LinkExpansion(name string, id ContentID) error {
	f.links = append(f.links, expansionLink{name: name, id: id})
	return nil
}

func (f *fakeExecutor) EligibleABIs() []EligibleABI { return f.abis }

func TestEnsureCheckingTimeoutAndCancel(t *testing.T) {
	cart := SumSHA256([]byte("source-rom"))
	ram := SumSHA256([]byte("slot-bytes"))
	entry := Entry{
		TitleID:    "snes-mario",
		System:     "snes",
		Launchable: true,
		Execute:    []Execute{{Kind: ExecuteNativeEmu}},
		Slots: []Slot{
			PrimaryMediaSlot(cart),
			ExpansionSlot("port", ram),
		},
	}
	exec := &fakeExecutor{
		node: "host-a",
		held: map[string]SlotState{
			cart.String(): StateChecking,
			ram.String():  StatePresent,
		},
	}
	_, err := Ensure(context.Background(), entry, "host-a", exec, EnsureOption{
		LeaseFree:       true,
		CheckingTimeout: 30 * time.Millisecond,
	})
	if !errors.Is(err, ErrCheckingTimeout) || errors.Is(err, ErrExecuteBlocked) {
		t.Fatalf("err %v", err)
	}
	if len(exec.links) != 0 || len(exec.pulls) != 0 {
		t.Fatalf("timeout linked %+v pulled %+v", exec.links, exec.pulls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Ensure(ctx, entry, "host-a", exec, EnsureOption{LeaseFree: true, CheckingTimeout: time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err %v", err)
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatal("canceled ensure touched the executor pull")
	}
}

func TestEnsureRefusesUnownedAndInUseBeforePull(t *testing.T) {
	bios := SumSHA256([]byte("bios"))
	cart := SumSHA256([]byte("source-rom"))
	ram := SumSHA256([]byte("slot-bytes"))
	entry := colecoEntry(bios, cart, ram)
	exec := &fakeExecutor{
		node:    "kit-a",
		sources: map[string]bool{bios.String(): true, cart.String(): true, ram.String(): true},
		abis:    []EligibleABI{{ID: "fes.application", Major: 1}},
	}
	for _, opt := range []EnsureOption{{LeaseFree: true, InUse: true}, {InUse: true}, {}} {
		_, err := Ensure(context.Background(), entry, "kit-a", exec, opt)
		if !errors.Is(err, ErrLeaseNotFree) {
			t.Fatalf("opt %+v err %v", opt, err)
		}
	}
	if len(exec.pulls) != 0 || len(exec.links) != 0 || len(exec.lookups) != 0 {
		t.Fatalf("unowned ensure touched the executor pulls=%d links=%d lookups=%d", len(exec.pulls), len(exec.links), len(exec.lookups))
	}
}

func TestEnsureResultsHaveNoJSONTags(t *testing.T) {
	for _, value := range []any{Result{}, SlotStatus{}, ExecuteBlockedError{}, ContentMissingError{}} {
		typ := reflect.TypeOf(value)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if tag := field.Tag.Get("json"); tag != "" {
				t.Fatalf("%s.%s has json tag %q", typ.Name(), field.Name, tag)
			}
		}
	}
	title, ok := reflect.TypeOf(Entry{}).FieldByName("TitleID")
	if !ok || title.Tag.Get("json") != "title_id" {
		t.Fatal("catalog entry lost its host json tag")
	}
}

func assertSlotStates(t *testing.T, result Result, states ...SlotState) {
	t.Helper()
	if len(result.Slots) != len(states) {
		t.Fatalf("slots %+v", result.Slots)
	}
	for i, state := range states {
		if result.Slots[i].State != state {
			t.Fatalf("slot %d state %s", i, result.Slots[i].State)
		}
	}
}
