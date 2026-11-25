// SPDX-License-Identifier: GPL-3.0-or-later
package usbhub

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

func makeXInputDev(addr uint8) usb.BusDevice {
	return usb.BusDevice{
		Address: addr,
		Speed:   usb.SpeedFull,
		Descriptor: usb.DeviceDescriptor{
			VendorID: 0x045e, ProductID: 0x028e,
		},
		Config: usb.FullConfiguration{
			Value: 1,
			Interfaces: []usb.InterfaceDescriptor{
				{
					Number: 0, Class: 0xff, SubClass: 0x5d, Protocol: 0x01,
					Endpoints: []usb.EndpointDescriptor{
						{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 32, Interval: 4},
						{Address: 0x01, TransferType: usb.TransferInterrupt, MaxPacketSize: 32, Interval: 8},
					},
				},
				{
					Number: 1, Class: 0xff, SubClass: 0x5d, Protocol: 0x03, // headset/audio ignored
					Endpoints: []usb.EndpointDescriptor{
						{Address: 0x82, TransferType: usb.TransferInterrupt, MaxPacketSize: 32, Interval: 4},
					},
				},
			},
		},
	}
}

func makeXboxGIPDev(addr uint8) usb.BusDevice {
	return makeXboxGIPModelDev(addr, 0x045e, 0x0b12)
}

func makeXboxGIPModelDev(addr uint8, vid, pid uint16) usb.BusDevice {
	return usb.BusDevice{
		Address: addr,
		Speed:   usb.SpeedHigh,
		Descriptor: usb.DeviceDescriptor{
			VendorID: vid, ProductID: pid,
		},
		Config: usb.FullConfiguration{
			Value: 1,
			Interfaces: []usb.InterfaceDescriptor{
				{
					Number: 0, Class: 0xff, SubClass: 0x47, Protocol: 0xd0,
					Endpoints: []usb.EndpointDescriptor{
						{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 64, Interval: 4},
						{Address: 0x01, TransferType: usb.TransferInterrupt, MaxPacketSize: 64, Interval: 4},
					},
				},
				{
					Number: 1, Class: 0xff, SubClass: 0x47, Protocol: 0xd0, // Audio interface must be ignored
					Endpoints: []usb.EndpointDescriptor{
						{Address: 0x82, TransferType: usb.TransferIsochronous, MaxPacketSize: 256, Interval: 1},
					},
				},
				{
					Number: 2, Class: 0xff, SubClass: 0x47, Protocol: 0xd0, // Accessory interface must be ignored
					Endpoints: []usb.EndpointDescriptor{
						{Address: 0x83, TransferType: usb.TransferInterrupt, MaxPacketSize: 64, Interval: 4},
					},
				},
			},
		},
	}
}

func makeSonyUSBDev(addr uint8, pid uint16) usb.BusDevice {
	return usb.BusDevice{
		Address: addr,
		Speed:   usb.SpeedHigh,
		Descriptor: usb.DeviceDescriptor{
			VendorID: 0x054c, ProductID: pid,
		},
		Config: usb.FullConfiguration{
			Value: 1,
			Interfaces: []usb.InterfaceDescriptor{
				{Number: 0, Class: 0x01, SubClass: 0x01, Protocol: 0x00}, // Audio control ignored
				{
					Number: 3, Class: 3, SubClass: 0, Protocol: 0, HIDReportBytes: 120,
					Endpoints: []usb.EndpointDescriptor{
						{Address: 0x84, TransferType: usb.TransferInterrupt, MaxPacketSize: 64, Interval: 4},
						{Address: 0x03, TransferType: usb.TransferInterrupt, MaxPacketSize: 64, Interval: 4},
					},
				},
			},
		},
	}
}

