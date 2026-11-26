// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/usbhub"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// fakeMultiSource models usbhub.Hub: reports a test supplies reach the source
// during the next Service, which (like the hub) queues only merged-state
// changes; PausePolling drops queued changes but keeps the live merged state.
type fakeMultiSource struct {
	kbArrivals       [][8]byte
	padArrivals      []usbhub.GamepadUpdate
	mouseArrivals    []usbhub.MouseUpdate
	kbQueue          [][8]byte
	padQueue         []usbhub.GamepadUpdate
	mouseQueue       []usbhub.MouseUpdate
	kbLive           [8]byte
	padLive          usbhub.GamepadUpdate
	mouseButtonsLive uint8
	enumerated       bool
	kbUnreported     bool
	serviceErr       error
	gameplayCalls    []bool
	pairingActive    bool
	pairingRem       time.Duration
	dongleConn       bool
	dongleName       string
	btDevices        []usbhub.BluetoothDevice
	btGen            uint64
	padDevices       []usbhub.GamepadDevice
	padGen           uint64
	startPairErr     error
	forgetErr        error
	forgotten        []string
	saveCalls        int
	bondsDirty       bool
	bondsFailed      bool
	saveErr          error
	pauseCalls       int
	resumeCalls      int
	ready            chan struct{}
}

func newFakeMultiSource() *fakeMultiSource {
	return &fakeMultiSource{
		padLive: usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
		btGen:   1,
		padGen:  1,
		ready:   make(chan struct{}, 4),
	}
}

func (s *fakeMultiSource) keys(reports ...[8]byte) { s.kbArrivals = append(s.kbArrivals, reports...) }
func (s *fakeMultiSource) pads(updates ...usbhub.GamepadUpdate) {
	s.padArrivals = append(s.padArrivals, updates...)
}
func (s *fakeMultiSource) mice(updates ...usbhub.MouseUpdate) {
	s.mouseArrivals = append(s.mouseArrivals, updates...)
}

func (s *fakeMultiSource) Service() error {
	if s.serviceErr != nil {
		return s.serviceErr
	}
	s.enumerated = true
	if len(s.kbArrivals) != 0 {
		s.kbUnreported = false
	}
	for _, rep := range s.kbArrivals {
		if rep != s.kbLive {
			s.kbLive = rep
			s.kbQueue = append(s.kbQueue, rep)
		}
	}
	for _, upd := range s.padArrivals {
		if upd != s.padLive {
			s.padLive = upd
			s.padQueue = append(s.padQueue, upd)
		}
	}
	for _, upd := range s.mouseArrivals {
		buttons := upd.Buttons & 0x03
		if upd.DX != 0 || upd.DY != 0 || buttons != s.mouseButtonsLive {
			s.mouseButtonsLive = buttons
			s.mouseQueue = append(s.mouseQueue, usbhub.MouseUpdate{DX: upd.DX, DY: upd.DY, Buttons: buttons})
		}
	}
	s.kbArrivals, s.padArrivals, s.mouseArrivals = nil, nil, nil
	return nil
}
func (s *fakeMultiSource) NextKeyboardReport() ([8]byte, bool) {
	if len(s.kbQueue) == 0 {
		return [8]byte{}, false
	}
	rep := s.kbQueue[0]
	s.kbQueue = s.kbQueue[1:]
	return rep, true
}
func (s *fakeMultiSource) NextGamepadUpdate() (usbhub.GamepadUpdate, bool) {
	if len(s.padQueue) == 0 {
		return usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}}, false
	}
	upd := s.padQueue[0]
	s.padQueue = s.padQueue[1:]
	return upd, true
}
func (s *fakeMultiSource) NextMouseUpdate() (usbhub.MouseUpdate, bool) {
	if len(s.mouseQueue) == 0 {
		return usbhub.MouseUpdate{}, false
	}
	upd := s.mouseQueue[0]
	s.mouseQueue = s.mouseQueue[1:]
	return upd, true
}
func (s *fakeMultiSource) setPadDevices(devs ...usbhub.GamepadDevice) {
	s.padDevices = append([]usbhub.GamepadDevice(nil), devs...)
	s.padGen++
}
func (s *fakeMultiSource) GamepadGeneration() uint64 { return s.padGen }
func (s *fakeMultiSource) GamepadDevicesInto(dst []usbhub.GamepadDevice) int {
	return copy(dst, s.padDevices)
}
func (s *fakeMultiSource) KeyboardNeutral() bool {
	return s.enumerated && !s.kbUnreported && s.kbLive == [8]byte{}
}
func (s *fakeMultiSource) GamepadNeutral() bool {
	return s.enumerated && input.GamepadNeutral(s.padLive.State) && !s.padLive.HomePressed && s.padLive.Ports == ([usbhub.MaxPlayerPorts]uint8{})
}
func (s *fakeMultiSource) MouseNeutral() bool {
	return s.enumerated && s.mouseButtonsLive == 0
}
func (s *fakeMultiSource) SetGameplayActive(active bool) error {
	s.gameplayCalls = append(s.gameplayCalls, active)
	if active {
		s.pairingActive = false
		s.pairingRem = 0
		s.btGen++
	}
	return nil
}
func (s *fakeMultiSource) StartPairing(d time.Duration) error {
	if s.startPairErr != nil {
		return s.startPairErr
	}
	s.pairingActive = true
	s.pairingRem = d
	s.btGen++
	return nil
}
func (s *fakeMultiSource) StopPairing() error {
	s.pairingActive = false
	s.pairingRem = 0
	s.btGen++
	return nil
}
func (s *fakeMultiSource) PairingActive() bool             { return s.pairingActive }
func (s *fakeMultiSource) PairingRemaining() time.Duration { return s.pairingRem }
func (s *fakeMultiSource) BluetoothGeneration() uint64     { return s.btGen }
func (s *fakeMultiSource) BluetoothSnapshot() usbhub.BluetoothSnapshot {
	connected := 0
	for _, dev := range s.btDevices {
		if dev.Connected {
			connected++
		}
	}
	return usbhub.BluetoothSnapshot{
		DongleConnected:  s.dongleConn,
		DongleName:       s.dongleName,
		PairingActive:    s.pairingActive,
		PairingRemaining: s.pairingRem,
		ConnectedCount:   connected,
		Devices:          append([]usbhub.BluetoothDevice(nil), s.btDevices...),
		BondsPending:     s.bondsDirty,
		BondsSaveFailed:  s.bondsFailed,
	}
}
func (s *fakeMultiSource) ForgetBluetoothDevice(address string) error {
	s.forgotten = append(s.forgotten, address)
	if s.forgetErr != nil {
		return s.forgetErr
	}
	kept := s.btDevices[:0]
	for _, dev := range s.btDevices {
		if !strings.EqualFold(dev.Address, address) {
			kept = append(kept, dev)
		}
	}
	s.btDevices = kept
	s.bondsDirty = true
	s.btGen++
	return nil
}
func (s *fakeMultiSource) SaveBondsIfDirty(context.Context) (bool, error) {
	s.saveCalls++
	if !s.bondsDirty {
		return false, nil
	}
	if s.saveErr != nil {
		s.bondsFailed = true
		s.btGen++
		return false, s.saveErr
	}
	s.bondsDirty = false
	s.bondsFailed = false
	s.btGen++
	return true, nil
}
func (s *fakeMultiSource) PausePolling(context.Context) error {
	s.pauseCalls++
	s.kbQueue, s.padQueue, s.mouseQueue = nil, nil, nil
	return nil
}
func (s *fakeMultiSource) ResumePolling(context.Context) error { s.resumeCalls++; return nil }
func (s *fakeMultiSource) Ready() <-chan struct{}              { return s.ready }

