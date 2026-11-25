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

	// A failed bond write is retried after a doubling delay, so callers may
	// poll SaveBondsIfDirty every frame without hammering storage.
	bondRetryInitial = time.Second
	bondRetryMax     = time.Minute
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
	home         bool
	switchCal    controllers.SwitchStickCal
	initStep     uint8
	initNextTry  time.Time
	initAttempts uint8
	txSeq        uint8
	outPending   bool
	gipAckQueue  [4]gipAckRequest
	gipAckHead   uint8
	gipAckCount  uint8
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

	btKeyboards  [hidp.MaxPlayerSlots][8]byte
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
	txBuf       [usb.MaxHIDReportBytes]byte
	initialized bool

	bondRetryAt    time.Time
	bondRetryDelay time.Duration
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
				if kind == controllers.KindUnknown && h.btKeyboards[slot] != ([8]byte{}) {
					h.btKeyboards[slot] = [8]byte{}
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
		return false, err
	}
	h.cfg.Bonds.ClearDirty()
	h.bondRetryAt, h.bondRetryDelay = time.Time{}, 0
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
	if !h.initialized || !input.GamepadNeutral(h.mergedGamepad) || h.mergedHome {
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
	for i := range h.btKeyboards {
		h.btKeyboards[i] = [8]byte{}
	}
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
	return h.bt.ServiceSession()
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
			continue // Boot mouse is out of scope for Tenfoot/core input.
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
		slot.gipAckHead = (slot.gipAckHead + 1) % uint8(len(slot.gipAckQueue))
		slot.gipAckCount--
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
		if slot.initStep == 0 {
			n := controllers.BuildXInputLED(h.txBuf[:], uint8(slotIdx))
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 1
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
		if slot.initStep == 0 {
			n := controllers.BuildDualSenseUSBOutput(h.txBuf[:], uint8(slotIdx), 0, 0)
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 1
			}
		}
	case controllers.KindDualShock4:
		if slot.initStep == 0 {
			n := controllers.BuildDualShock4USBOutput(h.txBuf[:], uint8(slotIdx), 0, 0)
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 1
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
		case 4:
			n := controllers.BuildSwitchProSetPlayerLEDs(h.txBuf[:], slot.txSeq&0x0f, uint8(slotIdx))
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 5
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
	nextHome := slot.ctrlState.Buttons&controllers.ButtonHome != 0
	if nextState != slot.state || nextHome != slot.home {
		slot.state = nextState
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
	switch slot.kind {
	case controllers.KindXInput:
		n = controllers.BuildXInputRumble(h.txBuf[:], leftHeavy, rightLight)
	case controllers.KindXboxGIP:
		n = controllers.BuildXboxGIPRumble(h.txBuf[:], slot.txSeq, leftHeavy, rightLight)
		slot.txSeq++
	case controllers.KindDualSense:
		n = controllers.BuildDualSenseUSBOutput(h.txBuf[:], slotIdx, rightLight, leftHeavy)
	case controllers.KindDualShock4:
		n = controllers.BuildDualShock4USBOutput(h.txBuf[:], slotIdx, rightLight, leftHeavy)
	default:
		return errors.New("usbhub: rumble unsupported on this wired device")
	}
	if n == 0 {
		return errors.New("usbhub: failed to build rumble packet")
	}
	if err := slot.outPipe.Write(h.txBuf[:n]); err != nil {
		return err
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
