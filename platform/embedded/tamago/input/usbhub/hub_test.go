// SPDX-License-Identifier: GPL-3.0-or-later
package usbhub

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

type fakePipe struct {
	addr    uint8
	ep      usb.EndpointDescriptor
	inQueue [][]byte
	pollErr error
	closed  bool
}

func (p *fakePipe) DeviceAddress() uint8              { return p.addr }
func (p *fakePipe) Endpoint() usb.EndpointDescriptor  { return p.ep }
func (p *fakePipe) SetIntervalOverride(time.Duration) {}
func (p *fakePipe) Close() error                      { p.closed = true; return nil }
func (p *fakePipe) WriteDone() (bool, error)          { return true, nil }
func (p *fakePipe) Write([]byte) error                { return nil }
func (p *fakePipe) PollInto(dst []byte) (int, error) {
	if p.pollErr != nil {
		return 0, p.pollErr
	}
	if len(p.inQueue) == 0 {
		return 0, nil
	}
	pkt := p.inQueue[0]
	p.inQueue = p.inQueue[1:]
	return copy(dst, pkt), nil
}

type fakeBus struct {
	serviceErr   error
	events       []usb.HostEvent
	devices      []usb.BusDevice
	reportDescs  map[uint32][]byte
	pipes        map[uint32]*fakePipe
	btEvPipe     *fakePipe
	btACLInPipe  *fakePipe
	btACLOutPipe *fakePipe
	ready        chan struct{}
	paused       bool
	pauseCalls   int
	resumeCalls  int
	lmpSub       uint16
}

func newFakeBus() *fakeBus {
	return &fakeBus{
		reportDescs: make(map[uint32][]byte),
		pipes:       make(map[uint32]*fakePipe),
		ready:       make(chan struct{}, 4),
		lmpSub:      0x8761,
	}
}

func key32(addr uint8, val uint16) uint32 { return uint32(addr)<<16 | uint32(val) }

