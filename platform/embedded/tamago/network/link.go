// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/soypat/lneto/tcp"
	gnet "github.com/usbarmory/go-net"
)

// rxWaiter is an optional interface for devices that signal RX availability
// via an interrupt channel so Link.Start can block in WFI instead of spinning.
type rxWaiter interface {
	RXReady() <-chan struct{}
}

type linkFailure struct{ err error }

// Link connects the pinned Lneto IP stack to Ethernet. It installs the TX callback
// before Configure starts stack workers. One receive owner calls Start.
type Link struct {
	stack  gnet.Stack
	engine *lnetoEngine
	device gnet.NetworkDevice
	prefix netip.Prefix
	state  *linkState
}

// Handles may be copied; all mutable ownership remains in this private object.
type linkState struct {
	failure       atomic.Pointer[linkFailure]
	driverFailure atomic.Pointer[linkFailure]
	expired       func() bool
	io            sync.RWMutex
	paused        atomic.Bool
	claimed       atomic.Bool
	renewerActive atomic.Bool
	tapMu         sync.Mutex
	nextTapID     uint64
	taps          map[uint64]func([]byte)
}

func NewLink(cfg *gnet.LnetoConfig, device gnet.NetworkDevice, address, mac, gateway string) (*Link, error) {
	return newLink(cfg, device, address, mac, gateway, nil)
}

func newLink(cfg *gnet.LnetoConfig, device gnet.NetworkDevice, address, mac, gateway string, expired func() bool) (*Link, error) {
	if expired != nil && expired() {
		return nil, ErrLeaseExpired
	}
	if device == nil {
		return nil, errors.New("network: missing stack/device")
	}
	prefix, err := netip.ParsePrefix(address)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() < 1 || prefix.Bits() > 30 || !usableHost(prefix, prefix.Addr()) {
		return nil, errors.New("network: invalid link IPv4 prefix")
	}
	hardware, err := net.ParseMAC(mac)
	if err != nil || len(hardware) != 6 || hardware[0]&1 != 0 || hardware.String() == "00:00:00:00:00:00" {
		return nil, errors.New("network: invalid link MAC")
	}
	var gw netip.Addr
	if gateway != "" {
		if _, err = staticConfig(address, gateway); err != nil {
			return nil, err
		}
		gw = netip.MustParseAddr(gateway)
	}
	engine := newLnetoEngine(cfg)
	link := &Link{stack: engine, engine: engine, device: device, prefix: prefix, state: &linkState{expired: expired}}
	engine.SetWriteNotify(link.transmit)
	if err = engine.Configure(hardware, prefix, gw); err != nil {
		return nil, err
	}
	return link, nil
}

// Prefix returns the IPv4 CIDR prefix configured on the link.
func (l *Link) Prefix() netip.Prefix {
	if l == nil {
		return netip.Prefix{}
	}
	return l.prefix
}

