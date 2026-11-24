// SPDX-License-Identifier: GPL-3.0-or-later
package hidp

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type hidpMockPipe struct {
	ep           usb.EndpointDescriptor
	inQueue      [][]byte
	inRepeat     []byte
	outLog       [][]byte
	noOutLog     bool
	writeStalled bool
}

func (p *hidpMockPipe) DeviceAddress() uint8             { return 2 }
func (p *hidpMockPipe) Endpoint() usb.EndpointDescriptor { return p.ep }
func (p *hidpMockPipe) SetIntervalOverride(time.Duration) {}
func (p *hidpMockPipe) Close() error                     { return nil }
func (p *hidpMockPipe) WriteDone() (bool, error)         { return !p.writeStalled, nil }
func (p *hidpMockPipe) Write(data []byte) error {
	if !p.noOutLog {
		p.outLog = append(p.outLog, append([]byte(nil), data...))
	}
	return nil
}
func (p *hidpMockPipe) PollInto(dst []byte) (int, error) {
	if p.inRepeat != nil {
		return copy(dst, p.inRepeat), nil
	}
	if len(p.inQueue) == 0 {
		return 0, nil
	}
	pkt := p.inQueue[0]
	p.inQueue = p.inQueue[1:]
	return copy(dst, pkt), nil
}

type hidpMockBus struct {
	dev        usb.BusDevice
	evPipe     *hidpMockPipe
	aclInPipe  *hidpMockPipe
	aclOutPipe *hidpMockPipe
	scanEnable byte
}

func newHIDPMockBus() *hidpMockBus {
	dev := usb.BusDevice{
		Address: 2,
		Speed:   usb.SpeedHigh,
		Descriptor: usb.DeviceDescriptor{
			VendorID: 0x0bda, ProductID: 0xa728, Class: 0xe0, SubClass: 0x01, Protocol: 0x01,
		},
		Config: usb.FullConfiguration{
			Value: 1,
			Interfaces: []usb.InterfaceDescriptor{
				{
					Number: 0, Class: 0xe0, SubClass: 0x01, Protocol: 0x01,
					Endpoints: []usb.EndpointDescriptor{
						{Address: 0x81, TransferType: usb.TransferInterrupt, MaxPacketSize: 16, Interval: 1},
						{Address: 0x82, TransferType: usb.TransferBulk, MaxPacketSize: 512},
						{Address: 0x02, TransferType: usb.TransferBulk, MaxPacketSize: 512},
					},
				},
			},
		},
	}
	return &hidpMockBus{dev: dev}
}

func (b *hidpMockBus) Service() error                   { return nil }
func (b *hidpMockBus) NextEvent() (usb.HostEvent, bool) { return usb.HostEvent{}, false }
func (b *hidpMockBus) Devices() []usb.BusDevice         { return []usb.BusDevice{b.dev} }
func (b *hidpMockBus) Ready() <-chan struct{}           { return nil }
func (b *hidpMockBus) OpenPipe(_ uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	p := &hidpMockPipe{ep: ep}
	switch {
	case ep.TransferType == usb.TransferInterrupt && ep.In():
		b.evPipe = p
	case ep.TransferType == usb.TransferBulk && ep.In():
		b.aclInPipe = p
	case ep.TransferType == usb.TransferBulk && !ep.In():
		b.aclOutPipe = p
	}
	return p, nil
}
func (b *hidpMockBus) ControlTransfer(_ uint8, reqType, req byte, _, _ uint16, data []byte) (int, error) {
	if reqType == 0x20 && req == 0 && len(data) >= 3 {
		op := binary.LittleEndian.Uint16(data[0:2])
		ev := []byte{hci.EventCommandComplete, 4, 1, byte(op), byte(op >> 8), 0}
		if op == hci.OpcodeReadLocalVersion {
			ev = []byte{hci.EventCommandComplete, 12, 1, byte(op), byte(op >> 8), 0, 0x0d, 0, 0, 0x0d, 0x5d, 0, 0x22, 0xd9}
		} else if op == hci.OpcodeReadBufferSize {
			ev = []byte{hci.EventCommandComplete, 11, 1, byte(op), byte(op >> 8), 0, 0x00, 0x01, 0x40, 0x10, 0x00, 0x04, 0x00}
		} else if op == hci.OpcodeReadBDAddr {
			ev = []byte{hci.EventCommandComplete, 10, 1, byte(op), byte(op >> 8), 0, 1, 2, 3, 4, 5, 6}
		} else if op == hci.OpcodeWriteScanEnable && len(data) >= 4 {
			b.scanEnable = data[3]
		}
		b.evPipe.inQueue = append(b.evPipe.inQueue, ev)
	}
	return len(data), nil
}

func makeHIDACL(handle, cid uint16, body []byte) []byte {
	l2 := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint16(l2[0:2], uint16(len(body)))
	binary.LittleEndian.PutUint16(l2[2:4], cid)
	copy(l2[4:], body)
	acl := make([]byte, 4+len(l2))
	binary.LittleEndian.PutUint16(acl[0:2], (handle&0x0fff)|uint16(hci.ACLBoundaryFirstAutoFlush)<<12)
	binary.LittleEndian.PutUint16(acl[2:4], uint16(len(l2)))
	copy(acl[4:], l2)
	return acl
}

