// SPDX-License-Identifier: GPL-3.0-or-later

// Package usbhub coordinates multiple hotpluggable wired USB HID devices
// (boot keyboards and HID gamepads) alongside one Classic Bluetooth HCI dongle
// sharing a single usb.BusHost.
package usbhub

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hidp"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

const (
	// MaxPlayerPorts is the maximum number of independent player slots tracked
	// by Hub (P1..P4) without renumbering surviving players on disconnect.
	MaxPlayerPorts = 4

	// MaxGamepadDevices is the maximum number of concurrent wired and
	// Bluetooth gamepads reported by Hub.GamepadDevicesInto.
	MaxGamepadDevices = maxWiredGamepads + hidp.MaxPlayerSlots

	maxWiredKeyboards = 4
	maxWiredGamepads  = 4
	maxWiredMice      = 4
	maxQueuedReports  = 16
	maxPollsPerSlot   = 4

	// A failed bond write is retried after a doubling delay, so callers may
	// poll SaveBondsIfDirty every frame without hammering storage.
	bondRetryInitial = time.Second
	bondRetryMax     = time.Minute
)

// GamepadPadState is a per-pad canonical snapshot carried inside GamepadUpdate
// so callers preserve fast press/release edges without allocating.
type GamepadPadState struct {
	ID         int
	Player     uint8
	Buttons    controllers.Button
	LeftX      int16
	LeftY      int16
	RawButtons uint32
	RawAxes    [6]int16
}

// GamepadUpdate carries a normalized merged gamepad snapshot, whether any
// controller's Home/PS/Guide button is currently held, per-player 8-bit
// fes.computer button masks indexed by zero-based player port (P1..P4), and
// per-pad canonical snapshots for consumer profiles and edge preservation.
type GamepadUpdate struct {
	State       usb.GamepadState
	HomePressed bool
	Ports       [MaxPlayerPorts]uint8
	PadCount    uint8
	Pads        [MaxGamepadDevices]GamepadPadState
}

// MouseUpdate carries one signed relative HID boot-mouse motion packet and the
// merged 2-bit Left/Right button state across all attached wired mice.
type MouseUpdate struct {
	DX      int8
	DY      int8
	Buttons uint8
}

// DeviceEventKind classifies hotplug topology and player transitions emitted by Hub.
type DeviceEventKind uint8

const (
	EventKeyboardAttached DeviceEventKind = iota + 1
	EventKeyboardDetached
	EventGamepadAttached
	EventGamepadDetached
	EventMouseAttached
	EventMouseDetached
	EventBTDongleAttached
	EventBTDongleDetached
	EventBTPlayerAttached
	EventBTPlayerDetached
)

func (k DeviceEventKind) String() string {
	switch k {
	case EventKeyboardAttached:
		return "usb-keyboard-attached"
	case EventKeyboardDetached:
		return "usb-keyboard-detached"
	case EventGamepadAttached:
		return "usb-gamepad-attached"
	case EventGamepadDetached:
		return "usb-gamepad-detached"
	case EventMouseAttached:
		return "usb-mouse-attached"
	case EventMouseDetached:
		return "usb-mouse-detached"
	case EventBTDongleAttached:
		return "bt-dongle-attached"
	case EventBTDongleDetached:
		return "bt-dongle-detached"
	case EventBTPlayerAttached:
		return "bt-player-attached"
	case EventBTPlayerDetached:
		return "bt-player-detached"
	default:
		return "unknown"
	}
}

// DeviceEvent describes an attach/detach transition on the shared USB/BT hub.
type DeviceEvent struct {
	Kind      DeviceEventKind
	Address   uint8
	Interface uint8
	Slot      uint8
	Name      string
}

// BluetoothDevice describes one paired or connected Classic Bluetooth device.
type BluetoothDevice struct {
	Address   string
	Name      string
	Kind      controllers.Kind
	Connected bool
	Slot      uint8 // 1..4 when assigned a player port, 0 when disconnected or unassigned
}

// BluetoothSnapshot summarizes the active USB Bluetooth adapter, pairing
// state, bonded/connected controllers, and bond persistence state.
type BluetoothSnapshot struct {
	DongleConnected  bool
	DongleName       string
	PairingActive    bool
	PairingRemaining time.Duration
	ConnectedCount   int
	Devices          []BluetoothDevice
	BondsPending     bool
	BondsSaveFailed  bool
}

