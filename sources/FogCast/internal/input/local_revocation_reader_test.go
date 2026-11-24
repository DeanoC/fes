package input

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/zx81keys"
)

// A confirmed manager cleanup must eventually release the reader after EOF;
// the old lease cannot admit a further local input operation after revocation.
func TestLocalReaderRetiresAfterConfirmedLeaseCleanup(t *testing.T) {
	f := newV2BackendFixture(t)
	manager := kitlease.New(time.Minute, func(ctx context.Context) error {
		if err := f.c.ReleaseAll(ctx); err != nil {
			return err
		}
		kitlease.ConfirmInputCleanup(ctx)
		return nil
	})
	t.Cleanup(manager.Close)
	f.c.SetLocalLeaseManager(manager)
	startup, stopStartup := context.WithTimeout(context.Background(), time.Second)
	defer stopStartup()
	if _, err := manager.Settled(startup); err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("c", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f.req.LeaseGeneration = lease.Status.Generation
	binding := KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	f.c.ports.setObservation(CoreObservation{Active: true, RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration, KeyboardHID: &binding})
	f.c.localKeyboardGrant = &localKeyboardGrant{executionGeneration: lease.Status.Generation, binding: binding}
	testEnableRecovery(t, f.c)
	f.c.localV2Guard.Store(true)
	client, peer := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); f.c.readLocal(ctx, peer, true) }()
	testRecoverConnection(t, f.c, client)
	begin := v2BeginFor(f)
	begin.Request = 2
	reply := v2Exchange(t, client, begin)
	if reply.Status != "ready" {
		t.Fatalf("begin = %+v", reply)
	}
	edge := localhid.Request{Version: 2, Op: "edge", Request: 3, Token: reply.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if r := v2Exchange(t, client, edge); r.Status != "accepted" {
		t.Fatalf("edge = %+v", r)
	}
	if _, err := manager.Release(lease.Token); err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	status, err := manager.Settled(wait)
	if err != nil || status.State != "free" {
		t.Fatalf("manager cleanup: %+v, %v", status, err)
	}
	f.mu.Lock()
	if len(f.rows) < 2 || f.rows[len(f.rows)-1][0]&(1<<4) != 0 {
		f.mu.Unlock()
		t.Fatal("manager cleanup failed to neutralize held key")
	}
	f.mu.Unlock()
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(1200 * time.Millisecond):
		t.Fatal("reader remains blocked after confirmed lease cleanup")
	}
	next, nextPeer := net.Pipe()
	defer next.Close()
	nextDone := make(chan struct{})
	go func() { defer close(nextDone); f.c.readLocal(ctx, nextPeer, true) }()
	testRecoverConnection(t, f.c, next)
	_ = next.Close()
	select {
	case <-nextDone:
	case <-time.After(time.Second):
		t.Fatal("successor reader did not drain")
	}
}

func TestLocalMatrixReaderRetiresAfterConfirmedLeaseCleanup(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	var calls atomic.Int32
	manager := kitlease.New(time.Minute, func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			return nil // no source exists during this fixture's startup
		}
		if err := f.c.ReleaseAll(ctx); err != nil {
			return err
		}
		kitlease.ConfirmInputCleanup(ctx)
		return nil
	})
	t.Cleanup(manager.Close)
	f.c.SetLocalLeaseManager(manager)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	lease, err := manager.ClaimLocal()
	if err != nil {
		t.Fatal(err)
	}
	f.req.LeaseGeneration = lease.Status.Generation
	testEnableRecovery(t, f.c)
	f.c.localV2Guard.Store(true)
	client, peer := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() { defer close(done); f.c.readLocal(ctx, peer, true) }()
	testRecoverConnection(t, f.c, client)
	f.req.Request = 2
	begin := v2Exchange(t, client, f.req)
	if begin.Status != "ready" {
		t.Fatalf("begin = %+v", begin)
	}
	edge := localhid.Request{Version: 2, Op: "edge", Request: 3, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if r := v2Exchange(t, client, edge); r.Status != "accepted" {
		t.Fatalf("edge = %+v", r)
	}
	if _, err := manager.Release(lease.Token); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Settled(ctx)
	if err != nil || status.State != "free" {
		t.Fatalf("manager cleanup: %+v, %v", status, err)
	}
	if len(f.posts) < 2 || f.posts[len(f.posts)-1] != zx81keys.Neutral {
		t.Fatalf("matrix not neutral: %v", f.posts)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("matrix reader remains blocked after confirmed cleanup")
	}
	next, nextPeer := net.Pipe()
	defer next.Close()
	nextDone := make(chan struct{})
	go func() { defer close(nextDone); f.c.readLocal(ctx, nextPeer, true) }()
	testRecoverConnection(t, f.c, next)
	_ = next.Close()
	select {
	case <-nextDone:
	case <-ctx.Done():
		t.Fatal("successor reader did not drain")
	}
}

func TestLocalReaderDoesNotRetireAfterUnconfirmedLeaseCleanup(t *testing.T) {
	f := newV2BackendFixture(t)
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	f.c.SetLocalLeaseManager(manager)
	wait, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if _, err := manager.Settled(wait); err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("d", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f.req.LeaseGeneration = lease.Status.Generation
	f.c.localKeyboardGrant.executionGeneration = lease.Status.Generation
	testEnableRecovery(t, f.c)
	f.c.localV2Guard.Store(true)
	client, peer := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); f.c.readLocal(ctx, peer, true) }()
	testRecoverConnection(t, f.c, client)
	begin := v2BeginFor(f)
	begin.Request = 2
	reply := v2Exchange(t, client, begin)
	if reply.Status != "ready" {
		t.Fatal(reply)
	}
	if r := v2Exchange(t, client, localhid.Request{Version: 2, Op: "edge", Request: 3, Token: reply.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}); r.Status != "accepted" {
		t.Fatal(r)
	}
	if _, err := manager.Release(lease.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Settled(wait); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	select {
	case <-done:
		t.Fatal("unmarked cleanup allowed successor recovery")
	case <-time.After(650 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not stop on shutdown")
	}
}
