// SPDX-License-Identifier: GPL-3.0-or-later

package rtc

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
