// SPDX-License-Identifier: GPL-3.0-or-later

// Package usbhub coordinates multiple hotpluggable wired USB HID devices
// (boot keyboards and HID gamepads) alongside one Classic Bluetooth HCI dongle
// sharing a single usb.BusHost.
package usbhub

import (
	"context"
	"errors"
	"slices"
	"strings"
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

// GamepadUpdate carries a normalized merged gamepad snapshot, whether any
// controller's Home/PS/Guide button is currently held, and per-player 8-bit
// fes.computer button masks indexed by zero-based player port (P1..P4).
type GamepadUpdate struct {
	State       usb.GamepadState
	HomePressed bool
	Ports       [MaxPlayerPorts]uint8
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
	vendorID     uint16
	productID    uint16
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

	btKeyboards    [hidp.MaxPlayerSlots][8]byte
	btMouseButtons [hidp.MaxPlayerSlots]uint8
	btSlotActive   [hidp.MaxPlayerSlots]bool
	btGamepads     [hidp.MaxPlayerSlots]usb.GamepadState
	btMasks        [hidp.MaxPlayerSlots]uint8
	btHome         [hidp.MaxPlayerSlots]bool

	playerPorts [MaxPlayerPorts]playerPortOwner

	mergedKeyboard     [8]byte
	mergedMouseButtons uint8
	mergedGamepad      usb.GamepadState
	mergedHome         bool
	mergedPorts        [MaxPlayerPorts]uint8

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
		h.btMouseButtons[slot] = buttons & 0x03
		merged := h.computeMouseButtons()
		if dx != 0 || dy != 0 || merged != h.mergedMouseButtons {
			h.mergedMouseButtons = merged
			h.enqueueMouse(MouseUpdate{DX: dx, DY: dy, Buttons: merged})
		}
	}
	h.bt.OnGamepadState = func(slot uint8, _ controllers.Kind, state controllers.State) {
		if slot >= hidp.MaxPlayerSlots {
			return
		}
		h.btSlotActive[slot] = true
		h.assignPlayerPort(playerPortBT, slot)
		h.btGamepads[slot] = state.ToUSBGamepadState()
		h.btMasks[slot] = state.ToComputerMask()
		h.btHome[slot] = state.Buttons&controllers.ButtonHome != 0
		h.recomputeGamepad()
	}
	h.bt.OnSlotChanged = func(slot uint8, connected bool, kind controllers.Kind, name string) {
		if slot < hidp.MaxPlayerSlots {
			if connected && kind != controllers.KindUnknown {
				h.btSlotActive[slot] = true
				if h.assignPlayerPort(playerPortBT, slot) < 0 {
					_ = h.bt.SetPlayerPort(slot, 0, false)
				}
			} else if !connected {
				h.btSlotActive[slot] = false
				h.btGamepads[slot] = usb.GamepadState{Hat: -1}
				h.btMasks[slot] = 0
				h.btHome[slot] = false
				h.releasePlayerPort(playerPortBT, slot)
				h.recomputeGamepad()
				if kind == controllers.KindUnknown {
					if h.btKeyboards[slot] != ([8]byte{}) {
						h.btKeyboards[slot] = [8]byte{}
						h.recomputeKeyboard()
					}
					if h.btMouseButtons[slot] != 0 {
						h.btMouseButtons[slot] = 0
						merged := h.computeMouseButtons()
						if merged != h.mergedMouseButtons {
							h.mergedMouseButtons = merged
							h.enqueueMouse(MouseUpdate{Buttons: merged})
						}
					}
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

// StartPairing opens a bounded SSP pairing window in menu mode.
func (h *Hub) StartPairing(duration time.Duration) error {
	if h == nil || h.bt == nil {
		return errors.New("usbhub: Bluetooth unavailable")
	}
	err := h.bt.StartPairing(duration)
	h.btGeneration++
	return err
}

// StopPairing closes any open Bluetooth pairing window.
func (h *Hub) StopPairing() error {
	if h == nil || h.bt == nil {
		return nil
	}
	err := h.bt.StopPairing()
	h.btGeneration++
	return err
}

// PairingActive reports whether a Bluetooth pairing window is open.
func (h *Hub) PairingActive() bool {
	return h != nil && h.bt != nil && h.bt.PairingActive()
}

// PairingRemaining returns the remaining time in the active Bluetooth pairing window.
func (h *Hub) PairingRemaining() time.Duration {
	if h == nil || h.bt == nil {
		return 0
	}
	return h.bt.PairingRemaining()
}

// BluetoothGeneration returns a monotonically increasing counter bumped whenever
// Bluetooth dongle, pairing, connected slot, or bond persistence state changes.
func (h *Hub) BluetoothGeneration() uint64 {
	if h == nil {
		return 0
	}
	return h.btGeneration
}

// BluetoothSnapshot returns a merged view of adapter state, active pairing window,
// bonded devices, connected player slots, and pending/failed bond saves.
func (h *Hub) BluetoothSnapshot() BluetoothSnapshot {
	if h == nil || h.bt == nil {
		return BluetoothSnapshot{}
	}
	snap := BluetoothSnapshot{
		DongleConnected:  h.bt.DongleConnected(),
		DongleName:       h.bt.DongleSpec().Name,
		PairingActive:    h.bt.PairingActive(),
		PairingRemaining: h.bt.PairingRemaining(),
		ConnectedCount:   h.bt.ConnectedPlayers(),
		BondsPending:     h.cfg.Bonds != nil && h.cfg.Bonds.Dirty() && h.cfg.PersistBonds != nil,
		BondsSaveFailed:  h.bondSaveFailed,
	}
	byAddr := make(map[l2cap.BDAddr]BluetoothDevice)
	if h.cfg.Bonds != nil {
		for _, rec := range h.cfg.Bonds.List() {
			byAddr[rec.Addr] = BluetoothDevice{
				Address: rec.Addr.String(),
				Name:    rec.Name,
				Kind:    rec.Kind,
			}
		}
	}
	for slotIdx := uint8(0); slotIdx < hidp.MaxPlayerSlots; slotIdx++ {
		ps, ok := h.bt.Player(slotIdx)
		if !ok {
			continue
		}
		dev := byAddr[ps.Addr]
		dev.Address = ps.Addr.String()
		if ps.Name != "" {
			dev.Name = ps.Name
		}
		if ps.Kind != controllers.KindUnknown {
			dev.Kind = ps.Kind
		}
		dev.Connected = true
		if port, ok := h.playerPortFor(playerPortBT, slotIdx); ok {
			dev.Slot = port + 1
		} else {
			dev.Slot = 0
		}
		byAddr[ps.Addr] = dev
	}
	if len(byAddr) > 0 {
		snap.Devices = make([]BluetoothDevice, 0, len(byAddr))
		for _, dev := range byAddr {
			if strings.TrimSpace(dev.Name) == "" {
				if dev.Kind != controllers.KindUnknown {
					dev.Name = dev.Kind.String()
				} else {
					dev.Name = "Bluetooth device"
				}
			}
			snap.Devices = append(snap.Devices, dev)
		}
		slices.SortFunc(snap.Devices, func(a, b BluetoothDevice) int {
			if a.Connected != b.Connected {
				if a.Connected {
					return -1
				}
				return 1
			}
			if a.Connected && a.Slot != b.Slot {
				if a.Slot == 0 {
					return 1
				}
				if b.Slot == 0 {
					return -1
				}
				if a.Slot < b.Slot {
					return -1
				}
				return 1
			}
			if cmp := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.Address, b.Address)
		})
	}
	return snap
}

// ForgetBluetoothDevice closes any active pairing window, deletes the stored
// bond for address, and disconnects any active connection to it.
func (h *Hub) ForgetBluetoothDevice(address string) error {
	if h == nil || h.bt == nil {
		return errors.New("usbhub: Bluetooth unavailable")
	}
	addr, err := l2cap.ParseBDAddr(address)
	if err != nil {
		return err
	}
	wasPairing := h.bt.PairingActive()
	found, err := h.bt.ForgetDevice(addr)
	if found {
		h.bondRetryAt = time.Time{}
		h.bondRetryDelay = 0
		h.bondSaveFailed = false
		h.btGeneration++
	} else if h.bt.PairingActive() != wasPairing {
		h.btGeneration++
	}
	if err != nil {
		return err
	}
	if !found {
		return errors.New("usbhub: Bluetooth device not found")
	}
	return nil
}

// SaveBondsIfDirty persists updated Bluetooth bonds when configured and
// clear-marks dirty on success. After a failed attempt it returns (false, nil)
// without writing until a doubling delay (1 s, capped at 1 min) has elapsed,
// so a refusing store reports each failure once per retry, not once per poll.
func (h *Hub) SaveBondsIfDirty(ctx context.Context) (bool, error) {
	if h == nil || h.cfg.Bonds == nil || !h.cfg.Bonds.Dirty() || h.cfg.PersistBonds == nil {
		return false, nil
	}
	now := h.cfg.Now()
	if !h.bondRetryAt.IsZero() && now.Before(h.bondRetryAt) {
		return false, nil
	}
	raw, err := h.cfg.Bonds.MarshalJSON()
	if err == nil {
		err = h.cfg.PersistBonds(ctx, raw)
	}
	if err != nil {
		h.bondRetryDelay = min(max(2*h.bondRetryDelay, bondRetryInitial), bondRetryMax)
		h.bondRetryAt = now.Add(h.bondRetryDelay)
		if !h.bondSaveFailed {
			h.bondSaveFailed = true
			h.btGeneration++
		}
		return false, err
	}
	h.cfg.Bonds.ClearDirty()
	h.bondRetryAt, h.bondRetryDelay = time.Time{}, 0
	h.bondSaveFailed = false
	h.btGeneration++
	return true, nil
}

// KeyboardNeutral reports whether the combined keyboard is known to be
// released: the bus has been enumerated, every wired keyboard has delivered a
// report (SET_IDLE makes idle keyboards repeat theirs) and the merged report is
// all-up. A keyboard that has not reported yet may be held, so it keeps this
// false, matching the single-keyboard rule that a missing report never arms.
func (h *Hub) KeyboardNeutral() bool {
	if h == nil {
		return true
	}
	if !h.initialized || h.mergedKeyboard != ([8]byte{}) {
		return false
	}
	for i := range h.wiredKeyboards {
		if h.wiredKeyboards[i].active && !h.wiredKeyboards[i].reported {
			return false
		}
	}
	return true
}

// GamepadNeutral reports whether the combined gamepad is known to be neutral:
// the bus has been enumerated, every wired gamepad has delivered a report, and
// the merged buttons, sticks, hats and Home are released.
func (h *Hub) GamepadNeutral() bool {
	if h == nil {
		return true
	}
	if !h.initialized || !input.GamepadNeutral(h.mergedGamepad) || h.mergedHome || h.mergedPorts != ([MaxPlayerPorts]uint8{}) {
		return false
	}
	for i := range h.wiredGamepads {
		if h.wiredGamepads[i].active && !h.wiredGamepads[i].reported {
			return false
		}
	}
	return true
}

// NextKeyboardReport pops the next merged 8-byte boot keyboard snapshot change, if any.
func (h *Hub) NextKeyboardReport() ([8]byte, bool) {
	if h == nil || h.kbCount == 0 {
		return [8]byte{}, false
	}
	report := h.kbQueue[h.kbHead]
	h.kbHead = (h.kbHead + 1) % maxQueuedReports
	h.kbCount--
	return report, true
}

// MouseNeutral reports whether the bus has been enumerated, every active wired
// boot mouse has delivered an initial report, and all wired + Bluetooth
// Left/Right mouse buttons are released.
func (h *Hub) MouseNeutral() bool {
	if h == nil {
		return true
	}
	if !h.initialized || h.mergedMouseButtons != 0 {
		return false
	}
	for i := range h.wiredMice {
		if h.wiredMice[i].active && !h.wiredMice[i].reported {
			return false
		}
	}
	return true
}

// NextMouseUpdate pops the next queued relative boot-mouse update, if any.
func (h *Hub) NextMouseUpdate() (MouseUpdate, bool) {
	if h == nil || h.mouseCount == 0 {
		return MouseUpdate{}, false
	}
	u := h.mouseQueue[h.mouseHead]
	h.mouseHead = (h.mouseHead + 1) % maxQueuedReports
	h.mouseCount--
	return u, true
}

// NextGamepadUpdate pops the next merged gamepad snapshot change, if any.
func (h *Hub) NextGamepadUpdate() (GamepadUpdate, bool) {
	if h == nil || h.padCount == 0 {
		return GamepadUpdate{State: usb.GamepadState{Hat: -1}}, false
	}
	u := h.padQueue[h.padHead]
	h.padHead = (h.padHead + 1) % maxQueuedReports
	h.padCount--
	return u, true
}

func (h *Hub) enqueueKeyboard(report [8]byte) {
	if h.kbCount == maxQueuedReports {
		h.kbHead = (h.kbHead + 1) % maxQueuedReports
		h.kbCount--
	}
	tail := (h.kbHead + h.kbCount) % maxQueuedReports
	h.kbQueue[tail] = report
	h.kbCount++
}

func (h *Hub) enqueueGamepad(u GamepadUpdate) {
	if h.padCount == maxQueuedReports {
		h.padHead = (h.padHead + 1) % maxQueuedReports
		h.padCount--
	}
	tail := (h.padHead + h.padCount) % maxQueuedReports
	h.padQueue[tail] = u
	h.padCount++
}

func (h *Hub) enqueueMouse(u MouseUpdate) {
	if h.mouseCount == maxQueuedReports {
		tailIdx := (h.mouseHead + h.mouseCount - 1) % maxQueuedReports
		tail := &h.mouseQueue[tailIdx]
		if tail.Buttons == u.Buttons {
			sumX := int(tail.DX) + int(u.DX)
			sumY := int(tail.DY) + int(u.DY)
			if sumX >= -128 && sumX <= 127 && sumY >= -128 && sumY <= 127 {
				tail.DX = int8(sumX)
				tail.DY = int8(sumY)
				return
			}
		}
		h.mouseHead = (h.mouseHead + 1) % maxQueuedReports
		h.mouseCount--
	}
	tail := (h.mouseHead + h.mouseCount) % maxQueuedReports
	h.mouseQueue[tail] = u
	h.mouseCount++
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
		h.btMouseButtons[i] = 0
	}
	for i := range h.btGamepads {
		h.btSlotActive[i] = false
		h.btGamepads[i] = usb.GamepadState{Hat: -1}
		h.btMasks[i] = 0
		h.btHome[i] = false
	}
	h.playerPorts = [MaxPlayerPorts]playerPortOwner{}
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

func (h *Hub) attachDevice(dev usb.BusDevice) {
	if dev.Address == 0 {
		return
	}
	for _, iface := range dev.Config.Interfaces {
		if iface.AlternateSetting != 0 {
			continue
		}
		if iface.Class == 0xff && iface.SubClass == 0x5d && iface.Protocol == 0x01 {
			h.attachWiredNativeController(dev, iface, controllers.KindXInput)
			continue
		}
		if iface.Number == 0 && iface.Class == 0xff && iface.SubClass == 0x47 && iface.Protocol == 0xd0 {
			h.attachWiredNativeController(dev, iface, controllers.KindXboxGIP)
			continue
		}
		if iface.Class != 3 {
			continue
		}
		if iface.SubClass == 1 && iface.Protocol == 1 {
			h.attachWiredKeyboard(dev.Address, iface)
			continue
		}
		if iface.SubClass == 1 && iface.Protocol == 2 {
			h.attachWiredMouse(dev.Address, iface)
			continue
		}
		if kind := controllers.IdentifyByVIDPID(dev.Descriptor.VendorID, dev.Descriptor.ProductID); kind != controllers.KindUnknown {
			h.attachWiredNativeController(dev, iface, kind)
			continue
		}
		h.attachWiredGamepad(dev.Address, iface)
	}
}

func (h *Hub) detachDevice(addr uint8) {
	if addr == 0 {
		return
	}
	for i := range h.wiredKeyboards {
		if h.wiredKeyboards[i].active && h.wiredKeyboards[i].addr == addr {
			h.closeWiredKeyboard(i)
		}
	}
	for i := range h.wiredMice {
		if h.wiredMice[i].active && h.wiredMice[i].addr == addr {
			h.closeWiredMouse(i)
		}
	}
	for i := range h.wiredGamepads {
		if h.wiredGamepads[i].active && h.wiredGamepads[i].addr == addr {
			h.closeWiredGamepad(i)
		}
	}
}

func (h *Hub) attachWiredKeyboard(addr uint8, iface usb.InterfaceDescriptor) {
	ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || ep.MaxPacketSize < 8 || ep.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free := -1
	for i := range h.wiredKeyboards {
		slot := &h.wiredKeyboards[i]
		if slot.active {
			if slot.addr == addr && slot.iface == iface.Number {
				return
			}
		} else if free < 0 {
			free = i
		}
	}
	if free < 0 {
		return
	}
	if _, err := h.bus.ControlTransfer(addr, 0x21, 0x0b, 0, uint16(iface.Number), nil); err != nil {
		return
	}
	// Request 100 ms HID idle snapshots where supported; devices that STALL
	// SET_IDLE still operate on state-change interrupt reports.
	_, _ = h.bus.ControlTransfer(addr, 0x21, 0x0a, 25<<8, uint16(iface.Number), nil)
	pipe, err := h.bus.OpenPipe(addr, ep)
	if err != nil {
		return
	}
	h.wiredKeyboards[free] = wiredKeyboardSlot{
		active: true,
		addr:   addr,
		iface:  iface.Number,
		pipe:   pipe,
	}
	h.emit(DeviceEvent{
		Kind:      EventKeyboardAttached,
		Address:   addr,
		Interface: iface.Number,
		Slot:      uint8(free),
	})
}

func (h *Hub) findFreeWiredGamepadSlot(addr, iface uint8) (int, bool) {
	free := -1
	for i := range h.wiredGamepads {
		slot := &h.wiredGamepads[i]
		if slot.active {
			if slot.addr == addr && slot.iface == iface {
				return -1, false
			}
		} else if free < 0 {
			free = i
		}
	}
	if free < 0 {
		return -1, false
	}
	return free, true
}

func (h *Hub) attachWiredGamepad(addr uint8, iface usb.InterfaceDescriptor) {
	if iface.HIDReportBytes == 0 || iface.HIDReportBytes > usb.MaxConfigurationBytes {
		return
	}
	ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || ep.MaxPacketSize == 0 || ep.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free, ok := h.findFreeWiredGamepadSlot(addr, iface.Number)
	if !ok {
		return
	}
	n, err := h.bus.ControlTransfer(addr, 0x81, 0x06, 0x2200, uint16(iface.Number), h.descBuf[:iface.HIDReportBytes])
	if err != nil || n != int(iface.HIDReportBytes) {
		return
	}
	decoder, err := usb.ParseGamepad(h.descBuf[:n])
	if err != nil || decoder.MaxReportBytes() > int(ep.MaxPacketSize) {
		return
	}
	pipe, err := h.bus.OpenPipe(addr, ep)
	if err != nil {
		return
	}
	h.wiredGamepads[free] = wiredGamepadSlot{
		active:  true,
		addr:    addr,
		iface:   iface.Number,
		kind:    controllers.KindGenericHID,
		pipe:    pipe,
		decoder: decoder,
		state:   usb.GamepadState{Hat: -1},
	}
	h.assignPlayerPort(playerPortWired, uint8(free))
	h.emit(DeviceEvent{
		Kind:      EventGamepadAttached,
		Address:   addr,
		Interface: iface.Number,
		Slot:      uint8(free),
		Name:      controllers.KindGenericHID.String(),
	})
}

func xboxGIPNeedsSInit(vid, pid uint16) bool {
	return vid == 0x045e && (pid == 0x02ea || pid == 0x0b00)
}

func (h *Hub) attachWiredNativeController(dev usb.BusDevice, iface usb.InterfaceDescriptor, kind controllers.Kind) {
	inEP, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || inEP.MaxPacketSize == 0 || inEP.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free, ok := h.findFreeWiredGamepadSlot(dev.Address, iface.Number)
	if !ok {
		return
	}
	inPipe, err := h.bus.OpenPipe(dev.Address, inEP)
	if err != nil {
		return
	}
	var outPipe usb.Pipe
	if outEP, ok := iface.FindEndpoint(usb.TransferInterrupt, false); ok && outEP.MaxPacketSize > 0 && outEP.MaxPacketSize <= usb.MaxHIDReportBytes {
		outPipe, err = h.bus.OpenPipe(dev.Address, outEP)
		if err != nil {
			_ = inPipe.Close()
			return
		}
	}
	initStep := uint8(0)
	if kind == controllers.KindXboxGIP && !xboxGIPNeedsSInit(dev.Descriptor.VendorID, dev.Descriptor.ProductID) {
		initStep = 1
	}
	h.wiredGamepads[free] = wiredGamepadSlot{
		active:    true,
		addr:      dev.Address,
		iface:     iface.Number,
		vendorID:  dev.Descriptor.VendorID,
		productID: dev.Descriptor.ProductID,
		kind:      kind,
		pipe:      inPipe,
		outPipe:   outPipe,
		initStep:  initStep,
		state:     usb.GamepadState{Hat: -1},
	}
	h.assignPlayerPort(playerPortWired, uint8(free))
	h.emit(DeviceEvent{
		Kind:      EventGamepadAttached,
		Address:   dev.Address,
		Interface: iface.Number,
		Slot:      uint8(free),
		Name:      kind.String(),
	})
}

func (h *Hub) writeSlotPacket(slotIdx int, pkt []byte) bool {
	slot := &h.wiredGamepads[slotIdx]
	if !slot.active || slot.outPipe == nil || slot.outPending || len(pkt) == 0 {
		return false
	}
	err := slot.outPipe.Write(pkt)
	if err == nil {
		slot.outPending = true
		return true
	}
	if errors.Is(err, usbcontrol.ErrTransition) || errors.Is(err, usbcontrol.ErrBusy) {
		return false
	}
	h.closeWiredGamepad(slotIdx)
	return false
}

func (h *Hub) enqueueGIPAck(slot *wiredGamepadSlot, seq, cmdID, payloadLen uint8) {
	if int(slot.gipAckCount) == len(slot.gipAckQueue) {
		// Service stops polling when full, so this is only a defensive guard.
		// Never overwrite an older request whose ACK is still required.
		return
	}
	tail := (slot.gipAckHead + slot.gipAckCount) % uint8(len(slot.gipAckQueue))
	slot.gipAckQueue[tail] = gipAckRequest{seq: seq, cmdID: cmdID, payloadLen: payloadLen}
	slot.gipAckCount++
}

func (h *Hub) serviceWiredInit(slotIdx int) (paused bool) {
	slot := &h.wiredGamepads[slotIdx]
	if !slot.active || slot.outPipe == nil {
		return false
	}
	if slot.outPending {
		done, err := slot.outPipe.WriteDone()
		if err != nil {
			if errors.Is(err, usbcontrol.ErrTransition) || errors.Is(err, usbcontrol.ErrBusy) {
				return true
			}
			h.closeWiredGamepad(slotIdx)
			return false
		}
		if !done {
			return false
		}
		slot.outPending = false
	}
	if slot.gipAckCount > 0 {
		ack := slot.gipAckQueue[slot.gipAckHead]
		n := controllers.BuildXboxGIPAck(h.txBuf[:], ack.seq, ack.cmdID, ack.payloadLen)
		if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
			slot.gipAckHead = (slot.gipAckHead + 1) % uint8(len(slot.gipAckQueue))
			slot.gipAckCount--
		}
		return false
	}
	now := h.cfg.Now()
	if !slot.initNextTry.IsZero() && now.Before(slot.initNextTry) {
		return false
	}
	switch slot.kind {
	case controllers.KindXInput:
		if slot.ledDirty && slot.ledAssigned {
			n := controllers.BuildXInputLED(h.txBuf[:], slot.ledPort)
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.ledDirty = false
			}
		}
	case controllers.KindXboxGIP:
		switch slot.initStep {
		case 0:
			n := controllers.BuildXboxGIPSInit(h.txBuf[:], slot.txSeq)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 1
			}
		case 1:
			n := controllers.BuildXboxGIPPowerOn(h.txBuf[:], slot.txSeq)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 2
			}
		case 2:
			n := controllers.BuildXboxGIPLedOn(h.txBuf[:], slot.txSeq)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 3
			}
		case 3:
			n := controllers.BuildXboxGIPAuthComplete(h.txBuf[:], slot.txSeq)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 4
			}
		}
	case controllers.KindDualSense:
		if slot.ledDirty && slot.ledAssigned {
			n := controllers.BuildDualSenseUSBOutput(h.txBuf[:], slot.ledPort, slot.rumbleRight, slot.rumbleLeft)
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.ledDirty = false
			}
		}
	case controllers.KindDualShock4:
		if slot.ledDirty && slot.ledAssigned {
			n := controllers.BuildDualShock4USBOutput(h.txBuf[:], slot.ledPort, slot.rumbleRight, slot.rumbleLeft)
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.ledDirty = false
			}
		}
	case controllers.KindSwitchPro:
		switch slot.initStep {
		case 0:
			if slot.initAttempts >= 10 {
				slot.initStep = 1
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
				return false
			}
			n := controllers.BuildSwitchProUSBHandshake(h.txBuf[:])
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initAttempts++
				slot.initNextTry = now.Add(100 * time.Millisecond)
			}
		case 1:
			n := controllers.BuildSwitchProUSBEnableHID(h.txBuf[:])
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 2
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
			}
		case 2:
			if slot.switchCal.Valid || slot.initAttempts >= 5 {
				slot.initStep = 3
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
				return false
			}
			n := controllers.BuildSwitchProReadCalibration(h.txBuf[:], slot.txSeq&0x0f)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initAttempts++
				slot.initNextTry = now.Add(100 * time.Millisecond)
			}
		case 3:
			n := controllers.BuildSwitchProSetFullMode(h.txBuf[:], slot.txSeq&0x0f)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 4
			}
		default:
			if slot.ledDirty && slot.ledAssigned {
				n := controllers.BuildSwitchProSetPlayerLEDs(h.txBuf[:], slot.txSeq&0x0f, slot.ledPort)
				slot.txSeq++
				if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
					slot.ledDirty = false
				}
			}
		}
	}
	return false
}