func openIncomingHIDChannels(bus *hidpMockBus, handle uint16, ctrlRemoteCID, intrRemoteCID uint16) {
	// Controller-initiated reconnect: opens PSM 0x0011 then PSM 0x0013!
	connCtrl := []byte{0x02, 1, 4, 0, 0x11, 0x00, byte(ctrlRemoteCID), byte(ctrlRemoteCID >> 8)}
	cfgReqCtrl := []byte{0x04, 2, 4, 0, 0x40, 0x00, 0x00, 0x00}
	cfgRspCtrl := []byte{0x05, 1, 6, 0, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}
	connIntr := []byte{0x02, 3, 4, 0, 0x13, 0x00, byte(intrRemoteCID), byte(intrRemoteCID >> 8)}
	cfgReqIntr := []byte{0x04, 4, 4, 0, 0x41, 0x00, 0x00, 0x00}
	cfgRspIntr := []byte{0x05, 2, 6, 0, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeHIDACL(handle, l2cap.CIDSignaling, connCtrl),
		makeHIDACL(handle, l2cap.CIDSignaling, cfgReqCtrl),
		makeHIDACL(handle, l2cap.CIDSignaling, cfgRspCtrl),
		makeHIDACL(handle, l2cap.CIDSignaling, connIntr),
		makeHIDACL(handle, l2cap.CIDSignaling, cfgReqIntr),
		makeHIDACL(handle, l2cap.CIDSignaling, cfgRspIntr),
	)
}

func makeSwitchProSubcmdReply(ack, subcmd byte, extra []byte) []byte {
	pkt := make([]byte, 50)
	pkt[0] = hidpDataInput
	pkt[1] = controllers.SwitchProReportSubcmdReply
	pkt[14] = ack
	pkt[15] = subcmd
	copy(pkt[16:], extra)
	return pkt
}

func makeSwitchProSPICalReply(ack byte) []byte {
	var extra [4 + 1 + controllers.SwitchProFactoryStickCalLen]byte
	binary.LittleEndian.PutUint32(extra[0:4], controllers.SwitchProFactoryStickCalAddr)
	extra[4] = controllers.SwitchProFactoryStickCalLen
	for i := 5; i < len(extra); i += 3 {
		extra[i], extra[i+1], extra[i+2] = 0x00, 0x08, 0x80 // center=2048, minOff=2048, maxOff=2048
	}
	return makeSwitchProSubcmdReply(ack, controllers.SwitchProSubcmdSPIRead, extra[:])
}

