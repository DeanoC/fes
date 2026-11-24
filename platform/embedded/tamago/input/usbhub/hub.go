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
	maxWiredKeyboards = 4
	maxWiredGamepads  = 4
	maxQueuedReports  = 16
	maxPollsPerSlot   = 4
)

// GamepadUpdate carries a normalized merged gamepad snapshot plus whether a
// Classic Bluetooth controller's Home/PS/Guide button is currently held.
type GamepadUpdate struct {
	State       usb.GamepadState
	HomePressed bool
}

// DeviceEventKind classifies hotplug topology and player transitions emitted by Hub.
type DeviceEventKind uint8

const (
	EventKeyboardAttached DeviceEventKind = iota + 1
	EventKeyboardDetached
	EventGamepadAttached
	EventGamepadDetached
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
	addr   uint8
	iface  uint8
	pipe   usb.Pipe
	report [8]byte
}

type wiredGamepadSlot struct {
	active  bool
	addr    uint8
	iface   uint8
	pipe    usb.Pipe
	decoder *usb.Gamepad
	state   usb.GamepadState
}

// Hub supervises wired USB boot keyboards, wired USB HID gamepads, and one
// Classic Bluetooth dongle on a shared usb.BusHost with zero-device boot and
// live hotplug neutralization.
type Hub struct {
	bus usb.BusHost
	bt  *bt.Manager
	cfg Config

	wiredKeyboards [maxWiredKeyboards]wiredKeyboardSlot
	wiredGamepads  [maxWiredGamepads]wiredGamepadSlot

	btKeyboard   [8]byte
	btSlotActive [hidp.MaxPlayerSlots]bool
	btGamepads   [hidp.MaxPlayerSlots]usb.GamepadState
	btHome       [hidp.MaxPlayerSlots]bool

	mergedKeyboard [8]byte
	mergedGamepad  usb.GamepadState
	mergedHome     bool

	kbQueue  [maxQueuedReports][8]byte
	kbHead   int
	kbCount  int
	padQueue [maxQueuedReports]GamepadUpdate
	padHead  int
	padCount int

	pollBuf     [usb.MaxHIDReportBytes]byte
	descBuf     [usb.MaxConfigurationBytes]byte
	initialized bool
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
	}
	for i := range h.wiredGamepads {
		h.wiredGamepads[i].state = usb.GamepadState{Hat: -1}
	}
	for i := range h.btGamepads {
		h.btGamepads[i] = usb.GamepadState{Hat: -1}
	}
	h.bt = bt.NewManager(bus, cfg.Bonds, cfg.Firmware, cfg.Now)
	h.bt.OnKeyboardReport = func(report []byte) {
		if !validBootKeyboardReport(report) {
			return
		}
		var next [8]byte
		copy(next[:], report)
		if next == h.btKeyboard {
			return
		}
		h.btKeyboard = next
		h.recomputeKeyboard()
	}
	h.bt.OnGamepadState = func(slot uint8, _ controllers.Kind, state controllers.State) {
		if slot >= hidp.MaxPlayerSlots {
			return
		}
		h.btSlotActive[slot] = true
		h.btGamepads[slot] = state.ToUSBGamepadState()
		h.btHome[slot] = state.Buttons&controllers.ButtonHome != 0
		h.recomputeGamepad()
	}
	h.bt.OnSlotChanged = func(slot uint8, connected bool, kind controllers.Kind, name string) {
		if slot < hidp.MaxPlayerSlots {
			if connected && kind != controllers.KindUnknown {
				h.btSlotActive[slot] = true
			} else if !connected {
				h.btSlotActive[slot] = false
				h.btGamepads[slot] = usb.GamepadState{Hat: -1}
				h.btHome[slot] = false
				h.recomputeGamepad()
				if kind == controllers.KindUnknown && h.btKeyboard != ([8]byte{}) {
					h.btKeyboard = [8]byte{}
					h.recomputeKeyboard()
				}
			}
		}
		evKind := EventBTPlayerDetached
		if connected {
			evKind = EventBTPlayerAttached
		}
		h.emit(DeviceEvent{Kind: evKind, Slot: slot, Name: name})
	}
	h.bt.OnDongleChanged = func(connected bool, spec hci.DongleSpec) {
		evKind := EventBTDongleDetached
		if connected {
			evKind = EventBTDongleAttached
		}
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
// intact so held inputs still trip release fences.
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
	return h.bt.StartPairing(duration)
}

// StopPairing closes any open Bluetooth pairing window.
func (h *Hub) StopPairing() error {
	if h == nil || h.bt == nil {
		return nil
	}
	return h.bt.StopPairing()
}

