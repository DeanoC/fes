// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/usbhub"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

const defaultBTPairingDuration = 30 * time.Second

// ProgramMultiSource abstracts the unified USB + Bluetooth hotplug hub for
// Tenfoot navigation, running FPGA cores, radio quiesce, and bond saving.
//
// The report queues carry merged-state changes only and PausePolling drops
// them, so release fences cannot wait for a queued all-up snapshot: one is
// never queued while everything stays released. KeyboardNeutral and
// GamepadNeutral report whether the live merged state is known to be all-up.
type ProgramMultiSource interface {
	Service() error
	NextKeyboardReport() ([8]byte, bool)
	NextGamepadUpdate() (usbhub.GamepadUpdate, bool)
	NextMouseUpdate() (usbhub.MouseUpdate, bool)
	GamepadGeneration() uint64
	GamepadDevicesInto([]usbhub.GamepadDevice) int
	KeyboardNeutral() bool
	GamepadNeutral() bool
	MouseNeutral() bool
	SetGameplayActive(bool) error
	StartPairing(time.Duration) error
	StopPairing() error
	PairingActive() bool
	PairingRemaining() time.Duration
	BluetoothSnapshot() usbhub.BluetoothSnapshot
	BluetoothGeneration() uint64
	ForgetBluetoothDevice(string) error
	SaveBondsIfDirty(context.Context) (bool, error)
	PausePolling(context.Context) error
	ResumePolling(context.Context) error
	Ready() <-chan struct{}
}

var (
	_ ProgramMultiSource          = (*usbhub.Hub)(nil)
	_ tenfoot.BluetoothController = (*ProgramMultiInputPlatform)(nil)
)

// ProgramMultiConfig configures Tenfoot and core mapping plus pairing/bond hooks
// for ProgramMultiInputPlatform.
type ProgramMultiConfig struct {
	MenuButtons     input.GamepadButtons
	CoreKeys        ProgramGamepadKeys
	PairingDuration time.Duration
	App             *tenfoot.App
	Preferences     tenfoot.PreferenceStore
	OnPairingEvent  func(active bool, err error)
	OnBondsSaved    func(err error)
}

type trackedMultiPad struct {
	info       tenfoot.PadInfo
	buttons    controllers.Button
	remap      *inputmap.Remapper
	profileGen uint64
	seen       bool
}

// ProgramMultiInputPlatform routes live-hotplugged wired USB keyboards, wired
// USB HID gamepads, and Classic Bluetooth controllers/keyboards through the real
// Tenfoot platform loop and owned ProgramService computer sessions.
type ProgramMultiInputPlatform struct {
	operation         sync.Mutex
	base              tenfoot.Platform
	backend           *ProgramBackend
	source            ProgramMultiSource
	cfg               ProgramMultiConfig
	inputReady        <-chan struct{}
	uartReady         <-chan struct{}
	keyboard          ProgramKeyboard
	session           ProgramSession
	mode              ProgramServiceState
	generation        uint64
	boundary          uint64
	menuKB            usb.BootKeyboard
	coreKB            usb.BootKeyboard
	menuPadPressed    [10]bool
	kbRows            [9]uint16
	padRows           [9]uint16
	rawCtrlPorts      [usbhub.MaxPlayerPorts]uint8
	ctrlPorts         [usbhub.MaxPlayerPorts]uint8
	nativeControllers bool
	nativeMouse       bool
	mouseButtons      uint8
	prevHome          bool
	kbArmed           bool
	padArmed          bool
	mouseArmed        bool
	gameplaySynced    bool
	gameplayActive    bool
	returnRequested   bool
	traceKeyboard     func(context.Context, ProgramServiceStatus, []byte, bool)
	fault             error
	pending           []input.Event

	pads           map[int]*trackedMultiPad
	padCache       [usbhub.MaxGamepadDevices]usbhub.GamepadDevice
	padCount       int
	padProfiles    map[string]inputmap.Profile
	padProfilesGen uint64
	lastPadGen     uint64

	btMu         sync.RWMutex
	btStatus     tenfoot.BluetoothStatus
	lastBTGen    uint64
	lastBTRemSec int
}

