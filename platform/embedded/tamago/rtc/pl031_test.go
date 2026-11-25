// SPDX-License-Identifier: GPL-3.0-or-later

package rtc

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

type fakePL031 struct {
	mu      sync.Mutex
	dr      uint32
	mr      uint32
	lr      uint32
	cr      uint32
	imsc    uint32
	ris     uint32
	periph  [4]uint32
	pcell   [4]uint32
	writes  []uint32
	cleared int
}

func newFakePL031(epoch uint32) *fakePL031 {
	return &fakePL031{
		dr:     epoch,
		cr:     0,                                 // start disabled to verify OpenPL031 enables RTCCR
		periph: [4]uint32{0x31, 0x10, 0x14, 0x00}, // part=0x031, designer=0x41, rev=1
		pcell:  [4]uint32{0x0d, 0xf0, 0x05, 0xb1}, // 0xb105f00d
	}
}

func (f *fakePL031) Read32(off uint32) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch off {
	case RegDR:
		return f.dr
	case RegMR:
		return f.mr
	case RegLR:
		return f.lr
	case RegCR:
		return f.cr
	case RegIMSC:
		return f.imsc
	case RegRIS:
		return f.ris
	case RegMIS:
		return f.ris & f.imsc
	case RegPeriphID0:
		return f.periph[0]
	case RegPeriphID1:
		return f.periph[1]
	case RegPeriphID2:
		return f.periph[2]
	case RegPeriphID3:
		return f.periph[3]
	case RegPCellID0:
		return f.pcell[0]
	case RegPCellID1:
		return f.pcell[1]
	case RegPCellID2:
		return f.pcell[2]
	case RegPCellID3:
		return f.pcell[3]
	default:
		return 0
	}
}

func (f *fakePL031) Write32(off uint32, val uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, off)
	switch off {
	case RegMR:
		f.mr = val
	case RegLR:
		f.lr = val
		f.dr = val
	case RegCR:
		f.cr = val & 1
	case RegIMSC:
		f.imsc = val & 1
	case RegICR:
		if val&1 != 0 {
			f.ris = 0
			f.cleared++
		}
	}
}

func (f *fakePL031) triggerAlarm() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ris = 1
}

func TestOpenPL031Validation(t *testing.T) {
	if _, err := OpenPL031(nil); !errors.Is(err, ErrNilAccess) {
		t.Fatalf("nil io err=%v, want ErrNilAccess", err)
	}

	badPCell := newFakePL031(1700000000)
	badPCell.pcell[3] = 0x00
	if _, err := OpenPL031(badPCell); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad PrimeCell ID err=%v, want ErrNotFound", err)
	}

	badPart := newFakePL031(1700000000)
	badPart.periph[0] = 0x11 // PL011 instead of PL031
	if _, err := OpenPL031(badPart); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad part number err=%v, want ErrNotFound", err)
	}

	badDesigner := newFakePL031(1700000000)
	badDesigner.periph[2] = 0x15 // designer != 0x41
	if _, err := OpenPL031(badDesigner); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad designer err=%v, want ErrNotFound", err)
	}
}

