// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"testing"
	"time"
)

func TestDWC2IRQSOFWakeupAndLatencyTelemetry(t *testing.T) {
	ctrl, ghw := newMockGIC(t)
	hw := newSyncController()
	want1 := []byte{0, 0, 4, 0, 0, 0, 0, 0}
	want2 := []byte{0, 0, 5, 0, 0, 0, 0, 0}
	hw.base.reports = [][]byte{want1, want2}

	host, err := Open(hw, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	defer host.DisableIRQ()

	// Arm and complete first report via IRQ.
	if r, err := host.PollKeyboard(); err != nil || r != nil {
		t.Fatalf("arm 1: r=%x err=%v", r, err)
	}
	if hw.reg(gintmask) != irqMask {
		t.Fatalf("armed transfer left GINTMSK=%#x, want %#x (no SOF spam)", hw.reg(gintmask), irqMask)
	}
	ghw.fire(ctrl, testUSBIRQ)
	select {
	case <-host.Ready():
	default:
		t.Fatal("expected Ready signal after RX IRQ")
	}
	hw.mu.Lock()
	hw.base.tick = hw.base.tick.Add(250 * time.Microsecond)
	hw.mu.Unlock()
	var got1 []byte
	for range 16 {
		got1, err = host.PollKeyboard()
		if err != nil {
			t.Fatal(err)
		}
		if got1 != nil {
			break
		}
	}
	if !bytes.Equal(got1, want1) {
		t.Fatalf("report1=%x, want %x", got1, want1)
	}

	// Between intervals (!pending.active), SOF must be unmasked so the next USB
	// frame wakes Ready() without waiting for a 50ms fallback timer.
	if hw.reg(gintmask) != irqMaskSOF {
		t.Fatalf("between intervals GINTMSK=%#x, want %#x", hw.reg(gintmask), irqMaskSOF)
	}
	stats := host.Latency()
	if stats.Samples != 1 || stats.MinNS < int64(250*time.Microsecond) || stats.AvgNS() != stats.LastNS {
		t.Fatalf("unexpected latency stats after first report: %+v", stats)
	}

	// Simulate SOF firing once interval elapses; handler wakes Ready() and
	// PollKeyboard immediately re-arms the channel and masks SOF back off.
	hw.mu.Lock()
	hw.base.tick = hw.base.tick.Add(host.interval)
	hw.mu.Unlock()
	hw.setReg(gint, hw.reg(gint)|gintSOF)
	ghw.fire(ctrl, testUSBIRQ)
	select {
	case <-host.Ready():
	default:
		t.Fatal("SOF IRQ did not wake Ready()")
	}
	if r, err := host.PollKeyboard(); err != nil || r != nil {
		t.Fatalf("re-arm after SOF: r=%x err=%v", r, err)
	}
	if hw.reg(gintmask) != irqMask {
		t.Fatalf("re-armed transfer left GINTMSK=%#x, want %#x", hw.reg(gintmask), irqMask)
	}
	ghw.fire(ctrl, testUSBIRQ)
	select {
	case <-host.Ready():
	default:
		t.Fatal("second RX IRQ did not wake Ready()")
	}
	hw.mu.Lock()
	hw.base.tick = hw.base.tick.Add(100 * time.Microsecond)
	hw.mu.Unlock()
	var got2 []byte
	for range 16 {
		got2, err = host.PollKeyboard()
		if err != nil {
			t.Fatal(err)
		}
		if got2 != nil {
			break
		}
	}
	if !bytes.Equal(got2, want2) {
		t.Fatalf("report2=%x, want %x", got2, want2)
	}
	stats = host.Latency()
	if stats.Samples != 2 || stats.MinNS < int64(100*time.Microsecond) || stats.MaxNS < int64(250*time.Microsecond) {
		t.Fatalf("unexpected latency stats after second report: %+v", stats)
	}
	host.ResetLatency()
	if s := host.Latency(); s.Samples != 0 || s.MinNS != 0 || s.MaxNS != 0 {
		t.Fatalf("ResetLatency did not clear stats: %+v", s)
	}
}

type zeroAllocRegs struct {
	regs   [0x400]uint32
	rx     [4]uint32
	rxHead int
	rxLen  int
	words  [2]uint32
	wHead  int
	wLen   int
	tick   time.Time
	toggle uint32
}

func (z *zeroAllocRegs) Now() time.Time { return z.tick }
func (z *zeroAllocRegs) Read32(reg uint32) uint32 {
	switch reg {
	case gint:
		v := z.regs[reg/4]
		if z.rxHead < z.rxLen {
			v |= 1 << 4
		}
		return v
	case rxpeek:
		if z.rxHead < z.rxLen {
			return z.rx[z.rxHead]
		}
		return 0
	case rxpop:
		if z.rxHead < z.rxLen {
			v := z.rx[z.rxHead]
			z.rxHead++
			return v
		}
		return 0
	case fifo:
		if z.wHead < z.wLen {
			v := z.words[z.wHead]
			z.wHead++
			return v
		}
		return 0
	default:
		if int(reg/4) < len(z.regs) {
			return z.regs[reg/4]
		}
		return 0
	}
}
func (z *zeroAllocRegs) Write32(reg, value uint32) {
	switch reg {
	case gint:
		z.regs[reg/4] &^= value &^ uint32(1|1<<4)
	case hcint:
		z.regs[reg/4] &^= value
	case hcchar:
		z.regs[reg/4] = value
		if value&channelDisable != 0 {
			z.regs[reg/4] &^= channelEnable | channelDisable
			z.regs[hcint/4] |= 1 << 1
			return
		}
		if value&channelEnable != 0 {
			z.rx[0] = 2<<17 | z.toggle<<15 | 8<<4
			z.rx[1] = 3 << 17
			z.rxHead, z.rxLen = 0, 2
			z.words[0] = 0x00040000
			z.words[1] = 0
			z.wHead, z.wLen = 0, 2
			z.toggle ^= 2
			z.regs[hcint/4] |= 1 | 1<<1
			z.regs[reg/4] &^= channelEnable
		}
	default:
		if int(reg/4) < len(z.regs) {
			z.regs[reg/4] = value
		}
	}
}

func TestDWC2PollKeyboardZeroAllocs(t *testing.T) {
	// Enumerate with standard mock, then swap onto a zero-alloc register bank
	// to prove PollKeyboard itself performs zero heap allocations per report.
	hw := newController()
	host, err := Open(hw, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	z := &zeroAllocRegs{tick: hw.tick, toggle: host.toggle}
	for reg, val := range hw.regs {
		if int(reg/4) < len(z.regs) {
			z.regs[reg/4] = val
		}
	}
	host.io = z

	var buf [8]byte
	allocs := testing.AllocsPerRun(50, func() {
		z.tick = z.tick.Add(host.interval)
		for range 8 {
			n, err := host.PollKeyboardInto(buf[:])
			if err != nil {
				t.Fatalf("PollKeyboardInto: %v", err)
			}
			if n != 0 {
				if n != 8 || buf[2] != 4 {
					t.Fatalf("unexpected report: n=%d buf=%x", n, buf[:n])
				}
				break
			}
		}
	})
	if allocs != 0 {
		t.Fatalf("PollKeyboardInto allocated %.2f objects/report, want 0", allocs)
	}
}
