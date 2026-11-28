// SPDX-License-Identifier: GPL-3.0-or-later

// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"sync"
	"time"

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
	rawButtons uint32
	axes       [6][2]bool
	profile    *multiPadProfile
	profileGen uint64
	learning   bool
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
	lastPadBTGen   uint64

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
		p.syncTenfootPadTopology(true)
		var liveBuf [usbhub.MaxGamepadDevices]usbhub.GamepadPadState
		p.processPadStates(p.livePadStatesInto(liveBuf[:]), time.Now())
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
