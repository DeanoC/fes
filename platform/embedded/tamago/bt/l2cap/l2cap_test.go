// SPDX-License-Identifier: GPL-3.0-or-later
package l2cap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type l2capMockPipe struct {
	ep           usb.EndpointDescriptor
	inQueue      [][]byte
	inRepeat     []byte
	outLog       [][]byte
	noOutLog     bool
	writeStalled bool
}

func (p *l2capMockPipe) DeviceAddress() uint8              { return 2 }
func (p *l2capMockPipe) Endpoint() usb.EndpointDescriptor  { return p.ep }
func (p *l2capMockPipe) SetIntervalOverride(time.Duration) {}
func (p *l2capMockPipe) Close() error                      { return nil }
func (p *l2capMockPipe) WriteDone() (bool, error)          { return !p.writeStalled, nil }
func (p *l2capMockPipe) Write(data []byte) error {
	if !p.noOutLog {
		p.outLog = append(p.outLog, append([]byte(nil), data...))
	}
	return nil
}
func (p *l2capMockPipe) PollInto(dst []byte) (int, error) {
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

type l2capMockBus struct {
	dev            usb.BusDevice
	evPipe         *l2capMockPipe
	aclInPipe      *l2capMockPipe
	aclOutPipe     *l2capMockPipe
	cmds           []uint16
	lastParams     map[uint16][]byte
	failCreate     bool
	failInquiry    bool
	failName       bool
	failCancelConn bool
	failCancelInq  bool
	manualAuth     bool
	scanEnable     byte
}

func newL2CAPMockBus() *l2capMockBus {
	dev := usb.BusDevice{
		Address: 2,
		Speed:   usb.SpeedHigh,
		Descriptor: usb.DeviceDescriptor{
			VendorID: 0x0a12, ProductID: 0x0001, Class: 0xe0, SubClass: 0x01, Protocol: 0x01,
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
	return &l2capMockBus{dev: dev, lastParams: map[uint16][]byte{}}
}

func (b *l2capMockBus) Service() error                   { return nil }
func (b *l2capMockBus) NextEvent() (usb.HostEvent, bool) { return usb.HostEvent{}, false }
func (b *l2capMockBus) Devices() []usb.BusDevice         { return []usb.BusDevice{b.dev} }
func (b *l2capMockBus) Ready() <-chan struct{}           { return nil }
func (b *l2capMockBus) OpenPipe(_ uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	p := &l2capMockPipe{ep: ep}
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
func (b *l2capMockBus) ControlTransfer(_ uint8, reqType, req byte, _, _ uint16, data []byte) (int, error) {
	if reqType == 0x20 && req == 0 && len(data) >= 3 {
		op := binary.LittleEndian.Uint16(data[0:2])
		if op == hci.OpcodeCreateConnection && b.failCreate {
			b.failCreate = false
			return 0, errors.New("injected CreateConnection error")
		}
		if op == hci.OpcodeInquiry && b.failInquiry {
			b.failInquiry = false
			// Simulate controller returning Command Disallowed (0x0c) in CommandStatus.
			b.evPipe.inQueue = append(b.evPipe.inQueue, []byte{hci.EventCommandStatus, 4, 0x0c, 1, byte(op), byte(op >> 8)})
			return len(data), nil
		}
		if op == hci.OpcodeRemoteNameRequest && b.failName {
			b.evPipe.inQueue = append(b.evPipe.inQueue, []byte{hci.EventCommandStatus, 4, 0x0c, 1, byte(op), byte(op >> 8)})
			return len(data), nil
		}
		if op == hci.OpcodeCreateConnectionCancel && b.failCancelConn {
			b.failCancelConn = false
			b.cmds = append(b.cmds, op)
			b.evPipe.inQueue = append(b.evPipe.inQueue, []byte{hci.EventCommandComplete, 4, 1, byte(op), byte(op >> 8), 0x0c})
			return len(data), nil
		}
		if op == hci.OpcodeInquiryCancel && b.failCancelInq {
			b.failCancelInq = false
			b.cmds = append(b.cmds, op)
			b.evPipe.inQueue = append(b.evPipe.inQueue, []byte{hci.EventCommandComplete, 4, 1, byte(op), byte(op >> 8), 0x0c})
			return len(data), nil
		}
		b.cmds = append(b.cmds, op)
		b.lastParams[op] = append([]byte(nil), data[3:]...)
		if op == hci.OpcodeWriteScanEnable && len(data) >= 4 {
			b.scanEnable = data[3]
		}
		if op == hci.OpcodeAuthenticationRequested && len(data) >= 5 {
			b.evPipe.inQueue = append(b.evPipe.inQueue, []byte{hci.EventCommandStatus, 4, 0x00, 1, byte(op), byte(op >> 8)})
			if !b.manualAuth {
				b.evPipe.inQueue = append(b.evPipe.inQueue, []byte{hci.EventAuthenticationComplete, 3, 0x00, data[3], data[4]})
			}
			return len(data), nil
		}
		if op == hci.OpcodeSetConnectionEncryption && len(data) >= 6 {
			b.evPipe.inQueue = append(b.evPipe.inQueue, []byte{hci.EventCommandStatus, 4, 0x00, 1, byte(op), byte(op >> 8)})
			if !b.manualAuth {
				b.evPipe.inQueue = append(b.evPipe.inQueue, []byte{hci.EventEncryptionChange, 4, 0x00, data[3], data[4], data[5]})
			}
			return len(data), nil
		}
		ev := []byte{hci.EventCommandComplete, 4, 1, byte(op), byte(op >> 8), 0}
		if op == hci.OpcodeReadLocalVersion {
			ev = []byte{hci.EventCommandComplete, 12, 1, byte(op), byte(op >> 8), 0, 9, 0, 0, 9, 0x0a, 0, 0x22, 0x11}
		} else if op == hci.OpcodeReadBufferSize {
			ev = []byte{hci.EventCommandComplete, 11, 1, byte(op), byte(op >> 8), 0, 0x00, 0x01, 0x40, 0x08, 0x00, 0x04, 0x00}
		} else if op == hci.OpcodeReadBDAddr {
			ev = []byte{hci.EventCommandComplete, 10, 1, byte(op), byte(op >> 8), 0, 1, 2, 3, 4, 5, 6}
		}
		b.evPipe.inQueue = append(b.evPipe.inQueue, ev)
	}
	return len(data), nil
}

func makeACLPacket(handle uint16, pb uint8, cid uint16, body []byte) []byte {
	l2 := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint16(l2[0:2], uint16(len(body)))
	binary.LittleEndian.PutUint16(l2[2:4], cid)
	copy(l2[4:], body)
	acl := make([]byte, 4+len(l2))
	binary.LittleEndian.PutUint16(acl[0:2], (handle&0x0fff)|uint16(pb&3)<<12)
	binary.LittleEndian.PutUint16(acl[2:4], uint16(len(l2)))
	copy(acl[4:], l2)
	return acl
}

func TestBondStoreJSONRoundTripAndBounds(t *testing.T) {
	addr, err := ParseBDAddr("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("ParseBDAddr: %v", err)
	}
	if addr.String() != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("BDAddr.String=%q", addr.String())
	}
	store := NewBondStore()
	rec := BondRecord{
		Addr:      addr,
		LinkKey:   [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Name:      "DualSense Wireless Controller",
		VendorID:  0x054c,
		ProductID: 0x0ce6,
		Kind:      controllers.KindDualSense,
		Slot:      0,
	}
	if err := store.Upsert(rec); err != nil || !store.Dirty() {
		t.Fatalf("Upsert: err=%v dirty=%t", err, store.Dirty())
	}
	raw, err := store.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	store.ClearDirty()
	if store.Dirty() {
		t.Fatal("ClearDirty failed")
	}

	reloaded := NewBondStore()
	if err := reloaded.UnmarshalJSON(raw); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	got, ok := reloaded.Lookup(addr)
	if !ok || got.LinkKey != rec.LinkKey || got.Name != rec.Name || got.Kind != controllers.KindDualSense {
		t.Fatalf("reloaded mismatch: ok=%t got=%+v", ok, got)
	}
	if !reloaded.Remove(addr) || len(reloaded.List()) != 0 {
		t.Fatal("Remove failed")
	}
}

func TestSSPFailClosedOutsidePairingAndReconnectL2CAPZeroAlloc(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	bonds := NewBondStore()
	mgr, err := NewManager(tr, bonds, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	stranger, _ := ParseBDAddr("11:22:33:44:55:66")

	// 1. Outside pairing mode, unknown Connection Request & User Confirmation MUST be rejected!
	connReq := append([]byte{hci.EventConnectionRequest, 10}, stranger[:]...)
	connReq = append(connReq, 0x08, 0x25, 0x00, 0x01) // Gamepad CoD, ACL
	userConf := append([]byte{hci.EventUserConfirmationRequest, 10}, stranger[:]...)
	userConf = append(userConf, 0, 0, 0, 0)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connReq, userConf)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if _, rejected := bus.lastParams[hci.OpcodeRejectConnectionRequest]; !rejected {
		t.Fatal("unknown device was not rejected outside pairing mode")
	}
	if _, negConf := bus.lastParams[hci.OpcodeUserConfirmNegReply]; !negConf {
		t.Fatal("SSP UserConfirmation was not rejected outside pairing mode")
	}

	// 2. Enable Pairing Mode -> Inquiry discovers DualSense -> SSP Just Works -> LinkKey saved!
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil || !mgr.PairingActive() {
		t.Fatalf("SetPairingMode(true): %v", err)
	}
	// Extended Inquiry Result with Peripheral/Gamepad CoD 0x002508
	eir := make([]byte, 2+15)
	eir[0] = hci.EventExtendedInquiryResult
	eir[1] = 15
	eir[2] = 1
	copy(eir[3:9], stranger[:])
	eir[9] = 1
	eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if _, created := bus.lastParams[hci.OpcodeCreateConnection]; !created {
		t.Fatal("pairing mode did not initiate CreateConnection for Gamepad CoD")
	}

	// Connection Complete establishes the pairing session before SSP key events arrive.
	ioCap := append([]byte{hci.EventIOCapabilityRequest, 6}, stranger[:]...)
	linkKey := [16]byte{0xaa, 0xbb, 0xcc, 0xdd, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	lkNotif := append([]byte{hci.EventLinkKeyNotification, 23}, stranger[:]...)
	lkNotif = append(lkNotif, linkKey[:]...)
	lkNotif = append(lkNotif, 0x04)
	connComp := []byte{hci.EventConnectionComplete, 11, 0, 0x42, 0x00}
	connComp = append(connComp, stranger[:]...)
	connComp = append(connComp, 0x01, 0x01)
	nameEv := make([]byte, 2+7+32)
	nameEv[0] = hci.EventRemoteNameRequestComplete
	nameEv[1] = 39
	nameEv[2] = 0
	copy(nameEv[3:9], stranger[:])
	copy(nameEv[9:], "DualSense Wireless Controller")

	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connComp, ioCap, userConf, lkNotif, nameEv)
	var resolvedKind controllers.Kind
	mgr.OnNameResolved = func(_ uint16, _ BDAddr, _ string, kind controllers.Kind) {
		resolvedKind = kind
	}
	for range 3 {
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if _, acceptedConf := bus.lastParams[hci.OpcodeUserConfirmRequestReply]; !acceptedConf {
		t.Fatal("SSP UserConfirmation not accepted during pairing mode")
	}
	if resolvedKind != controllers.KindDualSense {
		t.Fatalf("resolvedKind=%v, want DualSense", resolvedKind)
	}
	rec, ok := mgr.Bonds().Lookup(stranger)
	if !ok || rec.LinkKey != linkKey || rec.Kind != controllers.KindDualSense || !mgr.Bonds().Dirty() {
		t.Fatalf("bond not saved cleanly: ok=%t rec=%+v", ok, rec)
	}

	// 3. Pairing window expires automatically -> returns to Page Scan only.
	now = now.Add(31 * time.Second)
	if err := mgr.Service(); err != nil || mgr.PairingActive() {
		t.Fatalf("pairing window did not expire: err=%v active=%t", err, mgr.PairingActive())
	}

	// 4. Complete L2CAP signaling handshake for PSM 0x0011 (Control) and 0x0013 (Interrupt)!
	var openPSMs []uint16
	mgr.OnChannelOpen = func(_ uint16, _ BDAddr, _ string, _ controllers.Kind, psm uint16) {
		openPSMs = append(openPSMs, psm)
	}
	// Remote accepts our outgoing PSM 0x0011 ConnReq (localCID=0x0040 -> remoteCID=0x0050)
	sigConnRspCtrl := []byte{sigConnRsp, 1, 8, 0, 0x50, 0x00, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}
	sigCfgReqCtrl := []byte{sigConfigReq, 2, 4, 0, 0x40, 0x00, 0x00, 0x00}
	sigCfgRspCtrl := []byte{sigConfigRsp, 2, 6, 0, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigConnRspCtrl),
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigCfgReqCtrl),
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigCfgRspCtrl),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	// Manager automatically initiated PSM 0x0013 (localCID=0x0041); accept & configure it.
	sigConnRspIntr := []byte{sigConnRsp, 3, 8, 0, 0x51, 0x00, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00}
	sigCfgReqIntr := []byte{sigConfigReq, 4, 4, 0, 0x41, 0x00, 0x00, 0x00}
	sigCfgRspIntr := []byte{sigConfigRsp, 4, 6, 0, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigConnRspIntr),
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigCfgReqIntr),
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigCfgRspIntr),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if len(openPSMs) != 2 || openPSMs[0] != PSMHIDControl || openPSMs[1] != PSMHIDInterrupt {
		t.Fatalf("unexpected openPSMs=%x", openPSMs)
	}

	// 5. Verify fragmented multi-ACL L2CAP reassembly AND zero-alloc hot path!
	fullHID := make([]byte, 79)
	fullHID[0] = 0xa1
	fullHID[1] = 0x31
	for i := 2; i < len(fullHID); i++ {
		fullHID[i] = byte(i)
	}
	var gotHID []byte
	mgr.OnHIDInterrupt = func(_ uint16, payload []byte) {
		gotHID = append(gotHID[:0], payload...)
	}
	// Split 79-byte payload (+4B L2CAP header = 83B) across two ACL fragments (40B + 43B).
	l2Full := make([]byte, 4+len(fullHID))
	binary.LittleEndian.PutUint16(l2Full[0:2], uint16(len(fullHID)))
	binary.LittleEndian.PutUint16(l2Full[2:4], 0x0041) // localCID for HID Interrupt
	copy(l2Full[4:], fullHID)
	frag1 := make([]byte, 4+40)
	binary.LittleEndian.PutUint16(frag1[0:2], 0x0042|uint16(hci.ACLBoundaryFirstAutoFlush)<<12)
	binary.LittleEndian.PutUint16(frag1[2:4], 40)
	copy(frag1[4:], l2Full[:40])
	frag2 := make([]byte, 4+43)
	binary.LittleEndian.PutUint16(frag2[0:2], 0x0042|uint16(hci.ACLBoundaryContinuing)<<12)
	binary.LittleEndian.PutUint16(frag2[2:4], 43)
	copy(frag2[4:], l2Full[40:])
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, frag1, frag2)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotHID, fullHID) {
		t.Fatalf("fragmented L2CAP reassembly mismatch len=%d/%d", len(gotHID), len(fullHID))
	}

	// Verify PB=0 (non-automatically-flushable first fragment) works for both
	// single-packet and multi-fragment L2CAP PDUs.
	gotHID = nil
	singleNonFlush := makeACLPacket(0x0042, hci.ACLBoundaryFirstNonFlush, 0x0041, fullHID)
	binary.LittleEndian.PutUint16(frag1[0:2], 0x0042|uint16(hci.ACLBoundaryFirstNonFlush)<<12)
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, singleNonFlush)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotHID, fullHID) {
		t.Fatalf("single PB=0 L2CAP PDU mismatch len=%d/%d", len(gotHID), len(fullHID))
	}
	gotHID = nil
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, frag1, frag2)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotHID, fullHID) {
		t.Fatalf("fragmented PB=0 L2CAP reassembly mismatch len=%d/%d", len(gotHID), len(fullHID))
	}

	// Verify SendChannel + single-packet HID Interrupt RX allocate 0 bytes!
	bus.aclOutPipe.noOutLog = true
	singleACL := makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, 0x0041, fullHID)
	bus.aclInPipe.inRepeat = singleACL
	var intrSeen int
	mgr.OnHIDInterrupt = func(_ uint16, payload []byte) {
		if len(payload) == 79 && payload[0] == 0xa1 {
			intrSeen++
		}
	}
	allocs := testing.AllocsPerRun(100, func() {
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 0 || intrSeen == 0 {
		t.Fatalf("L2CAP HID Interrupt hot path allocated %.2f allocs/run (seen=%d), want 0", allocs, intrSeen)
	}
}

func TestManagerResumesInquiryOnCreateConnectionFailure(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(200, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	countInquiry := func() int {
		n := 0
		for _, op := range bus.cmds {
			if op == hci.OpcodeInquiry {
				n++
			}
		}
		return n
	}
	if countInquiry() != 1 {
		t.Fatalf("expected 1 initial StartInquiry, got %d", countInquiry())
	}

	dev1, _ := ParseBDAddr("10:20:30:40:50:60")
	dev2, _ := ParseBDAddr("10:20:30:40:50:61")
	makeEIR := func(addr BDAddr) []byte {
		eir := make([]byte, 2+15)
		eir[0] = hci.EventExtendedInquiryResult
		eir[1] = 15
		eir[2] = 1
		copy(eir[3:9], addr[:])
		eir[9] = 1
		eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00 // Gamepad CoD
		return eir
	}

	// 1. Synchronous CreateConnection error must clear m.connecting and restart Inquiry.
	bus.failCreate = true
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, makeEIR(dev1))
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if mgr.connecting {
		t.Fatal("m.connecting remained true after synchronous CreateConnection failure")
	}
	if countInquiry() != 2 {
		t.Fatalf("expected StartInquiry to resume after sync CreateConnection failure (count=%d)", countInquiry())
	}

	// 2. Async failed ConnectionComplete (status=0x04 Page Timeout) must clear
	// m.connecting, restart Inquiry, and allow dev2 to connect.
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, makeEIR(dev1))
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if !mgr.connecting {
		t.Fatal("expected m.connecting=true while waiting on ConnectionComplete")
	}
	failConn := []byte{hci.EventConnectionComplete, 11, 0x04, 0x00, 0x00} // Page Timeout
	failConn = append(failConn, dev1[:]...)
	failConn = append(failConn, 0x01, 0x00)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, failConn)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if mgr.connecting {
		t.Fatal("m.connecting remained true after async failed ConnectionComplete")
	}
	if countInquiry() != 3 {
		t.Fatalf("expected StartInquiry to resume after async failed ConnectionComplete (count=%d)", countInquiry())
	}

	// Verify dev2 can now be discovered and initiate CreateConnection cleanly.
	delete(bus.lastParams, hci.OpcodeCreateConnection)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, makeEIR(dev2))
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeCreateConnection]; len(got) < 6 || !bytes.Equal(got[:6], dev2[:]) {
		t.Fatalf("expected CreateConnection for dev2 after recovery, got %x", got)
	}
}