// DialTCP opens an outbound IPv4 TCP connection to addr through the link's
// stack while enforcing the pause gate and device failure state.
func (l *Link) DialTCP(ctx context.Context, addr netip.AddrPort) (net.Conn, error) {
	if l == nil || l.state == nil || l.stack == nil {
		return nil, ErrControl
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !addr.IsValid() || !addr.Addr().Is4() || addr.Port() == 0 {
		return nil, fmt.Errorf("network: invalid IPv4 TCP destination %q", addr)
	}
	if err := l.Available(); err != nil {
		return nil, err
	}
	if l.engine != nil {
		conn, local, connID, err := l.engine.dialTCP(ctx, addr, 0)
		if err != nil {
			return nil, err
		}
		return &lnetoConn{
			Conn:       conn,
			localAddr:  net.TCPAddrFromAddrPort(local),
			remoteAddr: net.TCPAddrFromAddrPort(addr),
			engine:     l.engine,
			connID:     connID,
		}, nil
	}
	raw, err := l.socket(ctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM, nil, net.TCPAddrFromAddrPort(addr))
	if err != nil {
		return nil, err
	}
	conn, ok := raw.(net.Conn)
	if !ok {
		return nil, fmt.Errorf("network: unexpected socket type %T", raw)
	}
	return conn, nil
}

// ListenTCP binds a passive IPv4 TCP listener on port for the link's local
// address, enforcing the pause gate and race-safe listener close semantics.
func (l *Link) ListenTCP(ctx context.Context, port uint16) (net.Listener, error) {
	if l == nil || l.state == nil || l.stack == nil {
		return nil, ErrControl
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if port == 0 {
		return nil, errors.New("network: TCP listen port must be non-zero")
	}
	if !l.prefix.Addr().Is4() {
		return nil, errors.New("network: link missing IPv4 address")
	}
	if err := l.Available(); err != nil {
		return nil, err
	}
	if l.engine != nil {
		inner, pool, local, err := l.engine.listenTCP(port)
		if err != nil {
			return nil, err
		}
		return &lnetoListener{
			inner:     inner,
			pool:      pool,
			localAddr: net.TCPAddrFromAddrPort(local),
			engine:    l.engine,
			link:      l,
			closeCh:   make(chan struct{}),
		}, nil
	}
	laddr := net.TCPAddrFromAddrPort(netip.AddrPortFrom(l.prefix.Addr(), port))
	raw, err := l.socket(ctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM, laddr, nil)
	if err != nil {
		return nil, err
	}
	if wrapped, ok := raw.(interface{ LnetoListener() *tcp.Listener }); ok {
		return &lnetoListener{
			inner:     wrapped.LnetoListener(),
			localAddr: laddr,
			link:      l,
			closeCh:   make(chan struct{}),
		}, nil
	}
	ln, ok := raw.(net.Listener)
	if !ok {
		return nil, fmt.Errorf("network: unexpected listener type %T", raw)
	}
	return ln, nil
}

// lnetoListener wraps *tcp.Listener with atomic/channel close synchronization
// because xnet.tcplistener reads and writes a non-atomic closed bool in Accept/Close.
type lnetoListener struct {
	inner     *tcp.Listener
	pool      *pinnedTCPPool
	localAddr net.Addr
	engine    *lnetoEngine
	link      *Link
	closed    atomic.Bool
	closeOnce sync.Once
	closeCh   chan struct{}
}

func (ln *lnetoListener) Addr() net.Addr { return ln.localAddr }

func (ln *lnetoListener) Accept() (net.Conn, error) {
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()

	for {
		if ln.closed.Load() {
			return nil, net.ErrClosed
		}
		if ln.link != nil {
			if err := ln.link.Err(); err != nil {
				return nil, err
			}
		}
		if ln.engine != nil {
			c, connID, remoteAddr, lease, err := ln.engine.tryAccept(ln.inner, ln.pool)
			if err == nil && c != nil {
				return &lnetoConn{
					Conn:       c,
					localAddr:  ln.localAddr,
					remoteAddr: remoteAddr,
					engine:     ln.engine,
					lease:      lease,
					connID:     connID,
				}, nil
			}
			if err != nil && ln.closed.Load() {
				return nil, net.ErrClosed
			}
		} else if ln.inner.NumberOfReadyToAccept() > 0 {
			c, _, err := ln.inner.TryAccept()
			if err == nil {
				return &lnetoConn{Conn: c, localAddr: ln.localAddr}, nil
			}
			if ln.closed.Load() {
				return nil, net.ErrClosed
			}
		}
		select {
		case <-ln.closeCh:
			return nil, net.ErrClosed
		case <-timer.C:
			timer.Reset(time.Millisecond)
		}
	}
}

func (l *Link) socket(ctx context.Context, network string, family, sotype int, laddr, raddr net.Addr) (any, error) {
	return l.stack.Socket(ctx, network, family, sotype, laddr, raddr)
}

func (ln *lnetoListener) Close() error {
	alreadyClosed := true
	var err error
	ln.closeOnce.Do(func() {
		alreadyClosed = false
		ln.closed.Store(true)
		close(ln.closeCh)
		if ln.engine != nil {
			err = ln.engine.closeListener(ln.inner)
		} else {
			err = ln.inner.Close()
		}
	})
	if alreadyClosed {
		return net.ErrClosed
	}
	return err
}

type lnetoConn struct {
	*tcp.Conn
	localAddr  net.Addr
	remoteAddr net.Addr
	engine     *lnetoEngine
	lease      *connLease
	connID     uint64
	closed     atomic.Bool
}

func (c *lnetoConn) LnetoConn() *tcp.Conn { return c.Conn }
func (c *lnetoConn) LocalAddr() net.Addr  { return c.localAddr }
func (c *lnetoConn) RemoteAddr() net.Addr {
	if c.remoteAddr != nil {
		return c.remoteAddr
	}
	return &net.TCPAddr{
		IP:   c.Conn.RemoteAddr(),
		Port: int(c.Conn.RemotePort()),
	}
}
func (c *lnetoConn) Read(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	if !c.lease.beginOp() {
		return 0, net.ErrClosed
	}
	defer c.lease.endOp()
	return c.Conn.Read(p)
}
func (c *lnetoConn) Write(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	if !c.lease.beginOp() {
		return 0, net.ErrClosed
	}
	defer c.lease.endOp()
	n, err := c.Conn.Write(p)
	if c.engine != nil && n > 0 {
		c.engine.interruptBackoff()
	}
	return n, err
}
func (c *lnetoConn) SetDeadline(t time.Time) error {
	if c.closed.Load() {
		return net.ErrClosed
	}
	if !c.lease.beginOp() {
		return net.ErrClosed
	}
	defer c.lease.endOp()
	return c.Conn.SetDeadline(t)
}
func (c *lnetoConn) SetReadDeadline(t time.Time) error {
	if c.closed.Load() {
		return net.ErrClosed
	}
	if !c.lease.beginOp() {
		return net.ErrClosed
	}
	defer c.lease.endOp()
	return c.Conn.SetReadDeadline(t)
}
func (c *lnetoConn) SetWriteDeadline(t time.Time) error {
	if c.closed.Load() {
		return net.ErrClosed
	}
	if !c.lease.beginOp() {
		return net.ErrClosed
	}
	defer c.lease.endOp()
	return c.Conn.SetWriteDeadline(t)
}
func (c *lnetoConn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	if c.lease != nil {
		if c.lease.beginClose() {
			return nil
		}
		defer c.lease.endOp()
	}
	if c.engine != nil {
		if !c.engine.canCloseConn(c.Conn, c.connID) {
			return nil
		}
		err := c.Conn.Close()
		c.engine.interruptBackoff()
		return err
	}
	return c.Conn.Close()
}
func (c *lnetoConn) Abort() {
	c.closed.Store(true)
	if c.lease != nil {
		if c.lease.beginClose() {
			return
		}
		defer c.lease.endOp()
	}
	if c.engine != nil {
		c.engine.abortConnIfID(c.Conn, c.connID)
		return
	}
	c.Conn.Abort()
}
func (l *Link) Err() error {
	if l == nil || l.state == nil {
		return ErrControl
	}
	if err := l.driverError(); err != nil {
		return err
	}
	if failure := l.state.failure.Load(); failure != nil {
		return failure.err
	}
	return nil
}
func (l *Link) driverError() error {
	if failure := l.state.driverFailure.Load(); failure != nil {
		return failure.err
	}
	if d, ok := l.device.(interface{ Err() error }); ok {
		if err := d.Err(); err != nil {
			l.failDevice(err)
			return err
		}
	}
	return nil
}
func (l *Link) failDevice(err error) {
	l.state.driverFailure.CompareAndSwap(nil, &linkFailure{err: err})
}
func (l *Link) fail(err error) { l.state.failure.CompareAndSwap(nil, &linkFailure{err: err}) }

func (l *Link) claimRenewer() bool {
	return l != nil && l.state != nil && l.state.renewerActive.CompareAndSwap(false, true)
}

func (l *Link) releaseRenewer() {
	if l != nil && l.state != nil {
		l.state.renewerActive.Store(false)
	}
}
func (l *Link) transmit(buf []byte) {
	if !l.enter() {
		return
	}
	defer l.state.io.RUnlock()
	if len(buf) == 0 {
		buf = make([]byte, 1518)
	}
	n, err := l.stack.WriteOutboundPacket(buf)
	if err != nil {
		l.fail(err)
		return
	}
	if n < 0 || n > len(buf) {
		l.fail(errors.New("network: stack returned invalid frame length"))
		return
	}
	if n < 14 {
		return
	}
	if err = l.device.Transmit(buf[:n]); err != nil {
		l.failDevice(err)
	}
}

// SendRawFrame transmits one raw Ethernet frame while obeying the link's
// software pause gate and DMA failure contract.
func (l *Link) SendRawFrame(frame []byte) error {
	if l == nil || l.state == nil || l.device == nil {
		return ErrControl
	}
	if len(frame) < 14 || len(frame) > 1518 {
		return errors.New("network: invalid raw frame length")
	}
	if err := l.Available(); err != nil {
		return err
	}
	if !l.enter() {
		if err := l.Available(); err != nil {
			return err
		}
		return ErrPaused
	}
	defer l.state.io.RUnlock()
	if err := l.device.Transmit(frame); err != nil {
		l.failDevice(err)
		return err
	}
	return nil
}

// AddTap registers a non-blocking callback invoked with a copy of each inbound
// Ethernet frame received by Link.Start. Call the returned function to remove
// the tap (idempotent).
func (l *Link) AddTap(fn func([]byte)) func() {
	if l == nil || l.state == nil || fn == nil {
		return func() {}
	}
	l.state.tapMu.Lock()
	if l.state.taps == nil {
		l.state.taps = make(map[uint64]func([]byte))
	}
	l.state.nextTapID++
	id := l.state.nextTapID
	l.state.taps[id] = fn
	l.state.tapMu.Unlock()
	return func() {
		l.state.tapMu.Lock()
		delete(l.state.taps, id)
		l.state.tapMu.Unlock()
	}
}

func (l *Link) dispatchTaps(frame []byte) {
	l.state.tapMu.Lock()
	if len(l.state.taps) == 0 {
		l.state.tapMu.Unlock()
		return
	}
	callbacks := make([]func([]byte), 0, len(l.state.taps))
	for _, fn := range l.state.taps {
		callbacks = append(callbacks, fn)
	}
	l.state.tapMu.Unlock()
	cp := append([]byte(nil), frame...)
	for _, fn := range callbacks {
		fn(cp)
	}
}

func (l *Link) Start(ctx context.Context) error {
	defer func() {
		if l.engine != nil {
			l.engine.stop()
		}
		if err := ctx.Err(); err != nil {
			l.fail(err)
		}
	}()
	buf := make([]byte, 1518)
	for ctx.Err() == nil {
		if err := l.Err(); err != nil {
			return err
		}
		if !l.enter() {
			runtime.Gosched()
			continue
		}
		n, err := l.device.Receive(buf)
		l.state.io.RUnlock()
		if err != nil {
			l.failDevice(err)
			return err
		}
		if n < 0 || n > len(buf) {
			err := errors.New("network: device returned invalid frame length")
			l.failDevice(err)
			return err
		}
		if n == 0 {
			if waiter, ok := l.device.(rxWaiter); ok {
				if ready := waiter.RXReady(); ready != nil {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-ready:
					case <-time.After(10 * time.Millisecond):
					}
					continue
				}
			}
			runtime.Gosched()
			continue
		}
		l.dispatchTaps(buf[:n])
		// Invalid remote packets are rejected by the stack, not a device failure.
		_ = l.stack.RecvInboundPacket(buf[:n])
	}
	return ctx.Err()
}