func TestPL031ReadSetAndAlarm(t *testing.T) {
	const initial = uint32(1795380000) // 2026-11-22
	hw := newFakePL031(initial)

	dev, err := OpenPL031(hw)
	if err != nil {
		t.Fatalf("OpenPL031: %v", err)
	}
	if hw.Read32(RegCR) != 1 {
		t.Fatal("OpenPL031 did not enable RTCCR")
	}
	if dev.PartNumber() != PL031PartNumber || dev.Designer() != ARMDesignerID || dev.Revision() != 1 {
		t.Fatalf("IDs=(%#x, %#x, %d), want (%#x, %#x, 1)",
			dev.PartNumber(), dev.Designer(), dev.Revision(), PL031PartNumber, ARMDesignerID)
	}
	if got := dev.Seconds(); got != initial {
		t.Fatalf("Seconds()=%d, want %d", got, initial)
	}
	if got := dev.Now(); got.Unix() != int64(initial) || got.Location() != time.UTC {
		t.Fatalf("Now()=%v, want unix=%d UTC", got, initial)
	}

	target := time.Date(2026, time.November, 22, 12, 34, 56, 0, time.UTC)
	if err := dev.Set(target); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := dev.Now(); !got.Equal(target) {
		t.Fatalf("Now() after Set=%v, want %v", got, target)
	}

	// Out-of-range timestamps must be rejected without touching hardware.
	if err := dev.Set(time.Unix(-1, 0)); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("negative Set err=%v, want ErrOutOfRange", err)
	}
	if err := dev.Set(time.Unix(int64(math.MaxUint32)+1, 0)); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("overflow Set err=%v, want ErrOutOfRange", err)
	}
	if err := dev.SetAlarm(time.Unix(-5, 0)); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("negative SetAlarm err=%v, want ErrOutOfRange", err)
	}

	// Alarm programming, firing, and clearing.
	alarmAt := target.Add(30 * time.Second)
	if err := dev.SetAlarm(alarmAt); err != nil {
		t.Fatalf("SetAlarm: %v", err)
	}
	if hw.Read32(RegMR) != uint32(alarmAt.Unix()) {
		t.Fatalf("RTCMR=%d, want %d", hw.Read32(RegMR), alarmAt.Unix())
	}
	if hw.Read32(RegIMSC) != 1 {
		t.Fatal("SetAlarm did not unmask RTCIMSC")
	}
	if dev.AlarmPending() {
		t.Fatal("AlarmPending true before match")
	}
	hw.triggerAlarm()
	if !dev.AlarmPending() {
		t.Fatal("AlarmPending false after match")
	}
	dev.ClearAlarm()
	if dev.AlarmPending() || hw.Read32(RegIMSC) != 0 || hw.Read32(RegRIS) != 0 {
		t.Fatalf("ClearAlarm left state pending=%t imsc=%d ris=%d",
			dev.AlarmPending(), hw.Read32(RegIMSC), hw.Read32(RegRIS))
	}
}

func TestPL031SetReanchorsBoundClock(t *testing.T) {
	const initial = uint32(1795380000)
	hw := newFakePL031(initial)
	dev, err := OpenPL031(hw)
	if err != nil {
		t.Fatalf("OpenPL031: %v", err)
	}

	var mono atomic.Int64
	mono.Store(1_000_000_000)
	clock := NewClock(dev.Now(), mono.Load)
	dev.BindClock(clock)

	mono.Add(int64(5 * time.Second))
	if got, want := clock.Now(), time.Unix(int64(initial)+5, 0).UTC(); !got.Equal(want) {
		t.Fatalf("clock before Set=%v, want %v", got, want)
	}

	target := time.Date(2030, time.January, 2, 3, 4, 5, 900_000_000, time.FixedZone("offset", 3*60*60))
	if err := dev.Set(target); err != nil {
		t.Fatalf("Set: %v", err)
	}
	setEpoch := time.Unix(target.Unix(), 0).UTC()
	if got := clock.Now(); !got.Equal(setEpoch) {
		t.Fatalf("clock after Set=%v, want %v", got, setEpoch)
	}

	mono.Add(int64(250 * time.Millisecond))
	if got, want := clock.Now(), setEpoch.Add(250*time.Millisecond); !got.Equal(want) {
		t.Fatalf("clock after Set and monotonic advance=%v, want %v", got, want)
	}

	beforeInvalidSet := clock.Now()
	if err := dev.Set(time.Unix(int64(math.MaxUint32)+1, 0)); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("out-of-range Set err=%v, want ErrOutOfRange", err)
	}
	if got := clock.Now(); !got.Equal(beforeInvalidSet) {
		t.Fatalf("clock changed after rejected Set: got %v, want %v", got, beforeInvalidSet)
	}
}