func (h *Hub) feedWiredGamepadPacket(slotIdx int, pkt []byte) {
	slot := &h.wiredGamepads[slotIdx]
	if slot.kind == controllers.KindGenericHID {
		state, changed, feedErr := slot.decoder.Feed(pkt)
		if feedErr == nil {
			slot.reported = true
			if changed {
				slot.state = state
				slot.ctrlMask = usbGamepadToComputerMask(state)
				h.recomputeGamepad()
			}
		}
		return
	}

	var updated bool
	switch slot.kind {
	case controllers.KindXInput:
		updated = controllers.ParseXInput(pkt, &slot.ctrlState)
	case controllers.KindXboxGIP:
		var ackSeq uint8
		var needsAck bool
		updated, ackSeq, needsAck = controllers.ParseXboxGIPInput(pkt, &slot.ctrlState)
		if updated && needsAck && slot.outPipe != nil {
			if slot.outPending || slot.gipAckCount > 0 {
				h.enqueueGIPAck(slot, ackSeq, pkt[0], pkt[3])
			} else {
				n := controllers.BuildXboxGIPAck(h.txBuf[:], ackSeq, pkt[0], pkt[3])
				if !h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
					if !slot.active {
						return
					}
					h.enqueueGIPAck(slot, ackSeq, pkt[0], pkt[3])
				}
			}
		}
	case controllers.KindDualSense:
		updated = controllers.ParseDualSenseUSB(pkt, &slot.ctrlState)
	case controllers.KindDualShock4:
		updated = controllers.ParseDualShock4USB(pkt, &slot.ctrlState)
	case controllers.KindSwitchPro:
		if len(pkt) >= 2 && pkt[0] == 0x81 && pkt[1] == 0x02 {
			if slot.initStep == 0 {
				slot.initStep = 1
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
			}
			return
		}
		if controllers.ParseSwitchProSPICalibration(pkt, &slot.switchCal) && slot.initStep == 2 {
			slot.initStep = 3
			slot.initAttempts = 0
			slot.initNextTry = time.Time{}
		}
		updated = controllers.ParseSwitchProInput(pkt, &slot.switchCal, &slot.ctrlState)
	}
	if !updated {
		return
	}
	slot.reported = true
	nextState := slot.ctrlState.ToUSBGamepadState()
	nextMask := slot.ctrlState.ToComputerMask()
	nextHome := slot.ctrlState.Buttons&controllers.ButtonHome != 0
	if nextState != slot.state || nextMask != slot.ctrlMask || nextHome != slot.home {
		slot.state = nextState
		slot.ctrlMask = nextMask
		slot.home = nextHome
		h.recomputeGamepad()
	}
}

