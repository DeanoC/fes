// SPDX-License-Identifier: GPL-3.0-or-later

package gic

import (
	"errors"
	"testing"
)

type fakeAccess struct {
	hasGICv3 bool
	mpidr    uint64

	r32 map[uint64]uint32
	r64 map[uint64]uint64

	wakerPollsBeforeWake int
	wakerPollCount       int

	ctlrHistory []uint32
	cpuPMR      uint8
	cpuInited   bool

	ackQueue []uint32
	ended    []uint32
	sgis     []uint64
	events   []string
}

func newFakeAccess(gicd, gicr uint64, mpidr uint64, itLines uint32, redistAffs []uint32) *fakeAccess {
	f := &fakeAccess{
		hasGICv3: true,
		mpidr:    mpidr,
		r32:      make(map[uint64]uint32),
		r64:      make(map[uint64]uint64),
	}
	f.r32[gicd+GICD_TYPER] = itLines
	for i, aff := range redistAffs {
		base := gicr + uint64(i)*gicrStrideV3
		typer := uint64(aff) << 32
		if i == len(redistAffs)-1 {
			typer |= gicrTyperLast
		}
		f.r64[base+GICR_TYPER] = typer
		f.r32[base+GICR_WAKER] = gicrWakerSleep | gicrWakerAsleep
	}
	return f
}

func (f *fakeAccess) HasGICv3() bool { return f.hasGICv3 }
func (f *fakeAccess) MPIDR() uint64  { return f.mpidr }

func (f *fakeAccess) Read32(addr uint64) uint32 {
	v := f.r32[addr]
	if addr&0xffff == GICR_WAKER && v&gicrWakerSleep == 0 && v&gicrWakerAsleep != 0 {
		f.wakerPollCount++
		if f.wakerPollCount > f.wakerPollsBeforeWake {
			v &^= gicrWakerAsleep
			f.r32[addr] = v
		}
	}
	return v
}

func (f *fakeAccess) Write32(addr uint64, val uint32) {
	f.r32[addr] = val
	if addr&0xffff == GICD_CTLR && addr < 0x1d060000 && addr != 0x080a0000 {
		f.ctlrHistory = append(f.ctlrHistory, val)
	}
}

func (f *fakeAccess) Read64(addr uint64) uint64       { return f.r64[addr] }
func (f *fakeAccess) Write64(addr uint64, val uint64) { f.r64[addr] = val }
func (f *fakeAccess) InitCPU(pmr uint8)               { f.cpuInited = true; f.cpuPMR = pmr }
func (f *fakeAccess) SendSGI(val uint64)              { f.sgis = append(f.sgis, val) }

func (f *fakeAccess) Ack() uint32 {
	if len(f.ackQueue) == 0 {
		return 1023
	}
	id := f.ackQueue[0]
	f.ackQueue = f.ackQueue[1:]
	return id
}

func (f *fakeAccess) End(id uint32) {
	f.ended = append(f.ended, id)
	f.events = append(f.events, "end")
}

