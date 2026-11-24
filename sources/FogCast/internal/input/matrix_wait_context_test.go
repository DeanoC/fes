package input

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestMatrixCanceledWaitDoesNotOvertakeInFlightPost(t *testing.T) {
	for _, mode := range []string{"edge", "replace-source"} {
		t.Run(mode, func(t *testing.T) {
			sink, binding := matrixFixture(t)
			entered := make(chan uint64, 4)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			sink.SetPoster(func(_ context.Context, _ localhid.MatrixPost, matrix uint64) error {
				entered <- matrix
				<-release // Model an already-dispatched writer that must drain.
				return nil
			})
			owner := make(chan error, 1)
			go func() {
				owner <- sink.applyFromBound(context.Background(), sourceRemote, matrixKey(zx81keys.Letter('A'), remoteinput.ActionPress), &binding, nil)
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("owner did not dispatch")
			}
			wait, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			var err error
			if mode == "edge" {
				err = sink.applyFromBound(wait, sourceLocal, matrixKey(zx81keys.Letter('B'), remoteinput.ActionPress), &binding, nil)
			} else {
				err = sink.replaceSource(wait, sourceLocalMatrix, map[remoteinput.Code]bool{zx81keys.Letter('B'): true})
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("blocked caller = %v", err)
			}
			successor := make(chan error, 1)
			go func() {
				successor <- sink.applyFromBound(context.Background(), sourceRemote, matrixKey(zx81keys.Letter('A'), remoteinput.ActionRelease), &binding, nil)
			}()
			select {
			case <-entered:
				t.Fatal("successor wrote before the in-flight owner drained")
			case <-time.After(30 * time.Millisecond):
			}
			unblock()
			if err := <-owner; err != nil {
				t.Fatal(err)
			}
			if err := <-successor; err != nil {
				t.Fatal(err)
			}
			select {
			case matrix := <-entered:
				if matrix != zx81keys.Neutral {
					t.Fatalf("canceled waiter changed successor union: %#x", matrix)
				}
			default:
				t.Fatal("successor did not post")
			}
		})
	}
}

func TestMatrixObservationWaitHonorsContext(t *testing.T) {
	sink, binding := matrixFixture(t)
	ports := &controllerPortsSink{keys: sink}
	sink.mu.Lock()
	locked := true
	defer func() {
		if locked {
			sink.mu.Unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- ports.setObservationContext(ctx, CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation})
	}()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("observation = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		sink.mu.Unlock()
		locked = false
		<-result
		t.Fatal("observation ignored cancellation while matrix sink was busy")
	}
}

func TestMatrixObservationWaitsForPortsPosterWithoutOvertaking(t *testing.T) {
	sink, binding := matrixFixture(t)
	ports := &controllerPortsSink{keys: sink}
	obs := CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation}
	if err := ports.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	sink.SetPoster(func(context.Context, localhid.MatrixPost, uint64) error { entered <- struct{}{}; <-release; return nil })
	owner := make(chan error, 1)
	go func() {
		owner <- ports.applyWithBinding(context.Background(), sourceRemote, matrixKey(zx81keys.Letter('A'), remoteinput.ActionPress), &binding, nil, nil, false)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("owner did not enter poster")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := ports.setObservationContext(ctx, obs); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("observation = %v", err)
	}
	successor := make(chan error, 1)
	go func() {
		successor <- ports.applyWithBinding(context.Background(), sourceRemote, matrixKey(zx81keys.Letter('A'), remoteinput.ActionRelease), &binding, nil, nil, false)
	}()
	select {
	case <-entered:
		t.Fatal("successor posted before owner drained")
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	if err := <-owner; err != nil {
		t.Fatal(err)
	}
	if err := <-successor; err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("successor did not post")
	}
}