// SetWiredRumble sends a rumble/LED packet to a wired native controller slot (0..3).
func (h *Hub) SetWiredRumble(slotIdx uint8, leftHeavy, rightLight uint8) error {
	if h == nil || slotIdx >= maxWiredGamepads {
		return errors.New("usbhub: invalid wired gamepad slot")
	}
	slot := &h.wiredGamepads[slotIdx]
	if !slot.active || slot.outPipe == nil {
		return errors.New("usbhub: wired controller output unavailable")
	}
	if slot.outPending {
		done, err := slot.outPipe.WriteDone()
		if err != nil {
			return err
		}
		if !done {
			return errors.New("usbhub: wired output pipe busy")
		}
		slot.outPending = false
	}
	if slot.gipAckCount > 0 {
		return errors.New("usbhub: wired output pipe busy")
	}
	var n int
	clearLEDDirty := false
	switch slot.kind {
	case controllers.KindXInput:
		n = controllers.BuildXInputRumble(h.txBuf[:], leftHeavy, rightLight)
	case controllers.KindXboxGIP:
		n = controllers.BuildXboxGIPRumble(h.txBuf[:], slot.txSeq, leftHeavy, rightLight)
		slot.txSeq++
	case controllers.KindDualSense:
		if slot.ledAssigned {
			n = controllers.BuildDualSenseUSBOutput(h.txBuf[:], slot.ledPort, rightLight, leftHeavy)
			clearLEDDirty = true
		} else {
			n = controllers.BuildDualSenseUSBRumble(h.txBuf[:], rightLight, leftHeavy)
		}
	case controllers.KindDualShock4:
		if slot.ledAssigned {
			n = controllers.BuildDualShock4USBOutput(h.txBuf[:], slot.ledPort, rightLight, leftHeavy)
			clearLEDDirty = true
		} else {
			n = controllers.BuildDualShock4USBRumble(h.txBuf[:], rightLight, leftHeavy)
		}
	default:
		return errors.New("usbhub: rumble unsupported on this wired device")
	}
	if n == 0 {
		return errors.New("usbhub: failed to build rumble packet")
	}
	if err := slot.outPipe.Write(h.txBuf[:n]); err != nil {
		return err
	}
	slot.rumbleLeft = leftHeavy
	slot.rumbleRight = rightLight
	if clearLEDDirty {
		slot.ledDirty = false
	}
	slot.outPending = true
	return nil
}