func multiTestKeys() ProgramGamepadKeys {
	keys := ProgramGamepadKeys{Up: 0x52, Down: 0x51, Left: 0x50, Right: 0x4f}
	keys.Buttons[0], keys.Buttons[1] = 0x16, 0x2c
	return keys
}

func multiTestConfig() ProgramMultiConfig {
	return ProgramMultiConfig{MenuButtons: input.GamepadButtons{Confirm: 1, Back: 2}, CoreKeys: multiTestKeys()}
}

// multiNext advances p once and requires exactly the wanted Tenfoot commands.
func multiNext(t *testing.T, p *ProgramMultiInputPlatform, step string, want ...tenfoot.Command) tenfoot.PlatformInput {
	t.Helper()
	frame, err := p.Next(context.Background())
	if err != nil || !slices.Equal(frame.Commands, want) {
		t.Fatalf("%s: commands=%v err=%v, want %v", step, frame.Commands, err, want)
	}
	return frame
}

func TestProgramMultiInputPlatformMenuCoreMergeReturnAndPairing(t *testing.T) {
	f, b, c := backendFixture(t)
	src := newFakeMultiSource()
	base := &programInputBase{}

	var pairingEvents []bool
	var bondSaveErrs []error
	p, err := NewProgramMultiInputPlatform(base, b, src, ProgramMultiConfig{
		MenuButtons:     input.GamepadButtons{Confirm: 1, Back: 2},
		CoreKeys:        multiTestKeys(),
		PairingDuration: 30 * time.Second,
		OnPairingEvent: func(active bool, err error) {
			if err == nil {
				pairingEvents = append(pairingEvents, active)
			}
		},
		OnBondsSaved: func(err error) {
			bondSaveErrs = append(bondSaveErrs, err)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Duplicate attachment is rejected cleanly.
	if _, err := NewProgramMultiInputPlatform(base, b, src, multiTestConfig()); !errors.Is(err, ErrProgramBusy) {
		t.Fatalf("expected ErrProgramBusy for duplicate attachment, got %v", err)
	}

	// 1. In ProgramReady: inputs whose first report after enumeration is held cannot
	// fire until an all-up snapshot arms the release fence; then Ctrl+Alt+P toggles BT
	// pairing without emitting text 'p', while normal keyboard + gamepad events navigate
	// Tenfoot simultaneously.
	src.bondsDirty = true
	src.keys(
		[8]byte{0x00, 0, 0x28, 0, 0, 0, 0, 0}, // held Enter at boot -> blocked
		[8]byte{},                             // all-up -> arms keyboard
		[8]byte{0x05, 0, 0x13, 0, 0, 0, 0, 0}, // Ctrl+Alt+P -> start pairing
		[8]byte{0x00, 0, 0x1d, 0, 0, 0, 0, 0}, // 'z' -> Tenfoot text (no nav command)
		[8]byte{},
	)
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: 0}}, // held Confirm at boot -> blocked
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},            // neutral -> arms gamepad
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: 0}}, // Up + Confirm
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
	)
	frame, err := p.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !src.PairingActive() || len(pairingEvents) != 1 || !pairingEvents[0] {
		t.Fatalf("expected pairing active after Ctrl+Alt+P, events=%v", pairingEvents)
	}
	if frame.Text != "z" {
		t.Fatalf("expected 'z' text only (pairing 'p' suppressed), got %q", frame.Text)
	}
	if !slices.Equal(frame.Commands, []tenfoot.Command{tenfoot.CmdUp, tenfoot.CmdSelect}) {
		t.Fatalf("expected gamepad Up+Select commands only, got %v", frame.Commands)
	}
	if len(bondSaveErrs) != 1 || bondSaveErrs[0] != nil || src.bondsDirty {
		t.Fatalf("expected bond save in ProgramReady, errs=%v dirty=%t", bondSaveErrs, src.bondsDirty)
	}
	// Gameplay mode should have synced false in Ready.
	if len(src.gameplayCalls) != 1 || src.gameplayCalls[0] {
		t.Fatalf("expected SetGameplayActive(false) in Ready, got %v", src.gameplayCalls)
	}

	// Present and serviceMenuInput work in Ready. Bonds dirtied meanwhile are written
	// by the next Next, never while the menu copies scanout rows.
	if err := p.Present(ctx, tenfoot.Snapshot{}); err != nil || base.presents != 1 {
		t.Fatalf("Present failed: err=%v presents=%d", err, base.presents)
	}
	src.bondsDirty = true
	saves := src.saveCalls
	src.keys([8]byte{0, 0, 0x1d, 0, 0, 0, 0, 0}, [8]byte{})
	if err := p.serviceMenuInput(ctx); err != nil {
		t.Fatalf("serviceMenuInput failed: %v", err)
	}
	if src.saveCalls != saves || !src.bondsDirty {
		t.Fatalf("bond write during menu scanout: calls=%d dirty=%t", src.saveCalls-saves, src.bondsDirty)
	}
	frame, err = p.Next(ctx)
	if err != nil || frame.Text != "z" {
		t.Fatalf("expected buffered menu text after serviceMenuInput, frame=%+v err=%v", frame, err)
	}
	if len(bondSaveErrs) != 2 || bondSaveErrs[1] != nil || src.bondsDirty {
		t.Fatalf("expected deferred bond save from Next, errs=%v dirty=%t", bondSaveErrs, src.bondsDirty)
	}
	if err := p.StopBluetoothPairing(); err != nil || src.PairingActive() {
		t.Fatalf("StopBluetoothPairing failed: err=%v active=%t", err, src.PairingActive())
	}

	// 2. 'b' and gamepad button 2 are still held when the core launches. Pause/Resume
	// wrap Launch, SetGameplayActive(true) disables BT scanning, and the held inputs
	// stay out of the core until everything is released; then keyboard and gamepad
	// rows merge cleanly without clobbering.
	src.keys([8]byte{0, 0, 0x05, 0, 0, 0, 0, 0})
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 2, Hat: -1}})
	if frame := multiNext(t, p, "hold before launch", tenfoot.CmdBack); frame.Text != "b" {
		t.Fatalf("expected menu text 'b', got %q", frame.Text)
	}
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	if src.pauseCalls == 0 || src.resumeCalls == 0 {
		t.Fatalf("expected PausePolling/ResumePolling around Launch, pause=%d resume=%d", src.pauseCalls, src.resumeCalls)
	}
	src.keys([8]byte{0, 0, 0x05, 0x04, 0, 0, 0, 0}) // 'a' pressed while 'b' is still held -> blocked
	if _, err := p.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatalf("inputs held across launch reached the core: %v", f.cpu.keyboardRows)
	}
	src.keys([8]byte{}, [8]byte{0, 0, 0x04, 0, 0, 0, 0, 0}) // all-up arms, then 'a' (row0 bit 4)
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},             // neutral -> arms
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}}, // S (0x16 -> row1 bit 6)
	)
	if _, err := p.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if len(src.gameplayCalls) != 2 || !src.gameplayCalls[1] {
		t.Fatalf("expected SetGameplayActive(true) in Running, got %v", src.gameplayCalls)
	}
	if err := p.StartBluetoothPairing(30 * time.Second); err == nil {
		t.Fatal("expected StartBluetoothPairing refused while ProgramRunning")
	}
	if f.cpu.keyboardRows[0] != 1<<4 || f.cpu.keyboardRows[1] != 1<<6 {
		t.Fatalf("expected merged kb+pad rows [0x%x, 0x%x], got %v", 1<<4, 1<<6, f.cpu.keyboardRows)
	}

	// Unplugging gamepad (neutral gamepad update) clears padRows while preserving kbRows!
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}})
	if _, err := p.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardRows[0] != 1<<4 || f.cpu.keyboardRows[1] != 0 {
		t.Fatalf("expected pad detach to preserve kbRows and clear padRows, got %v", f.cpu.keyboardRows)
	}

	// 3. BT Home button triggers return (neutralizes matrix and emits CmdStop).
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}, HomePressed: true})
	frame, err = p.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop || f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatalf("expected HomePressed to neutralize core and request Stop, frame=%+v rows=%v", frame, f.cpu.keyboardRows)
	}
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	// Back in Ready: SetGameplayActive(false) restored.
	if _, err := p.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if len(src.gameplayCalls) != 3 || src.gameplayCalls[2] {
		t.Fatalf("expected SetGameplayActive(false) on Return, got %v", src.gameplayCalls)
	}

	// 4. Launch again and verify Ctrl+Alt+Escape also returns cleanly, and fault parks core.
	if _, err := c.Launch(ctx, "native-1"); err != nil {
		t.Fatal(err)
	}
	src.keys(
		[8]byte{0x05, 0, 0x29, 0, 0, 0, 0, 0}, // pressed while 'a' is still held -> blocked
		[8]byte{},
		[8]byte{0x05, 0, 0x29, 0, 0, 0, 0, 0}, // fresh Ctrl+Alt+Escape -> returns
	)
	frame, err = p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop {
		t.Fatalf("expected Ctrl+Alt+Escape Stop, frame=%+v err=%v", frame, err)
	}
	src.serviceErr = errors.New("usb lease retired")
	if _, err := p.Next(ctx); err == nil || f.s.Status().State != ProgramParked {
		t.Fatalf("expected fault to park running core, err=%v state=%v", err, f.s.Status().State)
	}
}

