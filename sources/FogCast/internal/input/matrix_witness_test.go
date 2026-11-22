package input

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func matrixFixture(t *testing.T) (*KeyboardSink, MatrixBinding) {
	t.Helper()
	sink := NewKeyboardSink()
	binding := MatrixBinding{RuntimeInstance: strings.Repeat("a", 32), PackageID: strings.Repeat("b", 64), Generation: 7}
	if err := sink.BindInitial(binding); err != nil {
		t.Fatal(err)
	}
	return sink, binding
}
func matrixKey(code remoteinput.Code, action remoteinput.Action) protocol.InputFrame {
	return protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Action: uint8(action), Code: uint16(code)}
}
func TestMatrixNotDispatchedRollsBackProvisionalKey(t *testing.T) {
	sink, binding := matrixFixture(t)
	var posts []uint64
	reject := true
	sink.SetPoster(func(_ context.Context, _ localhid.MatrixPost, matrix uint64) error {
		if reject {
			return localhid.ErrMatrixNotDispatched
		}
		posts = append(posts, matrix)
		return nil
	})
	j := matrixKey(zx81keys.Letter('J'), remoteinput.ActionPress)
	if err := sink.applyFromBound(context.Background(), sourceLocal, j, &binding, nil); !errors.Is(err, localhid.ErrMatrixNotDispatched) {
		t.Fatal(err)
	}
	if sink.uncertain != nil || len(sink.pressed[sourceLocal]) != 0 {
		t.Fatal("pre-dispatch rejection changed source or latched uncertainty")
	}
	reject = false
	k := matrixKey(zx81keys.Letter('K'), remoteinput.ActionPress)
	if err := sink.applyFromBound(context.Background(), sourceRemote, k, &binding, nil); err != nil {
		t.Fatal(err)
	}
	expected := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('K'): true})
	if len(posts) != 1 || posts[0] != expected {
		t.Fatalf("rejected J leaked into union: %v", posts)
	}
}
func TestMatrixAmbiguousSourceRequiresExactNeutral(t *testing.T) {
	sink, binding := matrixFixture(t)
	var recorded []struct {
		post   localhid.MatrixPost
		matrix uint64
	}
	fail := false
	sink.SetPoster(func(_ context.Context, post localhid.MatrixPost, matrix uint64) error {
		recorded = append(recorded, struct {
			post   localhid.MatrixPost
			matrix uint64
		}{post, matrix})
		if fail {
			return errors.New("ambiguous")
		}
		return nil
	})
	if err := sink.applyFromBound(context.Background(), sourceRemote, matrixKey(zx81keys.Letter('J'), remoteinput.ActionPress), &binding, nil); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := sink.applyFromBound(context.Background(), sourceLocal, matrixKey(zx81keys.Letter('K'), remoteinput.ActionPress), &binding, nil); err == nil {
		t.Fatal("ambiguous write accepted")
	}
	old := *sink.uncertain
	if err := sink.releaseSourceContext(context.Background(), sourceRemote); !errors.Is(err, errMatrixUnconfirmed) {
		t.Fatalf("wrong source neutral: %v", err)
	}
	if len(recorded) != 2 {
		t.Fatal("wrong-source post crossed fence")
	}
	fail = false
	if err := sink.releaseSourceContext(context.Background(), sourceLocal); err != nil {
		t.Fatal(err)
	}
	last := recorded[len(recorded)-1]
	if last.post.NeutralOf == nil || *last.post.NeutralOf != old {
		t.Fatal("neutral did not bind exact failed operation")
	}
	expected := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('J'): true})
	if last.matrix != expected || sink.uncertain != nil {
		t.Fatalf("incorrect retained union: %#x", last.matrix)
	}
}
func TestMatrixRetirementSamplesAfterLatchAndRejectsUnknown(t *testing.T) {
	sink, binding := matrixFixture(t)
	sink.SetPoster(func(context.Context, localhid.MatrixPost, uint64) error { return errors.New("ambiguous") })
	if err := sink.applyFromBound(context.Background(), sourceLocal, matrixKey(zx81keys.Letter('J'), remoteinput.ActionPress), &binding, nil); err == nil {
		t.Fatal("post accepted")
	}
	old := *sink.uncertain
	var proofs []localhid.MatrixRetirement
	sink.SetRetirer(func(_ context.Context, proof localhid.MatrixRetirement) error {
		proofs = append(proofs, proof)
		return nil
	})
	guard := func(ctx context.Context) error { return ctx.Err() }
	unknown := func(context.Context) (CoreObservation, error) {
		return CoreObservation{RuntimeInstance: strings.Repeat("c", 32)}, nil
	}
	if err := sink.retireObserved(context.Background(), unknown, guard); err == nil {
		t.Fatal("unknown state retired source")
	}
	same := func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation}, nil
	}
	if err := sink.retireObserved(context.Background(), same, guard); err == nil {
		t.Fatal("same generation retired source")
	}
	idle := func(context.Context) (CoreObservation, error) {
		return CoreObservation{RuntimeInstance: binding.RuntimeInstance, IdleConfirmed: true}, nil
	}
	if err := sink.retireObserved(context.Background(), idle, guard); err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 1 || proofs[0].RetireOf != old || sink.uncertain != nil {
		t.Fatal("incorrect retirement")
	}
}