func (h *Hub) closeWiredKeyboard(idx int) {
	slot := &h.wiredKeyboards[idx]
	if !slot.active {
		return
	}
	addr, iface := slot.addr, slot.iface
	if slot.pipe != nil {
		_ = slot.pipe.Close()
	}
	*slot = wiredKeyboardSlot{}
	h.recomputeKeyboard()
	h.emit(DeviceEvent{
		Kind:      EventKeyboardDetached,
		Address:   addr,
		Interface: iface,
		Slot:      uint8(idx),
	})
}

func (h *Hub) attachWiredMouse(addr uint8, iface usb.InterfaceDescriptor) {
	ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || ep.MaxPacketSize < 3 || ep.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free := -1
	for i := range h.wiredMice {
		slot := &h.wiredMice[i]
		if slot.active {
			if slot.addr == addr && slot.iface == iface.Number {
				return
			}
		} else if free < 0 {
			free = i
		}
	}
	if free < 0 {
		return
	}
	if _, err := h.bus.ControlTransfer(addr, 0x21, 0x0b, 0, uint16(iface.Number), nil); err != nil {
		return
	}
	// Request 100 ms HID idle snapshots where supported so idle mice confirm
	// initial button state cleanly without blocking the neutral fence.
	_, _ = h.bus.ControlTransfer(addr, 0x21, 0x0a, 25<<8, uint16(iface.Number), nil)
	pipe, err := h.bus.OpenPipe(addr, ep)
	if err != nil {
		return
	}
	h.wiredMice[free] = wiredMouseSlot{
		active: true,
		addr:   addr,
		iface:  iface.Number,
		pipe:   pipe,
	}
	h.emit(DeviceEvent{
		Kind:      EventMouseAttached,
		Address:   addr,
		Interface: iface.Number,
		Slot:      uint8(free),
	})
}