// The hub queues only merged-state changes and PausePolling drops queued ones,
// so no all-up snapshot is queued while everything stays released. The first
// menu key and d-pad press after boot, and the first press after a boundary
// crossed while released, must still count; a keyboard that has not reported
// yet may be holding a key, so its first report cannot press.
func TestProgramMultiInputPlatformFirstPressAfterBootAndReleasedBoundaries(t *testing.T) {
	f, b, c := backendFixture(t)
	src := newFakeMultiSource()
	p, err := NewProgramMultiInputPlatform(&programInputBase{}, b, src, multiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	down := [8]byte{0, 0, 0x51, 0, 0, 0, 0, 0}
	padDown := usbhub.GamepadUpdate{State: usb.GamepadState{Hat: 4}}
	padIdle := usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}}

	multiNext(t, p, "boot enumerates released devices")
	src.keys(down)
	multiNext(t, p, "first menu key after boot", tenfoot.CmdDown)
	src.keys([8]byte{})
	src.pads(padDown)
	multiNext(t, p, "first d-pad press after boot", tenfoot.CmdDown)
	src.pads(padIdle)
	multiNext(t, p, "menu release")

	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	src.keys([8]byte{0, 0, 0x04, 0, 0, 0, 0, 0})
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}})
	multiNext(t, p, "first core presses after a released launch")
	if f.cpu.keyboardRows[0] != 1<<4 || f.cpu.keyboardRows[1] != 1<<6 {
		t.Fatalf("first core presses after a released launch were dropped: %v", f.cpu.keyboardRows)
	}
	src.keys([8]byte{})
	src.pads(padIdle)
	multiNext(t, p, "core release")
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	src.keys(down)
	src.pads(padDown)
	multiNext(t, p, "first presses after a released return", tenfoot.CmdDown, tenfoot.CmdDown)
	src.keys([8]byte{})
	src.pads(padIdle)
	multiNext(t, p, "menu release after return")

	src.kbUnreported = true
	if _, err := c.Launch(ctx, "native-1"); err != nil {
		t.Fatal(err)
	}
	src.keys([8]byte{0, 0, 0x04, 0, 0, 0, 0, 0})
	multiNext(t, p, "unreported keyboard's first report")
	if f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatalf("a keyboard's first report pressed before any all-up snapshot: %v", f.cpu.keyboardRows)
	}
	src.keys([8]byte{}, [8]byte{0, 0, 0x04, 0, 0, 0, 0, 0})
	multiNext(t, p, "press after all-up")
	if f.cpu.keyboardRows[0] != 1<<4 {
		t.Fatalf("press after the all-up snapshot was dropped: %v", f.cpu.keyboardRows)
	}
}

// digitalPadDescriptor is a hat + four-button HID gamepad. Its idle report
// (null hat, no buttons) decodes to the decoder's initial state, so the real
// hub never queues a snapshot for it: only the live state can arm its fence.
var digitalPadDescriptor = []byte{
	0x05, 0x01, 0x09, 0x05, 0xa1, 0x01,
	0x09, 0x39, 0x15, 0x00, 0x25, 0x07, 0x75, 0x04, 0x95, 0x01, 0x81, 0x42,
	0x05, 0x09, 0x19, 0x01, 0x29, 0x04, 0x15, 0x00, 0x25, 0x01, 0x75, 0x01, 0x95, 0x04, 0x81, 0x02,
	0xc0,
}

const (
	multiTestKeyboard = 2
	multiTestPad      = 3
	padIdleReport     = 0x08 // null hat, no buttons
	padDownReport     = 0x04
	padConfirmReport  = 0x18 // null hat, button 1
)

// multiTestBus is a minimal usb.BusHost for the real usbhub.Hub: a boot
// keyboard and the digital pad are present at enumeration, each with one
// interrupt IN pipe that returns the reports a test supplies.
type multiTestBus struct {
	devices []usb.BusDevice
	pipes   map[uint8]*multiTestPipe
}

type multiTestPipe struct {
	addr    uint8
	ep      usb.EndpointDescriptor
	reports [][]byte
}

func (p *multiTestPipe) DeviceAddress() uint8              { return p.addr }
func (p *multiTestPipe) Endpoint() usb.EndpointDescriptor  { return p.ep }
func (p *multiTestPipe) Write([]byte) error                { return nil }
func (p *multiTestPipe) WriteDone() (bool, error)          { return true, nil }
func (p *multiTestPipe) SetIntervalOverride(time.Duration) {}
func (p *multiTestPipe) Close() error                      { return nil }
func (p *multiTestPipe) PollInto(dst []byte) (int, error) {
	if len(p.reports) == 0 {
		return 0, nil
	}
	n := copy(dst, p.reports[0])
	p.reports = p.reports[1:]
	return n, nil
}

