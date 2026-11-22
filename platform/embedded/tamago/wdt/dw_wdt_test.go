// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

type fakeDW struct {
	mu       sync.Mutex
	cr       uint32
	torr     uint32
	ccvr     uint32
	stat     uint32
	version  uint32
	compType uint32
	kicks    int
	eoiReads int
}

func newFakeDW() *fakeDW {
	return &fakeDW{
		version:  0x3130392a, // "109*"
		compType: CompTypeDW,
	}
}

func (f *fakeDW) Read32(off uint32) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch off {
	case RegCR:
		return f.cr
	case RegTORR:
		return f.torr
	case RegCCVR:
		return f.ccvr
	case RegSTAT:
		return f.stat
	case RegEOI:
		f.eoiReads++
		f.stat = 0
		return 0
	case RegCompVersion:
		return f.version
	case RegCompType:
		return f.compType
	default:
		return 0
	}
}

func (f *fakeDW) Write32(off uint32, val uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch off {
	case RegCR:
		// Hardware keeps WDT_EN sticky once set.
		sticky := f.cr & CRWDTEnable
		f.cr = (val & 0x1f) | sticky
	case RegTORR:
		f.torr = val & 0xff
	case RegCRR:
		if val == KickMagic {
			f.kicks++
			top := f.torr & 0x0f
			f.ccvr = uint32(1) << (16 + top)
		}
	}
}

func (f *fakeDW) setCounter(v uint32) {
	f.mu.Lock()
	f.ccvr = v
	f.mu.Unlock()
}

func (f *fakeDW) raisePreTimeout() {
	f.mu.Lock()
	f.stat = 1
	f.mu.Unlock()
}

type fakeGICAccess struct {
	mu      sync.Mutex
	regs32  map[uint64]uint32
	regs64  map[uint64]uint64
	pending uint32
}

func newFakeGIC(t *testing.T) (*gic.Controller, *fakeGICAccess) {
	t.Helper()
	acc := &fakeGICAccess{
		regs32: map[uint64]uint32{
			0x1d000004: 7,          // GICD_TYPER: ITLinesNumber=7 -> 256 lines
			0x1d060008: 0x00000010, // GICR_TYPER Last=1
		},
		regs64: map[uint64]uint64{
			0x1d060008: 0x00000010, // GICR_TYPER affinity 0, Last=1
		},
		pending: 1023,
	}
	ctrl := gic.NewWithAccess(0x1d000000, 0x1d060000, acc)
	if err := ctrl.Init(); err != nil {
		t.Fatalf("gic.Init: %v", err)
	}
	return ctrl, acc
}

func (a *fakeGICAccess) HasGICv3() bool { return true }
func (a *fakeGICAccess) MPIDR() uint64  { return 0 }
func (a *fakeGICAccess) Read32(addr uint64) uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.regs32[addr]
}
func (a *fakeGICAccess) Write32(addr uint64, val uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.regs32[addr] = val
}
func (a *fakeGICAccess) Read64(addr uint64) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.regs64[addr]
}
func (a *fakeGICAccess) Write64(addr uint64, val uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.regs64[addr] = val
}
func (a *fakeGICAccess) InitCPU(uint8) {}
func (a *fakeGICAccess) Ack() uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := a.pending
	a.pending = 1023
	return id
}
func (a *fakeGICAccess) End(uint32)     {}
func (a *fakeGICAccess) SendSGI(uint64) {}

func TestOpenValidationAndOptIn(t *testing.T) {
	if _, err := Open(nil, 100_000_000); !errors.Is(err, ErrNilAccess) {
		t.Fatalf("nil io err=%v, want ErrNilAccess", err)
	}
	hw := newFakeDW()
	if _, err := Open(hw, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("zero clock err=%v, want ErrInvalidConfig", err)
	}
	bad := newFakeDW()
	bad.compType = 0xdeadbeef
	if _, err := Open(bad, 100_000_000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad comp type err=%v, want ErrNotFound", err)
	}

	dev, err := Open(hw, 100_000_000)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if dev.Enabled() {
		t.Fatal("Open must not arm watchdog")
	}
	if dev.Version() != hw.version || dev.ClockHz() != 100_000_000 {
		t.Fatalf("version=%#x clock=%d", dev.Version(), dev.ClockHz())
	}
	if err := dev.Kick(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Kick before Start err=%v, want ErrNotRunning", err)
	}
}

func TestTopSelectionAndDurationMath(t *testing.T) {
	const clk = 100_000_000 // 100 MHz L4 clock

	if _, err := DurationForTop(0, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("zero clock DurationForTop err=%v", err)
	}
	if _, err := DurationForTop(clk, MaxTop+1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("overflow top DurationForTop err=%v", err)
	}
	if _, _, err := SelectTop(0, time.Second); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("zero clock SelectTop err=%v", err)
	}
	if _, _, err := SelectTop(clk, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("zero timeout SelectTop err=%v", err)
	}

	// At 100 MHz: top=14 -> 2^30 = 1,073,741,824 cycles = 10.73741824s.
	top, dur, err := SelectTop(clk, 10*time.Second)
	if err != nil {
		t.Fatalf("SelectTop 10s: %v", err)
	}
	if top != 14 || dur < 10*time.Second || dur > 11*time.Second {
		t.Fatalf("SelectTop 10s = (top=%d, dur=%v), want top=14 ~10.74s", top, dur)
	}

	// Requests larger than max hardware period clamp to MaxTop (15 -> ~21.47s at 100 MHz).
	topMax, durMax, err := SelectTop(clk, time.Minute)
	if err != nil {
		t.Fatalf("SelectTop 1m: %v", err)
	}
	if topMax != MaxTop || durMax < 21*time.Second {
		t.Fatalf("SelectTop 1m = (top=%d, dur=%v), want top=15", topMax, durMax)
	}

	// Overflow protection in cyclesToDuration.
	if got := cyclesToDuration(0, clk); got != 0 {
		t.Fatalf("0 cycles duration=%v", got)
	}
	if got := cyclesToDuration(math.MaxUint64, 1); got != time.Duration(math.MaxInt64) {
		t.Fatalf("overflow duration=%v, want MaxInt64", got)
	}
	// Very slow clock where hi < clockHz still saturates above MaxInt64 ns.
	if got := cyclesToDuration(1<<35, 1); got != time.Duration(math.MaxInt64) {
		t.Fatalf("ns > MaxInt64 duration=%v, want MaxInt64", got)
	}
}