func (h *Hub) closeWiredMouse(idx int) {
	slot := &h.wiredMice[idx]
	if !slot.active {
		return
	}
	addr, iface := slot.addr, slot.iface
	if slot.pipe != nil {
		_ = slot.pipe.Close()
	}
	*slot = wiredMouseSlot{}
	merged := h.computeMouseButtons()
	if merged != h.mergedMouseButtons {
		h.mergedMouseButtons = merged
		h.enqueueMouse(MouseUpdate{Buttons: merged})
	}
	h.emit(DeviceEvent{
		Kind:      EventMouseDetached,
		Address:   addr,
		Interface: iface,
		Slot:      uint8(idx),
	})
}

func (h *Hub) computeMouseButtons() uint8 {
	var merged uint8
	for i := range h.wiredMice {
		if h.wiredMice[i].active {
			merged |= h.wiredMice[i].buttons
		}
	}
	for i := range h.btMouseButtons {
		merged |= h.btMouseButtons[i]
	}
	return merged & 0x03
}

func (h *Hub) closeWiredGamepad(idx int) {
	slot := &h.wiredGamepads[idx]
	if !slot.active {
		return
	}
	addr, iface, kind := slot.addr, slot.iface, slot.kind
	if slot.pipe != nil {
		_ = slot.pipe.Close()
	}
	if slot.outPipe != nil {
		_ = slot.outPipe.Close()
	}
	*slot = wiredGamepadSlot{state: usb.GamepadState{Hat: -1}}
	h.releasePlayerPort(playerPortWired, uint8(idx))
	h.recomputeGamepad()
	h.emit(DeviceEvent{
		Kind:      EventGamepadDetached,
		Address:   addr,
		Interface: iface,
		Slot:      uint8(idx),
		Name:      kind.String(),
	})
}

