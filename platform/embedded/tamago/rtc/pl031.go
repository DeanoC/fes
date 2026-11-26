// SPDX-License-Identifier: GPL-3.0-or-later

// Package rtc provides bare-metal real-time clock drivers and an epoch-anchored
// wall clock for TamaGo boards.
//
// TamaGo's runtime/goos.Nanotime must stay strictly monotonic (driven by
// CNTPCT_EL0) so timers, deadlines, and sleep scheduling never jump backward.
// This package keeps wall-clock UTC separate from runtime monotonic time:
// PL031 supplies the initial UTC epoch (in whole seconds) on QEMU virt, and
// Clock anchors that epoch to a monotonic nanosecond counter so repeated Now()
// calls avoid MMIO and preserve sub-second progression.
package rtc

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// ARM PrimeCell PL031 RTC register offsets.
const (
	RegDR   uint32 = 0x000 // Data Register (current 32-bit Unix seconds)
	RegMR   uint32 = 0x004 // Match Register (alarm target seconds)
	RegLR   uint32 = 0x008 // Load Register (set counter seconds)
	RegCR   uint32 = 0x00c // Control Register (bit 0 = RTC start/enable)
	RegIMSC uint32 = 0x010 // Interrupt Mask Set/Clear (bit 0 = mask)
	RegRIS  uint32 = 0x014 // Raw Interrupt Status (bit 0)
	RegMIS  uint32 = 0x018 // Masked Interrupt Status (bit 0)
	RegICR  uint32 = 0x01c // Interrupt Clear Register (write 1 to clear)

	RegPeriphID0 uint32 = 0xfe0
	RegPeriphID1 uint32 = 0xfe4
	RegPeriphID2 uint32 = 0xfe8
	RegPeriphID3 uint32 = 0xfec

	RegPCellID0 uint32 = 0xff0
	RegPCellID1 uint32 = 0xff4
	RegPCellID2 uint32 = 0xff8
	RegPCellID3 uint32 = 0xffc
)

// ARM PrimeCell PL031 identification constants.
const (
	PrimeCellID     uint32 = 0xb105f00d
	PL031PartNumber uint16 = 0x031
	ARMDesignerID   uint8  = 0x41

	crStart uint32 = 1 << 0
	irqBit  uint32 = 1 << 0
)

var (
	// ErrNilAccess is returned when OpenPL031 receives a nil Registers handle.
	ErrNilAccess = errors.New("rtc: nil register access")
	// ErrInvalidConfig is returned when IRQ registration receives an invalid GIC or SPI ID.
	ErrInvalidConfig = errors.New("rtc: invalid PL031 configuration")
	// ErrNotFound is returned when the PrimeCell or PL031 peripheral ID does not match.
	ErrNotFound = errors.New("rtc: PL031 PrimeCell ID mismatch")
	// ErrOutOfRange is returned when a wall-clock time falls outside uint32 Unix seconds.
	ErrOutOfRange = errors.New("rtc: timestamp outside 32-bit unsigned Unix range")
)

// Registers abstracts 32-bit little-endian MMIO access to a PL031 block.
type Registers interface {
	Read32(offset uint32) uint32
	Write32(offset uint32, val uint32)
}

// PL031 drives an ARM PrimeCell PL031 real-time clock.
type PL031 struct {
	mu         sync.Mutex
	io         Registers
	clock      *Clock
	partNumber uint16
	designer   uint8
	revision   uint8

	irqCfgMu  sync.Mutex
	irqMu     sync.Mutex
	irqWG     sync.WaitGroup
	irqActive atomic.Bool
	irqCount  atomic.Uint64
	irqGIC    *gic.Controller
	irqID     uint32
	onAlarm   func()
}

