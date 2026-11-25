// SPDX-License-Identifier: GPL-3.0-or-later

package serial

import "sync/atomic"

// LineError identifies UART receive line-status errors observed when reading a
// character or status register.
type LineError uint8

const (
	ErrOverrun LineError = 1 << iota
	ErrParity
	ErrFraming
	ErrBreak
)

// Corrupt reports whether the receive line status indicates data corruption
// (parity, framing, or break) rather than an overrun warning alone.
func (e LineError) Corrupt() bool {
	return e&(ErrParity|ErrFraming|ErrBreak) != 0
}

// DecodeDWAPBLSR extracts LineError flags from a DesignWare APB / 16550 UART
// Line Status Register (LSR bits [4:1]: OE, PE, FE, BI).
func DecodeDWAPBLSR(lsr uint32) LineError {
	return LineError((lsr & 0x1e) >> 1)
}

// DecodePL011DR extracts LineError flags from an ARM PL011 UARTDR read value
// (FE bit 8, PE bit 9, BE bit 10, OE bit 11).
func DecodePL011DR(dr uint32) LineError {
	var errs LineError
	if dr&(1<<11) != 0 {
		errs |= ErrOverrun
	}
	if dr&(1<<9) != 0 {
		errs |= ErrParity
	}
	if dr&(1<<8) != 0 {
		errs |= ErrFraming
	}
	if dr&(1<<10) != 0 {
		errs |= ErrBreak
	}
	return errs
}

// Stats reports cumulative UART interrupt, byte, ring-drop, and line-error
// counters.
type Stats struct {
	IRQCount  uint64
	RXBytes   uint64
	RXDropped uint32
	TXBytes   uint64
	TXDropped uint32
	Overrun   uint64
	Parity    uint64
	Framing   uint64
	Break     uint64
}

// ErrorCounters records atomic per-error counts for UART receive line-status
// faults without allocations or locks.
type ErrorCounters struct {
	overrun atomic.Uint64
	parity  atomic.Uint64
	framing atomic.Uint64
	brk     atomic.Uint64
}

// Record increments the counter for each error bit set in errs.
func (c *ErrorCounters) Record(errs LineError) {
	if errs == 0 {
		return
	}
	if errs&ErrOverrun != 0 {
		c.overrun.Add(1)
	}
	if errs&ErrParity != 0 {
		c.parity.Add(1)
	}
	if errs&ErrFraming != 0 {
		c.framing.Add(1)
	}
	if errs&ErrBreak != 0 {
		c.brk.Add(1)
	}
}

// Snapshot returns cumulative (overrun, parity, framing, break) counts.
func (c *ErrorCounters) Snapshot() (overrun, parity, framing, brk uint64) {
	return c.overrun.Load(), c.parity.Load(), c.framing.Load(), c.brk.Load()
}

// Reset zeroes all error counters.
func (c *ErrorCounters) Reset() {
	c.overrun.Store(0)
	c.parity.Store(0)
	c.framing.Store(0)
	c.brk.Store(0)
}