func TestStartKickRemainingAndModes(t *testing.T) {
	const clk = 100_000_000
	hw := newFakeDW()
	dev, err := Open(hw, clk)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := dev.Start(time.Second, ResponseMode(9)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid mode err=%v", err)
	}
	if _, err := dev.Start(-time.Second, ModeReset); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative timeout err=%v", err)
	}

	actual, err := dev.Start(5*time.Second, ModeReset)
	if err != nil {
		t.Fatalf("Start ModeReset: %v", err)
	}
	if !dev.Enabled() {
		t.Fatal("Start did not enable WDT_EN")
	}
	if actual < 5*time.Second {
		t.Fatalf("actual=%v < 5s", actual)
	}
	if hw.Read32(RegCR)&(CRWDTEnable|CRRespIRQ|CRRPLMask) != (CRWDTEnable | CRRPL32) {
		t.Fatalf("CR=%#x, want WDT_EN|RPL32 without RMOD", hw.Read32(RegCR))
	}
	// TOP=13 -> 2^29 = 536,870,912 cycles (~5.368s); TORR should set both TOP_INIT and TOP.
	if got := hw.Read32(RegTORR); got != (13<<4)|13 {
		t.Fatalf("TORR=%#x, want %#x", got, (13<<4)|13)
	}
	if hw.kicks != 1 {
		t.Fatalf("kicks=%d after Start, want 1", hw.kicks)
	}

	hw.setCounter(100_000_000) // 1s left
	if rem := dev.Remaining(); rem != time.Second {
		t.Fatalf("Remaining=%v, want 1s", rem)
	}

	if err := dev.Kick(); err != nil {
		t.Fatalf("Kick: %v", err)
	}
	if hw.kicks != 2 {
		t.Fatalf("kicks=%d after Kick, want 2", hw.kicks)
	}

	// Switch to ModePreTimeoutIRQ and test interrupt pending + manual clear.
	if _, err := dev.Start(2*time.Second, ModePreTimeoutIRQ); err != nil {
		t.Fatalf("Start ModePreTimeoutIRQ: %v", err)
	}
	if hw.Read32(RegCR)&CRRespIRQ == 0 {
		t.Fatal("ModePreTimeoutIRQ did not set RMOD")
	}
	if dev.InterruptPending() {
		t.Fatal("InterruptPending true before expiry")
	}
	hw.raisePreTimeout()
	if !dev.InterruptPending() {
		t.Fatal("InterruptPending false after expiry")
	}
	dev.ClearInterrupt()
	if dev.InterruptPending() || hw.eoiReads != 1 {
		t.Fatalf("ClearInterrupt failed pending=%t eoiReads=%d", dev.InterruptPending(), hw.eoiReads)
	}
}

func TestPreTimeoutIRQDispatchAndDisable(t *testing.T) {
	hw := newFakeDW()
	dev, err := Open(hw, 100_000_000)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctrl, acc := newFakeGIC(t)

	if err := dev.EnableIRQ(nil, 149, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil GIC err=%v", err)
	}
	if err := dev.EnableIRQ(ctrl, 16, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("non-SPI err=%v", err)
	}
	if err := dev.EnableIRQ(ctrl, ctrl.Lines(), nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("out-of-range SPI err=%v", err)
	}

	// DisableIRQ when not enabled is a safe no-op.
	dev.DisableIRQ(ctrl)

	var cbHits atomic.Int32
	if err := dev.EnableIRQ(ctrl, 149, func() { cbHits.Add(1) }); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	// Re-enabling replaces previous registration cleanly.
	if err := dev.EnableIRQ(ctrl, 149, func() { cbHits.Add(1) }); err != nil {
		t.Fatalf("re-EnableIRQ: %v", err)
	}

	// Spurious dispatch when WDT_STAT is 0 must not increment count or call callback.
	acc.mu.Lock()
	acc.pending = 149
	acc.mu.Unlock()
	ctrl.Dispatch()
	if dev.IRQCount() != 0 || cbHits.Load() != 0 {
		t.Fatalf("spurious IRQ counted: count=%d cb=%d", dev.IRQCount(), cbHits.Load())
	}

	// Real pre-timeout interrupt reads WDT_EOI, clears line, and invokes callback.
	hw.raisePreTimeout()
	acc.mu.Lock()
	acc.pending = 149
	acc.mu.Unlock()
	ctrl.Dispatch()
	if dev.IRQCount() != 1 || cbHits.Load() != 1 || dev.InterruptPending() {
		t.Fatalf("real IRQ state: count=%d cb=%d pending=%t", dev.IRQCount(), cbHits.Load(), dev.InterruptPending())
	}

	// DisableIRQ prevents late/stale handler execution from touching hardware.
	dev.DisableIRQ(ctrl)
	hw.raisePreTimeout()
	dev.handleIRQ()
	if dev.IRQCount() != 1 || !dev.InterruptPending() {
		t.Fatal("disabled handleIRQ touched hardware")
	}
}
