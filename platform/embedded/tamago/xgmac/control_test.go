// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

type stopRegisters struct {
	*hardware
	allowed                  bool
	busy                     string
	txReads, rxReads         int
	txAcks, rxAcks, rxDrains int
	events                   []string
	cancel                   context.CancelFunc
}

func (r *stopRegisters) Read32(offset uint32) uint32 {
	if !r.allowed {
		panic("MMIO without platform grant")
	}
	if offset == channelStatus && r.regs[txControl]&1 == 0 {
		v := r.hardware.Read32(offset)
		r.txAcks++
		if r.txAcks == 1 {
			r.events = append(r.events, "tx-ack")
		}
		if r.busy == "cancel-tx-ack" {
			r.cancel()
		}
		if r.busy == "no-tx-ack" || r.txAcks < 3 {
			v &^= txStopped
		}
		if r.busy == "fault-tx-ack" {
			v |= 1 << 12
		}
		if r.busy == "early-rx-ack" {
			v |= rxStopped
		}
		if r.regs[rxControl]&1 == 0 {
			r.rxAcks++
			if r.rxAcks == 1 {
				r.events = append(r.events, "rx-ack")
			}
			if r.busy == "cancel-rx-ack" {
				r.cancel()
			}
			if r.busy == "no-rx-ack" || r.rxAcks < 3 {
				v &^= rxStopped
			}
			if r.busy == "fault-rx-ack" {
				v |= 1 << 12
			}
			if r.busy == "lost-tx-ack" {
				v &^= txStopped
			}
		}
		return v
	}
	if offset == txDebug {
		if r.busy == "tx-reserved" {
			return 1<<1 | 1<<3
		}
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
		if r.regs[rxConfig]&1 == 0 {
			r.rxDrains++
			if r.rxDrains == 1 {
				r.events = append(r.events, "rx-drain")
			}
			if r.busy == "cancel-post-rx" {
				r.cancel()
				return 0
			}
			if r.busy == "rx-drain" || r.rxDrains < 3 {
				return 1<<16 | 1<<4
			}
			return r.hardware.Read32(offset)
		}
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
			r.regs[channelStatus] |= rxStopped
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
	want := []string{"tx-dma", "tx-ack", "tx-fifo", "rx-fifo", "tx-mac", "rx-mac", "rx-drain", "rx-dma", "rx-ack", "platform-stop"}
	original := &m.data[0]
	m.data[len(m.data)-1] = 0xa5
	for cycle := 0; cycle < 3; cycle++ {
		r.events = nil
		r.txReads, r.rxReads = 0, 0
		r.txAcks, r.rxAcks, r.rxDrains = 0, 0, 0
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
	for _, fault := range []string{"tx", "tx-reserved", "rx", "rx-drain", "readback", "cancel-tx", "cancel-rx", "cancel-empty-tx", "cancel-empty-rx", "cancel-post-rx", "cancel-mac", "no-tx-ack", "no-rx-ack", "fault-tx-ack", "fault-rx-ack", "early-rx-ack", "lost-tx-ack", "cancel-tx-ack", "cancel-rx-ack", "platform-stop", "cancel-stop", "frozen"} {
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
			if fault == "cancel-empty-tx" && len(r.events) != 2 {
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

func TestProcessStopMatchesVendorRegisterMap(t *testing.T) {
	raw, err := os.ReadFile("testdata/agilex5-stop-fields.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Status uint32 `json:"status_offset"`
		Fields map[string]struct{ Bit, Width uint32 }
	}
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	mask := func(name string) uint32 {
		f, ok := source.Fields[name]
		if !ok || f.Width != 1 {
			t.Fatal("invalid source bit", name)
		}
		return 1 << f.Bit
	}
	if channelStatus != source.Status || txStopped != mask("TPS") || rxStopped != mask("RPS") || dmaFaults != mask("DDE")|mask("FBE")|mask("CDE") {
		t.Fatal("DMA status diverges from vendor fields")
	}
	f := source.Fields["TRCSTS"]
	if txReadShift != f.Bit || txReadMask != 1<<f.Width-1 {
		t.Fatal("TRCSTS includes reserved bits")
	}
}

func TestControlledStopRejectsPreviouslyStoppedChannels(t *testing.T) {
	for _, flag := range []uint32{txStopped, rxStopped} {
		t.Run(map[uint32]string{txStopped: "TX", rxStopped: "RX"}[flag], func(t *testing.T) {
			d, r, _ := controlDevice(t)
			calls := 0
			c, err := d.BindControl(network.DMAControl{Stop: func(context.Context) error { calls++; return nil }, Start: func(context.Context) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			r.regs[channelStatus] |= flag
			if err = c.Stop(context.Background()); err == nil || d.Err() == nil || calls != 0 {
				t.Fatal("old stopped status supplied fresh permission", err)
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

func TestControlledRestartConfirmsAfterInitializationBeforeTraffic(t *testing.T) {
	for _, mode := range []string{"success", "failed", "cancelled", "PHY"} {
		t.Run(mode, func(t *testing.T) {
			d, r, m := controlDevice(t)
			confirms := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hooks, err := d.BindControl(network.DMAControl{
				Stop: func(context.Context) error { r.allowed = false; return nil },
				Start: func(context.Context) error {
					r.allowed = true
					if mode == "PHY" {
						r.phy[3] ^= 1
					}
					return nil
				},
				Confirm: func(context.Context) error {
					confirms++
					if !d.paused || !d.started || m.Read32(rxRing+12)&own == 0 || r.regs[txControl]&1 == 0 || r.regs[rxControl]&1 == 0 {
						t.Fatal("confirmation before initialized hardware or after traffic enabled")
					}
					if mode == "cancelled" {
						cancel()
					}
					if mode == "failed" {
						r.allowed = false
						return errors.New("firmware confirmation lost")
					}
					return nil
				},
			})
			if err != nil || hooks.Confirm != nil {
				t.Fatal("driver must consume the platform confirmation", err)
			}
			if err = hooks.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			err = hooks.Start(ctx)
			if mode == "success" {
				if err != nil || d.paused || confirms != 1 {
					t.Fatal(err, d.paused, confirms)
				}
				return
			}
			if err == nil || d.Err() == nil || !d.paused || (mode == "PHY" && confirms != 0) || (mode != "PHY" && confirms != 1) {
				t.Fatal("failed confirmation reopened traffic", err, confirms)
			}
			accesses, writes := r.accesses, m.writes
			hooks.Stop(context.Background())
			hooks.Start(context.Background())
			d.Receive(make([]byte, maxFrame))
			d.Transmit(make([]byte, 14))
			if accesses != r.accesses || writes != m.writes {
				t.Fatal("retired confirmation accessed device/memory")
			}
		})
	}
}
