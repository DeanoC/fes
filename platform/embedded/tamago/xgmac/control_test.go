// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

type stopRegisters struct {
	*hardware
	allowed          bool
	busy             string
	txReads, rxReads int
	events           []string
	cancel           context.CancelFunc
}

func (r *stopRegisters) Read32(offset uint32) uint32 {
	if !r.allowed {
		panic("MMIO without platform grant")
	}
	if offset == txDebug {
		if r.busy == "cancel-empty-tx" {
			r.cancel()
			return 0
		}
		r.txReads++
		if r.txReads == 1 {
			r.events = append(r.events, "tx-fifo")
		}
		if r.busy == "cancel-tx" {
			r.cancel()
		}
		if r.busy == "tx" || r.busy == "cancel-tx" || r.txReads < 3 {
			return 1<<1 | 1<<4
		}
	}
	if offset == rxDebug {
		if r.busy == "cancel-empty-rx" {
			r.cancel()
			return 0
		}
		r.rxReads++
		if r.rxReads == 1 {
			r.events = append(r.events, "rx-fifo")
		}
		if r.busy == "cancel-rx" {
			r.cancel()
		}
		if r.busy == "rx" || r.busy == "cancel-rx" || r.rxReads < 3 {
			return 1<<16 | 1<<4
		}
	}
	return r.hardware.Read32(offset)
}
func (r *stopRegisters) Write32(offset, value uint32) {
	if !r.allowed {
		panic("MMIO write without platform grant")
	}
	if value&1 == 0 {
		for _, v := range []struct {
			offset uint32
			name   string
		}{{txControl, "tx-dma"}, {txConfig, "tx-mac"}, {rxConfig, "rx-mac"}, {rxControl, "rx-dma"}} {
			if offset == v.offset {
				r.events = append(r.events, v.name)
			}
		}
		if r.busy == "readback" && offset == rxControl {
			return
		}
	}
	r.hardware.Write32(offset, value)
	if r.busy == "cancel-mac" && offset == txConfig && value&1 == 0 {
		r.cancel()
	}
}

type stopMemory struct {
	*memory
	registers *stopRegisters
	base      uint64
}

func (m *stopMemory) Base() uint64 { return m.base }
func (m *stopMemory) Read32(o uint32) uint32 {
	if !m.registers.allowed {
		panic("DMA memory read without grant")
	}
	return m.memory.Read32(o)
}
func (m *stopMemory) Write32(o, v uint32) {
	if !m.registers.allowed {
		panic("DMA memory write without grant")
	}
	m.memory.Write32(o, v)
}
func (m *stopMemory) Read(o uint32, b []byte) {
	if !m.registers.allowed {
		panic("DMA read without grant")
	}
	m.memory.Read(o, b)
}
func (m *stopMemory) Write(o uint32, b []byte) {
	if !m.registers.allowed {
		panic("DMA write without grant")
	}
	m.memory.Write(o, b)
}
func (m *stopMemory) Barrier() {
	if !m.registers.allowed {
		panic("DMA barrier without grant")
	}
	m.memory.Barrier()
}

func controlDevice(t *testing.T) (*Device, *stopRegisters, *stopMemory) {
	t.Helper()
	h := newHardware()
	d := open(t, h)
	r := &stopRegisters{hardware: h, allowed: true}
	m := &stopMemory{memory: h.mem, registers: r, base: h.mem.Base()}
	d.io, d.memory = r, m
	return d, r, m
}