func TestSetPairingModeRollsBackOnInquiryFailure(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return time.Unix(300, 0) })
	if err != nil {
		t.Fatal(err)
	}

	bus.failInquiry = true
	if err := mgr.SetPairingMode(true, 30*time.Second); err == nil {
		t.Fatal("expected SetPairingMode(true) error when StartInquiry fails")
	}
	if mgr.PairingActive() || !mgr.pairingUntil.IsZero() {
		t.Fatalf("pairing state not rolled back: active=%t until=%v", mgr.PairingActive(), mgr.pairingUntil)
	}
	if bus.scanEnable != hci.ScanPageOnly {
		t.Fatalf("radio scanEnable=%#02x, want ScanPageOnly (%#02x)", bus.scanEnable, hci.ScanPageOnly)
	}

	// Verify unknown devices remain rejected fail-closed after inquiry failure rollback.
	stranger, _ := ParseBDAddr("22:33:44:55:66:77")
	connReq := append([]byte{hci.EventConnectionRequest, 10}, stranger[:]...)
	connReq = append(connReq, 0x08, 0x25, 0x00, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connReq)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeRejectConnectionRequest]; len(got) < 6 || !bytes.Equal(got[:6], stranger[:]) {
		t.Fatalf("expected stranger to be rejected fail-closed after failed SetPairingMode, got %x", got)
	}
}