func (h *Hub) recomputeKeyboard() {
	var next [8]byte
	keyCount := 0
	addReport := func(rep [8]byte) {
		next[0] |= rep[0]
		for _, usage := range rep[2:] {
			if usage < 4 || usage > 0xdf {
				continue
			}
			dup := false
			for j := 0; j < keyCount; j++ {
				if next[2+j] == usage {
					dup = true
					break
				}
			}
			if !dup && keyCount < 6 {
				next[2+keyCount] = usage
				keyCount++
			}
		}
	}
	for i := range h.wiredKeyboards {
		if h.wiredKeyboards[i].active {
			addReport(h.wiredKeyboards[i].report)
		}
	}
	for i := range h.btKeyboards {
		addReport(h.btKeyboards[i])
	}
	if next != h.mergedKeyboard {
		h.mergedKeyboard = next
		h.enqueueKeyboard(next)
	}
}

func (h *Hub) recomputeGamepad() {
	merged := usb.GamepadState{Hat: -1}
	var dirs [4]bool
	var home bool
	haveActive := false
	mergeState := func(st usb.GamepadState) {
		merged.Buttons |= st.Buttons
		d := input.GamepadDirections(st)
		for i := range dirs {
			if d[i] {
				dirs[i] = true
			}
		}
		for i := 0; i < 4; i++ {
			if absI16(st.Axes[i]) > absI16(merged.Axes[i]) {
				merged.Axes[i] = st.Axes[i]
			}
		}
		for i := 4; i < 6; i++ {
			if !haveActive || st.Axes[i] > merged.Axes[i] {
				merged.Axes[i] = st.Axes[i]
			}
		}
		haveActive = true
	}
	for i := range h.wiredGamepads {
		if h.wiredGamepads[i].active {
			mergeState(h.wiredGamepads[i].state)
			if h.wiredGamepads[i].home {
				home = true
			}
		}
	}
	for i := range h.btGamepads {
		if h.btSlotActive[i] {
			mergeState(h.btGamepads[i])
			if h.btHome[i] {
				home = true
			}
		}
	}
	merged.Hat = directionsToHat(dirs)
	var ports [MaxPlayerPorts]uint8
	for p, owner := range h.playerPorts {
		switch owner.source {
		case playerPortWired:
			if int(owner.slot) < len(h.wiredGamepads) && h.wiredGamepads[owner.slot].active {
				ports[p] = h.wiredGamepads[owner.slot].ctrlMask
			}
		case playerPortBT:
			if int(owner.slot) < len(h.btMasks) && h.btSlotActive[owner.slot] {
				ports[p] = h.btMasks[owner.slot]
			}
		}
	}
	if merged != h.mergedGamepad || home != h.mergedHome || ports != h.mergedPorts {
		h.mergedGamepad = merged
		h.mergedHome = home
		h.mergedPorts = ports
		h.enqueueGamepad(GamepadUpdate{State: merged, HomePressed: home, Ports: ports})
	}
}