func TestDualSenseAndSwitchProConcurrentHIDPAndZeroAllocs(t *testing.T) {
	bus := newHIDPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}

	// Pre-bond a DualSense (Player 1, slot 0) and a Switch Pro (Player 2, slot 1)!
	dsAddr, _ := l2cap.ParseBDAddr("AA:11:22:33:44:55")
	swAddr, _ := l2cap.ParseBDAddr("BB:11:22:33:44:55")
	bonds := l2cap.NewBondStore()
	_ = bonds.Upsert(l2cap.BondRecord{Addr: dsAddr, LinkKey: [16]byte{1}, Name: "DualSense Wireless Controller", Kind: controllers.KindDualSense, Slot: 0})
	_ = bonds.Upsert(l2cap.BondRecord{Addr: swAddr, LinkKey: [16]byte{2}, Name: "Pro Controller", Kind: controllers.KindSwitchPro, Slot: 1})

	now := time.Unix(200, 0)
	sess, err := NewSession(tr, bonds, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	var slotEvents int
	var gotDS, gotSW controllers.State
	sess.OnSlotChanged = func(_ uint8, _ bool, _ controllers.Kind, _ string) { slotEvents++ }
	sess.OnGamepadState = func(slot uint8, kind controllers.Kind, st controllers.State) {
		if slot == 0 && kind == controllers.KindDualSense {
			gotDS = st
		}
		if slot == 1 && kind == controllers.KindSwitchPro {
			gotSW = st
		}
	}

	// Reconnect both bonded controllers concurrently (handles 0x0041 and 0x0042).
	dsConn := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, dsAddr[:]...)
	dsConn = append(dsConn, 0x01, 0x01)
	swConn := append([]byte{hci.EventConnectionComplete, 11, 0, 0x42, 0x00}, swAddr[:]...)
	swConn = append(swConn, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, dsConn, swConn)
	openIncomingHIDChannels(bus, 0x0041, 0x0050, 0x0051)
	openIncomingHIDChannels(bus, 0x0042, 0x0060, 0x0061)
	for range 12 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	// Replenish controller ACL TX credits so all queued frames flush and drive
	// Switch Pro initialization through matching 0x21 subcommand ACKs.
	bus.evPipe.inQueue = append(bus.evPipe.inQueue,
		[]byte{hci.EventNumCompletedPackets, 5, 1, 0x42, 0x00, 0x08, 0x00})
	for range 4 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0042, 0x0041, makeSwitchProSPICalReply(0x90)))
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0042, 0x0041, makeSwitchProSubcmdReply(0x80, controllers.SwitchProSubcmdSetMode, nil)))
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0042, 0x0041, makeSwitchProSubcmdReply(0x80, controllers.SwitchProSubcmdSetPlayerLD, nil)))
	for range 6 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if sess.slots[1].initStep != initStepDone || !sess.slots[1].cal.Valid {
		t.Fatalf("expected Switch Pro init complete with valid calibration, step=%d calValid=%t",
			sess.slots[1].initStep, sess.slots[1].cal.Valid)
	}
	if sess.ConnectedCount() != 2 || slotEvents != 2 {
		t.Fatalf("expected 2 connected slots, count=%d events=%d", sess.ConnectedCount(), slotEvents)
	}
	if _, ok := sess.Slot(0); !ok {
		t.Fatal("missing DualSense slot 0")
	}
	if _, ok := sess.Slot(1); !ok {
		t.Fatal("missing Switch Pro slot 1")
	}

	// Verify DualSense sent HIDP GET_REPORT Feature 0x05 on Ctrl (0x0050) AND CRC-32 LED output 0x31 on Intr (0x0051),
	// AND Switch Pro sent SPI cal (0x10) + Full Mode (0x03 0x30) + Player 2 LED (0x30 0x02) on Intr (0x0061)!
	var sawDSUnlock, sawDSLED, sawSWCal, sawSWFull, sawSWLED bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) < 10 {
			continue
		}
		handle := binary.LittleEndian.Uint16(pkt[0:2]) & 0x0fff
		cid := binary.LittleEndian.Uint16(pkt[6:8])
		hid := pkt[8:]
		if handle == 0x0041 && cid == 0x0050 && len(hid) >= 2 && hid[0] == hidpGetReportFeature && hid[1] == 0x05 {
			sawDSUnlock = true
		}
		if handle == 0x0041 && cid == 0x0051 && len(hid) == 79 && hid[0] == hidpDataOutput && hid[1] == 0x31 && hid[2] == 0x00 && hid[3] == 0x10 && hid[47] == 0x04 {
			sawDSLED = true
		}
		if handle == 0x0042 && cid == 0x0061 && len(hid) == 50 && hid[0] == hidpDataOutput && hid[1] == 0x01 {
			switch hid[11] {
			case controllers.SwitchProSubcmdSPIRead:
				sawSWCal = true
			case controllers.SwitchProSubcmdSetMode:
				sawSWFull = hid[12] == controllers.SwitchProReportFull
			case controllers.SwitchProSubcmdSetPlayerLD:
				sawSWLED = hid[12] == 0x02 // Player 2 = slot 1
			}
		}
	}
	if !sawDSUnlock || !sawDSLED || !sawSWCal || !sawSWFull || !sawSWLED {
		t.Fatalf("missing init packets: dsUnlock=%t dsLED=%t swCal=%t swFull=%t swLED=%t",
			sawDSUnlock, sawDSLED, sawSWCal, sawSWFull, sawSWLED)
	}

	// Verify gameplay radio mode disables RF scan windows (0x00).
	if err := sess.SetGameplayActive(true); err != nil || bus.scanEnable != hci.ScanDisabled {
		t.Fatalf("SetGameplayActive(true): err=%v scan=%#x", err, bus.scanEnable)
	}
	bus.aclOutPipe.outLog = nil
	if err := sess.SetRumble(0, 0x30, 0x10); err != nil {
		t.Fatalf("SetRumble: %v", err)
	}
	var sawRumbleSeq1 bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) == 8+79 && pkt[8] == hidpDataOutput && pkt[9] == 0x31 && pkt[10] == 0x10 && pkt[11] == 0x10 && pkt[14] == 0x30 && pkt[15] == 0x10 {
			sawRumbleSeq1 = true
		}
	}
	if !sawRumbleSeq1 {
		t.Fatalf("expected DualSense rumble output report with advancing seq=1 (0x10), outLog=%x", bus.aclOutPipe.outLog)
	}

	// Deliver concurrent DualSense 0x31 and Switch Pro 0x30 reports!
	dsPkt := make([]byte, 79)
	dsPkt[0] = hidpDataInput
	dsPkt[1] = controllers.DualSenseBTReportFull
	dsPkt[3], dsPkt[4], dsPkt[5], dsPkt[6] = 0x80, 0x80, 0x80, 0x80
	dsPkt[10] = 0x08 | 0x20 // Cross (South)
	dsSum := crc32.Update(0, crc32.IEEETable, []byte{0xa1})
	dsSum = crc32.Update(dsSum, crc32.IEEETable, dsPkt[1:75])
	binary.LittleEndian.PutUint32(dsPkt[75:79], dsSum)
	swPkt := make([]byte, 50)
	swPkt[0] = hidpDataInput
	swPkt[1] = controllers.SwitchProReportFull
	swPkt[4] = 0x08 // A (East)
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeHIDACL(0x0041, 0x0041, dsPkt),
		makeHIDACL(0x0042, 0x0041, swPkt),
	)
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	if gotDS.Buttons != controllers.ButtonSouth || gotSW.Buttons != controllers.ButtonEast {
		t.Fatalf("unexpected decoded states ds=%+v sw=%+v", gotDS, gotSW)
	}

	// Verify BT HID Keyboard fallback on an unknown-kind slot (slot 2).
	kbdAddr, _ := l2cap.ParseBDAddr("CC:11:22:33:44:55")
	_ = bonds.Upsert(l2cap.BondRecord{Addr: kbdAddr, LinkKey: [16]byte{3}, Name: "BT Keyboard", Slot: 2})
	kbdConn := append([]byte{hci.EventConnectionComplete, 11, 0, 0x43, 0x00}, kbdAddr[:]...)
	kbdConn = append(kbdConn, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, kbdConn)
	openIncomingHIDChannels(bus, 0x0043, 0x0070, 0x0071)
	var gotKbd []byte
	sess.OnKeyboardReport = func(r []byte) { gotKbd = append(gotKbd[:0], r...) }
	kbdWant := []byte{0, 0, 0x04, 0, 0, 0, 0, 0}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0043, 0x0041, append([]byte{hidpDataInput, 0x01}, kbdWant...)))
	for range 4 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(gotKbd, kbdWant) {
		t.Fatalf("BT keyboard report=%x, want %x", gotKbd, kbdWant)
	}

	// Verify zero heap allocations on steady-state DualSense 0x31 HIDP dispatch!
	bus.aclOutPipe.noOutLog = true
	bus.aclInPipe.inRepeat = makeHIDACL(0x0041, 0x0041, dsPkt)
	var frames int
	sess.OnGamepadState = func(_ uint8, _ controllers.Kind, _ controllers.State) { frames++ }
	allocs := testing.AllocsPerRun(100, func() {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 0 || frames == 0 {
		t.Fatalf("HIDP DualSense hot path allocated %.2f allocs/run (frames=%d), want 0", allocs, frames)
	}
}

func TestSessionPersistsAssignedSlotOnConcurrentPairingCollision(t *testing.T) {
	bus := newHIDPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	bonds := l2cap.NewBondStore()
	sess, err := NewSession(tr, bonds, nil)
	if err != nil {
		t.Fatal(err)
	}

	pad1, _ := l2cap.ParseBDAddr("01:02:03:04:05:06")
	pad2, _ := l2cap.ParseBDAddr("06:05:04:03:02:01")

	// Both newly paired bonds start with default Slot: 0.
	_ = bonds.Upsert(l2cap.BondRecord{Addr: pad1, LinkKey: [16]byte{1}, Name: "DualSense Wireless Controller", Kind: controllers.KindDualSense, Slot: 0})
	_ = bonds.Upsert(l2cap.BondRecord{Addr: pad2, LinkKey: [16]byte{2}, Name: "DualSense Wireless Controller", Kind: controllers.KindDualSense, Slot: 0})

	c1 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, pad1[:]...)
	c1 = append(c1, 0x01, 0x01)
	c2 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x42, 0x00}, pad2[:]...)
	c2 = append(c2, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, c1, c2)
	openIncomingHIDChannels(bus, 0x0041, 0x0050, 0x0051)
	openIncomingHIDChannels(bus, 0x0042, 0x0060, 0x0061)
	for range 6 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}

	rec1, ok1 := bonds.Lookup(pad1)
	rec2, ok2 := bonds.Lookup(pad2)
	if !ok1 || !ok2 || rec1.Slot != 0 || rec2.Slot != 1 {
		t.Fatalf("expected persisted slots 0 and 1, got ok=(%t,%t) slots=(%d,%d)", ok1, ok2, rec1.Slot, rec2.Slot)
	}

	// Disconnect both, then reconnect ONLY pad2: it must return directly to slot 1!
	disc1 := []byte{hci.EventDisconnectionComplete, 4, 0, 0x41, 0x00, 0x13}
	disc2 := []byte{hci.EventDisconnectionComplete, 4, 0, 0x42, 0x00, 0x13}
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, disc1, disc2)
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	c2b := append([]byte{hci.EventConnectionComplete, 11, 0, 0x43, 0x00}, pad2[:]...)
	c2b = append(c2b, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, c2b)
	openIncomingHIDChannels(bus, 0x0043, 0x0070, 0x0071)
	for range 4 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := sess.Slot(0); ok {
		t.Fatal("pad2 hijacked slot 0 instead of returning to persisted slot 1")
	}
	if s1, ok := sess.Slot(1); !ok || s1.Addr != pad2 {
		t.Fatalf("expected pad2 in slot 1 on reconnect, got ok=%t slot=%+v", ok, s1)
	}
}