func TestMatrixMediaNeutralPreservesBothLatchesAndSuppression(t *testing.T) {
	sink, binding := matrixFixture(t)
	fail := false
	sink.SetPoster(func(_ context.Context, _ localhid.MatrixPost, _ uint64) error {
		if fail {
			return errors.New("ambiguous source")
		}
		return nil
	})
	j := matrixKey(zx81keys.Letter('J'), remoteinput.ActionPress)
	k := matrixKey(zx81keys.Letter('K'), remoteinput.ActionPress)
	if err := sink.applyFromBound(context.Background(), sourceRemote, j, &binding, nil); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := sink.applyFromBound(context.Background(), sourceLocal, k, &binding, nil); err == nil {
		t.Fatal("failed source accepted")
	}
	old := *sink.uncertain
	media := localhid.MatrixMediaWitness{Operation: [16]byte{1}, RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation}
	query := func(context.Context) (localhid.MatrixMediaWitness, bool, error) { return media, true, nil }
	guard := func(ctx context.Context) error { return ctx.Err() }
	neutralFails := true
	neutral := func(_ context.Context, request localhid.MatrixMediaNeutral) error {
		if request.MediaOf != media || request.SourceOf == nil || *request.SourceOf != old {
			t.Fatal("neutral did not carry both exact witnesses")
		}
		if neutralFails {
			return localhid.ErrMatrixNotDispatched
		}
		return nil
	}
	if _, err := sink.neutralMedia(context.Background(), query, neutral, guard); !errors.Is(err, localhid.ErrMatrixNotDispatched) {
		t.Fatal(err)
	}
	if sink.uncertain == nil || *sink.uncertain != old || !sink.blocked[sourceLocal][zx81keys.Letter('K')] || !sink.blocked[sourceRemote][zx81keys.Letter('J')] {
		t.Fatal("failed neutral lost latch or held-key suppression")
	}
	neutralFails = false
	if _, err := sink.neutralMedia(context.Background(), query, neutral, guard); err != nil {
		t.Fatal(err)
	}
	if sink.uncertain != nil {
		t.Fatal("confirmed neutral retained source latch")
	}
	fail = false
	if err := sink.applyFromBound(context.Background(), sourceLocal, k, &binding, nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.pressed[sourceLocal]) != 0 {
		t.Fatal("held key repeat reasserted after neutral")
	}
	k.Action = uint8(remoteinput.ActionRelease)
	if err := sink.applyFromBound(context.Background(), sourceLocal, k, &binding, nil); err != nil {
		t.Fatal(err)
	}
	k.Action = uint8(remoteinput.ActionPress)
	if err := sink.applyFromBound(context.Background(), sourceLocal, k, &binding, nil); err != nil {
		t.Fatal(err)
	}
	if !sink.pressed[sourceLocal][zx81keys.Letter('K')] {
		t.Fatal("fresh press remained blocked")
	}
}

func TestMatrixOldRemoteBindingCannotPostAfterRuntimeRestart(t *testing.T) {
	sink, old := matrixFixture(t)
	posts := 0
	sink.SetPoster(func(context.Context, localhid.MatrixPost, uint64) error { posts++; return nil })
	ports := &controllerPortsSink{keys: sink, keyboard: true, coreActive: true, observed: true, coreID: "fes.sms", coreGeneration: old.Generation, matrixBinding: &old}
	controller := newTargetControllerWithSink("", ports)
	controller.ports, controller.keyboard = ports, sink
	remote := &remoteSourceSink{ports: ports, matrix: &old, controller: controller}
	server, err := bridge.New(bridge.Config{Token: []byte("0123456789abcdef"), Session: 1}, remote)
	if err != nil {
		t.Fatal(err)
	}
	remote.server = server
	controller.active = &lease{session: 1, server: server}
	frame := matrixKey(zx81keys.Letter('J'), remoteinput.ActionPress)
	if err := remote.Apply(frame); err != nil {
		t.Fatal(err)
	}
	newInstance := strings.Repeat("c", 32)
	if err := ports.setObservationContext(context.Background(), CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: newInstance, PackageID: old.PackageID, Generation: old.Generation}); err != nil {
		t.Fatal(err)
	}
	if err := remote.Apply(frame); err == nil {
		t.Fatal("old bridge posted into restarted runtime")
	}
	if posts != 1 {
		t.Fatalf("stale bridge wrote %d posts", posts)
	}
}