// NewProgramMultiInputPlatform attaches a unified multi-device USB + Bluetooth
// hotplug source to base and backend while backend is in ProgramReady.
func NewProgramMultiInputPlatform(base tenfoot.Platform, backend *ProgramBackend, source ProgramMultiSource, cfg ProgramMultiConfig) (*ProgramMultiInputPlatform, error) {
	if base == nil || backend == nil || backend.service == nil || source == nil {
		return nil, ErrProgramSession
	}
	b := cfg.MenuButtons
	if b.Confirm == 0 || b.Confirm > 32 || b.Back == 0 || b.Back > 32 || b.Confirm == b.Back {
		return nil, ErrProgramSession
	}
	if !cfg.CoreKeys.valid() {
		return nil, ErrProgramSession
	}
	if cfg.PairingDuration <= 0 {
		cfg.PairingDuration = defaultBTPairingDuration
	}
	if !backend.operation.TryLock() {
		return nil, ErrProgramBusy
	}
	defer backend.operation.Unlock()
	if backend.inputAttached || backend.inputFault != nil || backend.service.Status().State != ProgramReady {
		return nil, ErrProgramBusy
	}
	backend.inputAttached = true
	backend.inputPolling = source
	p := &ProgramMultiInputPlatform{
		base:    base,
		backend: backend,
		source:  source,
		cfg:     cfg,
	}
	p.traceKeyboard = lifecycleKeyboardObserver(base)
	p.inputReady = attachInputReady(base, source)
	if uart, ok := base.(interface{ Ready() <-chan struct{} }); ok {
		p.uartReady = uart.Ready()
	}
	p.refreshBluetoothStatus(true)
	if p.cfg.App != nil {
		p.syncTenfootPads(true)
	}
	return p, nil
}

func btRemainingSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

func convertBluetoothSnapshot(s usbhub.BluetoothSnapshot) tenfoot.BluetoothStatus {
	out := tenfoot.BluetoothStatus{
		DongleConnected:  s.DongleConnected,
		DongleName:       s.DongleName,
		PairingActive:    s.PairingActive,
		PairingRemaining: s.PairingRemaining,
		ConnectedCount:   s.ConnectedCount,
		BondsPending:     s.BondsPending,
		BondsSaveFailed:  s.BondsSaveFailed,
	}
	if len(s.Devices) > 0 {
		out.Devices = make([]tenfoot.BluetoothDevice, len(s.Devices))
		for i, dev := range s.Devices {
			var kind string
			if dev.Kind != controllers.KindUnknown {
				kind = dev.Kind.String()
			}
			out.Devices[i] = tenfoot.BluetoothDevice{
				Address:   dev.Address,
				Name:      dev.Name,
				Kind:      kind,
				Connected: dev.Connected,
				Slot:      int(dev.Slot),
			}
		}
	}
	return out
}

func (p *ProgramMultiInputPlatform) refreshBluetoothStatus(force bool) {
	gen := p.source.BluetoothGeneration()
	remSec := btRemainingSeconds(p.source.PairingRemaining())
	if !force && gen == p.lastBTGen && remSec == p.lastBTRemSec {
		return
	}
	snap := convertBluetoothSnapshot(p.source.BluetoothSnapshot())
	p.btMu.Lock()
	p.btStatus = snap
	p.btMu.Unlock()
	p.lastBTGen = gen
	p.lastBTRemSec = remSec
}

// BluetoothStatus returns the nonblocking cached Bluetooth adapter, pairing,
// connected slot, and bond status for Tenfoot Settings rendering.
func (p *ProgramMultiInputPlatform) BluetoothStatus() tenfoot.BluetoothStatus {
	if p == nil {
		return tenfoot.BluetoothStatus{}
	}
	p.btMu.RLock()
	st := p.btStatus
	if len(st.Devices) > 0 {
		st.Devices = append([]tenfoot.BluetoothDevice(nil), st.Devices...)
	}
	p.btMu.RUnlock()
	return st
}

// StartBluetoothPairing opens a bounded SSP pairing window when in ProgramReady.
func (p *ProgramMultiInputPlatform) StartBluetoothPairing(duration time.Duration) error {
	if p == nil || p.backend == nil || p.backend.service == nil || p.source == nil {
		return ErrProgramSession
	}
	if !p.operation.TryLock() {
		return ErrProgramBusy
	}
	defer p.operation.Unlock()
	if p.fault != nil {
		return p.fault
	}
	if !p.backend.operation.TryLock() {
		return ErrProgramBusy
	}
	defer p.backend.operation.Unlock()
	if p.backend.service.Status().State != ProgramReady {
		return errors.New("native input: Bluetooth pairing is only allowed in menu")
	}
	if duration <= 0 {
		duration = p.cfg.PairingDuration
	}
	err := p.source.StartPairing(duration)
	p.refreshBluetoothStatus(true)
	if p.cfg.OnPairingEvent != nil {
		p.cfg.OnPairingEvent(err == nil && p.source.PairingActive(), err)
	}
	return err
}

// StopBluetoothPairing closes an active SSP pairing window.
func (p *ProgramMultiInputPlatform) StopBluetoothPairing() error {
	if p == nil || p.backend == nil || p.source == nil {
		return nil
	}
	if !p.operation.TryLock() {
		return ErrProgramBusy
	}
	defer p.operation.Unlock()
	if !p.backend.operation.TryLock() {
		return ErrProgramBusy
	}
	defer p.backend.operation.Unlock()
	err := p.source.StopPairing()
	p.refreshBluetoothStatus(true)
	if p.cfg.OnPairingEvent != nil {
		p.cfg.OnPairingEvent(false, err)
	}
	return err
}