func TestManagerResumesInquiryOnSSPFailure(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(400, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	peer, _ := ParseBDAddr("33:44:55:66:77:88")
	connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x44, 0x00}, peer[:]...)
	connOK = append(connOK, 0x01, 0x01)
	sspFail := append([]byte{hci.EventSimplePairingComplete, 7, 0x05}, peer[:]...) // Authentication Failure
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK, sspFail)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	inquiries := 0
	for _, op := range bus.cmds {
		if op == hci.OpcodeInquiry {
			inquiries++
		}
	}
	if inquiries != 2 {
		t.Fatalf("expected StartInquiry to resume after EventSimplePairingComplete failure (count=%d)", inquiries)
	}
}

func TestOpenChannelRollsBackAndRetriesOnACLQueueFull(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return time.Unix(500, 0) })
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	peer, _ := ParseBDAddr("44:55:66:77:88:99")
	eir := make([]byte, 2+15)
	eir[0] = hci.EventExtendedInquiryResult
	eir[1] = 15
	eir[2] = 1
	copy(eir[3:9], peer[:])
	eir[9] = 1
	eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	// Stall ACL OUT and fill all 16 queued ring entries (plus 1 in-flight).
	bus.aclOutPipe.writeStalled = true
	for range 17 {
		if err := tr.SendACL(0x0041, hci.ACLBoundaryFirstAutoFlush, []byte{0xaa, 0xbb}); err != nil {
			t.Fatalf("prefill SendACL failed early: %v", err)
		}
	}
	bus.aclOutPipe.outLog = nil

	// Outgoing ConnectionComplete during pairing attempts OpenChannel(PSMHIDControl), which
	// fails with ACL TX queue full: channel slot MUST roll back and queue retry via pendingOpenPSM.
	connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x45, 0x00}, peer[:]...)
	connOK = append(connOK, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	conn := mgr.findConnByHandle(0x0045)
	if conn == nil {
		t.Fatal("missing ACL conn 0x0045")
	}
	if conn.findByPSM(PSMHIDControl) != nil {
		t.Fatal("failed OpenChannel left PSMHIDControl allocated instead of rolling back")
	}
	if conn.pendingOpenPSM != PSMHIDControl {
		t.Fatalf("expected pendingOpenPSM=%#04x, got %#04x", PSMHIDControl, conn.pendingOpenPSM)
	}

	// Unstall ACL OUT, replenish credits across drains, and pump Service() -> must retry ConnReq!
	bus.aclOutPipe.writeStalled = false
	for range 24 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x08, 0x00})
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if conn.pendingOpenPSM != 0 || conn.findByPSM(PSMHIDControl) == nil {
		t.Fatalf("expected OpenChannel retry to succeed: pending=%#04x ch=%v", conn.pendingOpenPSM, conn.findByPSM(PSMHIDControl))
	}
	var sawConnReq bool
	for _, pkt := range bus.aclOutPipe.outLog {
		// 4B ACL hdr + 4B L2CAP hdr + 8B sigConnReq (PSM 0x0011)
		if len(pkt) == 16 &&
			(binary.LittleEndian.Uint16(pkt[0:2])&0x0fff) == 0x0045 &&
			binary.LittleEndian.Uint16(pkt[6:8]) == CIDSignaling &&
			pkt[8] == sigConnReq &&
			binary.LittleEndian.Uint16(pkt[12:14]) == PSMHIDControl {
			sawConnReq = true
		}
	}
	if !sawConnReq {
		t.Fatalf("expected retried L2CAP sigConnReq for PSMHIDControl, outLog=%x", bus.aclOutPipe.outLog)
	}
}

func TestConfigRequestRetriesAfterACLQueueFull(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return time.Unix(600, 0) })
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	peer, _ := ParseBDAddr("55:66:77:88:99:AA")
	eir := make([]byte, 2+15)
	eir[0] = hci.EventExtendedInquiryResult
	eir[1] = 15
	eir[2] = 1
	copy(eir[3:9], peer[:])
	eir[9] = 1
	eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
	connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x46, 0x00}, peer[:]...)
	connOK = append(connOK, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir, connOK)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	var opened []uint16
	mgr.OnChannelOpen = func(_ uint16, _ BDAddr, _ string, _ controllers.Kind, psm uint16) {
		opened = append(opened, psm)
	}

	// Stall ACL OUT and fill all 16 queued ring entries (plus 1 in-flight) before sigConnRsp arrives.
	bus.aclOutPipe.writeStalled = true
	for range 17 {
		if err := tr.SendACL(0x0041, hci.ACLBoundaryFirstAutoFlush, []byte{0x12, 0x34}); err != nil {
			t.Fatalf("prefill SendACL failed early: %v", err)
		}
	}
	bus.aclOutPipe.outLog = nil

	// Incoming sigConnRsp succeeds, triggers sigConfigReq -> drops on TX queue full -> must record pendingCfg.
	sigConnRspCtrl := []byte{sigConnRsp, 1, 8, 0, 0x50, 0x00, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0046, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigConnRspCtrl),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	conn := mgr.findConnByHandle(0x0046)
	ch := conn.findByPSM(PSMHIDControl)
	if ch == nil || !ch.pendingCfg {
		t.Fatalf("expected pendingCfg=true after dropped sigConfigReq, ch=%+v", ch)
	}

	// Unstall ACL OUT, replenish credits, and pump Service() -> must retry sigConfigReq.
	bus.aclOutPipe.writeStalled = false
	for range 24 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x08, 0x00})
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if ch.pendingCfg {
		t.Fatal("expected pendingCfg to clear after sigConfigReq retry")
	}
	var sawCfgReq bool
	for _, pkt := range bus.aclOutPipe.outLog {
		// 4B ACL hdr + 4B L2CAP hdr + 12B sigConfigReq
		if len(pkt) == 20 &&
			(binary.LittleEndian.Uint16(pkt[0:2])&0x0fff) == 0x0046 &&
			binary.LittleEndian.Uint16(pkt[6:8]) == CIDSignaling &&
			pkt[8] == sigConfigReq &&
			binary.LittleEndian.Uint16(pkt[12:14]) == 0x0050 {
			sawCfgReq = true
		}
	}
	if !sawCfgReq {
		t.Fatalf("expected retried sigConfigReq for remoteCID=0x0050, outLog=%x", bus.aclOutPipe.outLog)
	}

	// Finish handshake -> channel opens cleanly.
	sigCfgReqCtrl := []byte{sigConfigReq, 2, 4, 0, 0x40, 0x00, 0x00, 0x00}
	sigCfgRspCtrl := []byte{sigConfigRsp, 2, 6, 0, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0046, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigCfgReqCtrl),
		makeACLPacket(0x0046, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigCfgRspCtrl),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0] != PSMHIDControl || !ch.open {
		t.Fatalf("expected PSMHIDControl to open after retry, opened=%x ch=%+v", opened, ch)
	}
}

func TestPairingRejectsUnsupportedPeripheralConnection(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return time.Unix(700, 0) })
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	// Keyboards, combo devices, remotes, and non-HID classes are rejected fail-closed.
	for i, cod := range []uint32{0x000540, 0x00050c, 0x0005c0, 0x00058c, 0x002548, 0x5a020c} {
		peer := BDAddr{0x66, 0x55, 0x44, 0x33, 0x22, byte(i + 1)}
		req := append([]byte{hci.EventConnectionRequest, 10}, peer[:]...)
		req = append(req, byte(cod), byte(cod>>8), byte(cod>>16), 0x01)
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, req)
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
		if got := bus.lastParams[hci.OpcodeRejectConnectionRequest]; len(got) < 6 || !bytes.Equal(got[:6], peer[:]) {
			t.Fatalf("CoD %#06x: expected unsupported peripheral to be rejected, got %x", cod, got)
		}
	}

	// Gamepad CoD 0x002508 and pointing/mouse CoD 0x000580 are accepted during pairing.
	gamepad, _ := ParseBDAddr("77:66:55:44:33:22")
	hidReq := append([]byte{hci.EventConnectionRequest, 10}, gamepad[:]...)
	hidReq = append(hidReq, 0x08, 0x25, 0x00, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, hidReq)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeAcceptConnectionRequest]; len(got) < 6 || !bytes.Equal(got[:6], gamepad[:]) {
		t.Fatalf("expected gamepad CoD to be accepted during pairing, got %x", got)
	}
	mouse, _ := ParseBDAddr("77:66:55:44:33:23")
	mouseReq := append([]byte{hci.EventConnectionRequest, 10}, mouse[:]...)
	mouseReq = append(mouseReq, 0x80, 0x05, 0x00, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, mouseReq)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeAcceptConnectionRequest]; len(got) < 6 || !bytes.Equal(got[:6], mouse[:]) {
		t.Fatalf("expected mouse CoD to be accepted during pairing, got %x", got)
	}
}

func TestPairingInquiryConnectsOnlyGamepadJoystickOrMouse(t *testing.T) {
	for _, tc := range []struct {
		name string
		cod  uint32
		want bool
	}{
		{name: "gamepad", cod: 0x002508, want: true},
		{name: "joystick", cod: 0x002504, want: true},
		{name: "mouse", cod: 0x000580, want: true},
		{name: "mouse minor class", cod: 0x002580, want: true},
		{name: "keyboard", cod: 0x000540},
		{name: "remote control", cod: 0x00050c},
		{name: "pointing remote combo", cod: 0x00058c},
		{name: "keyboard and mouse", cod: 0x0005c0},
		{name: "gamepad with keyboard flag", cod: 0x002548},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := newL2CAPMockBus()
			tr, err := hci.Open(bus, bus.dev)
			if err != nil {
				t.Fatal(err)
			}
			if err := tr.Initialize(nil); err != nil {
				t.Fatal(err)
			}
			mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return time.Unix(700, 0) })
			if err != nil {
				t.Fatal(err)
			}
			if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
				t.Fatal(err)
			}

			peer, _ := ParseBDAddr("88:77:66:55:44:33")
			eir := make([]byte, 2+15)
			eir[0] = hci.EventExtendedInquiryResult
			eir[1] = 15
			eir[2] = 1
			copy(eir[3:9], peer[:])
			eir[9] = 1
			eir[11], eir[12], eir[13] = byte(tc.cod), byte(tc.cod>>8), byte(tc.cod>>16)
			bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir)
			if err := mgr.Service(); err != nil {
				t.Fatal(err)
			}
			_, got := bus.lastParams[hci.OpcodeCreateConnection]
			if got != tc.want {
				t.Fatalf("CreateConnection present=%t, want %t for CoD %#06x", got, tc.want, tc.cod)
			}
			if tc.name == "mouse" {
				// Complete pairing -> persisted bond records KindMouse cleanly.
				connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, peer[:]...)
				connOK = append(connOK, 0x01, 0x00)
				linkKey := append([]byte{hci.EventLinkKeyNotification, 23}, peer[:]...)
				linkKey = append(linkKey, make([]byte, 16)...)
				linkKey = append(linkKey, 0x04)
				bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK, linkKey)
				if err := mgr.Service(); err != nil {
					t.Fatal(err)
				}
				if rec, ok := mgr.Bonds().Lookup(peer); !ok || rec.Kind != controllers.KindMouse {
					t.Fatalf("expected bonded mouse KindMouse, got ok=%t rec=%+v", ok, rec)
				}
			}
		})
	}
}