func (b *fakeBus) Service() error           { return b.serviceErr }
func (b *fakeBus) Ready() <-chan struct{}   { return b.ready }
func (b *fakeBus) Devices() []usb.BusDevice { return b.devices }
func (b *fakeBus) PausePolling(context.Context) error {
	b.paused = true
	b.pauseCalls++
	return nil
}
func (b *fakeBus) ResumePolling(context.Context) error {
	b.paused = false
	b.resumeCalls++
	return nil
}
func (b *fakeBus) NextEvent() (usb.HostEvent, bool) {
	if len(b.events) == 0 {
		return usb.HostEvent{}, false
	}
	ev := b.events[0]
	b.events = b.events[1:]
	return ev, true
}
func (b *fakeBus) OpenPipe(addr uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	p := &fakePipe{addr: addr, ep: ep}
	b.pipes[key32(addr, uint16(ep.Address))] = p
	if b.hasBTAddr(addr) {
		switch {
		case ep.TransferType == usb.TransferInterrupt && ep.In():
			b.btEvPipe = p
		case ep.TransferType == usb.TransferBulk && ep.In():
			b.btACLInPipe = p
		case ep.TransferType == usb.TransferBulk && !ep.In():
			b.btACLOutPipe = p
		}
	}
	return p, nil
}
func (b *fakeBus) hasBTAddr(addr uint8) bool {
	for _, dev := range b.devices {
		if dev.Address == addr {
			_, _, ok := hci.FindBluetoothInterface(dev)
			return ok
		}
	}
	return false
}
func (b *fakeBus) ControlTransfer(addr uint8, reqType, req byte, value, index uint16, data []byte) (int, error) {
	if reqType == 0x81 && req == 0x06 && value == 0x2200 {
		desc, ok := b.reportDescs[key32(addr, index)]
		if !ok {
			return 0, errors.New("missing report descriptor")
		}
		return copy(data, desc), nil
	}
	if reqType == 0x21 && (req == 0x0b || req == 0x0a) {
		return 0, nil
	}
	if reqType == 0x20 && req == 0 && len(data) >= 3 && b.btEvPipe != nil {
		op := binary.LittleEndian.Uint16(data[0:2])
		ev := []byte{hci.EventCommandComplete, 4, 1, byte(op), byte(op >> 8), 0}
		switch op {
		case hci.OpcodeReadLocalVersion:
			ev = []byte{hci.EventCommandComplete, 12, 1, byte(op), byte(op >> 8), 0, 0x0d, 0, 0, 0x0d, 0x5d, 0, byte(b.lmpSub), byte(b.lmpSub >> 8)}
		case 0xfc6d:
			ev = []byte{hci.EventCommandComplete, 5, 1, byte(op), byte(op >> 8), 0, 0x01}
		case 0xfc20:
			if len(data) >= 4 && data[3]&0x80 != 0 {
				b.lmpSub = 0xd922
			}
			ev = []byte{hci.EventCommandComplete, 5, 1, byte(op), byte(op >> 8), 0, data[3]}
		case hci.OpcodeReadBufferSize:
			ev = []byte{hci.EventCommandComplete, 11, 1, byte(op), byte(op >> 8), 0, 0x00, 0x01, 0x40, 0x10, 0x00, 0x04, 0x00}
		case hci.OpcodeReadBDAddr:
			ev = []byte{hci.EventCommandComplete, 10, 1, byte(op), byte(op >> 8), 0, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
		case hci.OpcodeAuthenticationRequested:
			if len(data) >= 5 {
				b.btEvPipe.inQueue = append(b.btEvPipe.inQueue,
					[]byte{hci.EventCommandStatus, 4, 0x00, 1, byte(op), byte(op >> 8)},
					[]byte{hci.EventAuthenticationComplete, 3, 0x00, data[3], data[4]},
				)
				return len(data), nil
			}
		case hci.OpcodeSetConnectionEncryption:
			if len(data) >= 6 {
				b.btEvPipe.inQueue = append(b.btEvPipe.inQueue,
					[]byte{hci.EventCommandStatus, 4, 0x00, 1, byte(op), byte(op >> 8)},
					[]byte{hci.EventEncryptionChange, 4, 0x00, data[3], data[4], data[5]},
				)
				return len(data), nil
			}
		}
		b.btEvPipe.inQueue = append(b.btEvPipe.inQueue, ev)
		return len(data), nil
	}
	return len(data), nil
}

func (b *fakeBus) attach(dev usb.BusDevice) {
	b.devices = append(b.devices, dev)
	b.events = append(b.events, usb.HostEvent{Kind: usb.DeviceAttached, Device: dev})
}

func (b *fakeBus) detach(dev usb.BusDevice) {
	filtered := b.devices[:0]
	for _, d := range b.devices {
		if d.Address != dev.Address {
			filtered = append(filtered, d)
		}
	}
	b.devices = filtered
	b.events = append(b.events, usb.HostEvent{Kind: usb.DeviceDetached, Device: dev})
}

func sampleGamepadDescriptor() []byte {
	return []byte{
		0x05, 0x01, 0x09, 0x05, 0xa1, 0x01,
		0x05, 0x01, 0x09, 0x30, 0x09, 0x31, 0x15, 0x00, 0x26, 0xff, 0x00, 0x75, 0x08, 0x95, 0x02, 0x81, 0x02,
		0x05, 0x09, 0x19, 0x01, 0x29, 0x04, 0x15, 0x00, 0x25, 0x01, 0x75, 0x01, 0x95, 0x04, 0x81, 0x02,
		0x75, 0x04, 0x95, 0x01, 0x81, 0x03,
		0xc0,
	}
}

func consumerHIDDescriptor() []byte {
	return []byte{
		0x05, 0x0c, 0x09, 0x01, 0xa1, 0x01,
		0x15, 0x00, 0x25, 0x01, 0x75, 0x01, 0x95, 0x08, 0x81, 0x02,
		0xc0,
	}
}

func makeKeyboardDev(addr uint8) usb.BusDevice {
	return usb.BusDevice{
		Address: addr,
		Speed:   usb.SpeedFull,
		Config: usb.FullConfiguration{
			Value: 1,
			Interfaces: []usb.InterfaceDescriptor{
				{
					Number: 0, Class: 3, SubClass: 1, Protocol: 1,
					Endpoints: []usb.EndpointDescriptor{{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 8, Interval: 8}},
				},
				{
					Number: 1, Class: 3, SubClass: 0, Protocol: 0, HIDReportBytes: uint16(len(consumerHIDDescriptor())),
					Endpoints: []usb.EndpointDescriptor{{Address: 0x82, TransferType: usb.TransferInterrupt, MaxPacketSize: 8, Interval: 8}},
				},
			},
		},
	}
}

func makeGamepadDev(addr uint8) usb.BusDevice {
	return usb.BusDevice{
		Address: addr,
		Speed:   usb.SpeedFull,
		Config: usb.FullConfiguration{
			Value: 1,
			Interfaces: []usb.InterfaceDescriptor{{
				Number: 0, Class: 3, SubClass: 0, Protocol: 0, HIDReportBytes: uint16(len(sampleGamepadDescriptor())),
				Endpoints: []usb.EndpointDescriptor{{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 8, Interval: 4}},
			}},
		},
	}
}

func makeBTDongleDev(addr uint8) usb.BusDevice {
	return usb.BusDevice{
		Address: addr,
		Speed:   usb.SpeedHigh,
		Descriptor: usb.DeviceDescriptor{
			VendorID: 0x0bda, ProductID: 0xa728, Class: 0xe0, SubClass: 0x01, Protocol: 0x01,
		},
		Config: usb.FullConfiguration{
			Value: 1,
			Interfaces: []usb.InterfaceDescriptor{{
				Number: 0, Class: 0xe0, SubClass: 0x01, Protocol: 0x01,
				Endpoints: []usb.EndpointDescriptor{
					{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 16, Interval: 1},
					{Address: 0x82, TransferType: usb.TransferBulk, MaxPacketSize: 512},
					{Address: 0x02, TransferType: usb.TransferBulk, MaxPacketSize: 512},
				},
			}},
		},
	}
}

func TestHubZeroDeviceBootAndLiveHotplugMergeAndDetachNeutralize(t *testing.T) {
	bus := newFakeBus()
	var events []DeviceEvent
	bonds := l2cap.NewBondStore()
	hub, err := NewHub(bus, Config{
		Bonds: bonds,
		Now:   func() time.Time { return time.Unix(100, 0) },
		OnDeviceEvent: func(ev DeviceEvent) {
			events = append(events, ev)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()

	// Boot with zero devices -> Service returns nil and everything stays neutral.
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if !hub.KeyboardNeutral() || !hub.GamepadNeutral() {
		t.Fatal("expected neutral input on zero-device boot")
	}

	// Attach composite keyboard #1 + keyboard #2 + gamepad + BT dongle simultaneously.
	kbd1 := makeKeyboardDev(2)
	kbd2 := makeKeyboardDev(3)
	pad1 := makeGamepadDev(4)
	dongle := makeBTDongleDev(5)
	bus.reportDescs[key32(2, 1)] = consumerHIDDescriptor()
	bus.reportDescs[key32(3, 1)] = consumerHIDDescriptor()
	bus.reportDescs[key32(4, 0)] = sampleGamepadDescriptor()
	bus.attach(kbd1)
	bus.attach(kbd2)
	bus.attach(pad1)
	bus.attach(dongle)

	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if !hub.Bluetooth().DongleConnected() {
		t.Fatal("expected BT dongle connected")
	}
	if _, skipped := bus.pipes[key32(2, 0x82)]; skipped {
		t.Fatal("consumer HID interface should not be claimed as a gamepad")
	}

	// Hold 'a' on kbd1 and 'b' on kbd2 simultaneously.
	bus.pipes[key32(2, 0x81)].inQueue = append(bus.pipes[key32(2, 0x81)].inQueue, []byte{0x01, 0, 0x04, 0, 0, 0, 0, 0})
	bus.pipes[key32(3, 0x81)].inQueue = append(bus.pipes[key32(3, 0x81)].inQueue, []byte{0x00, 0, 0x05, 0, 0, 0, 0, 0})
	// Hold button 1 + up on wired gamepad.
	bus.pipes[key32(4, 0x81)].inQueue = append(bus.pipes[key32(4, 0x81)].inQueue, []byte{128, 0, 0x01})
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}

	var lastKB [8]byte
	for {
		rep, ok := hub.NextKeyboardReport()
		if !ok {
			break
		}
		lastKB = rep
	}
	if lastKB != ([8]byte{0x01, 0, 0x04, 0x05, 0, 0, 0, 0}) {
		t.Fatalf("unexpected merged keyboard report: %x", lastKB)
	}
	padUpd, ok := hub.NextGamepadUpdate()
	if !ok || padUpd.State.Buttons != 1 || padUpd.State.Hat != 0 {
		t.Fatalf("expected merged gamepad update, got ok=%t upd=%+v", ok, padUpd)
	}

	// Unplug kbd1 while its key is held -> 'a' and LCtrl release, kbd2 'b' stays held!
	bus.detach(kbd1)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	rep, ok := hub.NextKeyboardReport()
	if !ok || rep != ([8]byte{0, 0, 0x05, 0, 0, 0, 0, 0}) {
		t.Fatalf("expected sibling kbd2 key preserved after kbd1 detach, got ok=%t rep=%x", ok, rep)
	}
	if !bus.pipes[key32(2, 0x81)].closed {
		t.Fatal("detached keyboard pipe not closed")
	}

	// Detach kbd2 -> neutralizes cleanly.
	bus.detach(kbd2)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	rep, ok = hub.NextKeyboardReport()
	if !ok || rep != ([8]byte{}) || !hub.KeyboardNeutral() {
		t.Fatalf("expected keyboard neutral after all keyboards unplugged, got ok=%t rep=%x", ok, rep)
	}

	// Pause/Resume boundaries delegate without error or device drops, and ErrTransition does not detach pipes.
	ctx := context.Background()
	if err := hub.PausePolling(ctx); err != nil || !bus.paused || bus.pauseCalls != 1 {
		t.Fatalf("PausePolling failed: err=%v paused=%t", err, bus.paused)
	}
	bus.serviceErr = usbcontrol.ErrTransition
	if err := hub.Service(); err != nil {
		t.Fatalf("ErrTransition should be non-fatal pause, got %v", err)
	}
	bus.serviceErr = nil
	if err := hub.ResumePolling(ctx); err != nil || bus.paused || bus.resumeCalls != 1 {
		t.Fatalf("ResumePolling failed: err=%v paused=%t", err, bus.paused)
	}

	// Unplug pad1 while held -> neutralizes gamepad cleanly.
	bus.detach(pad1)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	padUpd, ok = hub.NextGamepadUpdate()
	if !ok || !hub.GamepadNeutral() || padUpd.State.Buttons != 0 {
		t.Fatalf("expected gamepad neutral on unplug, got ok=%t upd=%+v", ok, padUpd)
	}

	// Detach dongle -> BT disconnects cleanly without affecting wired state.
	bus.detach(dongle)
	if err := hub.Service(); err != nil || hub.Bluetooth().DongleConnected() {
		t.Fatalf("expected BT dongle detached: err=%v connected=%t", err, hub.Bluetooth().DongleConnected())
	}
	if len(events) == 0 {
		t.Fatal("expected device events emitted")
	}
	for _, ev := range events {
		if ev.Kind.String() == "" || ev.Kind.String() == "unknown" {
			t.Fatalf("unexpected device event kind string: %v", ev)
		}
	}
}

func TestHubPipeFaultIsolationGameplayGuardAndSaveBonds(t *testing.T) {
	bus := newFakeBus()
	kbd1 := makeKeyboardDev(2)
	kbd2 := makeKeyboardDev(3)
	dongle := makeBTDongleDev(5)
	bus.attach(kbd1)
	bus.attach(kbd2)
	bus.attach(dongle)

	var persisted []byte
	addr, _ := l2cap.ParseBDAddr("11:22:33:44:55:66")
	bonds := l2cap.NewBondStore()
	_ = bonds.Upsert(l2cap.BondRecord{Addr: addr, LinkKey: [16]byte{1}})

	hub, err := NewHub(bus, Config{
		Bonds: bonds,
		PersistBonds: func(_ context.Context, raw []byte) error {
			persisted = append([]byte(nil), raw...)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}

	// Gameplay mode closes pairing and rejects StartPairing.
	if err := hub.StartPairing(30 * time.Second); err != nil || !hub.PairingActive() {
		t.Fatalf("StartPairing failed: err=%v active=%t", err, hub.PairingActive())
	}
	if err := hub.StopPairing(); err != nil || hub.PairingActive() {
		t.Fatalf("StopPairing failed: err=%v active=%t", err, hub.PairingActive())
	}
	if hub.Ready() == nil {
		t.Fatal("expected non-nil Ready channel")
	}
	if err := hub.StartPairing(30 * time.Second); err != nil || !hub.PairingActive() {
		t.Fatalf("StartPairing failed: err=%v active=%t", err, hub.PairingActive())
	}
	if err := hub.SetGameplayActive(true); err != nil || hub.PairingActive() {
		t.Fatalf("SetGameplayActive(true) failed: err=%v active=%t", err, hub.PairingActive())
	}
	if err := hub.StartPairing(30 * time.Second); err == nil {
		t.Fatal("expected StartPairing rejected during gameplay")
	}

	// Verify two Bluetooth HID keyboards in separate slots merge independently
	// and detaching/releasing one preserves keys held on the other.
	hub.bt.OnSlotKeyboardReport(0, []byte{0x01, 0, 0x04, 0, 0, 0, 0, 0}) // LCtrl+'a'
	hub.bt.OnSlotKeyboardReport(1, []byte{0x00, 0, 0x05, 0, 0, 0, 0, 0}) // 'b'
	hub.bt.OnSlotChanged(0, false, controllers.KindUnknown, "BT Kbd 0")
	var lastBTKB [8]byte
	for {
		rep, ok := hub.NextKeyboardReport()
		if !ok {
			break
		}
		lastBTKB = rep
	}
	if lastBTKB != ([8]byte{0x00, 0, 0x05, 0, 0, 0, 0, 0}) {
		t.Fatalf("expected BT slot 1 key preserved after slot 0 disconnect, got %x", lastBTKB)
	}
	hub.bt.OnSlotKeyboardReport(1, make([]byte, 8))

	// Verify active BT analog trigger merging uses highest signed value (-1000 beats -32768 rest).
	hub.bt.OnGamepadState(0, controllers.KindDualSense, controllers.State{LeftTrigger: 0})
	hub.bt.OnGamepadState(1, controllers.KindDualSense, controllers.State{LeftTrigger: 124})
	var lastPad GamepadUpdate
	for {
		upd, ok := hub.NextGamepadUpdate()
		if !ok {
			break
		}
		lastPad = upd
	}
	if lastPad.State.Axes[4] != -1024 {
		t.Fatalf("expected active BT trigger merge -1024, got %d", lastPad.State.Axes[4])
	}

	// Verify PausePolling flushes stale unconsumed queued reports while preserving held state.
	bus.pipes[key32(2, 0x81)].inQueue = append(bus.pipes[key32(2, 0x81)].inQueue, []byte{0, 0, 0x05, 0, 0, 0, 0, 0})
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if err := hub.PausePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := hub.NextKeyboardReport(); ok {
		t.Fatal("expected PausePolling to flush stale queued keyboard report")
	}
	if hub.KeyboardNeutral() {
		t.Fatal("expected held keyboard state to remain non-neutral after PausePolling")
	}
	if err := hub.ResumePolling(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Inject pipe error on kbd1 only -> kbd1 detaches and neutralizes while kbd2 keeps running.
	bus.pipes[key32(2, 0x81)].inQueue = append(bus.pipes[key32(2, 0x81)].inQueue, []byte{0, 0, 0x04, 0, 0, 0, 0, 0})
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	bus.pipes[key32(2, 0x81)].pollErr = errors.New("usb stall")
	bus.pipes[key32(3, 0x81)].inQueue = append(bus.pipes[key32(3, 0x81)].inQueue, []byte{0, 0, 0x06, 0, 0, 0, 0, 0})
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	var lastKB [8]byte
	for {
		rep, ok := hub.NextKeyboardReport()
		if !ok {
			break
		}
		lastKB = rep
	}
	if lastKB != ([8]byte{0, 0, 0x06, 0, 0, 0, 0, 0}) {
		t.Fatalf("expected kbd2 isolated from kbd1 stall, got %x", lastKB)
	}

	// Fatal bus error propagates out of Service.
	bus.serviceErr = usbcontrol.ErrRetired
	if err := hub.Service(); !errors.Is(err, usbcontrol.ErrRetired) {
		t.Fatalf("expected ErrRetired propagated, got %v", err)
	}

	// SaveBondsIfDirty persists once and clears dirty flag.
	saved, err := hub.SaveBondsIfDirty(context.Background())
	if err != nil || !saved || len(persisted) == 0 || bonds.Dirty() {
		t.Fatalf("SaveBondsIfDirty failed: saved=%t err=%v dirty=%t", saved, err, bonds.Dirty())
	}
	saved, err = hub.SaveBondsIfDirty(context.Background())
	if err != nil || saved {
		t.Fatalf("expected second SaveBondsIfDirty no-op, got saved=%t err=%v", saved, err)
	}
}

func TestHubSteadyStateServiceZeroAllocs(t *testing.T) {
	bus := newFakeBus()
	kbd := makeKeyboardDev(2)
	pad := makeGamepadDev(3)
	bus.reportDescs[key32(3, 0)] = sampleGamepadDescriptor()
	bus.attach(kbd)
	bus.attach(pad)

	hub, err := NewHub(bus, Config{Now: func() time.Time { return time.Unix(200, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(50, func() {
		if err := hub.Service(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("expected 0 allocations per idle Service(), got %.1f", allocs)
	}
}
