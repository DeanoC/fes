package input

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestLocalPadCleanupWaitsForEarlierUnionPost(t *testing.T) {
	f := newV2BackendFixture(t)
	oldEntered, oldRelease := make(chan struct{}), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(oldRelease) })
	var mu sync.Mutex
	calls := 0
	f.c.ports.poster = func(_ context.Context, _ string, _ uint64, _ uint8, _ uint8, _ uint16) error {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 2 {
			close(oldEntered)
			<-oldRelease
		}
		return nil
	}
	if err := f.c.ports.bind(&ControllerBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	pad := protocol.InputFrame{Device: uint8(remoteinput.DeviceGamepad), Kind: uint8(remoteinput.KindButton), Action: uint8(remoteinput.ActionPress), Code: uint16(remoteinput.ButtonA)}
	if err := f.c.deliverLocal(context.Background(), pad); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan error, 1)
	go func() {
		f.c.ports.mu.Lock()
		defer f.c.ports.mu.Unlock()
		oldDone <- f.c.ports.publishPortLocked(context.Background(), 0, true)
	}()
	select {
	case <-oldEntered:
	case <-time.After(time.Second):
		t.Fatal("old union post did not start")
	}
	cleanup := make(chan error, 1)
	go func() { cleanup <- f.c.ports.releaseSourceWithoutHIDContext(context.Background(), sourceLocal) }()
	select {
	case err := <-cleanup:
		t.Fatalf("cleanup confirmed while old union post was still in flight: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release.Do(func() { close(oldRelease) })
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-cleanup:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish after drain")
	}
}

func TestLocalPadCleanupCancellationDoesNotConfirmDrain(t *testing.T) {
	f := newV2BackendFixture(t)
	oldEntered, oldRelease := make(chan struct{}), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(oldRelease) })
	var mu sync.Mutex
	calls := 0
	var physical [2]uint8
	f.c.ports.poster = func(_ context.Context, _ string, _ uint64, port uint8, buttons uint8, _ uint16) error {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 2 {
			close(oldEntered)
			<-oldRelease
		}
		mu.Lock()
		physical[port] = buttons
		mu.Unlock()
		return nil
	}
	if err := f.c.ports.bind(&ControllerBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	pad := protocol.InputFrame{Device: uint8(remoteinput.DeviceGamepad), Kind: uint8(remoteinput.KindButton), Action: uint8(remoteinput.ActionPress), Code: uint16(remoteinput.ButtonA)}
	if err := f.c.deliverLocal(context.Background(), pad); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan error, 1)
	go func() {
		f.c.ports.mu.Lock()
		defer f.c.ports.mu.Unlock()
		oldDone <- f.c.ports.publishPortLocked(context.Background(), 0, true)
	}()
	select {
	case <-oldEntered:
	case <-time.After(time.Second):
		t.Fatal("old union post did not start")
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		err := f.c.ports.releaseSourceWithoutHIDContext(ctx, sourceLocal)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cleanup %d falsely confirmed drain: %v", i, err)
		}
	}
	mu.Lock()
	before := calls
	mu.Unlock()
	if before != 2 {
		t.Fatalf("cleanup posted before prior write drained: %d", before)
	}
	release.Do(func() { close(oldRelease) })
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	if err := f.c.ports.releaseSourceWithoutHIDContext(context.Background(), sourceLocal); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := physical[0]
	mu.Unlock()
	if got != 0 {
		t.Fatalf("confirmed cleanup left old physical pad union %#x", got)
	}
}

func TestLocalPadCleanupPreservesRemotePadAcrossDrain(t *testing.T) {
	f := newV2BackendFixture(t)
	oldEntered, oldRelease := make(chan struct{}), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(oldRelease) })
	var mu sync.Mutex
	var physical [2]uint8
	blockOld := false
	f.c.ports.poster = func(_ context.Context, _ string, _ uint64, port uint8, buttons uint8, _ uint16) error {
		mu.Lock()
		block := blockOld && port == 0 && buttons == 16
		if block {
			blockOld = false
		}
		mu.Unlock()
		if block {
			close(oldEntered)
			<-oldRelease
		}
		mu.Lock()
		physical[port] = buttons
		mu.Unlock()
		return nil
	}
	if err := f.c.ports.bind(&ControllerBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	local := protocol.InputFrame{Device: uint8(remoteinput.DeviceGamepad), Kind: uint8(remoteinput.KindButton), Action: uint8(remoteinput.ActionPress), Code: uint16(remoteinput.ButtonA)}
	remote := local
	remote.Code = uint16(remoteinput.ButtonB)
	if err := f.c.deliverLocal(context.Background(), local); err != nil {
		t.Fatal(err)
	}
	if err := f.c.ports.applyContext(context.Background(), sourceRemote, remote); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if physical != [2]uint8{16, 32} {
		t.Fatalf("initial physical pads = %v", physical)
	}
	blockOld = true
	mu.Unlock()
	oldDone := make(chan error, 1)
	go func() {
		f.c.ports.mu.Lock()
		defer f.c.ports.mu.Unlock()
		oldDone <- f.c.ports.publishPortLocked(context.Background(), 0, true)
	}()
	select {
	case <-oldEntered:
	case <-time.After(time.Second):
		t.Fatal("old local union did not start")
	}
	cleanup := make(chan error, 1)
	go func() { cleanup <- f.c.ports.releaseSourceWithoutHIDContext(context.Background(), sourceLocal) }()
	select {
	case err := <-cleanup:
		t.Fatalf("cleanup preceded old post drain: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release.Do(func() { close(oldRelease) })
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	if err := <-cleanup; err != nil {
		t.Fatal(err)
	}
	f.c.ports.mu.Lock()
	inflight := f.c.ports.inflight
	f.c.ports.mu.Unlock()
	if inflight != [2]int{} {
		t.Fatalf("cleanup left older port posts in flight: %v", inflight)
	}
	mu.Lock()
	got := physical
	mu.Unlock()
	if got != [2]uint8{32, 0} {
		t.Fatalf("remote pad was lost or local pad remained: %v", got)
	}
}
