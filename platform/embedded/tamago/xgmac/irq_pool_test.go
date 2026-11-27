// SPDX-License-Identifier: GPL-3.0-or-later

package xgmac

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	gnet "github.com/usbarmory/go-net"
)

type testCoherency struct {
	mu   sync.Mutex
	line uint64
	dsbs int
	dmbs int
}

func (c *testCoherency) CacheLineSize() uint64     { return c.line }
func (c *testCoherency) DataBarrier()              { c.mu.Lock(); c.dmbs++; c.mu.Unlock() }
func (c *testCoherency) SyncBarrier()              { c.mu.Lock(); c.dsbs++; c.mu.Unlock() }
func (c *testCoherency) CleanRange(uint64, uint64) {}
func (c *testCoherency) InvalidateRange(uint64, uint64) {
}
func (c *testCoherency) CleanInvalidateRange(uint64, uint64) {}

type mockGICAccess struct {
	mu      sync.Mutex
	regs32  map[uint64]uint32
	pending []uint32
}

func newMockGIC(t *testing.T) (*gic.Controller, *mockGICAccess) {
	t.Helper()
	const gicd, gicr = uint64(0x1d000000), uint64(0x1d060000)
	hw := &mockGICAccess{
		regs32: map[uint64]uint32{
			gicd + gic.GICD_TYPER: 9, // 320 INTIDs (covers Agilex 5 IRQEMAC0/1/2 = 222/239/256)
		},
	}
	ctrl := gic.NewWithAccess(gicd, gicr, hw)
	if err := ctrl.Init(); err != nil {
		t.Fatalf("gic.Init: %v", err)
	}
	return ctrl, hw
}

func (m *mockGICAccess) HasGICv3() bool { return true }
func (m *mockGICAccess) MPIDR() uint64  { return 0 }
func (m *mockGICAccess) Read32(addr uint64) uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.regs32[addr]
}
func (m *mockGICAccess) Write32(addr uint64, val uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	const gicd = uint64(0x1d000000)
	if addr >= gicd+gic.GICD_ISENABLER && addr < gicd+gic.GICD_ICENABLER {
		m.regs32[addr] |= val
		return
	}
	if addr >= gicd+gic.GICD_ICENABLER && addr < gicd+gic.GICD_ISPENDR {
		isenabler := addr - gic.GICD_ICENABLER + gic.GICD_ISENABLER
		m.regs32[isenabler] &^= val
		return
	}
	m.regs32[addr] = val
}
func (m *mockGICAccess) Read64(addr uint64) uint64 {
	if addr == 0x1d060000+gic.GICR_TYPER {
		return 1 << 4
	}
	return 0
}
func (m *mockGICAccess) Write64(uint64, uint64) {}
func (m *mockGICAccess) InitCPU(uint8)          {}
func (m *mockGICAccess) SendSGI(uint64)         {}
func (m *mockGICAccess) Ack() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) == 0 {
		return gic.SpuriousID
	}
	id := m.pending[0]
	m.pending = m.pending[1:]
	return id
}
func (m *mockGICAccess) End(uint32) {}

func (m *mockGICAccess) isEnabled(id uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	word := uint64(0x1d000000) + gic.GICD_ISENABLER + uint64(id/32)*4
	return m.regs32[word]&(1<<(id%32)) != 0
}

func (m *mockGICAccess) fire(ctrl *gic.Controller, id uint32) {
	m.mu.Lock()
	m.pending = append(m.pending, id)
	m.mu.Unlock()
	ctrl.Dispatch()
}

// irqHardware is a thread-safe XGMAC 2.10 hardware model backed by raw DMA RAM
// (matching PoolMemory) with real IOC/RI/NIS interrupt signaling and MMIO containment checks.
type irqHardware struct {
	mu            sync.Mutex
	regs          map[uint32]uint32
	phy           map[uint32]uint16
	dmaBase       uint64
	dmaRAM        []byte
	now           time.Time
	rxIndex       uint32
	txIndex       uint32
	frames        [][]byte
	mmioDisabled  bool
	mmioViolation bool
	irqCtrl       *gic.Controller
	irqHW         *mockGICAccess
	irqID         uint32
}

