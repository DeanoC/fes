// SPDX-License-Identifier: GPL-3.0-or-later
package bt

import (
	"context"
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type mgrPipe struct {
	ep       usb.EndpointDescriptor
	inQueue  [][]byte
	outCount int
	closed   bool
}

func (p *mgrPipe) DeviceAddress() uint8             { return 2 }
func (p *mgrPipe) Endpoint() usb.EndpointDescriptor { return p.ep }
func (p *mgrPipe) SetIntervalOverride(time.Duration) {}
func (p *mgrPipe) Close() error                     { p.closed = true; return nil }
func (p *mgrPipe) WriteDone() (bool, error)         { return true, nil }
func (p *mgrPipe) Write([]byte) error               { p.outCount++; return nil }
func (p *mgrPipe) PollInto(dst []byte) (int, error) {
	if len(p.inQueue) == 0 {
		return 0, nil
	}
	pkt := p.inQueue[0]
	p.inQueue = p.inQueue[1:]
	return copy(dst, pkt), nil
}

type mgrBus struct {
	events     []usb.HostEvent
	evPipe     *mgrPipe
	aclInPipe  *mgrPipe
	aclOutPipe *mgrPipe
	rtlChunks  int
	lmpSub     uint16
}

func (b *mgrBus) Service() error             { return nil }
func (b *mgrBus) Devices() []usb.BusDevice   { return nil }
func (b *mgrBus) Ready() <-chan struct{}     { return nil }
func (b *mgrBus) NextEvent() (usb.HostEvent, bool) {
	if len(b.events) == 0 {
		return usb.HostEvent{}, false
	}
	ev := b.events[0]
	b.events = b.events[1:]
	return ev, true
}
func (b *mgrBus) OpenPipe(_ uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	p := &mgrPipe{ep: ep}
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
func (b *mgrBus) ControlTransfer(_ uint8, reqType, req byte, _, _ uint16, data []byte) (int, error) {
	if reqType != 0x20 || req != 0 || len(data) < 3 {
		return len(data), nil
	}
	op := binary.LittleEndian.Uint16(data[0:2])
	ev := []byte{hci.EventCommandComplete, 4, 1, byte(op), byte(op >> 8), 0}
	switch op {
	case hci.OpcodeReadLocalVersion:
		ev = []byte{hci.EventCommandComplete, 12, 1, byte(op), byte(op >> 8), 0, 0x0d, 0, 0, 0x0d, 0x5d, 0, byte(b.lmpSub), byte(b.lmpSub >> 8)}
	case 0xfc6d: // RTL Read ROM Version
		ev = []byte{hci.EventCommandComplete, 5, 1, byte(op), byte(op >> 8), 0, 0x01}
	case 0xfc20: // RTL Download
		b.rtlChunks++
		if len(data) >= 4 && data[3]&0x80 != 0 {
			b.lmpSub = 0xd922
		}
		ev = []byte{hci.EventCommandComplete, 5, 1, byte(op), byte(op >> 8), 0, data[3]}
	case hci.OpcodeReadBufferSize:
		ev = []byte{hci.EventCommandComplete, 11, 1, byte(op), byte(op >> 8), 0, 0x00, 0x01, 0x40, 0x10, 0x00, 0x04, 0x00}
	case hci.OpcodeReadBDAddr:
		ev = []byte{hci.EventCommandComplete, 10, 1, byte(op), byte(op >> 8), 0, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	}
	b.evPipe.inQueue = append(b.evPipe.inQueue, ev)
	return len(data), nil
}

type testFWLoader map[string][]byte

func (l testFWLoader) LoadFirmware(name string) ([]byte, error) {
	b, ok := l[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return b, nil
}

func makeDongleDevice(addr uint8) usb.BusDevice {
	return usb.BusDevice{
		Address: addr,
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
}

func TestManagerHotplug0BDAA728GameplayPairingGuardAndDetachNeutralize(t *testing.T) {
	fwPath := filepath.Join("..", "..", "..", "..", "image", "firmware", "rtl_bt", "rtl8761bu_fw.bin")
	cfgPath := filepath.Join("..", "..", "..", "..", "image", "firmware", "rtl_bt", "rtl8761bu_config.bin")
	fwBytes, err := os.ReadFile(fwPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	fw := testFWLoader{"rtl8761bu_fw.bin": fwBytes, "rtl8761bu_config.bin": cfgBytes}

	bus := &mgrBus{lmpSub: 0x8761}
	bonds := l2cap.NewBondStore()
	now := time.Unix(300, 0)
	mgr := NewManager(bus, bonds, fw, func() time.Time { return now })

	var dongleStates []bool
	mgr.OnDongleChanged = func(connected bool, _ hci.DongleSpec) {
		dongleStates = append(dongleStates, connected)
	}

	// Boot with no dongle attached -> Service succeeds cleanly.
	if err := mgr.Service(); err != nil || mgr.DongleConnected() {
		t.Fatalf("empty boot: err=%v connected=%t", err, mgr.DongleConnected())
	}

	// Hot-attach Deano's 0bda:a728 Realtek BT 5.4 dongle!
	dev := makeDongleDevice(2)
	bus.events = append(bus.events, usb.HostEvent{Kind: usb.DeviceAttached, Device: dev})
	if err := mgr.Service(); err != nil || !mgr.DongleConnected() {
		t.Fatalf("attach 0bda:a728 failed: err=%v connected=%t", err, mgr.DongleConnected())
	}
	if bus.rtlChunks == 0 || mgr.DongleSpec().ProductID != 0xa728 {
		t.Fatalf("expected RTL8761BU upload on 0bda:a728: chunks=%d spec=%+v", bus.rtlChunks, mgr.DongleSpec())
	}

	// Pairing succeeds in menu, is refused during gameplay.
	if err := mgr.StartPairing(30 * time.Second); err != nil || !mgr.PairingActive() {
		t.Fatalf("StartPairing in menu: err=%v active=%t", err, mgr.PairingActive())
	}
	if err := mgr.SetGameplayActive(true); err != nil || mgr.PairingActive() {
		t.Fatalf("SetGameplayActive(true) did not close pairing: err=%v active=%t", err, mgr.PairingActive())
	}
	if err := mgr.StartPairing(30 * time.Second); err == nil {
		t.Fatal("StartPairing allowed while gameplay active")
	}
	if err := mgr.SetGameplayActive(false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.StopPairing(); err != nil {
		t.Fatal(err)
	}
	if mgr.ConnectedPlayers() != 0 || mgr.Bonds() != bonds {
		t.Fatal("unexpected player count or bond store")
	}
	if _, ok := mgr.Player(0); ok {
		t.Fatal("unexpected player 0")
	}
	if err := mgr.SetRumble(0, 0, 0); err == nil {
		t.Fatal("SetRumble succeeded on disconnected slot")
	}

	// Hot-detach dongle cleanly closes pipes and transitions DongleConnected -> false.
	bus.events = append(bus.events, usb.HostEvent{Kind: usb.DeviceDetached, Device: dev})
	if err := mgr.Service(); err != nil || mgr.DongleConnected() {
		t.Fatalf("detach 0bda:a728 failed: err=%v connected=%t", err, mgr.DongleConnected())
	}
	if !bus.evPipe.closed || !bus.aclInPipe.closed || !bus.aclOutPipe.closed {
		t.Fatal("dongle detach leaked USB pipes")
	}
	if len(dongleStates) != 2 || !dongleStates[0] || dongleStates[1] {
		t.Fatalf("unexpected dongle state transitions: %v", dongleStates)
	}
}

type memBondStorage struct {
	bonds []byte
	fw    map[string][]byte
}

func (m *memBondStorage) ReadBTBonds(context.Context) ([]byte, error) {
	if len(m.bonds) == 0 {
		return nil, fs.ErrNotExist
	}
	return m.bonds, nil
}
func (m *memBondStorage) WriteBTBonds(_ context.Context, data []byte) error {
	m.bonds = append([]byte(nil), data...)
	return nil
}
func (m *memBondStorage) ReadBTFirmware(_ context.Context, name string) ([]byte, error) {
	b, ok := m.fw[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return b, nil
}

func TestStoreAdaptersLoadSaveBondsAndFirmware(t *testing.T) {
	ctx := context.Background()
	st := &memBondStorage{fw: map[string][]byte{"rtl8761bu_config.bin": {0x55, 0xab, 0x23, 0x87, 0, 0}}}
	bonds, err := LoadBonds(ctx, st)
	if err != nil || len(bonds.List()) != 0 {
		t.Fatalf("empty LoadBonds: err=%v list=%v", err, bonds.List())
	}
	addr, _ := l2cap.ParseBDAddr("DE:AD:BE:EF:00:01")
	_ = bonds.Upsert(l2cap.BondRecord{Addr: addr, LinkKey: [16]byte{7}, Name: "Pro Controller", Kind: controllers.KindSwitchPro})
	if err := SaveBondsIfDirty(ctx, st, bonds); err != nil || bonds.Dirty() {
		t.Fatalf("SaveBondsIfDirty: err=%v dirty=%t", err, bonds.Dirty())
	}
	reloaded, err := LoadBonds(ctx, st)
	if err != nil {
		t.Fatalf("LoadBonds reloaded: %v", err)
	}
	if rec, ok := reloaded.Lookup(addr); !ok || rec.Kind != controllers.KindSwitchPro {
		t.Fatalf("reloaded bond mismatch: ok=%t rec=%+v", ok, rec)
	}
	loader := NewStoreFirmwareLoader(ctx, st)
	if b, err := loader.LoadFirmware("rtl8761bu_config.bin"); err != nil || len(b) != 6 {
		t.Fatalf("LoadFirmware: len=%d err=%v", len(b), err)
	}
}