func TestInitAgilex5Core2A76(t *testing.T) {
	const (
		gicd = 0x1d000000
		gicr = 0x1d060000
		// Agilex 5 A76 primary core: RES1 (bit 31) | MT (bit 24) | Aff1=2, Aff0=0.
		mpidrA76 = 0x81000200
	)
	hw := newFakeAccess(gicd, gicr, mpidrA76, 17, []uint32{0x000, 0x100, 0x200, 0x300})
	hw.wakerPollsBeforeWake = 2

	g := NewWithAccess(gicd, gicr, hw)
	if err := g.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	wantRD := uint64(0x1d0a0000)
	if g.Redistributor() != wantRD {
		t.Fatalf("Redistributor = %#x, want %#x", g.Redistributor(), wantRD)
	}
	if g.Lines() != 576 {
		t.Fatalf("Lines = %d, want 576", g.Lines())
	}
	if !hw.cpuInited || hw.cpuPMR != DefaultPriorityMask {
		t.Fatalf("CPU interface not initialized properly: inited=%v pmr=%#x", hw.cpuInited, hw.cpuPMR)
	}
	if hw.r32[wantRD+GICR_WAKER] != 0 {
		t.Fatalf("GICR_WAKER = %#x, want 0", hw.r32[wantRD+GICR_WAKER])
	}
	if hw.r32[wantRD+GICR_IGROUPR0] != 0xffffffff {
		t.Fatalf("GICR_IGROUPR0 = %#x, want all Group 1", hw.r32[wantRD+GICR_IGROUPR0])
	}
	if hw.r32[wantRD+GICR_ICENABLER0] != 0xffffffff || hw.r32[wantRD+GICR_ICPENDR0] != 0xffffffff || hw.r32[wantRD+GICR_ICACTIVER0] != 0xffffffff {
		t.Fatal("redistributor SGI/PPI lines not disabled and cleared")
	}
	if hw.r32[wantRD+GICR_IPRIORITYR0+28] != defaultPrioWord {
		t.Fatalf("PPI priority word = %#x, want %#x", hw.r32[wantRD+GICR_IPRIORITYR0+28], defaultPrioWord)
	}
	if len(hw.ctlrHistory) != 2 || hw.ctlrHistory[0] != ctlrARENS || hw.ctlrHistory[1] != (ctlrARENS|ctlrEnableGrp1NS) {
		t.Fatalf("unexpected GICD_CTLR sequence: %#x", hw.ctlrHistory)
	}
	if hw.r32[gicd+GICD_IGROUPR+4] != 0xffffffff || hw.r32[gicd+GICD_ICENABLER+4] != 0xffffffff {
		t.Fatal("SPI bank 1 not initialized to disabled Group 1")
	}
	if hw.r32[gicd+GICD_IPRIORITYR+32] != defaultPrioWord {
		t.Fatalf("SPI 32..35 priority = %#x, want %#x", hw.r32[gicd+GICD_IPRIORITYR+32], defaultPrioWord)
	}
	if hw.r32[gicd+GICD_ICFGR+8] != 0 {
		t.Fatalf("SPI ICFGR = %#x, want 0 (level)", hw.r32[gicd+GICD_ICFGR+8])
	}
}

