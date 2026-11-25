// SPDX-License-Identifier: GPL-3.0-or-later

// Package gic implements a small, direct ARM GICv3 interrupt controller for
// single-core bare-metal EL1 programs on QEMU virt and DE25-Nano (Agilex 5).
//
// It uses Non-Secure Group 1 interrupts exclusively (ICC_*1_EL1 and GICD_CTLR
// ARE_NS|EnableGrp1NS), which works identically on Agilex 5 under TF-A BL31
// (DS=0, Non-Secure EL1) and QEMU virt with gic-version=3 (DS=1).
package gic

import (
	"errors"
	"sync/atomic"
)

const (
	// MaxINTID is the fixed handler table size. It covers SGIs (0..15),
	// PPIs (16..31) and SPIs up to 639 (Agilex 5 implements 544 SPIs up to 575).
	MaxINTID   = 640
	FirstPPI   = 16
	FirstSPI   = 32
	SpuriousID = 1020

	// DefaultPriority is assigned to all lines at Init. Priorities numerically
	// below DefaultPriorityMask are delivered by the CPU interface.
	DefaultPriority     uint8 = 0xa0
	DefaultPriorityMask uint8 = 0xf0

	// DefaultUnhandledStormLimit is the consecutive unhandled-interrupt
	// threshold after which Dispatch masks a PPI/SPI line.
	DefaultUnhandledStormLimit uint32 = 16
)

// GICv3 Distributor register offsets from GICD base (ARM IHI 0069G).
const (
	GICD_CTLR       = 0x0000
	GICD_TYPER      = 0x0004
	GICD_IGROUPR    = 0x0080
	GICD_ISENABLER  = 0x0100
	GICD_ICENABLER  = 0x0180
	GICD_ISPENDR    = 0x0200
	GICD_ICPENDR    = 0x0280
	GICD_ICACTIVER  = 0x0380
	GICD_IPRIORITYR = 0x0400
	GICD_ICFGR      = 0x0c00
	GICD_IROUTER    = 0x6000
)

// GICv3 Redistributor register offsets from a core's GICR frame (ARM IHI 0069G).
const (
	GICR_CTLR        = 0x0000
	GICR_TYPER       = 0x0008
	GICR_WAKER       = 0x0014
	GICR_SGI_BASE    = 0x10000
	GICR_IGROUPR0    = GICR_SGI_BASE + 0x0080
	GICR_ISENABLER0  = GICR_SGI_BASE + 0x0100
	GICR_ICENABLER0  = GICR_SGI_BASE + 0x0180
	GICR_ISPENDR0    = GICR_SGI_BASE + 0x0200
	GICR_ICPENDR0    = GICR_SGI_BASE + 0x0280
	GICR_ICACTIVER0  = GICR_SGI_BASE + 0x0380
	GICR_IPRIORITYR0 = GICR_SGI_BASE + 0x0400
	GICR_ICFGR1      = GICR_SGI_BASE + 0x0c04
)

const (
	ctlrEnableGrp1NS = uint32(1 << 1)
	ctlrARENS        = uint32(1 << 4)
	ctlrRWP          = uint32(1 << 31)

	gicrCTLRRWP     = uint32(1 << 3)
	gicrTyperVLPIS  = uint64(1 << 1)
	gicrTyperLast   = uint64(1 << 4)
	gicrWakerSleep  = uint32(1 << 1)
	gicrWakerAsleep = uint32(1 << 2)
	gicrStrideV3    = uint64(0x20000)
	gicrStrideV4    = uint64(0x40000)
	maxRedistWalk   = 64
	maxWaitSpins    = 1_000_000
	mpidrAffMask    = uint64(0xff00ffffff)
	defaultPrioWord = uint32(DefaultPriority) * 0x01010101
)