func wiredKindHasPlayerLED(kind controllers.Kind) bool {
	switch kind {
	case controllers.KindXInput, controllers.KindDualSense, controllers.KindDualShock4, controllers.KindSwitchPro:
		return true
	default:
		return false
	}
}

func (h *Hub) syncPlayerPortLED(source playerPortSource, slot uint8, port uint8) {
	switch source {
	case playerPortWired:
		if int(slot) >= len(h.wiredGamepads) {
			return
		}
		w := &h.wiredGamepads[slot]
		if !w.active {
			return
		}
		if !w.ledAssigned || w.ledPort != port {
			w.ledAssigned = true
			w.ledPort = port
			w.ledDirty = wiredKindHasPlayerLED(w.kind)
		}
	case playerPortBT:
		if h.bt != nil {
			_ = h.bt.SetPlayerPort(slot, port, true)
		}
	}
}

func (h *Hub) assignPlayerPort(source playerPortSource, slot uint8) int {
	for p, owner := range h.playerPorts {
		if owner.source == source && owner.slot == slot {
			return p
		}
	}
	for p, owner := range h.playerPorts {
		if owner.source == playerPortNone {
			h.playerPorts[p] = playerPortOwner{source: source, slot: slot}
			h.syncPlayerPortLED(source, slot, uint8(p))
			return p
		}
	}
	return -1
}

