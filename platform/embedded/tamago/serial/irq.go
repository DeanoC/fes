//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package serial

import (
	"errors"
	"sync/atomic"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

var (
	rxRing    Ring
	rxReady   = make(chan struct{}, 1)
	irqActive atomic.Bool
	irqCount  atomic.Uint64
)

// Read returns the next received console byte without blocking.
// When EnableIRQ is active it pops from the interrupt-fed ring buffer;
// otherwise it polls the UART RX register directly.
func Read() (byte, bool) {
	if irqActive.Load() {
		return rxRing.Pop()
	}
	return readRaw()
}

// Ready returns a channel signalled (non-blocking, buffered 1) whenever an RX
// interrupt buffers new bytes. Callers can select on Ready() to wake directly
// from WFI instead of waiting for a polling timer.
func Ready() <-chan struct{} { return rxReady }

// IRQCount returns the number of console UART RX interrupts handled since EnableIRQ.
func IRQCount() uint64 { return irqCount.Load() }

// Dropped returns the number of RX bytes dropped because the ring buffer was full.
func Dropped() uint32 { return rxRing.Dropped() }

// EnableIRQ routes the console UART's RX SPI through g into a fixed 256-byte
// ring buffer. Call after g.Init(); g.Serve must run to dispatch interrupts.
func EnableIRQ(g *gic.Controller) error {
	if g == nil {
		return errors.New("serial: nil GIC controller")
	}
	id, err := consoleIRQ()
	if err != nil {
		return err
	}
	if id >= g.Lines() {
		return gic.ErrInvalidConfig
	}

	disableHWIRQ()
	g.Disable(id)
	rxRing.Reset()
	irqCount.Store(0)
	select {
	case <-rxReady:
	default:
	}

	g.Register(id, handleRXIRQ)
	irqActive.Store(true)
	drainFIFO()
	clearHWIRQ()
	g.Enable(id)
	enableHWIRQ()
	return nil
}

// DisableIRQ masks the console UART's RX interrupt in both the peripheral and GIC
// and returns Read() to direct hardware polling.
func DisableIRQ(g *gic.Controller) {
	disableHWIRQ()
	if g != nil {
		if id, err := consoleIRQ(); err == nil {
			g.Disable(id)
			g.Register(id, nil)
		}
	}
	irqActive.Store(false)
}

func handleRXIRQ() {
	irqCount.Add(1)
	clearHWIRQ()
	drainFIFO()
}

func drainFIFO() {
	got := false
	for {
		b, ok := readRaw()
		if !ok {
			break
		}
		rxRing.Push(b)
		got = true
	}
	if got {
		select {
		case rxReady <- struct{}{}:
		default:
		}
	}
}
