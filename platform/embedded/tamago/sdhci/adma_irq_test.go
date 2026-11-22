// SPDX-License-Identifier: GPL-3.0-or-later

package sdhci

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

type testCoherency struct {
	mu     sync.Mutex
	line   uint64
	cleans int
	invs   int
	clInvs int
	dmbs   int
	dsbs   int
}

func (c *testCoherency) CacheLineSize() uint64 { return c.line }
func (c *testCoherency) DataBarrier()          { c.mu.Lock(); c.dmbs++; c.mu.Unlock() }
func (c *testCoherency) SyncBarrier()          { c.mu.Lock(); c.dsbs++; c.mu.Unlock() }
func (c *testCoherency) CleanRange(uint64, uint64) {
	c.mu.Lock()
	c.cleans++
	c.mu.Unlock()
}
func (c *testCoherency) InvalidateRange(uint64, uint64) {
	c.mu.Lock()
	c.invs++
	c.mu.Unlock()
}
func (c *testCoherency) CleanInvalidateRange(uint64, uint64) {
	c.mu.Lock()
	c.clInvs++
	c.mu.Unlock()
}

// mockGICAccess drives a real *gic.Controller on the host for SDHCI IRQ tests.
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
			gicd + gic.GICD_TYPER: 7, // 256 INTIDs (covers Agilex 5 IRQSDMMC = 128)
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
	m.regs32[addr] = val
}
func (m *mockGICAccess) Read64(addr uint64) uint64 {
	if addr == 0x1d060000+gic.GICR_TYPER {
		return 1 << 4 // LAST=1, affinity=0
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

func (m *mockGICAccess) fire(ctrl *gic.Controller, id uint32) {
	m.mu.Lock()
	m.pending = append(m.pending, id)
	m.mu.Unlock()
	ctrl.Dispatch()
}

// advancedCard models Cadence SDHCI 3.0 with SCR/4-bit, CMD18/CMD25 multi-block,
// real ADMA2 descriptor-table traversal over a backing RAM window, and IRQ firing.
type advancedCard struct {
	mu             sync.Mutex
	regs           map[uint32]uint32
	tick           time.Time
	image          []byte
	dmaBase        uint64
	dmaRAM         []byte
	scr            [8]byte
	busWidth       int
	acmd           bool
	rca            uint16
	commands       []uint32
	pioBuf         []byte
	pioPos         int
	pioWriteLBA    uint32
	pioWriteLeft   int
	admaFault      bool
	rejectADMA     bool
	delayCMD13Poll int
	irqCtrl        *gic.Controller
	irqHW          *mockGICAccess
	irqID          uint32
	asyncIRQ       bool
}

func newAdvancedCard(sectors int, dmaBase uint64, dmaRAM []byte, caps uint32) *advancedCard {
	img := make([]byte, sectors*512)
	for i := range img {
		img[i] = byte((i*31 + i/512*17) & 0xff)
	}
	return &advancedCard{
		regs: map[uint32]uint32{
			host:         1 << 8,
			clock:        7,
			present:      1<<19 | 1<<10, // writable socket + buffer write enable
			capabilities: caps,
		},
		tick:     time.Unix(1, 0),
		image:    img,
		dmaBase:  dmaBase,
		dmaRAM:   dmaRAM,
		scr:      [8]byte{0x02, 0x35, 0, 0, 0, 0, 0, 0}, // SD_BUS_WIDTHS=5 (1-bit + 4-bit)
		busWidth: 1,
		rca:      9,
	}
}

func (c *advancedCard) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tick = c.tick.Add(time.Millisecond)
	return c.tick
}

func (c *advancedCard) Read32(reg uint32) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if reg == status && c.delayCMD13Poll > 0 {
		c.delayCMD13Poll--
		if c.delayCMD13Poll == 0 {
			c.regs[status] |= 1
		}
	}
	if reg == dataPort {
		if c.pioPos+4 > len(c.pioBuf) {
			return 0
		}
		v := binary.LittleEndian.Uint32(c.pioBuf[c.pioPos : c.pioPos+4])
		c.pioPos += 4
		if c.pioPos == len(c.pioBuf) {
			c.regs[status] |= 2 // Transfer Complete
		} else if c.pioPos%512 == 0 {
			c.regs[status] |= 1 << 5 // Next block ready
		}
		return v
	}
	return c.regs[reg]
}

