//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

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
	lease, acquiredAt, err := acquireDHCPWithTime(startup, nic, dhcpCfg)
	if err != nil {
		return nil, err
	}
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
	current := new(atomic.Pointer[Interface])
	iface, err := newInterface(ctx, c, nic, lease, leaseAcquiredAt, hasLease, current)
	if err != nil {
		return nil, err
	}
	current.Store(iface)
	// Installed once. In-flight connections stay on their original link.
	net.SocketFunc = func(ctx context.Context, network string, family, sotype int, laddr, raddr net.Addr) (any, error) {
		active := current.Load()
		if err := active.Err(); err != nil {
			return nil, err
		}
		if err := active.bridge.Available(); err != nil {
			return nil, err
		}
		return active.bridge.socket(ctx, network, family, sotype, laddr, raddr)
	}
	return iface, nil
}