// ForgetBluetoothDevice deletes the stored bond for address and disconnects any
// active session while in ProgramReady.
func (p *ProgramMultiInputPlatform) ForgetBluetoothDevice(address string) error {
	if p == nil || p.backend == nil || p.backend.service == nil || p.source == nil {
		return ErrProgramSession
	}
	if !p.operation.TryLock() {
		return ErrProgramBusy
	}
	defer p.operation.Unlock()
	if p.fault != nil {
		return p.fault
	}
	if !p.backend.operation.TryLock() {
		return ErrProgramBusy
	}
	defer p.backend.operation.Unlock()
	if p.backend.service.Status().State != ProgramReady {
		return errors.New("native input: forgetting Bluetooth devices is only allowed in menu")
	}
	wasPairing := p.source.PairingActive()
	err := p.source.ForgetBluetoothDevice(address)
	p.refreshBluetoothStatus(true)
	if wasPairing && !p.source.PairingActive() && p.cfg.OnPairingEvent != nil {
		p.cfg.OnPairingEvent(false, nil)
	}
	return err
}

func (p *ProgramMultiInputPlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	if ctx == nil {
		return tenfoot.PlatformInput{}, ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return tenfoot.PlatformInput{}, err
	}
	if !p.operation.TryLock() {
		return tenfoot.PlatformInput{}, ErrProgramBusy
	}
	defer p.operation.Unlock()
	if p.fault != nil {
		return tenfoot.PlatformInput{}, p.fault
	}
	if p.inputReady != nil && p.backend.service.Status().State == ProgramRunning {
		if frame, handled, err := p.runGameplayFastPath(ctx); handled {
			return frame, err
		}
	}
	frame, err := p.base.Next(ctx)
	if err != nil {
		return frame, err
	}
	events, err := p.poll(ctx, false)
	if err != nil {
		return p.handleInputFault(err)
	}
	return p.applyEvents(ctx, frame, events)
}

func (p *ProgramMultiInputPlatform) Present(ctx context.Context, snapshot tenfoot.Snapshot) error {
	return p.base.Present(ctx, snapshot)
}

func (p *ProgramMultiInputPlatform) serviceMenuInput(ctx context.Context) error {
	if !p.operation.TryLock() {
		return ErrProgramBusy
	}
	defer p.operation.Unlock()
	if p.fault != nil {
		return p.fault
	}
	_, err := p.poll(ctx, true)
	if err != nil {
		p.fault = err
	}
	return err
}

func (p *ProgramMultiInputPlatform) runGameplayFastPath(ctx context.Context) (tenfoot.PlatformInput, bool, error) {
	timer := time.NewTimer(gameplayHousekeepingWindow)
	defer timer.Stop()
	for {
		events, err := p.poll(ctx, false)
		if err != nil {
			frame, faultErr := p.handleInputFault(err)
			return frame, true, faultErr
		}
		if len(events) > 0 || p.returnRequested {
			frame, err := p.applyEvents(ctx, tenfoot.PlatformInput{At: time.Now()}, events)
			return frame, true, err
		}
		if p.backend.service.Status().State != ProgramRunning {
			return tenfoot.PlatformInput{}, false, nil
		}
		select {
		case <-ctx.Done():
			return tenfoot.PlatformInput{}, true, ctx.Err()
		case <-p.inputReady:
			continue
		case <-p.uartReady:
			if w, ok := p.base.(interface{ wakeNext() }); ok {
				w.wakeNext()
			}
			return tenfoot.PlatformInput{}, false, nil
		case <-timer.C:
			return tenfoot.PlatformInput{}, false, nil
		}
	}
}

func (p *ProgramMultiInputPlatform) handleInputFault(err error) (tenfoot.PlatformInput, error) {
	p.fault = err
	if p.session.Generation() != 0 && p.backend.service.Status().State == ProgramRunning {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		p.fault = errors.Join(err, p.park(cleanup))
		cancel()
	}
	return tenfoot.PlatformInput{}, p.fault
}

func (p *ProgramMultiInputPlatform) applyEvents(ctx context.Context, frame tenfoot.PlatformInput, events []input.Event) (tenfoot.PlatformInput, error) {
	var commands []tenfoot.Command
	for _, event := range events {
		if command := tenfoot.CommandFromKey(event.Key); command != tenfoot.CmdNone {
			commands = append(commands, command)
		}
		frame.Text += event.Text
	}
	if observer, ok := p.base.(interface {
		observeProgramCommands(context.Context, string, []tenfoot.Command) error
	}); ok {
		if err := observer.observeProgramCommands(ctx, "usb-multi", commands); err != nil {
			p.fault = err
			return tenfoot.PlatformInput{}, err
		}
	}
	frame.Commands = append(frame.Commands, commands...)
	return frame, nil
}