func (c *advancedCard) Write32(reg, value uint32) {
	c.mu.Lock()
	var fireIRQ bool
	defer func() {
		if fireIRQ && c.irqCtrl != nil && c.irqHW != nil {
			go c.irqHW.fire(c.irqCtrl, c.irqID)
		}
	}()
	defer c.mu.Unlock()

	if reg == status {
		c.regs[status] &= ^value
		return
	}
	if reg == signalEnable {
		c.regs[signalEnable] = value
		if value != 0 && c.regs[status]&value != 0 {
			fireIRQ = true
		}
		return
	}
	if reg == clock {
		c.regs[clock] = value &^ (7 << 24)
		if value&1 != 0 {
			c.regs[clock] |= 2
		}
		return
	}
	if reg == dataPort {
		if c.pioWriteLeft > 0 {
			offset := int(c.pioWriteLBA)*512 + c.pioPos
			binary.LittleEndian.PutUint32(c.image[offset:offset+4], value)
			c.pioPos += 4
			if c.pioPos == c.pioWriteLeft {
				c.pioWriteLeft = 0
				c.regs[status] |= 2 // Transfer Complete
			} else if c.pioPos%512 == 0 {
				c.regs[status] |= 1 << 4 // Next block buffer write ready
			}
		}
		return
	}
	if reg == host && c.rejectADMA && value&(3<<3) != 0 {
		// Simulate hardware refusing the requested ADMA2 mode bit.
		c.regs[host] = value &^ (3 << 3)
		return
	}
	c.regs[reg] = value
	if reg != command {
		return
	}
	c.commands = append(c.commands, value)
	index := value >> 24
	c.regs[response] = 0
	switch index {
	case 0:
	case 8:
		c.regs[response] = c.regs[argument] & 0xfff
	case 55:
		c.acmd = true
		c.regs[response] = 1 << 5
	case 41:
		c.acmd = false
		c.regs[response] = 0xc0300000
	case 2:
		c.regs[response+0] = 0x11223344
		c.regs[response+4] = 0x55667788
		c.regs[response+8] = 0x99aabbcc
		c.regs[response+12] = 0x00ddeeff
	case 3:
		c.regs[response] = uint32(c.rca) << 16
	case 9:
		csize := uint32(len(c.image)/512/1024 - 1)
		c.regs[response+0] = 0
		c.regs[response+4] = csize << 8
		c.regs[response+8] = 0x32 << 24 // 25 MHz default speed
		c.regs[response+12] = 1 << 22   // CSD v2
	case 7:
		c.regs[response] = 4<<9 | 1<<8
		c.regs[status] |= 2
	case 13:
		c.regs[response] = 4<<9 | 1<<8
	case 6:
		if c.acmd {
			c.acmd = false
			if c.regs[argument] == 2 {
				c.busWidth = 4
			}
		}
		c.regs[response] = 4<<9 | 1<<8
	case 51:
		c.acmd = false
		c.pioBuf = append([]byte(nil), c.scr[:]...)
		c.pioPos = 0
		c.regs[status] |= 1 << 5 // Buffer Read Ready
	case 17, 18:
		lba := c.regs[argument]
		blocks := c.regs[blockSize] >> 16
		total := int(blocks) * 512
		if value&1 != 0 {
			c.runADMA2Locked(lba, total, true)
		} else {
			c.pioBuf = append([]byte(nil), c.image[int(lba)*512:int(lba)*512+total]...)
			c.pioPos = 0
			c.regs[status] |= 1 << 5
		}
	case 24, 25:
		lba := c.regs[argument]
		blocks := c.regs[blockSize] >> 16
		total := int(blocks) * 512
		if value&1 != 0 {
			c.runADMA2Locked(lba, total, false)
		} else {
			c.pioWriteLBA = lba
			c.pioPos = 0
			c.pioWriteLeft = total
			c.regs[status] |= 1 << 4
		}
	}
	c.regs[status] |= 1 // Command Complete
	if c.delayCMD13Poll > 0 && index == 13 {
		// Delay CMD13 completion for several status reads without firing an IRQ.
		c.regs[status] &^= 1
	}
	if c.asyncIRQ && (index == 17 || index == 18 || index == 24 || index == 25) && value&1 != 0 {
		// Defer Transfer Complete slightly so waitStatusIRQ exercises the IRQ wakeup path.
		c.regs[status] &^= 2
		go func() {
			time.Sleep(2 * time.Millisecond)
			c.mu.Lock()
			c.regs[status] |= 2
			sig := c.regs[signalEnable]
			c.mu.Unlock()
			if sig&2 != 0 && c.irqCtrl != nil && c.irqHW != nil {
				c.irqHW.fire(c.irqCtrl, c.irqID)
			}
		}()
	}
}

