// SPDX-License-Identifier: GPL-3.0-or-later

// Package fpgairq provides a host-testable FPGA-to-HPS doorbell and VSync
// wakeup dispatcher over an ARM GICv3 controller (Agilex 5 f2s_irq lines
// 0..63 mapped to SPI INTIDs 48..111).
//
// Each claimed line exposes a coalesced wakeup channel (Ready), blocking
// Wait/WaitTimeout helpers that allow TamaGo's WFI idle governor to sleep
// between hardware events, and a YieldFunc adapter pluggable into
// scanout.ReaderLimits.Yield, scanout.MenuLimits.Yield, and computer.Limits.Yield.
package fpgairq

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// Agilex 5 FPGA-to-HPS GIC-600 SPI range (socfpga_agilex5.dtsi).
const (
	BaseSPI   uint32 = 48 // SPI 16 (IRQFPGA2HPS0)
	LineCount uint32 = 64 // 64 lines: 0..63 -> INTIDs 48..111
)

// Trigger selects edge-triggered or level-sensitive SPI sensing.
type Trigger uint8

const (
	// Edge selects rising-edge-triggered interrupts.
	Edge Trigger = iota
	// Level selects active-high level-sensitive interrupts. Callers must
	// supply Config.Ack to clear the FPGA interrupt source inside the handler.
	Level
)

var (
	// ErrInvalidConfig is returned when a GIC controller, line index, or trigger config is invalid.
	ErrInvalidConfig = errors.New("fpgairq: invalid configuration")
	// ErrLineInUse is returned when opening an FPGA IRQ line that is already claimed.
	ErrLineInUse = errors.New("fpgairq: line already claimed")
	// ErrClosed is returned when operating on a closed dispatcher or doorbell.
	ErrClosed = errors.New("fpgairq: closed")
)

// Config configures one FPGA-to-HPS doorbell line.
type Config struct {
	// Line is the FPGA-to-HPS line index in [0, LineCount).
	Line uint32
	// Trigger selects Edge or Level sensing. Level requires a non-nil Ack.
	Trigger Trigger
	// Ack clears the hardware interrupt source inside the GIC handler while
	// holding the doorbell's IRQ lock. Required when Trigger == Level.
	Ack func()
	// OnEvent is an optional non-blocking callback invoked after Ack and
	// count increment, outside the doorbell lock.
	OnEvent func(count uint64)
}

var (
	claimsMu         sync.Mutex
	controllerClaims = make(map[*gic.Controller]map[uint32]*Doorbell)
)

// Dispatcher owns registration of FPGA-to-HPS SPI lines (48..111) on a GICv3
// controller. Line ownership is coordinated across all Dispatcher instances
// attached to the same Controller so two callers cannot claim the same SPI.
type Dispatcher struct {
	mu       sync.Mutex
	gic      *gic.Controller
	maxLines uint32
	closed   bool
	lines    map[uint32]*Doorbell
}

// New creates a Dispatcher bound to an initialized GICv3 controller.
func New(g *gic.Controller) (*Dispatcher, error) {
	if g == nil || g.Lines() <= BaseSPI {
		return nil, ErrInvalidConfig
	}
	avail := g.Lines() - BaseSPI
	if avail > LineCount {
		avail = LineCount
	}
	return &Dispatcher{
		gic:      g,
		maxLines: avail,
		lines:    make(map[uint32]*Doorbell),
	}, nil
}

// LineCount reports how many FPGA-to-HPS lines this controller can address (up to 64).
func (p *Dispatcher) LineCount() uint32 { return p.maxLines }

// ActiveCount reports how many FPGA-to-HPS doorbells are currently open.
func (p *Dispatcher) ActiveCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.lines)
}

// Open exclusively claims cfg.Line, configures its GIC SPI sensing, clears any
// stale pending state, registers its handler, and unmasks the SPI.
func (p *Dispatcher) Open(cfg Config) (*Doorbell, error) {
	if cfg.Line >= p.maxLines {
		return nil, fmt.Errorf("%w: line %d >= %d", ErrInvalidConfig, cfg.Line, p.maxLines)
	}
	if cfg.Trigger != Edge && cfg.Trigger != Level {
		return nil, fmt.Errorf("%w: unknown trigger %d", ErrInvalidConfig, cfg.Trigger)
	}
	if cfg.Trigger == Level && cfg.Ack == nil {
		return nil, fmt.Errorf("%w: level-sensitive line requires Ack callback", ErrInvalidConfig)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, ErrClosed
	}

	claimsMu.Lock()
	defer claimsMu.Unlock()
	byLine := controllerClaims[p.gic]
	if _, exists := byLine[cfg.Line]; exists {
		return nil, fmt.Errorf("%w: line %d", ErrLineInUse, cfg.Line)
	}

	irqID := BaseSPI + cfg.Line
	d := &Doorbell{
		disp:    p,
		gic:     p.gic,
		line:    cfg.Line,
		irq:     irqID,
		ack:     cfg.Ack,
		onEvent: cfg.OnEvent,
		ready:   make(chan struct{}, 1),
		closed:  make(chan struct{}),
	}
	d.active.Store(true)
	if byLine == nil {
		byLine = make(map[uint32]*Doorbell)
		controllerClaims[p.gic] = byLine
	}
	byLine[cfg.Line] = d
	p.lines[cfg.Line] = d

	p.gic.Disable(irqID)
	p.gic.ClearPending(irqID)
	p.gic.SetEdge(irqID, cfg.Trigger == Edge)
	p.gic.Register(irqID, d.handleIRQ)
	p.gic.Enable(irqID)
	return d, nil
}