func (p *ProgramMultiInputPlatform) poll(ctx context.Context, buffer bool) ([]input.Event, error) {
	b := p.backend
	if !b.operation.TryLock() {
		return nil, nil
	}
	defer b.operation.Unlock()
	if b.inputFault != nil {
		return nil, b.inputFault
	}
	v := b.service.Status()
	if v.State != ProgramReady && v.State != ProgramRunning {
		return nil, nil
	}
	boundaryChanged := p.boundary != b.inputBoundary || p.mode != v.State || p.generation != v.Generation
	if boundaryChanged {
		p.pending = nil
		p.returnRequested = false
		p.kbRows = [9]uint16{}
		p.padRows = [9]uint16{}
		p.rawCtrlPorts = [usbhub.MaxPlayerPorts]uint8{}
		p.ctrlPorts = [usbhub.MaxPlayerPorts]uint8{}
		p.nativeControllers = false
		p.nativeMouse = false
		p.mouseButtons = 0
		p.prevHome = false
		p.menuKB = usb.BootKeyboard{}
		p.coreKB = usb.BootKeyboard{}
		p.menuPadPressed = [10]bool{}
		// No source service has run since the boundary drained the queues,
		// so the live state is the state at the boundary. Inputs held (or
		// not yet reported) across it keep the fence closed until a queued
		// all-up snapshot; released ones let the first fresh press through.
		p.kbArmed = p.source.KeyboardNeutral()
		p.padArmed = p.source.GamepadNeutral()
		p.mouseArmed = p.source.MouseNeutral()
	}
	wantGameplay := v.State == ProgramRunning
	if !p.gameplaySynced || p.gameplayActive != wantGameplay {
		if err := p.source.SetGameplayActive(wantGameplay); err == nil {
			p.gameplaySynced = true
			p.gameplayActive = wantGameplay
			if wantGameplay {
				p.refreshBluetoothStatus(false)
			}
		}
	}
	if v.State == ProgramRunning {
		p.session = b.session
		boundNew := false
		if p.keyboard.state == nil || p.keyboard.state.session.Generation() != v.Generation {
			var err error
			p.keyboard, err = b.bindKeyboard(ctx, v.Generation)
			if err != nil {
				b.inputFault = err
				return nil, err
			}
			boundNew = true
		}
		caps := b.service.Capabilities(b.session)
		p.nativeControllers = caps&cg.FesComputerCapabilityGamepadPorts != 0
		p.nativeMouse = caps&cg.FesComputerCapabilityMouseRelative != 0
		if boundNew || boundaryChanged {
			if err := p.neutralizeControllerPorts(ctx); err != nil {
				b.inputFault = err
				return nil, err
			}
			if err := p.neutralizeMouse(ctx); err != nil {
				b.inputFault = err
				return nil, err
			}
		}
	}
	p.mode, p.generation, p.boundary = v.State, v.Generation, b.inputBoundary

	if err := p.source.Service(); err != nil {
		b.inputFault = err
		return nil, err
	}
	if p.cfg.App != nil {
		p.syncTenfootPads(false)
	}

	events := p.pending
	p.pending = nil
	var edgeBuf [28]usb.KeyEvent

	kbDrained := false
	for n := 0; n < 8; n++ {
		if v.State == ProgramRunning && p.returnRequested {
			break
		}
		if err := ctx.Err(); err != nil {
			b.inputFault = err
			return nil, err
		}
		rep, ok := p.source.NextKeyboardReport()
		if !ok {
			kbDrained = true
			break
		}
		if p.traceKeyboard != nil {
			p.traceKeyboard(ctx, v, rep[:], p.kbArmed)
		}
		if v.State == ProgramRunning {
			next := p.coreKB
			edgeCount, err := next.FeedInto(rep[:], edgeBuf[:])
			if errors.Is(err, usb.ErrKeyboardRollover) {
				continue
			}
			if err != nil {
				b.inputFault = err
				return nil, err
			}
			if !p.kbArmed {
				p.coreKB = next
				p.kbArmed = rep == ([8]byte{})
				continue
			}
			for _, edge := range edgeBuf[:edgeCount] {
				if edge.Pressed && edge.Usage == 0x29 && edge.Modifiers&0x11 != 0 && edge.Modifiers&0x44 != 0 {
					if err := p.neutralizeCore(ctx); err != nil {
						b.inputFault = err
						return nil, err
					}
					p.coreKB = next
					p.returnRequested = true
					events = append(events, input.Event{Key: "s"})
					break
				}
			}
			if p.returnRequested {
				continue
			}
			p.kbRows = bootReportToRows(rep)
			if err := p.keyboard.state.writeRows(ctx, combineRows(p.kbRows, p.padRows)); err != nil {
				b.inputFault = err
				return nil, err
			}
			p.coreKB = next
			continue
		}

		// ProgramReady menu keyboard handling.
		edgeCount, err := p.menuKB.FeedInto(rep[:], edgeBuf[:])
		if errors.Is(err, usb.ErrKeyboardRollover) {
			continue
		}
		if err != nil {
			b.inputFault = err
			return nil, err
		}
		if !p.kbArmed {
			p.kbArmed = rep == ([8]byte{})
			continue
		}
		for _, edge := range edgeBuf[:edgeCount] {
			if edge.Pressed && edge.Usage == 0x13 && edge.Modifiers&0x11 != 0 && edge.Modifiers&0x44 != 0 {
				p.togglePairing()
				continue
			}
			if ev, ok := input.USBKeyPress(edge); ok {
				if p.cfg.App != nil {
					p.cfg.App.NoteInput(tenfoot.InputKeyboard, 0)
				}
				events = append(events, ev)
			}
		}
		if len(events) > maxProgramMenuEvents {
			b.inputFault = errors.New("native input: menu event queue exhausted")
			return nil, b.inputFault
		}
	}
	// With the queue drained the live state is the newest snapshot. Startup
	// arms here: devices reporting all-up change nothing, so queue nothing.
	if !p.kbArmed && kbDrained {
		p.kbArmed = p.source.KeyboardNeutral()
	}

	padDrained := false
	for n := 0; n < 8; n++ {
		if v.State == ProgramRunning && p.returnRequested {
			break
		}
		if err := ctx.Err(); err != nil {
			b.inputFault = err
			return nil, err
		}
		upd, ok := p.source.NextGamepadUpdate()
		if !ok {
			padDrained = true
			break
		}
		if v.State == ProgramRunning {
			if !p.padArmed {
				p.padArmed = input.GamepadNeutral(upd.State) && !upd.HomePressed && upd.Ports == ([usbhub.MaxPlayerPorts]uint8{})
				p.prevHome = upd.HomePressed
				continue
			}
			if upd.HomePressed && !p.prevHome {
				if err := p.neutralizeCore(ctx); err != nil {
					b.inputFault = err
					return nil, err
				}
				p.prevHome = true
				p.returnRequested = true
				events = append(events, input.Event{Key: "s"})
				continue
			}
			p.prevHome = upd.HomePressed
			p.rawCtrlPorts = upd.Ports
			if p.nativeControllers {
				if err := p.writeControllerPorts(ctx, p.effectiveControllerPorts(p.rawCtrlPorts)); err != nil {
					b.inputFault = err
					return nil, err
				}
				continue
			}
			p.padRows = p.cfg.CoreKeys.rows(upd.State)
			if err := p.keyboard.state.writeRows(ctx, combineRows(p.kbRows, p.padRows)); err != nil {
				b.inputFault = err
				return nil, err
			}
			continue
		}

		// ProgramReady menu gamepad handling.
		if !p.padArmed {
			p.padArmed = input.GamepadNeutral(upd.State) && !upd.HomePressed && upd.Ports == ([usbhub.MaxPlayerPorts]uint8{})
			p.prevHome = upd.HomePressed
			p.menuPadPressed = [10]bool{}
			continue
		}
		if upd.HomePressed && !p.prevHome {
			events = append(events, input.Event{Key: "o"})
		}
		p.prevHome = upd.HomePressed
		if p.cfg.App != nil && p.padCount > 0 {
			continue
		}
		nextPressed := [10]bool{}
		dirs := input.GamepadDirections(upd.State)
		copy(nextPressed[:4], dirs[:])
		nextPressed[4] = upd.State.Buttons&(uint32(1)<<(p.cfg.MenuButtons.Confirm-1)) != 0
		nextPressed[5] = upd.State.Buttons&(uint32(1)<<(p.cfg.MenuButtons.Back-1)) != 0
		names := [10]string{"up", "down", "left", "right", "return", "escape", "x", "i", "[", "]"}
		for i, down := range nextPressed {
			if down && !p.menuPadPressed[i] {
				events = append(events, input.Event{Key: names[i]})
			}
		}
		p.menuPadPressed = nextPressed
		if len(events) > maxProgramMenuEvents {
			b.inputFault = errors.New("native input: menu event queue exhausted")
			return nil, b.inputFault
		}
	}
	if !p.padArmed && padDrained {
		p.padArmed = p.source.GamepadNeutral()
	}
	if v.State == ProgramRunning && p.padArmed && p.nativeControllers && !p.returnRequested && p.cfg.App != nil && p.padCount > 0 {
		if err := p.writeControllerPorts(ctx, p.effectiveControllerPorts(p.rawCtrlPorts)); err != nil {
			b.inputFault = err
			return nil, err
		}
	}
	if v.State == ProgramReady && p.padArmed && p.cfg.App != nil && p.padCount > 0 {
		nextPressed := p.menuControlsFromPads()
		names := [10]string{"up", "down", "left", "right", "return", "escape", "x", "i", "[", "]"}
		for i, down := range nextPressed {
			if down && !p.menuPadPressed[i] {
				events = append(events, input.Event{Key: names[i]})
			}
		}
		p.menuPadPressed = nextPressed
		if len(events) > maxProgramMenuEvents {
			b.inputFault = errors.New("native input: menu event queue exhausted")
			return nil, b.inputFault
		}
	}

	mouseDrained := false
	for n := 0; n < 16; n++ {
		if v.State == ProgramRunning && p.returnRequested {
			break
		}
		if err := ctx.Err(); err != nil {
			b.inputFault = err
			return nil, err
		}
		upd, ok := p.source.NextMouseUpdate()
		if !ok {
			mouseDrained = true
			break
		}
		if v.State == ProgramRunning {
			if !p.mouseArmed {
				p.mouseArmed = upd.Buttons == 0
				continue
			}
			if p.nativeMouse {
				if upd.DX == 0 && upd.DY == 0 && upd.Buttons == p.mouseButtons {
					continue
				}
				if err := b.service.MouseRelative(ctx, p.session, upd.DX, upd.DY, upd.Buttons); err != nil {
					b.inputFault = err
					return nil, err
				}
				p.mouseButtons = upd.Buttons
			}
			continue
		}
		if !p.mouseArmed {
			p.mouseArmed = upd.Buttons == 0
		}
	}
	if !p.mouseArmed && mouseDrained {
		p.mouseArmed = p.source.MouseNeutral()
	}

	// Bond writes run between frames from Next, never from serviceMenuInput
	// while the menu copies scanout rows; the source backs off failed writes.
	if v.State == ProgramReady && !buffer {
		if saved, err := p.source.SaveBondsIfDirty(ctx); (saved || err != nil) && p.cfg.OnBondsSaved != nil {
			p.cfg.OnBondsSaved(err)
		}
		p.refreshBluetoothStatus(false)
	}

	if buffer {
		p.pending = events
		return nil, nil
	}
	return events, nil
}