func newIRQHardware(dmaBase uint64, dmaRAM []byte) *irqHardware {
	return &irqHardware{
		regs: map[uint32]uint32{
			version:   0x21,
			features0: 1<<5 | 1<<1,
			features1: 8<<6 | 7,
			features2: 7<<18 | 7<<12,
		},
		phy: map[uint32]uint16{
			0:  1 << 12,
			1:  1<<8 | 1<<5 | 1<<3 | 1<<2,
			2:  0x001c,
			3:  0xc916,
			9:  1 << 9,
			10: 1 << 11,
		},
		dmaBase: dmaBase,
		dmaRAM:  dmaRAM,
		now:     time.Unix(1, 0),
	}
}

func (h *irqHardware) Now() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.now = h.now.Add(10 * time.Microsecond)
	return h.now
}

func (h *irqHardware) Read32(reg uint32) uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mmioDisabled {
		h.mmioViolation = true
		return 0
	}
	return h.regs[reg]
}

func (h *irqHardware) Write32(reg, value uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mmioDisabled {
		h.mmioViolation = true
		return
	}
	switch reg {
	case dmaMode:
		h.regs[reg] = 0
		h.rxIndex, h.txIndex = 0, 0
		return
	case mdioData:
		addr := h.regs[mdioAddress]
		h.regs[reg] = uint32(h.phy[addr&31])
		return
	case channelStatus:
		h.regs[reg] &= ^value
		return
	case txControl:
		if h.regs[reg]&1 != 0 && value&1 == 0 {
			h.regs[channelStatus] |= txStopped
		}
	case rxControl:
		if h.regs[reg]&1 != 0 && value&1 == 0 {
			h.regs[channelStatus] |= rxStopped
		}
	case txTail:
		base := uint32(h.dmaBase) + txRing
		idx := ((value-base)/16 + ringSize - 1) % ringSize
		desc := txRing + idx*16
		state := binary.LittleEndian.Uint32(h.dmaRAM[desc+12 : desc+16])
		size := binary.LittleEndian.Uint32(h.dmaRAM[desc+8:desc+12]) & 0x3fff
		offset := txBuffers + idx*bufferSize
		h.frames = append(h.frames, bytes.Clone(h.dmaRAM[offset:offset+size]))
		binary.LittleEndian.PutUint32(h.dmaRAM[desc+12:desc+16], state&^own)
		h.txIndex = (h.txIndex + 1) % ringSize
	}
	h.regs[reg] = value
}

// deliverRX simulates the XGMAC DMA engine writing an incoming Ethernet frame
// into the next device-owned RX descriptor and firing the GIC SPI when IOC and
// NIE|RIE are set.
func (h *irqHardware) deliverRX(t *testing.T, frame []byte) bool {
	t.Helper()
	h.mu.Lock()
	desc := rxRing + h.rxIndex*16
	state := binary.LittleEndian.Uint32(h.dmaRAM[desc+12 : desc+16])
	if state&own == 0 {
		h.mu.Unlock()
		return false
	}
	hadIOC := state&rxIOC != 0
	copy(h.dmaRAM[rxBuffers+h.rxIndex*bufferSize:], frame)
	binary.LittleEndian.PutUint32(h.dmaRAM[desc:desc+4], 0xfeedface)
	binary.LittleEndian.PutUint32(h.dmaRAM[desc+12:desc+16], first|last|uint32(len(frame)))
	h.rxIndex = (h.rxIndex + 1) % ringSize
	var shouldFire bool
	if hadIOC {
		h.regs[channelStatus] |= statusRI | statusNIS
		if h.regs[channelInterrupt]&(irqNIE|irqRIE) == (irqNIE|irqRIE) && h.irqCtrl != nil && h.irqHW != nil {
			shouldFire = true
		}
	}
	ctrl, ghw, id := h.irqCtrl, h.irqHW, h.irqID
	h.mu.Unlock()
	if shouldFire {
		ghw.fire(ctrl, id)
	}
	return true
}

func TestDMAPagesSeparateDescriptorTXAndRX(t *testing.T) {
	// A non-page-aligned pool must still allocate whole, independently mappable pages.
	const base = 0x92000040
	pool, err := dmaops.NewPool(base, make([]byte, 64<<10), dmaops.Uncached, &testCoherency{line: 64})
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewPoolMemory(pool)
	if err != nil {
		t.Fatal(err)
	}
	if m.Base() != 0x92001000 || m.Size() != 5*4096 || m.Cap() != m.Size() {
		t.Fatalf("DMA allocation base=%#x size=%#x reserved=%#x", m.Base(), m.Size(), m.Cap())
	}

	if txRing != 0 || txRing+2*agilexRingSize*16 > 4096 || txBuffers != 4096 ||
		rxBuffers != 3*4096 || txBuffers+bufferCount*bufferSize > rxBuffers ||
		uint64(rxBuffers+bufferCount*bufferSize) > m.Size() {
		t.Fatal("descriptor, TX and RX spans exceed their dedicated pages")
	}
	// A cache-line-aligned raw Memory can bypass the pool allocator. Reject it
	// before touching either MMIO or the unaligned descriptor page.
	unaligned := &stopMemory{memory: newMemory(), base: base}
	hw := &hardware{}
	if _, err := Open(hw, unaligned, configuration()); err == nil || hw.accesses != 0 {
		t.Fatal("accepted raw DMA memory sharing a physical page with its neighbours")
	}
}

