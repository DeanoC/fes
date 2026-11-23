//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/soypat/lneto"
	gnet "github.com/usbarmory/go-net"
)

type irqDevice interface {
	EnableIRQ(*gic.Controller) error
	DisableIRQ(*gic.Controller)
	IRQCount() uint64
}

// Interface owns one board NIC for the lifetime of the image. The board must
// initialize a shared DMA arena outside Go's heap before Open is called.
// DMA must be mapped Normal memory; no buffer is returned to the allocator
// after the device begins DMA. This prototype polls rather than using IRQs.
type Interface struct {
	config     Config
	device     Device
	bridge     *Link
	ctx        context.Context
	stop       context.CancelCauseFunc
	irqEnabled atomic.Bool
	leaseState interfaceLease
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
	nic, ctx, err := admitAndOpenDevice(ctx, factory)
	if err != nil {
		return nil, err
	}
	return installInterface(ctx, c, nic, DHCPLease{}, time.Time{}, false)
}

// OpenDeviceDHCP opens one explicit board device, acquires a validated IPv4
// lease via RFC 2131 DHCPv4, and installs the shared Go socket/IP service.
func OpenDeviceDHCP(ctx context.Context, factory func() (Device, error), dhcpCfg DHCPConfig) (*Interface, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return OpenDeviceDHCPWithStartup(ctx, ctx, factory, dhcpCfg)
}

// OpenDeviceDHCPWithStartup separates the bounded startup context (used for
// device open and RFC 2131 DHCPv4 lease acquisition) from the stack lifetime
// context (used by background Lneto socket/IP workers after return).
func OpenDeviceDHCPWithStartup(startup, lifetime context.Context, factory func() (Device, error), dhcpCfg DHCPConfig) (*Interface, error) {
	if err := validateDHCPConfig(dhcpCfg); err != nil {
		return nil, err
	}
	if startup == nil {
		startup = context.Background()
	}
	if err := startup.Err(); err != nil {
		return nil, err
	}
	nic, lifetime, err := admitAndOpenDevice(lifetime, factory)
	if err != nil {
		return nil, err
	}
	lease, err := AcquireDHCP(startup, nic, dhcpCfg)
	if err != nil {
		return nil, err
	}
	acquiredAt := time.Now()
	if err := lifetime.Err(); err != nil {
		return nil, err
	}
	c, err := staticConfig(lease.Address, lease.Gateway)
	if err != nil {
		return nil, err
	}
	return installInterface(lifetime, c, nic, lease, acquiredAt, true)
}

func admitAndOpenDevice(ctx context.Context, factory func() (Device, error)) (Device, context.Context, error) {
	if factory == nil {
		return nil, nil, errors.New("network: missing device factory")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if net.SocketFunc != nil || !opened.CompareAndSwap(false, true) {
		return nil, nil, errors.New("network: socket provider already installed or attempted")
	}
	nic, err := factory()
	if err != nil {
		return nil, nil, err
	}
	if nic == nil {
		return nil, nil, errors.New("network: factory returned nil device")
	}
	return nic, ctx, nil
}

func installInterface(ctx context.Context, c Config, nic Device, lease DHCPLease, leaseAcquiredAt time.Time, hasLease bool) (*Interface, error) {
	cfg := gnet.DefaultLnetoStackConfig()
	cfg.Hostname = "fes-native"
	cfg.MaxActiveTCPPorts = 16
	cfg.MaxListenerConns = 8
	runCtx, stop := context.WithCancelCause(ctx)
	iface := &Interface{config: c, device: nic, ctx: runCtx, stop: stop}
	iface.leaseState.init(lease, leaseAcquiredAt, hasLease)
	backoff := func(consecutive uint) time.Duration {
		if !iface.irqEnabled.Load() || consecutive == 0 {
			return lneto.BackoffFlagGosched
		}
		return 5 * time.Millisecond
	}
	cfg.BackoffStack = backoff
	cfg.NewBackoffTCP = func() lneto.BackoffStrategy { return backoff }
	stack := gnet.NewLnetoStack(cfg)
	var err error
	iface.bridge, err = NewLink(stack, nic, c.Address, nic.MAC(), c.Gateway)
	if err != nil {
		stop(err)
		return nil, err
	}
	net.SocketFunc = func(ctx context.Context, network string, family, sotype int, laddr, raddr net.Addr) (any, error) {
		if err := iface.Err(); err != nil {
			return nil, err
		}
		if err := iface.bridge.Available(); err != nil {
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

// Lease returns the acquired DHCPv4 lease and true if the interface was opened
// via DHCP, or the zero value and false for static IPv4 configurations.
func (i *Interface) Lease() (DHCPLease, bool) {
	if i == nil {
		return DHCPLease{}, false
	}
	return i.leaseState.get()
}

// NewLeaseRenewer creates an opt-in RFC 2131 DHCPv4 T1/T2 LeaseRenewer wired
// to this interface's active *Link (preserving its pause/DMA failure gate and
// original lease acquisition timestamp). Only one LeaseRenewer may own the
// interface at a time (concurrent calls fail with ErrRenewerActive until the
// active renewer stops or exits). Successful renewals synchronize back to
// Interface.Lease() and future NewLeaseRenewer calls. If the lease expires,
// is NAK'd, or changes IP/gateway, the interface and link are halted immediately
// per RFC 2131 §4.4.5 so traffic stops using the unleased address.
// Returns ErrDHCPInvalidConfig if the interface was not opened via DHCP.
func (i *Interface) NewLeaseRenewer(cfg RenewConfig) (*LeaseRenewer, error) {
	if i == nil || i.bridge == nil || i.device == nil {
		return nil, fmt.Errorf("%w: interface has no active DHCP lease", ErrDHCPInvalidConfig)
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	return i.leaseState.newRenewer(i.bridge, i.device.MAC(), cfg, func(err error) {
		i.bridge.fail(err)
		if i.stop != nil {
			i.stop(err)
		}
	})
}

// EnableIRQ routes the underlying NIC's RX interrupt through g so Link.Start
// can wait in WFI between packets. Returns gic.ErrUnsupported on polling-only NICs.
func (i *Interface) EnableIRQ(g *gic.Controller) error {
	if i == nil || i.device == nil {
		return ErrControl
	}
	dev, ok := i.device.(irqDevice)
	if !ok {
		return gic.ErrUnsupported
	}
	if err := dev.EnableIRQ(g); err != nil {
		return err
	}
	i.irqEnabled.Store(true)
	return nil
}

// DisableIRQ masks the underlying NIC's RX interrupt and returns Link.Start to polling.
func (i *Interface) DisableIRQ(g *gic.Controller) {
	if i == nil || i.device == nil {
		return
	}
	i.irqEnabled.Store(false)
	if dev, ok := i.device.(irqDevice); ok {
		dev.DisableIRQ(g)
	}
}

// IRQCount returns the number of NIC interrupts handled since EnableIRQ.
func (i *Interface) IRQCount() uint64 {
	if i == nil || i.device == nil {
		return 0
	}
	if dev, ok := i.device.(irqDevice); ok {
		return dev.IRQCount()
	}
	return 0
}

// BindControl requires a trusted platform supplier and binds once. Ordinary
// OpenDevice and cancellation provide no DMA-drain capability themselves.
func (i *Interface) BindControl(dma DMAControl) (*Controller, error) {
	if i == nil || i.bridge == nil {
		return nil, ErrControl
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	return NewController(i.bridge, dma)
}

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