func (p *ProgramMultiInputPlatform) togglePairing() {
	if p.source.PairingActive() {
		err := p.source.StopPairing()
		if p.cfg.OnPairingEvent != nil {
			p.cfg.OnPairingEvent(false, err)
		}
		return
	}
	err := p.source.StartPairing(p.cfg.PairingDuration)
	if p.cfg.OnPairingEvent != nil {
		p.cfg.OnPairingEvent(err == nil && p.source.PairingActive(), err)
	}
}

func (p *ProgramMultiInputPlatform) park(ctx context.Context) error {
	b := p.backend
	if !b.operation.TryLock() {
		return ErrProgramBusy
	}
	defer b.operation.Unlock()
	if b.service.Status().State != ProgramRunning || b.session != p.session || p.session.Generation() == 0 {
		return ErrProgramSession
	}
	defer b.publish()
	_, err := b.service.Park(ctx, p.session)
	if err == nil {
		if p.keyboard.state != nil {
			p.keyboard.state.closed = true
		}
		b.session, b.play = ProgramSession{}, programPlay{}
	}
	return err
}

func bootReportToRows(rep [8]byte) [9]uint16 {
	var rows [9]uint16
	rows[8] = uint16(rep[0])
	for _, usage := range rep[2:] {
		if usage >= 4 && usage <= 0x7f {
			rows[int(usage)/16] |= 1 << uint(usage%16)
		}
	}
	return rows
}

