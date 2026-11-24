//go:build linux

package tenfoot

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

func TestNativeMappedPadServiceSnapshotPhysicalUnionAndDrain(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	nativeSetDescribedPadOnly(a)
	a.mu.Unlock()
	frames := make(chan protocol.InputFrame, 8)
	var attempts atomic.Int32
	feed := localfeed.New("memory", func(string, string, time.Duration) (net.Conn, error) {
		n := attempts.Add(1)
		if n > 2 {
			return nil, errors.New("unexpected recovery")
		}
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			r := bufio.NewReader(peer)
			req, err := localhid.ReadRequest(r)
			if err != nil {
				return
			}
			reply := localhid.Reply{Version: 2, Op: "recover", Request: req.Request, Status: "reader-drained", AgentInstance: req.AgentInstance, TargetID: req.TargetID, BootID: req.BootID}
			wire, err := localhid.EncodeReply(reply)
			if err != nil {
				return
			}
			if _, err = peer.Write(wire); err != nil {
				return
			}
			for {
				frame, err := protocol.DecodeInputFrame(r, 48)
				if err != nil {
					return
				}
				frames <- frame
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
	nativeWaitFor(t, "mapped pad ready", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.nativeMappedPad.active })
	inputs.localKeyboardGate.snapshotUI(func(int) ([]byte, error) { return gateCaps(30), nil }, func(int) ([]byte, error) { return make([]byte, nativeKeyBitmapBytes), nil }, func(in *nativeInput, held []uint16) {
		reconcileNativeKeyboardUI(in, inputs.devices, held, true, func(Command) {})
	}, func(*nativeInput) {})
	event := func(value int32) {
		t.Helper()
		handled, _, err := inputs.gatedKeyboardRecord(in, 1, 30, value, time.Now())
		if !handled || err != nil {
			t.Fatalf("physical edge: %v %v", handled, err)
		}
	}
	event(1)
	var pressed protocol.InputFrame
	select {
	case pressed = <-frames:
	case <-time.After(2 * time.Second):
		t.Fatal("mapped press missing")
	}
	if pressed.Device != uint8(remoteinput.DeviceGamepad) || pressed.Action != uint8(remoteinput.ActionPress) {
		t.Fatalf("wrong mapped press: %+v", pressed)
	}
	physical := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.Code(pressed.Code)}
	if err := feed.Send(physical, time.Now()); err != nil {
		t.Fatal(err)
	}
	event(0)
	nativeWaitFor(t, "mapped release processed", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.nativeMappedPad.keys) == 0 && len(a.nativeMappedPad.queue) == 0 && a.nativeMappedPad.pending == nil
	})
	select {
	case f := <-frames:
		t.Fatalf("mapped release cleared physical hold: %+v", f)
	default:
	}
	physical.Action = remoteinput.ActionRelease
	if err := feed.Send(physical, time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case released := <-frames:
		if released.Action != uint8(remoteinput.ActionRelease) {
			t.Fatal("physical release missing")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("physical release timed out")
	}
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
}

func TestNativeManagedSmokeOrNoInputNeverDials(t *testing.T) {
	for _, opts := range []Options{{KitHost: true, Smoke: true, Input: "auto"}, {KitHost: true, Input: "none"}} {
		opts = managedNativeKeyboardOptions(opts, "1")
		a := readyNativeBindingApp(t)
		var dials atomic.Int32
		feed := localfeed.New("memory", func(string, string, time.Duration) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("unexpected dial")
		})
		a.mu.Lock()
		a.localFeed = feed
		a.delegatedV2Active = false
		a.mu.Unlock()
		inputs := &nativeInputs{}
		service, err := configureNativeKeyboardService(a, inputs, nativeKeyboardInputEnabled(opts))
		if err != nil || service != nil || inputs.localKeyboardGate != nil || dials.Load() != 0 {
			t.Fatalf("service=%v gate=%v dials=%d err=%v", service, inputs.localKeyboardGate, dials.Load(), err)
		}
		a.Stop()
	}
}

func TestConfiguredSDLKitNoInputCannotFallbackToHost(t *testing.T) {
	for _, smoke := range []bool{false, true} {
		opts := managedNativeKeyboardOptions(Options{GFX: "sdl", KitHost: true, Input: "none", Smoke: smoke, APIBase: "test-invalid://unused"}, "1")
		a, err := configuredApp(opts)
		if err != nil {
			t.Fatal(err)
		}
		a.mu.Lock()
		a.session = kitDisplaySession()
		a.hostAwaitStatus = false
		a.mu.Unlock()
		pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
		if a.SendPlayHID(pad) {
			t.Fatal("disabled input fell back to host")
		}
		a.mu.Lock()
		if a.playHIDTail != nil || a.localFeed != nil {
			a.mu.Unlock()
			t.Fatal("disabled input scheduled guest send")
		}
		a.mu.Unlock()
		a.Stop()
	}
}

func TestConfiguredKitHostWindowUsesFeedWithoutMenuPlane(t *testing.T) {
	for _, backend := range []string{"sdl", "linuxfb"} {
		t.Run(backend, func(t *testing.T) {
			a, err := configuredApp(Options{GFX: backend, KitHost: true, NativeKeyboardV2: true, APIBase: "test-invalid://unused", Width: 1280, Height: 720})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Stop()
			a.mu.Lock()
			feed := a.localFeed
			cores := a.localCores
			plane := a.needsSessionDisplayLocked()
			a.mu.Unlock()
			if feed == nil || cores != nil || plane {
				t.Fatal("kit window did not install feed-only route")
			}
			if backend == "sdl" {
				service, err := configureSDLPadRecovery(a, true)
				if err != nil || service == nil {
					t.Fatalf("SDL recovery setup: %v", err)
				}
				a.mu.Lock()
				a.session.State = "active"
				a.session.CoreKeyboardHID = true
				blocked := a.kitSDLKeyboardGuestBlockedLocked()
				a.mu.Unlock()
				if !blocked {
					t.Fatal("SDL keyboard guest path available")
				}
				if !strings.Contains(a.Snapshot().Status, kitLocalKeyboardUnavailableStatus) {
					t.Fatal("SDL unavailable status missing")
				}
				fake := &fakePadFeed{}
				a.SetKitLocal(nil, fake)
				a.mu.Lock()
				a.hostAwaitStatus = false
				a.mu.Unlock()
				pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
				if !a.HandleLocalPad(pad, time.Now()) || len(fake.snapshot()) != 1 {
					t.Fatal("SDL kit pad did not reach local feed")
				}
			}
		})
		t.Run(backend+"-unmanaged", func(t *testing.T) {
			a, err := configuredApp(Options{GFX: backend, KitHost: true, APIBase: "test-invalid://unused", Width: 1280, Height: 720})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Stop()
			if a.localFeed != nil || a.localCores != nil {
				t.Fatal("unmanaged default gained local input route")
			}
		})
	}
	a, err := configuredApp(Options{GFX: "sdl", APIBase: "test-invalid://unused", Width: 1280, Height: 720})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	if a.localFeed != nil || a.localCores != nil || a.kitLocalKeyboardUnavailable {
		t.Fatal("paired SDL route changed")
	}
}