func TestControlledStopRestartPreservesPinnedWindowAndOrdersHardware(t *testing.T) {
	d, r, m := controlDevice(t)
	stops, starts := 0, 0
	control, err := d.BindControl(network.DMAControl{
		Stop: func(context.Context) error {
			stops++
			r.events = append(r.events, "platform-stop")
			if d.memory.Read32(rxRing+12)&own == 0 {
				t.Fatal("RX ownership invented as returned")
			}
			r.allowed = false
			return nil
		}, Start: func(context.Context) error { starts++; r.allowed = true; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tx-dma", "tx-fifo", "tx-mac", "rx-mac", "rx-fifo", "rx-dma", "platform-stop"}
	original := &m.data[0]
	m.data[len(m.data)-1] = 0xa5
	for cycle := 0; cycle < 3; cycle++ {
		r.events = nil
		r.txReads, r.rxReads = 0, 0
		if err = control.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.events, want) {
			t.Fatal("stop order", r.events)
		}
		before := bytes.Clone(m.data)
		accesses, writes := r.accesses, m.writes
		if err = control.Stop(context.Background()); err != nil || stops != cycle+1 {
			t.Fatal("duplicate stop", err)
		}
		if _, err = d.Receive(make([]byte, maxFrame)); !errors.Is(err, network.ErrPaused) {
			t.Fatal(err)
		}
		if err = d.Transmit(make([]byte, 14)); !errors.Is(err, network.ErrPaused) {
			t.Fatal(err)
		}
		if accesses != r.accesses || writes != m.writes || !bytes.Equal(before, m.data) {
			t.Fatal("contained driver touched MMIO or DMA memory")
		}
		if err = control.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if starts != cycle+1 || d.Err() != nil || d.paused || d.rx != 0 || d.tx != 0 || &m.data[0] != original || m.data[len(m.data)-1] != 0xa5 {
			t.Fatal("restart replaced memory or failed to rearm")
		}
		if err = control.Start(context.Background()); !errors.Is(err, network.ErrControl) || starts != cycle+1 {
			t.Fatal("unpaired restart", err)
		}
		if err = d.Transmit(make([]byte, 14)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestControlledStopFailuresNeverGrantContainmentOrRetry(t *testing.T) {
	for _, fault := range []string{"tx", "rx", "readback", "cancel-tx", "cancel-rx", "cancel-empty-tx", "cancel-empty-rx", "cancel-mac", "platform-stop", "cancel-stop", "frozen"} {
		t.Run(fault, func(t *testing.T) {
			d, r, m := controlDevice(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r.busy = fault
			r.cancel = cancel
			if fault == "frozen" {
				r.frozen = true
				r.busy = "tx"
			}
			stops, starts := 0, 0
			control, err := d.BindControl(network.DMAControl{Stop: func(context.Context) error {
				stops++
				r.allowed = false
				if fault == "cancel-stop" {
					cancel()
					return nil
				}
				return errors.New("fence reply lost")
			}, Start: func(context.Context) error { starts++; return nil }})
			if err != nil {
				t.Fatal(err)
			}
			if err = control.Stop(ctx); err == nil || d.Err() == nil {
				t.Fatal("failed stop admitted", err)
			}
			if fault != "platform-stop" && fault != "cancel-stop" && stops != 0 {
				t.Fatal("FIFO failure called platform fence")
			}
			if fault == "cancel-empty-tx" && len(r.events) != 1 {
				t.Fatal("cancelled FIFO read continued MAC stop", r.events)
			}
			if fault == "cancel-empty-rx" && r.regs[rxControl]&1 == 0 {
				t.Fatal("cancelled RX read continued DMA stop")
			}
			if fault == "cancel-mac" && r.regs[rxConfig]&1 == 0 {
				t.Fatal("cancelled MAC write continued RX stop")
			}
			accesses, writes, called := r.accesses, m.writes, stops
			if control.Stop(context.Background()) == nil || control.Start(context.Background()) == nil {
				t.Fatal("failed control retried")
			}
			d.Transmit(make([]byte, 14))
			d.Receive(make([]byte, maxFrame))
			if accesses != r.accesses || writes != m.writes || stops != called || starts != 0 {
				t.Fatal("retired control touched device")
			}
		})
	}
}

func TestControlledRestartRequiresNewGrantAndOriginalPHYWindow(t *testing.T) {
	for _, fault := range []string{"platform-start", "cancel-start", "PHY", "window-before", "window-after"} {
		t.Run(fault, func(t *testing.T) {
			d, r, m := controlDevice(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			starts := 0
			control, err := d.BindControl(network.DMAControl{Stop: func(context.Context) error { r.allowed = false; return nil }, Start: func(context.Context) error {
				starts++
				if fault == "platform-start" {
					return errors.New("grant lost")
				}
				if fault == "cancel-start" {
					cancel()
					return nil
				}
				if fault == "window-after" {
					m.base += 64
					return nil
				}
				r.allowed = true
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err = control.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if fault == "PHY" {
				r.phy[3] ^= 1
			}
			if fault == "window-before" {
				m.base += 64
			}
			if err = control.Start(ctx); err == nil || d.Err() == nil {
				t.Fatal("changed restart admitted", err)
			}
			accesses, writes, calls := r.accesses, m.writes, starts
			control.Start(context.Background())
			control.Stop(context.Background())
			d.Receive(make([]byte, maxFrame))
			d.Transmit(make([]byte, 14))
			if accesses != r.accesses || writes != m.writes || calls != starts {
				t.Fatal("uncertain grant reused")
			}
			if fault == "window-before" && starts != 0 {
				t.Fatal("changed memory obtained platform grant")
			}
		})
	}
}

func TestControlledBindingRequiresCompleteExclusiveCallbacks(t *testing.T) {
	d, r, _ := controlDevice(t)
	valid := network.DMAControl{Stop: func(context.Context) error { return nil }, Start: func(context.Context) error { return nil }}
	for _, bad := range []network.DMAControl{{}, {Stop: valid.Stop}, {Start: valid.Start}} {
		if _, err := d.BindControl(bad); err == nil || d.controlBound {
			t.Fatal("incomplete control bound")
		}
	}
	before := r.accesses
	c, err := d.BindControl(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.BindControl(valid); err == nil || r.accesses != before {
		t.Fatal("duplicate binding or constructor MMIO")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(c.Stop(ctx), context.Canceled) || d.paused || r.accesses != before {
		t.Fatal("pre-cancellation touched hardware")
	}
	if c.Stop(nil) == nil || c.Start(nil) == nil {
		t.Fatal("nil context")
	}
	if c.Start(context.Background()) == nil {
		t.Fatal("restart without fence")
	}
}

func TestControlledConcurrentAttemptPoisonsPendingCompletion(t *testing.T) {
	d, r, _ := controlDevice(t)
	entered, release := make(chan struct{}), make(chan struct{})
	c, err := d.BindControl(network.DMAControl{Stop: func(context.Context) error { close(entered); <-release; r.allowed = false; return nil }, Start: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Stop(context.Background()) }()
	<-entered
	if !errors.Is(c.Start(context.Background()), network.ErrBusy) {
		t.Fatal("overlapping control admitted")
	}
	close(release)
	select {
	case err = <-done:
		if err == nil || d.Err() == nil {
			t.Fatal("pending operation ignored retirement")
		}
	case <-time.After(time.Second):
		t.Fatal("pending operation did not return")
	}
}
