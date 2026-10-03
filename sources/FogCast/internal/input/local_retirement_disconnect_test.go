package input

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestLocalMatrixRetirementDisconnectKeepsFailedNeutralPending(t *testing.T) {
	for _, keyboardHeld := range []bool{false, true} {
		t.Run(fmt.Sprint("keyboard=", keyboardHeld), func(t *testing.T) {
			rig := newLocalRetirementRig(t)
			if keyboardHeld {
				if err := rig.controller.deliverLocal(context.Background(), keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)); err != nil {
					t.Fatal(err)
				}
			}
			held := rig.matrix
			// The local observation path must exhaust its bounded neutral budget.
			// Disconnect cleanup uses the poster's own deadline, so its simulated
			// unavailable runtime returns immediately instead of waiting forever.
			rig.sink.keys.SetPoster(func(ctx context.Context, matrix uint64) error {
				rig.posts++
				if rig.stall {
					if _, bounded := ctx.Deadline(); bounded {
						<-ctx.Done()
						rig.timedOut++
						return ctx.Err()
					}
					return errors.New("runtime write unavailable")
				}
				rig.matrix = matrix
				return nil
			})
			rig.probeErr = errors.New("runtime probe unavailable")
			rig.stall = true
			if err := rig.refresh(t); err == nil {
				t.Fatal("failed observation accepted local input")
			}
			if rig.timedOut != 1 || !rig.sink.pendingMatrixNeutral {
				t.Fatalf("retirement lost pending neutral: timeouts=%d pending=%t", rig.timedOut, rig.sink.pendingMatrixNeutral)
			}
			// Persistent probe errors expire the kit's bound-core cache and close
			// its socket. The agent's disconnect cleanup still cannot reach runtime.
			_ = rig.sink.releaseSource(sourceLocal)
			if rig.matrix != held {
				t.Fatal("failed disconnect cleanup changed the simulated runtime")
			}
			rig.probeErr = nil
			rig.stall = false
			// This unrelated Select release must first retire the abandoned pad
			// and keyboard holds when the same core generation becomes reachable.
			if err := rig.refresh(t); err != nil {
				t.Fatal(err)
			}
			if rig.matrix != zx81keys.Neutral || rig.sink.pendingMatrixNeutral {
				t.Fatalf("disconnect lost retirement retry: runtime=%x pending=%t", rig.matrix, rig.sink.pendingMatrixNeutral)
			}
		})
	}
}

func TestLocalKeyboardDisconnectRetriesFailedNeutralWithoutPadHold(t *testing.T) {
	rig := newLocalRetirementRig(t)
	if err := rig.controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if rig.matrix != zx81keys.Neutral || len(rig.sink.localKeys) != 0 {
		t.Fatal("initial pad release did not retire the synthetic matrix hold")
	}
	if err := rig.controller.deliverLocal(context.Background(), keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	held := rig.matrix
	if held == zx81keys.Neutral {
		t.Fatal("physical keyboard did not establish a runtime hold")
	}
	rig.sink.keys.SetPoster(func(_ context.Context, matrix uint64) error {
		if rig.stall {
			return errors.New("runtime write unavailable")
		}
		rig.matrix = matrix
		return nil
	})
	rig.stall = true
	if err := rig.sink.releaseSource(sourceLocal); err == nil {
		t.Fatal("failed physical-keyboard disconnect reported success")
	}
	if rig.matrix != held || !rig.sink.pendingMatrixNeutral {
		t.Fatalf("physical-keyboard disconnect lost its retry: runtime=%x pending=%t", rig.matrix, rig.sink.pendingMatrixNeutral)
	}
	rig.stall = false
	if err := rig.refresh(t); err != nil {
		t.Fatal(err)
	}
	if rig.matrix != zx81keys.Neutral || rig.sink.pendingMatrixNeutral {
		t.Fatalf("physical keyboard remained held after recovery: runtime=%x pending=%t", rig.matrix, rig.sink.pendingMatrixNeutral)
	}
}
