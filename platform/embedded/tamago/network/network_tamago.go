//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"net"
	"sync/atomic"

	gnet "github.com/usbarmory/go-net"
)

// Interface owns one QEMU NIC for the lifetime of the image. The board must
// initialize a shared DMA arena outside Go's heap before Open is called.
// DMA must be mapped Normal memory; no buffer is returned to the allocator
// after the device begins DMA. This prototype polls rather than using IRQs.
type Interface struct {
	config Config
	device *NIC
	bridge gnet.Interface
	ctx    context.Context
}

// monitoredNIC stops polling on a fatal driver failure. Invalid individual
// packets can still be dropped without taking down the entire interface.
type monitoredNIC struct {
	device *NIC
	stop   context.CancelCauseFunc
}

func (n monitoredNIC) Receive(buf []byte) (int, error) {
	count, err := n.device.Receive(buf)
	if err != nil && n.device.failed.Load() {
		n.stop(err)
	}
	return count, err
}

func (n monitoredNIC) Transmit(buf []byte) error {
	err := n.device.Transmit(buf)
	if err != nil && n.device.failed.Load() {
		n.stop(err)
	}
	return err
}

var opened atomic.Bool

// Open connects the existing Go net/socket/HTTP API to a real emulated NIC.
// It must run once at startup, before any application uses network sockets.
// A failed startup is not retried against an uncertain device state.
func Open(ctx context.Context, config Config) (*Interface, error) {
	c, err := config.normalized()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if net.SocketFunc != nil || !opened.CompareAndSwap(false, true) {
		return nil, errors.New("network: socket provider already installed or attempted")
	}
	nic, err := openNIC(c.MMIOAddress)
	if err != nil {
		return nil, err
	}
	cfg := gnet.DefaultLnetoStackConfig()
	cfg.Hostname = "fes-qemu"
	cfg.MaxActiveTCPPorts = 16
	cfg.MaxListenerConns = 8
	runCtx, stop := context.WithCancelCause(ctx)
	iface := &Interface{config: c, device: nic, ctx: runCtx,
		bridge: gnet.Interface{Stack: gnet.NewLnetoStack(cfg), NetworkDevice: monitoredNIC{device: nic, stop: stop}}}
	if err = iface.bridge.Init(c.Address, nic.mac.String(), c.Gateway); err != nil {
		nic.failed.Store(true)
		stop(err)
		return nil, err
	}
	net.SocketFunc = func(ctx context.Context, network string, family, sotype int, laddr, raddr net.Addr) (any, error) {
		if err := iface.Err(); err != nil {
			return nil, err
		}
		return iface.bridge.Stack.Socket(ctx, network, family, sotype, laddr, raddr)
	}
	go func() {
		err := iface.bridge.Start(runCtx)
		if err == nil {
			err = errors.New("network: receive loop stopped")
		}
		stop(err)
	}()
	return iface, nil
}

func (i *Interface) Address() string   { return i.config.Address }
func (i *Interface) MAC() string       { return i.device.mac.String() }
func (i *Interface) Statistics() Stats { return i.device.Statistics() }

// Err reports cancellation or the first fatal driver error. Existing socket
// operations still need deadlines; new sockets fail immediately after Err.
func (i *Interface) Err() error { return context.Cause(i.ctx) }

// Done closes when networking becomes unavailable. It is not a DMA fence;
// the device allocations remain reserved until reboot.
func (i *Interface) Done() <-chan struct{} { return i.ctx.Done() }
