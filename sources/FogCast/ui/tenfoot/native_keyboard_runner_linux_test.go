//go:build linux

package tenfoot

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

type fakeNativeRecoveryFeed struct {
	reply                      localhid.Reply
	recoverErr, promoteErr     error
	beforeReply, beforePromote func()
	closed, promoted           bool
}

func TestNativeKeyboardConnectionDrainsSnapshotEdgeAndReset(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	client, peer := net.Pipe() // In-memory only; snapshot and target are simulated.
	defer peer.Close()
	_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
	feed := localfeed.New("unused", func(string, string, time.Duration) (net.Conn, error) { return client, nil })
	defer feed.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type recovery struct {
		handle  localfeed.V2Handle
		binding nativeGuestBinding
		err     error
	}
	recovered := make(chan recovery, 1)
	go func() {
		h, b, err := a.nativeKeyboardRecoverOnce(ctx, feed, [16]byte{1}, strings.Repeat("e", 32), time.Second)
		recovered <- recovery{h, b, err}
	}()
	reader := bufio.NewReader(peer)
	respond := func(op, status, token string) localhid.Request {
		t.Helper()
		req, err := localhid.ReadRequest(reader)
		if err != nil || req.Op != op {
			t.Fatalf("want %s request=%+v err=%v", op, req, err)
		}
		reply := localhid.Reply{Version: 2, Op: op, Request: req.Request, Status: status, Token: token}
		if op == "recover" {
			reply.AgentInstance, reply.TargetID, reply.BootID = req.AgentInstance, req.TargetID, req.BootID
		}
		wire, err := localhid.EncodeReply(reply)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peer.Write(wire); err != nil {
			t.Fatal(err)
		}
		return req
	}
	respond("recover", "reader-drained", "")
	got := <-recovered
	if got.err != nil {
		t.Fatal(got.err)
	}
	device := localhid.DeviceID{1}
	boundary := nativeRawBoundary{Publisher: got.binding.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: device, Held: []uint16{42}}}}
	if err := a.publishNativeKeyboardBoundary(got.binding.AuthorityGeneration, boundary); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		finished <- a.runNativeKeyboardConnection(ctx, feed, got.handle, got.binding, 10*time.Millisecond)
	}()
	token := strings.Repeat("c", 64)
	begin := respond("begin", "ready", token)
	if len(begin.Devices) != 1 || len(begin.Devices[0].Held) != 1 || begin.Devices[0].Held[0] != 0xe1 {
		t.Fatalf("snapshot %+v", begin.Devices)
	}
	if _, err := a.recordNativeKeyboardEdge(got.binding.AuthorityGeneration, nativeRawEdge{Device: device, Sequence: 11, Code: 42, Down: false}); err != nil {
		t.Fatal(err)
	}
	edge := respond("edge", "accepted", token)
	if edge.Down || edge.Usage != 0xe1 || edge.EventSeq != 1 {
		t.Fatalf("held release %+v", edge)
	}
	a.nativeKeyboardAuthorityLost(got.binding)
	reset := respond("reset", "neutral-confirmed", token)
	if reset.LastEventSeq != 1 {
		t.Fatalf("reset sequence %+v", reset)
	}
	if err := <-finished; err != nil {
		t.Fatalf("worker: %v", err)
	}
	if err := a.nativeKeyboardAwaitRetirement(ctx); err != nil {
		t.Fatalf("retirement: %v", err)
	}
}

