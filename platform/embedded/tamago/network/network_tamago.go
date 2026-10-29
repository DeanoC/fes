//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"net"
	"sync/atomic"

	gnet "github.com/usbarmory/go-net"
)

// Interface owns one board NIC for the lifetime of the image. The board must
// initialize a shared DMA arena outside Go's heap before Open is called.
// DMA must be mapped Normal memory; no buffer is returned to the allocator
// after the device begins DMA. This prototype polls rather than using IRQs.
type Interface struct {
	config Config
	device Device
	bridge *Link
	ctx    context.Context
}

var opened atomic.Bool

// OpenDevice installs the shared Go socket/IP service over one explicit board
// device. Addressing is mandatory; this path has no QEMU defaults or discovery.
// The factory runs only after socket-provider ownership has been admitted.
func OpenDevice(ctx context.Context, address, gateway string, factory func() (Device, error)) (*Interface, error) {
	c, err := staticConfig(address, gateway)
	if err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, errors.New("network: missing device factory")
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
	nic, err := factory()
	if err != nil {
		return nil, err
	}
	if nic == nil {
		return nil, errors.New("network: factory returned nil device")
	}
	cfg := gnet.DefaultLnetoStackConfig()
	cfg.Hostname = "fes-native"
	cfg.MaxActiveTCPPorts = 16
	cfg.MaxListenerConns = 8
	runCtx, stop := context.WithCancelCause(ctx)
	stack := gnet.NewLnetoStack(cfg)
	iface := &Interface{config: c, device: nic, ctx: runCtx}
	iface.bridge, err = NewLink(stack, nic, c.Address, nic.MAC(), c.Gateway)
	if err != nil {
		stop(err)
		return nil, err
	}
	net.SocketFunc = func(ctx context.Context, network string, family, sotype int, laddr, raddr net.Addr) (any, error) {
		if err := iface.Err(); err != nil {
			return nil, err
		}
		return stack.Socket(ctx, network, family, sotype, laddr, raddr)
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
func (i *Interface) MAC() string       { return i.device.MAC() }
func (i *Interface) Statistics() Stats { return i.device.Statistics() }

// Err reports cancellation or the first fatal driver error. Existing socket
// operations still need deadlines; new sockets fail immediately after Err.
func (i *Interface) Err() error {
	if err := i.device.Err(); err != nil {
		return err
	}
	if err := i.bridge.Err(); err != nil {
		return err
	}
	return context.Cause(i.ctx)
}

// Done closes when networking becomes unavailable. It is not a DMA fence;
// the device allocations remain reserved until reboot.
func (i *Interface) Done() <-chan struct{} { return i.ctx.Done() }
