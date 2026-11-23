package input

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type matrixLifecycleControl struct {
	response     misterruntime.Protocol2Response
	failNeutral  bool
	neutralPosts int
}

func (c *matrixLifecycleControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return c.response, nil
}
func (c *matrixLifecycleControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return c.response, nil
}
func (c *matrixLifecycleControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return c.response, nil
}
func (c *matrixLifecycleControl) LoadMedia(context.Context, string) (misterruntime.Protocol2Response, error) {
	return c.response, nil
}
func (c *matrixLifecycleControl) SetKeyboard(_ context.Context, _ string, _ string, _ uint64, matrix uint64) (misterruntime.Protocol2Response, error) {
	if matrix == zx81keys.Neutral {
		c.neutralPosts++
	}
	if c.failNeutral && matrix == zx81keys.Neutral {
		return misterruntime.Protocol2Response{}, errors.New("ambiguous neutral")
	}
	return c.response, nil
}
func TestMatrixMediaFailureRecoversThroughActualReplacementBarrier(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	instance, id, generation := strings.Repeat("a", 32), strings.Repeat("b", 64), uint64(7)
	response := misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "running_development", Execution: "development", RuntimeInstance: &instance, Generation: &generation,
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: id, Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.simple-computer", Major: 1}}},
		Capabilities:  misterruntime.Protocol2Capabilities{ActiveInterfaces: []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}}}
	control := &matrixLifecycleControl{response: response, failNeutral: true}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	mediaErr := runtime.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: id, Generation: generation})
	if mediaErr == nil {
		t.Fatal("ambiguous restore accepted")
	}
	if _, pending, err := runtime.MatrixMediaUncertainty(context.Background()); err != nil || !pending {
		t.Fatalf("media failure not latched: %v", mediaErr)
	}
	keys := NewKeyboardSink()
	if err := keys.BindInitial(MatrixBinding{RuntimeInstance: instance, PackageID: id, Generation: generation}); err != nil {
		t.Fatal(err)
	}
	keys.SetPoster(runtime.SetKeyboard)
	keys.SetRetirer(runtime.RetireMatrixUncertainty)
	ports := &controllerPortsSink{keys: keys}
	controller := newTargetControllerWithSink("", ports)
	controller.ports, controller.keyboard = ports, keys
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Keyboard: true, RuntimeInstance: instance, PackageID: id, Generation: generation}, nil
	})
	controller.SetKeyboardMediaRetirement(runtime.MatrixMediaUncertainty, runtime.RetireMatrixMediaUncertainty, runtime.NeutralMatrixMediaUncertainty)
	if finish, err := controller.BeginCoreReplacement(context.Background()); err == nil {
		if finish != nil {
			_ = finish(context.Background(), false)
		}
		t.Fatal("failed neutral allowed replacement")
	}
	if _, pending, err := runtime.MatrixMediaUncertainty(context.Background()); err != nil || !pending {
		t.Fatal("failed replacement cleared media witness")
	}
	control.failNeutral = false
	// Closing an active bridge first sees the still-latched media failure.
	// Its fully drained release error must enter the same neutral recovery.
	remote := &remoteSourceSink{ports: ports}
	server, err := bridge.New(bridge.Config{Token: []byte("0123456789abcdef"), Session: 1}, remote)
	if err != nil {
		t.Fatal(err)
	}
	controller.active = &lease{session: 1, server: server}
	finish, err := controller.BeginCoreReplacement(context.Background())
	if err != nil {
		t.Fatalf("confirmed neutral failed replacement: %v", err)
	}
	if err := finish(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, pending, err := runtime.MatrixMediaUncertainty(context.Background()); err != nil || pending {
		t.Fatal("confirmed neutral retained media witness")
	}
	if control.neutralPosts < 3 {
		t.Fatalf("expected failed restore, failed neutral, and confirmed neutral: %d", control.neutralPosts)
	}
}

func TestMatrixLifecycleWaitsHonorCancellation(t *testing.T) {
	keys := NewKeyboardSink()
	ports := &controllerPortsSink{keys: keys}
	controller := newTargetControllerWithSink("", ports)
	controller.ports, controller.keyboard = ports, keys
	keys.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := controller.BeginCoreReplacement(ctx)
	keys.mu.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("replacement waited past canceled sink: %v", err)
	}
	controller.lifecycle.Lock()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()
	start = time.Now()
	err = controller.Attach(ctx2, Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.sms"})
	controller.lifecycle.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("attach waited past canceled lifecycle: %v", err)
	}
}

type failingCloseMemoryListener struct{ *memoryListener }