func TestDisconnectNeutralizesGamepadStateBeforeClearingSlot(t *testing.T) {
	bus := newHIDPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	dsAddr, _ := l2cap.ParseBDAddr("DD:11:22:33:44:55")
	bonds := l2cap.NewBondStore()
	_ = bonds.Upsert(l2cap.BondRecord{Addr: dsAddr, LinkKey: [16]byte{9}, Name: "DualSense Wireless Controller", Kind: controllers.KindDualSense, Slot: 0})

	sess, err := NewSession(tr, bonds, nil)
	if err != nil {
		t.Fatal(err)
	}

	var events []string
	var lastState controllers.State
	sess.OnGamepadState = func(slot uint8, kind controllers.Kind, st controllers.State) {
		if slot == 0 && kind == controllers.KindDualSense {
			lastState = st
			if st.Neutral() {
				events = append(events, "neutral")
			} else {
				events = append(events, "active")
			}
		}
	}
	sess.OnSlotChanged = func(slot uint8, connected bool, _ controllers.Kind, _ string) {
		if slot == 0 && !connected {
			events = append(events, "disconnected")
		}
	}

	conn := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, dsAddr[:]...)
	conn = append(conn, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, conn)
	openIncomingHIDChannels(bus, 0x0041, 0x0050, 0x0051)
	for range 6 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}

	dsPkt := make([]byte, 79)
	dsPkt[0] = hidpDataInput
	dsPkt[1] = controllers.DualSenseBTReportFull
	dsPkt[3], dsPkt[4], dsPkt[5], dsPkt[6] = 0x80, 0x80, 0x80, 0x80
	dsPkt[10] = 0x20 // Cross (South)
	dsSum := crc32.Update(0, crc32.IEEETable, []byte{0xa1})
	dsSum = crc32.Update(dsSum, crc32.IEEETable, dsPkt[1:75])
	binary.LittleEndian.PutUint32(dsPkt[75:79], dsSum)
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0041, 0x0041, dsPkt))
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	if lastState.Neutral() {
		t.Fatal("expected non-neutral active state before disconnect")
	}

	// Simulate an ordinary individual controller disconnect without a release report.
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventDisconnectionComplete, 4, 0, 0x41, 0x00, 0x13})
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	if !lastState.Neutral() {
		t.Fatalf("expected neutral state after disconnect, got %+v", lastState)
	}
	if len(events) != 3 || events[0] != "active" || events[1] != "neutral" || events[2] != "disconnected" {
		t.Fatalf("unexpected disconnect event sequence: %v", events)
	}
}