var (
	ErrInvalidConfig     = errors.New("gic: invalid GICD/GICR configuration")
	ErrUnsupported       = errors.New("gic: GICv3 system registers not implemented")
	ErrNoRedistributor   = errors.New("gic: no redistributor matched current CPU affinity")
	ErrRedistributorWake = errors.New("gic: redistributor did not wake")
	ErrRegisterWriteHung = errors.New("gic: RWP did not clear")
)

// Access abstracts raw MMIO and EL1 system-register operations so the
// controller logic can be unit-tested on the host.
type Access interface {
	HasGICv3() bool
	MPIDR() uint64
	Read32(addr uint64) uint32
	Write32(addr uint64, val uint32)
	Read64(addr uint64) uint64
	Write64(addr uint64, val uint64)
	InitCPU(pmr uint8)
	Ack() uint32
	End(id uint32)
	SendSGI(val uint64)
}

// Stats reports cumulative GICv3 interrupt dispatch telemetry.
type Stats struct {
	Dispatched  uint64
	Handled     uint64
	Unhandled   uint64
	Spurious    uint64
	StormMasked uint64
	IdleWakeups uint64
}

// Controller drives one GICv3 instance for the booting PE.
type Controller struct {
	GICD uint64
	GICR uint64

	hw    Access
	gicr  uint64
	mpidr uint64
	lines uint32

	idleHW      IdleAccess
	idleWakeups atomic.Uint64

	handlers        [MaxINTID]atomic.Pointer[handlerSlot]
	counts          [MaxINTID]atomic.Uint64
	unhandledStreak [MaxINTID]atomic.Uint32
	dispatched      atomic.Uint64
	handled         atomic.Uint64
	unhandled       atomic.Uint64
	spurious        atomic.Uint64
	stormMasked     atomic.Uint64
	stormConfig     atomic.Uint64
}

type handlerSlot struct {
	fn func()
}

const (
	stormConfigUnset    = uint64(0)
	stormConfigDisabled = uint64(1)
)

// NewWithAccess constructs a Controller backed by an explicit Access provider.
func NewWithAccess(gicd, gicr uint64, hw Access) *Controller {
	return &Controller{GICD: gicd, GICR: gicr, hw: hw}
}

