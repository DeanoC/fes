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
	ep       usb.EndpointDescriptor
	inQueue  [][]byte
	inRepeat []byte
	outLog   [][]byte
	noOutLog bool
}

func (p *l2capMockPipe) DeviceAddress() uint8             { return 2 }
func (p *l2capMockPipe) Endpoint() usb.EndpointDescriptor { return p.ep }
func (p *l2capMockPipe) SetIntervalOverride(time.Duration) {}
func (p *l2capMockPipe) Close() error                     { return nil }
func (p *l2capMockPipe) WriteDone() (bool, error)         { return true, nil }
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
	dev          usb.BusDevice
	evPipe       *l2capMockPipe
	aclInPipe    *l2capMockPipe
	aclOutPipe   *l2capMockPipe
	cmds         []uint16
	lastParams   map[uint16][]byte
	failCreate   bool
	failInquiry  bool
	scanEnable   byte
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
		b.cmds = append(b.cmds, op)
		b.lastParams[op] = append([]byte(nil), data[3:]...)
		if op == hci.OpcodeWriteScanEnable && len(data) >= 4 {
			b.scanEnable = data[3]
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

	// SSP IO Capability + User Confirmation + Link Key Notification + Connection Complete + Remote Name
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

	bus.evPipe.inQueue = append(bus.evPipe.inQueue, ioCap, userConf, lkNotif, connComp, nameEv)
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
