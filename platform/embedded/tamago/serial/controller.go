// SPDX-License-Identifier: GPL-3.0-or-later

package serial

import (
	"errors"
	"sync"
	"sync/atomic"
)

// UART abstracts a byte-oriented UART peripheral for interrupt-driven RX/TX
// buffering, polling fallback, and line-status error accounting.
type UART interface {
	ReadRX() (b byte, hasByte bool, errs LineError)
	WriteTX(b byte) bool
	EnableRXIRQ(enable bool)
	EnableTXIRQ(enable bool)
	ClearIRQ()
}

// Controller manages an ISR-safe RX/TX ring pair and line-error telemetry over
// an injected UART peripheral.
type Controller struct {
	uart      UART
	rx        Ring
	rxReady   chan struct{}
	rxBytes   atomic.Uint64
	rxErrors  ErrorCounters
	irqCount  atomic.Uint64
	irqActive atomic.Bool

	txMu    sync.Mutex
	tx      Ring
	txBytes atomic.Uint64
}

// NewController wraps uart in a bounded RX/TX Controller.
func NewController(uart UART) (*Controller, error) {
	if uart == nil {
		return nil, errors.New("serial: nil UART peripheral")
	}
	return &Controller{
		uart:    uart,
		rxReady: make(chan struct{}, 1),
	}, nil
}

// EnableIRQ resets buffered state and telemetry, drains any pending RX FIFO
// bytes into the RX ring, and enables RX interrupts.
func (c *Controller) EnableIRQ() {
	c.txMu.Lock()
	c.uart.EnableRXIRQ(false)
	c.uart.EnableTXIRQ(false)
	c.tx.Reset()
	c.txBytes.Store(0)
	c.txMu.Unlock()

	c.rx.Reset()
	c.rxErrors.Reset()
	c.rxBytes.Store(0)
	c.irqCount.Store(0)
	select {
	case <-c.rxReady:
	default:
	}

	c.irqActive.Store(true)
	c.DrainRX()
	c.uart.ClearIRQ()
	c.uart.EnableRXIRQ(true)
}

// DisableIRQ masks UART RX/TX interrupts and returns Read to polling mode.
func (c *Controller) DisableIRQ() {
	c.txMu.Lock()
	c.uart.EnableRXIRQ(false)
	c.uart.EnableTXIRQ(false)
	c.txMu.Unlock()
	c.irqActive.Store(false)
}

// Ready returns a channel notified whenever new RX bytes enter the RX ring.
func (c *Controller) Ready() <-chan struct{} { return c.rxReady }

// Read returns the next clean RX byte without blocking, popping from the RX
// ring when IRQs are enabled or polling the UART directly otherwise. Any line
// errors encountered are recorded in Stats and the corrupt byte is discarded.
func (c *Controller) Read() (byte, bool) {
	if c.irqActive.Load() {
		return c.rx.Pop()
	}
	for i := 0; i < ringSize; i++ {
		b, hasByte, errs := c.uart.ReadRX()
		c.rxErrors.Record(errs)
		if !hasByte {
			if errs == 0 {
				return 0, false
			}
			continue
		}
		if errs != 0 {
			continue
		}
		c.rxBytes.Add(1)
		return b, true
	}
	return 0, false
}

// SendByte transmits b immediately if the TX ring is empty and hardware FIFO
// accepts it, or queues it into the bounded TX ring. Returns false if the TX
// ring is full (incrementing Stats().TXDropped).
func (c *Controller) SendByte(b byte) bool {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	return c.writeByteLocked(b)
}

// Write queues p into the hardware TX FIFO and/or bounded TX ring, returning
// the number of accepted bytes and the number of bytes dropped due to overflow.
func (c *Controller) Write(p []byte) (written int, dropped uint32) {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	for _, b := range p {
		if c.writeByteLocked(b) {
			written++
		} else {
			dropped++
		}
	}
	return written, dropped
}

func (c *Controller) writeByteLocked(b byte) bool {
	if c.tx.Len() == 0 && c.uart.WriteTX(b) {
		c.txBytes.Add(1)
		return true
	}
	if !c.tx.Push(b) {
		return false
	}
	if c.irqActive.Load() {
		c.uart.EnableTXIRQ(true)
	}
	return true
}

// DrainRX drains up to one ring-capacity of RX bytes from hardware, recording
// line-status errors and waking Ready() if any clean byte was queued.
func (c *Controller) DrainRX() int {
	pushed := 0
	for i := 0; i < ringSize; i++ {
		b, hasByte, errs := c.uart.ReadRX()
		c.rxErrors.Record(errs)
		if !hasByte {
			if errs == 0 {
				break
			}
			continue
		}
		if errs != 0 {
			continue
		}
		c.rxBytes.Add(1)
		if c.rx.Push(b) {
			pushed++
		}
	}
	if pushed > 0 {
		select {
		case c.rxReady <- struct{}{}:
		default:
		}
	}
	return pushed
}

// DrainTX flushes queued TX ring bytes into the UART hardware FIFO until empty
// or hardware reports FIFO full, disabling TX interrupts once empty.
func (c *Controller) DrainTX() int {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	sent := 0
	for {
		b, ok := c.tx.Peek()
		if !ok {
			if c.irqActive.Load() {
				c.uart.EnableTXIRQ(false)
			}
			break
		}
		if !c.uart.WriteTX(b) {
			break
		}
		c.tx.Pop()
		c.txBytes.Add(1)
		sent++
	}
	return sent
}

// HandleIRQ services one UART interrupt: clears peripheral IRQ status, drains
// incoming RX bytes, and pushes queued TX bytes.
func (c *Controller) HandleIRQ() {
	c.irqCount.Add(1)
	c.uart.ClearIRQ()
	c.DrainRX()
	c.DrainTX()
}

// Stats returns a snapshot of all RX, TX, interrupt, and line-error counters.
func (c *Controller) Stats() Stats {
	overrun, parity, framing, brk := c.rxErrors.Snapshot()
	return Stats{
		IRQCount:  c.irqCount.Load(),
		RXBytes:   c.rxBytes.Load(),
		RXDropped: c.rx.Dropped(),
		TXBytes:   c.txBytes.Load(),
		TXDropped: c.tx.Dropped(),
		Overrun:   overrun,
		Parity:    parity,
		Framing:   framing,
		Break:     brk,
	}
}