// Init finds this CPU's redistributor, wakes it, disables and clears all lines,
// assigns Group 1 and default priorities, and enables the distributor and CPU
// interface for Non-Secure Group 1 interrupts. CPU IRQ unmasking stays separate.
func (g *Controller) Init() error {
	if g.GICD == 0 || g.GICR == 0 || g.hw == nil {
		return ErrInvalidConfig
	}
	if !g.hw.HasGICv3() {
		return ErrUnsupported
	}

	g.mpidr = g.hw.MPIDR()
	rd, err := g.findRedistributor(g.mpidr)
	if err != nil {
		return err
	}
	g.gicr = rd

	// Wake the redistributor for this PE.
	waker := g.hw.Read32(g.gicr + GICR_WAKER)
	g.hw.Write32(g.gicr+GICR_WAKER, waker&^gicrWakerSleep)
	if !g.waitClear(g.gicr+GICR_WAKER, gicrWakerAsleep) {
		return ErrRedistributorWake
	}

	// Reset local SGI/PPI state (INTIDs 0..31) in the redistributor.
	g.hw.Write32(g.gicr+GICR_ICENABLER0, 0xffffffff)
	g.hw.Write32(g.gicr+GICR_ICPENDR0, 0xffffffff)
	g.hw.Write32(g.gicr+GICR_ICACTIVER0, 0xffffffff)
	if !g.waitClear(g.gicr+GICR_CTLR, gicrCTLRRWP) {
		return ErrRegisterWriteHung
	}
	g.hw.Write32(g.gicr+GICR_IGROUPR0, 0xffffffff)
	for off := uint64(0); off < FirstSPI; off += 4 {
		g.hw.Write32(g.gicr+GICR_IPRIORITYR0+off, defaultPrioWord)
	}

	// Determine implemented interrupt lines from GICD_TYPER.ITLinesNumber.
	itLines := (g.hw.Read32(g.GICD+GICD_TYPER) & 0x1f) + 1
	hwLines := itLines * 32
	if hwLines > SpuriousID {
		hwLines = SpuriousID
	}
	g.lines = hwLines
	if g.lines > MaxINTID {
		g.lines = MaxINTID
	}

	// Keep affinity routing enabled while quiescing SPIs, then enable Group 1.
	g.hw.Write32(g.GICD+GICD_CTLR, ctlrARENS)
	if !g.waitClear(g.GICD+GICD_CTLR, ctlrRWP) {
		return ErrRegisterWriteHung
	}
	for bank := uint32(1); bank < hwLines/32; bank++ {
		off := uint64(bank) * 4
		g.hw.Write32(g.GICD+GICD_ICENABLER+off, 0xffffffff)
		g.hw.Write32(g.GICD+GICD_ICPENDR+off, 0xffffffff)
		g.hw.Write32(g.GICD+GICD_ICACTIVER+off, 0xffffffff)
		g.hw.Write32(g.GICD+GICD_IGROUPR+off, 0xffffffff)
	}
	for id := uint32(FirstSPI); id < hwLines; id += 4 {
		g.hw.Write32(g.GICD+GICD_IPRIORITYR+uint64(id), defaultPrioWord)
	}
	for id := uint32(FirstSPI); id < hwLines; id += 16 {
		g.hw.Write32(g.GICD+GICD_ICFGR+uint64(id/16)*4, 0)
	}
	if !g.waitClear(g.GICD+GICD_CTLR, ctlrRWP) {
		return ErrRegisterWriteHung
	}
	g.hw.Write32(g.GICD+GICD_CTLR, ctlrARENS|ctlrEnableGrp1NS)
	if !g.waitClear(g.GICD+GICD_CTLR, ctlrRWP) {
		return ErrRegisterWriteHung
	}

	g.hw.InitCPU(DefaultPriorityMask)
	g.ResetStats()
	return nil
}

// Redistributor returns the matched redistributor base address for this CPU.
func (g *Controller) Redistributor() uint64 { return g.gicr }

// Lines returns the number of usable INTIDs tracked by this controller.
func (g *Controller) Lines() uint32 { return g.lines }

// Enable routes an SPI to this CPU's affinity and unmasks the interrupt line.
func (g *Controller) Enable(id uint32) {
	if id >= g.lines || g.gicr == 0 {
		return
	}
	if id < MaxINTID {
		g.unhandledStreak[id].Store(0)
	}
	if id < FirstSPI {
		g.hw.Write32(g.gicr+GICR_ISENABLER0, 1<<id)
		return
	}
	g.hw.Write64(g.GICD+GICD_IROUTER+uint64(id)*8, g.mpidr&mpidrAffMask)
	g.hw.Write32(g.GICD+GICD_ISENABLER+uint64(id/32)*4, 1<<(id%32))
}

// Disable masks an interrupt line in the redistributor or distributor.
func (g *Controller) Disable(id uint32) {
	if id >= g.lines || g.gicr == 0 {
		return
	}
	if id < FirstSPI {
		g.hw.Write32(g.gicr+GICR_ICENABLER0, 1<<id)
		return
	}
	g.hw.Write32(g.GICD+GICD_ICENABLER+uint64(id/32)*4, 1<<(id%32))
}

// SetPriority sets an interrupt's 8-bit priority (lower value = higher priority).
func (g *Controller) SetPriority(id uint32, prio uint8) {
	if id >= g.lines || g.gicr == 0 {
		return
	}
	base := g.GICD + GICD_IPRIORITYR
	if id < FirstSPI {
		base = g.gicr + GICR_IPRIORITYR0
	}
	addr := base + uint64(id&^3)
	shift := (id & 3) * 8
	v := g.hw.Read32(addr)
	v = (v &^ (0xff << shift)) | (uint32(prio) << shift)
	g.hw.Write32(addr, v)
}

