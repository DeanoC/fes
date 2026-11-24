//go:build linux

package tenfoot

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

func nativeWaitFor(t *testing.T, what string, pred func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !pred() {
		select {
		case <-deadline.C:
			t.Fatalf("timeout waiting for %s", what)
		case <-tick.C:
		}
	}
}

func TestNativeKeyboardServiceRecoverySnapshotAndShutdown(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	token := strings.Repeat("c", 64)
	received := make(chan string, 8)
	serverErr := make(chan error, 1)
	feed := localfeed.New("memory", func(string, string, time.Duration) (net.Conn, error) {
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			r := bufio.NewReader(peer)
			for {
				req, err := localhid.ReadRequest(r)
				if err != nil {
					return
				}
				reply := localhid.Reply{Version: 2, Op: req.Op, Request: req.Request}
				switch req.Op {
				case "recover":
					reply.Status = "reader-drained"
					reply.AgentInstance = req.AgentInstance
					reply.TargetID = req.TargetID
					reply.BootID = req.BootID
				case "begin":
					if len(req.Devices) != 1 || len(req.Devices[0].Held) != 1 || req.Devices[0].Held[0] != 0xe1 {
						serverErr <- errors.New("bad held snapshot")
						return
					}
					reply.Status = "ready"
					reply.Token = token
				case "edge":
					if req.EventSeq != 1 || req.Usage != 0xe1 || req.Down {
						serverErr <- errors.New("bad buffered release")
						return
					}
					reply.Status = "accepted"
					reply.Token = token
				case "reset":
					if req.Token != token || req.LastEventSeq != 1 {
						serverErr <- errors.New("bad token reset")
						return
					}
					reply.Status = "neutral-confirmed"
					reply.Token = token
				default:
					serverErr <- errors.New("unexpected operation")
					return
				}
				wire, err := localhid.EncodeReply(reply)
				if err != nil {
					serverErr <- err
					return
				}
				if _, err = peer.Write(wire); err != nil {
					return
				}
				received <- req.Op
			}
		}()
		return client, nil
	})
	a.mu.Lock()
	a.localFeed = feed
	a.delegatedV2Active = false
	a.nativeKeyboardGeneration = 0
	a.mu.Unlock()
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	service, err := configureNativeKeyboardService(a, inputs, true)
	if err != nil {
		t.Fatal(err)
	}
	displayCtx, cancelDisplay := context.WithCancel(context.Background())
	cancelDisplay()
	if displayCtx.Err() == nil {
		t.Fatal("display did not cancel")
	}
	service.start()
	nativeWaitFor(t, "snapshot permit", func() bool { _, ok := a.nativeKeyboardSnapshotPermit(); return ok })
	ok, err := inputs.localKeyboardGate.snapshot(func(int) ([]byte, error) { return gateCaps(42), nil }, func(int) ([]byte, error) { return gateCaps(42), nil }, func(in *nativeInput, held []uint16) {
		reconcileNativeKeyboardUI(in, inputs.devices, held, true, func(Command) {})
	}, func(*nativeInput) {})
	if err != nil || !ok {
		t.Fatalf("snapshot %v %v", ok, err)
	}
	nativeWaitFor(t, "BEGIN ACK", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.nativeGuest.phase == nativeGuestReady })
	if handled, _, err := inputs.gatedKeyboardRecord(in, 1, 42, 0, time.Now()); !handled || err != nil {
		t.Fatalf("release %v %v", handled, err)
	}
	nativeWaitFor(t, "release ACK", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.nativeGuest.wireSeq == 1 && a.nativeGuest.pending == nil
	})
	a.nativeKeyboardBeginShutdown()
	if err := inputs.retireNativeKeyboards(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.nativeKeyboardAwaitRetirement(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serverErr:
		t.Fatal(err)
	default:
	}
	want := []string{"recover", "begin", "edge", "reset"}
	for _, op := range want {
		select {
		case got := <-received:
			if got != op {
				t.Fatalf("got %s want %s", got, op)
			}
		default:
			t.Fatalf("missing %s", op)
		}
	}
}

