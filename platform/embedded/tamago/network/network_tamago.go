//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
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

type irqDevice interface {
	EnableIRQ(*gic.Controller) error
	DisableIRQ(*gic.Controller)
	IRQCount() uint64
}

func (i *Interface) MAC() string { return i.device.MAC() }

func (i *Interface) Statistics() Stats { return i.device.Statistics() }

// DialTCP opens an outbound IPv4 TCP connection over this interface's active *Link.
func (i *Interface) DialTCP(ctx context.Context, addr netip.AddrPort) (net.Conn, error) {
	if i == nil || i.bridge == nil {
		return nil, ErrControl
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	return i.bridge.DialTCP(ctx, addr)
}

// ListenTCP opens a passive IPv4 TCP listener on port over this interface's active *Link.
func (i *Interface) ListenTCP(ctx context.Context, port uint16) (net.Listener, error) {
	if i == nil || i.bridge == nil {
		return nil, ErrControl
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	return i.bridge.ListenTCP(ctx, port)
}

// NewHTTPTransport returns an *http.Transport wired to this interface's active *Link.
func (i *Interface) NewHTTPTransport(cfg TransportConfig) (*http.Transport, error) {
	if i == nil || i.bridge == nil {
		return nil, fmt.Errorf("%w: interface unavailable", ErrInvalidHTTPConfig)
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	return NewHTTPTransport(i.bridge, cfg)
}

// NewHTTPClient returns an *http.Client wired to this interface's active *Link.
func (i *Interface) NewHTTPClient(cfg TransportConfig) (*http.Client, error) {
	if i == nil || i.bridge == nil {
		return nil, fmt.Errorf("%w: interface unavailable", ErrInvalidHTTPConfig)
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	return NewHTTPClient(i.bridge, cfg)
}

// NewHTTPServer constructs an idle HTTP/HTTPS server bound to this interface's active *Link.
func (i *Interface) NewHTTPServer(cfg ServerConfig) (*HTTPServer, error) {
	if i == nil || i.bridge == nil {
		return nil, fmt.Errorf("%w: interface unavailable", ErrInvalidHTTPConfig)
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	return NewHTTPServer(i.bridge, cfg)
}

// ResolveA resolves name to IPv4 addresses over this interface's active *Link.
// Unset LocalMAC, LocalPrefix, and Gateway default to the interface's active
// configuration; unset Servers default to the interface's DHCP lease DNS list.
func (i *Interface) ResolveA(ctx context.Context, name string, cfg DNSConfig) ([]netip.Addr, error) {
	if i == nil || i.bridge == nil || i.device == nil {
		return nil, fmt.Errorf("%w: interface unavailable", ErrDNSInvalidConfig)
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	if cfg.LocalMAC == "" {
		cfg.LocalMAC = i.device.MAC()
	}
	if cfg.LocalPrefix == "" {
		cfg.LocalPrefix = i.config.Address
	}
	if cfg.Gateway == "" {
		cfg.Gateway = i.config.Gateway
	}
	if len(cfg.Servers) == 0 {
		if lease, ok := i.Lease(); ok && len(lease.DNS) > 0 {
			cfg.Servers = append([]string(nil), lease.DNS...)
		}
	}
	return ResolveA(ctx, i.bridge, cfg, name)
}

// QuerySNTP executes one RFC 4330 SNTPv4 UTC time query over this interface's
// active *Link. Unset LocalMAC, LocalPrefix, and Gateway default to the
// interface's active configuration.
func (i *Interface) QuerySNTP(ctx context.Context, cfg SNTPConfig) (SNTPResult, error) {
	if i == nil || i.bridge == nil || i.device == nil {
		return SNTPResult{}, fmt.Errorf("%w: interface unavailable", ErrSNTPInvalidConfig)
	}
	if err := i.Err(); err != nil {
		return SNTPResult{}, err
	}
	cfg = i.defaultSNTPConfig(cfg)
	return QuerySNTP(ctx, i.bridge, cfg)
}

// NewSNTPSyncer constructs a periodic SNTPv4 discipline loop bound to this
// interface's active *Link. Unset LocalMAC, LocalPrefix, and Gateway default to
// the interface's active configuration.
func (i *Interface) NewSNTPSyncer(cfg SNTPSyncConfig) (*SNTPSyncer, error) {
	if i == nil || i.bridge == nil || i.device == nil {
		return nil, fmt.Errorf("%w: interface unavailable", ErrSNTPInvalidConfig)
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	cfg.SNTP = i.defaultSNTPConfig(cfg.SNTP)
	return NewSNTPSyncer(i.bridge, cfg)
}

func (i *Interface) defaultSNTPConfig(cfg SNTPConfig) SNTPConfig {
	if cfg.LocalMAC == "" {
		cfg.LocalMAC = i.device.MAC()
	}
	if cfg.LocalPrefix == "" {
		cfg.LocalPrefix = i.config.Address
	}
	if cfg.Gateway == "" {
		cfg.Gateway = i.config.Gateway
	}
	return cfg
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

// SendRawFrame transmits one raw Ethernet frame while obeying the link's
// pause gate and DMA failure contract.
func (i *Interface) SendRawFrame(frame []byte) error {
	if i == nil || i.bridge == nil {
		return ErrControl
	}
	if err := i.Err(); err != nil {
		return err
	}
	return i.bridge.SendRawFrame(frame)
}

// AddTap registers a non-blocking callback invoked with a copy of each inbound
// Ethernet frame received by Link.Start. Call the returned function to remove
// the tap (idempotent).
func (i *Interface) AddTap(fn func([]byte)) func() {
	if i == nil || i.bridge == nil {
		return func() {}
	}
	return i.bridge.AddTap(fn)
}

var _ RawFramePort = (*Interface)(nil)