// PairingActive reports whether a Bluetooth pairing window is open.
func (h *Hub) PairingActive() bool {
	return h != nil && h.bt != nil && h.bt.PairingActive()
}

// SaveBondsIfDirty persists updated Bluetooth bonds when configured and clear-marks dirty on success.
func (h *Hub) SaveBondsIfDirty(ctx context.Context) (bool, error) {
	if h == nil || h.cfg.Bonds == nil || !h.cfg.Bonds.Dirty() || h.cfg.PersistBonds == nil {
		return false, nil
	}
	raw, err := h.cfg.Bonds.MarshalJSON()
	if err != nil {
		return false, err
	}
	if err := h.cfg.PersistBonds(ctx, raw); err != nil {
		return false, err
	}
	h.cfg.Bonds.ClearDirty()
	return true, nil
}

// KeyboardNeutral reports whether all combined keyboard state is currently released.
func (h *Hub) KeyboardNeutral() bool {
	return h == nil || h.mergedKeyboard == ([8]byte{})
}

// GamepadNeutral reports whether all combined gamepad buttons, sticks, hats, and Home are neutral.
func (h *Hub) GamepadNeutral() bool {
	return h == nil || (input.GamepadNeutral(h.mergedGamepad) && !h.mergedHome)
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

// Close closes every open wired HID pipe and resets merged state to neutral.
// It intentionally leaves the shared usb.BusHost lease open for the caller.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	for i := range h.wiredKeyboards {
		h.closeWiredKeyboard(i)
	}
	for i := range h.wiredGamepads {
		h.closeWiredGamepad(i)
	}
	h.btKeyboard = [8]byte{}
	for i := range h.btGamepads {
		h.btSlotActive[i] = false
		h.btGamepads[i] = usb.GamepadState{Hat: -1}
		h.btHome[i] = false
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
				var snap [8]byte
				copy(snap[:], h.pollBuf[:8])
				if snap != h.wiredKeyboards[i].report {
					h.wiredKeyboards[i].report = snap
					h.recomputeKeyboard()
				}
			}
		}
	}
	for i := range h.wiredGamepads {
		if !h.wiredGamepads[i].active {
			continue
		}
		for p := 0; p < maxPollsPerSlot; p++ {
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
			state, changed, feedErr := h.wiredGamepads[i].decoder.Feed(h.pollBuf[:n])
			if feedErr == nil && changed {
				h.wiredGamepads[i].state = state
				h.recomputeGamepad()
			}
		}
	}
	return h.bt.ServiceSession()
}

func (h *Hub) attachDevice(dev usb.BusDevice) {
	if dev.Address == 0 {
		return
	}
	for _, iface := range dev.Config.Interfaces {
		if iface.AlternateSetting != 0 || iface.Class != 3 {
			continue
		}
		if iface.SubClass == 1 && iface.Protocol == 1 {
			h.attachWiredKeyboard(dev.Address, iface)
			continue
		}
		if iface.SubClass == 1 && iface.Protocol == 2 {
			continue // Boot mouse is out of scope for Tenfoot/core input.
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

func (h *Hub) attachWiredGamepad(addr uint8, iface usb.InterfaceDescriptor) {
	if iface.HIDReportBytes == 0 || iface.HIDReportBytes > usb.MaxConfigurationBytes {
		return
	}
	ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || ep.MaxPacketSize == 0 || ep.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free := -1
	for i := range h.wiredGamepads {
		slot := &h.wiredGamepads[i]
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
		pipe:    pipe,
		decoder: decoder,
		state:   usb.GamepadState{Hat: -1},
	}
	h.emit(DeviceEvent{
		Kind:      EventGamepadAttached,
		Address:   addr,
		Interface: iface.Number,
		Slot:      uint8(free),
	})
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

func (h *Hub) closeWiredGamepad(idx int) {
	slot := &h.wiredGamepads[idx]
	if !slot.active {
		return
	}
	addr, iface := slot.addr, slot.iface
	if slot.pipe != nil {
		_ = slot.pipe.Close()
	}
	*slot = wiredGamepadSlot{state: usb.GamepadState{Hat: -1}}
	h.recomputeGamepad()
	h.emit(DeviceEvent{
		Kind:      EventGamepadDetached,
		Address:   addr,
		Interface: iface,
		Slot:      uint8(idx),
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
	addReport(h.btKeyboard)
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
	if merged != h.mergedGamepad || home != h.mergedHome {
		h.mergedGamepad = merged
		h.mergedHome = home
		h.enqueueGamepad(GamepadUpdate{State: merged, HomePressed: home})
	}
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