// GamepadDevice describes one active wired USB or Classic Bluetooth gamepad
// tracked by Hub for UI layout selection, live button preview, and player assignment.
type GamepadDevice struct {
	ID           int
	Name         string
	IdentityName string
	Vendor       uint16
	Product      uint16
	Version      uint16
	Kind         controllers.Kind
	Bluetooth    bool
	Player       int // 1..4 when assigned a player port, 0 when menu-only overflow
	Buttons      controllers.Button
	LeftX        int16
	LeftY        int16
	RawButtons   uint32
	RawAxes      [6]int16
}

// Config configures optional Bluetooth bond/firmware persistence and logging.
type Config struct {
	Bonds         *l2cap.BondStore
	Firmware      hci.FirmwareLoader
	Now           func() time.Time
	PersistBonds  func(context.Context, []byte) error
	OnDeviceEvent func(DeviceEvent)
}

type wiredKeyboardSlot struct {
	active bool
	// reported is set by the first valid report since attach. Until then the
	// slot's state is unknown, so KeyboardNeutral stays false.
	reported bool
	addr     uint8
	iface    uint8
	pipe     usb.Pipe
	report   [8]byte
}

type gipAckRequest struct {
	seq        uint8
	cmdID      uint8
	payloadLen uint8
}

type playerPortSource uint8

const (
	playerPortNone playerPortSource = iota
	playerPortWired
	playerPortBT
)

type playerPortOwner struct {
	source playerPortSource
	slot   uint8
}

type wiredGamepadSlot struct {
	active bool
	// reported is set by the first decoded report since attach. Until then
	// the slot's state is unknown, so GamepadNeutral stays false.
	reported     bool
	addr         uint8
	iface        uint8
	name         string
	vendorID     uint16
	productID    uint16
	version      uint16
	kind         controllers.Kind
	pipe         usb.Pipe
	outPipe      usb.Pipe
	decoder      *usb.Gamepad
	state        usb.GamepadState
	ctrlState    controllers.State
	ctrlMask     uint8
	home         bool
	switchCal    controllers.SwitchStickCal
	initStep     uint8
	initNextTry  time.Time
	initAttempts uint8
	txSeq        uint8
	outPending   bool
	ledAssigned  bool
	ledPort      uint8
	ledDirty     bool
	rumbleRight  uint8
	rumbleLeft   uint8
	gipAckQueue  [4]gipAckRequest
	gipAckHead   uint8
	gipAckCount  uint8
}

type wiredMouseSlot struct {
	active bool
	// reported is set by the first valid boot-mouse report since attach.
	// Until then the slot's buttons are unknown, so MouseNeutral stays false.
	reported bool
	addr     uint8
	iface    uint8
	pipe     usb.Pipe
	buttons  uint8
}

// Hub supervises wired USB boot keyboards, wired USB boot mice, wired USB HID
// gamepads, and one Classic Bluetooth dongle on a shared usb.BusHost with
// zero-device boot and live hotplug neutralization.
type Hub struct {
	bus usb.BusHost
	bt  *bt.Manager
	cfg Config

	wiredKeyboards [maxWiredKeyboards]wiredKeyboardSlot
	wiredMice      [maxWiredMice]wiredMouseSlot
	wiredGamepads  [maxWiredGamepads]wiredGamepadSlot

	btKeyboards     [hidp.MaxPlayerSlots][8]byte
	btMouseActive   [hidp.MaxPlayerSlots]bool
	btMouseReported [hidp.MaxPlayerSlots]bool
	btMouseButtons  [hidp.MaxPlayerSlots]uint8
	btSlotActive    [hidp.MaxPlayerSlots]bool
	btSlotKind      [hidp.MaxPlayerSlots]controllers.Kind
	btSlotName      [hidp.MaxPlayerSlots]string
	btGamepads      [hidp.MaxPlayerSlots]usb.GamepadState
	btCtrlState     [hidp.MaxPlayerSlots]controllers.State
	btMasks         [hidp.MaxPlayerSlots]uint8
	btHome          [hidp.MaxPlayerSlots]bool

	playerPorts [MaxPlayerPorts]playerPortOwner

	mergedKeyboard       [8]byte
	mergedMouseButtons   uint8
	mergedGamepad        usb.GamepadState
	mergedHome           bool
	mergedPorts          [MaxPlayerPorts]uint8
	mergedActivePadCount uint8
	mergedActivePads     [MaxGamepadDevices]GamepadPadState

	kbQueue    [maxQueuedReports][8]byte
	kbHead     int
	kbCount    int
	mouseQueue [maxQueuedReports]MouseUpdate
	mouseHead  int
	mouseCount int
	padQueue   [maxQueuedReports]GamepadUpdate
	padHead    int
	padCount   int

	pollBuf     [usb.MaxHIDReportBytes]byte
	descBuf     [usb.MaxConfigurationBytes]byte
	txBuf       [usb.MaxHIDReportBytes]byte
	initialized bool

	bondRetryAt    time.Time
	bondRetryDelay time.Duration
	bondSaveFailed bool
	btGeneration   uint64
	padGeneration  uint64
}

