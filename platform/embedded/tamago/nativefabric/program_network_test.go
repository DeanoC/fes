// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	gnet "github.com/usbarmory/go-net"
)

// Models platform ownership only; these callbacks are not physical DMA proof.
type programNetwork struct {
	paused            atomic.Bool
	stops, starts     int
	stopErr, startErr error
	stop, start       func()
}

func (*programNetwork) Receive([]byte) (int, error) { return 0, nil }
func (*programNetwork) Transmit([]byte) error       { return nil }
func newProgramNetwork(t *testing.T) (*programNetwork, *network.Controller) {
	t.Helper()
	n := &programNetwork{}
	l, err := network.NewLink(gnet.NewLnetoStack(gnet.DefaultLnetoStackConfig()), n, "192.0.2.2/24", "02:00:00:00:00:02", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := network.NewController(l, network.DMAControl{
		Stop: func(ctx context.Context) error {
			n.stops++
			if n.stop != nil {
				n.stop()
			}
			if n.stopErr != nil {
				return n.stopErr
			}
			n.paused.Store(true)
			return ctx.Err()
		}, Start: func(ctx context.Context) error {
			n.starts++
			if n.start != nil {
				n.start()
			}
			if n.startErr != nil {
				return n.startErr
			}
			n.paused.Store(false)
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("network loop did not stop")
		}
	})
	return n, c
}

type programNetworkCPU struct {
	CPUTransport
	t      *testing.T
	n      *programNetwork
	strict bool
}

func (c *programNetworkCPU) Invoke(args [7]uint64) ([4]uint64, error) {
	if c.strict && !c.n.paused.Load() {
		c.t.Fatal("native firmware IO before network drain")
	}
	return c.CPUTransport.Invoke(args)
}

func (c *programNetworkCPU) ReadPhysical32(address uint64) (uint32, error) {
	if c.strict && !c.n.paused.Load() {
		c.t.Fatal("physical read before network drain")
	}
	return c.CPUTransport.ReadPhysical32(address)
}
func (c *programNetworkCPU) WritePhysical32(address uint64, value uint32) error {
	if c.strict && !c.n.paused.Load() {
		c.t.Fatal("physical write before network drain")
	}
	return c.CPUTransport.WritePhysical32(address, value)
}
func attachProgramNetwork(t *testing.T, s *ProgramService, loader *ProgramLoader) (*programNetwork, *programNetworkCPU) {
	t.Helper()
	n, c := newProgramNetwork(t)
	if err := s.AttachNetwork(c); err != nil {
		t.Fatal(err)
	}
	guard := &programNetworkCPU{CPUTransport: loader.boot.cpu.(CPUTransport), t: t, n: n, strict: true}
	loader.boot.cpu = guard
	return n, guard
}

func TestProgramNetworkLaunchReturnParkAndStaleRequests(t *testing.T) {
	var n *programNetwork
	f, b, client := backendFixture(t, func(f *serviceFixture) { n, _ = attachProgramNetwork(t, f.s, f.loader) })
	ctx := context.Background()
	if n.stops != 0 || n.starts != 0 {
		t.Fatal("attachment accessed hardware")
	}
	if _, err := f.s.Launch(ctx, 99, b.entries[0].Request); err == nil || n.stops != 0 {
		t.Fatal("stale launch touched network")
	}
	for i := 0; i < 2; i++ {
		play, err := client.Launch(ctx, "native-0")
		if err != nil {
			t.Fatal(err)
		}
		if n.paused.Load() || n.stops != 2*i+1 || n.starts != 2*i+1 {
			t.Fatal("launch did not restore network")
		}
		if _, err = client.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
		if n.paused.Load() || n.stops != 2*i+2 || n.starts != 2*i+2 {
			t.Fatal("return did not restore network")
		}
	}
	session, err := f.s.Launch(ctx, f.s.Status().Generation, b.entries[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Park(ctx, session); err != nil {
		t.Fatal(err)
	}
	if !n.paused.Load() || n.stops != 6 || n.starts != 5 || f.s.Status().State != ProgramParked {
		t.Fatal("terminal park restarted network")
	}
	calls := f.cpu.calls
	if _, err = f.s.Return(ctx, session); err == nil || calls != f.cpu.calls || n.stops != 6 {
		t.Fatal("terminal path replayed")
	}
}

func TestProgramNetworkInstallerMenuAndFinalShutdown(t *testing.T) {
	var n *programNetwork
	f, b, plan, _ := newProgramInstallFixture(t, func(f *programMenuFixture) { n, _ = attachProgramNetwork(t, f.s, f.r.f.loader) })
	if err := b.Install(context.Background(), 0, plan); err != nil {
		t.Fatal(err)
	}
	if n.stops != 1 || n.starts != 1 || n.paused.Load() {
		t.Fatal("install did not restore network")
	}
	if err := f.s.ParkMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n.stops != 2 || n.starts != 1 || !n.paused.Load() || f.s.Status().State != ProgramParked {
		t.Fatal("shutdown restarted network")
	}
}

func TestProgramNetworkFailureBeforeDrainDoesNoFirmwareIO(t *testing.T) {
	for _, when := range []string{"launch", "return", "retire", "shutdown"} {
		t.Run(when, func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			n, _ := attachProgramNetwork(t, f.s, f.r.f.loader)
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			var session ProgramSession
			if when == "return" {
				var err error
				session, err = f.s.Launch(context.Background(), 0, request)
				if err != nil {
					t.Fatal(err)
				}
			}
			n.stopErr = errors.New("DMA drain completion lost")
			calls, starts, identity, held := f.r.cpu.calls, n.starts, f.s.Status().Identity, f.s.Status().Held
			var err error
			switch when {
			case "launch":
				_, err = f.s.Launch(context.Background(), 0, request)
			case "return":
				_, err = f.s.Return(context.Background(), session)
			case "retire":
				err = f.s.Retire(errors.New("frontend failed"))
			case "shutdown":
				err = f.s.ParkMenu(context.Background())
			}
			if err == nil || f.s.Status().State != ProgramRebootRequired || calls != f.r.cpu.calls || starts != n.starts {
				t.Fatal("failed drain performed native cleanup", err)
			}
			if f.s.Status().Identity != identity || f.s.Status().Held != held {
				t.Fatal("software retirement forged hardware state")
			}
			stops := n.stops
			_ = f.s.Retire(errors.New("retry"))
			_, _ = f.s.Launch(context.Background(), f.s.Status().Generation, request)
			if calls != f.r.cpu.calls || stops != n.stops {
				t.Fatal("uncertain boundary replayed")
			}
		})
	}
}

func TestProgramNetworkRestartFailureIsReturnedWithoutCleanup(t *testing.T) {
	var n *programNetwork
	f, b, client := backendFixture(t, func(f *serviceFixture) { n, _ = attachProgramNetwork(t, f.s, f.loader) })
	n.startErr = errors.New("restart ownership lost")
	var callsAtRestart int
	n.start = func() { callsAtRestart = f.cpu.calls }
	if play, err := client.Launch(context.Background(), "native-0"); err == nil && play.ErrorCode == "" {
		t.Fatal("restart failure reported launch success")
	}
	if f.s.Status().State != ProgramRebootRequired || f.s.Status().Held || f.cpu.calls != callsAtRestart || !n.paused.Load() {
		t.Fatal("restart failure performed cleanup or forged Hold")
	}
	if b.session.Generation() != 0 {
		t.Fatal("failed launch published session")
	}
}

func TestProgramNetworkServiceReturnsRestartError(t *testing.T) {
	f := serviceTest(t)
	n, _ := attachProgramNetwork(t, f.s, f.loader)
	request, _, _ := addProgram(t, f.loader, f.files, 0)
	n.startErr = errors.New("restart completion lost")
	if _, err := f.s.Launch(context.Background(), 0, request); !errors.Is(err, n.startErr) || !errors.Is(err, ErrProgramRetired) {
		t.Fatal("direct service omitted deferred restart error", err)
	}
	if f.s.Status().State != ProgramRebootRequired {
		t.Fatal("direct caller saw healthy state")
	}
}

func TestProgramNetworkReturnAndInstallRestartFailures(t *testing.T) {
	for _, when := range []string{"return", "install"} {
		t.Run(when, func(t *testing.T) {
			var n *programNetwork
			f, b, plan, _ := newProgramInstallFixture(t, func(f *programMenuFixture) { n, _ = attachProgramNetwork(t, f.s, f.r.f.loader) })
			var session ProgramSession
			if when == "return" {
				var err error
				session, err = f.s.Launch(context.Background(), 0, b.entries[0].Request)
				if err != nil {
					t.Fatal(err)
				}
			}
			n.startErr = errors.New("restart completion unknown")
			calls := 0
			n.start = func() { calls = f.r.cpu.calls }
			var err error
			if when == "return" {
				_, err = f.s.Return(context.Background(), session)
			} else {
				err = b.Install(context.Background(), 0, plan)
			}
			if !errors.Is(err, n.startErr) || f.s.Status().State != ProgramRebootRequired || calls != f.r.cpu.calls || !n.paused.Load() {
				t.Fatal("restart error or unknown cleanup", err)
			}
			if when == "install" && len(b.entries) != 1 {
				t.Fatal("failed install advertised entry")
			}
		})
	}
}

func TestProgramNetworkPresentationDrainFailureDoesNotCloseFabric(t *testing.T) {
	f := newProgramMenuFixture(t, "")
	n, guard := attachProgramNetwork(t, f.s, f.r.f.loader)
	guard.strict = false
	f.fault = "observe-frame"
	n.stopErr = errors.New("network drain lost")
	calls := 0
	n.stop = func() { calls = f.r.cpu.calls }
	err := f.s.PresentMenu(context.Background(), menuFrame(1))
	if !errors.Is(err, n.stopErr) || f.s.Status().State != ProgramRebootRequired || f.r.cpu.calls != calls || n.starts != 0 {
		t.Fatal("presentation failure performed unknown cleanup", err)
	}
	if f.s.state.menu.closed || f.s.state.menu.session == nil {
		t.Fatal("lost retained menu session")
	}
}

func TestProgramNetworkNativeFailureAndFrontendRetireKeepNetworkStopped(t *testing.T) {
	for _, when := range []string{"configuration", "return", "frontend", "frame"} {
		t.Run(when, func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			n, guard := attachProgramNetwork(t, f.s, f.r.f.loader)
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			var err error
			if when == "frame" {
				guard.strict = false // Healthy presentation may coexist under a qualified policy.
				f.fault = "observe-frame"
				n.stop = func() { guard.strict = true }
				err = f.s.PresentMenu(context.Background(), menuFrame(1))
			} else if when == "frontend" {
				err = f.s.Retire(errors.New("frontend failed"))
			} else {
				if when == "configuration" {
					f.fault = "computer-identity"
				}
				var session ProgramSession
				session, err = f.s.Launch(context.Background(), 0, request)
				if when == "return" {
					if err != nil {
						t.Fatal(err)
					}
					f.r.f.cpu.loseClose = true
					_, err = f.s.Return(context.Background(), session)
				}
			}
			if err == nil || f.s.Status().State != ProgramRebootRequired || !n.paused.Load() {
				t.Fatal("failure restarted network", err, f.s.Status())
			}
			if when != "return" && n.starts != 0 || when == "return" && n.starts != 1 {
				t.Fatal("unexpected restart", n.starts)
			}
		})
	}
}

func TestProgramNetworkAttachmentIsExclusiveAndBeforeFrontend(t *testing.T) {
	f := serviceTest(t)
	n, c := newProgramNetwork(t)
	if f.s.AttachNetwork(nil) == nil || f.s.AttachNetwork(&network.Controller{}) == nil {
		t.Fatal("fabricated control accepted")
	}
	if err := f.s.AttachNetwork(c); err != nil {
		t.Fatal(err)
	}
	if f.s.AttachNetwork(c) == nil {
		t.Fatal("duplicate attachment")
	}
	other := serviceTest(t)
	if other.s.AttachNetwork(c) == nil || other.s.Status().State != ProgramReady {
		t.Fatal("foreign shared controller accepted")
	}
	if n.stops != 0 || n.starts != 0 {
		t.Fatal("binding accessed platform")
	}
	f2, _, _ := backendFixture(t)
	_, c2 := newProgramNetwork(t)
	if f2.s.AttachNetwork(c2) == nil {
		t.Fatal("attached after frontend transfer")
	}
}
