// SPDX-License-Identifier: GPL-3.0-or-later

package gic

import (
	"math"
	"testing"
)

type fakeIdleAccess struct {
	freq       uint64
	now        int64
	irqEnabled bool

	armedTicks []uint32
	disarms    int
	wfis       int
	events     []string
}

func (f *fakeIdleAccess) CounterFreq() uint64 { return f.freq }
func (f *fakeIdleAccess) NowNanos() int64     { return f.now }
func (f *fakeIdleAccess) IRQEnabled() bool    { return f.irqEnabled }

func (f *fakeIdleAccess) MaskIRQ() bool {
	was := f.irqEnabled
	f.irqEnabled = false
	f.events = append(f.events, "mask")
	return was
}

func (f *fakeIdleAccess) UnmaskIRQ() {
	f.irqEnabled = true
	f.events = append(f.events, "unmask")
}

func (f *fakeIdleAccess) ArmTimer(ticks uint32) {
	f.armedTicks = append(f.armedTicks, ticks)
	f.events = append(f.events, "arm")
}

func (f *fakeIdleAccess) DisarmTimer() {
	f.disarms++
	f.events = append(f.events, "disarm")
}

func (f *fakeIdleAccess) WFI() {
	f.wfis++
	f.events = append(f.events, "wfi")
}

func TestDeltaToTicks(t *testing.T) {
	cases := []struct {
		name  string
		delta int64
		freq  uint64
		want  uint32
	}{
		{"zero delta", 0, 400_000_000, 0},
		{"negative delta", -5, 400_000_000, 0},
		{"zero freq", 1_000_000, 0, 0},
		{"1ns rounds up to 1 tick", 1, 62_500_000, 1},
		{"1ms at 62.5MHz (QEMU)", 1_000_000, 62_500_000, 62_500},
		{"10ms at 400MHz (Agilex 5)", 10_000_000, 400_000_000, 4_000_000},
		{"100ns at 62.5MHz rounds up from 6.25 to 7", 100, 62_500_000, 7},
		{"5s at 400MHz fits signed 32-bit", 5_000_000_000, 400_000_000, 2_000_000_000},
		{"10s at 400MHz clamps to MaxInt32", 10_000_000_000, 400_000_000, math.MaxInt32},
		{"near MaxInt64 clamps safely", math.MaxInt64 - 1, 400_000_000, math.MaxInt32},
		{"huge synthetic freq clamps safely", 1_000, math.MaxUint64, math.MaxInt32},
	}
	for _, tc := range cases {
		if got := deltaToTicks(tc.delta, tc.freq); got != tc.want {
			t.Errorf("%s: deltaToTicks(%d, %d) = %d, want %d", tc.name, tc.delta, tc.freq, got, tc.want)
		}
	}
}

func TestIdleStepTimedWithIRQEnabled(t *testing.T) {
	var g Controller
	hw := &fakeIdleAccess{freq: 400_000_000, now: 1_000_000, irqEnabled: true}
	g.SetIdleAccess(hw)

	g.IdleStep(11_000_000) // 10ms ahead

	if g.IdleWakeups() != 1 || hw.wfis != 1 || hw.disarms != 1 {
		t.Fatalf("wakeups=%d wfis=%d disarms=%d, want all 1", g.IdleWakeups(), hw.wfis, hw.disarms)
	}
	if len(hw.armedTicks) != 1 || hw.armedTicks[0] != 4_000_000 {
		t.Fatalf("armedTicks = %v, want [4000000]", hw.armedTicks)
	}
	if !hw.irqEnabled {
		t.Fatal("IRQ was not restored to enabled after WFI")
	}
	want := []string{"mask", "arm", "wfi", "disarm", "unmask"}
	if len(hw.events) != len(want) {
		t.Fatalf("events = %v, want %v", hw.events, want)
	}
	for i := range want {
		if hw.events[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q", i, hw.events[i], want[i])
		}
	}
}

func TestIdleStepTimedWithIRQMaskedStaysMasked(t *testing.T) {
	var g Controller
	hw := &fakeIdleAccess{freq: 62_500_000, now: 500, irqEnabled: false}
	g.SetIdleAccess(hw)

	g.IdleStep(1_000_500) // 1ms ahead

	if hw.irqEnabled {
		t.Fatal("IRQ should remain masked when masked on entry")
	}
	want := []string{"mask", "arm", "wfi", "disarm"}
	if len(hw.events) != len(want) {
		t.Fatalf("events = %v, want %v", hw.events, want)
	}
	for i := range want {
		if hw.events[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q", i, hw.events[i], want[i])
		}
	}
}

func TestIdleStepEdgeCases(t *testing.T) {
	var g Controller
	// Nil IdleAccess is a safe no-op.
	g.IdleStep(1000)

	hw := &fakeIdleAccess{freq: 400_000_000, now: 1000, irqEnabled: false}
	g.SetIdleAccess(hw)

	// Already expired deadline does not arm or WFI.
	g.IdleStep(1000)
	g.IdleStep(500)
	// MaxInt64 with IRQs masked must not hang in WFI.
	g.IdleStep(math.MaxInt64)
	if hw.wfis != 0 || len(hw.armedTicks) != 0 {
		t.Fatalf("unexpected WFI/arm on expired or masked-indefinite idle: wfis=%d arms=%v", hw.wfis, hw.armedTicks)
	}

	// MaxInt64 with IRQs enabled executes WFI without arming the timer.
	hw.irqEnabled = true
	g.IdleStep(math.MaxInt64)
	if hw.wfis != 1 || len(hw.armedTicks) != 0 || g.IdleWakeups() != 1 {
		t.Fatalf("indefinite WFI state unexpected: wfis=%d arms=%v wakeups=%d", hw.wfis, hw.armedTicks, g.IdleWakeups())
	}

	// Zero counter frequency does not arm or WFI.
	hw.freq = 0
	g.IdleStep(2000)
	if hw.wfis != 1 {
		t.Fatalf("zero counter freq should not WFI: wfis=%d", hw.wfis)
	}
}
