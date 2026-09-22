package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

func TestKitLeaseCleanupFreesWhenRuntimeSocketStaysClosed(t *testing.T) {
	err := kitLeaseCleanup(context.Background(), func(context.Context) bool { return false }, time.Millisecond,
		func(context.Context) error { return nil },
		func(context.Context) (protocol.Status, *protocol.APIError) {
			t.Fatal("stop called while runtime socket was closed")
			return protocol.Status{}, nil
		})
	if !errors.Is(err, kitlease.ErrRuntimeUnreachable) {
		t.Fatalf("cleanup = %v", err)
	}
}

func TestKitLeaseCleanupKeepsPeripheralFailureWhenSocketIsClosed(t *testing.T) {
	err := kitLeaseCleanup(context.Background(), func(context.Context) bool { return false }, time.Millisecond,
		func(context.Context) error { return errors.New("input still held") },
		func(context.Context) (protocol.Status, *protocol.APIError) {
			t.Fatal("stop called while runtime socket was closed")
			return protocol.Status{}, nil
		})
	if err == nil || errors.Is(err, kitlease.ErrRuntimeUnreachable) {
		t.Fatalf("cleanup = %v", err)
	}
}

func TestKitLeaseCleanupStopsAfterSocketOpens(t *testing.T) {
	stops := 0
	probes := 0
	err := kitLeaseCleanup(context.Background(), func(context.Context) bool {
		probes++
		return probes >= 2
	}, 200*time.Millisecond,
		func(context.Context) error { return nil },
		func(context.Context) (protocol.Status, *protocol.APIError) {
			stops++
			return protocol.Status{State: protocol.StateIdle}, nil
		})
	if err != nil || stops != 1 || probes < 2 {
		t.Fatalf("cleanup=%v stops=%d probes=%d", err, stops, probes)
	}
}

func TestKitLeaseCleanupBlocksWhenStopDoesNotReachIdle(t *testing.T) {
	err := kitLeaseCleanup(context.Background(), func(context.Context) bool { return true }, time.Millisecond,
		func(context.Context) error { return nil },
		func(context.Context) (protocol.Status, *protocol.APIError) {
			return protocol.Status{State: protocol.StateFailed, Recovery: protocol.RecoveryRebootRequired}, nil
		})
	if err == nil || errors.Is(err, kitlease.ErrRuntimeUnreachable) {
		t.Fatalf("cleanup = %v", err)
	}
}
