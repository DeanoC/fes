// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
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
	KeyboardNeutral() bool
	GamepadNeutral() bool
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
	OnPairingEvent  func(active bool, err error)
	OnBondsSaved    func(err error)
}

// ProgramMultiInputPlatform routes live-hotplugged wired USB keyboards, wired
// USB HID gamepads, and Classic Bluetooth controllers/keyboards through the real
// Tenfoot platform loop and owned ProgramService computer sessions.
type ProgramMultiInputPlatform struct {
	operation       sync.Mutex
	base            tenfoot.Platform
	backend         *ProgramBackend
	source          ProgramMultiSource
	cfg             ProgramMultiConfig
	inputReady      <-chan struct{}
	uartReady       <-chan struct{}
	keyboard        ProgramKeyboard
	session         ProgramSession
	mode            ProgramServiceState
	generation      uint64
	boundary        uint64
	menuKB          usb.BootKeyboard
	coreKB          usb.BootKeyboard
	menuPadPressed  [6]bool
	kbRows          [9]uint16
	padRows         [9]uint16
	prevHome        bool
	kbArmed         bool
	padArmed        bool
	gameplaySynced  bool
	gameplayActive  bool
	returnRequested bool
	fault           error
	pending         []input.Event

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
	p.inputReady = attachInputReady(base, source)
	if uart, ok := base.(interface{ Ready() <-chan struct{} }); ok {
		p.uartReady = uart.Ready()
	}
	p.refreshBluetoothStatus(true)
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
	err := p.source.ForgetBluetoothDevice(address)
	p.refreshBluetoothStatus(true)
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
	if p.boundary != b.inputBoundary || p.mode != v.State || p.generation != v.Generation {
		p.pending = nil
		p.returnRequested = false
		p.kbRows = [9]uint16{}
		p.padRows = [9]uint16{}
		p.prevHome = false
		p.menuKB = usb.BootKeyboard{}
		p.coreKB = usb.BootKeyboard{}
		p.menuPadPressed = [6]bool{}
		// No source service has run since the boundary drained the queues,
		// so the live state is the state at the boundary. Inputs held (or
		// not yet reported) across it keep the fence closed until a queued
		// all-up snapshot; released ones let the first fresh press through.
		p.kbArmed = p.source.KeyboardNeutral()
		p.padArmed = p.source.GamepadNeutral()
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
		if p.keyboard.state == nil || p.keyboard.state.session.Generation() != v.Generation {
			var err error
			p.keyboard, err = b.bindKeyboard(ctx, v.Generation)
			if err != nil {
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
					if err := p.keyboard.state.writeRows(ctx, [9]uint16{}); err != nil {
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
				p.padArmed = input.GamepadNeutral(upd.State) && !upd.HomePressed
				p.prevHome = upd.HomePressed
				continue
			}
			if upd.HomePressed && !p.prevHome {
				if err := p.keyboard.state.writeRows(ctx, [9]uint16{}); err != nil {
					b.inputFault = err
					return nil, err
				}
				p.prevHome = true
				p.returnRequested = true
				events = append(events, input.Event{Key: "s"})
				continue
			}
			p.prevHome = upd.HomePressed
			p.padRows = p.cfg.CoreKeys.rows(upd.State)
			if err := p.keyboard.state.writeRows(ctx, combineRows(p.kbRows, p.padRows)); err != nil {
				b.inputFault = err
				return nil, err
			}
			continue
		}

		// ProgramReady menu gamepad handling.
		if !p.padArmed {
			p.padArmed = input.GamepadNeutral(upd.State) && !upd.HomePressed
			p.prevHome = upd.HomePressed
			continue
		}
		if upd.HomePressed && !p.prevHome {
			events = append(events, input.Event{Key: "o"})
		}
		p.prevHome = upd.HomePressed
		nextPressed := [6]bool{}
		dirs := input.GamepadDirections(upd.State)
		copy(nextPressed[:4], dirs[:])
		nextPressed[4] = upd.State.Buttons&(uint32(1)<<(p.cfg.MenuButtons.Confirm-1)) != 0
		nextPressed[5] = upd.State.Buttons&(uint32(1)<<(p.cfg.MenuButtons.Back-1)) != 0
		names := [6]string{"up", "down", "left", "right", "return", "escape"}
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
