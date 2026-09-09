package host_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestRemoteInputSourceExclusiveAndSessionBound(t *testing.T) {
	starter := &testBridgeStarter{}
	input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	ctx := context.Background()
	if err = input.Attach(ctx, "Pong"); err != nil {
		t.Fatal(err)
	}
	oldSession := input.Status().SessionID
	source, err := input.ClaimSource(oldSession)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = input.ClaimSource(oldSession); !errors.Is(err, host.ErrRemoteInputBusy) {
		t.Fatalf("second source: %v", err)
	}
	event, _ := remoteinput.NormalizeGamepad("a", true)
	if err = input.SendEvent(ctx, event, time.Now()); !errors.Is(err, host.ErrRemoteInputBusy) {
		t.Fatalf("desktop competitor: %v", err)
	}
	if err = source.SendEvent(ctx, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		starter.mu.Lock()
		sink := starter.sinks[0]
		starter.mu.Unlock()
		sink.mu.Lock()
		defer sink.mu.Unlock()
		return sink.releases > 0
	})
	next, err := input.ClaimSource(oldSession)
	if err != nil {
		t.Fatal(err)
	}
	if err = source.SendEvent(ctx, event, time.Now()); err == nil {
		t.Fatal("closed source sent input")
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	if err = next.SendEvent(ctx, event, time.Now()); err != nil {
		t.Fatalf("old close interfered: %v", err)
	}
	if err = input.Detach(ctx, "session_stop"); err != nil {
		t.Fatal(err)
	}
	if err = input.Attach(ctx, "Pong"); err != nil {
		t.Fatal(err)
	}
	if _, err = input.ClaimSource(oldSession); err == nil {
		t.Fatal("stale session claimed")
	}
	if err = next.SendEvent(ctx, event, time.Now()); err == nil {
		t.Fatal("stale source sent input")
	}
	current, err := input.ClaimSource(input.Status().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if err = next.Close(); err != nil {
		t.Fatal(err)
	}
	if err = current.SendEvent(ctx, event, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteInputSourceReclaimsReconnectWithoutWaitingForAnEvent(t *testing.T) {
	input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: &testBridgeStarter{}})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err = input.Attach(context.Background(), "Pong"); err != nil {
		t.Fatal(err)
	}
	sessionID := input.Status().SessionID
	source, err := input.ClaimSource(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	if status := input.Status(); status.State != host.RemoteInputReconnecting || status.Ready {
		t.Fatalf("closed source status = %#v", status)
	}
	replacement, err := input.ClaimSource(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if status := input.Status(); status.State != host.RemoteInputAttached || !status.Ready || status.SessionID != sessionID {
		t.Fatalf("reclaimed source status = %#v", status)
	}
}

func TestRemoteInputDesktopSourceBlocksLauncher(t *testing.T) {
	input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: &testBridgeStarter{}})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err = input.Attach(context.Background(), "Pong"); err != nil {
		t.Fatal(err)
	}
	event, _ := remoteinput.NormalizeGamepad("a", true)
	if err = input.Send(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err = input.ClaimSource(input.Status().SessionID); !errors.Is(err, host.ErrRemoteInputBusy) {
		t.Fatalf("claim over desktop: %v", err)
	}
}