func newMultiTestBus() *multiTestBus {
	in := []usb.EndpointDescriptor{{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 8, Interval: 8}}
	return &multiTestBus{
		devices: []usb.BusDevice{
			{Address: multiTestKeyboard, Config: usb.FullConfiguration{Value: 1, Interfaces: []usb.InterfaceDescriptor{
				{Class: 3, SubClass: 1, Protocol: 1, Endpoints: in}}}},
			{Address: multiTestPad, Config: usb.FullConfiguration{Value: 1, Interfaces: []usb.InterfaceDescriptor{
				{Class: 3, HIDReportBytes: uint16(len(digitalPadDescriptor)), Endpoints: in}}}},
		},
		pipes: map[uint8]*multiTestPipe{},
	}
}

func (b *multiTestBus) Service() error                   { return nil }
func (b *multiTestBus) NextEvent() (usb.HostEvent, bool) { return usb.HostEvent{}, false }
func (b *multiTestBus) Devices() []usb.BusDevice         { return b.devices }
func (b *multiTestBus) Ready() <-chan struct{}           { return nil }
func (b *multiTestBus) ControlTransfer(addr uint8, reqType, req byte, value, _ uint16, data []byte) (int, error) {
	if addr == multiTestPad && reqType == 0x81 && req == 0x06 && value == 0x2200 {
		return copy(data, digitalPadDescriptor), nil
	}
	return len(data), nil
}
func (b *multiTestBus) OpenPipe(addr uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	p := &multiTestPipe{addr: addr, ep: ep}
	b.pipes[addr] = p
	return p, nil
}
func (b *multiTestBus) report(addr uint8, report ...byte) {
	b.pipes[addr].reports = append(b.pipes[addr].reports, report)
}
func (b *multiTestBus) keys(usages ...byte) {
	report := make([]byte, 8)
	copy(report[2:], usages)
	b.report(multiTestKeyboard, report...)
}

func multiHubFixture(t *testing.T) (*serviceFixture, *hostclient.Client, *multiTestBus, *ProgramMultiInputPlatform) {
	t.Helper()
	f, b, c := backendFixture(t)
	bus := newMultiTestBus()
	hub, err := usbhub.NewHub(bus, usbhub.Config{Now: func() time.Time { return time.Unix(300, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProgramMultiInputPlatform(&programInputBase{}, b, hub, multiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Boot enumerates both devices. Neither has reported, so neither fence arms.
	multiNext(t, p, "enumerate")
	if bus.pipes[multiTestKeyboard] == nil || bus.pipes[multiTestPad] == nil {
		t.Fatal("hub did not open both input pipes at enumeration")
	}
	return f, c, bus, p
}

// End to end with the real hub, whose de-duplicated queue never carries the
// idle all-up reports the devices send.
func TestProgramMultiInputPlatformRealHubFirstPressAfterBootAndBoundaries(t *testing.T) {
	f, c, bus, p := multiHubFixture(t)
	ctx := context.Background()

	bus.keys()                              // SET_IDLE snapshot: all-up, same as the slot's initial report
	bus.report(multiTestPad, padIdleReport) // decodes to the pad's initial state
	multiNext(t, p, "idle reports queue nothing")
	bus.keys(0x51)
	multiNext(t, p, "first menu key after boot", tenfoot.CmdDown)
	bus.keys()
	bus.report(multiTestPad, padDownReport)
	multiNext(t, p, "first d-pad press after boot", tenfoot.CmdDown)
	bus.report(multiTestPad, padIdleReport)
	multiNext(t, p, "pad release")

	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	bus.keys(0x04)
	bus.report(multiTestPad, padConfirmReport)
	multiNext(t, p, "first core presses after a released launch")
	if f.cpu.keyboardRows[0] != 1<<4 || f.cpu.keyboardRows[1] != 1<<6 {
		t.Fatalf("first core presses after a released launch were dropped: %v", f.cpu.keyboardRows)
	}
	bus.keys()
	bus.report(multiTestPad, padIdleReport)
	multiNext(t, p, "core release")
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	bus.keys(0x51)
	bus.report(multiTestPad, padDownReport)
	multiNext(t, p, "first presses after a released return", tenfoot.CmdDown, tenfoot.CmdDown)

	bus.keys(0x05) // Down released; 'b' stays held into the next launch
	bus.report(multiTestPad, padIdleReport)
	if frame := multiNext(t, p, "hold 'b'"); frame.Text != "b" {
		t.Fatalf("expected menu text 'b', got %q", frame.Text)
	}
	if _, err := c.Launch(ctx, "native-1"); err != nil {
		t.Fatal(err)
	}
	bus.keys(0x05, 0x04)
	multiNext(t, p, "press while a key is held across launch")
	if f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatalf("keys held across launch reached the core: %v", f.cpu.keyboardRows)
	}
	bus.keys()
	bus.keys(0x04)
	multiNext(t, p, "press after release")
	if f.cpu.keyboardRows != ([9]uint16{0: 1 << 4}) {
		t.Fatalf("expected only 'a' after release, got %v", f.cpu.keyboardRows)
	}
}

func TestProgramMultiInputPlatformRealHubHeldAtBootIsNotAPress(t *testing.T) {
	_, _, bus, p := multiHubFixture(t)

	bus.keys(0x28)                             // Return held before enumeration
	bus.report(multiTestPad, padConfirmReport) // Confirm held before enumeration
	multiNext(t, p, "held at boot")
	bus.keys()
	bus.report(multiTestPad, padIdleReport)
	multiNext(t, p, "release arms without pressing")
	bus.keys(0x51)
	bus.report(multiTestPad, padConfirmReport)
	multiNext(t, p, "fresh presses", tenfoot.CmdDown, tenfoot.CmdSelect)
}

func TestProgramMultiInputPlatformTenfootBluetoothSettings(t *testing.T) {
	_, b, c := backendFixture(t)
	src := newFakeMultiSource()
	src.dongleConn = true
	src.dongleName = "Realtek RTL8761BU"
	src.btDevices = []usbhub.BluetoothDevice{
		{Address: "AA:BB:CC:DD:EE:01", Name: "DualSense Wireless Controller", Kind: controllers.KindDualSense, Connected: true, Slot: 1},
		{Address: "AA:BB:CC:DD:EE:02", Name: "Pro Controller", Kind: controllers.KindSwitchPro},
	}

	p, err := NewProgramMultiInputPlatform(&programInputBase{}, b, src, multiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	app := tenfoot.NewApp(tenfoot.NewClient(srv.URL, srv.Client()), 1280, 720, 4)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	if err := app.SetBluetooth(p); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	step := func() {
		t.Helper()
		frame, err := p.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, cmd := range frame.Commands {
			app.HandleCommand(cmd, frame.At)
		}
	}

	// Arm gamepad on initial idle enumerate.
	step()

	// Press Home/Guide in ProgramReady -> opens Settings via CmdSettings with zero keyboard input.
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}, HomePressed: true},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
	)
	step()
	snap := app.Snapshot()
	if !snap.Settings.Open {
		t.Fatal("expected HomePressed in ProgramReady to open Settings")
	}

	// Find and open Bluetooth section using gamepad navigation.
	btIdx := -1
	for i, row := range snap.Settings.Rows {
		if row.ID == "section-bluetooth" {
			btIdx = i
			break
		}
	}
	if btIdx < 0 {
		t.Fatalf("missing section-bluetooth in %+v", snap.Settings.Rows)
	}
	for range btIdx {
		src.pads(
			usbhub.GamepadUpdate{State: usb.GamepadState{Hat: 4}},
			usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
		)
		step()
	}
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
	)
	step()
	snap = app.Snapshot()
	if snap.Settings.Title != "Bluetooth" || len(snap.Settings.Rows) != 4 {
		t.Fatalf("expected Bluetooth section with 4 rows, got %#v", snap.Settings)
	}

	// Move to bt-pair (index 1); confirm once to arm, then again to start pairing.
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: 4}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
	)
	step()
	if src.PairingActive() || p.BluetoothStatus().PairingActive {
		t.Fatal("first confirmation unexpectedly started pairing")
	}
	if got := app.Snapshot().Settings.Rows[1].Value; got != "A again to pair nearby controller" {
		t.Fatalf("expected pairing confirmation in row value, got %q", got)
	}
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
	)
	step()
	if !src.PairingActive() || !p.BluetoothStatus().PairingActive {
		t.Fatal("expected pairing active after confirming bt-pair")
	}
	if got := app.Snapshot().Settings.Rows[1].Value; !strings.Contains(got, "Pairing (30s)") {
		t.Fatalf("expected Pairing (30s) in row value, got %q", got)
	}

	// Stop pairing with another Confirm.
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
	)
	step()
	if src.PairingActive() || p.BluetoothStatus().PairingActive {
		t.Fatal("expected pairing stopped")
	}

	// Navigate to Pro Controller (index 3), double-Confirm to forget it, and confirm bonds save cleanly.
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: 4}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: 4}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
	)
	step()
	// Run one more Next so SaveBondsIfDirty persists the forgotten bond.
	step()
	if len(src.forgotten) != 1 || src.forgotten[0] != "AA:BB:CC:DD:EE:02" {
		t.Fatalf("expected Pro Controller forgotten, got %v", src.forgotten)
	}
	if src.bondsDirty {
		t.Fatal("expected forgotten bond persisted by next Next")
	}
	if len(app.Snapshot().Settings.Rows) != 3 {
		t.Fatalf("expected 3 rows after forget, got %+v", app.Snapshot().Settings.Rows)
	}

	// Close Settings so menu sees clean state, then launch core and ensure:
	// 1) ForgetBluetoothDevice is refused while ProgramRunning
	// 2) Returning via Home while still held past return does NOT trigger CmdSettings.
	app.HandleCommand(tenfoot.CmdSettings, time.Now())
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ForgetBluetoothDevice("AA:BB:CC:DD:EE:01"); err == nil {
		t.Fatal("expected ForgetBluetoothDevice refused while ProgramRunning")
	}
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}, HomePressed: true})
	frame, err := p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop {
		t.Fatalf("expected HomePressed in ProgramRunning to trigger CmdStop, frame=%+v err=%v", frame, err)
	}
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	// Hold Home across the transition into ProgramReady -> must stay blocked until released.
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}, HomePressed: true})
	frame, err = p.Next(ctx)
	if err != nil || len(frame.Commands) != 0 {
		t.Fatalf("expected held Home across return to emit no CmdSettings, frame=%+v err=%v", frame, err)
	}
	// Release then fresh press opens Settings.
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}, HomePressed: false},
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}, HomePressed: true},
	)
	frame, err = p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdSettings {
		t.Fatalf("expected fresh Home press after release to emit CmdSettings, frame=%+v err=%v", frame, err)
	}
}