func TestSwitchProInitRetriesAfterACLQueueFull(t *testing.T) {
	bus := newHIDPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	swAddr, _ := l2cap.ParseBDAddr("EE:11:22:33:44:55")
	bonds := l2cap.NewBondStore()
	_ = bonds.Upsert(l2cap.BondRecord{Addr: swAddr, LinkKey: [16]byte{7}, Name: "Pro Controller", Kind: controllers.KindSwitchPro, Slot: 0})

	now := time.Unix(500, 0)
	sess, err := NewSession(tr, bonds, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	conn := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, swAddr[:]...)
	conn = append(conn, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, conn)
	// Open Control channel and start Interrupt channel up to cfgReq, leaving cfgRsp for after queue saturation.
	connCtrl := []byte{0x02, 1, 4, 0, 0x11, 0x00, 0x50, 0x00}
	cfgReqCtrl := []byte{0x04, 2, 4, 0, 0x40, 0x00, 0x00, 0x00}
	cfgRspCtrl := []byte{0x05, 1, 6, 0, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}
	connIntr := []byte{0x02, 3, 4, 0, 0x13, 0x00, 0x51, 0x00}
	cfgReqIntr := []byte{0x04, 4, 4, 0, 0x41, 0x00, 0x00, 0x00}
	cfgRspIntr := []byte{0x05, 2, 6, 0, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeHIDACL(0x0041, l2cap.CIDSignaling, connCtrl),
		makeHIDACL(0x0041, l2cap.CIDSignaling, cfgReqCtrl),
		makeHIDACL(0x0041, l2cap.CIDSignaling, cfgRspCtrl),
		makeHIDACL(0x0041, l2cap.CIDSignaling, connIntr),
		makeHIDACL(0x0041, l2cap.CIDSignaling, cfgReqIntr),
	)
	for range 6 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	bus.aclOutPipe.outLog = nil

	// Stall ACL OUT pipe and pre-fill 17 frames (1 enters txInFlight, 16 fill
	// txRing) so step 0 (SPIRead) hits ACL TX queue full during onChannelOpen.
	bus.aclOutPipe.writeStalled = true
	for range 17 {
		if err := tr.SendACL(0x0041, hci.ACLBoundaryFirstAutoFlush, []byte{0xde, 0xad}); err != nil {
			t.Fatalf("prefill SendACL failed early: %v", err)
		}
	}

	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0041, l2cap.CIDSignaling, cfgRspIntr))
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	if sess.slots[0].initStep != 0 || sess.slots[0].initWaitingAck {
		t.Fatalf("expected initStep=0 & initWaitingAck=false after step 0 queue overflow, got step=%d wait=%t",
			sess.slots[0].initStep, sess.slots[0].initWaitingAck)
	}

	// Unstall ACL OUT pipe and drain the queue so step 0 transmits with seq=0.
	bus.aclOutPipe.writeStalled = false
	for range 20 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x10, 0x00})
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if sess.slots[0].initStep != 0 || !sess.slots[0].initWaitingAck {
		t.Fatalf("expected initStep=0 awaiting 0x10 ACK after queue drain, got step=%d wait=%t",
			sess.slots[0].initStep, sess.slots[0].initWaitingAck)
	}

	// Deliver matching 0x21 ACKs for step 0 -> step 1 -> step 2.
	for _, ack := range [][]byte{
		makeSwitchProSPICalReply(0x90),
		makeSwitchProSubcmdReply(0x80, controllers.SwitchProSubcmdSetMode, nil),
		makeSwitchProSubcmdReply(0x80, controllers.SwitchProSubcmdSetPlayerLD, nil),
	} {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x04, 0x00})
		bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0041, 0x0041, ack))
		for range 3 {
			if err := sess.Service(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if sess.slots[0].initStep != initStepDone {
		t.Fatalf("Switch Pro init did not finish after ACKs: initStep=%d", sess.slots[0].initStep)
	}

	var spiCount, modeCount, ledCount int
	var seqs []byte
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) < 8+50 {
			continue
		}
		cid := binary.LittleEndian.Uint16(pkt[6:8])
		hid := pkt[8:]
		if cid != 0x0051 || hid[0] != hidpDataOutput || hid[1] != 0x01 {
			continue
		}
		seqs = append(seqs, hid[2])
		switch hid[11] {
		case controllers.SwitchProSubcmdSPIRead:
			spiCount++
		case controllers.SwitchProSubcmdSetMode:
			modeCount++
		case controllers.SwitchProSubcmdSetPlayerLD:
			ledCount++
		}
	}
	if spiCount != 1 || modeCount != 1 || ledCount != 1 {
		t.Fatalf("unexpected Switch Pro init command counts spi=%d mode=%d led=%d", spiCount, modeCount, ledCount)
	}
	if !bytes.Equal(seqs, []byte{0, 1, 2}) {
		t.Fatalf("unexpected Switch Pro retry subcommand sequence numbers: %v", seqs)
	}
}