func (l *failingCloseMemoryListener) Close() error {
	_ = l.memoryListener.Close()
	return errors.New("listener close failed")
}
func TestMatrixReplacementDoesNotHideMixedBridgeCloseError(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	instance, id, generation := strings.Repeat("a", 32), strings.Repeat("b", 64), uint64(7)
	response := misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "running_development", Execution: "development", RuntimeInstance: &instance, Generation: &generation,
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: id, Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.simple-computer", Major: 1}}},
		Capabilities:  misterruntime.Protocol2Capabilities{ActiveInterfaces: []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}}}
	control := &matrixLifecycleControl{response: response}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	keys := NewKeyboardSink()
	if err := keys.BindInitial(MatrixBinding{RuntimeInstance: instance, PackageID: id, Generation: generation}); err != nil {
		t.Fatal(err)
	}
	keys.SetPoster(runtime.SetKeyboard)
	keys.SetRetirer(runtime.RetireMatrixUncertainty)
	ports := &controllerPortsSink{keys: keys}
	controller := newTargetControllerWithSink("", ports)
	controller.ports, controller.keyboard = ports, keys
	listener := &failingCloseMemoryListener{&memoryListener{incoming: make(chan net.Conn, 1), done: make(chan struct{})}}
	controller.listen = func(string, string) (net.Listener, error) { return listener, nil }
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Keyboard: true, RuntimeInstance: instance, PackageID: id, Generation: generation}, nil
	})
	controller.SetKeyboardMediaRetirement(runtime.MatrixMediaUncertainty, runtime.RetireMatrixMediaUncertainty, runtime.NeutralMatrixMediaUncertainty)
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.sms"}); err != nil {
		t.Fatal(err)
	}
	control.failNeutral = true
	if err := runtime.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: id, Generation: generation}); err == nil {
		t.Fatal("expected failed media restoration")
	}
	control.failNeutral = false
	before := control.neutralPosts
	if finish, err := controller.BeginCoreReplacement(context.Background()); err == nil {
		if finish != nil {
			_ = finish(context.Background(), false)
		}
		t.Fatal("mixed close failure was hidden")
	}
	if control.neutralPosts != before {
		t.Fatal("mixed close failure was incorrectly superseded by neutral recovery")
	}
	if _, pending, err := runtime.MatrixMediaUncertainty(context.Background()); err != nil || !pending {
		t.Fatalf("media uncertainty lost: %v", err)
	}
}

// A fresh target has not admitted any matrix source. Idle maintenance must
// release its empty input state without inventing a runtime binding or post.
func TestFreshIdleTargetCleanupDoesNotPostUnboundMatrix(t *testing.T) {
	control := &matrixLifecycleControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	keys := NewKeyboardSink()
	keys.SetPoster(runtime.SetKeyboard)
	keys.SetRetirer(runtime.RetireMatrixUncertainty)
	ports := &controllerPortsSink{keys: keys}
	controller := newTargetControllerWithSink("", ports)
	controller.ports, controller.keyboard = ports, keys
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{IdleConfirmed: true, RuntimeInstance: strings.Repeat("a", 32)}, nil
	})
	controller.SetKeyboardMediaRetirement(runtime.MatrixMediaUncertainty, runtime.RetireMatrixMediaUncertainty, runtime.NeutralMatrixMediaUncertainty)
	if err := controller.ReleaseAll(context.Background()); err != nil {
		t.Fatalf("fresh idle cleanup: %v", err)
	}
	if control.neutralPosts != 0 {
		t.Fatal("unbound cleanup posted a matrix")
	}
}

func TestUnboundCleanupRequiresFreshIdleProof(t *testing.T) {
	instance := strings.Repeat("a", 32)
	for _, tc := range []struct {
		name    string
		observe func(context.Context) (CoreObservation, error)
	}{
		{"active", func(context.Context) (CoreObservation, error) {
			return CoreObservation{Active: true, RuntimeInstance: instance, Generation: 7}, nil
		}},
		{"unconfirmed", func(context.Context) (CoreObservation, error) { return CoreObservation{RuntimeInstance: instance}, nil }},
		{"missing-instance", func(context.Context) (CoreObservation, error) { return CoreObservation{IdleConfirmed: true}, nil }},
		{"idle-with-generation", func(context.Context) (CoreObservation, error) {
			return CoreObservation{IdleConfirmed: true, RuntimeInstance: instance, Generation: 7}, nil
		}},
		{"observation-error", func(context.Context) (CoreObservation, error) { return CoreObservation{}, errors.New("unavailable") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control := &matrixLifecycleControl{}
			runtime := misterruntime.NewRuntime(control, "", 0, 0)
			keys := NewKeyboardSink()
			keys.SetPoster(runtime.SetKeyboard)
			keys.SetRetirer(runtime.RetireMatrixUncertainty)
			ports := &controllerPortsSink{keys: keys}
			controller := newTargetControllerWithSink("", ports)
			controller.ports, controller.keyboard = ports, keys
			controller.ObserveCore(tc.observe)
			controller.SetKeyboardMediaRetirement(runtime.MatrixMediaUncertainty, runtime.RetireMatrixMediaUncertainty, runtime.NeutralMatrixMediaUncertainty)
			if err := controller.ReleaseAll(context.Background()); err == nil {
				t.Fatal("unproved idle admitted cleanup")
			}
			if keys.retiredNoPost || control.neutralPosts != 0 {
				t.Fatal("unproved idle retired or posted")
			}
		})
	}
}