// OpenPL031 probes the PrimeCell and Peripheral ID registers at io, ensures the
// RTC counter is enabled, and returns a ready PL031 handle.
func OpenPL031(io Registers) (*PL031, error) {
	if io == nil {
		return nil, ErrNilAccess
	}
	pcell := (io.Read32(RegPCellID0) & 0xff) |
		(io.Read32(RegPCellID1)&0xff)<<8 |
		(io.Read32(RegPCellID2)&0xff)<<16 |
		(io.Read32(RegPCellID3)&0xff)<<24
	if pcell != PrimeCellID {
		return nil, fmt.Errorf("%w: pcell=%#08x", ErrNotFound, pcell)
	}

	p0 := io.Read32(RegPeriphID0)
	p1 := io.Read32(RegPeriphID1)
	p2 := io.Read32(RegPeriphID2)
	partNumber := uint16(p0&0xff) | uint16(p1&0x0f)<<8
	designer := uint8((p1>>4)&0x0f) | uint8(p2&0x0f)<<4
	revision := uint8((p2 >> 4) & 0x0f)
	if partNumber != PL031PartNumber || designer != ARMDesignerID {
		return nil, fmt.Errorf("%w: part=%#03x designer=%#02x", ErrNotFound, partNumber, designer)
	}

	if io.Read32(RegCR)&crStart == 0 {
		io.Write32(RegCR, crStart)
	}
	return &PL031{
		io:         io,
		partNumber: partNumber,
		designer:   designer,
		revision:   revision,
	}, nil
}

// PartNumber reports the 12-bit PrimeCell peripheral part number (0x031).
func (r *PL031) PartNumber() uint16 { return r.partNumber }

// Designer reports the 8-bit JEDEC designer ID (0x41 for ARM).
func (r *PL031) Designer() uint8 { return r.designer }

// Revision reports the 4-bit PrimeCell peripheral revision nibble.
func (r *PL031) Revision() uint8 { return r.revision }

// Seconds reads the current 32-bit Unix epoch seconds from RTCDR.
func (r *PL031) Seconds() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.io.Read32(RegDR)
}

// Now returns the current RTC reading as a UTC time.Time (whole-second resolution).
func (r *PL031) Now() time.Time {
	return time.Unix(int64(r.Seconds()), 0).UTC()
}

// BindClock keeps c synchronized with this RTC's whole-second epoch. Binding
// reads the current hardware value, and successful Set calls re-anchor c to
// the value loaded into the PL031.
func (r *PL031) BindClock(c *Clock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clock = c
	if c != nil {
		c.Sync(time.Unix(int64(r.io.Read32(RegDR)), 0).UTC())
	}
}

// Set loads the PL031 counter (RTCLR) with t's Unix epoch seconds.
func (r *PL031) Set(t time.Time) error {
	sec, err := unixSecondsUint32(t)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.io.Write32(RegLR, sec)
	if r.clock != nil {
		r.clock.Sync(time.Unix(int64(sec), 0).UTC())
	}
	return nil
}

// SetAlarm programs RTCMR for t, clears any stale interrupt, and unmasks the
// PL031 alarm interrupt line.
func (r *PL031) SetAlarm(t time.Time) error {
	sec, err := unixSecondsUint32(t)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.io.Write32(RegIMSC, 0)
	r.io.Write32(RegICR, irqBit)
	r.io.Write32(RegMR, sec)
	r.io.Write32(RegIMSC, irqBit)
	return nil
}

// ClearAlarm masks and clears the PL031 alarm interrupt.
func (r *PL031) ClearAlarm() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.io.Write32(RegIMSC, 0)
	r.io.Write32(RegICR, irqBit)
}

// AlarmPending reports whether the masked PL031 alarm interrupt is asserted.
func (r *PL031) AlarmPending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.io.Read32(RegMIS)&irqBit != 0
}