func combineRows(a, b [9]uint16) [9]uint16 {
	var out [9]uint16
	for i := range out {
		out[i] = a[i] | b[i]
	}
	return out
}

func (p *ProgramMultiInputPlatform) neutralizeControllerPorts(ctx context.Context) error {
	if !p.nativeControllers {
		return nil
	}
	for port := uint8(0); port < uint8(cg.FesComputerControllerPortCount); port++ {
		if err := p.backend.service.ControllerButtons(ctx, p.session, port, 0); err != nil {
			return err
		}
	}
	p.ctrlPorts = [usbhub.MaxPlayerPorts]uint8{}
	return nil
}

func (p *ProgramMultiInputPlatform) neutralizeMouse(ctx context.Context) error {
	if !p.nativeMouse {
		return nil
	}
	if err := p.backend.service.MouseRelative(ctx, p.session, 0, 0, 0); err != nil {
		return err
	}
	p.mouseButtons = 0
	return nil
}

func (p *ProgramMultiInputPlatform) neutralizeCore(ctx context.Context) error {
	if err := p.keyboard.state.writeRows(ctx, [9]uint16{}); err != nil {
		return err
	}
	if err := p.neutralizeControllerPorts(ctx); err != nil {
		return err
	}
	return p.neutralizeMouse(ctx)
}

func (p *ProgramMultiInputPlatform) writeControllerPorts(ctx context.Context, next [usbhub.MaxPlayerPorts]uint8) error {
	for port := uint8(0); port < uint8(cg.FesComputerControllerPortCount) && int(port) < len(next); port++ {
		if next[port] == p.ctrlPorts[port] {
			continue
		}
		if err := p.backend.service.ControllerButtons(ctx, p.session, port, uint16(next[port])); err != nil {
			return err
		}
		p.ctrlPorts[port] = next[port]
	}
	p.ctrlPorts = next
	return nil
}

const (
	tamagoBusUSB       uint16 = 3
	tamagoBusBluetooth uint16 = 5
	tamagoStickZone           = int16(16384)
)