// NewHub constructs a Hub over bus. Call Close when shutting down wired pipes;
// the caller keeps ownership of the underlying bus lease.
func NewHub(bus usb.BusHost, cfg Config) (*Hub, error) {
	if bus == nil {
		return nil, errors.New("usbhub: nil bus host")
	}
	if cfg.Bonds == nil {
		cfg.Bonds = l2cap.NewBondStore()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	h := &Hub{
		bus:           bus,
		cfg:           cfg,
		mergedGamepad: usb.GamepadState{Hat: -1},
		btGeneration:  1,
		padGeneration: 1,
	}
	for i := range h.wiredGamepads {
		h.wiredGamepads[i].state = usb.GamepadState{Hat: -1}
	}
	for i := range h.btGamepads {
		h.btGamepads[i] = usb.GamepadState{Hat: -1}
	}
	h.bt = bt.NewManager(bus, cfg.Bonds, cfg.Firmware, cfg.Now)
	h.bt.OnSlotKeyboardReport = func(slot uint8, report []byte) {
		if slot >= hidp.MaxPlayerSlots || !validBootKeyboardReport(report) {
			return
		}
		h.deactivateBTMouse(slot)
		var next [8]byte
		copy(next[:], report)
		if next == h.btKeyboards[slot] {
			return
		}
		h.btKeyboards[slot] = next
		h.recomputeKeyboard()
	}
	h.bt.OnSlotMouseReport = func(slot uint8, dx, dy int8, buttons uint8) {
		if slot >= hidp.MaxPlayerSlots {
			return
		}
		h.activateBTMouse(slot)
		h.btMouseReported[slot] = true
		h.btMouseButtons[slot] = buttons & 0x03
		merged := h.computeMouseButtons()
		if dx != 0 || dy != 0 || merged != h.mergedMouseButtons {
			h.mergedMouseButtons = merged
			h.enqueueMouse(MouseUpdate{DX: dx, DY: dy, Buttons: merged})
		}
	}
	h.bt.OnGamepadState = func(slot uint8, kind controllers.Kind, state controllers.State) {
		if slot >= hidp.MaxPlayerSlots {
			return
		}
		wasActive := h.btSlotActive[slot]
		prevCtrl := h.btCtrlState[slot]
		h.btSlotActive[slot] = true
		if kind != controllers.KindUnknown {
			h.btSlotKind[slot] = kind
		}
		h.assignPlayerPort(playerPortBT, slot)
		h.btGamepads[slot] = state.ToUSBGamepadState()
		h.btCtrlState[slot] = state
		h.btMasks[slot] = state.ToComputerMask()
		h.btHome[slot] = state.Buttons&controllers.ButtonHome != 0
		if !wasActive || state.Buttons != prevCtrl.Buttons || state.LeftX != prevCtrl.LeftX || state.LeftY != prevCtrl.LeftY {
			h.padGeneration++
		}
		h.recomputeGamepad()
	}
	h.bt.OnSlotChanged = func(slot uint8, connected bool, kind controllers.Kind, name string) {
		if slot < hidp.MaxPlayerSlots {
			if connected {
				if kind.IsGamepad() {
					h.deactivateBTMouse(slot)
					h.btSlotActive[slot] = true
					h.btSlotKind[slot] = kind
					h.btSlotName[slot] = name
					if h.assignPlayerPort(playerPortBT, slot) < 0 {
						_ = h.bt.SetPlayerPort(slot, 0, false)
					}
					h.padGeneration++
				} else if kind == controllers.KindMouse || kind == controllers.KindUnknown {
					h.activateBTMouse(slot)
				}
			} else {
				wasGamepad := h.btSlotActive[slot] || kind.IsGamepad()
				h.btSlotActive[slot] = false
				h.btSlotKind[slot] = controllers.KindUnknown
				h.btSlotName[slot] = ""
				h.btGamepads[slot] = usb.GamepadState{Hat: -1}
				h.btCtrlState[slot] = controllers.State{}
				h.btMasks[slot] = 0
				h.btHome[slot] = false
				h.releasePlayerPort(playerPortBT, slot)
				if wasGamepad {
					h.padGeneration++
				}
				h.recomputeGamepad()
				if kind == controllers.KindUnknown && h.btKeyboards[slot] != ([8]byte{}) {
					h.btKeyboards[slot] = [8]byte{}
					h.recomputeKeyboard()
				}
				if !kind.IsGamepad() {
					h.deactivateBTMouse(slot)
				}
			}
		}
		evKind := EventBTPlayerDetached
		if connected {
			evKind = EventBTPlayerAttached
		}
		h.btGeneration++
		h.emit(DeviceEvent{Kind: evKind, Slot: slot, Name: name})
	}
	h.bt.OnDongleChanged = func(connected bool, spec hci.DongleSpec) {
		evKind := EventBTDongleDetached
		if connected {
			evKind = EventBTDongleAttached
		}
		h.btGeneration++
		h.emit(DeviceEvent{Kind: evKind, Name: spec.Name})
	}
	return h, nil
}

func (h *Hub) emit(ev DeviceEvent) {
	if h.cfg.OnDeviceEvent != nil {
		h.cfg.OnDeviceEvent(ev)
	}
}

// Bluetooth returns the underlying Bluetooth manager.
func (h *Hub) Bluetooth() *bt.Manager { return h.bt }

// Ready returns the underlying bus interrupt notification channel, or nil.
func (h *Hub) Ready() <-chan struct{} {
	if h == nil || h.bus == nil {
		return nil
	}
	return h.bus.Ready()
}

// PausePolling forwards input-boundary pause requests to the bus if supported
// and flushes stale unconsumed report queues while keeping live merged state
// intact. The queues carry only merged changes, so consumers re-arm release
// fences from KeyboardNeutral/GamepadNeutral at the boundary: inputs held
// across it keep the fences closed, while released ones arm them at once.
func (h *Hub) PausePolling(ctx context.Context) error {
	if h == nil || h.bus == nil {
		return errors.New("usbhub: nil bus host")
	}
	if pc, ok := h.bus.(input.PollingControl); ok {
		if err := pc.PausePolling(ctx); err != nil {
			return err
		}
	}
	h.kbHead, h.kbCount = 0, 0
	h.mouseHead, h.mouseCount = 0, 0
	h.padHead, h.padCount = 0, 0
	return nil
}

// ResumePolling forwards input-boundary resume requests to the bus if supported.
func (h *Hub) ResumePolling(ctx context.Context) error {
	if h == nil || h.bus == nil {
		return errors.New("usbhub: nil bus host")
	}
	if pc, ok := h.bus.(input.PollingControl); ok {
		return pc.ResumePolling(ctx)
	}
	return nil
}

// SetGameplayActive configures Classic Bluetooth radio scan suppression for gameplay.
func (h *Hub) SetGameplayActive(active bool) error {
	if h == nil || h.bt == nil {
		return nil
	}
	return h.bt.SetGameplayActive(active)
}

// Close closes every open wired HID pipe and resets merged state to neutral.
// It intentionally leaves the shared usb.BusHost lease open for the caller.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	for i := range h.btKeyboards {
		h.btKeyboards[i] = [8]byte{}
	}
	for i := range h.btMouseButtons {
		h.btMouseActive[i] = false
		h.btMouseReported[i] = false
		h.btMouseButtons[i] = 0
	}
	for i := range h.btGamepads {
		h.btSlotActive[i] = false
		h.btSlotKind[i] = controllers.KindUnknown
		h.btSlotName[i] = ""
		h.btGamepads[i] = usb.GamepadState{Hat: -1}
		h.btCtrlState[i] = controllers.State{}
		h.btMasks[i] = 0
		h.btHome[i] = false
	}
	h.playerPorts = [MaxPlayerPorts]playerPortOwner{}
	h.padGeneration++
	for i := range h.wiredKeyboards {
		h.closeWiredKeyboard(i)
	}
	for i := range h.wiredMice {
		h.closeWiredMouse(i)
	}
	for i := range h.wiredGamepads {
		h.closeWiredGamepad(i)
	}
	if merged := h.computeMouseButtons(); merged != h.mergedMouseButtons {
		h.mergedMouseButtons = merged
		h.enqueueMouse(MouseUpdate{Buttons: merged})
	}
	h.recomputeKeyboard()
	h.recomputeGamepad()
}