func TestProgramMultiInputPlatformNativeControllerPorts(t *testing.T) {
	ctx := t.Context()
	var port *spectrumFirmwareCPU
	var entries []ProgramEntry
	f := newProgramMenuFixtureContext(t, ctx, "", func(f *programMenuFixture) {
		l := f.r.f.loader
		specReq, specFiles, compiled, policy := packageSpectrumFixture(t, l, false)
		var first uploadShellRecord
		if err := json.Unmarshal(compiled, &first); err != nil {
			t.Fatal(err)
		}
		for name, data := range specFiles {
			f.r.f.files.files[name] = &fstest.MapFile{Data: data}
		}
		paired, err := l.WithSpectrum48(compiled)
		if err != nil {
			t.Fatal(err)
		}
		f.r.f.loader = paired
		f.cpu.configuration.spectrumPolicy = filepath.Join(t.TempDir(), "policy.bin")
		if err := os.WriteFile(f.cpu.configuration.spectrumPolicy, policy, 0600); err != nil {
			t.Fatal(err)
		}
		expected := specFiles[specReq.Sources[0].Path]
		port = &spectrumFirmwareCPU{CPUTransport: f.cpu, base: f.r.f.cpu.computerCPU, firmwareBytes: len(expected)}
		port.media = tapeCPU{base: port.base, spectrum: true}
		l.boot.cpu = port
		activate := f.cpu.activate
		f.cpu.activate = func(role programRole) {
			activate(role)
			port.active = role == programRoleUploadShell
			port.ready, port.held, port.image, port.header = false, false, nil, [4]uint16{}
			port.media.ready, port.media.image = false, nil
			if port.active {
				f.r.cpu.expectedROM = nil
				raw, err := hex.DecodeString(first.BuildID)
				if err != nil {
					t.Fatal(err)
				}
				for i := range 8 {
					port.base.words[8+i] = binary.LittleEndian.Uint16(raw[2*i:])
				}
				port.base.words[7] = uint16(spectrum48Capabilities)
				port.base.gpo, port.base.gpi, port.base.fetches = 0, cg.FesComputerSignature, 0
			} else {
				port.base.words[7] = uint16(l.computer.Capabilities)
			}
		}
		entries = []ProgramEntry{
			{ID: "spectrum48", Title: "Spectrum 48K", Request: specReq},
		}
	})
	b, err := NewProgramBackend(f.s, entries)
	if err != nil {
		t.Fatal(err)
	}
	c := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	src := newFakeMultiSource()
	p, err := NewProgramMultiInputPlatform(&programInputBase{}, b, src, multiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	step := func() tenfoot.PlatformInput {
		t.Helper()
		frame, err := p.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return frame
	}

	// Hold P1 Up across launch so the native-port fence must wait for neutral.
	src.pads(usbhub.GamepadUpdate{
		State: usb.GamepadState{Buttons: 1, Hat: 0},
		Ports: [usbhub.MaxPlayerPorts]uint8{0x11, 0, 0, 0},
	})
	step()

	play, err := c.Launch(ctx, "spectrum48")
	if err != nil || play.ErrorCode != "" {
		t.Fatalf("spectrum48 Launch failed: play=%+v err=%v", play, err)
	}
	if f.s.Capabilities(b.session)&cg.FesComputerCapabilityGamepadPorts == 0 {
		t.Fatal("expected GamepadPorts capability exposed for active Spectrum48 session")
	}

	// First poll in ProgramRunning binds keyboard and neutralizes both controller ports.
	step()
	if port.base.controllerWrites != 2 || port.base.controllerPorts != ([2]uint16{}) {
		t.Fatalf("expected 2 initial port zero writes, got writes=%d ports=%#v", port.base.controllerWrites, port.base.controllerPorts)
	}

	// Still held -> fence stays closed.
	src.pads(usbhub.GamepadUpdate{
		State: usb.GamepadState{Buttons: 1, Hat: 0},
		Ports: [usbhub.MaxPlayerPorts]uint8{0x13, 0, 0, 0},
	})
	step()
	if port.base.controllerWrites != 2 {
		t.Fatalf("held port input bypassed launch fence: writes=%d", port.base.controllerWrites)
	}

	// Release arms pad fence, then drive independent P1 and P2 masks + P3/P4 ignored safely.
	src.pads(
		usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}},
		usbhub.GamepadUpdate{
			State: usb.GamepadState{Buttons: 1, Hat: 0},
			Ports: [usbhub.MaxPlayerPorts]uint8{0x11, 0x22, 0x04, 0x08},
		},
	)
	step()
	if port.base.controllerWrites != 4 || port.base.controllerPorts != ([2]uint16{0x11, 0x22}) {
		t.Fatalf("expected P1=0x11 P2=0x22 across 2 port writes, got writes=%d ports=%#v", port.base.controllerWrites, port.base.controllerPorts)
	}
	if port.base.keyboardRows != ([9]uint16{}) {
		t.Fatalf("expected no CoreKeys keyboard matrix fallback when GamepadPorts enabled, got %v", port.base.keyboardRows)
	}

	// Change only P2 -> writes only the changed port (1 write instead of 2).
	src.pads(usbhub.GamepadUpdate{
		State: usb.GamepadState{Buttons: 1, Hat: 0},
		Ports: [usbhub.MaxPlayerPorts]uint8{0x11, 0x00, 0, 0},
	})
	step()
	if port.base.controllerWrites != 5 || port.base.controllerPorts != ([2]uint16{0x11, 0x00}) {
		t.Fatalf("expected single-port update (writes=5), got writes=%d ports=%#v", port.base.controllerWrites, port.base.controllerPorts)
	}

	// Mid-session boundary bump preserves nativeControllers, re-neutralizes both ports, and arms once neutral.
	b.operation.Lock()
	b.inputBoundary++
	b.operation.Unlock()
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}})
	step()
	if !p.nativeControllers || port.base.controllerPorts != ([2]uint16{}) || port.base.controllerWrites != 7 {
		t.Fatalf("expected mid-session boundary to preserve nativeControllers and re-zero both ports, native=%t writes=%d ports=%#v", p.nativeControllers, port.base.controllerWrites, port.base.controllerPorts)
	}
	src.pads(usbhub.GamepadUpdate{
		State: usb.GamepadState{Buttons: 1, Hat: -1},
		Ports: [usbhub.MaxPlayerPorts]uint8{0x10, 0, 0, 0},
	})
	step()
	if port.base.controllerWrites != 8 || port.base.controllerPorts != ([2]uint16{0x10, 0x00}) || port.base.keyboardRows != ([9]uint16{}) {
		t.Fatalf("expected native port write (not CoreKeys fallback) after mid-session boundary, writes=%d ports=%#v kb=%v", port.base.controllerWrites, port.base.controllerPorts, port.base.keyboardRows)
	}

	// Pressing Home zeroes both hardware controller ports and requests CmdStop.
	src.pads(usbhub.GamepadUpdate{
		State:       usb.GamepadState{Hat: -1},
		HomePressed: true,
		Ports:       [usbhub.MaxPlayerPorts]uint8{0x10, 0x22, 0, 0},
	})
	frame := step()
	if len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop {
		t.Fatalf("expected CmdStop on HomePressed, got %+v", frame)
	}
	if port.base.controllerPorts != ([2]uint16{}) || port.base.controllerWrites != 10 {
		t.Fatalf("expected both controller ports zeroed on Home return, got writes=%d ports=%#v", port.base.controllerWrites, port.base.controllerPorts)
	}

	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	if f.s.Capabilities(b.session) != 0 {
		t.Fatal("expected Capabilities() == 0 after returning to idle")
	}

	// Core without GamepadPorts capability falls back to CoreKeys keyboard matrix and never writes ControllerButtons.
	zxFix, zxB, zxC := backendFixture(t)
	zxSrc := newFakeMultiSource()
	zxP, err := NewProgramMultiInputPlatform(&programInputBase{}, zxB, zxSrc, multiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	zxPlay, err := zxC.Launch(ctx, "native-0")
	if err != nil || zxPlay.ErrorCode != "" {
		t.Fatalf("ZX81 Launch failed: play=%+v err=%v", zxPlay, err)
	}
	if zxFix.s.Capabilities(zxB.session)&cg.FesComputerCapabilityGamepadPorts != 0 {
		t.Fatal("expected ZX81 session without GamepadPorts capability")
	}
	zxSrc.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}})
	if _, err := zxP.Next(ctx); err != nil {
		t.Fatal(err)
	}
	zxSrc.pads(usbhub.GamepadUpdate{
		State: usb.GamepadState{Buttons: 1, Hat: -1},
		Ports: [usbhub.MaxPlayerPorts]uint8{0x10, 0, 0, 0},
	})
	if _, err := zxP.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if zxFix.cpu.controllerWrites != 0 {
		t.Fatalf("expected no ControllerButtons writes on non-GamepadPorts core, got %d", zxFix.cpu.controllerWrites)
	}
	if zxFix.cpu.keyboardRows[1] != 1<<6 {
		t.Fatalf("expected CoreKeys keyboard matrix fallback, got %v", zxFix.cpu.keyboardRows)
	}
}