func TestDisconnectPurgesQueuedACLFrames(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return time.Unix(800, 0) })
	if err != nil {
		t.Fatal(err)
	}

	p1, _ := ParseBDAddr("88:11:22:33:44:55")
	p2, _ := ParseBDAddr("99:11:22:33:44:55")
	c1 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, p1[:]...)
	c1 = append(c1, 0x01, 0x01)
	c2 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x42, 0x00}, p2[:]...)
	c2 = append(c2, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, c1, c2)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	// Consume all 8 controller credits so subsequent ACL frames queue in txRing.
	bus.aclOutPipe.noOutLog = true
	for range 8 {
		if err := tr.SendACL(0x0042, hci.ACLBoundaryFirstAutoFlush, []byte{0x01}); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	bus.aclOutPipe.noOutLog = false
	bus.aclOutPipe.outLog = nil

	// Queue frames on both 0x0041 and 0x0042 while credits == 0.
	if err := tr.SendACL(0x0041, hci.ACLBoundaryFirstAutoFlush, []byte{0xaa}); err != nil {
		t.Fatal(err)
	}
	if err := tr.SendACL(0x0042, hci.ACLBoundaryFirstAutoFlush, []byte{0xbb}); err != nil {
		t.Fatal(err)
	}

	// Disconnect 0x0041 before replenishing credits -> 0x0041 queued frame MUST be purged!
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventDisconnectionComplete, 4, 0, 0x41, 0x00, 0x13})
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x42, 0x00, 0x08, 0x00})
	for range 4 {
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if len(bus.aclOutPipe.outLog) != 1 || (binary.LittleEndian.Uint16(bus.aclOutPipe.outLog[0][0:2])&0x0fff) != 0x0042 {
		t.Fatalf("expected only handle 0x0042 frame to transmit after 0x0041 disconnect, outLog=%x", bus.aclOutPipe.outLog)
	}
	if tr.ACLCredits() != 7 {
		t.Fatalf("credits=%d after 1 remaining TX (started 8), want 7", tr.ACLCredits())
	}
}

func TestManagerRetriesRemoteNameOnFailure(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(900, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	peer, _ := ParseBDAddr("AA:22:33:44:55:66")
	connEv := append([]byte{hci.EventConnectionComplete, 11, 0, 0x47, 0x00}, peer[:]...)
	connEv = append(connEv, 0x01, 0x01)
	nameFail := append([]byte{hci.EventRemoteNameRequestComplete, 7, 0x04}, peer[:]...) // Page Timeout
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connEv, nameFail)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	countNameReq := func() int {
		n := 0
		for _, op := range bus.cmds {
			if op == hci.OpcodeRemoteNameRequest {
				n++
			}
		}
		return n
	}
	if countNameReq() != 1 {
		t.Fatalf("expected 1 initial RemoteNameRequest, got %d", countNameReq())
	}

	// Not yet past 250 ms backoff: no premature retry.
	now = now.Add(100 * time.Millisecond)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if countNameReq() != 1 {
		t.Fatalf("premature RemoteNameRequest retry before backoff: count=%d", countNameReq())
	}

	// Advance past backoff: retries and resolves DualSense!
	now = now.Add(200 * time.Millisecond)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if countNameReq() != 2 {
		t.Fatalf("expected RemoteNameRequest retry after backoff, got %d", countNameReq())
	}

	var resolved controllers.Kind
	mgr.OnNameResolved = func(_ uint16, _ BDAddr, _ string, kind controllers.Kind) {
		resolved = kind
	}
	nameOK := make([]byte, 2+7+32)
	nameOK[0] = hci.EventRemoteNameRequestComplete
	nameOK[1] = 39
	nameOK[2] = 0
	copy(nameOK[3:9], peer[:])
	copy(nameOK[9:], "DualSense Wireless Controller")
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, nameOK)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if resolved != controllers.KindDualSense {
		t.Fatalf("expected DualSense after name retry, got %v", resolved)
	}
}

func TestRejectsSSPAndLinkKeyOverwriteForBondedDeviceOutsidePairing(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	bonds := NewBondStore()
	peer, _ := ParseBDAddr("BB:22:33:44:55:66")
	origKey := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	_ = bonds.Upsert(BondRecord{Addr: peer, LinkKey: origKey, Name: "DualSense Wireless Controller", Kind: controllers.KindDualSense})

	mgr, err := NewManager(tr, bonds, func() time.Time { return time.Unix(1000, 0) })
	if err != nil {
		t.Fatal(err)
	}

	// While pairing mode is closed, SSP confirmation and link-key overwrite MUST be
	// rejected even if the peer presents a recorded bonded BD_ADDR.
	userConf := append([]byte{hci.EventUserConfirmationRequest, 10}, peer[:]...)
	userConf = append(userConf, 0, 0, 0, 0)
	newKey := [16]byte{0xff, 0xee, 0xdd, 0xcc, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	lkNotif := append([]byte{hci.EventLinkKeyNotification, 23}, peer[:]...)
	lkNotif = append(lkNotif, newKey[:]...)
	lkNotif = append(lkNotif, 0x04)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, userConf, lkNotif)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	if got := bus.lastParams[hci.OpcodeUserConfirmNegReply]; len(got) < 6 || !bytes.Equal(got[:6], peer[:]) {
		t.Fatalf("expected OpcodeUserConfirmNegReply for bonded peer outside pairing mode, got %x", got)
	}
	if _, accepted := bus.lastParams[hci.OpcodeUserConfirmRequestReply]; accepted {
		t.Fatal("accepted SSP UserConfirmation for bonded peer outside pairing mode")
	}
	rec, ok := bonds.Lookup(peer)
	if !ok || rec.LinkKey != origKey {
		t.Fatalf("bonded link key overwritten outside pairing mode: ok=%t key=%x", ok, rec.LinkKey)
	}
}

func TestDisconnectReclaimsSubmittedACLCreditsWithoutNumCompleted(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return time.Unix(1100, 0) })
	if err != nil {
		t.Fatal(err)
	}

	p1, _ := ParseBDAddr("12:34:56:78:9A:01")
	p2, _ := ParseBDAddr("12:34:56:78:9A:02")
	c1 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, p1[:]...)
	c1 = append(c1, 0x01, 0x01)
	c2 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x42, 0x00}, p2[:]...)
	c2 = append(c2, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, c1, c2)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	// Submit all 8 controller credits on handle 0x0041 without receiving any 0x13 completion.
	bus.aclOutPipe.noOutLog = true
	for range 8 {
		if err := tr.SendACL(0x0041, hci.ACLBoundaryFirstAutoFlush, []byte{0x01}); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if tr.ACLCredits() != 0 {
		t.Fatalf("expected 0 credits after 8 submissions, got %d", tr.ACLCredits())
	}
	bus.aclOutPipe.noOutLog = false
	bus.aclOutPipe.outLog = nil

	// Queue a frame on handle 0x0042 while credits == 0.
	if err := tr.SendACL(0x0042, hci.ACLBoundaryFirstAutoFlush, []byte{0x5a}); err != nil {
		t.Fatal(err)
	}

	// Disconnect 0x0041 with NO EventNumCompletedPackets -> must reclaim 0x0041's 8
	// credits and allow 0x0042's queued frame to transmit cleanly!
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventDisconnectionComplete, 4, 0, 0x41, 0x00, 0x13})
	for range 3 {
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if len(bus.aclOutPipe.outLog) != 1 || (binary.LittleEndian.Uint16(bus.aclOutPipe.outLog[0][0:2])&0x0fff) != 0x0042 {
		t.Fatalf("expected 0x0042 frame to transmit after reclaiming 0x0041 credits, outLog=%x", bus.aclOutPipe.outLog)
	}
	if tr.ACLCredits() != 7 {
		t.Fatalf("expected 7 remaining credits after reclaiming 8 and sending 1, got %d", tr.ACLCredits())
	}
}

func TestManagerResumesInquiryAfterPairingDisconnect(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1200, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	countInquiry := func() int {
		n := 0
		for _, op := range bus.cmds {
			if op == hci.OpcodeInquiry {
				n++
			}
		}
		return n
	}
	if countInquiry() != 1 {
		t.Fatalf("expected 1 initial StartInquiry, got %d", countInquiry())
	}

	dev1, _ := ParseBDAddr("22:33:44:55:66:77")
	eir := make([]byte, 2+15)
	eir[0] = hci.EventExtendedInquiryResult
	eir[1] = 15
	eir[2] = 1
	copy(eir[3:9], dev1[:])
	eir[9] = 1
	eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x48, 0x00}, dev1[:]...)
	connOK = append(connOK, 0x01, 0x01)
	disc := []byte{hci.EventDisconnectionComplete, 4, 0, 0x48, 0x00, 0x13}
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK, disc)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	if countInquiry() != 2 {
		t.Fatalf("expected StartInquiry to resume after pairing connection disconnects (count=%d)", countInquiry())
	}
}

func makeSDPPnPAttrList(vid, pid uint16) []byte {
	return []byte{
		0x35, 0x0e, // Outer DES (14 bytes)
		0x35, 0x0c, // Record DES (12 bytes)
		0x09, 0x02, 0x01, // AttrID 0x0201 (VendorID)
		0x09, byte(vid >> 8), byte(vid),
		0x09, 0x02, 0x02, // AttrID 0x0202 (ProductID)
		0x09, byte(pid >> 8), byte(pid),
	}
}

func makeSDPAttrRsp(txID uint16, attrChunk []byte, cont []byte) []byte {
	paramLen := 2 + len(attrChunk) + 1 + len(cont)
	pdu := make([]byte, 5+paramLen)
	pdu[0] = sdpServiceSearchAttrRsp
	binary.BigEndian.PutUint16(pdu[1:3], txID)
	binary.BigEndian.PutUint16(pdu[3:5], uint16(paramLen))
	binary.BigEndian.PutUint16(pdu[5:7], uint16(len(attrChunk)))
	copy(pdu[7:7+len(attrChunk)], attrChunk)
	pdu[7+len(attrChunk)] = byte(len(cont))
	copy(pdu[7+len(attrChunk)+1:], cont)
	return pdu
}