func (c *advancedCard) runADMA2Locked(lba uint32, total int, isRead bool) {
	if c.admaFault {
		c.regs[admaError] = 0x5
		c.regs[status] |= 1 << 25 // ADMA Error
		return
	}
	use64 := (c.regs[host] & (3 << 3)) == hostADMA2_64
	descAddr := uint64(c.regs[admaAddrLow]) | uint64(c.regs[admaAddrHigh])<<32
	descOff := descAddr - c.dmaBase
	copied := 0
	imgOff := int(lba) * 512
	for {
		hdr := binary.LittleEndian.Uint32(c.dmaRAM[descOff : descOff+4])
		if hdr&admaAttrValid == 0 || hdr&admaAttrTran == 0 {
			c.regs[admaError] = 0x1
			c.regs[status] |= 1 << 25
			return
		}
		chunk := int(hdr >> 16)
		if chunk == 0 {
			chunk = 65536
		}
		var bufAddr uint64
		if use64 {
			// SDHCI v3 64-bit ADMA2 uses 96-bit (12-byte) descriptors:
			// +0 attr/len, +4 addr low32, +8 addr high32.
			lo := uint64(binary.LittleEndian.Uint32(c.dmaRAM[descOff+4 : descOff+8]))
			hi := uint64(binary.LittleEndian.Uint32(c.dmaRAM[descOff+8 : descOff+12]))
			bufAddr = lo | (hi << 32)
			descOff += 12
		} else {
			bufAddr = uint64(binary.LittleEndian.Uint32(c.dmaRAM[descOff+4 : descOff+8]))
			descOff += 8
		}
		bufOff := bufAddr - c.dmaBase
		if isRead {
			copy(c.dmaRAM[bufOff:bufOff+uint64(chunk)], c.image[imgOff+copied:imgOff+copied+chunk])
		} else {
			copy(c.image[imgOff+copied:imgOff+copied+chunk], c.dmaRAM[bufOff:bufOff+uint64(chunk)])
		}
		copied += chunk
		if hdr&admaAttrEnd != 0 {
			break
		}
	}
	if copied != total {
		c.regs[admaError] = 0x4
		c.regs[status] |= 1 << 25
		return
	}
	c.regs[status] |= 2 // Transfer Complete
}

func TestBusWidth4BitAndMultiBlockPIO(t *testing.T) {
	hw := newAdvancedCard(2048, 0, nil, cap33V)
	dev, err := Open(hw, Config{
		InputClockHz:    200_000_000,
		TransferClockHz: 25_000_000,
		AllowWrites:     true,
		BusWidth4Bit:    true,
		MultiBlock:      true,
	})
	if err != nil {
		t.Fatalf("Open 4-bit multi-block: %v", err)
	}
	if dev.BusWidth() != 4 || dev.ADMA2Active() {
		t.Fatalf("unexpected mode: width=%d adma=%v", dev.BusWidth(), dev.ADMA2Active())
	}
	if hw.busWidth != 4 || hw.regs[host]&host4BitWidth == 0 {
		t.Fatalf("controller/card not switched to 4-bit: card=%d host=%#x", hw.busWidth, hw.regs[host])
	}

	// Read 140 sectors (spans >128 maxMultiBlocks: exercises CMD18 128 + CMD18 12).
	buf := make([]byte, 140*512)
	if err := dev.ReadSectors(10, buf); err != nil {
		t.Fatalf("ReadSectors 140: %v", err)
	}
	if !bytes.Equal(buf, hw.image[10*512:150*512]) {
		t.Fatal("multi-block PIO read mismatch")
	}

	// Write 130 sectors via CMD25 and verify byte-exact image mutation.
	wbuf := make([]byte, 130*512)
	for i := range wbuf {
		wbuf[i] = byte((i*13 + 9) & 0xff)
	}
	if err := dev.WriteSectors(20, wbuf); err != nil {
		t.Fatalf("WriteSectors 130: %v", err)
	}
	if !bytes.Equal(hw.image[20*512:150*512], wbuf) {
		t.Fatal("multi-block PIO write mismatch")
	}

	// Confirm CMD18 and CMD25 were issued with Multi-Block + Auto-CMD12 + BlockCount.
	var saw18, saw25 bool
	for _, cmd := range hw.commands {
		if cmd>>24 == 18 && cmd&0x36 == 0x36 {
			saw18 = true
		}
		if cmd>>24 == 25 && cmd&0x26 == 0x26 {
			saw25 = true
		}
	}
	if !saw18 || !saw25 {
		t.Fatalf("expected CMD18 and CMD25 multi-block commands (saw18=%v saw25=%v)", saw18, saw25)
	}

	// Quiesce and Resume must restore 4-bit mode and 25 MHz clock cleanly.
	if err := dev.Quiesce(); err != nil {
		t.Fatalf("Quiesce: %v", err)
	}
	if err := dev.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if dev.BusWidth() != 4 {
		t.Fatalf("BusWidth after resume=%d want 4", dev.BusWidth())
	}
	if err := dev.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Card without 4-bit bit in SCR must reject cleanly.
	hw1Bit := newAdvancedCard(2048, 0, nil, cap33V)
	hw1Bit.scr[1] = 0x31 // only 1-bit
	if _, err := Open(hw1Bit, Config{InputClockHz: 200_000_000, BusWidth4Bit: true}); err == nil {
		t.Fatal("expected 4-bit Open failure on 1-bit-only SCR")
	}
}

