// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

const (
	txDebug     = 0x1108
	rxDebug     = 0x1148
	txReadShift = 1
	txReadMask  = 3 // CS/R1 register map: TRCSTS is two bits, not the BSP's three.
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
			if platform.Confirm != nil {
				if err = ctx.Err(); err != nil {
					return d.retireControl(err)
				}
				if err = d.Err(); err != nil {
					return err
				}
				if err = platform.Confirm(ctx); err != nil {
					return d.retireControl(err)
				}
			}
			if err = ctx.Err(); err != nil {
				return d.retireControl(err)
			}
			if err := d.Err(); err != nil {
				return err
			}
			if !d.sameMemory() {
				return d.retireControl(errors.New("xgmac: confirmed DMA window changed"))
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
	return d.waitStop(ctx, offset, "FIFO", func(v uint32) (bool, error) { return empty(v), nil })
}

func (d *Device) waitStopped(ctx context.Context, want uint32) error {
	return d.waitStop(ctx, channelStatus, "DMA process-stop acknowledgement", func(v uint32) (bool, error) {
		if v&dmaFaults != 0 || v&(txStopped|rxStopped)&^want != 0 {
			return false, fmt.Errorf("xgmac: invalid DMA status during stop: %#x", v)
		}
		return v&want == want, nil
	})
}

func (d *Device) waitStop(ctx context.Context, offset uint32, what string, ready func(uint32) (bool, error)) error {
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
		done, err := ready(value)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if !d.io.Now().Before(end) {
			break
		}
		if count&255 == 255 {
			runtime.Gosched()
		}
	}
	return fmt.Errorf("xgmac: %s timeout at %#x; DMA ownership retained", what, offset)
}

// The vendor programming model additionally requires process-stop IRQ status,
// rather than inferring completion from cleared start bits or FIFO state. Active
// health checked both stop flags clear under this same driver lock before entry,
// so only fresh acknowledgements can satisfy these waits. This is no NoC fence.
func (d *Device) stopFIFO(ctx context.Context) error {
	if err := d.stopEnable(ctx, txControl); err != nil {
		return err
	}
	if err := d.waitStopped(ctx, txStopped); err != nil {
		return err
	}
	if err := d.waitFIFO(ctx, txDebug, func(v uint32) bool { return (v>>txReadShift)&txReadMask != 1 && v&(1<<4) == 0 }); err != nil {
		return err
	}
	if err := d.waitFIFO(ctx, rxDebug, func(v uint32) bool { return v&(3<<4) == 0 }); err != nil {
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
	if err := d.waitStopped(ctx, txStopped|rxStopped); err != nil {
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