func TestProgramMultiInputPlatformNativeMouseRelative(t *testing.T) {
	f, b, c := backendFixture(t)
	src := newFakeMultiSource()
	p, err := NewProgramMultiInputPlatform(&programInputBase{}, b, src, multiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	step := func() tenfoot.PlatformInput {
		t.Helper()
		frame, err := p.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return frame
	}

	// Menu-phase mouse motion is discarded without touching hardware; held Left button stays across launch.
	src.mice(usbhub.MouseUpdate{DX: 10, DY: -4, Buttons: 0x01})
	step()
	if f.cpu.mouseWrites != 0 {
		t.Fatalf("menu mouse activity touched hardware: writes=%d", f.cpu.mouseWrites)
	}

	play, err := c.Launch(ctx, "native-0")
	if err != nil || play.ErrorCode != "" {
		t.Fatalf("Launch failed: play=%+v err=%v", play, err)
	}

	// Re-open the live Computer handle advertising MouseRelative so Computer.MouseRelative validation accepts it.
	wantCaps := cg.FesComputerCapabilityVideoFixed720p60 | cg.FesComputerCapabilityKeyboardHid | cg.FesComputerCapabilityMouseRelative
	f.cpu.words[7] = uint16(wantCaps)
	var build [16]byte
	for i := range 8 {
		binary.LittleEndian.PutUint16(build[2*i:], f.cpu.words[8+i])
	}
	reopened, err := computer.Open(ctx, f.s.state.computer.port, computerLimits(), computer.Identity{
		Minor:        0,
		Capabilities: wantCaps,
		Build:        build,
	})
	if err != nil {
		t.Fatalf("reopen Computer with MouseRelative: %v", err)
	}
	f.s.state.computer.control = reopened
	f.s.state.viewMu.Lock()
	f.s.state.capabilities = wantCaps
	f.s.state.viewMu.Unlock()

	// First poll in ProgramRunning binds keyboard and writes initial zero MouseRelative neutralization.
	step()
	if f.cpu.mouseWrites != 1 || len(f.cpu.mouseEvents) != 1 || f.cpu.mouseEvents[0] != (mouseRecord{dx: 0, dy: 0, buttons: 0}) {
		t.Fatalf("expected 1 initial MouseRelative(0,0,0), got writes=%d events=%+v", f.cpu.mouseWrites, f.cpu.mouseEvents)
	}

	// Held Left button across launch keeps mouseArmed closed until released.
	src.mice(usbhub.MouseUpdate{DX: 5, DY: 5, Buttons: 0x01})
	step()
	if f.cpu.mouseWrites != 1 {
		t.Fatalf("held mouse button bypassed launch fence: writes=%d", f.cpu.mouseWrites)
	}

	// Release arms mouseArmed, then forward signed deltas and buttons.
	src.mice(
		usbhub.MouseUpdate{DX: 0, DY: 0, Buttons: 0},
		usbhub.MouseUpdate{DX: -12, DY: 8, Buttons: 0x01},
		usbhub.MouseUpdate{DX: 3, DY: -1, Buttons: 0x03},
	)
	step()
	wantEvents := []mouseRecord{
		{0, 0, 0},
		{-12, 8, 0x01},
		{3, -1, 0x03},
	}
	if !slices.Equal(f.cpu.mouseEvents, wantEvents) {
		t.Fatalf("mouseEvents=%+v, want %+v", f.cpu.mouseEvents, wantEvents)
	}

	// Mid-session boundary bump preserves nativeMouse and re-neutralizes buttons to 0.
	b.operation.Lock()
	b.inputBoundary++
	b.operation.Unlock()
	src.mice(usbhub.MouseUpdate{DX: 0, DY: 0, Buttons: 0})
	step()
	if !p.nativeMouse || f.cpu.mouseWrites != 4 || f.cpu.mouseEvents[3] != (mouseRecord{0, 0, 0}) {
		t.Fatalf("expected mid-session boundary to preserve nativeMouse and neutralize buttons, native=%t writes=%d events=%+v", p.nativeMouse, f.cpu.mouseWrites, f.cpu.mouseEvents)
	}

	// Press Right button, then trigger HomePressed -> neutralizeCore clears mouse buttons to 0.
	src.mice(usbhub.MouseUpdate{DX: 4, DY: 2, Buttons: 0x02})
	step()
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}, HomePressed: true})
	frame := step()
	if len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop {
		t.Fatalf("expected CmdStop on HomePressed, got %+v", frame)
	}
	last := f.cpu.mouseEvents[len(f.cpu.mouseEvents)-1]
	if last != (mouseRecord{0, 0, 0}) {
		t.Fatalf("expected MouseRelative(0,0,0) on Home return, got %+v", last)
	}
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}

	// Non-MouseRelative core ignores mouse updates with 0 MouseRelative writes.
	nonFix, nonB, nonC := backendFixture(t)
	nonSrc := newFakeMultiSource()
	nonP, err := NewProgramMultiInputPlatform(&programInputBase{}, nonB, nonSrc, multiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nonC.Launch(ctx, "native-0"); err != nil {
		t.Fatal(err)
	}
	nonSrc.mice(usbhub.MouseUpdate{DX: 10, DY: 10, Buttons: 0x01})
	if _, err := nonP.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if nonFix.cpu.mouseWrites != 0 {
		t.Fatalf("expected 0 MouseRelative writes on non-MouseRelative core, got %d", nonFix.cpu.mouseWrites)
	}
}

