// SPDX-License-Identifier: GPL-3.0-or-later

package gic

import (
	"math"
	"math/bits"
)

// IRQPhysTimer is the architectural ARMv8 EL1 non-secure physical timer PPI (PPI 14).
const IRQPhysTimer uint32 = 30

// IdleAccess abstracts the CPU timer, DAIF and WFI primitives used by IdleStep
// so the idle governor can be unit-tested on the host.
type IdleAccess interface {
	CounterFreq() uint64
	NowNanos() int64
	IRQEnabled() bool
	MaskIRQ() bool
	UnmaskIRQ()
	ArmTimer(ticks uint32)
	DisarmTimer()
	WFI()
}

// SetIdleAccess binds an IdleAccess backend for IdleStep.
func (g *Controller) SetIdleAccess(hw IdleAccess) { g.idleHW = hw }

// IdleWakeups returns the number of WFI wakeups completed by IdleStep.
func (g *Controller) IdleWakeups() uint64 { return g.idleWakeups.Load() }

// IdleStep implements runtime/goos.Idle using ARMv8 WFI and the EL1 physical
// timer (PPI 30). It performs no heap allocations or locks.
func (g *Controller) IdleStep(until int64) {
	hw := g.idleHW
	if hw == nil {
		return
	}
	if until == 0 || until == math.MaxInt64 {
		// No timer deadline (runtime beforeIdle passes 0; semasleep(-1) passes
		// math.MaxInt64). Sleep indefinitely only when IRQs are unmasked so an
		// interrupt can wake and dispatch; otherwise return to avoid wedging
		// polling code.
		if !hw.IRQEnabled() {
			return
		}
		hw.WFI()
		g.idleWakeups.Add(1)
		return
	}

	delta := until - hw.NowNanos()
	if delta <= 0 {
		return
	}
	ticks := deltaToTicks(delta, hw.CounterFreq())
	if ticks == 0 {
		return
	}

	// Mask DAIF.I across arm + WFI + disarm: ARMv8 WFI still wakes on any
	// enabled pending IRQ (including PPI 30) even while PSTATE.I=1. Disarming
	// before restoring DAIF.I avoids an unnecessary IRQ exception round-trip
	// for timer wakeups, while any other pending IRQ fires immediately on
	// UnmaskIRQ.
	wasEnabled := hw.MaskIRQ()
	hw.ArmTimer(ticks)
	hw.WFI()
	hw.DisarmTimer()
	g.idleWakeups.Add(1)
	if wasEnabled {
		hw.UnmaskIRQ()
	}
}

// deltaToTicks converts a positive nanosecond duration into a signed 32-bit
// CNTP_TVAL_EL0 countdown value, rounding up and clamping to [1, math.MaxInt32].
func deltaToTicks(deltaNS int64, freqHz uint64) uint32 {
	if deltaNS <= 0 || freqHz == 0 {
		return 0
	}
	const nsPerSec = 1_000_000_000
	d := uint64(deltaNS)
	secs := d / nsPerSec
	rem := d % nsPerSec

	if secs > math.MaxInt32/freqHz {
		return math.MaxInt32
	}
	whole := secs * freqHz

	hi, lo := bits.Mul64(rem, freqHz)
	lo2, carry := bits.Add64(lo, nsPerSec-1, 0)
	hi += carry
	if hi >= nsPerSec {
		return math.MaxInt32
	}
	frac, _ := bits.Div64(hi, lo2, nsPerSec)

	if frac >= math.MaxInt32 || whole > uint64(math.MaxInt32)-frac {
		return math.MaxInt32
	}
	ticks := whole + frac
	if ticks == 0 {
		return 1
	}
	return uint32(ticks)
}