func TestSwitchProInitWaitsForAckAndRetriesOnDropOrNack(t *testing.T) {
	bus := newHIDPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	swAddr, _ := l2cap.ParseBDAddr("FF:11:22:33:44:55")
	bonds := l2cap.NewBondStore()
	_ = bonds.Upsert(l2cap.BondRecord{Addr: swAddr, LinkKey: [16]byte{8}, Name: "Pro Controller", Kind: controllers.KindSwitchPro, Slot: 0})

	now := time.Unix(600, 0)
	sess, err := NewSession(tr, bonds, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	conn := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, swAddr[:]...)
	conn = append(conn, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, conn)
	openIncomingHIDChannels(bus, 0x0041, 0x0050, 0x0051)
	for range 8 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x04, 0x00})
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}

	collectSubcmds := func() (subcmds, seqs []byte) {
		for _, pkt := range bus.aclOutPipe.outLog {
			if len(pkt) < 8+50 {
				continue
			}
			cid := binary.LittleEndian.Uint16(pkt[6:8])
			hid := pkt[8:]
			if cid == 0x0051 && hid[0] == hidpDataOutput && hid[1] == 0x01 {
				seqs = append(seqs, hid[2])
				subcmds = append(subcmds, hid[11])
			}
		}
		return subcmds, seqs
	}

	// 1. Only SPIRead (0x10) should be sent initially; SetMode must wait for 0x21 ACK.
	subcmds, seqs := collectSubcmds()
	if !bytes.Equal(subcmds, []byte{controllers.SwitchProSubcmdSPIRead}) || !bytes.Equal(seqs, []byte{0}) {
		t.Fatalf("expected only SPIRead seq=0 before ACK, got subcmds=%x seqs=%v", subcmds, seqs)
	}

	// 2. NACK (ack=0x00) and mismatched subcommand ACK must NOT advance or spam immediately.
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeHIDACL(0x0041, 0x0041, makeSwitchProSPICalReply(0x00)),
		makeHIDACL(0x0041, 0x0041, makeSwitchProSubcmdReply(0x80, controllers.SwitchProSubcmdSetMode, nil)),
	)
	for range 3 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if sess.slots[0].initStep != 0 || sess.slots[0].cal.Valid {
		t.Fatalf("NACK or wrong subcommand advanced init: step=%d calValid=%t", sess.slots[0].initStep, sess.slots[0].cal.Valid)
	}
	subcmds, _ = collectSubcmds()
	if len(subcmds) != 1 {
		t.Fatalf("expected rate-limiting to prevent retry before interval, got subcmds=%x", subcmds)
	}

	// 3. Advancing clock by retry interval retries step 0 with incremented seq=1.
	now = now.Add(switchProSubcmdRetryInterval)
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	subcmds, seqs = collectSubcmds()
	if !bytes.Equal(subcmds, []byte{controllers.SwitchProSubcmdSPIRead, controllers.SwitchProSubcmdSPIRead}) || !bytes.Equal(seqs, []byte{0, 1}) {
		t.Fatalf("expected SPIRead retry seq=1, got subcmds=%x seqs=%v", subcmds, seqs)
	}

	// 4. Valid 0x21 ACK for 0x10 calibrates sticks and advances immediately to step 1 (0x03).
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0041, 0x0041, makeSwitchProSPICalReply(0x90)))
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	if !sess.slots[0].cal.Valid || sess.slots[0].initStep != 1 {
		t.Fatalf("expected valid calibration and step=1, got calValid=%t step=%d", sess.slots[0].cal.Valid, sess.slots[0].initStep)
	}
	subcmds, seqs = collectSubcmds()
	want := []byte{controllers.SwitchProSubcmdSPIRead, controllers.SwitchProSubcmdSPIRead, controllers.SwitchProSubcmdSetMode}
	if !bytes.Equal(subcmds, want) || !bytes.Equal(seqs, []byte{0, 1, 2}) {
		t.Fatalf("expected SetMode seq=2 after 0x10 ACK, got subcmds=%x seqs=%v", subcmds, seqs)
	}

	// 5. Simulate dropped reply on step 1: advance clock -> retries 0x03 (seq=3), then ACK -> sends 0x30 (seq=4).
	now = now.Add(switchProSubcmdRetryInterval)
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x04, 0x00})
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0041, 0x0041, makeSwitchProSubcmdReply(0x80, controllers.SwitchProSubcmdSetMode, nil)))
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}

	// 6. Valid 0x21 ACK for 0x30 completes init.
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0041, 0x0041, makeSwitchProSubcmdReply(0x80, controllers.SwitchProSubcmdSetPlayerLD, nil)))
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	if sess.slots[0].initStep != initStepDone {
		t.Fatalf("expected initStepDone after step 2 ACK, got %d", sess.slots[0].initStep)
	}

	// 7. Bounded max attempts: if a second controller ignores step 0 replies
	// forever, after switchProMaxStepAttempts it gives up on step 0 and advances.
	swAddr2, _ := l2cap.ParseBDAddr("FF:22:33:44:55:66")
	_ = bonds.Upsert(l2cap.BondRecord{Addr: swAddr2, LinkKey: [16]byte{9}, Name: "Pro Controller", Kind: controllers.KindSwitchPro, Slot: 1})
	conn2 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x42, 0x00}, swAddr2[:]...)
	conn2 = append(conn2, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, conn2)
	openIncomingHIDChannels(bus, 0x0042, 0x0060, 0x0061)
	for range 8 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x42, 0x00, 0x08, 0x00})
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	for range switchProMaxStepAttempts {
		now = now.Add(switchProSubcmdRetryInterval)
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x42, 0x00, 0x04, 0x00})
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if sess.slots[1].initStep != 1 {
		t.Fatalf("expected unanswered step 0 to give up after %d attempts and advance to step 1, got step=%d",
			switchProMaxStepAttempts, sess.slots[1].initStep)
	}
}