func TestPoolMemoryValidationRoundTripAndPoison(t *testing.T) {
	coh := &testCoherency{line: 64}

	if _, err := NewPoolMemory(nil); err == nil {
		t.Fatal("nil pool should reject")
	}
	cachedPool, err := dmaops.NewPool(0x92000000, make([]byte, 64<<10), dmaops.Cached, coh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPoolMemory(cachedPool); err == nil {
		t.Fatal("cached pool should reject")
	}
	smallPool, err := dmaops.NewPool(0x92000000, make([]byte, 4096), dmaops.Uncached, coh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPoolMemory(smallPool); err == nil {
		t.Fatal("undersized pool should reject")
	}
	highPool, err := dmaops.NewPool(0x1_0000_0000, make([]byte, 64<<10), dmaops.Uncached, coh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPoolMemory(highPool); err == nil {
		t.Fatal("above-4GiB pool should reject")
	}
	if highPool.Stats().LiveBuffers != 0 {
		t.Fatalf("above-4GiB rejection leaked buffer: %+v", highPool.Stats())
	}

	// Open XGMAC over PoolMemory, reject duplicate adoption, exchange a frame,
	// then trigger a DMA fault and verify PoolMemory is permanently poisoned and pinned.
	ram := make([]byte, 64<<10)
	pool, err := dmaops.NewPool(0x92000000, ram, dmaops.Uncached, coh)
	if err != nil {
		t.Fatal(err)
	}
	pm, err := NewPoolMemory(pool)
	if err != nil {
		t.Fatal(err)
	}
	if pm.Base() != 0x92000000 || pm.Size() != RequiredBytes || pm.Cap() < RequiredBytes || pm.Err() != nil {
		t.Fatalf("unexpected PoolMemory geometry: base=%#x size=%d cap=%d err=%v", pm.Base(), pm.Size(), pm.Cap(), pm.Err())
	}
	hw := newIRQHardware(pm.Base(), ram)
	dev, err := Open(hw, pm, configuration())
	if err != nil {
		t.Fatalf("Open with PoolMemory: %v", err)
	}
	// A PoolMemory adopted by an active device must reject re-adoption.
	if _, err := Open(hw, pm, configuration()); err == nil {
		t.Fatal("expected duplicate PoolMemory adoption to reject")
	}
	frame := bytes.Repeat([]byte{0x5a}, 64)
	if !hw.deliverRX(t, frame) {
		t.Fatal("deliverRX failed")
	}
	got := make([]byte, 1600)
	n, err := dev.Receive(got)
	if err != nil || !bytes.Equal(got[:n], frame) {
		t.Fatalf("Receive via PoolMemory: n=%d err=%v", n, err)
	}
	if err := dev.Transmit(frame); err != nil {
		t.Fatalf("Transmit via PoolMemory: %v", err)
	}
	if len(hw.frames) != 1 || !bytes.Equal(hw.frames[0], frame) {
		t.Fatal("Transmit frame mismatch via PoolMemory")
	}
	if coh.dsbs == 0 {
		t.Fatal("expected PoolMemory.Barrier to invoke SyncBarrier")
	}

	// Trigger fatal DMA bus fault -> fail() must poison PoolMemory.
	hw.mu.Lock()
	hw.regs[channelStatus] |= 1 << 12
	hw.mu.Unlock()
	if _, err := dev.Receive(got); err == nil {
		t.Fatal("expected DMA bus fault error")
	}
	if pm.Err() == nil {
		t.Fatal("expected non-nil PoolMemory.Err() after DMA fault")
	}
	if st := pool.Stats(); st.PoisonedBuffers != 1 || st.PinnedBytes != pm.Cap() {
		t.Fatalf("expected PoolMemory permanently pinned after fault: %+v", st)
	}
}

func TestXGMACIRQWakeupLinkControlAndFaults(t *testing.T) {
	const irqEMAC0 = uint32(222)
	coh := &testCoherency{line: 64}
	ram := make([]byte, 128<<10)
	pool, err := dmaops.NewPool(0x92000000, ram, dmaops.Uncached, coh)
	if err != nil {
		t.Fatal(err)
	}
	pmPoll, err := NewPoolMemory(pool)
	if err != nil {
		t.Fatal(err)
	}
	hw := newIRQHardware(pmPoll.Base(), ram)

	// Invalid SPI IDs must reject at Open before adopting memory.
	badCfg := configuration()
	badCfg.IRQ = 15 // SGI, not SPI
	if _, err := Open(hw, pmPoll, badCfg); err == nil {
		t.Fatal("expected rejection for non-SPI IRQ ID")
	}
	// Polling-only (IRQ == 0) returns gic.ErrUnsupported from EnableIRQ.
	g, ghw := newMockGIC(t)
	pollDev, err := Open(hw, pmPoll, configuration())
	if err != nil {
		t.Fatal(err)
	}
	if pollDev.RXReady() != nil {
		t.Fatal("polling device should have nil RXReady")
	}
	if err := pollDev.EnableIRQ(g); !errors.Is(err, gic.ErrUnsupported) {
		t.Fatalf("polling EnableIRQ: got %v want ErrUnsupported", err)
	}
	if binary.LittleEndian.Uint32(ram[rxRing+12:rxRing+16])&rxIOC != 0 {
		t.Fatal("polling mode should not set rxIOC on posted RX descriptors")
	}

	// Open a fresh PoolMemory with IRQEMAC0 bound.
	pm, err := NewPoolMemory(pool)
	if err != nil {
		t.Fatal(err)
	}
	pmOff := pm.Base() - pool.Base()
	hw = newIRQHardware(pm.Base(), ram[pmOff:pmOff+pm.Size()])
	hw.irqCtrl = g
	hw.irqHW = ghw
	hw.irqID = irqEMAC0
	cfg := configuration()
	cfg.IRQ = irqEMAC0
	dev, err := Open(hw, pm, cfg)
	if err != nil {
		t.Fatalf("Open with IRQ: %v", err)
	}
	if binary.LittleEndian.Uint32(ram[pmOff+uint64(rxRing)+12:pmOff+uint64(rxRing)+16])&(own|rxIOC) != (own | rxIOC) {
		t.Fatal("IRQ-configured XGMAC must set OWN|IOC on posted RX descriptors")
	}
	if err := dev.EnableIRQ(nil); err == nil {
		t.Fatal("nil GIC should reject")
	}
	if err := dev.EnableIRQ(g); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	if err := dev.EnableIRQ(g); err == nil {
		t.Fatal("double EnableIRQ should reject")
	}
	if dev.RXReady() == nil {
		t.Fatal("expected non-nil RXReady after EnableIRQ")
	}
	// Drain initial startup wakeup token.
	select {
	case <-dev.RXReady():
	default:
	}

	// Empty Receive must keep channelInterrupt armed with NIE|RIE.
	dst := make([]byte, 1600)
	if n, err := dev.Receive(dst); err != nil || n != 0 {
		t.Fatalf("empty Receive: n=%d err=%v", n, err)
	}
	hw.mu.Lock()
	ie := hw.regs[channelInterrupt]
	hw.mu.Unlock()
	if ie != irqNIE|irqRIE {
		t.Fatalf("expected channelInterrupt=%#x after empty Receive, got %#x", irqNIE|irqRIE, ie)
	}

	// Deliver frame -> triggers GIC SPI 222 -> handler masks channelInterrupt=0,
	// clears RI|NIS, increments IRQCount, and wakes RXReady().
	frame1 := bytes.Repeat([]byte{0xab}, 128)
	if !hw.deliverRX(t, frame1) {
		t.Fatal("deliverRX frame1 failed")
	}
	select {
	case <-dev.RXReady():
	case <-time.After(time.Second):
		t.Fatal("RXReady did not wake on RX IRQ")
	}
	if dev.IRQCount() != 1 {
		t.Fatalf("IRQCount=%d want 1", dev.IRQCount())
	}
	hw.mu.Lock()
	if hw.regs[channelInterrupt] != 0 || hw.regs[channelStatus]&(statusRI|statusNIS) != 0 {
		t.Fatalf("handler did not mask/clear RX IRQ: ie=%#x st=%#x", hw.regs[channelInterrupt], hw.regs[channelStatus])
	}
	hw.mu.Unlock()

	n, err := dev.Receive(dst)
	if err != nil || !bytes.Equal(dst[:n], frame1) {
		t.Fatalf("Receive frame1: n=%d err=%v", n, err)
	}
	// Re-arm path catches a packet that arrives while channelInterrupt is still masked.
	frame2 := bytes.Repeat([]byte{0xcd}, 96)
	if !hw.deliverRX(t, frame2) {
		t.Fatal("deliverRX frame2 failed")
	}
	n, err = dev.Receive(dst)
	if err != nil || !bytes.Equal(dst[:n], frame2) {
		t.Fatalf("Receive frame2 (masked-window batch): n=%d err=%v", n, err)
	}
	// Draining to empty re-arms channelInterrupt for the next IRQ.
	if n, err := dev.Receive(dst); err != nil || n != 0 {
		t.Fatalf("empty drain Receive: n=%d err=%v", n, err)
	}

	// Exercise real network.Link.Start sleeping on RXReady() and waking via GIC SPI 222.
	stackCfg := gnet.DefaultLnetoStackConfig()
	stackCfg.Hostname = "xgmac-irq-test"
	link, err := network.NewLink(stackCfg, dev, "10.0.2.15/24", dev.MAC(), "10.0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	linkDone := make(chan error, 1)
	go func() { linkDone <- link.Start(ctx) }()

	// Wait briefly for Link.Start to enter RXReady() wait, then fire an Ethernet frame.
	time.Sleep(15 * time.Millisecond)
	beforeIRQ := dev.IRQCount()
	ethFrame := make([]byte, 60)
	copy(ethFrame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(ethFrame[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	ethFrame[12], ethFrame[13] = 0x08, 0x06 // ARP ethertype
	if !hw.deliverRX(t, ethFrame) {
		t.Fatal("deliverRX ethFrame failed")
	}
	deadline := time.Now().Add(time.Second)
	for dev.Statistics().Received < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if dev.Statistics().Received < 3 || dev.IRQCount() <= beforeIRQ {
		t.Fatalf("Link.Start did not wake on XGMAC IRQ: received=%d irqs=%d (before=%d)", dev.Statistics().Received, dev.IRQCount(), beforeIRQ)
	}
	cancel()
	<-linkDone

	// Exercise BindControl Stop/Start: Stop must suspend IRQ and prevent ANY MMIO
	// while contained; Start must restore IRQ delivery.
	ctrlHooks, err := dev.BindControl(network.DMAControl{
		Stop: func(context.Context) error {
			hw.mu.Lock()
			hw.mmioDisabled = true
			hw.mu.Unlock()
			return nil
		},
		Start: func(context.Context) error {
			hw.mu.Lock()
			hw.mmioDisabled = false
			hw.regs[channelStatus] = 0
			hw.mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("BindControl: %v", err)
	}
	if err := ctrlHooks.Stop(context.Background()); err != nil {
		t.Fatalf("BindControl.Stop: %v", err)
	}
	if dev.RXReady() != nil {
		t.Fatal("RXReady must be nil while paused")
	}
	// Even a spurious GIC dispatch while contained must perform ZERO MMIO.
	ghw.fire(g, irqEMAC0)
	hw.mu.Lock()
	violated := hw.mmioViolation
	hw.mu.Unlock()
	if violated {
		t.Fatal("IRQ handler touched XGMAC MMIO while device was contained")
	}

	if err := ctrlHooks.Start(context.Background()); err != nil {
		t.Fatalf("BindControl.Start: %v", err)
	}
	if dev.RXReady() == nil {
		t.Fatal("RXReady must be restored after BindControl.Start")
	}
	select {
	case <-dev.RXReady():
	default:
	}
	beforeIRQ = dev.IRQCount()
	frame3 := bytes.Repeat([]byte{0xef}, 80)
	if !hw.deliverRX(t, frame3) {
		t.Fatal("deliverRX after Start failed")
	}
	if dev.IRQCount() <= beforeIRQ {
		t.Fatalf("expected IRQ wakeup after BindControl.Start: before=%d after=%d", beforeIRQ, dev.IRQCount())
	}

	// DisableIRQ cleanly unregisters SPI and clears channelInterrupt.
	dev.DisableIRQ(g)
	if dev.RXReady() != nil {
		t.Fatal("RXReady must be nil after DisableIRQ")
	}
	if ghw.isEnabled(irqEMAC0) {
		t.Fatal("GIC SPI should be masked after DisableIRQ")
	}
}
