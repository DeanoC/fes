// SPDX-License-Identifier: GPL-3.0-or-later
package hci

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type mockPipe struct {
	addr     uint8
	ep       usb.EndpointDescriptor
	inQueue  [][]byte
	inRepeat []byte
	outLog   [][]byte
	noOutLog bool
	outDone  bool
	closed   bool
}

func (p *mockPipe) DeviceAddress() uint8               { return p.addr }
func (p *mockPipe) Endpoint() usb.EndpointDescriptor   { return p.ep }
func (p *mockPipe) SetIntervalOverride(time.Duration)  {}
func (p *mockPipe) Close() error                       { p.closed = true; return nil }
func (p *mockPipe) WriteDone() (bool, error)           { return p.outDone, nil }
func (p *mockPipe) Write(data []byte) error {
	if !p.noOutLog {
		p.outLog = append(p.outLog, append([]byte(nil), data...))
	}
	p.outDone = true
	return nil
}
func (p *mockPipe) PollInto(dst []byte) (int, error) {
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

type mockBTBus struct {
	dev        usb.BusDevice
	evPipe     *mockPipe
	aclInPipe  *mockPipe
	aclOutPipe *mockPipe
	cmdLog     [][]byte
	rtlChunks  int
	rtlFinal   bool
	rtlVersion uint16
	romVersion byte
	scanEnable byte
	linkPolicy uint16
}

func newMockBTBus(vid, pid uint16, romVersion byte) *mockBTBus {
	dev := usb.BusDevice{
		Address: 2,
		Speed:   usb.SpeedHigh,
		Descriptor: usb.DeviceDescriptor{
			VendorID:  vid,
			ProductID: pid,
			Class:     0xe0,
			SubClass:  0x01,
			Protocol:  0x01,
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
	return &mockBTBus{
		dev:        dev,
		romVersion: romVersion,
		rtlVersion: rtlLMPSub8761B,
	}
}

func (b *mockBTBus) Service() error                 { return nil }
func (b *mockBTBus) NextEvent() (usb.HostEvent, bool) { return usb.HostEvent{}, false }
func (b *mockBTBus) Devices() []usb.BusDevice       { return []usb.BusDevice{b.dev} }
func (b *mockBTBus) Ready() <-chan struct{}         { return nil }
func (b *mockBTBus) OpenPipe(addr uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	p := &mockPipe{addr: addr, ep: ep}
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

func (b *mockBTBus) pushCmdComplete(op uint16, ret ...byte) {
	ev := make([]byte, 5+len(ret))
	ev[0] = EventCommandComplete
	ev[1] = byte(3 + len(ret))
	ev[2] = 1
	binary.LittleEndian.PutUint16(ev[3:5], op)
	copy(ev[5:], ret)
	b.evPipe.inQueue = append(b.evPipe.inQueue, ev)
}

func (b *mockBTBus) ControlTransfer(_ uint8, reqType, req byte, _, _ uint16, data []byte) (int, error) {
	if reqType != 0x20 || req != 0x00 || len(data) < 3 {
		return 0, nil
	}
	b.cmdLog = append(b.cmdLog, append([]byte(nil), data...))
	op := binary.LittleEndian.Uint16(data[0:2])
	params := data[3:]
	switch op {
	case OpcodeReadLocalVersion:
		b.pushCmdComplete(op, 0, 0x0d, 0x00, 0x00, 0x0d, 0x5d, 0x00, byte(b.rtlVersion), byte(b.rtlVersion>>8))
	case opcodeRTLReadROMVersion:
		b.pushCmdComplete(op, 0, b.romVersion)
	case opcodeRTLDownload:
		b.rtlChunks++
		if len(params) > 0 && params[0]&0x80 != 0 {
			b.rtlFinal = true
			b.rtlVersion = 0xd922 // patched LMP subversion
		}
		b.pushCmdComplete(op, 0, params[0])
	case OpcodeReadBufferSize:
		// 256-byte ACL packets, 4 controller buffer slots
		b.pushCmdComplete(op, 0, 0x00, 0x01, 0x40, 0x04, 0x00, 0x08, 0x00)
	case OpcodeReadBDAddr:
		b.pushCmdComplete(op, 0, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66)
	case OpcodeWriteDefaultLinkPolicy:
		if len(params) >= 2 {
			b.linkPolicy = binary.LittleEndian.Uint16(params[:2])
		}
		b.pushCmdComplete(op, 0)
	case OpcodeWriteScanEnable:
		if len(params) >= 1 {
			b.scanEnable = params[0]
		}
		b.pushCmdComplete(op, 0)
	default:
		b.pushCmdComplete(op, 0)
	}
	return len(data), nil
}

type mapFirmwareLoader map[string][]byte

func (m mapFirmwareLoader) LoadFirmware(name string) ([]byte, error) {
	b, ok := m[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return b, nil
}

func TestRealRepoRTL8761BUFirmwareUploadOn0BDAA728(t *testing.T) {
	// Load the actual pinned Realtek RTL8761BU firmware from image/firmware/rtl_bt/.
	fwPath := filepath.Join("..", "..", "..", "..", "..", "image", "firmware", "rtl_bt", "rtl8761bu_fw.bin")
	cfgPath := filepath.Join("..", "..", "..", "..", "..", "image", "firmware", "rtl_bt", "rtl8761bu_config.bin")
	fwBytes, err := os.ReadFile(fwPath)
	if err != nil {
		t.Fatalf("read repo rtl8761bu_fw.bin: %v", err)
	}
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read repo rtl8761bu_config.bin: %v", err)
	}

	// Verify both rom_version=0 (chip_id=1) and rom_version=1 (chip_id=2) parse cleanly.
	p0, err := BuildRTLPatchPayload(fwBytes, cfgBytes, 0)
	if err != nil || len(p0) != 14048+6 {
		t.Fatalf("rom_version=0 patch mismatch len=%d err=%v", len(p0), err)
	}
	p1, err := BuildRTLPatchPayload(fwBytes, cfgBytes, 1)
	if err != nil || len(p1) != 30204+6 {
		t.Fatalf("rom_version=1 patch mismatch len=%d err=%v", len(p1), err)
	}

	// Simulate Deano's exact 0bda:a728 Realtek BT 5.4 dongle!
	bus := newMockBTBus(0x0bda, 0xa728, 1)
	tr, err := Open(bus, bus.dev)
	if err != nil {
		t.Fatalf("Open(0bda:a728): %v", err)
	}
	defer tr.Close()
	if tr.Spec().Quirks&(QuirkRTL8761BU|QuirkBrokenExtScan) != (QuirkRTL8761BU | QuirkBrokenExtScan) {
		t.Fatalf("unexpected 0bda:a728 quirks: %+v", tr.Spec())
	}

	loader := mapFirmwareLoader{
		"rtl8761bu_fw.bin":     fwBytes,
		"rtl8761bu_config.bin": cfgBytes,
	}
	if err := tr.Initialize(loader); err != nil {
		t.Fatalf("Initialize(0bda:a728): %v", err)
	}
	wantChunks := (len(p1) + rtlMaxChunkBytes - 1) / rtlMaxChunkBytes
	if bus.rtlChunks != wantChunks || !bus.rtlFinal {
		t.Fatalf("RTL firmware download chunks=%d want=%d final=%t", bus.rtlChunks, wantChunks, bus.rtlFinal)
	}
	if tr.LocalVersion().LMPSubversion != 0xd922 {
		t.Fatalf("post-firmware LMP subversion=%#04x, want 0xd922", tr.LocalVersion().LMPSubversion)
	}
	if tr.BDAddr() != ([6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}) {
		t.Fatalf("unexpected BD_ADDR: %x", tr.BDAddr())
	}
	// Gaming latency invariants: Role Switch only (0x0001), Page Scan only (0x02).
	if bus.linkPolicy != 0x0001 || bus.scanEnable != ScanPageOnly {
		t.Fatalf("gaming policy mismatch linkPolicy=%#04x scanEnable=%#02x", bus.linkPolicy, bus.scanEnable)
	}

	// Gameplay radio mode disables scan windows completely (0x00) and restores Page Scan (0x02).
	if err := tr.SetGameplayRadioMode(true); err != nil || bus.scanEnable != ScanDisabled {
		t.Fatalf("SetGameplayRadioMode(true): err=%v scan=%#x", err, bus.scanEnable)
	}
	if err := tr.SetGameplayRadioMode(false); err != nil || bus.scanEnable != ScanPageOnly {
		t.Fatalf("SetGameplayRadioMode(false): err=%v scan=%#x", err, bus.scanEnable)
	}

	// Exercise SSP / connection / inquiry command helpers cleanly.
	peer := [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	if err := tr.AcceptConnectionRequest(peer, 0); err != nil {
		t.Fatal(err)
	}
	if err := tr.RejectConnectionRequest(peer, 0x0f); err != nil {
		t.Fatal(err)
	}
	if err := tr.CreateConnection(peer, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := tr.Disconnect(0x0042, 0x13); err != nil {
		t.Fatal(err)
	}
	if err := tr.StartInquiry(8, 0); err != nil {
		t.Fatal(err)
	}
	if err := tr.CancelInquiry(); err != nil {
		t.Fatal(err)
	}
	if err := tr.RemoteNameRequest(peer); err != nil {
		t.Fatal(err)
	}
	if err := tr.IOCapabilityRequestReply(peer); err != nil {
		t.Fatal(err)
	}
	if err := tr.UserConfirmationRequestReply(peer, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.UserConfirmationRequestReply(peer, false); err != nil {
		t.Fatal(err)
	}
	if err := tr.LinkKeyRequestReply(peer, [16]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := tr.LinkKeyRequestNegativeReply(peer); err != nil {
		t.Fatal(err)
	}
}

func TestHCIAclFlowControlFragmentationAndZeroAllocs(t *testing.T) {
	bus := newMockBTBus(0x0a12, 0x0001, 0) // CSR8510 (zero-blob)
	bus.rtlVersion = 0x22bb
	tr, err := Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}

	// Send 300-byte payload (> 256-byte ACL max len) -> fragments into 256 + 44 with PB=0x02 then PB=0x01.
	big := make([]byte, 300)
	for i := range big {
		big[i] = byte(i)
	}
	if err := tr.SendACL(0x0042, ACLBoundaryFirstAutoFlush, big); err != nil {
		t.Fatalf("SendACL: %v", err)
	}
	if err := tr.Service(nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(bus.aclOutPipe.outLog) != 2 {
		t.Fatalf("expected 2 ACL fragments, got %d", len(bus.aclOutPipe.outLog))
	}
	hdr0 := binary.LittleEndian.Uint16(bus.aclOutPipe.outLog[0][0:2])
	hdr1 := binary.LittleEndian.Uint16(bus.aclOutPipe.outLog[1][0:2])
	if hdr0 != (0x0042|uint16(ACLBoundaryFirstAutoFlush)<<12) || hdr1 != (0x0042|uint16(ACLBoundaryContinuing)<<12) {
		t.Fatalf("unexpected fragment headers %#04x %#04x", hdr0, hdr1)
	}
	if !bytes.Equal(bus.aclOutPipe.outLog[0][4:], big[:256]) || !bytes.Equal(bus.aclOutPipe.outLog[1][4:], big[256:]) {
		t.Fatal("fragmented ACL payload mismatch")
	}
	if tr.ACLCredits() != 2 {
		t.Fatalf("expected 2 remaining credits, got %d", tr.ACLCredits())
	}

	// Return 2 credits via Number_Of_Completed_Packets event (0x13).
	numComp := []byte{EventNumCompletedPackets, 5, 1, 0x42, 0x00, 0x02, 0x00}
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, numComp)
	if err := tr.Service(nil, nil); err != nil {
		t.Fatal(err)
	}
	if tr.ACLCredits() != 4 {
		t.Fatalf("credits not restored: %d", tr.ACLCredits())
	}

	// Verify multi-handle Number_Of_Completed_Packets (0x13) reads adjacent {handle,count} pairs
	// instead of treating the second handle ID (e.g. 0x0042) as the first count.
	bus.aclOutPipe.noOutLog = true
	for range 4 {
		if err := tr.SendACL(0x0041, ACLBoundaryFirstAutoFlush, []byte{1, 2, 3, 4}); err != nil {
			t.Fatal(err)
		}
		if err := tr.Service(nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if tr.ACLCredits() != 0 {
		t.Fatalf("expected 0 credits after 4 ACL writes, got %d", tr.ACLCredits())
	}
	multiComp := []byte{
		EventNumCompletedPackets, 9, 2,
		0x41, 0x00, 0x01, 0x00,
		0x42, 0x00, 0x01, 0x00,
	}
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, multiComp)
	if err := tr.Service(nil, nil); err != nil {
		t.Fatal(err)
	}
	if tr.ACLCredits() != 2 {
		t.Fatalf("expected exactly 2 credits from 2-handle 0x13 event, got %d", tr.ACLCredits())
	}
	// Restore credits to max (4) before the steady-state zero-alloc check.
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, []byte{EventNumCompletedPackets, 5, 1, 0x41, 0x00, 0x02, 0x00})
	if err := tr.Service(nil, nil); err != nil {
		t.Fatal(err)
	}

	// Zero-allocation check on steady-state ACL RX + ACL TX + credit replenishment.
	bus.aclOutPipe.noOutLog = true
	inACL := make([]byte, 4+82)
	binary.LittleEndian.PutUint16(inACL[0:2], 0x0042|uint16(ACLBoundaryFirstAutoFlush)<<12)
	binary.LittleEndian.PutUint16(inACL[2:4], 82)
	bus.aclInPipe.inRepeat = inACL
	var rxCount int
	onACL := func(handle uint16, pb uint8, payload []byte) {
		if handle == 0x0042 && pb == ACLBoundaryFirstAutoFlush && len(payload) == 82 {
			rxCount++
		}
	}
	allocs := testing.AllocsPerRun(100, func() {
		if err := tr.Service(nil, onACL); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 0 || rxCount == 0 {
		t.Fatalf("HCI Service ACL RX allocated %.2f allocs/run (rxCount=%d), want 0", allocs, rxCount)
	}
}

func TestSendACLCapsFragmentsToBulkOutMPS(t *testing.T) {
	bus := newMockBTBus(0x0a12, 0x0001, 0)
	bus.rtlVersion = 0x22bb
	// Simulate a Full-Speed / small-MPS bulk OUT endpoint (64-byte MPS) while
	// Read_Buffer_Size returns 256 bytes.
	bus.dev.Config.Interfaces[0].Endpoints[2].MaxPacketSize = 64
	tr, err := Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}

	payload := make([]byte, 83)
	for i := range payload {
		payload[i] = byte(i + 1)
	}
	if err := tr.SendACL(0x0042, ACLBoundaryFirstAutoFlush, payload); err != nil {
		t.Fatalf("SendACL: %v", err)
	}
	for range 3 {
		if err := tr.Service(nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(bus.aclOutPipe.outLog) != 2 {
		t.Fatalf("expected 2 MPS-bounded ACL fragments, got %d", len(bus.aclOutPipe.outLog))
	}
	f0, f1 := bus.aclOutPipe.outLog[0], bus.aclOutPipe.outLog[1]
	if len(f0) != 64 || len(f1) != 27 {
		t.Fatalf("unexpected fragment USB lengths %d, %d (want 64, 27)", len(f0), len(f1))
	}
	hdr0 := binary.LittleEndian.Uint16(f0[0:2])
	hdr1 := binary.LittleEndian.Uint16(f1[0:2])
	if hdr0 != (0x0042|uint16(ACLBoundaryFirstAutoFlush)<<12) || hdr1 != (0x0042|uint16(ACLBoundaryContinuing)<<12) {
		t.Fatalf("unexpected fragment PB headers %#04x %#04x", hdr0, hdr1)
	}
	if !bytes.Equal(f0[4:], payload[:60]) || !bytes.Equal(f1[4:], payload[60:]) {
		t.Fatal("MPS-capped ACL fragment payload mismatch")
	}
}

func TestReassembleChunkedHCIEventsAndACLFrames(t *testing.T) {
	bus := newMockBTBus(0x0bda, 0xa728, 1)
	bus.rtlVersion = 0xd922 // already patched so Initialize only does cmd traffic
	tr, err := Open(bus, bus.dev)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if err := tr.Initialize(nil); err != nil {
		t.Fatal(err)
	}

	// 1. Chunk a 17-byte EventInquiryResult (16B + 1B) AND a 257-byte
	// EventRemoteNameRequestComplete (seventeen <=16B chunks) for Service().
	inqEv := make([]byte, 17)
	inqEv[0] = EventInquiryResult
	inqEv[1] = 15
	inqEv[2] = 1
	for i := 3; i < len(inqEv); i++ {
		inqEv[i] = byte(0xa0 + i)
	}
	nameEv := make([]byte, 257)
	nameEv[0] = EventRemoteNameRequestComplete
	nameEv[1] = 255
	nameEv[2] = 0
	copy(nameEv[3:9], []byte{1, 2, 3, 4, 5, 6})
	copy(nameEv[9:], "DualSense Wireless Controller")

	// Feed inqEv as 16B then 1B across two separate Service() ticks first.
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, inqEv[:16])
	var events [][]byte
	if err := tr.Service(func(ev []byte) { events = append(events, append([]byte(nil), ev...)) }, nil); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("partial 16B HCI event should not dispatch early, got %x", events)
	}
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, inqEv[16:])
	if err := tr.Service(func(ev []byte) { events = append(events, append([]byte(nil), ev...)) }, nil); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !bytes.Equal(events[0], inqEv) {
		t.Fatalf("reassembled InquiryResult mismatch: %x", events)
	}

	// Feed 257B RemoteNameRequestComplete split into 16B USB packets.
	for off := 0; off < len(nameEv); off += 16 {
		bus.evPipe.inQueue = append(bus.evPipe.inQueue, nameEv[off:min(off+16, len(nameEv))])
	}
	events = nil
	if err := tr.Service(func(ev []byte) { events = append(events, append([]byte(nil), ev...)) }, nil); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !bytes.Equal(events[0], nameEv) {
		t.Fatalf("reassembled RemoteNameRequestComplete len=%d/%d", len(events[0]), len(nameEv))
	}

	// 2. Also verify SendCommand reassembles chunked asynchronous events and
	// preserves them intact in the deferred queue alongside CommandComplete.
	bus.evPipe.inQueue = append(bus.evPipe.inQueue, inqEv[:16], inqEv[16:])
	if err := tr.WriteScanEnable(ScanPageOnly); err != nil {
		t.Fatalf("WriteScanEnable during chunked event: %v", err)
	}
	events = nil
	if err := tr.Service(func(ev []byte) { events = append(events, append([]byte(nil), ev...)) }, nil); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !bytes.Equal(events[0], inqEv) {
		t.Fatalf("deferred chunked InquiryResult mismatch: %x", events)
	}

	// 3. Chunk an 87-byte DualSense ACL RX frame across 64B + 23B USB bulk-IN
	// packets both across separate Service() ticks and back-to-back.
	aclFrame := make([]byte, 87)
	binary.LittleEndian.PutUint16(aclFrame[0:2], 0x0042|uint16(ACLBoundaryFirstAutoFlush)<<12)
	binary.LittleEndian.PutUint16(aclFrame[2:4], 83)
	for i := 4; i < len(aclFrame); i++ {
		aclFrame[i] = byte(i)
	}

	var gotACL [][]byte
	onACL := func(handle uint16, pb uint8, payload []byte) {
		if handle == 0x0042 && pb == ACLBoundaryFirstAutoFlush {
			gotACL = append(gotACL, append([]byte(nil), payload...))
		}
	}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, aclFrame[:64])
	if err := tr.Service(nil, onACL); err != nil {
		t.Fatal(err)
	}
	if len(gotACL) != 0 {
		t.Fatalf("partial 64B ACL chunk should not dispatch early, got %d frames", len(gotACL))
	}
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, aclFrame[64:])
	if err := tr.Service(nil, onACL); err != nil {
		t.Fatal(err)
	}
	if len(gotACL) != 1 || !bytes.Equal(gotACL[0], aclFrame[4:]) {
		t.Fatalf("reassembled ACL RX across ticks mismatch: got %d frames", len(gotACL))
	}

	gotACL = nil
	bus.aclInPipe.inQueue = append(bus.aclInPipe.inQueue, aclFrame[:64], aclFrame[64:])
	if err := tr.Service(nil, onACL); err != nil {
		t.Fatal(err)
	}
	if len(gotACL) != 1 || !bytes.Equal(gotACL[0], aclFrame[4:]) {
		t.Fatalf("reassembled ACL RX in one tick mismatch: got %d frames", len(gotACL))
	}
}