func TestProgramMultiInputPlatformControllersUIFamilyGlyphsAndPerPadProfiles(t *testing.T) {
	var prefs *ProgramPreferences
	f, b, c := backendFixture(t, func(f *serviceFixture) {
		var err error
		prefs, err = LoadProgramPreferences(context.Background(), f.s.state.store, false)
		if err != nil {
			t.Fatalf("LoadProgramPreferences: %v", err)
		}
		if err := f.s.AttachPreferences(prefs); err != nil {
			t.Fatalf("AttachPreferences: %v", err)
		}
	})
	client := tenfoot.NewClient("http://native.invalid", &http.Client{Transport: b, Timeout: 5 * time.Second})
	app := tenfoot.NewApp(client, 1280, 720, 40)
	if err := app.SetPreferenceStore(prefs); err != nil {
		t.Fatalf("SetPreferenceStore: %v", err)
	}
	src := newFakeMultiSource()
	src.setPadDevices(
		usbhub.GamepadDevice{
			ID:      1,
			Name:    "Xbox 360 Controller",
			Vendor:  0x045e,
			Product: 0x028e,
			Kind:    controllers.KindXInput,
			Player:  1,
		},
		usbhub.GamepadDevice{
			ID:        101,
			Name:      "DualSense Wireless Controller",
			Vendor:    0x054c,
			Product:   0x0ce6,
			Kind:      controllers.KindDualSense,
			Bluetooth: true,
			Player:    2,
		},
	)
	cfg := multiTestConfig()
	cfg.App = app
	cfg.Preferences = prefs
	p, err := NewProgramMultiInputPlatform(&programInputBase{}, b, src, cfg)
	if err != nil {
		t.Fatalf("NewProgramMultiInputPlatform: %v", err)
	}

	step := func() tenfoot.PlatformInput {
		frame, err := p.Next(context.Background())
		if err != nil {
			t.Fatalf("Next failed: %v", err)
		}
		for _, cmd := range frame.Commands {
			app.HandleCommand(cmd, frame.At)
		}
		return frame
	}

	// Initial neutral boot frame arms startup pad/keyboard fences.
	step()

	openControllers := func(at time.Time) {
		if app.Snapshot().Controllers.Open {
			app.HandleCommand(tenfoot.CmdBack, at)
		}
		if app.Snapshot().Settings.Open {
			app.HandleCommand(tenfoot.CmdBack, at)
		}
		app.HandleCommand(tenfoot.CmdSettings, at)
		var hasControllersRow bool
		for i, row := range app.Snapshot().Settings.Rows {
			if row.ID == "section-controllers" {
				hasControllersRow = true
				for j := 0; j < i; j++ {
					app.HandleCommand(tenfoot.CmdDown, at)
				}
				app.HandleCommand(tenfoot.CmdSelect, at)
				break
			}
		}
		if !hasControllersRow {
			t.Fatalf("expected Controllers section in Settings rows: %+v", app.Snapshot().Settings.Rows)
		}
	}

	// Open Settings › Controllers and verify both pads, players, and families are present.
	openControllers(time.Now())
	snap := app.Snapshot().Controllers
	if !snap.Open || len(snap.Tabs) != 2 || snap.Tabs[0].Label != "P1" || snap.Tabs[1].Label != "P2" {
		t.Fatalf("unexpected Controllers snapshot: %+v", snap)
	}

	// Press North on wired Xbox P1: switches hints to Xbox glyphs and highlights P1 tab.
	btDev := usbhub.GamepadDevice{
		ID:        101,
		Name:      "DualSense Wireless Controller",
		Vendor:    0x054c,
		Product:   0x0ce6,
		Kind:      controllers.KindDualSense,
		Bluetooth: true,
		Player:    2,
	}
	xboxDev := usbhub.GamepadDevice{
		ID:      1,
		Name:    "Xbox 360 Controller",
		Vendor:  0x045e,
		Product: 0x028e,
		Kind:    controllers.KindXInput,
		Player:  1,
		Buttons: controllers.ButtonNorth,
	}
	src.setPadDevices(xboxDev, btDev)
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1, Buttons: uint32(controllers.ButtonNorth)}, Ports: [usbhub.MaxPlayerPorts]uint8{0, 0}})
	step()
	if got := app.Snapshot().Prompts["A"]; got != string(gfx.PromptRune(gfx.PromptXbox, gfx.PromptSouth)) {
		t.Fatalf("expected Xbox prompt after Xbox P1 press, got %q", got)
	}
	if !app.Snapshot().Controllers.Tabs[0].Pressed {
		t.Fatalf("expected P1 tab Pressed highlight during held Xbox button")
	}

	// Release Xbox ButtonNorth, switch to P1 tab with L1 ("["), release, then press North on BT DualSense.
	xboxDev.Buttons = controllers.ButtonL1
	src.setPadDevices(xboxDev, btDev)
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}})
	step()
	if got := app.Snapshot().Controllers.Tab; got != 0 || app.Snapshot().Controllers.Family != gfx.PromptXbox {
		t.Fatalf("L1 shoulder did not step to Xbox P1 tab: %+v", app.Snapshot().Controllers)
	}
	xboxDev.Buttons = 0
	btDev.Buttons = controllers.ButtonNorth
	src.setPadDevices(xboxDev, btDev)
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1, Buttons: uint32(controllers.ButtonNorth)}, Ports: [usbhub.MaxPlayerPorts]uint8{0, 0}})
	step()
	if got := app.Snapshot().Prompts["A"]; got != string(gfx.PromptRune(gfx.PromptPlayStation, gfx.PromptSouth)) {
		t.Fatalf("expected PlayStation prompt after BT DualSense press, got %q", got)
	}
	if !app.Snapshot().Controllers.Tabs[1].Pressed {
		t.Fatalf("expected P2 tab Pressed highlight during held button")
	}

	// Switch to P2 tab using R1 shoulder command (CmdFilterNext = "]").
	btDev.Buttons = 0
	src.setPadDevices(xboxDev, btDev)
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}})
	step()
	btDev.Buttons = controllers.ButtonR1
	src.setPadDevices(xboxDev, btDev)
	step()
	if got := app.Snapshot().Controllers.Tab; got != 1 || app.Snapshot().Controllers.Family != gfx.PromptPlayStation {
		t.Fatalf("R1 shoulder did not step to PlayStation P2 tab: %+v", app.Snapshot().Controllers)
	}

	// While ButtonR1 stays held on P2, toggle Swap A / B on P2 via Settings › Controllers.
	for _, cmd := range []tenfoot.Command{tenfoot.CmdDown, tenfoot.CmdDown, tenfoot.CmdDown, tenfoot.CmdLeft, tenfoot.CmdLeft, tenfoot.CmdSelect} {
		app.HandleCommand(cmd, time.Now())
	}
	if !app.Snapshot().Controllers.SwapAB {
		t.Fatal("Swap A / B did not enable in Controllers UI")
	}

	// While ButtonR1 is still held on P2, profile reload is deferred, so Swap A / B does not flip mid-hold.
	btDev.Buttons = controllers.ButtonR1 | controllers.ButtonEast
	src.setPadDevices(xboxDev, btDev)
	frame := step()
	if !slices.Contains(frame.Commands, tenfoot.CmdBack) {
		t.Fatalf("expected deferred reload while ButtonR1 held (ButtonEast still CmdBack), got %v", frame.Commands)
	}

	// Release all buttons on P2 -> profile reloads -> ButtonEast now emits CmdSelect and ButtonSouth emits CmdBack.
	btDev.Buttons = 0
	src.setPadDevices(xboxDev, btDev)
	step()
	btDev.Buttons = controllers.ButtonEast
	src.setPadDevices(xboxDev, btDev)
	frame = step()
	if !slices.Contains(frame.Commands, tenfoot.CmdSelect) {
		t.Fatalf("expected swapped ButtonEast to emit CmdSelect, got %v", frame.Commands)
	}
	btDev.Buttons = controllers.ButtonSouth
	src.setPadDevices(xboxDev, btDev)
	frame = step()
	if !slices.Contains(frame.Commands, tenfoot.CmdBack) {
		t.Fatalf("expected swapped ButtonSouth to emit CmdBack, got %v", frame.Commands)
	}

	// Launch a fes.gamepad.ports core and verify P2's Swap A / B profile flips A/B bits on port 2!
	btDev.Buttons = 0
	src.setPadDevices(xboxDev, btDev)
	step()
	ctx := context.Background()
	play, err := c.Launch(ctx, "native-0")
	if err != nil || play.ErrorCode != "" {
		t.Fatalf("Launch failed: play=%+v err=%v", play, err)
	}
	wantCaps := cg.FesComputerCapabilityVideoFixed720p60 | cg.FesComputerCapabilityKeyboardHid | cg.FesComputerCapabilityGamepadPorts
	f.cpu.words[7] = uint16(wantCaps)
	var build [16]byte
	for i := range 8 {
		binary.LittleEndian.PutUint16(build[2*i:], f.cpu.words[8+i])
	}
	reopened, err := computer.Open(ctx, f.s.state.computer.port, computerLimits(), computer.Identity{
		Capabilities: wantCaps,
		Build:        build,
	})
	if err != nil {
		t.Fatalf("reopen Computer: %v", err)
	}
	f.s.state.computer.control = reopened
	f.s.state.viewMu.Lock()
	f.s.state.capabilities = wantCaps
	f.s.state.viewMu.Unlock()

	step() // Enter ProgramRunning & neutralize ports (padArmed=true since btDev is neutral).
	btDev.Buttons = controllers.ButtonEast
	src.setPadDevices(xboxDev, btDev)
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1, Buttons: uint32(controllers.ButtonEast)}, Ports: [usbhub.MaxPlayerPorts]uint8{0, 1 << 5}})
	step()
	// Bit 4 (0x10) is logical A on port 1 (0-based P2), sent when swapped physical ButtonEast is held.
	if f.cpu.controllerWrites == 0 || f.cpu.controllerPorts[1] != (1<<4) {
		t.Fatalf("expected swapped BT P2 physical East -> port 1 A (0x10), got writes=%d ports=%#v", f.cpu.controllerWrites, f.cpu.controllerPorts)
	}

	// Detaching BT P2 drops it from Settings › Controllers once back in menu.
	if res, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil || res.ErrorCode != "" {
		t.Fatalf("StopExpectedStamped failed: res=%+v err=%v", res, err)
	}
	step()
	openControllers(time.Now())
	if !app.Snapshot().Controllers.Open || len(app.Snapshot().Controllers.Tabs) != 2 {
		t.Fatalf("expected Controllers open with 2 tabs before detach, got %+v", app.Snapshot().Controllers)
	}
	src.setPadDevices(xboxDev)
	src.pads(usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}})
	step()
	if snap := app.Snapshot().Controllers; !snap.Open || len(snap.Tabs) != 1 || snap.Tabs[0].Label != "P1" {
		t.Fatalf("expected 1 tab (P1) after P2 detach, got %+v", snap)
	}
}