// AlarmTime returns the programmed UTC alarm match register (RTCMR) and
// whether the PL031 alarm interrupt mask (RTCIMSC) is currently enabled.
func (r *PL031) AlarmTime() (time.Time, bool) {
	if r == nil {
		return time.Time{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	alarm := time.Unix(int64(r.io.Read32(RegMR)), 0).UTC()
	enabled := r.io.Read32(RegIMSC)&irqBit != 0
	return alarm, enabled
}

// EnableIRQ binds the level-sensitive PL031 alarm SPI on g. When the alarm
// interrupt fires, the handler writes RTCICR to clear the level-sensitive line
// while keeping RTCIMSC intact, increments IRQCount, and calls onAlarm (if
// non-nil). onAlarm must be non-blocking and must not call EnableIRQ or
// DisableIRQ; calling SetAlarm, ClearAlarm, Now, Seconds, or Set is safe.
func (r *PL031) EnableIRQ(g *gic.Controller, irqID uint32, onAlarm func()) error {
	if g == nil || irqID < gic.FirstSPI || irqID >= g.Lines() {
		return ErrInvalidConfig
	}
	r.irqCfgMu.Lock()
	defer r.irqCfgMu.Unlock()

	r.disableIRQLocked(g)

	r.irqMu.Lock()
	r.irqGIC = g
	r.irqID = irqID
	r.onAlarm = onAlarm

	g.Disable(irqID)
	g.ClearPending(irqID)
	g.SetEdge(irqID, false) // PL031 RTCINTR is active-high level-sensitive.
	r.irqActive.Store(true)
	g.Register(irqID, r.handleIRQ)
	g.Enable(irqID)
	r.irqMu.Unlock()
	return nil
}

// DisableIRQ masks and unregisters the PL031 alarm SPI on g and waits for any
// in-flight alarm callback to finish before returning.
func (r *PL031) DisableIRQ(g *gic.Controller) {
	r.irqCfgMu.Lock()
	defer r.irqCfgMu.Unlock()
	r.disableIRQLocked(g)
}

func (r *PL031) disableIRQLocked(g *gic.Controller) {
	r.irqMu.Lock()
	if !r.irqActive.Load() {
		r.irqMu.Unlock()
		return
	}
	r.irqActive.Store(false)
	target := r.irqGIC
	if target == nil {
		target = g
	}
	if target != nil {
		target.Disable(r.irqID)
		target.ClearPending(r.irqID)
		target.Register(r.irqID, nil)
	}
	r.irqGIC = nil
	r.onAlarm = nil
	r.irqMu.Unlock()

	r.irqWG.Wait()
}

// IRQCount reports the number of PL031 alarm interrupts handled.
func (r *PL031) IRQCount() uint64 { return r.irqCount.Load() }

func (r *PL031) handleIRQ() {
	r.irqMu.Lock()
	if !r.irqActive.Load() {
		r.irqMu.Unlock()
		return
	}
	cb := r.onAlarm

	r.mu.Lock()
	pending := r.io.Read32(RegMIS)&irqBit != 0
	if pending {
		// Clear RTCRIS/RTCMIS before invoking cb so level-sensitive GIC re-entry cannot storm.
		r.io.Write32(RegICR, irqBit)
	}
	r.mu.Unlock()
	if pending && cb != nil {
		r.irqWG.Add(1)
	}
	r.irqMu.Unlock()

	if !pending {
		return
	}
	r.irqCount.Add(1)
	if cb != nil {
		defer r.irqWG.Done()
		cb()
	}
}

func unixSecondsUint32(t time.Time) (uint32, error) {
	sec := t.Unix()
	if sec < 0 || sec > math.MaxUint32 {
		return 0, fmt.Errorf("%w: %d", ErrOutOfRange, sec)
	}
	return uint32(sec), nil
}

// ClockStatus describes synchronization state and the most recent time step for
// a monotonic-anchored Clock.
type ClockStatus struct {
	Synced       bool
	SyncCount    uint64
	LastSyncWall time.Time
	LastSyncAge  time.Duration
	LastStep     time.Duration
}

// Clock tracks UTC wall-clock time by anchoring a reference wall-clock instant
// to a monotonic nanosecond source. Repeated Now() calls perform no MMIO and
// advance with sub-second monotonic precision.
type Clock struct {
	mu        sync.RWMutex
	synced    bool
	syncCount uint64
	lastStep  time.Duration
	baseWall  time.Time
	baseMono  int64
	mono      func() int64
}

func resolveMonoSource(mono func() int64) func() int64 {
	if mono != nil {
		return mono
	}
	start := time.Now()
	return func() int64 { return int64(time.Since(start)) }
}

// NewClock anchors wall to mono(). If mono is nil, a monotonic clock backed by
// time.Since is used.
func NewClock(wall time.Time, mono func() int64) *Clock {
	m := resolveMonoSource(mono)
	return &Clock{
		synced:    true,
		syncCount: 1,
		baseWall:  wall.UTC(),
		baseMono:  m(),
		mono:      m,
	}
}

// NewUnsyncedClock creates a monotonic-backed Clock with no valid UTC epoch
// until Sync is called. WallTime returns (time.Time{}, false) before the
// first Sync.
func NewUnsyncedClock(mono func() int64) *Clock {
	m := resolveMonoSource(mono)
	return &Clock{
		baseMono: m(),
		mono:     m,
	}
}

// Synced reports whether the clock holds a synchronized UTC epoch.
func (c *Clock) Synced() bool {
	c.mu.RLock()
	synced := c.synced
	c.mu.RUnlock()
	return synced
}

// Status returns a snapshot of synchronization count, last epoch, monotonic age
// since that sync, and the most recent time step applied on re-sync.
func (c *Clock) Status() ClockStatus {
	if c == nil {
		return ClockStatus{}
	}
	c.mu.RLock()
	synced := c.synced
	syncCount := c.syncCount
	baseWall := c.baseWall
	baseMono := c.baseMono
	lastStep := c.lastStep
	mono := c.mono
	c.mu.RUnlock()

	if !synced {
		return ClockStatus{}
	}
	age := mono() - baseMono
	if age < 0 {
		age = 0
	}
	return ClockStatus{
		Synced:       true,
		SyncCount:    syncCount,
		LastSyncWall: baseWall,
		LastSyncAge:  time.Duration(age),
		LastStep:     lastStep,
	}
}

// WallTime returns the current estimated UTC wall-clock time alongside
// whether Sync (or NewClock) has established a valid UTC epoch.
func (c *Clock) WallTime() (time.Time, bool) {
	c.mu.RLock()
	synced := c.synced
	baseWall := c.baseWall
	baseMono := c.baseMono
	mono := c.mono
	c.mu.RUnlock()

	if !synced {
		return time.Time{}, false
	}
	delta := mono() - baseMono
	if delta < 0 {
		delta = 0
	}
	return baseWall.Add(time.Duration(delta)).UTC(), true
}

// Now returns the current estimated UTC wall-clock time.
func (c *Clock) Now() time.Time {
	c.mu.RLock()
	baseWall := c.baseWall
	baseMono := c.baseMono
	mono := c.mono
	c.mu.RUnlock()

	delta := mono() - baseMono
	if delta < 0 {
		delta = 0
	}
	return baseWall.Add(time.Duration(delta)).UTC()
}

// Sync re-anchors the wall-clock epoch to wall at the current monotonic tick.
func (c *Clock) Sync(wall time.Time) {
	c.mu.Lock()
	nowMono := c.mono()
	wallUTC := wall.UTC()
	if c.synced {
		delta := nowMono - c.baseMono
		if delta < 0 {
			delta = 0
		}
		prevWall := c.baseWall.Add(time.Duration(delta))
		c.lastStep = wallUTC.Sub(prevWall)
	} else {
		c.lastStep = 0
	}
	c.synced = true
	c.syncCount++
	c.baseWall = wallUTC
	c.baseMono = nowMono
	c.mu.Unlock()
}