func TestEnableDisableRoutePriorityAndTrigger(t *testing.T) {
	const (
		gicd  = 0x08000000
		gicr  = 0x080a0000
		mpidr = 0x80000000 // RES1 bit 31 set, Aff = 0
	)
	hw := newFakeAccess(gicd, gicr, mpidr, 8, []uint32{0})
	g := NewWithAccess(gicd, gicr, hw)
	if err := g.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// SGI 0 and PPI 30 touch GICR_ISENABLER0 / GICR_ICENABLER0.
	g.Enable(0)
	if hw.r32[gicr+GICR_ISENABLER0] != 1 {
		t.Fatalf("SGI 0 enable = %#x, want 1", hw.r32[gicr+GICR_ISENABLER0])
	}
	g.Enable(30)
	if hw.r32[gicr+GICR_ISENABLER0] != 1<<30 {
		t.Fatalf("PPI 30 enable = %#x, want %#x", hw.r32[gicr+GICR_ISENABLER0], uint32(1<<30))
	}
	g.Disable(30)
	if hw.r32[gicr+GICR_ICENABLER0] != 1<<30 {
		t.Fatalf("PPI 30 disable = %#x, want %#x", hw.r32[gicr+GICR_ICENABLER0], uint32(1<<30))
	}

	// SPI 141 (bank 4, bit 13) routes to masked affinity (IRM bit 31 MUST be 0).
	g.Enable(141)
	if got := hw.r64[gicd+GICD_IROUTER+141*8]; got != 0 {
		t.Fatalf("GICD_IROUTER[141] = %#x, want 0 (IRM clear)", got)
	}
	if got := hw.r32[gicd+GICD_ISENABLER+4*4]; got != 1<<13 {
		t.Fatalf("GICD_ISENABLER[4] = %#x, want %#x", got, uint32(1<<13))
	}
	g.Disable(141)
	if got := hw.r32[gicd+GICD_ICENABLER+4*4]; got != 1<<13 {
		t.Fatalf("GICD_ICENABLER[4] = %#x, want %#x", got, uint32(1<<13))
	}

	// Priority updates only the targeted byte in the 32-bit word.
	g.SetPriority(30, 0x40)
	if got := hw.r32[gicr+GICR_IPRIORITYR0+28]; got != 0xa040a0a0 {
		t.Fatalf("PPI 30 priority word = %#x, want 0xa040a0a0", got)
	}
	g.SetPriority(141, 0x80)
	if got := hw.r32[gicd+GICD_IPRIORITYR+140]; got != 0xa0a080a0 {
		t.Fatalf("SPI 141 priority word = %#x, want 0xa0a080a0", got)
	}

	// Edge/level trigger configuration for PPI and SPI (SGIs ignored).
	g.SetEdge(0, true)
	g.SetEdge(30, true)
	if got := hw.r32[gicr+GICR_ICFGR1]; got != 1<<((30-16)*2+1) {
		t.Fatalf("GICR_ICFGR1 = %#x", got)
	}
	g.SetEdge(30, false)
	if got := hw.r32[gicr+GICR_ICFGR1]; got != 0 {
		t.Fatalf("GICR_ICFGR1 after level = %#x, want 0", got)
	}
	g.SetEdge(48, true)
	if got := hw.r32[gicd+GICD_ICFGR+3*4]; got != 1<<1 {
		t.Fatalf("GICD_ICFGR[3] = %#x, want 0x2", got)
	}
	g.SetEdge(48, false)
	if got := hw.r32[gicd+GICD_ICFGR+3*4]; got != 0 {
		t.Fatalf("GICD_ICFGR[3] after level = %#x, want 0", got)
	}

	// Pending set/clear for PPI and SPI (SGIs and out-of-range ignored).
	g.SetPending(0)
	g.ClearPending(0)
	g.SetPending(30)
	if got := hw.r32[gicr+GICR_ISPENDR0]; got != 1<<30 {
		t.Fatalf("GICR_ISPENDR0 = %#x, want %#x", got, uint32(1<<30))
	}
	g.ClearPending(30)
	if got := hw.r32[gicr+GICR_ICPENDR0]; got != 1<<30 {
		t.Fatalf("GICR_ICPENDR0 = %#x, want %#x", got, uint32(1<<30))
	}
	g.SetPending(48)
	if got := hw.r32[gicd+GICD_ISPENDR+1*4]; got != 1<<16 {
		t.Fatalf("GICD_ISPENDR[1] = %#x, want %#x", got, uint32(1<<16))
	}
	g.ClearPending(48)
	if got := hw.r32[gicd+GICD_ICPENDR+1*4]; got != 1<<16 {
		t.Fatalf("GICD_ICPENDR[1] = %#x, want %#x", got, uint32(1<<16))
	}

	// Out-of-bounds calls are safe no-ops.
	g.Enable(999)
	g.Disable(999)
	g.SetPending(999)
	g.ClearPending(999)
	g.SetPriority(999, 0x10)
	g.SetEdge(999, true)
	g.SendSGI(16)
	g.Register(999, func() {})
}

func TestSendSGIEncoding(t *testing.T) {
	hw := newFakeAccess(0x1d000000, 0x1d060000, 0x81000200, 17, []uint32{0x000, 0x100, 0x200})
	g := NewWithAccess(0x1d000000, 0x1d060000, hw)
	if err := g.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	g.SendSGI(5)
	if len(hw.sgis) != 1 {
		t.Fatalf("expected 1 SGI write, got %d", len(hw.sgis))
	}
	// Aff3=0, RS=0, IRM=0, Aff2=0, INTID=5 (bits 27:24), Aff1=2 (bits 23:16), TargetList=1<<0.
	want := uint64(5<<24 | 2<<16 | 1)
	if hw.sgis[0] != want {
		t.Fatalf("ICC_SGI1R_EL1 = %#x, want %#x", hw.sgis[0], want)
	}
}