func TestADMA2ReadWriteCachedAnd64Bit(t *testing.T) {
	// 1. 32-bit ADMA2 on Cached pool (verifies PoC cache maintenance + 2-descriptor 64 KiB chain).
	const base32 = uint64(0x94800000)
	ram32 := make([]byte, 128<<10)
	coh := &testCoherency{line: 64}
	pool32, err := dmaops.NewPool(base32, ram32, dmaops.Cached, coh)
	if err != nil {
		t.Fatal(err)
	}
	hw32 := newAdvancedCard(2048, base32, ram32, cap33V|capADMA2)
	dev, err := Open(hw32, Config{
		InputClockHz:    200_000_000,
		TransferClockHz: 25_000_000,
		AllowWrites:     true,
		BusWidth4Bit:    true,
		MultiBlock:      true,
		UseADMA2:        true,
		DMAPool:         pool32,
	})
	if err != nil {
		t.Fatalf("Open ADMA2 32-bit: %v", err)
	}
	if !dev.ADMA2Active() || hw32.regs[host]&(3<<3) != hostADMA2_32 {
		t.Fatalf("expected 32-bit ADMA2 host bits, got %#x", hw32.regs[host])
	}

	// Read 140 sectors (64 KiB 2-descriptor chain + 6 KiB single-descriptor chain).
	got := make([]byte, 140*512)
	if err := dev.ReadSectors(4, got); err != nil {
		t.Fatalf("ADMA2 ReadSectors: %v", err)
	}
	if !bytes.Equal(got, hw32.image[4*512:144*512]) {
		t.Fatal("ADMA2 32-bit read data mismatch")
	}

	// Write 132 sectors via ADMA2 and read back.
	payload := make([]byte, 132*512)
	for i := range payload {
		payload[i] = byte((i*7 + 3) & 0xff)
	}
	if err := dev.WriteSectors(8, payload); err != nil {
		t.Fatalf("ADMA2 WriteSectors: %v", err)
	}
	if !bytes.Equal(hw32.image[8*512:140*512], payload) {
		t.Fatal("ADMA2 32-bit write data mismatch")
	}
	if coh.cleans == 0 || coh.invs == 0 || coh.clInvs == 0 {
		t.Fatalf("expected cached ADMA2 PoC ops, got cleans=%d invs=%d clInvs=%d", coh.cleans, coh.invs, coh.clInvs)
	}

	// Quiesce + Resume preserves ADMA2 cleanly and clears SRS03[0] DMA bit via CMD13.
	if err := dev.Quiesce(); err != nil {
		t.Fatalf("ADMA2 Quiesce: %v", err)
	}
	if err := dev.Resume(); err != nil {
		t.Fatalf("ADMA2 Resume: %v", err)
	}
	if err := dev.Close(); err != nil {
		t.Fatalf("ADMA2 Close: %v", err)
	}
	if st := pool32.Stats(); st.LiveBuffers != 0 || st.FreeSpans != 1 {
		t.Fatalf("ADMA2 Close leaked buffers: %+v", st)
	}

	// Close + Resume must reallocate and keep ADMA2 active, not fall back to PIO.
	if err := dev.Resume(); err != nil {
		t.Fatalf("ADMA2 Resume after Close: %v", err)
	}
	if !dev.ADMA2Active() {
		t.Fatal("ADMA2 inactive after Close + Resume")
	}
	if st := pool32.Stats(); st.LiveBuffers == 0 {
		t.Fatalf("ADMA2 Resume after Close did not allocate buffers: %+v", st)
	}
	if err := dev.Close(); err != nil {
		t.Fatalf("ADMA2 Close after Resume: %v", err)
	}
	if st := pool32.Stats(); st.LiveBuffers != 0 || st.FreeSpans != 1 {
		t.Fatalf("ADMA2 second Close leaked buffers: %+v", st)
	}

	// 2. 64-bit ADMA2 above 4 GiB.
	const base64 = uint64(0x1_0000_0000)
	ram64 := make([]byte, 128<<10)
	pool64, err := dmaops.NewPool(base64, ram64, dmaops.Uncached, coh)
	if err != nil {
		t.Fatal(err)
	}
	// Without cap64BitBus, Open must reject buffers above 4 GiB and release pool buffers.
	hwNo64 := newAdvancedCard(2048, base64, ram64, cap33V|capADMA2)
	if _, err := Open(hwNo64, Config{InputClockHz: 200_000_000, UseADMA2: true, DMAPool: pool64}); err == nil {
		t.Fatal("expected rejection without 64-bit bus support")
	}
	if pool64.Stats().LiveBuffers != 0 {
		t.Fatalf("failed Open leaked pool buffers: %+v", pool64.Stats())
	}

	hw64 := newAdvancedCard(2048, base64, ram64, cap33V|capADMA2|cap64BitBus)
	dev64, err := Open(hw64, Config{
		InputClockHz: 200_000_000,
		MultiBlock:   true,
		UseADMA2:     true,
		DMAPool:      pool64,
	})
	if err != nil {
		t.Fatalf("Open ADMA2 64-bit: %v", err)
	}
	if hw64.regs[host]&(3<<3) != hostADMA2_64 {
		t.Fatalf("expected 64-bit ADMA2 host bits, got %#x", hw64.regs[host])
	}
	// Transfer >32 KiB (80 sectors = 40 KiB) so two chained 96-bit (12-byte)
	// SDHCI v3 64-bit descriptors are built and traversed.
	got64 := make([]byte, 80*512)
	if err := dev64.ReadSectors(0, got64); err != nil {
		t.Fatalf("ADMA2 64-bit ReadSectors: %v", err)
	}
	if !bytes.Equal(got64, hw64.image[:80*512]) {
		t.Fatal("ADMA2 64-bit read mismatch")
	}
	// Verify second 96-bit descriptor starts at byte offset 12 with high32 == 1.
	descOff := dev64.adma.descBuf.PhysAddr() - base64
	if hi := binary.LittleEndian.Uint32(ram64[descOff+12+8 : descOff+12+12]); hi != 1 {
		t.Fatalf("expected 2nd 96-bit descriptor addr high32=1 at +20, got %#x", hi)
	}
	if err := dev64.Close(); err != nil {
		t.Fatal(err)
	}

	// Host-control readback rejection in configureADMA must free both ADMA2 buffers.
	hwReject := newAdvancedCard(2048, base64, ram64, cap33V|capADMA2|cap64BitBus)
	hwReject.rejectADMA = true
	if _, err := Open(hwReject, Config{InputClockHz: 200_000_000, UseADMA2: true, DMAPool: pool64}); err == nil {
		t.Fatal("expected Open error when ADMA2 host readback mismatches")
	}
	if st := pool64.Stats(); st.LiveBuffers != 0 || st.FreeSpans != 1 {
		t.Fatalf("configureADMA host readback failure leaked buffers: %+v", st)
	}
}