func TestClockAnchoringSyncAndConcurrency(t *testing.T) {
	var mono atomic.Int64
	mono.Store(1_000_000_000) // 1s monotonic uptime

	base := time.Date(2026, time.November, 22, 20, 0, 0, 0, time.UTC)
	clk := NewClock(base, mono.Load)
	if got := clk.Now(); !got.Equal(base) {
		t.Fatalf("initial Now()=%v, want %v", got, base)
	}

	mono.Add(int64(250 * time.Millisecond))
	want := base.Add(250 * time.Millisecond)
	if got := clk.Now(); !got.Equal(want) {
		t.Fatalf("Now() +250ms = %v, want %v", got, want)
	}

	// Backward monotonic jitter clamps delta to 0 instead of regressing before base.
	mono.Store(500_000_000)
	if got := clk.Now(); !got.Equal(base) {
		t.Fatalf("clamped Now()=%v, want %v", got, base)
	}

	// Re-sync re-anchors both wall and monotonic references.
	mono.Store(5_000_000_000)
	synced := base.Add(10 * time.Second)
	clk.Sync(synced)
	mono.Add(int64(75 * time.Millisecond))
	if got := clk.Now(); !got.Equal(synced.Add(75 * time.Millisecond)) {
		t.Fatalf("synced Now()=%v, want %v", got, synced.Add(75*time.Millisecond))
	}

	// Default nil mono source advances monotonically.
	defaultClk := NewClock(base, nil)
	first := defaultClk.Now()
	time.Sleep(2 * time.Millisecond)
	second := defaultClk.Now()
	if second.Before(first) {
		t.Fatalf("default clock regressed: %v < %v", second, first)
	}

	// Concurrent readers + writers under -race.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if id == 0 && j%10 == 0 {
					clk.Sync(synced.Add(time.Duration(j) * time.Millisecond))
				}
				mono.Add(1000)
				_ = clk.Now()
			}
		}(i)
	}
	wg.Wait()
}