// Service advances the USB bus, processes live attach/detach transitions across
// wired HID keyboards, wired HID gamepads, and Bluetooth HCI dongles, and polls
// active input streams without allocating on the steady-state hot path.
func (h *Hub) Service() error {
	if h == nil || h.bus == nil {
		return errors.New("usbhub: nil bus host")
	}
	if err := h.bus.Service(); err != nil {
		if errors.Is(err, usbcontrol.ErrTransition) || errors.Is(err, usbcontrol.ErrBusy) {
			return nil
		}
		return err
	}
	if !h.initialized {
		h.initialized = true
		for _, dev := range h.bus.Devices() {
			h.attachDevice(dev)
		}
	}
	for {
		ev, ok := h.bus.NextEvent()
		if !ok {
			break
		}
		switch ev.Kind {
		case usb.DeviceAttached:
			h.attachDevice(ev.Device)
			h.bt.HandleBusEvent(ev)
		case usb.DeviceDetached:
			h.detachDevice(ev.Device.Address)
			h.bt.HandleBusEvent(ev)
		}
	}
	for i := range h.wiredKeyboards {
		if !h.wiredKeyboards[i].active {
			continue
		}
		for p := 0; p < maxPollsPerSlot; p++ {
			n, err := h.wiredKeyboards[i].pipe.PollInto(h.pollBuf[:])
			if err != nil {
				if errors.Is(err, usbcontrol.ErrTransition) || errors.Is(err, usbcontrol.ErrBusy) {
					return nil
				}
				h.closeWiredKeyboard(i)
				break
			}
			if n == 0 {
				break
			}
			if validBootKeyboardReport(h.pollBuf[:n]) {
				h.wiredKeyboards[i].reported = true
				var snap [8]byte
				copy(snap[:], h.pollBuf[:8])
				if snap != h.wiredKeyboards[i].report {
					h.wiredKeyboards[i].report = snap
					h.recomputeKeyboard()
				}
			}
		}
	}
	for i := range h.wiredMice {
		if !h.wiredMice[i].active {
			continue
		}
		for p := 0; p < maxPollsPerSlot; p++ {
			n, err := h.wiredMice[i].pipe.PollInto(h.pollBuf[:])
			if err != nil {
				if errors.Is(err, usbcontrol.ErrTransition) || errors.Is(err, usbcontrol.ErrBusy) {
					return nil
				}
				h.closeWiredMouse(i)
				break
			}
			if n == 0 {
				break
			}
			if n >= 3 {
				h.wiredMice[i].reported = true
				buttons := h.pollBuf[0] & 0x03
				dx, dy := int8(h.pollBuf[1]), int8(h.pollBuf[2])
				h.wiredMice[i].buttons = buttons
				merged := h.computeMouseButtons()
				if dx != 0 || dy != 0 || merged != h.mergedMouseButtons {
					h.mergedMouseButtons = merged
					h.enqueueMouse(MouseUpdate{DX: dx, DY: dy, Buttons: merged})
				}
			}
		}
	}
	for i := range h.wiredGamepads {
		if !h.wiredGamepads[i].active {
			continue
		}
		if h.serviceWiredInit(i) {
			return nil
		}
		if !h.wiredGamepads[i].active {
			continue
		}
		for p := 0; p < maxPollsPerSlot; p++ {
			if slot := &h.wiredGamepads[i]; slot.kind == controllers.KindXboxGIP && int(slot.gipAckCount) == len(slot.gipAckQueue) {
				// Leave the next report on the device until a queued ACK can be
				// sent. GIP request packets cannot be discarded without losing
				// their required ACK.
				break
			}
			n, err := h.wiredGamepads[i].pipe.PollInto(h.pollBuf[:])
			if err != nil {
				if errors.Is(err, usbcontrol.ErrTransition) || errors.Is(err, usbcontrol.ErrBusy) {
					return nil
				}
				h.closeWiredGamepad(i)
				break
			}
			if n == 0 {
				break
			}
			h.feedWiredGamepadPacket(i, h.pollBuf[:n])
			if !h.wiredGamepads[i].active {
				break
			}
		}
	}
	wasPairing := h.bt.PairingActive()
	wasDirty := h.cfg.Bonds != nil && h.cfg.Bonds.Dirty()
	err := h.bt.ServiceSession()
	if h.bt.PairingActive() != wasPairing || (h.cfg.Bonds != nil && h.cfg.Bonds.Dirty() != wasDirty) {
		h.btGeneration++
	}
	return err
}