func TestDispatchAndOrder(t *testing.T) {
	hw := newFakeAccess(0x08000000, 0x080a0000, 0, 8, []uint32{0})
	g := NewWithAccess(0x08000000, 0x080a0000, hw)
	if err := g.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	g.Register(0, func() { hw.events = append(hw.events, "sgi0") })
	g.Register(30, func() { hw.events = append(hw.events, "timer") })
	hw.ackQueue = []uint32{0, 30, 50, 1023}

	if n := g.Dispatch(); n != 3 {
		t.Fatalf("Dispatch handled %d, want 3", n)
	}
	wantEvents := []string{"sgi0", "end", "timer", "end", "end"}
	if len(hw.events) != len(wantEvents) {
		t.Fatalf("events = %v, want %v", hw.events, wantEvents)
	}
	for i := range wantEvents {
		if hw.events[i] != wantEvents[i] {
			t.Fatalf("event[%d] = %q, want %q", i, hw.events[i], wantEvents[i])
		}
	}
	if len(hw.ended) != 3 || hw.ended[0] != 0 || hw.ended[1] != 30 || hw.ended[2] != 50 {
		t.Fatalf("ended = %v, want [0 30 50]", hw.ended)
	}

	// Spurious EOI is ignored; nil Access returns spurious.
	g.End(SpuriousID)
	if len(hw.ended) != 3 {
		t.Fatal("End wrote spurious INTID")
	}
	var zero Controller
	if zero.Ack() != SpuriousID {
		t.Fatal("nil Access Ack should return SpuriousID")
	}
	zero.End(0)
}

func TestInitErrorsAndClamping(t *testing.T) {
	if err := (&Controller{}).Init(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("empty controller Init = %v, want ErrInvalidConfig", err)
	}

	hw := newFakeAccess(0x08000000, 0x080a0000, 0, 31, []uint32{0})
	hw.hasGICv3 = false
	if err := NewWithAccess(0x08000000, 0x080a0000, hw).Init(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("no-GICv3 Init = %v, want ErrUnsupported", err)
	}

	// Missing matching affinity stops at Last bit.
	hw = newFakeAccess(0x08000000, 0x080a0000, 0x400, 8, []uint32{0x000, 0x100})
	if err := NewWithAccess(0x08000000, 0x080a0000, hw).Init(); !errors.Is(err, ErrNoRedistributor) {
		t.Fatalf("missing redistributor = %v, want ErrNoRedistributor", err)
	}

	// Stuck redistributor waker times out cleanly.
	hw = newFakeAccess(0x08000000, 0x080a0000, 0, 8, []uint32{0})
	hw.wakerPollsBeforeWake = maxWaitSpins + 1
	if err := NewWithAccess(0x08000000, 0x080a0000, hw).Init(); !errors.Is(err, ErrRedistributorWake) {
		t.Fatalf("stuck waker = %v, want ErrRedistributorWake", err)
	}

	// Stuck RWP bit times out cleanly.
	hw = newFakeAccess(0x08000000, 0x080a0000, 0, 8, []uint32{0})
	hw.r32[0x080a0000+GICR_CTLR] = gicrCTLRRWP
	if err := NewWithAccess(0x08000000, 0x080a0000, hw).Init(); !errors.Is(err, ErrRegisterWriteHung) {
		t.Fatalf("stuck RWP = %v, want ErrRegisterWriteHung", err)
	}

	// GICv4 stride (VLPIS bit set) and ITLinesNumber clamp to MaxINTID.
	hw = &fakeAccess{
		hasGICv3: true,
		mpidr:    0x100,
		r32:      map[uint64]uint32{0x08000000 + GICD_TYPER: 31},
		r64: map[uint64]uint64{
			0x080a0000 + GICR_TYPER: gicrTyperVLPIS,
			0x080e0000 + GICR_TYPER: (uint64(0x100) << 32) | gicrTyperLast,
		},
	}
	g := NewWithAccess(0x08000000, 0x080a0000, hw)
	if err := g.Init(); err != nil {
		t.Fatalf("GICv4 stride Init: %v", err)
	}
	if g.Redistributor() != 0x080e0000 || g.Lines() != MaxINTID {
		t.Fatalf("got redist=%#x lines=%d, want 0x080e0000 and %d", g.Redistributor(), g.Lines(), MaxINTID)
	}
}
