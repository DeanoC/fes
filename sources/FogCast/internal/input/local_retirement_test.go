package input

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

type localRetirementRig struct {
	controller  *TargetController
	sink        *controllerPortsSink
	observation CoreObservation
	probeErr    error
	stall       bool
	matrix      uint64
	posts       int
	timedOut    int
}

func newLocalRetirementRig(t *testing.T) *localRetirementRig {
	t.Helper()
	rig := &localRetirementRig{
		observation: CoreObservation{Active: true, Keyboard: true, CoreID: "fes.coleco", Generation: 1},
		matrix:      zx81keys.Neutral,
	}
	keys := NewKeyboardSink()
	keys.SetPoster(func(ctx context.Context, matrix uint64) error {
		rig.posts++
		if rig.stall {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("local matrix retirement has no deadline")
				return errors.New("missing local matrix deadline")
			}
			if remaining := time.Until(deadline); remaining > localCoreWriteTimeout+10*time.Millisecond {
				t.Errorf("local matrix deadline remaining %s exceeds %s", remaining, localCoreWriteTimeout)
			}
			<-ctx.Done()
			rig.timedOut++
			return ctx.Err()
		}
		rig.matrix = matrix
		return nil
	})
	rig.sink = &controllerPortsSink{keys: keys}
	rig.controller = newTargetControllerWithSink("127.0.0.1:0", rig.sink)
	rig.controller.ports = rig.sink
	rig.controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return rig.observation, rig.probeErr
	})
	if err := rig.controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if rig.matrix == zx81keys.Neutral {
		t.Fatal("initial local A did not establish a runtime matrix hold")
	}
	return rig
}

func (rig *localRetirementRig) refresh(t *testing.T) error {
	t.Helper()
	expireLocalObservation(rig.sink)
	started := time.Now()
	// Select has no joystick-matrix mapping. It cannot accidentally release
	// the held A and hide a lost retirement retry.
	err := rig.controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonSelect, remoteinput.ActionRelease, 0))
	if elapsed := time.Since(started); elapsed > 2*localCoreWriteTimeout+200*time.Millisecond {
		t.Errorf("local retirement frame exceeded its bounded write budget: %s", elapsed)
	}
	if !rig.controller.lifecycle.TryLock() {
		t.Fatal("retirement retained the local lifecycle lock")
	}
	rig.controller.lifecycle.Unlock()
	return err
}

func TestLocalMatrixRetirementTimeoutRetriesAfterProbeRecovers(t *testing.T) {
	for _, keyboardHeld := range []bool{false, true} {
		t.Run(fmt.Sprint("keyboard=", keyboardHeld), func(t *testing.T) {
			rig := newLocalRetirementRig(t)
			want := zx81keys.Neutral
			if keyboardHeld {
				if err := rig.controller.deliverLocal(context.Background(), keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)); err != nil {
					t.Fatal(err)
				}
				want = zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('J'): true})
			}
			held := rig.matrix
			rig.probeErr = errors.New("runtime probe unavailable")
			rig.stall = true
			if err := rig.refresh(t); err == nil {
				t.Fatal("failed observation accepted local play input")
			}
			if rig.timedOut != 1 || rig.matrix != held {
				t.Fatalf("failed retirement: timeouts=%d runtime=%x held=%x", rig.timedOut, rig.matrix, held)
			}
			rig.probeErr = nil
			rig.stall = false
			if err := rig.refresh(t); err != nil {
				t.Fatal(err)
			}
			if rig.matrix != want {
				t.Fatalf("recovered observation left phantom local A or lost physical keyboard: runtime=%x want=%x", rig.matrix, want)
			}
		})
	}
}

func TestLocalMatrixRetirementRetriesRemainBounded(t *testing.T) {
	rig := newLocalRetirementRig(t)
	rig.probeErr = errors.New("runtime probe unavailable")
	rig.stall = true
	for attempt := 1; attempt <= 2; attempt++ {
		if err := rig.refresh(t); err == nil {
			t.Fatal("failed observation accepted local play input")
		}
		if rig.timedOut != attempt {
			t.Fatalf("retirement attempt %d lost its retry: timeout count=%d", attempt, rig.timedOut)
		}
	}
}

func TestLocalMatrixRetirementConfirmedCoreChangeDiscardsPendingRelease(t *testing.T) {
	for _, next := range []struct {
		name        string
		observation CoreObservation
	}{
		{name: "idle"},
		{name: "different_core", observation: CoreObservation{Active: true, Keyboard: true, CoreID: "fes.zx81", Generation: 2}},
		{name: "same_core_new_generation", observation: CoreObservation{Active: true, Keyboard: true, CoreID: "fes.coleco", Generation: 2}},
	} {
		t.Run(next.name, func(t *testing.T) {
			rig := newLocalRetirementRig(t)
			rig.probeErr = errors.New("runtime probe unavailable")
			rig.stall = true
			if err := rig.refresh(t); err == nil {
				t.Fatal("failed observation accepted local play input")
			}
			rig.probeErr = nil
			// Confirmed idle, a different core, or a new generation retires the
			// old runtime input. A later release must not target a replacement.
			rig.matrix = zx81keys.Neutral
			rig.observation = next.observation
			posts := rig.posts
			err := rig.refresh(t)
			if !next.observation.Active && !errors.Is(err, errNoCore) {
				t.Fatalf("confirmed idle: %v", err)
			}
			if next.observation.Active && err != nil {
				t.Fatalf("confirmed replacement: %v", err)
			}
			if rig.posts != posts {
				t.Fatal("pending old release was posted after confirmed retirement")
			}
			// Return to the same core ID with reset hardware state. There must
			// be no retained A hold and no late neutral request on this core.
			rig.observation = CoreObservation{Active: true, Keyboard: true, CoreID: "fes.coleco", Generation: 3}
			if err := rig.refresh(t); err != nil {
				t.Fatal(err)
			}
			if rig.posts != posts || rig.matrix != zx81keys.Neutral {
				t.Fatalf("replacement inherited retired input: posts=%d want=%d runtime=%x", rig.posts, posts, rig.matrix)
			}
		})
	}
}