var canonicalButtonMap = []struct {
	bit  controllers.Button
	code remoteinput.Code
}{
	{controllers.ButtonSouth, remoteinput.ButtonA},
	{controllers.ButtonEast, remoteinput.ButtonB},
	{controllers.ButtonWest, remoteinput.ButtonX},
	{controllers.ButtonNorth, remoteinput.ButtonY},
	{controllers.ButtonL1, remoteinput.ButtonL},
	{controllers.ButtonR1, remoteinput.ButtonR},
	{controllers.ButtonSelect, remoteinput.ButtonSelect},
	{controllers.ButtonStart, remoteinput.ButtonStart},
	{controllers.ButtonDPadUp, remoteinput.ButtonDPadUp},
	{controllers.ButtonDPadDown, remoteinput.ButtonDPadDown},
	{controllers.ButtonDPadLeft, remoteinput.ButtonDPadLeft},
	{controllers.ButtonDPadRight, remoteinput.ButtonDPadRight},
}

func (p *ProgramMultiInputPlatform) refreshPadProfiles() {
	if p.cfg.App == nil || p.cfg.Preferences == nil {
		return
	}
	gen := p.cfg.App.PadProfileGeneration() + 1
	if gen == p.padProfilesGen {
		return
	}
	raw, err := p.cfg.Preferences.ReadPreferences()
	profiles := map[string]inputmap.Profile{}
	if err == nil {
		var parsed struct {
			Controllers map[string]inputmap.Profile `json:"controllers"`
		}
		if jsonErr := json.Unmarshal(raw, &parsed); jsonErr == nil && parsed.Controllers != nil {
			profiles = parsed.Controllers
		}
	}
	p.padProfiles = profiles
	p.padProfilesGen = gen
}

func (p *ProgramMultiInputPlatform) syncTenfootPads(force bool) {
	app := p.cfg.App
	if app == nil {
		return
	}
	p.refreshPadProfiles()
	gen := p.source.GamepadGeneration()
	profilePending := false
	for _, tracked := range p.pads {
		if tracked.profileGen != p.padProfilesGen {
			profilePending = true
			break
		}
	}
	if !force && gen == p.lastPadGen && !profilePending {
		return
	}
	n := p.source.GamepadDevicesInto(p.padCache[:])
	p.padCount = n
	p.lastPadGen = gen

	if p.pads == nil {
		p.pads = map[int]*trackedMultiPad{}
	}
	for _, tracked := range p.pads {
		tracked.seen = false
	}
	for i := 0; i < n; i++ {
		dev := p.padCache[i]
		info := tamagoPadInfo(dev)
		tracked, exists := p.pads[dev.ID]
		if !exists {
			tracked = &trackedMultiPad{info: info}
			p.pads[dev.ID] = tracked
			app.SetPadInfo(dev.ID, info)
			app.AttachInput(tenfoot.InputGamepad, dev.ID)
		} else if tracked.info != info {
			tracked.info = info
			app.SetPadInfo(dev.ID, info)
		}
		tracked.seen = true

		if !exists || (tracked.profileGen != p.padProfilesGen && dev.Buttons == 0) {
			remap, err := compileMultiProfile(p.padProfiles[info.GUID])
			if err == nil {
				tracked.remap = remap
			} else {
				tracked.remap = nil
			}
			tracked.profileGen = p.padProfilesGen
		}

		pressedAny := false
		changed := dev.Buttons ^ tracked.buttons
		if changed != 0 {
			for _, m := range canonicalButtonMap {
				if changed&m.bit != 0 {
					down := dev.Buttons&m.bit != 0
					app.NotePadButton(dev.ID, m.code, down)
					if down {
						pressedAny = true
					}
				}
			}
			if !pressedAny && dev.Buttons&^tracked.buttons != 0 {
				pressedAny = true
			}
		}
		if pressedAny || dev.LeftX < -tamagoStickZone || dev.LeftX > tamagoStickZone || dev.LeftY < -tamagoStickZone || dev.LeftY > tamagoStickZone {
			app.NoteInput(tenfoot.InputGamepad, dev.ID)
		}
		tracked.buttons = dev.Buttons
	}
	for id, tracked := range p.pads {
		if tracked.seen {
			continue
		}
		for _, m := range canonicalButtonMap {
			if tracked.buttons&m.bit != 0 {
				app.NotePadButton(id, m.code, false)
			}
		}
		app.DetachInput(tenfoot.InputGamepad, id)
		delete(p.pads, id)
	}
}

func compileMultiProfile(profile inputmap.Profile) (*inputmap.Remapper, error) {
	if profile.Preset == "" && len(profile.Bindings) == 0 {
		return nil, nil
	}
	return inputmap.NewRemapper(profile)
}

func (p *ProgramMultiInputPlatform) effectiveControllerPorts(base [usbhub.MaxPlayerPorts]uint8) [usbhub.MaxPlayerPorts]uint8 {
	ports := base
	for i := 0; i < p.padCount; i++ {
		dev := p.padCache[i]
		if dev.Player < 1 || dev.Player > usbhub.MaxPlayerPorts {
			continue
		}
		tracked := p.pads[dev.ID]
		if tracked == nil || tracked.remap == nil {
			continue
		}
		ports[dev.Player-1] = computerMaskFromControls(remappedControls(dev, tracked.remap))
	}
	return ports
}

