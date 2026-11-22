// SPDX-License-Identifier: GPL-3.0-or-later

// Package wdt provides an opt-in Synopsys DesignWare APB Watchdog Timer
// (snps,dw-wdt) driver for bare-metal TamaGo boards such as Agilex 5 DE25-Nano.
//
// Opening a watchdog verifies its DesignWare component type register and never
// arms the timer. Callers must invoke Start explicitly; once WDT_EN is set in
// hardware, DesignWare APB watchdogs remain armed until peripheral reset.
package wdt

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// Synopsys DesignWare APB Watchdog register offsets.
const (
	RegCR          uint32 = 0x00 // Control Register
	RegTORR        uint32 = 0x04 // Timeout Range Register
	RegCCVR        uint32 = 0x08 // Current Counter Value Register (RO)
	RegCRR         uint32 = 0x0c // Counter Restart Register (WO, write KickMagic)
	RegSTAT        uint32 = 0x10 // Interrupt Status Register (RO, bit 0)
	RegEOI         uint32 = 0x14 // Interrupt Clear Register (RO, read clears IRQ)
	RegCompVersion uint32 = 0xf8 // Component Version Register (RO)
	RegCompType    uint32 = 0xfc // Component Type Register (RO, CompTypeDW)
)

// DesignWare APB Watchdog register fields and magic values.
const (
	CRWDTEnable uint32 = 1 << 0     // WDT_EN (write-once until peripheral reset)
	CRRespIRQ   uint32 = 1 << 1     // RMOD=1: IRQ on first timeout, reset on second
	CRRPLShift  uint32 = 2          // Reset Pulse Length field shift (bits 4:2)
	CRRPLMask   uint32 = 0x7 << 2   // Reset Pulse Length field mask
	CRRPL32     uint32 = 0x4 << 2   // 32 pclk reset pulse length (Linux dw_wdt default)
	KickMagic   uint32 = 0x76       // Magic write value for WDT_CRR counter reload
	CompTypeDW  uint32 = 0x44570120 // "DW\x01\x20" DesignWare APB WDT identifier
	MaxTop      uint8  = 15         // TOP / TOP_INIT range 0..15 -> 2^(16+TOP) cycles
	statActive  uint32 = 1 << 0
)

// ResponseMode selects the DesignWare WDT action on counter expiry.
type ResponseMode uint8

const (
	// ModeReset asserts system reset immediately when the counter expires (RMOD=0).
	ModeReset ResponseMode = 0
	// ModePreTimeoutIRQ raises an interrupt on the first expiry and asserts
	// system reset on the second expiry if the interrupt is not cleared (RMOD=1).
	ModePreTimeoutIRQ ResponseMode = 1
)

var (
	// ErrNilAccess is returned when Open receives a nil Registers handle.
	ErrNilAccess = errors.New("wdt: nil register access")
	// ErrInvalidConfig is returned when clockHz, timeout, or response mode is invalid.
	ErrInvalidConfig = errors.New("wdt: invalid watchdog configuration")
	// ErrNotFound is returned when WDT_COMP_TYPE does not match CompTypeDW.
	ErrNotFound = errors.New("wdt: DesignWare APB watchdog component type mismatch")
	// ErrNotRunning is returned when Kick is called on an unarmed watchdog.
	ErrNotRunning = errors.New("wdt: watchdog is not enabled")
)

// Registers abstracts 32-bit little-endian MMIO access to a DesignWare WDT block.
type Registers interface {
	Read32(offset uint32) uint32
	Write32(offset uint32, val uint32)
}

// DW drives a Synopsys DesignWare APB Watchdog Timer.
type DW struct {
	mu      sync.Mutex
	io      Registers
	clockHz uint64
	version uint32

	irqMu     sync.Mutex
	irqActive atomic.Bool
	irqCount  atomic.Uint64
	irqGIC    *gic.Controller
	irqID     uint32
	onPreIRQ  func()
}

// Open probes WDT_COMP_TYPE at io without enabling or modifying the watchdog.
func Open(io Registers, clockHz uint64) (*DW, error) {
	if io == nil {
		return nil, ErrNilAccess
	}
	if clockHz == 0 {
		return nil, fmt.Errorf("%w: clockHz must be > 0", ErrInvalidConfig)
	}
	compType := io.Read32(RegCompType)
	if compType != CompTypeDW {
		return nil, fmt.Errorf("%w: got %#08x want %#08x", ErrNotFound, compType, CompTypeDW)
	}
	return &DW{
		io:      io,
		clockHz: clockHz,
		version: io.Read32(RegCompVersion),
	}, nil
}

// Version reports the raw WDT_COMP_VERSION register read during Open.
func (w *DW) Version() uint32 { return w.version }

// ClockHz reports the input clock frequency configured for this watchdog.
func (w *DW) ClockHz() uint64 { return w.clockHz }

// Enabled reports whether WDT_CR.WDT_EN is currently set in hardware.
func (w *DW) Enabled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.io.Read32(RegCR)&CRWDTEnable != 0
}

// DurationForTop returns the hardware single-stage timeout period for selector
// top (0..15 -> 2^(16+top) cycles) at clockHz.
func DurationForTop(clockHz uint64, top uint8) (time.Duration, error) {
	if clockHz == 0 || top > MaxTop {
		return 0, ErrInvalidConfig
	}
	cycles := uint64(1) << (16 + uint(top))
	return cyclesToDuration(cycles, clockHz), nil
}

