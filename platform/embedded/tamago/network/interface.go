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
	config       Config
	device       Device
	bridge       *Link
	ctx          context.Context
	stop         context.CancelCauseFunc
	irqEnabled   atomic.Bool
	leaseState   interfaceLease
	current      *atomic.Pointer[Interface]
	receiverDone chan struct{}
}

func newInterface(ctx context.Context, c Config, nic Device, lease DHCPLease, leaseAcquiredAt time.Time, hasLease bool, current *atomic.Pointer[Interface]) (*Interface, error) {
	cfg := gnet.DefaultLnetoStackConfig()
	cfg.Hostname = "fes-native"
	cfg.MaxActiveTCPPorts = 16
	cfg.MaxListenerConns = 8
	runCtx, stop := context.WithCancelCause(ctx)
	iface := &Interface{config: c, device: nic, ctx: runCtx, stop: stop, current: current, receiverDone: make(chan struct{})}
	iface.leaseState.init(lease, leaseAcquiredAt, hasLease)
	backoff := func(consecutive uint) time.Duration {
		if !iface.irqEnabled.Load() || consecutive == 0 {
			return lneto.BackoffFlagGosched
		}
		return 5 * time.Millisecond
	}
	cfg.BackoffStack = backoff
	cfg.NewBackoffTCP = func() lneto.BackoffStrategy { return backoff }
	var err error
	var expired func() bool
	if hasLease {
		expired = iface.dhcpExpired
	}
	iface.bridge, err = newLink(cfg, nic, c.Address, nic.MAC(), c.Gateway, expired)
	if err != nil {
		stop(err)
		return nil, err
	}
	go func() {
		defer close(iface.receiverDone)
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
// original lease acquisition timestamp). Only one LeaseRenewer may be active on
// the interface at a time (concurrent Start calls fail with ErrRenewerActive
// until the active renewer stops or exits). Successful renewals synchronize back
// to Interface.Lease() and future renewer starts. If the lease expires, is
// NAK'd, or changes IP/gateway, the interface and link are halted immediately
// per RFC 2131 §4.4.5 so traffic stops using the unleased address.
// Returns ErrDHCPInvalidConfig if the interface was not opened via DHCP.
func (i *Interface) NewLeaseRenewer(cfg RenewConfig) (*LeaseRenewer, error) {
	if i == nil || i.bridge == nil || i.device == nil {
		return nil, fmt.Errorf("%w: interface has no active DHCP lease", ErrDHCPInvalidConfig)
	}
	if err := i.Err(); err != nil {
		return nil, err
	}
	return i.leaseState.newRenewer(i.bridge, i.device.MAC(), cfg, i.Err, func(err error) {
		i.bridge.fail(err)
		if i.stop != nil {
			i.stop(err)
		}
	})
}

// RenewLeaseOnce executes one RFC 2131 DHCPv4 RENEWING (rebind=false) or
// REBINDING (rebind=true) exchange over this interface's active *Link and
// synchronizes the renewed lease back to Interface.Lease(). Fails with
// ErrRenewerActive if another renewal or LeaseRenewer is already active, and
// halts the interface immediately on ErrLeaseNAK or ErrLeaseChanged.
func (i *Interface) RenewLeaseOnce(ctx context.Context, rebind bool, cfg RenewConfig) (DHCPLease, error) {
	if i == nil || i.bridge == nil || i.device == nil {
		return DHCPLease{}, fmt.Errorf("%w: interface has no active DHCP lease", ErrDHCPInvalidConfig)
	}
	return i.leaseState.renewOnce(ctx, i.bridge, i.device.MAC(), rebind, cfg, i.Err, func(err error) {
		i.bridge.fail(err)
		if i.stop != nil {
			i.stop(err)
		}
	})
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

var _ RawFramePort = (*Interface)(nil)

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

func (i *Interface) dhcpExpired() bool {
	l := &i.leaseState
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.acquiredAt.IsZero() || l.lease.LeaseDuration <= 0 || (l.lease.LeaseDuration < dhcpInfiniteLease && !time.Now().Before(l.acquiredAt.Add(l.lease.LeaseDuration)))
}

// RecoverDHCPAfterPause gives a paired launcher a fresh software endpoint when
// its C64 session spans a lease expiry. Only the existing exclusive hardware
// controller can install this policy or start the same device again.
func (i *Interface) RecoverDHCPAfterPause(c *Controller, lifetime context.Context, cfg DHCPConfig,
	withdraw func(), publish func(*Interface) error, unavailable func(error)) error {
	if i == nil || c == nil || c.state == nil || lifetime == nil || withdraw == nil || publish == nil || unavailable == nil ||
		i.current == nil || i.current.Load() != i || validateDHCPConfig(cfg) != nil {
		return ErrControl
	}
	state := c.state
	state.operation.Lock()
	defer state.operation.Unlock()
	if state.link != i.bridge || !state.assigned.Load() || state.lease != nil || state.paused {
		return ErrControl
	}
	if _, ok := i.Lease(); !ok {
		return ErrControl
	}
	active := i
	state.lease = &pauseLease{
		expired: func() bool { return active.dhcpExpired() },
		withdraw: func(ctx context.Context) error {
			// The controller has blocked both old packet directions. Stop all
			// application sockets, joins and taps before raw DHCP owns RX.
			active.stop(ErrLeaseExpired)
			withdraw()
			select {
			case <-active.receiverDone:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		acquire: func(ctx context.Context) (*Link, error) {
			if err := lifetime.Err(); err != nil {
				return nil, err
			}
			lease, at, err := acquireDHCPWithTime(ctx, active.device, cfg)
			if err != nil {
				var deviceErr dhcpDeviceError
				if errors.As(err, &deviceErr) {
					active.bridge.failDevice(deviceErr.err)
				}
				return nil, err
			}
			cfg4, err := staticConfig(lease.Address, lease.Gateway)
			if err != nil {
				return nil, err
			}
			fresh, err := newInterface(lifetime, cfg4, active.device, lease, at, true, active.current)
			if err != nil {
				return nil, err
			}
			fresh.irqEnabled.Store(active.irqEnabled.Load())
			if err = publish(fresh); err != nil {
				fresh.bridge.state.paused.Store(true)
				fresh.bridge.fail(err)
				fresh.stop(err)
				return fresh.bridge, err
			}
			fresh.current.Store(fresh)
			active = fresh
			return fresh.bridge, nil
		},
		unavailable: unavailable,
	}
	return nil
}