func TestUnsyncedClockTransitionsOnSync(t *testing.T) {
	var mono atomic.Int64
	mono.Store(10_000_000_000)

	clk := NewUnsyncedClock(mono.Load)
	if clk.Synced() {
		t.Fatal("unsynced clock reported Synced=true")
	}
	if _, ok := clk.WallTime(); ok {
		t.Fatal("unsynced clock returned WallTime ok=true")
	}

	mono.Add(int64(500 * time.Millisecond))
	epoch := time.Date(2026, time.November, 25, 8, 30, 0, 0, time.UTC)
	clk.Sync(epoch)
	if !clk.Synced() {
		t.Fatal("synced clock reported Synced=false")
	}
	mono.Add(int64(250 * time.Millisecond))
	got, ok := clk.WallTime()
	if !ok {
		t.Fatal("WallTime ok=false after Sync")
	}
	if want := epoch.Add(250 * time.Millisecond); !got.Equal(want) {
		t.Fatalf("WallTime=%v, want %v", got, want)
	}
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

func TestPL031AlarmIRQDispatchAndDisable(t *testing.T) {
	const initial = uint32(1795380000)
	const rtcSPI = uint32(34) // QEMU virt PL031 SPI 2 (INTID 34)
	hw := newFakePL031(initial)
	dev, err := OpenPL031(hw)
	if err != nil {
		t.Fatalf("OpenPL031: %v", err)
	}
	ctrl, acc := newFakeGIC(t)

	if err := dev.EnableIRQ(nil, rtcSPI, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil GIC err=%v, want ErrInvalidConfig", err)
	}
	if err := dev.EnableIRQ(ctrl, 16, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("non-SPI err=%v, want ErrInvalidConfig", err)
	}
	if err := dev.EnableIRQ(ctrl, ctrl.Lines(), nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("out-of-range SPI err=%v, want ErrInvalidConfig", err)
	}

	// DisableIRQ before EnableIRQ must be a safe no-op.
	dev.DisableIRQ(ctrl)

	var cbHits atomic.Int32
	if err := dev.EnableIRQ(ctrl, rtcSPI, func() { cbHits.Add(1) }); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}

	// Re-enabling replaces the previous handler cleanly and supports switching SPI IDs
	// as well as nil callbacks before restoring our test callback on rtcSPI.
	if err := dev.EnableIRQ(ctrl, rtcSPI+1, nil); err != nil {
		t.Fatalf("re-EnableIRQ nil cb: %v", err)
	}
	if err := dev.EnableIRQ(ctrl, rtcSPI, func() {
		cbHits.Add(1)
		// Re-arming the alarm inside the callback must not deadlock.
		_ = dev.SetAlarm(time.Unix(int64(initial)+120, 0))
	}); err != nil {
		t.Fatalf("re-EnableIRQ rtcSPI: %v", err)
	}

	// Spurious dispatch when RegMIS == 0 must not increment count or invoke callback.
	acc.mu.Lock()
	acc.pending = rtcSPI
	acc.mu.Unlock()
	ctrl.Dispatch()
	if dev.IRQCount() != 0 || cbHits.Load() != 0 {
		t.Fatalf("spurious IRQ counted: count=%d cb=%d", dev.IRQCount(), cbHits.Load())
	}

	// Armed alarm firing: clears RegICR (RTCRIS=0, AlarmPending=false), increments count,
	// runs callback which re-arms RTCMR while leaving RTCIMSC enabled.
	if err := dev.SetAlarm(time.Unix(int64(initial)+60, 0)); err != nil {
		t.Fatalf("SetAlarm: %v", err)
	}
	hw.triggerAlarm()
	acc.mu.Lock()
	acc.pending = rtcSPI
	acc.mu.Unlock()
	ctrl.Dispatch()
	if dev.IRQCount() != 1 || cbHits.Load() != 1 || dev.AlarmPending() || hw.Read32(RegRIS) != 0 {
		t.Fatalf("after 1st alarm: count=%d cb=%d pending=%t ris=%d",
			dev.IRQCount(), cbHits.Load(), dev.AlarmPending(), hw.Read32(RegRIS))
	}
	if got := hw.Read32(RegMR); got != initial+120 {
		t.Fatalf("callback re-arm RTCMR=%d, want %d", got, initial+120)
	}
	if hw.Read32(RegIMSC) != 1 {
		t.Fatal("RTCIMSC was masked across alarm dispatch")
	}

	// Second alarm fires on re-armed timer.
	hw.triggerAlarm()
	acc.mu.Lock()
	acc.pending = rtcSPI
	acc.mu.Unlock()
	ctrl.Dispatch()
	if dev.IRQCount() != 2 || cbHits.Load() != 2 || dev.AlarmPending() {
		t.Fatalf("after 2nd alarm: count=%d cb=%d pending=%t",
			dev.IRQCount(), cbHits.Load(), dev.AlarmPending())
	}

	// Nil callback still clears ICR and increments IRQCount.
	if err := dev.EnableIRQ(ctrl, rtcSPI, nil); err != nil {
		t.Fatalf("EnableIRQ nil callback: %v", err)
	}
	hw.triggerAlarm()
	acc.mu.Lock()
	acc.pending = rtcSPI
	acc.mu.Unlock()
	ctrl.Dispatch()
	if dev.IRQCount() != 3 || cbHits.Load() != 2 || dev.AlarmPending() {
		t.Fatalf("after nil-cb alarm: count=%d cb=%d pending=%t",
			dev.IRQCount(), cbHits.Load(), dev.AlarmPending())
	}

	// DisableIRQ prevents stale/late handler execution from touching hardware.
	dev.DisableIRQ(ctrl)
	hw.triggerAlarm()
	dev.handleIRQ()
	if dev.IRQCount() != 3 || !dev.AlarmPending() {
		t.Fatal("disabled handleIRQ touched hardware")
	}

	// Verify DisableIRQ waits for an in-flight alarm callback under -race.
	hw.Write32(RegICR, irqBit)
	entered := make(chan struct{})
	release := make(chan struct{})
	var cbFinished atomic.Bool
	if err := dev.EnableIRQ(ctrl, rtcSPI, func() {
		close(entered)
		<-release
		cbFinished.Store(true)
	}); err != nil {
		t.Fatalf("EnableIRQ blocking test: %v", err)
	}
	hw.triggerAlarm()
	acc.mu.Lock()
	acc.pending = rtcSPI
	acc.mu.Unlock()
	go ctrl.Dispatch()

	<-entered
	disableDone := make(chan struct{})
	go func() {
		dev.DisableIRQ(ctrl)
		close(disableDone)
	}()

	select {
	case <-disableDone:
		t.Fatal("DisableIRQ returned while alarm callback was still running")
	case <-time.After(15 * time.Millisecond):
	}

	close(release)
	<-disableDone
	if !cbFinished.Load() {
		t.Fatal("DisableIRQ returned before alarm callback finished")
	}
}