func TestADMA2FaultPoisonsDMAAndRetiresDevice(t *testing.T) {
	const base = uint64(0x94800000)
	ram := make([]byte, 128<<10)
	coh := &testCoherency{line: 64}
	pool, err := dmaops.NewPool(base, ram, dmaops.Uncached, coh)
	if err != nil {
		t.Fatal(err)
	}
	hw := newAdvancedCard(2048, base, ram, cap33V|capADMA2)
	dev, err := Open(hw, Config{
		InputClockHz: 200_000_000,
		MultiBlock:   true,
		UseADMA2:     true,
		DMAPool:      pool,
	})
	if err != nil {
		t.Fatal(err)
	}
	hw.admaFault = true
	if err := dev.ReadSectors(0, make([]byte, 4*512)); err == nil {
		t.Fatal("expected ADMA error")
	}
	if err := dev.ReadSectors(0, make([]byte, 512)); !errors.Is(err, ErrRetired) {
		t.Fatalf("expected ErrRetired after ADMA fault, got %v", err)
	}
	// Poisoned ADMA2 buffers must stay permanently pinned.
	if err := dev.Close(); !errors.Is(err, ErrRetired) {
		t.Fatalf("Close after ADMA fault: got %v", err)
	}
	st := pool.Stats()
	if st.PoisonedBuffers != 2 || st.PinnedBytes < maxMultiBlocks*512 {
		t.Fatalf("expected descBuf + dataBuf poisoned and pinned, got %+v", st)
	}
}