func TestNativeKeyboardConnectionLossNeverConfirmsNeutral(t *testing.T) {
	for _, lostAt := range []string{"idle", "begin", "edge", "reset"} {
		t.Run(lostAt, func(t *testing.T) {
			a := readyNativeBindingApp(t)
			defer a.Stop()
			client, peer := net.Pipe()
			defer peer.Close()
			_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
			feed := localfeed.New("unused", func(string, string, time.Duration) (net.Conn, error) { return client, nil })
			defer feed.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			type result struct {
				handle  localfeed.V2Handle
				binding nativeGuestBinding
				err     error
			}
			recovered := make(chan result, 1)
			go func() {
				h, b, err := a.nativeKeyboardRecoverOnce(ctx, feed, [16]byte{1}, strings.Repeat("e", 32), time.Second)
				recovered <- result{h, b, err}
			}()
			reader := bufio.NewReader(peer)
			read := func(op string) localhid.Request {
				t.Helper()
				req, err := localhid.ReadRequest(reader)
				if err != nil || req.Op != op {
					t.Fatalf("%s request=%+v err=%v", op, req, err)
				}
				return req
			}
			reply := func(req localhid.Request, status, token string) {
				t.Helper()
				r := localhid.Reply{Version: 2, Op: req.Op, Request: req.Request, Status: status, Token: token}
				if req.Op == "recover" {
					r.AgentInstance, r.TargetID, r.BootID = req.AgentInstance, req.TargetID, req.BootID
				}
				wire, err := localhid.EncodeReply(r)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = peer.Write(wire); err != nil {
					t.Fatal(err)
				}
			}
			recoverReq := read("recover")
			reply(recoverReq, "reader-drained", "")
			got := <-recovered
			if got.err != nil {
				t.Fatal(got.err)
			}
			device := localhid.DeviceID{1}
			if lostAt != "idle" {
				if err := a.publishNativeKeyboardBoundary(got.binding.AuthorityGeneration, nativeRawBoundary{Publisher: got.binding.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: device, Held: []uint16{42}}}}); err != nil {
					t.Fatal(err)
				}
			}
			finished := make(chan error, 1)
			go func() {
				finished <- a.runNativeKeyboardConnection(ctx, feed, got.handle, got.binding, 10*time.Millisecond)
			}()
			token := strings.Repeat("c", 64)
			if lostAt != "idle" {
				begin := read("begin")
				if lostAt != "begin" {
					reply(begin, "ready", token)
					if lostAt == "edge" {
						if _, err := a.recordNativeKeyboardEdge(got.binding.AuthorityGeneration, nativeRawEdge{Device: device, Sequence: 11, Code: 42, Down: false}); err != nil {
							t.Fatal(err)
						}
						read("edge")
					} else {
						a.nativeKeyboardAuthorityLost(got.binding)
						read("reset")
					}
				}
			}
			_ = peer.Close()
			if err := <-finished; err == nil {
				t.Fatal("lost connection reported clean retirement")
			}
			if _, permit := a.nativeKeyboardSnapshotPermit(); permit {
				t.Fatal("lost connection left snapshot permission")
			}
			a.mu.Lock()
			quarantined, phase := a.nativeKeyboardQuarantined, a.nativeGuest.phase
			a.mu.Unlock()
			if !quarantined || (lostAt != "idle" && phase != nativeGuestUncertain) {
				t.Fatalf("lost connection quarantine=%v phase=%v", quarantined, phase)
			}
		})
	}
}

func (f *fakeNativeRecoveryFeed) RecoverV2(_ context.Context, previous, target, boot string, _ time.Duration) (localhid.Reply, localfeed.V2Handle, error) {
	if f.beforeReply != nil {
		f.beforeReply()
	}
	if f.recoverErr != nil {
		return localhid.Reply{}, localfeed.V2Handle{}, f.recoverErr
	}
	return localhid.Reply{Version: 2, Op: "recover", Request: 1, AgentInstance: previous, TargetID: target, BootID: boot, Status: "reader-drained"}, localfeed.V2Handle{}, nil
}
func (f *fakeNativeRecoveryFeed) PromoteRecovered(_ localfeed.V2Handle, reply localhid.Reply) error {
	if f.beforePromote != nil {
		f.beforePromote()
	}
	if f.promoteErr != nil {
		return f.promoteErr
	}
	f.reply, f.promoted = reply, true
	return nil
}
func (f *fakeNativeRecoveryFeed) CloseV2(localfeed.V2Handle) { f.closed = true }