func TestIdleRetirementDoesNotAuthorizeLaterUnboundPost(t *testing.T) {
	instance, id, generation := strings.Repeat("a", 32), strings.Repeat("b", 64), uint64(7)
	control := &matrixLifecycleControl{response: misterruntime.Protocol2Response{OK: true, RuntimeInstance: &instance, Generation: &generation, ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: id}}}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	keys := NewKeyboardSink()
	keys.SetPoster(runtime.SetKeyboard)
	if err := keys.retireUnboundIdle(context.Background(), func(context.Context) (CoreObservation, error) {
		return CoreObservation{IdleConfirmed: true, RuntimeInstance: instance}, nil
	}, func(ctx context.Context) error { return ctx.Err() }); err != nil {
		t.Fatal(err)
	}
	if err := keys.ApplyFrom(context.Background(), sourceRemote, matrixKey(zx81keys.Letter('K'), remoteinput.ActionPress)); err == nil {
		t.Fatal("idle proof accepted an unbound press")
	}
	if len(keys.pressed[sourceRemote]) != 0 {
		t.Fatal("rejected unbound press was retained")
	}
	if err := keys.BindInitial(MatrixBinding{RuntimeInstance: instance, PackageID: id, Generation: generation}); err != nil {
		t.Fatal(err)
	}
	if keys.retiredNoPost {
		t.Fatal("new binding inherited idle no-post state")
	}
	if err := keys.ApplyFrom(context.Background(), sourceRemote, matrixKey(zx81keys.Letter('J'), remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if control.neutralPosts != 0 {
		t.Fatal("fresh bound press became neutral")
	}
}

func TestUnboundCleanupSamplesIdleOnEveryAttempt(t *testing.T) {
	instance := strings.Repeat("a", 32)
	control := &matrixLifecycleControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	keys := NewKeyboardSink()
	keys.SetPoster(runtime.SetKeyboard)
	ports := &controllerPortsSink{keys: keys}
	controller := newTargetControllerWithSink("", ports)
	controller.ports, controller.keyboard = ports, keys
	calls := 0
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		calls++
		if calls == 1 {
			return CoreObservation{IdleConfirmed: true, RuntimeInstance: instance}, nil
		}
		return CoreObservation{Active: true, RuntimeInstance: instance, Generation: 1}, nil
	})
	if err := controller.ReleaseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys.retiredNoPost {
		t.Fatal("idle proof leaked past cleanup")
	}
	if err := controller.ReleaseAll(context.Background()); err == nil {
		t.Fatal("stale idle proof allowed later active cleanup")
	}
	if calls != 2 {
		t.Fatalf("idle observations = %d", calls)
	}
}

func TestUnboundIdleProofRejectsCancellationAndTransition(t *testing.T) {
	for _, change := range []string{"cancel", "transition"} {
		t.Run(change, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			control := &matrixLifecycleControl{}
			runtime := misterruntime.NewRuntime(control, "", 0, 0)
			keys := NewKeyboardSink()
			keys.SetPoster(runtime.SetKeyboard)
			ports := &controllerPortsSink{keys: keys}
			controller := newTargetControllerWithSink("", ports)
			controller.ports, controller.keyboard = ports, keys
			controller.ObserveCore(func(context.Context) (CoreObservation, error) {
				if change == "cancel" {
					cancel()
				} else {
					done := controller.beginLifecycleTransition()
					done()
				}
				return CoreObservation{IdleConfirmed: true, RuntimeInstance: strings.Repeat("a", 32)}, nil
			})
			if err := controller.ReleaseAll(ctx); err == nil {
				t.Fatal("changed authority accepted")
			}
			if keys.retiredNoPost || control.neutralPosts != 0 {
				t.Fatal("changed authority retired or posted")
			}
		})
	}
}

type idleFailingFallback struct{}

func (idleFailingFallback) Apply(protocol.InputFrame) error { return nil }
func (idleFailingFallback) ReleaseAll() error               { return errors.New("pad cleanup failed") }
func (idleFailingFallback) Close() error                    { return nil }

func TestUnboundIdleProofDoesNotHideOtherCleanupFailure(t *testing.T) {
	control := &matrixLifecycleControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	keys := NewKeyboardSink()
	keys.SetPoster(runtime.SetKeyboard)
	ports := &controllerPortsSink{keys: keys, fallback: idleFailingFallback{}}
	controller := newTargetControllerWithSink("", ports)
	controller.ports, controller.keyboard = ports, keys
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{IdleConfirmed: true, RuntimeInstance: strings.Repeat("a", 32)}, nil
	})
	if err := controller.ReleaseAll(context.Background()); err == nil {
		t.Fatal("other cleanup failure hidden")
	}
}