// Close masks, clears, and unregisters all open doorbells, waits for any
// in-flight handlers to finish, and prevents future Open calls.
func (p *Dispatcher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	open := make([]*Doorbell, 0, len(p.lines))
	for _, d := range p.lines {
		open = append(open, d)
	}
	p.mu.Unlock()

	for _, d := range open {
		_ = d.Close()
	}
	return nil
}

// Doorbell represents one claimed FPGA-to-HPS IRQ wakeup channel.
type Doorbell struct {
	disp   *Dispatcher
	gic    *gic.Controller
	line   uint32
	irq    uint32
	ready  chan struct{}
	closed chan struct{}

	closeMu sync.Mutex
	irqMu   sync.Mutex
	wg      sync.WaitGroup
	active  atomic.Bool
	ack     func()
	onEvent func(count uint64)
	count   atomic.Uint64
}

// Line returns the FPGA-to-HPS line index (0..63).
func (d *Doorbell) Line() uint32 { return d.line }

// IRQ returns the GICv3 SPI INTID (48..111).
func (d *Doorbell) IRQ() uint32 { return d.irq }

// Ready returns a capacity-1 coalesced notification channel pulsed on each IRQ.
func (d *Doorbell) Ready() <-chan struct{} { return d.ready }

// Count returns the total number of interrupts handled on this doorbell.
func (d *Doorbell) Count() uint64 { return d.count.Load() }

// TriggerPending sets the bound SPI pending in GICD_ISPENDR (for self-tests
// and software wakeup verification). No-op once closed.
func (d *Doorbell) TriggerPending() {
	d.irqMu.Lock()
	defer d.irqMu.Unlock()
	if !d.active.Load() {
		return
	}
	d.gic.SetPending(d.irq)
}

// Wait blocks until an IRQ arrives on this doorbell, ctx is canceled, or the
// doorbell is closed. Parking in Wait lets TamaGo's WFI idle governor sleep.
func (d *Doorbell) Wait(ctx context.Context) error {
	if !d.active.Load() {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-d.ready:
		return nil
	case <-d.closed:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitTimeout blocks until an IRQ arrives or timeout elapses, returning true
// if a doorbell event woke the caller.
func (d *Doorbell) WaitTimeout(timeout time.Duration) bool {
	if !d.active.Load() {
		return false
	}
	if timeout <= 0 {
		select {
		case <-d.ready:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-d.ready:
		return true
	case <-d.closed:
		return false
	case <-timer.C:
		return false
	}
}

// YieldFunc returns a bounded wakeup function directly compatible with
// scanout.ReaderLimits.Yield, scanout.MenuLimits.Yield, and computer.Limits.Yield.
// Each call wakes immediately when the doorbell fires, or returns after maxWait.
func (d *Doorbell) YieldFunc(maxWait time.Duration) func() {
	return func() {
		if maxWait <= 0 {
			select {
			case <-d.ready:
			default:
				runtime.Gosched()
			}
			return
		}
		if !d.active.Load() {
			runtime.Gosched()
			return
		}
		timer := time.NewTimer(maxWait)
		defer timer.Stop()
		select {
		case <-d.ready:
		case <-d.closed:
		case <-timer.C:
		}
	}
}

// YieldOrFallback returns db.YieldFunc(maxWait) when db is non-nil, or falls
// back to fallback (or runtime.Gosched when fallback is nil).
func YieldOrFallback(db *Doorbell, maxWait time.Duration, fallback func()) func() {
	if db != nil {
		return db.YieldFunc(maxWait)
	}
	if fallback != nil {
		return fallback
	}
	return runtime.Gosched
}

// Close masks and unregisters the SPI, waits for any in-flight IRQ handler and
// OnEvent callback to finish, releases the controller-wide line claim, and
// unblocks any callers parked in Wait or YieldFunc. Idempotent. Callbacks must
// not call Close synchronously.
func (d *Doorbell) Close() error {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	d.irqMu.Lock()
	if !d.active.Swap(false) {
		d.irqMu.Unlock()
		return nil
	}
	d.gic.Disable(d.irq)
	d.gic.ClearPending(d.irq)
	d.gic.Register(d.irq, nil)
	d.ack = nil
	d.onEvent = nil
	close(d.closed)
	d.irqMu.Unlock()

	// Drain any in-flight handleIRQ (including count update, ready pulse, and
	// OnEvent callback) before releasing the line claim or returning.
	d.wg.Wait()

	d.disp.mu.Lock()
	if d.disp.lines[d.line] == d {
		delete(d.disp.lines, d.line)
	}
	claimsMu.Lock()
	if byLine := controllerClaims[d.gic]; byLine[d.line] == d {
		delete(byLine, d.line)
		if len(byLine) == 0 {
			delete(controllerClaims, d.gic)
		}
	}
	claimsMu.Unlock()
	d.disp.mu.Unlock()
	return nil
}

func (d *Doorbell) handleIRQ() {
	d.irqMu.Lock()
	if !d.active.Load() {
		d.irqMu.Unlock()
		return
	}
	ack := d.ack
	onEvent := d.onEvent
	if ack != nil {
		ack()
	}
	d.wg.Add(1)
	d.irqMu.Unlock()
	defer d.wg.Done()

	n := d.count.Add(1)
	select {
	case d.ready <- struct{}{}:
	default:
	}
	if onEvent != nil {
		onEvent(n)
	}
}