// SetEdge configures a PPI or SPI as edge-triggered (true) or level-sensitive (false).
func (g *Controller) SetEdge(id uint32, edge bool) {
	if id < FirstPPI || id >= g.lines || g.gicr == 0 {
		return
	}
	var addr uint64
	var shift uint32
	if id < FirstSPI {
		addr = g.gicr + GICR_ICFGR1
		shift = (id-FirstPPI)*2 + 1
	} else {
		addr = g.GICD + GICD_ICFGR + uint64(id/16)*4
		shift = (id%16)*2 + 1
	}
	v := g.hw.Read32(addr)
	if edge {
		v |= 1 << shift
	} else {
		v &^= 1 << shift
	}
	g.hw.Write32(addr, v)
}

// SendSGI raises Software Generated Interrupt id (0..15) targeting this CPU.
func (g *Controller) SendSGI(id uint32) {
	if id >= FirstPPI || g.gicr == 0 {
		return
	}
	g.hw.SendSGI(encodeSelfSGI(g.mpidr, id))
}

// SetPending sets a PPI or SPI pending in software (GICR_ISPENDR0 / GICD_ISPENDR).
func (g *Controller) SetPending(id uint32) {
	if id < FirstPPI || id >= g.lines || g.gicr == 0 {
		return
	}
	if id < FirstSPI {
		g.hw.Write32(g.gicr+GICR_ISPENDR0, 1<<id)
		return
	}
	g.hw.Write32(g.GICD+GICD_ISPENDR+uint64(id/32)*4, 1<<(id%32))
}

// ClearPending clears a pending PPI or SPI (GICR_ICPENDR0 / GICD_ICPENDR).
func (g *Controller) ClearPending(id uint32) {
	if id < FirstPPI || id >= g.lines || g.gicr == 0 {
		return
	}
	if id < FirstSPI {
		g.hw.Write32(g.gicr+GICR_ICPENDR0, 1<<id)
		return
	}
	g.hw.Write32(g.GICD+GICD_ICPENDR+uint64(id/32)*4, 1<<(id%32))
}

// Ack reads ICC_IAR1_EL1 and returns the acknowledged INTID (>= 1020 if spurious).
func (g *Controller) Ack() uint32 {
	if g.hw == nil {
		return SpuriousID
	}
	return g.hw.Ack() & 0xffffff
}

// End writes ICC_EOIR1_EL1 to complete and deactivate an acknowledged INTID.
func (g *Controller) End(id uint32) {
	if g.hw == nil || id >= SpuriousID {
		return
	}
	g.hw.End(id)
}

// Register binds a handler for INTID in the static dispatch table. Pass nil to clear.
func (g *Controller) Register(id uint32, fn func()) {
	if id >= MaxINTID {
		return
	}
	var slot *handlerSlot
	if fn != nil {
		g.unhandledStreak[id].Store(0)
		slot = &handlerSlot{fn: fn}
	}
	g.handlers[id].Store(slot)
}

// SetUnhandledStormLimit sets how many consecutive unhandled deliveries of an
// INTID trigger automatic masking in Dispatch (for PPI/SPI lines). Passing 0
// disables automatic masking. The default when unset is DefaultUnhandledStormLimit.
func (g *Controller) SetUnhandledStormLimit(limit uint32) {
	if limit == 0 {
		g.stormConfig.Store(stormConfigDisabled)
		return
	}
	// Zero means unset/default, one means explicitly disabled, and configured
	// positive limits are stored as limit+1 so the full uint32 range is usable.
	g.stormConfig.Store(uint64(limit) + 1)
}

func (g *Controller) effectiveStormLimit() uint32 {
	state := g.stormConfig.Load()
	switch state {
	case stormConfigUnset:
		return DefaultUnhandledStormLimit
	case stormConfigDisabled:
		return 0
	}
	return uint32(state - 1)
}