func (p *ProgramMultiInputPlatform) menuControlsFromPads() [10]bool {
	var held [10]bool
	for i := 0; i < p.padCount; i++ {
		dev := p.padCache[i]
		var remap *inputmap.Remapper
		if tracked := p.pads[dev.ID]; tracked != nil {
			remap = tracked.remap
		}
		c := remappedControls(dev, remap)
		if c[remoteinput.ButtonDPadUp] {
			held[0] = true
		}
		if c[remoteinput.ButtonDPadDown] {
			held[1] = true
		}
		if c[remoteinput.ButtonDPadLeft] {
			held[2] = true
		}
		if c[remoteinput.ButtonDPadRight] {
			held[3] = true
		}
		if c[remoteinput.ButtonA] {
			held[4] = true
		}
		if c[remoteinput.ButtonB] {
			held[5] = true
		}
		if c[remoteinput.ButtonX] {
			held[6] = true
		}
		if c[remoteinput.ButtonY] {
			held[7] = true
		}
		if c[remoteinput.ButtonL] {
			held[8] = true
		}
		if c[remoteinput.ButtonR] {
			held[9] = true
		}
	}
	return held
}

func remappedControls(dev usbhub.GamepadDevice, remap *inputmap.Remapper) map[remoteinput.Code]bool {
	out := make(map[remoteinput.Code]bool, 12)
	add := func(code remoteinput.Code) {
		out[remap.Target(code)] = true
	}
	for _, m := range canonicalButtonMap {
		if dev.Buttons&m.bit != 0 {
			add(m.code)
		}
	}
	if dev.LeftY < -tamagoStickZone {
		add(remoteinput.ButtonDPadUp)
	} else if dev.LeftY > tamagoStickZone {
		add(remoteinput.ButtonDPadDown)
	}
	if dev.LeftX < -tamagoStickZone {
		add(remoteinput.ButtonDPadLeft)
	} else if dev.LeftX > tamagoStickZone {
		add(remoteinput.ButtonDPadRight)
	}
	return out
}

func computerMaskFromControls(c map[remoteinput.Code]bool) uint8 {
	var mask uint8
	if c[remoteinput.ButtonDPadUp] {
		mask |= 1 << 0
	}
	if c[remoteinput.ButtonDPadDown] {
		mask |= 1 << 1
	}
	if c[remoteinput.ButtonDPadLeft] {
		mask |= 1 << 2
	}
	if c[remoteinput.ButtonDPadRight] {
		mask |= 1 << 3
	}
	if c[remoteinput.ButtonA] {
		mask |= 1 << 4
	}
	if c[remoteinput.ButtonB] {
		mask |= 1 << 5
	}
	if c[remoteinput.ButtonSelect] {
		mask |= 1 << 6
	}
	if c[remoteinput.ButtonStart] {
		mask |= 1 << 7
	}
	return mask
}

func tamagoPadInfo(dev usbhub.GamepadDevice) tenfoot.PadInfo {
	bus := tamagoBusUSB
	if dev.Bluetooth {
		bus = tamagoBusBluetooth
	}
	return tenfoot.PadInfo{
		Name:       dev.Name,
		GUID:       tamagoPadGUID(bus, dev.Name, dev.Vendor, dev.Product),
		Vendor:     dev.Vendor,
		Product:    dev.Product,
		Family:     tamagoPromptFamily(dev.Kind, dev.Vendor),
		Recognised: dev.Kind != controllers.KindGenericHID && dev.Kind != controllers.KindUnknown,
		Bluetooth:  dev.Bluetooth,
		Player:     dev.Player,
	}
}

func tamagoPromptFamily(kind controllers.Kind, vendor uint16) gfx.PromptFamily {
	switch kind {
	case controllers.KindDualSense, controllers.KindDualShock4:
		return gfx.PromptPlayStation
	case controllers.KindXInput, controllers.KindXboxGIP:
		return gfx.PromptXbox
	case controllers.KindSwitchPro:
		return gfx.PromptNintendo
	}
	switch vendor {
	case 0x054c:
		return gfx.PromptPlayStation
	case 0x045e:
		return gfx.PromptXbox
	case 0x057e:
		return gfx.PromptNintendo
	default:
		return gfx.PromptGeneric
	}
}

func tamagoPadGUID(bus uint16, name string, vendor, product uint16) string {
	var g [16]byte
	binary.LittleEndian.PutUint16(g[0:], bus)
	binary.LittleEndian.PutUint16(g[2:], sdlCRC16(name))
	if vendor != 0 {
		binary.LittleEndian.PutUint16(g[4:], vendor)
		binary.LittleEndian.PutUint16(g[8:], product)
	} else {
		copy(g[4:15], name)
	}
	return hex.EncodeToString(g[:])
}

func sdlCRC16(name string) uint16 {
	var crc uint16
	for i := 0; i < len(name); i++ {
		crc ^= uint16(name[i])
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}