func TestSDPPnPDistinguishesDualSenseAndDualShock4NamedWirelessController(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	bonds := NewBondStore()
	now := time.Unix(1300, 0)
	mgr, err := NewManager(tr, bonds, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	var resolved []controllers.Kind
	mgr.OnNameResolved = func(_ uint16, _ BDAddr, _ string, kind controllers.Kind) {
		resolved = append(resolved, kind)
	}

	resolveViaSDP := func(handle uint16, addr BDAddr, vid, pid uint16, twoChunk bool) {
		t.Helper()
		_ = bonds.Upsert(BondRecord{Addr: addr, LinkKey: [16]byte{byte(handle)}})
		connOK := append([]byte{hci.EventConnectionComplete, 11, 0, byte(handle), byte(handle >> 8)}, addr[:]...)
		connOK = append(connOK, 0x01, 0x01)
		nameOK := make([]byte, 2+7+len("Wireless Controller")+1)
		nameOK[0] = hci.EventRemoteNameRequestComplete
		nameOK[1] = byte(len(nameOK) - 2)
		copy(nameOK[3:9], addr[:])
		copy(nameOK[9:], "Wireless Controller")
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK, nameOK)
		for range 4 {
			bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, byte(handle), byte(handle >> 8), 0x08, 0x00})
			if err := mgr.Service(); err != nil {
				t.Fatal(err)
			}
		}

		// Locate outgoing L2CAP ConnReq for PSMSDP on handle.
		var sdpReqSigID byte
		var localSDPCID uint16
		for _, pkt := range bus.aclOutPipe.outLog {
			if len(pkt) < 16 || (binary.LittleEndian.Uint16(pkt[0:2])&0x0fff) != handle {
				continue
			}
			if binary.LittleEndian.Uint16(pkt[6:8]) == CIDSignaling && pkt[8] == sigConnReq && binary.LittleEndian.Uint16(pkt[12:14]) == PSMSDP {
				sdpReqSigID = pkt[9]
				localSDPCID = binary.LittleEndian.Uint16(pkt[14:16])
			}
		}
		if localSDPCID == 0 {
			t.Fatalf("expected outgoing PSMSDP ConnReq for handle %#x", handle)
		}

		remoteSDPCID := uint16(0x0080 + handle)
		sdpConnRsp := []byte{sigConnRsp, sdpReqSigID, 8, 0, byte(remoteSDPCID), byte(remoteSDPCID >> 8), byte(localSDPCID), byte(localSDPCID >> 8), 0, 0, 0, 0}
		sdpCfgReq := []byte{sigConfigReq, 20, 4, 0, byte(localSDPCID), byte(localSDPCID >> 8), 0, 0}
		sdpCfgRsp := []byte{sigConfigRsp, 1, 6, 0, byte(localSDPCID), byte(localSDPCID >> 8), 0, 0, 0, 0}
		bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
			makeACLPacket(handle, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sdpConnRsp),
			makeACLPacket(handle, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sdpCfgReq),
			makeACLPacket(handle, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sdpCfgRsp),
		)
		for range 4 {
			bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, byte(handle), byte(handle >> 8), 0x08, 0x00})
			if err := mgr.Service(); err != nil {
				t.Fatal(err)
			}
		}

		lastSDPTxID := func() uint16 {
			var tx uint16
			for _, pkt := range bus.aclOutPipe.outLog {
				if len(pkt) >= 13 && (binary.LittleEndian.Uint16(pkt[0:2])&0x0fff) == handle && binary.LittleEndian.Uint16(pkt[6:8]) == remoteSDPCID {
					pdu := pkt[8:]
					if pdu[0] == sdpServiceSearchAttrReq {
						tx = binary.BigEndian.Uint16(pdu[1:3])
					}
				}
			}
			return tx
		}

		attrList := makeSDPPnPAttrList(vid, pid)
		if twoChunk {
			tx1 := lastSDPTxID()
			if tx1 == 0 {
				t.Fatal("missing initial SDP ServiceSearchAttributeRequest")
			}
			bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeACLPacket(handle, hci.ACLBoundaryFirstAutoFlush, localSDPCID, makeSDPAttrRsp(tx1, attrList[:8], []byte{0xaa})))
			for range 3 {
				if err := mgr.Service(); err != nil {
					t.Fatal(err)
				}
			}
			tx2 := lastSDPTxID()
			if tx2 == tx1 {
				t.Fatal("expected follow-up SDP request for continuation state")
			}
			bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeACLPacket(handle, hci.ACLBoundaryFirstAutoFlush, localSDPCID, makeSDPAttrRsp(tx2, attrList[8:], nil)))
		} else {
			tx1 := lastSDPTxID()
			if tx1 == 0 {
				t.Fatal("missing SDP ServiceSearchAttributeRequest")
			}
			bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, makeACLPacket(handle, hci.ACLBoundaryFirstAutoFlush, localSDPCID, makeSDPAttrRsp(tx1, attrList, nil)))
		}
		for range 3 {
			if err := mgr.Service(); err != nil {
				t.Fatal(err)
			}
		}

		// Verify SDP channel was closed with sigDisconnReq.
		var sawDisconn bool
		for _, pkt := range bus.aclOutPipe.outLog {
			if len(pkt) >= 14 && (binary.LittleEndian.Uint16(pkt[0:2])&0x0fff) == handle &&
				binary.LittleEndian.Uint16(pkt[6:8]) == CIDSignaling && pkt[8] == sigDisconnReq &&
				binary.LittleEndian.Uint16(pkt[12:14]) == remoteSDPCID {
				sawDisconn = true
			}
		}
		if !sawDisconn {
			t.Fatalf("expected sigDisconnReq closing SDP channel for handle %#x", handle)
		}
	}

	dsAddr, _ := ParseBDAddr("77:11:22:33:44:55")
	ds4Addr, _ := ParseBDAddr("77:11:22:33:44:56")
	resolveViaSDP(0x0049, dsAddr, 0x054c, 0x0ce6, true)
	resolveViaSDP(0x004a, ds4Addr, 0x054c, 0x09cc, false)

	recDS, ok1 := bonds.Lookup(dsAddr)
	recDS4, ok2 := bonds.Lookup(ds4Addr)
	if !ok1 || recDS.Kind != controllers.KindDualSense || recDS.VendorID != 0x054c || recDS.ProductID != 0x0ce6 {
		t.Fatalf("DualSense named 'Wireless Controller' misclassified: ok=%t rec=%+v", ok1, recDS)
	}
	if !ok2 || recDS4.Kind != controllers.KindDualShock4 || recDS4.VendorID != 0x054c || recDS4.ProductID != 0x09cc {
		t.Fatalf("DualShock 4 named 'Wireless Controller' misclassified: ok=%t rec=%+v", ok2, recDS4)
	}
	if len(resolved) < 4 || resolved[1] != controllers.KindDualSense || resolved[3] != controllers.KindDualShock4 {
		t.Fatalf("unexpected OnNameResolved sequence: %v", resolved)
	}
}

func TestSetPairingModeFalseCancelsPendingCreateConnection(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1400, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	makeEIR := func(addr BDAddr) []byte {
		eir := make([]byte, 2+15)
		eir[0] = hci.EventExtendedInquiryResult
		eir[1] = 15
		eir[2] = 1
		copy(eir[3:9], addr[:])
		eir[9] = 1
		eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
		return eir
	}

	// 1. Explicit SetPairingMode(false, 0) cancels pending CreateConnection page.
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	dev1, _ := ParseBDAddr("99:11:22:33:44:55")
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, makeEIR(dev1))
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if !mgr.connecting {
		t.Fatal("expected mgr.connecting=true before ConnectionComplete")
	}
	if err := mgr.SetPairingMode(false, 0); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeCreateConnectionCancel]; !bytes.Equal(got, dev1[:]) {
		t.Fatalf("expected CreateConnectionCancel for %s, got %x", dev1, got)
	}
	if mgr.connecting {
		t.Fatal("expected mgr.connecting=false after SetPairingMode(false)")
	}

	// 2. Pairing window expiry inside Service() also cancels pending CreateConnection.
	delete(bus.lastParams, hci.OpcodeCreateConnectionCancel)
	if err := mgr.SetPairingMode(true, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	dev2, _ := ParseBDAddr("99:11:22:33:44:56")
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, makeEIR(dev2))
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeCreateConnectionCancel]; !bytes.Equal(got, dev2[:]) {
		t.Fatalf("expected CreateConnectionCancel on pairing expiry for %s, got %x", dev2, got)
	}
}

func TestInfoResponseRetriesAfterACLQueueFull(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return time.Unix(1500, 0) })
	if err != nil {
		t.Fatal(err)
	}

	peer, _ := ParseBDAddr("AA:BB:CC:DD:EE:01")
	connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, peer[:]...)
	connOK = append(connOK, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	// Saturate ACL TX (1 in-flight + 16 queued in txRing) so sigInfoReq replies cannot fit.
	bus.aclOutPipe.writeStalled = true
	for range 17 {
		if err := tr.SendACL(0x0041, hci.ACLBoundaryFirstAutoFlush, []byte{0x99}); err != nil {
			t.Fatalf("prefill SendACL: %v", err)
		}
	}
	bus.aclOutPipe.outLog = nil

	infoReqExt := []byte{sigInfoReq, 7, 2, 0, 0x02, 0x00}
	infoReqFixed := []byte{sigInfoReq, 8, 2, 0, 0x03, 0x00}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, infoReqExt),
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, infoReqFixed),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if mgr.conns[0].sigQCount != 2 {
		t.Fatalf("expected 2 queued sigInfoRsp frames under queue full, got %d", mgr.conns[0].sigQCount)
	}

	// Unstall ACL OUT pipe, replenish credits, and drain queued signaling frames.
	bus.aclOutPipe.writeStalled = false
	for range 24 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x08, 0x00})
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if mgr.conns[0].sigQCount != 0 {
		t.Fatalf("expected sigQCount=0 after drain, got %d", mgr.conns[0].sigQCount)
	}

	var rspIDs []byte
	var sawExt, sawFixed bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) < 12 || binary.LittleEndian.Uint16(pkt[6:8]) != CIDSignaling || pkt[8] != sigInfoRsp {
			continue
		}
		id := pkt[9]
		rspIDs = append(rspIDs, id)
		body := pkt[12:]
		if id == 7 && len(body) == 8 && binary.LittleEndian.Uint16(body[0:2]) == 0x0002 {
			sawExt = true
		}
		if id == 8 && len(body) == 12 && binary.LittleEndian.Uint16(body[0:2]) == 0x0003 && body[4] == 0x02 {
			sawFixed = true
		}
	}
	if !sawExt || !sawFixed || !bytes.Equal(rspIDs, []byte{7, 8}) {
		t.Fatalf("expected ordered retry sigInfoRsp [7,8] ext=%t fixed=%t, got ids=%v", sawExt, sawFixed, rspIDs)
	}
}