func TestNativeKeyboardServiceEOFQuarantinesWithoutRetirement(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	var attempts atomic.Int32
	closePeer := make(chan struct{})
	feed := localfeed.New("memory", func(string, string, time.Duration) (net.Conn, error) {
		if attempts.Add(1) != 1 {
			return nil, errors.New("no second connection")
		}
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			r := bufio.NewReader(peer)
			for {
				req, err := localhid.ReadRequest(r)
				if err != nil {
					return
				}
				reply := localhid.Reply{Version: 2, Op: req.Op, Request: req.Request}
				switch req.Op {
				case "recover":
					reply.Status = "reader-drained"
					reply.AgentInstance = req.AgentInstance
					reply.TargetID = req.TargetID
					reply.BootID = req.BootID
				case "begin":
					reply.Status = "ready"
					reply.Token = strings.Repeat("c", 64)
				default:
					return
				}
				wire, err := localhid.EncodeReply(reply)
				if err != nil {
					return
				}
				if _, err = peer.Write(wire); err != nil {
					return
				}
				if req.Op == "begin" {
					<-closePeer
					return
				}
			}
		}()
		return client, nil
	})
	a.mu.Lock()
	a.localFeed = feed
	a.delegatedV2Active = false
	a.nativeKeyboardGeneration = 0
	a.mu.Unlock()
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	service, err := configureNativeKeyboardService(a, inputs, true)
	if err != nil {
		t.Fatal(err)
	}
	service.start()
	nativeWaitFor(t, "snapshot permit", func() bool { _, ok := a.nativeKeyboardSnapshotPermit(); return ok })
	ok, err := inputs.localKeyboardGate.snapshot(func(int) ([]byte, error) { return gateCaps(42), nil }, func(int) ([]byte, error) { return make([]byte, nativeKeyBitmapBytes), nil }, func(in *nativeInput, held []uint16) {
		reconcileNativeKeyboardUI(in, inputs.devices, held, true, func(Command) {})
	}, func(*nativeInput) {})
	if err != nil || !ok {
		t.Fatalf("snapshot %v %v", ok, err)
	}
	nativeWaitFor(t, "ready source", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.nativeGuest.phase == nativeGuestReady && !a.nativeKeyboardQuarantined
	})
	close(closePeer)
	nativeWaitFor(t, "uncertain source", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.nativeGuest.phase == nativeGuestUncertain && a.nativeKeyboardQuarantined
	})
	a.nativeKeyboardBeginShutdown()
	_ = inputs.retireNativeKeyboards()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.nativeKeyboardAwaitRetirement(ctx); err == nil {
		t.Fatal("EOF was treated as retirement")
	}
	if err := service.stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNativeKeyboardServicePadOnlyShutdownWaitsForExactDrain(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	nativeSetDescribedPadOnly(a)
	a.mu.Unlock()
	var attempts atomic.Int32
	drainStarted, releaseDrain := make(chan struct{}), make(chan struct{})
	feed := localfeed.New("memory", func(string, string, time.Duration) (net.Conn, error) {
		n := attempts.Add(1)
		if n > 2 {
			return nil, errors.New("unexpected recovery retry")
		}
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			req, err := localhid.ReadRequest(bufio.NewReader(peer))
			if err != nil {
				return
			}
			if n == 2 {
				close(drainStarted)
				<-releaseDrain
			}
			reply := localhid.Reply{Version: 2, Op: "recover", Request: req.Request, Status: "reader-drained", AgentInstance: req.AgentInstance, TargetID: req.TargetID, BootID: req.BootID}
			wire, err := localhid.EncodeReply(reply)
			if err != nil {
				return
			}
			if _, err = peer.Write(wire); err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, peer)
		}()
		return client, nil
	})
	a.mu.Lock()
	a.localFeed = feed
	a.delegatedV2Active = false
	a.nativeKeyboardGeneration = 0
	a.mu.Unlock()
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	service, err := configureNativeKeyboardService(a, inputs, true)
	if err != nil {
		t.Fatal(err)
	}
	service.start()
	nativeWaitFor(t, "pad promotion", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.nativePadRetiring })
	if err := feed.Send(remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}, time.Now()); err != nil {
		t.Fatal(err)
	}
	a.nativeKeyboardBeginShutdown()
	if err := inputs.retireNativeKeyboards(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- a.nativeKeyboardAwaitRetirement(ctx) }()
	select {
	case <-drainStarted:
	case <-ctx.Done():
		t.Fatal("drain recovery did not start")
	}
	select {
	case <-result:
		t.Fatal("shutdown completed before drain proof")
	default:
	}
	close(releaseDrain)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("drain not acknowledged")
	}
	if err := service.stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNativeKeyboardOptInInstallsBeforeInputAndReservesFeed(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	feed := localfeed.New("memory", nil)
	a.mu.Lock()
	a.localFeed = feed
	a.delegatedV2Active = false
	a.nativeKeyboardGeneration = 0
	a.mu.Unlock()
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	service, err := configureNativeKeyboardService(a, inputs, false)
	if err != nil || service != nil || inputs.localKeyboardGate != nil {
		t.Fatal("default-off startup changed input")
	}
	service, err = configureNativeKeyboardService(a, inputs, true)
	if err != nil || service == nil || inputs.localKeyboardGate == nil || in.localKeyboardID == ([16]byte{}) {
		t.Fatalf("opt-in startup %v", err)
	}
	a.mu.Lock()
	active, quarantined := a.delegatedV2Active, a.nativeKeyboardQuarantined
	a.mu.Unlock()
	if !active || !quarantined {
		t.Fatal("guest input not fenced at startup")
	}
	if err := feed.Send(remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}, time.Now()); err == nil {
		t.Fatal("pad escaped reservation before recovery")
	}
	if handled, _, err := inputs.gatedKeyboardRecord(in, 1, 30, 1, time.Time{}); !handled || err != nil {
		t.Fatalf("native keyboard missed gate: %v %v", handled, err)
	}
}

func TestNativeKeyboardOptInRejectsNonKitHostAndClosing(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	a.kitHost = false
	a.localFeed = localfeed.New("memory", nil)
	a.mu.Unlock()
	if _, err := configureNativeKeyboardService(a, &nativeInputs{}, true); err == nil {
		t.Fatal("non-kit opt-in accepted")
	}
	b := readyNativeBindingApp(t)
	defer b.Stop()
	b.nativeKeyboardBeginShutdown()
	if _, err := b.nativeKeyboardPrepareBinding([16]byte{1}); err == nil {
		t.Fatal("shutdown admitted a new recovery")
	}
}