func TestDualSenseNamedWirelessControllerUnlocksViaSDPPnP(t *testing.T) {
	bus := newHIDPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	bonds := l2cap.NewBondStore()
	now := time.Unix(700, 0)
	sess, err := NewSession(tr, bonds, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	var slotKinds []controllers.Kind
	var gotState controllers.State
	sess.OnSlotChanged = func(_ uint8, connected bool, kind controllers.Kind, _ string) {
		if connected {
			slotKinds = append(slotKinds, kind)
		}
	}
	sess.OnGamepadState = func(_ uint8, kind controllers.Kind, st controllers.State) {
		if kind == controllers.KindDualSense {
			gotState = st
		}
	}

	dsAddr, _ := l2cap.ParseBDAddr("88:11:22:33:44:55")
	_ = bonds.Upsert(l2cap.BondRecord{Addr: dsAddr, LinkKey: [16]byte{0x42}})

	// Connect and open HID Control (0x0011) + Interrupt (0x0013) BEFORE
	// Remote Name + SDP complete, while Kind is still KindUnknown.
	connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, dsAddr[:]...)
	connOK = append(connOK, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK)
	openIncomingHIDChannels(bus, 0x0041, 0x0050, 0x0051)
	for range 6 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x08, 0x00})
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}

	// Must NOT send DualShock 4 unlock (0x02) while Kind is still KindUnknown.
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 10 && binary.LittleEndian.Uint16(pkt[6:8]) == 0x0050 && pkt[8] == hidpGetReportFeature {
			t.Fatalf("sent premature unlock feature report %#x before SDP resolved", pkt[9])
		}
	}

	// Now deliver ambiguous Remote Name "Wireless Controller", triggering SDP PnP query.
	nameOK := make([]byte, 2+7+len("Wireless Controller")+1)
	nameOK[0] = hci.EventRemoteNameRequestComplete
	nameOK[1] = byte(len(nameOK) - 2)
	copy(nameOK[3:9], dsAddr[:])
	copy(nameOK[9:], "Wireless Controller")
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, nameOK)
	for range 3 {
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}

	// Complete the outgoing SDP channel setup and respond with Sony DualSense VID:PID (054c:0ce6).
	var sdpSigID byte
	var localSDPCID uint16
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 16 && binary.LittleEndian.Uint16(pkt[6:8]) == l2cap.CIDSignaling &&
			pkt[8] == 0x02 && binary.LittleEndian.Uint16(pkt[12:14]) == l2cap.PSMSDP {
			sdpSigID = pkt[9]
			localSDPCID = binary.LittleEndian.Uint16(pkt[14:16])
		}
	}
	if localSDPCID == 0 {
		t.Fatal("expected outgoing SDP L2CAP ConnReq")
	}
	const remoteSDPCID uint16 = 0x0090
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeHIDACL(0x0041, l2cap.CIDSignaling, []byte{0x03, sdpSigID, 8, 0, byte(remoteSDPCID), byte(remoteSDPCID >> 8), byte(localSDPCID), byte(localSDPCID >> 8), 0, 0, 0, 0}),
		makeHIDACL(0x0041, l2cap.CIDSignaling, []byte{0x04, 15, 4, 0, byte(localSDPCID), byte(localSDPCID >> 8), 0, 0}),
		makeHIDACL(0x0041, l2cap.CIDSignaling, []byte{0x05, 1, 6, 0, byte(localSDPCID), byte(localSDPCID >> 8), 0, 0, 0, 0}),
	)
	for range 4 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x08, 0x00})
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}

	var sdpTxID uint16
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 13 && binary.LittleEndian.Uint16(pkt[6:8]) == remoteSDPCID && pkt[8] == 0x06 {
			sdpTxID = binary.BigEndian.Uint16(pkt[9:11])
		}
	}
	if sdpTxID == 0 {
		t.Fatal("expected outgoing SDP ServiceSearchAttributeRequest")
	}
	attrList := []byte{0x35, 0x0e, 0x35, 0x0c, 0x09, 0x02, 0x01, 0x09, 0x05, 0x4c, 0x09, 0x02, 0x02, 0x09, 0x0c, 0xe6}
	rsp := make([]byte, 5+2+len(attrList)+1)
	rsp[0] = 0x07
	binary.BigEndian.PutUint16(rsp[1:3], sdpTxID)
	binary.BigEndian.PutUint16(rsp[3:5], uint16(2+len(attrList)+1))
	binary.BigEndian.PutUint16(rsp[5:7], uint16(len(attrList)))
	copy(rsp[7:], attrList)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x08, 0x00})
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0041, localSDPCID, rsp))
	for range 6 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x08, 0x00})
		if err := sess.Service(); err != nil {
			t.Fatal(err)
		}
	}

	var sawDSUnlock, sawDSLED bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) < 10 {
			continue
		}
		cid := binary.LittleEndian.Uint16(pkt[6:8])
		hid := pkt[8:]
		if cid == 0x0050 && len(hid) >= 2 && hid[0] == hidpGetReportFeature && hid[1] == controllers.DualSenseFeatureCalib {
			sawDSUnlock = true
		}
		if cid == 0x0051 && len(hid) == 79 && hid[0] == hidpDataOutput && hid[1] == 0x31 {
			sawDSLED = true
		}
	}
	if !sawDSUnlock || !sawDSLED {
		t.Fatalf("expected DualSense 0x05 unlock and 0x31 LED after SDP PnP resolution (unlock=%t led=%t)", sawDSUnlock, sawDSLED)
	}
	if len(slotKinds) < 2 || slotKinds[len(slotKinds)-1] != controllers.KindDualSense {
		t.Fatalf("expected OnSlotChanged update to KindDualSense after SDP, got %v", slotKinds)
	}

	// Verify 0x31 DualSense input reports decode cleanly now!
	dsPkt := make([]byte, 79)
	dsPkt[0] = hidpDataInput
	dsPkt[1] = controllers.DualSenseBTReportFull
	dsPkt[3], dsPkt[4], dsPkt[5], dsPkt[6] = 0x80, 0x80, 0x80, 0x80
	dsPkt[10] = 0x08 | 0x20 // Cross (South)
	dsSum := crc32.Update(0, crc32.IEEETable, []byte{0xa1})
	dsSum = crc32.Update(dsSum, crc32.IEEETable, dsPkt[1:75])
	binary.LittleEndian.PutUint32(dsPkt[75:79], dsSum)
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeHIDACL(0x0041, 0x0041, dsPkt))
	if err := sess.Service(); err != nil {
		t.Fatal(err)
	}
	if gotState.Buttons != controllers.ButtonSouth {
		t.Fatalf("expected decoded ButtonSouth on DualSense 0x31 report, got %+v", gotState)
	}
}