func TestPairingModeReplacesStaleLinkKeyAndRecoversOnAuthFailure(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1600, 0)
	bonds := NewBondStore()
	peer, _ := ParseBDAddr("BB:CC:DD:EE:FF:01")
	oldKey := [16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	newKey := [16]byte{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
	_ = bonds.Upsert(BondRecord{Addr: peer, LinkKey: oldKey, Name: "DualSense Wireless Controller", Kind: controllers.KindDualSense})

	mgr, err := NewManager(tr, bonds, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	// 1. Outside pairing mode, LinkKeyRequest supplies stored link key for fast reconnect.
	lkReq := append([]byte{hci.EventLinkKeyRequest, 6}, peer[:]...)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, lkReq)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeLinkKeyRequestReply]; len(got) < 22 || !bytes.Equal(got[:6], peer[:]) || !bytes.Equal(got[6:22], oldKey[:]) {
		t.Fatalf("expected LinkKeyRequestReply with oldKey outside pairing, got %x", got)
	}

	// 2. Inside explicit pairing, a current-session controller connection can
	// replace its stale key after LinkKeyRequestNegativeReply.
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	delete(bus.lastParams, hci.OpcodeLinkKeyRequestNegReply)
	eir := make([]byte, 2+15)
	eir[0] = hci.EventExtendedInquiryResult
	eir[1] = 15
	eir[2] = 1
	copy(eir[3:9], peer[:])
	eir[9] = 1
	eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
	conn := append([]byte{hci.EventConnectionComplete, 11, 0, 0x4a, 0x00}, peer[:]...)
	conn = append(conn, 0x01, 0x00)
	lkNotif := append([]byte{hci.EventLinkKeyNotification, 23}, peer[:]...)
	lkNotif = append(lkNotif, newKey[:]...)
	lkNotif = append(lkNotif, 0x04)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir, conn, lkReq, lkNotif)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeLinkKeyRequestNegReply]; len(got) < 6 || !bytes.Equal(got[:6], peer[:]) {
		t.Fatalf("expected LinkKeyRequestNegativeReply during pairing window, got %x", got)
	}
	if rec, ok := bonds.Lookup(peer); !ok || rec.LinkKey != newKey {
		t.Fatalf("expected stale bond replaced with newKey, ok=%t key=%x", ok, rec.LinkKey)
	}

	// 3. Authentication failure (status != 0) recovers canceled inquiry during pairing.
	dev2, _ := ParseBDAddr("BB:CC:DD:EE:FF:02")
	eir = make([]byte, 2+15)
	eir[0] = hci.EventExtendedInquiryResult
	eir[1] = 15
	eir[2] = 1
	copy(eir[3:9], dev2[:])
	eir[9] = 1
	eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	bus.manualAuth = true
	connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x4b, 0x00}, dev2[:]...)
	connOK = append(connOK, 0x01, 0x01)
	authFail := []byte{hci.EventAuthenticationComplete, 3, 0x05, 0x4b, 0x00}
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK, authFail)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	inquiries := 0
	for _, op := range bus.cmds {
		if op == hci.OpcodeInquiry {
			inquiries++
		}
	}
	if inquiries != 2 {
		t.Fatalf("expected StartInquiry to resume after AuthenticationComplete failure (count=%d)", inquiries)
	}
}

func TestBatchedInquiryResultsDecodeInterleavedRecords(t *testing.T) {
	for _, tc := range []struct {
		name   string
		evCode byte
		codOff int
		clkOff int
	}{
		{name: "inquiry_result", evCode: hci.EventInquiryResult, codOff: 9, clkOff: 12},
		{name: "inquiry_result_with_rssi", evCode: hci.EventInquiryResultWithRSSI, codOff: 8, clkOff: 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := newL2CAPMockBus()
			tr, err := hci.Open(bus, bus.dev)
			if err != nil {
				t.Fatal(err)
			}
			if err := tr.Initialize(nil); err != nil {
				t.Fatal(err)
			}
			now := time.Unix(1700, 0)
			mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
				t.Fatal(err)
			}

			phone, _ := ParseBDAddr("10:11:12:13:14:01")
			pad, _ := ParseBDAddr("10:11:12:13:14:02")

			// Num_Responses = 2 laid out as interleaved 14-byte HCI inquiry records.
			ev := make([]byte, 2+1+2*14)
			ev[0] = tc.evCode
			ev[1] = 1 + 2*14
			ev[2] = 2 // Num_Responses

			rec0 := ev[3 : 3+14]
			copy(rec0[0:6], phone[:])
			rec0[6] = 0                                                              // psrm[0]
			rec0[tc.codOff], rec0[tc.codOff+1], rec0[tc.codOff+2] = 0x0c, 0x02, 0x5a // phone CoD
			binary.LittleEndian.PutUint16(rec0[tc.clkOff:tc.clkOff+2], 0x1111)

			rec1 := ev[3+14 : 3+28]
			copy(rec1[0:6], pad[:])
			rec1[6] = 1                                                              // psrm[1]
			rec1[tc.codOff], rec1[tc.codOff+1], rec1[tc.codOff+2] = 0x08, 0x25, 0x00 // gamepad CoD
			binary.LittleEndian.PutUint16(rec1[tc.clkOff:tc.clkOff+2], 0x2233)

			bus.evPipe.inQueue = append(bus.evPipe.inQueue, ev)
			if err := mgr.Service(); err != nil {
				t.Fatal(err)
			}
			got := bus.lastParams[hci.OpcodeCreateConnection]
			if len(got) < 12 || !bytes.Equal(got[0:6], pad[:]) || got[8] != 1 || binary.LittleEndian.Uint16(got[10:12]) != 0x2233 {
				t.Fatalf("expected CreateConnection for pad %s with psrm=1 clk=0x2233, got %x", pad, got)
			}
		})
	}
}

func TestIncomingPairingConnectionNotHostInitiatedAndRetriesFailedInquiry(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	// 1. An accepted incoming controller request must NOT set hostInitiated or
	// initiate an outgoing HID ConnReq.
	inPeer, _ := ParseBDAddr("77:88:99:AA:BB:CC")
	connReq := append([]byte{hci.EventConnectionRequest, 10}, inPeer[:]...)
	connReq = append(connReq, 0x08, 0x25, 0x00, 0x01)
	connIn := append([]byte{hci.EventConnectionComplete, 11, 0, 0x4c, 0x00}, inPeer[:]...)
	connIn = append(connIn, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connReq, connIn)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	conn := mgr.findConnByHandle(0x004c)
	if conn == nil || conn.hostInitiated {
		t.Fatalf("expected incoming pairing conn without hostInitiated, conn=%+v", conn)
	}
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 16 && (binary.LittleEndian.Uint16(pkt[0:2])&0x0fff) == 0x004c &&
			binary.LittleEndian.Uint16(pkt[6:8]) == CIDSignaling && pkt[8] == sigConnReq &&
			binary.LittleEndian.Uint16(pkt[12:14]) == PSMHIDControl {
			t.Fatal("incoming pairing conn unexpectedly sent outgoing sigConnReq for PSMHIDControl")
		}
	}

	// 2. Failed StartInquiry during resumeInquiryIfPairing records inquiryRetryPending
	// and retries inside Service() once nameRetryBackoff elapses.
	countInquiry := func() int {
		n := 0
		for _, op := range bus.cmds {
			if op == hci.OpcodeInquiry {
				n++
			}
		}
		return n
	}
	beforeInq := countInquiry()
	bus.failInquiry = true
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventInquiryComplete, 1, 0x00})
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if !mgr.inquiryRetryPending {
		t.Fatal("expected inquiryRetryPending=true after failed StartInquiry resume")
	}
	if got := countInquiry(); got != beforeInq {
		t.Fatalf("expected inquiry count unchanged (%d) on failure, got %d", beforeInq, got)
	}

	now = now.Add(nameRetryBackoff)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if mgr.inquiryRetryPending {
		t.Fatal("expected inquiryRetryPending cleared after successful retry")
	}
	if got := countInquiry(); got != beforeInq+1 {
		t.Fatalf("expected StartInquiry retry count=%d, got %d", beforeInq+1, got)
	}
}

func TestSyncRemoteNameFailureExhaustionFallsBackToSDP(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1900, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	bus.failName = true
	peer, _ := ParseBDAddr("88:99:AA:BB:CC:DD")
	connOK := append([]byte{hci.EventConnectionComplete, 11, 0, 0x4d, 0x00}, peer[:]...)
	connOK = append(connOK, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connOK)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	for range maxNameRetries + 1 {
		now = now.Add(nameRetryBackoff)
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}

	conn := mgr.findConnByHandle(0x004d)
	if conn == nil {
		t.Fatal("missing ACL conn 0x004d")
	}
	if conn.namePending || !conn.sdpQueried {
		t.Fatalf("expected sync name exhaustion to stop name retries and start SDP: namePending=%t sdpQueried=%t",
			conn.namePending, conn.sdpQueried)
	}
	var sawSDPReq bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 16 && (binary.LittleEndian.Uint16(pkt[0:2])&0x0fff) == 0x004d &&
			binary.LittleEndian.Uint16(pkt[6:8]) == CIDSignaling && pkt[8] == sigConnReq &&
			binary.LittleEndian.Uint16(pkt[12:14]) == PSMSDP {
			sawSDPReq = true
		}
	}
	if !sawSDPReq {
		t.Fatal("expected outgoing PSMSDP ConnReq after synchronous RemoteNameRequest retries exhausted")
	}
}