func TestNativeKeyboardRecoveryRechecksAuthorityBeforeSnapshot(t *testing.T) {
	for _, change := range []string{"none", "before-reply", "promotion-failed", "transport"} {
		t.Run(change, func(t *testing.T) {
			a := readyNativeBindingApp(t)
			defer a.Stop()
			feed := &fakeNativeRecoveryFeed{}
			mutate := func() { a.mu.Lock(); a.session.InputAgentInstance = strings.Repeat("f", 32); a.mu.Unlock() }
			switch change {
			case "before-reply":
				feed.beforeReply = mutate
			case "promotion-failed":
				feed.promoteErr = errors.New("busy")
			case "transport":
				feed.recoverErr = errors.New("unavailable")
			}
			_, _, err := a.nativeKeyboardRecoverOnce(context.Background(), feed, [16]byte{1}, strings.Repeat("e", 32), time.Second)
			_, permit := a.nativeKeyboardSnapshotPermit()
			if change == "none" {
				if err != nil || !permit || !feed.promoted || feed.closed {
					t.Fatalf("recovery err=%v permit=%v feed=%+v", err, permit, feed)
				}
			} else if err == nil || permit || !feed.closed {
				t.Fatalf("%s admitted err=%v permit=%v feed=%+v", change, err, permit, feed)
			}
		})
	}
}

func TestNativeKeyboardPromotionKeepsAuthorityLocked(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	started, finished := make(chan struct{}), make(chan struct{})
	feed := &fakeNativeRecoveryFeed{}
	feed.beforePromote = func() {
		go func() {
			close(started)
			a.mu.Lock()
			a.session.InputAgentInstance = strings.Repeat("f", 32)
			a.mu.Unlock()
			close(finished)
		}()
		<-started
		select {
		case <-finished:
			t.Error("authority changed during promotion")
		default:
		}
	}
	if _, _, err := a.nativeKeyboardRecoverOnce(context.Background(), feed, [16]byte{1}, strings.Repeat("e", 32), time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("authority change did not resume")
	}
}

func TestNativeKeyboardPromotionHonorsCancellationWhileWaitingForApp(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	expected, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	feed := &fakeNativeRecoveryFeed{}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: 1, Status: "reader-drained", AgentInstance: expected.InputAgentInstance, TargetID: expected.TargetID, BootID: expected.BootID}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- a.nativeKeyboardPromoteWithAuthority(ctx, feed, localfeed.V2Handle{}, expected, expected.InputAgentInstance, reply)
	}()
	cancel()
	a.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("promotion: %v", err)
	}
	if feed.promoted {
		t.Fatal("canceled promotion reached feed")
	}
	if _, permit := a.nativeKeyboardSnapshotPermit(); permit {
		t.Fatal("canceled promotion granted snapshot")
	}
}

func TestNativeKeyboardWorkerUsesOwnedLifetimeAndWaitsForWake(t *testing.T) {
	display, stopDisplay := context.WithCancel(context.Background())
	worker, stopWorker := context.WithCancel(context.WithoutCancel(display))
	defer stopWorker()
	wake := make(chan struct{}, 1)
	calls := make(chan struct{}, 3)
	done := make(chan error, 1)
	go func() {
		done <- runNativeKeyboardWorker(worker, wake, time.Hour, func(context.Context) (bool, error) { calls <- struct{}{}; return false, nil })
	}()
	<-calls
	stopDisplay()
	select {
	case err := <-done:
		t.Fatalf("display cancellation stopped worker: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case <-calls:
		t.Fatal("idle worker spun")
	default:
	}
	wake <- struct{}{}
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("worker missed wake")
	}
	stopWorker()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker shutdown: %v", err)
	}
}
