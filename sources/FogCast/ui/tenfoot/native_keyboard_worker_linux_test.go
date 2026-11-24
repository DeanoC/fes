//go:build linux

package tenfoot

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
)

func workerApp(t *testing.T) (*App, nativeGuestBinding, localhid.DeviceID) {
	t.Helper()
	s, b, id := nativeGuestTestSource(t)
	a := NewApp(nil, 1280, 720, 20)
	t.Cleanup(a.Stop)
	a.mu.Lock()
	a.delegatedV2Active = true
	a.nativeGuest = *s
	a.nativeGuestBinding = b
	a.nativeGuestBeginRequest = &localhid.Request{Op: "begin"}
	a.mu.Unlock()
	return a, b, id
}

func TestNativeKeyboardAwaitRetirementDoesNotTreatTimeoutAsNeutral(t *testing.T) {
	a, b, _ := workerApp(t)
	op, _, ok := a.nativeKeyboardTakeAction(b, b)
	if !ok {
		t.Fatal("missing begin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := a.nativeKeyboardAwaitRetirement(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pending begin: %v", err)
	}
	a.nativeKeyboardCancelUnsent(op)
	if err := a.nativeKeyboardAwaitRetirement(context.Background()); err != nil {
		t.Fatalf("unsent source: %v", err)
	}
}

func TestNativeKeyboardAwaitRetirementWaitsForPadDrain(t *testing.T) {
	a := NewApp(nil, 1280, 720, 20)
	defer a.Stop()
	a.mu.Lock()
	a.nativePadRetiring = true
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := a.nativeKeyboardAwaitRetirement(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("undrained pad: %v", err)
	}
	a.mu.Lock()
	a.nativePadRetiring = false
	a.mu.Unlock()
	if err := a.nativeKeyboardAwaitRetirement(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeKeyboardWorkerNeverSendsStaleOrReplaysLostBegin(t *testing.T) {
	a, b, _ := workerApp(t)
	calls := 0
	observe := func() (nativeGuestBinding, error) {
		calls++
		if calls > 1 {
			b.Runtime.Generation++
		}
		return b, nil
	}
	sent := 0
	worked, err := a.nativeKeyboardRunOne(context.Background(), b, observe, func(context.Context, localhid.Request) (localhid.Reply, error) { sent++; return localhid.Reply{}, nil })
	if !worked || err == nil || sent != 0 {
		t.Fatalf("stale action worked=%v err=%v sent=%d", worked, err, sent)
	}

	a2, b2, _ := workerApp(t)
	observe2 := func() (nativeGuestBinding, error) { return b2, nil }
	send := func(context.Context, localhid.Request) (localhid.Reply, error) {
		sent++
		return localhid.Reply{}, io.EOF
	}
	worked, err = a2.nativeKeyboardRunOne(context.Background(), b2, observe2, send)
	if !worked || !errors.Is(err, io.EOF) {
		t.Fatalf("lost ACK worked=%v err=%v", worked, err)
	}
	before := sent
	worked, _ = a2.nativeKeyboardRunOne(context.Background(), b2, observe2, send)
	if worked || sent != before {
		t.Fatal("ambiguous BEGIN was replayed")
	}
}

func TestNativeKeyboardWorkerDropsUnsentStaleBegin(t *testing.T) {
	a, b, _ := workerApp(t)
	changed := b
	changed.Runtime.Generation++
	if _, _, ok := a.nativeKeyboardTakeAction(b, changed); ok {
		t.Fatal("sent stale BEGIN")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nativeGuest.phase != nativeGuestIdle || a.nativeGuestSnapshotAllowed || a.nativeGuest.lastGeneration != b.AuthorityGeneration {
		t.Fatal("stale source did not require a fresh snapshot")
	}
}

func TestNativeKeyboardOldWorkerCannotTakeNewSourceAction(t *testing.T) {
	a, old, _ := workerApp(t)
	newer := old
	newer.QueueEpoch++
	newer.AuthorityGeneration++
	a.mu.Lock()
	a.nativeGuestBinding = newer
	a.mu.Unlock()
	if _, _, ok := a.nativeKeyboardTakeAction(old, newer); ok {
		t.Fatal("old worker took new source action")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nativeGuestBeginRequest == nil {
		t.Fatal("old worker discarded new source BEGIN")
	}
}

func TestNativeKeyboardWorkerOrdersAcknowledgedEdgesAndReset(t *testing.T) {
	a, b, id := workerApp(t)
	a.mu.Lock()
	if err := a.nativeGuest.rawEdge(b, nativeRawEdge{Device: id, Sequence: 11, Code: 1, Down: false}); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	begin, req, ok := a.nativeKeyboardTakeAction(b, b)
	if !ok || req.Op != "begin" {
		t.Fatal("missing BEGIN")
	}
	if _, _, ok := a.nativeKeyboardTakeAction(b, b); ok {
		t.Fatal("concurrent BEGIN")
	}
	token := strings.Repeat("c", 64)
	a.nativeKeyboardCompleteAction(begin, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	edge, req, ok := a.nativeKeyboardTakeAction(b, b)
	if !ok || req.Op != "edge" || req.Down || req.EventSeq != 1 {
		t.Fatalf("held release %+v", req)
	}
	a.nativeKeyboardCompleteAction(begin, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	if _, _, ok := a.nativeKeyboardTakeAction(b, b); ok {
		t.Fatal("stale completion acknowledged pending edge")
	}
	changed := b
	changed.Runtime.Generation++
	a.nativeKeyboardCompleteAction(edge, changed, localhid.Reply{Op: "edge", Status: "accepted", Token: token}, nil)
	reset, req, ok := a.nativeKeyboardTakeAction(b, changed)
	if !ok || req.Op != "reset" || req.Token != token {
		t.Fatalf("missing old-source reset %+v", req)
	}
	a.nativeKeyboardCompleteAction(reset, changed, localhid.Reply{Op: "reset", Status: "neutral-confirmed", Token: token}, nil)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nativeGuest.phase != nativeGuestIdle || a.nativeGuestSnapshotAllowed {
		t.Fatal("reset granted a snapshot without new authority")
	}
}

func TestNativeKeyboardWorkerUnsentEdgeRequiresResetWithoutReplay(t *testing.T) {
	a, b, id := workerApp(t)
	begin, _, ok := a.nativeKeyboardTakeAction(b, b)
	if !ok {
		t.Fatal("missing begin")
	}
	token := strings.Repeat("c", 64)
	a.nativeKeyboardCompleteAction(begin, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	a.mu.Lock()
	if err := a.nativeGuest.rawEdge(b, nativeRawEdge{Device: id, Sequence: 11, Code: 1, Down: false}); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	edge, _, ok := a.nativeKeyboardTakeAction(b, b)
	if !ok {
		t.Fatal("missing edge")
	}
	a.nativeKeyboardCancelUnsent(edge)
	reset, req, ok := a.nativeKeyboardTakeAction(b, b)
	if !ok || req.Op != "reset" || req.Token != token || req.LastEventSeq != 0 {
		t.Fatalf("unsent edge did not reset source: %+v", req)
	}
	a.nativeKeyboardCancelUnsent(edge) // Stale cookie cannot alter reset.
	if _, _, ok := a.nativeKeyboardTakeAction(b, b); ok {
		t.Fatal("stale cookie changed in-flight reset")
	}
	a.nativeKeyboardCancelUnsent(reset)
	if _, req, ok := a.nativeKeyboardTakeAction(b, b); !ok || req.Op != "reset" {
		t.Fatal("unsent reset was not retryable")
	}
}