func makeSwitchProUSBDev(addr uint8) usb.BusDevice {
	return usb.BusDevice{
		Address: addr,
		Speed:   usb.SpeedFull,
		Descriptor: usb.DeviceDescriptor{
			VendorID: 0x057e, ProductID: 0x2009,
		},
		Config: usb.FullConfiguration{
			Value: 1,
			Interfaces: []usb.InterfaceDescriptor{{
				Number: 0, Class: 3, SubClass: 0, Protocol: 0, HIDReportBytes: 200,
				Endpoints: []usb.EndpointDescriptor{
					{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 64, Interval: 8},
					{Address: 0x01, TransferType: usb.TransferInterrupt, MaxPacketSize: 64, Interval: 8},
				},
			}},
		},
	}
}

func drainGamepadUpdates(hub *Hub) (GamepadUpdate, int) {
	var last GamepadUpdate
	count := 0
	for {
		u, ok := hub.NextGamepadUpdate()
		if !ok {
			return last, count
		}
		last = u
		count++
	}
}

func TestHubWiredXInputAndXboxGIP(t *testing.T) {
	bus := newFakeBus()
	now := time.Unix(100, 0)
	var events []DeviceEvent
	hub, err := NewHub(bus, Config{
		Now:           func() time.Time { return now },
		OnDeviceEvent: func(ev DeviceEvent) { events = append(events, ev) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()

	// 1. Attach XInput controller (headset IF1 must be ignored).
	xi := makeXInputDev(2)
	bus.attach(xi)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if _, skipped := bus.pipes[key32(2, 0x82)]; skipped {
		t.Fatal("XInput headset interface must not be claimed")
	}
	if hub.GamepadNeutral() {
		t.Fatal("newly attached XInput without initial report should not arm GamepadNeutral")
	}
	xiOut := bus.pipes[key32(2, 0x01)]
	if len(xiOut.outPackets) != 1 || len(xiOut.outPackets[0]) != 3 || xiOut.outPackets[0][0] != 0x01 || xiOut.outPackets[0][2] != 0x06 {
		t.Fatalf("expected XInput LED init packet, got %x", xiOut.outPackets)
	}

	// Send neutral report -> arms GamepadNeutral, then held Guide + A + LeftY up (negative canonical Y).
	var xiRep [controllers.XInputInputLen]byte
	xiRep[1] = 0x14
	bus.pipes[key32(2, 0x81)].inQueue = append(bus.pipes[key32(2, 0x81)].inQueue, append([]byte(nil), xiRep[:]...))
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if !hub.GamepadNeutral() {
		t.Fatal("expected GamepadNeutral after neutral XInput report")
	}
	xiRep[3] = 0x04 | 0x10 // Guide(Home) | South(A)
	binary.LittleEndian.PutUint16(xiRep[8:10], 0x7fff)
	bus.pipes[key32(2, 0x81)].inQueue = append(bus.pipes[key32(2, 0x81)].inQueue, append([]byte(nil), xiRep[:]...))
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	upd, cnt := drainGamepadUpdates(hub)
	if cnt == 0 || !upd.HomePressed || upd.State.Buttons&uint32(controllers.ButtonSouth) == 0 || upd.State.Axes[1] != -32767 {
		t.Fatalf("unexpected XInput update cnt=%d upd=%+v", cnt, upd)
	}
	if err := hub.SetWiredRumble(0, 0x80, 0x40); err != nil {
		t.Fatalf("SetWiredRumble(xinput) failed: %v", err)
	}
	if len(xiOut.outPackets) != 2 || xiOut.outPackets[1][0] != 0x00 || xiOut.outPackets[1][3] != 0x80 || xiOut.outPackets[1][4] != 0x40 {
		t.Fatalf("unexpected XInput rumble packet: %x", xiOut.outPackets[1])
	}

	// Unplug XInput while held -> closes both IN/OUT pipes and neutralizes HomePressed + buttons.
	bus.detach(xi)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	upd, cnt = drainGamepadUpdates(hub)
	if cnt == 0 || upd.HomePressed || !hub.GamepadNeutral() {
		t.Fatalf("expected XInput detach neutralization, cnt=%d upd=%+v", cnt, upd)
	}
	if !bus.pipes[key32(2, 0x81)].closed || !xiOut.closed {
		t.Fatal("XInput IN/OUT pipes were not closed on detach")
	}

	// 2. Attach Xbox One/Series GIP controller and advance full 3-step init
	// (PowerOn -> LedOn -> AuthComplete) including PDP pads.
	gip := makeXboxGIPDev(3)
	bus.attach(gip)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if _, skipped := bus.pipes[key32(3, 0x83)]; skipped || hub.wiredGamepads[1].active {
		t.Fatalf("GIP audio/accessory IF1/IF2 must not be claimed as extra gamepads")
	}
	gipOut := bus.pipes[key32(3, 0x01)]
	for range 2 {
		if err := hub.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if len(gipOut.outPackets) != 3 ||
		gipOut.outPackets[0][0] != controllers.XboxGIPCommandPower ||
		gipOut.outPackets[1][0] != 0x0a ||
		gipOut.outPackets[2][0] != 0x06 {
		t.Fatalf("expected Xbox GIP PowerOn -> LedOn -> AuthComplete sequence, got %x", gipOut.outPackets)
	}

	// Receive Guide press 0x07 (flags=0x10 ACK required) while OUT pipe is still
	// busy with rumble (holdWriteDone=true) -> ACK must be queued and sent once ready.
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	gipOut.holdWriteDone = true
	if err := hub.SetWiredRumble(0, 255, 128); err != nil {
		t.Fatalf("SetWiredRumble(xboxgip) failed: %v", err)
	}
	gipGuide := []byte{controllers.XboxGIPCommandGuide, 0x10, 0x09, 0x02, 0x01, 0x00}
	var gipIn [20]byte
	gipIn[0], gipIn[3], gipIn[4] = controllers.XboxGIPCommandInput, 14, 0x10 // South(A)
	bus.pipes[key32(3, 0x81)].inQueue = append(bus.pipes[key32(3, 0x81)].inQueue, gipGuide, gipIn[:])
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if len(gipOut.outPackets) != 4 || gipOut.outPackets[3][0] != controllers.XboxGIPCommandRumble {
		t.Fatalf("expected only rumble packet while OUT pending, got %x", gipOut.outPackets)
	}
	// Release OUT pipe completion, but inject transient ErrBusy -> queued ACK
	// must stay queued and flush only once Write succeeds.
	gipOut.holdWriteDone = false
	gipOut.writeErr = usbcontrol.ErrBusy
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if len(gipOut.outPackets) != 4 {
		t.Fatalf("expected queued ACK retained while ErrBusy, got %x", gipOut.outPackets)
	}
	gipOut.writeErr = nil
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if len(gipOut.outPackets) != 5 || gipOut.outPackets[4][0] != controllers.XboxGIPCommandAck || gipOut.outPackets[4][2] != 0x09 {
		t.Fatalf("expected queued GIP ACK seq=0x09 flushed after WriteDone, got %x", gipOut.outPackets)
	}

	// Immediate Guide ACK hitting transient ErrBusy also enqueues and flushes cleanly.
	gipOut.writeErr = usbcontrol.ErrBusy
	bus.pipes[key32(3, 0x81)].inQueue = append(bus.pipes[key32(3, 0x81)].inQueue, []byte{controllers.XboxGIPCommandGuide, 0x10, 0x0a, 0x02, 0x01, 0x00})
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if len(gipOut.outPackets) != 5 {
		t.Fatalf("expected immediate ACK retained on ErrBusy, got %x", gipOut.outPackets)
	}
	gipOut.writeErr = nil
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if len(gipOut.outPackets) != 6 || gipOut.outPackets[5][0] != controllers.XboxGIPCommandAck || gipOut.outPackets[5][2] != 0x0a {
		t.Fatalf("expected immediate busy-enqueued ACK seq=0x0a flushed, got %x", gipOut.outPackets)
	}
	upd, cnt = drainGamepadUpdates(hub)
	if cnt == 0 || !upd.HomePressed || upd.State.Buttons&uint32(controllers.ButtonSouth) == 0 {
		t.Fatalf("expected GIP HomePressed preserved across 0x20 input, cnt=%d upd=%+v", cnt, upd)
	}
	if len(events) < 2 || events[0].Name != "xinput" {
		t.Fatalf("expected xinput device event name, got %+v", events)
	}

	// 3. Attach Xbox One S (045e:02ea) -> sends model-specific S-init (0x0f 0x06) before PowerOn->LedOn->Auth.
	bus.detach(gip)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	oneS := makeXboxGIPModelDev(4, 0x045e, 0x02ea)
	bus.attach(oneS)
	for range 4 {
		if err := hub.Service(); err != nil {
			t.Fatal(err)
		}
	}
	oneSOut := bus.pipes[key32(4, 0x01)]
	if len(oneSOut.outPackets) != 4 ||
		oneSOut.outPackets[0][0] != 0x05 || oneSOut.outPackets[0][3] != 0x0f || oneSOut.outPackets[0][4] != 0x06 ||
		oneSOut.outPackets[1][0] != 0x05 || oneSOut.outPackets[1][3] != 0x01 ||
		oneSOut.outPackets[2][0] != 0x0a || oneSOut.outPackets[3][0] != 0x06 {
		t.Fatalf("expected Xbox One S init sequence [0f06 -> PowerOn -> LedOn -> Auth], got %x", oneSOut.outPackets)
	}
}

func TestHubWiredDualSenseDS4AndSwitchPro(t *testing.T) {
	bus := newFakeBus()
	now := time.Unix(200, 0)
	hub, err := NewHub(bus, Config{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()

	// DualSense USB (slot 0) + DS4 USB (slot 1) without report descriptors registered in bus.reportDescs!
	ds := makeSonyUSBDev(2, 0x0ce6)
	ds4 := makeSonyUSBDev(3, 0x09cc)
	bus.attach(ds)
	bus.attach(ds4)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	dsOut := bus.pipes[key32(2, 0x03)]
	ds4Out := bus.pipes[key32(3, 0x03)]
	if len(dsOut.outPackets) != 1 || dsOut.outPackets[0][0] != 0x02 {
		t.Fatalf("expected DualSense 0x02 LED packet, got %x", dsOut.outPackets)
	}
	if len(ds4Out.outPackets) != 1 || ds4Out.outPackets[0][0] != 0x05 {
		t.Fatalf("expected DS4 0x05 LED packet, got %x", ds4Out.outPackets)
	}

	var dsIn [64]byte
	dsIn[0] = 0x01
	dsIn[1], dsIn[2], dsIn[3], dsIn[4] = 0x80, 0x80, 0x80, 0x80
	dsIn[8] = 0x08 | 0x20 // neutral hat(0x08) | Cross(South)
	dsIn[10] = 0x01       // PS Home
	var ds4In [64]byte
	ds4In[0] = 0x01
	ds4In[1], ds4In[2], ds4In[3], ds4In[4] = 0x80, 0x80, 0x80, 0x80
	ds4In[5] = 0x08 | 0x40 // neutral hat(0x08) | Circle(East)
	bus.pipes[key32(2, 0x84)].inQueue = append(bus.pipes[key32(2, 0x84)].inQueue, dsIn[:])
	bus.pipes[key32(3, 0x84)].inQueue = append(bus.pipes[key32(3, 0x84)].inQueue, ds4In[:])
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	upd, _ := drainGamepadUpdates(hub)
	want := uint32(controllers.ButtonSouth | controllers.ButtonEast)
	if !upd.HomePressed || upd.State.Buttons&want != want {
		t.Fatalf("expected merged DualSense+DS4 state, got %+v", upd)
	}
	if err := hub.SetWiredRumble(0, 0x20, 0x10); err != nil {
		t.Fatalf("SetWiredRumble(dualsense) failed: %v", err)
	}
	if err := hub.SetWiredRumble(1, 0x30, 0x15); err != nil {
		t.Fatalf("SetWiredRumble(ds4) failed: %v", err)
	}
	bus.detach(ds)
	bus.detach(ds4)
	if err := hub.Service(); err != nil || !hub.GamepadNeutral() {
		t.Fatalf("expected neutral after DualSense/DS4 detach: err=%v neutral=%t", err, hub.GamepadNeutral())
	}

	// Switch Pro USB multi-step handshake: 0x80 0x02 -> reply 0x81 0x02 -> 0x80 0x04 -> 0x10 cal -> 0x03 0x30 -> LED 0x30.
	sw := makeSwitchProUSBDev(4)
	bus.attach(sw)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	swOut := bus.pipes[key32(4, 0x01)]
	if len(swOut.outPackets) != 1 || swOut.outPackets[0][0] != 0x80 || swOut.outPackets[0][1] != 0x02 {
		t.Fatalf("expected Switch Pro [0x80, 0x02] handshake, got %x", swOut.outPackets)
	}
	// Inject 0x81 0x02 handshake ACK -> next Service sends 0x80 0x04 EnableHID.
	bus.pipes[key32(4, 0x81)].inQueue = append(bus.pipes[key32(4, 0x81)].inQueue, []byte{0x81, 0x02, 0x00})
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	// Handshake packet must NOT falsely mark Switch Pro reported.
	if hub.GamepadNeutral() {
		t.Fatal("Switch Pro 0x81 handshake packet must not arm GamepadNeutral")
	}
	now = now.Add(150 * time.Millisecond)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if len(swOut.outPackets) != 2 || swOut.outPackets[1][0] != 0x80 || swOut.outPackets[1][1] != 0x04 {
		t.Fatalf("expected Switch Pro [0x80, 0x04], got %x", swOut.outPackets)
	}
	// Next Service sends SPI calibration read subcmd 0x10.
	now = now.Add(150 * time.Millisecond)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if len(swOut.outPackets) != 3 || swOut.outPackets[2][10] != controllers.SwitchProSubcmdSPIRead {
		t.Fatalf("expected Switch Pro SPI read, got %x", swOut.outPackets)
	}
	// Return 0x21 SPI calibration reply, then advance remaining init steps (0x03 0x30 + Player LED).
	var cal [49]byte
	cal[0], cal[13], cal[14] = controllers.SwitchProReportSubcmdReply, 0x90, controllers.SwitchProSubcmdSPIRead
	binary.LittleEndian.PutUint32(cal[15:19], controllers.SwitchProFactoryStickCalAddr)
	cal[19] = controllers.SwitchProFactoryStickCalLen
	cal[20], cal[21], cal[22] = 0xdc, 0x55, 0x5d // 1500, 1500
	cal[23], cal[24], cal[25] = 0x00, 0x08, 0x80 // 2048, 2048
	cal[26], cal[27], cal[28] = 0xdc, 0x55, 0x5d
	cal[29], cal[30], cal[31] = 0x00, 0x08, 0x80
	cal[32], cal[33], cal[34] = 0xdc, 0x55, 0x5d
	cal[35], cal[36], cal[37] = 0xdc, 0x55, 0x5d
	bus.pipes[key32(4, 0x81)].inQueue = append(bus.pipes[key32(4, 0x81)].inQueue, cal[:])
	// First Service processes the queued 0x21 SPI calibration reply during inPipe polling.
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		now = now.Add(150 * time.Millisecond)
		if err := hub.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if len(swOut.outPackets) != 5 || swOut.outPackets[3][10] != controllers.SwitchProSubcmdSetMode || swOut.outPackets[4][10] != controllers.SwitchProSubcmdSetPlayerLD {
		t.Fatalf("expected Switch Pro SetFullMode + SetPlayerLEDs, got %x", swOut.outPackets)
	}
	// Inject 0x30 Full Input with Home pressed and verify HomePressed propagation.
	var swFull [49]byte
	swFull[0] = controllers.SwitchProReportFull
	swFull[4] = 0x10 // Home
	swFull[6], swFull[7], swFull[8] = 0x00, 0x08, 0x80
	swFull[9], swFull[10], swFull[11] = 0x00, 0x08, 0x80
	bus.pipes[key32(4, 0x81)].inQueue = append(bus.pipes[key32(4, 0x81)].inQueue, swFull[:])
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	upd, cnt := drainGamepadUpdates(hub)
	if cnt == 0 || !upd.HomePressed {
		t.Fatalf("expected Switch Pro HomePressed, cnt=%d upd=%+v", cnt, upd)
	}
	// SetWiredRumble refuses Switch Pro cleanly without panic or detaching slot.
	if err := hub.SetWiredRumble(0, 10, 10); err == nil {
		t.Fatal("expected SetWiredRumble unsupported error for Switch Pro")
	}
}

func TestHubWiredGIPFaultIsolationAndZeroAllocs(t *testing.T) {
	bus := newFakeBus()
	gip := makeXboxGIPDev(2)
	ds := makeSonyUSBDev(3, 0x0ce6)
	kbd := makeKeyboardDev(4)
	bus.attach(gip)
	bus.attach(ds)
	bus.attach(kbd)

	hub, err := NewHub(bus, Config{Now: func() time.Time { return time.Unix(300, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}

	// Inject write error during GIP Guide ACK -> isolates/closes GIP only, leaving DualSense & keyboard alive.
	bus.pipes[key32(2, 0x01)].writeErr = errors.New("out stall")
	bus.pipes[key32(2, 0x81)].inQueue = append(bus.pipes[key32(2, 0x81)].inQueue,
		[]byte{controllers.XboxGIPCommandGuide, 0x10, 0x03, 0x02, 0x01, 0x00})
	bus.pipes[key32(4, 0x81)].inQueue = append(bus.pipes[key32(4, 0x81)].inQueue,
		[]byte{0, 0, 0x04, 0, 0, 0, 0, 0})
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	if hub.wiredGamepads[0].active || !hub.wiredGamepads[1].active {
		t.Fatalf("expected GIP slot 0 isolated while DualSense slot 1 stayed active: s0=%t s1=%t",
			hub.wiredGamepads[0].active, hub.wiredGamepads[1].active)
	}
	if rep, ok := hub.NextKeyboardReport(); !ok || rep[2] != 0x04 {
		t.Fatalf("expected keyboard unaffected by GIP fault: ok=%t rep=%x", ok, rep)
	}
	hub.closeWiredGamepad(1)
	hub.closeWiredKeyboard(0)

	// Verify 0 heap allocations during steady-state active wired XInput + DualSense polling.
	xi := makeXInputDev(5)
	ds2 := makeSonyUSBDev(6, 0x0ce6)
	bus.attach(xi)
	bus.attach(ds2)
	if err := hub.Service(); err != nil {
		t.Fatal(err)
	}
	var xiRep [controllers.XInputInputLen]byte
	xiRep[1] = 0x14
	var dsRep [64]byte
	dsRep[0], dsRep[1], dsRep[2], dsRep[3], dsRep[4], dsRep[8] = 0x01, 0x80, 0x80, 0x80, 0x80, 0x08
	xiSlice := xiRep[:]
	dsSlice := dsRep[:]
	xiBacking := make([][]byte, 1)
	dsBacking := make([][]byte, 1)
	xiBacking[0] = xiSlice
	dsBacking[0] = dsSlice
	xiIn := bus.pipes[key32(5, 0x81)]
	dsIn := bus.pipes[key32(6, 0x84)]

	allocs := testing.AllocsPerRun(50, func() {
		xiIn.inQueue = xiBacking
		dsIn.inQueue = dsBacking
		if err := hub.Service(); err != nil {
			t.Fatal(err)
		}
		_, _ = hub.NextGamepadUpdate()
	})
	if allocs > 0 {
		t.Fatalf("wired native controller steady-state Service() allocated %.2f allocs/run, want 0", allocs)
	}
}
