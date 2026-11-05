package host

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func TestCanceledLauncherEventDoesNotAlterDisconnectedStream(t *testing.T) {
	bridge := &refusedLeaseBridge{code: protocol.CodeKitLeaseDenied}
	r := &RemoteInput{
		bridge: bridge, state: RemoteInputAttached, ready: true,
		session: 1, source: "launcher", sourceGeneration: 1,
		now: time.Now, grace: time.Second, dial: time.Second,
	}
	source := &RemoteInputSource{owner: r, session: 1, generation: 1}

	// Cancellation occurs while this input is waiting for the same lock held
	// by the heartbeat or another input operation.
	r.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		done <- source.SendEvent(ctx, tapA, time.Now())
	}()
	<-started
	cancel()
	r.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled event: %v", err)
	}
	if bridge.dials != 0 || r.state != RemoteInputAttached || !r.ready || r.inputState.Pressed(tapA.Code) {
		t.Fatalf("canceled event changed stream: dials=%d state=%s ready=%v pressed=%v", bridge.dials, r.state, r.ready, r.inputState.Pressed(tapA.Code))
	}
}

func TestCanceledLauncherEventDoesNotSendOnLiveStream(t *testing.T) {
	r, frames := tapRig(t)
	r.source = "launcher"
	r.sourceGeneration = 1
	source := &RemoteInputSource{owner: r, session: 7, generation: 1}
	r.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		done <- source.SendEvent(ctx, tapA, time.Now())
	}()
	<-started
	cancel()
	r.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled event: %v", err)
	}
	if r.inputState.Pressed(tapA.Code) {
		t.Fatal("canceled event changed key state")
	}
	select {
	case frame := <-frames:
		t.Fatalf("canceled event sent frame: %+v", frame)
	case <-time.After(30 * time.Millisecond):
	}
}