func (h *Hub) releasePlayerPort(source playerPortSource, slot uint8) {
	released := false
	for p, owner := range h.playerPorts {
		if owner.source == source && owner.slot == slot {
			h.playerPorts[p] = playerPortOwner{}
			released = true
		}
	}
	if released {
		h.promoteWaitingPlayerPorts()
	}
}

func (h *Hub) promoteWaitingPlayerPorts() {
	for i := range h.wiredGamepads {
		if h.wiredGamepads[i].active && h.assignPlayerPort(playerPortWired, uint8(i)) < 0 {
			return
		}
	}
	for i := range h.btSlotActive {
		if !h.btSlotActive[i] {
			continue
		}
		if _, already := h.playerPortFor(playerPortBT, uint8(i)); already {
			continue
		}
		if h.assignPlayerPort(playerPortBT, uint8(i)) < 0 {
			return
		}
		h.btGeneration++
	}
}

func (h *Hub) playerPortFor(source playerPortSource, slot uint8) (uint8, bool) {
	for p, owner := range h.playerPorts {
		if owner.source == source && owner.slot == slot {
			return uint8(p), true
		}
	}
	return 0, false
}

func usbGamepadToComputerMask(st usb.GamepadState) uint8 {
	var mask uint8
	dirs := input.GamepadDirections(st)
	for i, down := range dirs {
		if down {
			mask |= 1 << uint(i)
		}
	}
	if st.Buttons&(1<<0) != 0 {
		mask |= 1 << 4
	}
	if st.Buttons&(1<<1) != 0 {
		mask |= 1 << 5
	}
	if st.Buttons&(1<<8) != 0 {
		mask |= 1 << 6
	}
	if st.Buttons&(1<<9) != 0 {
		mask |= 1 << 7
	}
	return mask
}

func directionsToHat(dirs [4]bool) int8 {
	up := dirs[0] && !dirs[1]
	down := dirs[1] && !dirs[0]
	left := dirs[2] && !dirs[3]
	right := dirs[3] && !dirs[2]
	switch {
	case up && right:
		return 1
	case down && right:
		return 3
	case down && left:
		return 5
	case up && left:
		return 7
	case up:
		return 0
	case right:
		return 2
	case down:
		return 4
	case left:
		return 6
	default:
		return -1
	}
}

func absI16(v int16) int32 {
	if v < 0 {
		return -int32(v)
	}
	return int32(v)
}

func validBootKeyboardReport(report []byte) bool {
	if len(report) != 8 {
		return false
	}
	for i, usage := range report[2:] {
		if usage == 0 {
			continue
		}
		if usage <= 3 || (usage >= 0xe0 && usage <= 0xe7) {
			return false
		}
		for j := 0; j < i; j++ {
			if report[2+j] == usage {
				return false
			}
		}
	}
	return true
}