func TestSetPairingModeClosePropagatesAndRetriesFailedCancellations(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2000, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	// 1. InquiryCancel error on close must return an error, keep pairingActive=false,
	// retain cancelInquiryPending=true, and retry in Service().
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	bus.failCancelInq = true
	if err := mgr.SetPairingMode(false, 0); err == nil {
		t.Fatal("expected SetPairingMode(false) to return CancelInquiry failure")
	}
	if mgr.PairingActive() || !mgr.cancelInquiryPending || !mgr.inquiryActive {
		t.Fatalf("unexpected state after failed InquiryCancel: pairing=%t cancelInq=%t inqActive=%t",
			mgr.PairingActive(), mgr.cancelInquiryPending, mgr.inquiryActive)
	}
	now = now.Add(nameRetryBackoff)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if mgr.cancelInquiryPending || mgr.inquiryActive {
		t.Fatalf("expected Service retry to clear inquiry cancellation: cancelInq=%t inqActive=%t",
			mgr.cancelInquiryPending, mgr.inquiryActive)
	}

	// 2. CreateConnectionCancel error on close must return an error, retain
	// cancelConnPending=true with addr, and retry in Service().
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	dev1, _ := ParseBDAddr("AA:11:22:33:44:99")
	eir := make([]byte, 2+15)
	eir[0] = hci.EventExtendedInquiryResult
	eir[1] = 15
	eir[2] = 1
	copy(eir[3:9], dev1[:])
	eir[9] = 1
	eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	delete(bus.lastParams, hci.OpcodeCreateConnectionCancel)
	bus.failCancelConn = true
	if err := mgr.SetPairingMode(false, 0); err == nil {
		t.Fatal("expected SetPairingMode(false) to return CreateConnectionCancel failure")
	}
	if !mgr.cancelConnPending || mgr.cancelConnAddr != dev1 {
		t.Fatalf("expected cancelConnPending for %s, got pending=%t addr=%s", dev1, mgr.cancelConnPending, mgr.cancelConnAddr)
	}
	now = now.Add(nameRetryBackoff)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if mgr.cancelConnPending || !bytes.Equal(bus.lastParams[hci.OpcodeCreateConnectionCancel], dev1[:]) {
		t.Fatalf("expected Service retry CreateConnectionCancel for %s: pending=%t last=%x",
			dev1, mgr.cancelConnPending, bus.lastParams[hci.OpcodeCreateConnectionCancel])
	}
}

func TestSetPairingModeReopenReusesActiveInquiry(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2500, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	countInquiry := func() int {
		n := 0
		for _, op := range bus.cmds {
			if op == hci.OpcodeInquiry {
				n++
			}
		}
		return n
	}

	// (a) Calling SetPairingMode(true, ...) while already pairing extends the
	// deadline and reuses the active inquiry instead of reissuing StartInquiry.
	if err := mgr.SetPairingMode(true, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	firstUntil := mgr.pairingUntil
	if got := countInquiry(); got != 1 {
		t.Fatalf("expected 1 StartInquiry call, got %d", got)
	}

	now = now.Add(5 * time.Second)
	bus.failInquiry = true
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatalf("reopening active pairing failed: %v", err)
	}
	bus.failInquiry = false
	if got := countInquiry(); got != 1 {
		t.Fatalf("expected active inquiry to be reused, got %d StartInquiry calls", got)
	}
	if !mgr.pairingUntil.After(firstUntil) || !mgr.PairingActive() {
		t.Fatalf("expected extended pairingUntil > %v, got %v (active=%t)", firstUntil, mgr.pairingUntil, mgr.PairingActive())
	}

	// (b) After a failed InquiryCancel leaves inquiryActive=true and
	// cancelInquiryPending=true, reopening pairing clears cancelInquiryPending and
	// reuses the running inquiry without issuing a duplicate StartInquiry.
	bus.failCancelInq = true
	if err := mgr.SetPairingMode(false, 0); err == nil {
		t.Fatal("expected SetPairingMode(false) to return CancelInquiry failure")
	}
	if !mgr.cancelInquiryPending || !mgr.inquiryActive || mgr.PairingActive() {
		t.Fatalf("unexpected state after failed cancel: cancelInq=%t inqActive=%t pairing=%t",
			mgr.cancelInquiryPending, mgr.inquiryActive, mgr.PairingActive())
	}
	bus.failInquiry = true
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatalf("reopening pairing after failed InquiryCancel failed: %v", err)
	}
	bus.failInquiry = false
	if mgr.cancelInquiryPending || !mgr.inquiryActive || !mgr.PairingActive() {
		t.Fatalf("expected reopened pairing to clear cancelInquiryPending and reuse inquiry: cancelInq=%t inqActive=%t pairing=%t",
			mgr.cancelInquiryPending, mgr.inquiryActive, mgr.PairingActive())
	}
	if got := countInquiry(); got != 1 {
		t.Fatalf("expected 1 total StartInquiry across reopen, got %d", got)
	}
}

func TestHIDChannelsRequireAuthenticationAndEncryption(t *testing.T) {
	bus := newL2CAPMockBus()
	bus.manualAuth = true
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2600, 0)
	bonds := NewBondStore()
	peer, _ := ParseBDAddr("12:34:56:78:9A:BC")
	_ = bonds.Upsert(BondRecord{
		Addr:    peer,
		LinkKey: [16]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 9, 8, 7, 6, 5, 4},
		Name:    "DualSense Wireless Controller",
		Kind:    controllers.KindDualSense,
	})
	mgr, err := NewManager(tr, bonds, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	var opened []uint16
	var hidPayloads int
	mgr.OnChannelOpen = func(_ uint16, _ BDAddr, _ string, _ controllers.Kind, psm uint16) {
		opened = append(opened, psm)
	}
	mgr.OnHIDControl = func(_ uint16, _ []byte) {
		hidPayloads++
	}

	// 1. Spoofed bonded peer connects with ACL unencrypted/unauthenticated and
	// sends incoming sigConnReq(PSMHIDControl) + premature sigConfigReq before auth finishes.
	conn1 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, peer[:]...)
	conn1 = append(conn1, 0x01, 0x00)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, conn1)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeAuthenticationRequested]; len(got) < 2 || binary.LittleEndian.Uint16(got[:2]) != 0x0041 {
		t.Fatalf("expected AuthenticationRequested on handle 0x0041, got %x", got)
	}
	if err := mgr.OpenChannel(0x0041, PSMHIDControl); err == nil {
		t.Fatal("expected OpenChannel(PSMHIDControl) to fail before auth+encryption")
	}

	bus.aclOutPipe.outLog = nil
	sigConnCtrl := []byte{sigConnReq, 1, 4, 0, 0x11, 0x00, 0x55, 0x00}
	sigCfgPremature := []byte{sigConfigReq, 2, 4, 0, 0x40, 0x00, 0x00, 0x00}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigConnCtrl),
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, sigCfgPremature),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	var sawPending, sawCfgRspEarly bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 20 && pkt[8] == sigConnRsp && binary.LittleEndian.Uint16(pkt[16:18]) == 0x0001 && binary.LittleEndian.Uint16(pkt[18:20]) == 0x0001 {
			sawPending = true
		}
		if len(pkt) >= 12 && pkt[8] == sigConfigRsp {
			sawCfgRspEarly = true
		}
	}
	if !sawPending || sawCfgRspEarly {
		t.Fatalf("expected Pending/AuthPending (0x0001/0x0001) and no premature ConfigRsp; pending=%t cfgRspEarly=%t out=%x", sawPending, sawCfgRspEarly, bus.aclOutPipe.outLog)
	}

	// Authentication failure rejects parked channel with Security Block (0x0003) and disconnects handle.
	bus.aclOutPipe.outLog = nil
	delete(bus.lastParams, hci.OpcodeDisconnect)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventAuthenticationComplete, 3, 0x05, 0x41, 0x00})
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	var sawSecBlock bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 20 && pkt[8] == sigConnRsp && binary.LittleEndian.Uint16(pkt[16:18]) == 0x0003 {
			sawSecBlock = true
		}
	}
	if !sawSecBlock || len(opened) != 0 {
		t.Fatalf("expected SecurityBlock (0x0003) and no open channels, block=%t opened=%v", sawSecBlock, opened)
	}
	if disc := bus.lastParams[hci.OpcodeDisconnect]; len(disc) < 3 || binary.LittleEndian.Uint16(disc[:2]) != 0x0041 || disc[2] != 0x05 {
		t.Fatalf("expected Disconnect(0x0041, 0x05) on auth failure, got %x", disc)
	}
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventDisconnectionComplete, 4, 0, 0x41, 0x00, 0x05})
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}

	// 2. Legitimate peer reconnect: parks sigConnReq as Pending while Auth runs,
	// stays blocked during SetConnectionEncryption, then promotes & opens after EncryptionChange(1).
	conn2 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x42, 0x00}, peer[:]...)
	conn2 = append(conn2, 0x01, 0x00)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, conn2)
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConnReq, 3, 4, 0, 0x11, 0x00, 0x56, 0x00}),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	delete(bus.lastParams, hci.OpcodeSetConnectionEncryption)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventAuthenticationComplete, 3, 0x00, 0x42, 0x00})
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if got := bus.lastParams[hci.OpcodeSetConnectionEncryption]; len(got) < 3 || binary.LittleEndian.Uint16(got[:2]) != 0x0042 || got[2] != 0x01 {
		t.Fatalf("expected SetConnectionEncryption(0x0042, 1) after AuthComplete, got %x", got)
	}
	if err := mgr.OpenChannel(0x0042, PSMHIDInterrupt); err == nil {
		t.Fatal("expected OpenChannel(PSMHIDInterrupt) to fail before EncryptionChange")
	}

	bus.aclOutPipe.outLog = nil
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventEncryptionChange, 4, 0x00, 0x42, 0x00, 0x01})
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	var sawSuccess, sawCfgReq bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 20 && pkt[8] == sigConnRsp && binary.LittleEndian.Uint16(pkt[16:18]) == 0x0000 {
			sawSuccess = true
		}
		if len(pkt) >= 16 && pkt[8] == sigConfigReq {
			sawCfgReq = true
		}
	}
	if !sawSuccess || !sawCfgReq {
		t.Fatalf("expected promoted sigConnRsp(Success) and sigConfigReq after EncryptionChange, success=%t cfg=%t", sawSuccess, sawCfgReq)
	}
	// Remote finishes L2CAP config (localCID=0x0040 allocated by parked conn).
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigReq, 4, 4, 0, 0x40, 0x00, 0x00, 0x00}),
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigRsp, 2, 6, 0, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0] != PSMHIDControl {
		t.Fatalf("expected PSMHIDControl open after auth+enc, got %v", opened)
	}

	// 3. Mid-session encryption downgrade (enc=0) disconnects and immediately drops HID PDUs.
	delete(bus.lastParams, hci.OpcodeDisconnect)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{hci.EventEncryptionChange, 4, 0x00, 0x42, 0x00, 0x00})
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0042, hci.ACLBoundaryFirstAutoFlush, 0x0040, []byte{0xa1, 0x01}),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if disc := bus.lastParams[hci.OpcodeDisconnect]; len(disc) < 3 || binary.LittleEndian.Uint16(disc[:2]) != 0x0042 || disc[2] != 0x05 {
		t.Fatalf("expected Disconnect(0x0042, 0x05) on encryption downgrade, got %x", disc)
	}
	if hidPayloads != 0 {
		t.Fatalf("expected HID PDU after encryption downgrade to be dropped, got %d", hidPayloads)
	}
}