// Stats returns a lock-free snapshot of interrupt dispatch telemetry.
func (g *Controller) Stats() Stats {
	return Stats{
		Dispatched:  g.dispatched.Load(),
		Handled:     g.handled.Load(),
		Unhandled:   g.unhandled.Load(),
		Spurious:    g.spurious.Load(),
		StormMasked: g.stormMasked.Load(),
		IdleWakeups: g.idleWakeups.Load(),
	}
}

// Count returns the number of acknowledged interrupts for INTID id.
func (g *Controller) Count(id uint32) uint64 {
	if id >= MaxINTID {
		return 0
	}
	return g.counts[id].Load()
}

// ResetStats zeroes all dispatch telemetry counters and unhandled streak state.
func (g *Controller) ResetStats() {
	g.dispatched.Store(0)
	g.handled.Store(0)
	g.unhandled.Store(0)
	g.spurious.Store(0)
	g.stormMasked.Store(0)
	g.idleWakeups.Store(0)
	for i := range g.counts {
		g.counts[i].Store(0)
		g.unhandledStreak[i].Store(0)
	}
}

// Dispatch drains all currently pending Group 1 interrupts, invoking each
// registered handler before writing EOI. Unhandled PPI/SPI lines exceeding the
// storm limit are masked before EOI to prevent level-triggered lockups. Returns
// the number of non-spurious interrupts acknowledged.
func (g *Controller) Dispatch() int {
	handled := 0
	for {
		id := g.Ack()
		if id >= SpuriousID {
			if handled == 0 {
				g.spurious.Add(1)
			}
			return handled
		}
		g.dispatched.Add(1)
		var fn func()
		if id < MaxINTID {
			g.counts[id].Add(1)
			if slot := g.handlers[id].Load(); slot != nil {
				fn = slot.fn
			}
		}
		if fn != nil {
			g.unhandledStreak[id].Store(0)
			fn()
			g.handled.Add(1)
			g.End(id)
		} else {
			g.unhandled.Add(1)
			limit := g.effectiveStormLimit()
			if id < MaxINTID && limit > 0 && g.unhandledStreak[id].Add(1) >= limit {
				g.unhandledStreak[id].Store(0)
				if id >= FirstPPI && id < g.lines {
					g.Disable(id)
					g.stormMasked.Add(1)
					g.End(id)
					handled++
					continue
				}
				g.End(id)
				handled++
				return handled
			}
			g.End(id)
		}
		handled++
	}
}

func (g *Controller) findRedistributor(mpidr uint64) (uint64, error) {
	want := mpidrToTyperAffinity(mpidr)
	addr := g.GICR
	for i := 0; i < maxRedistWalk; i++ {
		typer := g.hw.Read64(addr + GICR_TYPER)
		if uint32(typer>>32) == want {
			return addr, nil
		}
		if typer&gicrTyperLast != 0 {
			return 0, ErrNoRedistributor
		}
		if typer&gicrTyperVLPIS != 0 {
			addr += gicrStrideV4
		} else {
			addr += gicrStrideV3
		}
	}
	return 0, ErrNoRedistributor
}

func (g *Controller) waitClear(addr uint64, mask uint32) bool {
	for i := 0; i < maxWaitSpins; i++ {
		if g.hw.Read32(addr)&mask == 0 {
			return true
		}
	}
	return false
}

func mpidrToTyperAffinity(mpidr uint64) uint32 {
	return uint32((mpidr & 0x00ffffff) | ((mpidr >> 8) & 0xff000000))
}

func encodeSelfSGI(mpidr uint64, id uint32) uint64 {
	aff0 := mpidr & 0xff
	aff1 := (mpidr >> 8) & 0xff
	aff2 := (mpidr >> 16) & 0xff
	aff3 := (mpidr >> 32) & 0xff
	rs := (aff0 >> 4) & 0xf
	targetList := uint64(1) << (aff0 & 0xf)
	return (aff3 << 48) | (rs << 44) | (aff2 << 32) | (uint64(id&0xf) << 24) | (aff1 << 16) | targetList
}