// SelectTop chooses the smallest TOP selector (0..15) whose period is at least
// requested, clamping to MaxTop if requested exceeds the hardware maximum.
func SelectTop(clockHz uint64, requested time.Duration) (uint8, time.Duration, error) {
	if clockHz == 0 || requested <= 0 {
		return 0, 0, ErrInvalidConfig
	}
	var bestDur time.Duration
	for top := uint8(0); top <= MaxTop; top++ {
		dur, err := DurationForTop(clockHz, top)
		if err != nil {
			return 0, 0, err
		}
		bestDur = dur
		if dur >= requested {
			return top, dur, nil
		}
	}
	return MaxTop, bestDur, nil
}

// Start programs TORR with the selected timeout period, configures RMOD,
// enables WDT_EN, and kicks CRR so the counter reloads immediately.
// It returns the actual achieved single-stage hardware period.
func (w *DW) Start(timeout time.Duration, mode ResponseMode) (time.Duration, error) {
	if mode != ModeReset && mode != ModePreTimeoutIRQ {
		return 0, fmt.Errorf("%w: unknown response mode %d", ErrInvalidConfig, mode)
	}
	top, actual, err := SelectTop(w.clockHz, timeout)
	if err != nil {
		return 0, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	// Program both TOP_INIT (bits 7:4) and TOP (bits 3:0) before enabling.
	torr := uint32(top)<<4 | uint32(top)
	w.io.Write32(RegTORR, torr)

	cr := w.io.Read32(RegCR)
	if cr&CRRPLMask == 0 {
		cr |= CRRPL32
	}
	if mode == ModePreTimeoutIRQ {
		cr |= CRRespIRQ
	} else {
		cr &^= CRRespIRQ
	}
	cr |= CRWDTEnable
	w.io.Write32(RegCR, cr)
	w.io.Write32(RegCRR, KickMagic)
	return actual, nil
}

// Kick reloads the running watchdog counter by writing KickMagic (0x76) to CRR.
func (w *DW) Kick() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.io.Read32(RegCR)&CRWDTEnable == 0 {
		return ErrNotRunning
	}
	w.io.Write32(RegCRR, KickMagic)
	return nil
}

// Remaining reads WDT_CCVR and converts the remaining countdown cycles to a
// time.Duration using ClockHz.
func (w *DW) Remaining() time.Duration {
	w.mu.Lock()
	cycles := uint64(w.io.Read32(RegCCVR))
	w.mu.Unlock()
	return cyclesToDuration(cycles, w.clockHz)
}

// InterruptPending reports whether WDT_STAT bit 0 is asserted.
func (w *DW) InterruptPending() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.io.Read32(RegSTAT)&statActive != 0
}

// ClearInterrupt reads WDT_EOI to acknowledge and clear a pre-timeout interrupt.
func (w *DW) ClearInterrupt() {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.io.Read32(RegEOI)
}

// EnableIRQ binds the level-sensitive watchdog pre-timeout SPI on g. When the
// interrupt fires, the handler reads WDT_EOI to clear the level line, increments
// IRQCount, and calls onPreTimeout (if non-nil).
func (w *DW) EnableIRQ(g *gic.Controller, irqID uint32, onPreTimeout func()) error {
	if g == nil || irqID < gic.FirstSPI || irqID >= g.Lines() {
		return ErrInvalidConfig
	}
	w.irqMu.Lock()
	defer w.irqMu.Unlock()
	if w.irqActive.Load() {
		if w.irqGIC != nil {
			w.irqGIC.Disable(w.irqID)
			w.irqGIC.ClearPending(w.irqID)
			w.irqGIC.Register(w.irqID, nil)
		}
		w.irqActive.Store(false)
	}
	w.irqGIC = g
	w.irqID = irqID
	w.onPreIRQ = onPreTimeout

	g.Disable(irqID)
	g.ClearPending(irqID)
	g.SetEdge(irqID, false) // DW WDT interrupt is active-high level-sensitive.
	w.irqActive.Store(true)
	g.Register(irqID, w.handleIRQ)
	g.Enable(irqID)
	return nil
}

// DisableIRQ masks and unregisters the watchdog SPI on g.
func (w *DW) DisableIRQ(g *gic.Controller) {
	w.irqMu.Lock()
	defer w.irqMu.Unlock()
	if !w.irqActive.Load() {
		return
	}
	w.irqActive.Store(false)
	target := w.irqGIC
	if target == nil {
		target = g
	}
	if target != nil {
		target.Disable(w.irqID)
		target.ClearPending(w.irqID)
		target.Register(w.irqID, nil)
	}
	w.irqGIC = nil
	w.onPreIRQ = nil
}

// IRQCount reports the number of pre-timeout watchdog interrupts handled.
func (w *DW) IRQCount() uint64 { return w.irqCount.Load() }

func (w *DW) handleIRQ() {
	w.irqMu.Lock()
	if !w.irqActive.Load() {
		w.irqMu.Unlock()
		return
	}
	cb := w.onPreIRQ

	w.mu.Lock()
	pending := w.io.Read32(RegSTAT)&statActive != 0
	if pending {
		_ = w.io.Read32(RegEOI)
	}
	w.mu.Unlock()
	w.irqMu.Unlock()

	if !pending {
		return
	}
	w.irqCount.Add(1)
	if cb != nil {
		cb()
	}
}

func cyclesToDuration(cycles, clockHz uint64) time.Duration {
	if cycles == 0 || clockHz == 0 {
		return 0
	}
	hi, lo := bits.Mul64(cycles, uint64(time.Second))
	if hi >= clockHz {
		return time.Duration(math.MaxInt64)
	}
	ns, _ := bits.Div64(hi, lo, clockHz)
	if ns > math.MaxInt64 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ns)
}