func TestL2CAPConfigHonorsPeerMTUAndRejectsBelowMinimum(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	bonds := NewBondStore()
	peer, _ := ParseBDAddr("22:33:44:55:66:77")
	_ = bonds.Upsert(BondRecord{Addr: peer, LinkKey: [16]byte{7}, Name: "Switch Pro", Kind: controllers.KindSwitchPro})

	mgr, err := NewManager(tr, bonds, func() time.Time { return time.Unix(2700, 0) })
	if err != nil {
		t.Fatal(err)
	}
	var opened []uint16
	mgr.OnChannelOpen = func(_ uint16, _ BDAddr, _ string, _ controllers.Kind, psm uint16) {
		opened = append(opened, psm)
	}

	connEv := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, peer[:]...)
	connEv = append(connEv, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connEv)
	// Peer opens PSMHIDControl and requests invalid MTU = 32 (< minL2CAPMTU=48).
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConnReq, 1, 4, 0, 0x11, 0x00, 0x50, 0x00}),
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigReq, 2, 8, 0, 0x40, 0x00, 0x00, 0x00, 0x01, 0x02, 32, 0}),
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigRsp, 2, 6, 0, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	var sawUnacceptable bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 18 && pkt[8] == sigConfigRsp && binary.LittleEndian.Uint16(pkt[16:18]) == 0x0001 {
			sawUnacceptable = true
		}
	}
	if !sawUnacceptable || len(opened) != 0 {
		t.Fatalf("expected ConfigRsp Unacceptable Parameters (0x0001) and closed channel, saw=%t opened=%v", sawUnacceptable, opened)
	}

	// Peer retries with minimum valid MTU = 48 -> accepted, channel opens, 48-byte SendChannel succeeds, >48 fails.
	bus.aclOutPipe.outLog = nil
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigReq, 3, 8, 0, 0x40, 0x00, 0x00, 0x00, 0x01, 0x02, 48, 0}),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0] != PSMHIDControl {
		t.Fatalf("expected PSMHIDControl open after valid MTU=48, got %v", opened)
	}
	bus.aclOutPipe.outLog = nil
	if err := mgr.SendChannel(0x0041, PSMHIDControl, make([]byte, 48)); err != nil {
		t.Fatalf("SendChannel(48) failed: %v", err)
	}
	if err := mgr.SendChannel(0x0041, PSMHIDControl, make([]byte, 50)); err == nil {
		t.Fatal("expected SendChannel(50) to fail against peerMTU=48")
	}
}

func TestSetPairingModeFalseAbortsUnbondedPairingACL(t *testing.T) {
	bus := newL2CAPMockBus()
	bus.manualAuth = true
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2800, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var opened []uint16
	mgr.OnChannelOpen = func(_ uint16, _ BDAddr, _ string, _ controllers.Kind, psm uint16) {
		opened = append(opened, psm)
	}

	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	// 1. Outgoing unbonded pairing connection reaches ConnectionComplete (clears connecting),
	// then pairing closes before LinkKeyNotification arrives -> must Disconnect and block HID.
	peer1, _ := ParseBDAddr("33:44:55:66:77:88")
	eir := make([]byte, 2+15)
	eir[0] = hci.EventExtendedInquiryResult
	eir[1] = 15
	eir[2] = 1
	copy(eir[3:9], peer1[:])
	eir[9] = 1
	eir[11], eir[12], eir[13] = 0x08, 0x25, 0x00
	conn1 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x41, 0x00}, peer1[:]...)
	conn1 = append(conn1, 0x01, 0x00)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, eir, conn1)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	delete(bus.lastParams, hci.OpcodeDisconnect)
	if err := mgr.SetPairingMode(false, 0); err != nil {
		t.Fatal(err)
	}
	if disc := bus.lastParams[hci.OpcodeDisconnect]; len(disc) < 3 || binary.LittleEndian.Uint16(disc[:2]) != 0x0041 {
		t.Fatalf("expected immediate Disconnect on unbonded pairing ACL 0x0041, got %x", disc)
	}
	// Even if delayed auth/enc + incoming HID ConnReq arrive before DisconnectComplete, stay blocked.
	bus.aclOutPipe.outLog = nil
	bus.evPipe.inQueue = append(bus.evPipe.inQueue,
		[]byte{hci.EventAuthenticationComplete, 3, 0x00, 0x41, 0x00},
		[]byte{hci.EventEncryptionChange, 4, 0x00, 0x41, 0x00, 0x01},
	)
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0041, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConnReq, 1, 4, 0, 0x11, 0x00, 0x50, 0x00}),
	)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	var sawSecBlock bool
	for _, pkt := range bus.aclOutPipe.outLog {
		if len(pkt) >= 20 && pkt[8] == sigConnRsp && binary.LittleEndian.Uint16(pkt[16:18]) == 0x0003 {
			sawSecBlock = true
		}
	}
	if !sawSecBlock || len(opened) != 0 {
		t.Fatalf("expected aborted pairing ACL to stay blocked: secBlock=%t opened=%v", sawSecBlock, opened)
	}

	// 2. Incoming unbonded ConnectionRequest accepted during pairing whose ConnectionComplete
	// arrives after SetPairingMode(false, 0) must be disconnected immediately without allocating conn.
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	peer2, _ := ParseBDAddr("33:44:55:66:77:99")
	creq := append([]byte{hci.EventConnectionRequest, 10}, peer2[:]...)
	creq = append(creq, 0x08, 0x25, 0x00, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, creq)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetPairingMode(false, 0); err != nil {
		t.Fatal(err)
	}
	delete(bus.lastParams, hci.OpcodeDisconnect)
	conn2 := append([]byte{hci.EventConnectionComplete, 11, 0, 0x42, 0x00}, peer2[:]...)
	conn2 = append(conn2, 0x01, 0x00)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, conn2)
	if err := mgr.Service(); err != nil {
		t.Fatal(err)
	}
	if disc := bus.lastParams[hci.OpcodeDisconnect]; len(disc) < 3 || binary.LittleEndian.Uint16(disc[:2]) != 0x0042 {
		t.Fatalf("expected Disconnect for late ConnectionComplete 0x0042 after pairing closed, got %x", disc)
	}
	if mgr.findConnByHandle(0x0042) != nil {
		t.Fatal("expected no allocated aclConn for late unbonded pairing completion")
	}
}

func TestPairingRemainingAndDisconnectAddr(t *testing.T) {
	bus := newL2CAPMockBus()
	tr, err := hci.Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2900, 0)
	mgr, err := NewManager(tr, NewBondStore(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if rem := mgr.PairingRemaining(); rem != 0 {
		t.Fatalf("expected 0 remaining while inactive, got %v", rem)
	}
	if err := mgr.SetPairingMode(true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	now = now.Add(12 * time.Second)
	if rem := mgr.PairingRemaining(); rem != 18*time.Second {
		t.Fatalf("expected 18s remaining, got %v", rem)
	}
	now = now.Add(20 * time.Second)
	if rem := mgr.PairingRemaining(); rem != 0 {
		t.Fatalf("expected 0 remaining past expiry, got %v", rem)
	}
	if err := mgr.Service(); err != nil || mgr.PairingActive() {
		t.Fatalf("expected expired pairing closed by Service, err=%v active=%t", err, mgr.PairingActive())
	}

	// Open bonded HID Control + Interrupt channels and disconnect by BDAddr.
	peer, _ := ParseBDAddr("11:22:33:44:55:AA")
	other, _ := ParseBDAddr("11:22:33:44:55:BB")
	if matched, err := mgr.DisconnectAddr(other); matched || err != nil {
		t.Fatalf("expected non-matching addr (false, nil), got (%t, %v)", matched, err)
	}
	_ = mgr.bonds.Upsert(BondRecord{Addr: peer, LinkKey: [16]byte{1}, Name: "DualSense Wireless Controller", Kind: controllers.KindDualSense})
	var closedPSMs []uint16
	mgr.OnChannelClose = func(_ uint16, _ BDAddr, psm uint16) {
		closedPSMs = append(closedPSMs, psm)
	}
	connEv := append([]byte{hci.EventConnectionComplete, 11, 0, 0x45, 0x00}, peer[:]...)
	connEv = append(connEv, 0x01, 0x01)
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, connEv)
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue,
		makeACLPacket(0x0045, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConnReq, 1, 4, 0, 0x11, 0x00, 0x50, 0x00}),
		makeACLPacket(0x0045, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigReq, 2, 4, 0, 0x40, 0x00, 0x00, 0x00}),
		makeACLPacket(0x0045, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigRsp, 1, 6, 0, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00}),
		makeACLPacket(0x0045, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConnReq, 3, 4, 0, 0x13, 0x00, 0x51, 0x00}),
		makeACLPacket(0x0045, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigReq, 4, 4, 0, 0x41, 0x00, 0x00, 0x00}),
		makeACLPacket(0x0045, hci.ACLBoundaryFirstAutoFlush, CIDSignaling, []byte{sigConfigRsp, 2, 6, 0, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00}),
	)
	for range 4 {
		if err := mgr.Service(); err != nil {
			t.Fatal(err)
		}
	}
	delete(bus.lastParams, hci.OpcodeDisconnect)
	matched, err := mgr.DisconnectAddr(peer)
	if !matched || err != nil {
		t.Fatalf("DisconnectAddr failed: matched=%t err=%v", matched, err)
	}
	if disc := bus.lastParams[hci.OpcodeDisconnect]; len(disc) < 3 || binary.LittleEndian.Uint16(disc[:2]) != 0x0045 || disc[2] != 0x13 {
		t.Fatalf("expected Disconnect(0x0045, 0x13), got %x", disc)
	}
	if len(closedPSMs) != 2 {
		t.Fatalf("expected both HID channels closed immediately, got %v", closedPSMs)
	}
}
