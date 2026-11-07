package kitsoftware

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/appliance/software"
)

// resolvedControl exercises the real factory/store resolution and manifest
// compatibility checks; the embedded controller only simulates processes.
type resolvedControl struct {
	*fakeControl
	base        software.Compatibility
	factory     software.Release
	factoryRoot string
	store       *software.Store
}

func (c *resolvedControl) Verify(ctx context.Context, id string) error {
	_, err := ResolveRelease(ctx, id, c.factoryRoot, c.factory, c.base, c.store)
	return err
}

func versionedBundle(t *testing.T, release software.Release, root, version string) (software.Release, []byte) {
	t.Helper()
	release.Manifest.Version = version
	id, err := release.Manifest.ID()
	if err != nil {
		t.Fatal(err)
	}
	release.ID = id
	var bundle bytes.Buffer
	if err := software.WriteBundle(&bundle, release.Manifest, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	return release, bundle.Bytes()
}

// The OS image selector and physical reboot are exercised separately. This
// test models which base is running while using actual verified bundle stores,
// the real persisted selection and the supervisor's factory/hold-back logic.
func TestImageContractTransitionAndRollback(t *testing.T) {
	ctx := context.Background()
	oldFactory, oldRoot, _ := softwareFixture(t, "fes-kit-base-v1-old")
	newFactory, newRoot, _ := softwareFixture(t, "fes-kit-base-v2-new")
	oldSelected, oldArchive := versionedBundle(t, oldFactory, oldRoot, "old-selection")
	newSelected, newArchive := versionedBundle(t, newFactory, newRoot, "new-selection")
	base := func(contract string) software.Compatibility {
		return software.Compatibility{Board: "de10-nano", Architecture: "armv7", BaseContract: contract, Interfaces: map[string]string{"runtime": "1"}}
	}
	oldBase, newBase := base(oldFactory.Manifest.BaseContract), base(newFactory.Manifest.BaseContract)
	storeRoot := filepath.Join(t.TempDir(), "releases")
	oldStore, err := software.New(storeRoot, oldBase)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := oldStore.Install(ctx, bytes.NewReader(oldArchive)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := oldStore.Install(ctx, bytes.NewReader(newArchive)); !errors.Is(err, software.ErrIncompatible) {
		t.Fatalf("v1 image installed v2 bundle: %v", err)
	}
	newStore, err := software.New(storeRoot, newBase)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := newStore.Install(ctx, bytes.NewReader(oldArchive)); !errors.Is(err, software.ErrIncompatible) {
		t.Fatalf("v2 image installed v1 bundle: %v", err)
	}
	repo, engine, fake, _ := setup(t)
	selectRelease(t, repo, oldSelected.ID)
	control := &resolvedControl{fakeControl: fake, base: oldBase, factory: oldFactory, factoryRoot: oldRoot, store: oldStore}
	engine.Controller, engine.Factory = control, oldFactory.ID
	boot := Request{ID: strings.Repeat("b", 64), Requester: "boot"}
	assertState := func(wantActive, wantSelected, wantHeld string) {
		t.Helper()
		state, err := repo.Read()
		if err != nil || fake.active != wantActive || state.Selected != wantSelected || engine.HeldBack != wantHeld {
			t.Fatalf("active=%s selected=%s held=%s state=%+v err=%v", fake.active, state.Selected, engine.HeldBack, state, err)
		}
	}
	if err := engine.Boot(ctx, boot); err != nil {
		t.Fatal(err)
	}
	assertState(oldSelected.ID, oldSelected.ID, "")

	// An appliance trial uses its own factory without changing the durable
	// selection. If the trial fails and the old image boots again, its selected
	// release is still available and runs unchanged.
	control.base, control.factory, control.factoryRoot, control.store = newBase, newFactory, newRoot, newStore
	engine.Factory = newFactory.ID
	if err := engine.StartFactory(ctx, newFactory.ID); err != nil {
		t.Fatal(err)
	}
	assertState(newFactory.ID, oldSelected.ID, "")
	control.base, control.factory, control.factoryRoot, control.store = oldBase, oldFactory, oldRoot, oldStore
	engine.Factory = oldFactory.ID
	if err := engine.Boot(ctx, boot); err != nil {
		t.Fatal(err)
	}
	assertState(oldSelected.ID, oldSelected.ID, "")

	// After a successful v2 image boot, the v1 selection is held back. An
	// explicit activation selects the separately installed v2 update.
	control.base, control.factory, control.factoryRoot, control.store = newBase, newFactory, newRoot, newStore
	engine.Factory = newFactory.ID
	if err := engine.StartFactory(ctx, newFactory.ID); err != nil {
		t.Fatal(err)
	}
	assertState(newFactory.ID, oldSelected.ID, "")
	if err := engine.Boot(ctx, boot); err != nil {
		t.Fatal(err)
	}
	assertState(newFactory.ID, oldSelected.ID, oldSelected.ID)
	if !strings.Contains(engine.HeldBackReason, "base contract") {
		t.Fatalf("unexpected v1 hold-back reason: %s", engine.HeldBackReason)
	}
	if _, _, err := newStore.Install(ctx, bytes.NewReader(newArchive)); err != nil {
		t.Fatal(err)
	}
	state, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	if err := Queue(repo, Request{ID: strings.Repeat("c", 64), Candidate: newSelected.ID, Requester: "test", Generation: state.Generation}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	assertState(newSelected.ID, newSelected.ID, "")

	// Restoring the old image holds back the v2 selection until the named v1
	// baseline is explicitly reactivated; no implicit selection is recorded.
	control.base, control.factory, control.factoryRoot, control.store = oldBase, oldFactory, oldRoot, oldStore
	engine.Factory = oldFactory.ID
	if err := engine.Boot(ctx, boot); err != nil {
		t.Fatal(err)
	}
	assertState(oldFactory.ID, newSelected.ID, newSelected.ID)
	if !strings.Contains(engine.HeldBackReason, "base contract") {
		t.Fatalf("unexpected v2 hold-back reason: %s", engine.HeldBackReason)
	}
	state, err = repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	if err := Queue(repo, Request{ID: strings.Repeat("d", 64), Candidate: oldSelected.ID, Requester: "test", Generation: state.Generation}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	assertState(oldSelected.ID, oldSelected.ID, "")
}
