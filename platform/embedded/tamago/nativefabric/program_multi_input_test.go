// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/usbhub"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type fakeMultiSource struct {
	kbNeutral     bool
	padNeutral    bool
	kbQueue       [][8]byte
	padQueue      []usbhub.GamepadUpdate
	serviceErr    error
	gameplayCalls []bool
	pairingActive bool
	startPairErr  error
	saveCalls     int
	bondsDirty    bool
	saveErr       error
	pauseCalls    int
	resumeCalls   int
	ready         chan struct{}
}

func newFakeMultiSource() *fakeMultiSource {
	return &fakeMultiSource{
		kbNeutral:  true,
		padNeutral: true,
		ready:      make(chan struct{}, 4),
	}
}

func (s *fakeMultiSource) Service() error { return s.serviceErr }
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
func (s *fakeMultiSource) KeyboardNeutral() bool { return s.kbNeutral }
func (s *fakeMultiSource) GamepadNeutral() bool  { return s.padNeutral }
func (s *fakeMultiSource) SetGameplayActive(active bool) error {
	s.gameplayCalls = append(s.gameplayCalls, active)
	if active {
		s.pairingActive = false
	}
	return nil
}
func (s *fakeMultiSource) StartPairing(time.Duration) error {
	if s.startPairErr != nil {
		return s.startPairErr
	}
	s.pairingActive = true
	return nil
}
func (s *fakeMultiSource) StopPairing() error {
	s.pairingActive = false
	return nil
}
func (s *fakeMultiSource) PairingActive() bool { return s.pairingActive }
func (s *fakeMultiSource) SaveBondsIfDirty(context.Context) (bool, error) {
	s.saveCalls++
	if !s.bondsDirty {
		return false, nil
	}
	if s.saveErr != nil {
		return false, s.saveErr
	}
	s.bondsDirty = false
	return true, nil
}
func (s *fakeMultiSource) PausePolling(context.Context) error  { s.pauseCalls++; return nil }
func (s *fakeMultiSource) ResumePolling(context.Context) error { s.resumeCalls++; return nil }
func (s *fakeMultiSource) Ready() <-chan struct{}              { return s.ready }

func multiTestKeys() ProgramGamepadKeys {
	keys := ProgramGamepadKeys{Up: 0x52, Down: 0x51, Left: 0x50, Right: 0x4f}
	keys.Buttons[0], keys.Buttons[1] = 0x16, 0x2c
	return keys
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
	if _, err := NewProgramMultiInputPlatform(base, b, src, ProgramMultiConfig{
		MenuButtons: input.GamepadButtons{Confirm: 1, Back: 2},
		CoreKeys:    multiTestKeys(),
	}); !errors.Is(err, ErrProgramBusy) {
		t.Fatalf("expected ErrProgramBusy for duplicate attachment, got %v", err)
	}

	// 1. In ProgramReady: Ctrl+Alt+P toggles BT pairing without emitting text 'p',
	// while normal keyboard + gamepad events navigate Tenfoot simultaneously.
	src.bondsDirty = true
	src.kbQueue = append(src.kbQueue,
		[8]byte{0x05, 0, 0x13, 0, 0, 0, 0, 0}, // Ctrl+Alt+P -> start pairing
		[8]byte{0x00, 0, 0x1d, 0, 0, 0, 0, 0}, // 'z' -> Tenfoot text (no nav command)
		[8]byte{},
	)
	src.padQueue = append(src.padQueue,
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
	if len(frame.Commands) < 2 || frame.Commands[0] != tenfoot.CmdUp || frame.Commands[1] != tenfoot.CmdSelect {
		t.Fatalf("expected gamepad Up+Select commands, got %v", frame.Commands)
	}
	if len(bondSaveErrs) != 1 || bondSaveErrs[0] != nil || src.bondsDirty {
		t.Fatalf("expected bond save in ProgramReady, errs=%v dirty=%t", bondSaveErrs, src.bondsDirty)
	}
	// Gameplay mode should have synced false in Ready.
	if len(src.gameplayCalls) != 1 || src.gameplayCalls[0] {
		t.Fatalf("expected SetGameplayActive(false) in Ready, got %v", src.gameplayCalls)
	}

	// Direct Start/StopBluetoothPairing, Present, and serviceMenuInput work in Ready.
	if err := p.Present(ctx, tenfoot.Snapshot{}); err != nil || base.presents != 1 {
		t.Fatalf("Present failed: err=%v presents=%d", err, base.presents)
	}
	src.kbQueue = append(src.kbQueue, [8]byte{0, 0, 0x1d, 0, 0, 0, 0, 0}, [8]byte{})
	if err := p.serviceMenuInput(ctx); err != nil {
		t.Fatalf("serviceMenuInput failed: %v", err)
	}
	frame, err = p.Next(ctx)
	if err != nil || frame.Text != "z" {
		t.Fatalf("expected buffered menu text after serviceMenuInput, frame=%+v err=%v", frame, err)
	}
	if err := p.StopBluetoothPairing(); err != nil || src.PairingActive() {
		t.Fatalf("StopBluetoothPairing failed: err=%v active=%t", err, src.PairingActive())
	}

	// 2. Launch core -> Pause/Resume called, SetGameplayActive(true) disables BT scanning,
	// and keyboard + gamepad rows merge cleanly without clobbering.
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	if src.pauseCalls == 0 || src.resumeCalls == 0 {
		t.Fatalf("expected PausePolling/ResumePolling around Launch, pause=%d resume=%d", src.pauseCalls, src.resumeCalls)
	}

	src.kbQueue = append(src.kbQueue, [8]byte{0, 0, 0x04, 0, 0, 0, 0, 0})                                   // 'a' (row0 bit 4)
	src.padQueue = append(src.padQueue, usbhub.GamepadUpdate{State: usb.GamepadState{Buttons: 1, Hat: -1}}) // S (0x16 -> row1 bit 6)
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
	src.padQueue = append(src.padQueue, usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}})
	if _, err := p.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardRows[0] != 1<<4 || f.cpu.keyboardRows[1] != 0 {
		t.Fatalf("expected pad detach to preserve kbRows and clear padRows, got %v", f.cpu.keyboardRows)
	}

	// 3. BT Home button triggers return (neutralizes matrix and emits CmdStop).
	src.padQueue = append(src.padQueue, usbhub.GamepadUpdate{State: usb.GamepadState{Hat: -1}, HomePressed: true})
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
	src.kbQueue = append(src.kbQueue,
		[8]byte{0x05, 0, 0x29, 0, 0, 0, 0, 0},
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
