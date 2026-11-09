// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"context"
	"errors"
	"net"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

const (
	txDebug = 0x1108
	rxDebug = 0x1148
)

// BindControl joins this exact driver/window to trusted platform callbacks.
// The caller must already own the initial firmware MAC grant, route all driver
// traffic through the same Link, and bind the returned hooks to its controller.
// Bind during device creation, before starting the link's packet workers.
// Stop must independently prove DMA drained; FIFO idle and cleared start bits
// are insufficient. Start must restore the same MAC/PHY/window grant with DMA
// stopped. Neither callback may reclaim memory. There is no default supplier.
func (d *Device) BindControl(platform network.DMAControl) (network.DMAControl, error) {
	if d == nil || platform.Stop == nil || platform.Start == nil {
		return network.DMAControl{}, network.ErrControl
	}
	if !d.mu.TryLock() {
		return network.DMAControl{}, network.ErrBusy
	}
	defer d.mu.Unlock()
	if err := d.Err(); err != nil {
		return network.DMAControl{}, err
	}
	if d.controlBound || d.paused || !d.started || !d.sameMemory() {
		return network.DMAControl{}, network.ErrControl
	}
	d.controlBound = true
	contained := false // private state shared by copies of the returned callbacks
	return network.DMAControl{
		Stop: func(ctx context.Context) error {
			if ctx == nil {
				return network.ErrControl
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !d.mu.TryLock() {
				return d.retireControl(network.ErrBusy)
			}
			defer d.mu.Unlock()
			if err := d.Err(); err != nil {
				return err
			}
			if contained {
				return nil
			}
			if !d.sameMemory() {
				return d.retireControl(errors.New("xgmac: DMA window changed"))
			}
			if err := d.health(); err != nil {
				return err
			}
			d.paused = true
			if err := d.stopFIFO(ctx); err != nil {
				return d.retireControl(err)
			}
			if err := ctx.Err(); err != nil {
				return d.retireControl(err)
			}
			if err := d.Err(); err != nil {
				return err
			}
			if err := platform.Stop(ctx); err != nil {
				return d.retireControl(err)
			}
			if err := ctx.Err(); err != nil {
				return d.retireControl(err)
			}
			if err := d.Err(); err != nil {
				return err
			}
			if !d.sameMemory() {
				return d.retireControl(errors.New("xgmac: stopped DMA window changed"))
			}
			// No controller or DMA-memory access after the platform stop: it
			// may have asserted reset or removed the register mapping.
			d.started = false
			contained = true
			return nil
		},
		Start: func(ctx context.Context) error {
			if ctx == nil {
				return network.ErrControl
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !d.mu.TryLock() {
				return d.retireControl(network.ErrBusy)
			}
			defer d.mu.Unlock()
			if err := d.Err(); err != nil {
				return err
			}
			if !contained || !d.paused {
				return network.ErrControl
			}
			if !d.sameMemory() {
				return d.retireControl(errors.New("xgmac: resume DMA window changed"))
			}
			if err := platform.Start(ctx); err != nil {
				return d.retireControl(err)
			}
			if err := ctx.Err(); err != nil {
				return d.retireControl(err)
			}
			if err := d.Err(); err != nil {
				return err
			}
			if !d.sameMemory() {
				return d.retireControl(errors.New("xgmac: resumed DMA window changed"))
			}
			mac, err := net.ParseMAC(d.mac)
			if err != nil {
				return d.retireControl(err)
			}
			d.rx, d.tx = 0, 0
			// Descriptor reinitialization follows the independent completed
			// drain and new platform grant. The original arena stays pinned.
			if err = d.initialize(mac); err != nil {
				return d.retireControl(err)
			}
			if err = ctx.Err(); err != nil {
				return d.retireControl(err)
			}
			if err := d.Err(); err != nil {
				return err
			}
			contained, d.paused = false, false
			return nil
		},
	}, nil
}

func (d *Device) sameMemory() bool {
	return d.memory.Base() == d.dmaBase && d.memory.Size() == d.dmaBytes
}

// A platform failure can mean MMIO access was revoked. Retire without best-
// effort register writes; new Receive/Transmit refuse through the cached error.
func (d *Device) retireControl(err error) error {
	d.fault.CompareAndSwap(nil, &failure{err: err})
	return d.Err()
}

func (d *Device) waitFIFO(ctx context.Context, offset uint32, empty func(uint32) bool) error {
	end := d.io.Now().Add(100 * time.Millisecond)
	for count := 0; count < 1000000; count++ {
		if err := d.Err(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		value := d.io.Read32(offset)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.Err(); err != nil {
			return err
		}
		if empty(value) {
			return nil
		}
		if !d.io.Now().Before(end) {
			break
		}
		if count&255 == 255 {
			runtime.Gosched()
		}
	}
	return errors.New("xgmac: FIFO stop timeout; DMA ownership retained")
}

// Same ordered MAC/FIFO stages as the matched BSP, with checked timeouts and
// context cancellation. This establishes only driver-side stop, not a DMA fence.
func (d *Device) stopFIFO(ctx context.Context) error {
	if err := d.stopEnable(ctx, txControl); err != nil {
		return err
	}
	if err := d.waitFIFO(ctx, txDebug, func(v uint32) bool { return (v>>1)&7 != 1 && v&(1<<4) == 0 }); err != nil {
		return err
	}
	if err := d.stopEnable(ctx, txConfig); err != nil {
		return err
	}
	if err := d.stopEnable(ctx, rxConfig); err != nil {
		return err
	}
	if err := d.waitFIFO(ctx, rxDebug, func(v uint32) bool { return v&((0x7fff<<16)|(3<<4)) == 0 }); err != nil {
		return err
	}
	if err := d.stopEnable(ctx, rxControl); err != nil {
		return err
	}
	d.memory.Barrier()
	for _, offset := range []uint32{txControl, rxControl, txConfig, rxConfig} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.io.Read32(offset)&1 != 0 {
			return errors.New("xgmac: MAC/DMA stop readback changed")
		}
	}
	return nil
}

func (d *Device) stopEnable(ctx context.Context, offset uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.Err(); err != nil {
		return err
	}
	value := d.io.Read32(offset)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.Err(); err != nil {
		return err
	}
	d.io.Write32(offset, value&^1)
	return errors.Join(ctx.Err(), d.Err())
}