func TestSDHCIIRQDrivenADMA2AndQuiesceResume(t *testing.T) {
	const base = uint64(0x94800000)
	const irqSDMMC = uint32(128)
	ram := make([]byte, 128<<10)
	coh := &testCoherency{line: 64}
	pool, err := dmaops.NewPool(base, ram, dmaops.Uncached, coh)
	if err != nil {
		t.Fatal(err)
	}
	g, ghw := newMockGIC(t)
	hw := newAdvancedCard(2048, base, ram, cap33V|capADMA2)
	hw.irqCtrl = g
	hw.irqHW = ghw
	hw.irqID = irqSDMMC
	hw.asyncIRQ = true

	dev, err := Open(hw, Config{
		InputClockHz:    200_000_000,
		TransferClockHz: 25_000_000,
		AllowWrites:     true,
		BusWidth4Bit:    true,
		MultiBlock:      true,
		UseADMA2:        true,
		DMAPool:         pool,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.EnableIRQ(nil, irqSDMMC); err == nil {
		t.Fatal("nil GIC should reject")
	}
	if err := dev.EnableIRQ(g, g.Lines()); !errors.Is(err, gic.ErrInvalidConfig) {
		t.Fatalf("out-of-range INTID: got %v", err)
	}
	if err := dev.EnableIRQ(g, irqSDMMC); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	if err := dev.EnableIRQ(g, irqSDMMC); err == nil {
		t.Fatal("double EnableIRQ should reject")
	}

	got := make([]byte, 16*512)
	if err := dev.ReadSectors(2, got); err != nil {
		t.Fatalf("IRQ ADMA2 ReadSectors: %v", err)
	}
	if !bytes.Equal(got, hw.image[2*512:18*512]) {
		t.Fatal("IRQ ADMA2 read mismatch")
	}
	if dev.IRQCount() == 0 {
		t.Fatal("expected at least one SDHCI IRQ wakeup")
	}
	if hw.regs[signalEnable] != 0 {
		t.Fatalf("signalEnable must be 0 when idle, got %#x", hw.regs[signalEnable])
	}

	// Quiesce/Resume must stop IRQ waits before masking the GIC so a delayed
	// CMD13 inside Quiesce completes via polling without stalling on the masked SPI.
	hw.mu.Lock()
	hw.delayCMD13Poll = 5
	hw.mu.Unlock()
	quiesceStart := time.Now()
	if err := dev.Quiesce(); err != nil {
		t.Fatalf("Quiesce with IRQ and delayed CMD13: %v", err)
	}
	if time.Since(quiesceStart) > 200*time.Millisecond {
		t.Fatalf("Quiesce stalled on masked IRQ (%v)", time.Since(quiesceStart))
	}
	if err := dev.Resume(); err != nil {
		t.Fatalf("Resume with IRQ: %v", err)
	}
	before := dev.IRQCount()
	if err := dev.ReadSectors(4, got); err != nil {
		t.Fatalf("post-resume IRQ ReadSectors: %v", err)
	}
	if dev.IRQCount() <= before {
		t.Fatalf("expected IRQ count to advance after resume: before=%d after=%d", before, dev.IRQCount())
	}

	dev.DisableIRQ()
	if err := dev.Close(); err != nil {
		t.Fatal(err)
	}
}
