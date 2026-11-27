// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/soypat/lneto"
	"github.com/soypat/lneto/ethernet"
	"github.com/soypat/lneto/ipv4"
	"github.com/soypat/lneto/tcp"
	"github.com/soypat/lneto/x/xnet"
	gnet "github.com/usbarmory/go-net"
)

const (
	lnetoARPRetryInterval = 250 * time.Millisecond
	lnetoDialTimeout      = 4 * time.Second
	lnetoEstablishedTO    = 4 * time.Second
	lnetoClosingTO        = 2 * time.Second
	lnetoRequeueInterval  = 400 * time.Millisecond
	lnetoMaxPortRetries   = 8
	lnetoGatewayTimeout   = 4 * time.Second
	lnetoGatewayRetries   = 3
	lnetoDefaultMaxPorts  = 16
	lnetoDefaultMaxConns  = 32
	lnetoDefaultTCPQueue  = 8
	lnetoDefaultTCPBufMul = 3
)

var errDialAwaitingSyn = errors.New("network: tcp dial awaiting syn")

type tcpPeer struct {
	ip  [4]byte
	mac [6]byte
}

// lnetoEngine owns an xnet.StackAsync and serializes every stack mutation,
// ingress/egress step, listener close, and connection abort behind one mutex.
type lnetoEngine struct {
	mu sync.Mutex

	hostname         string
	maxTCPPorts      uint16
	maxListenerConns uint16
	tcpBufSize       int
	tcpQueueSize     int
	rawBackoff       lneto.BackoffStrategy
	tcpBackoff       func() lneto.BackoffStrategy
	backoffIRQ       chan struct{}

	stack        xnet.StackAsync
	subnet       netip.Prefix
	tcpPeers     []tcpPeer
	plcfg        xnet.TCPPoolConfig
	ephemeralOff uint32
	notify       atomic.Pointer[func([]byte)]
	goroutineID  atomic.Uint32
	totalSent    atomic.Uint64
}

var _ gnet.Stack = (*lnetoEngine)(nil)

func newLnetoEngine(cfg *gnet.LnetoConfig) *lnetoEngine {
	defaults := gnet.DefaultLnetoStackConfig()
	if cfg == nil {
		cfg = defaults
	}
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = defaults.Hostname
		if hostname == "" {
			hostname = "gonet-lneto"
		}
	}
	maxTCPPorts := cfg.MaxActiveTCPPorts
	if maxTCPPorts == 0 {
		maxTCPPorts = defaults.MaxActiveTCPPorts
		if maxTCPPorts == 0 {
			maxTCPPorts = lnetoDefaultMaxPorts
		}
	}
	maxListenerConns := cfg.MaxListenerConns
	if maxListenerConns == 0 {
		maxListenerConns = defaults.MaxListenerConns
		if maxListenerConns == 0 {
			maxListenerConns = lnetoDefaultMaxConns
		}
	}
	tcpBufSize := cfg.TCPBufferSize
	if tcpBufSize <= 0 {
		tcpBufSize = defaults.TCPBufferSize
		if tcpBufSize <= 0 {
			tcpBufSize = lnetoDefaultTCPBufMul * (gnet.MTU - 40)
		}
	}
	tcpQueueSize := cfg.TCPQueueSize
	if tcpQueueSize <= 0 {
		tcpQueueSize = defaults.TCPQueueSize
		if tcpQueueSize <= 0 {
			tcpQueueSize = lnetoDefaultTCPQueue
		}
	}
	stackBackoff := cfg.BackoffStack
	if stackBackoff == nil {
		if runtime.GOMAXPROCS(0) == 1 {
			stackBackoff = lnetoYieldBackoff
		} else {
			stackBackoff = lnetoDefaultStackBackoff
		}
	}
	tcpBackoff := cfg.NewBackoffTCP
	if tcpBackoff == nil {
		if runtime.GOMAXPROCS(0) == 1 {
			tcpBackoff = func() lneto.BackoffStrategy { return lnetoYieldBackoff }
		} else {
			tcpBackoff = lnetoDefaultNewTCPBackoff
		}
	}
	return &lnetoEngine{
		hostname:         hostname,
		maxTCPPorts:      maxTCPPorts,
		maxListenerConns: maxListenerConns,
		tcpBufSize:       tcpBufSize,
		tcpQueueSize:     tcpQueueSize,
		rawBackoff:       stackBackoff,
		tcpBackoff:       tcpBackoff,
		backoffIRQ:       make(chan struct{}, 1),
	}
}

func (e *lnetoEngine) Configure(mac net.HardwareAddr, ip netip.Prefix, gw netip.Addr) error {
	if len(mac) != 6 {
		return errors.New("network: only 6-byte MAC address supported")
	}
	rnd := make([]byte, 8)
	if _, err := rand.Read(rnd); err != nil {
		return fmt.Errorf("network: seed stack PRNG: %w", err)
	}
	cfg := xnet.StackConfig{
		RandSeed:          int64(binary.LittleEndian.Uint64(rnd)),
		MaxActiveTCPPorts: e.maxTCPPorts,
		Hostname:          e.hostname,
		HardwareAddress:   [6]byte(mac),
		MTU:               uint16(gnet.MTU),
		ICMPQueueLimit:    32,
	}
	if ip.Addr().Is4() {
		cfg.StaticAddress4 = ip.Addr().As4()
	} else if ip.Addr().Is6() {
		cfg.StaticAddress6 = ip.Addr().As16()
		cfg.IPv6Stack = xnet.DefaultStack6()
	}

	var hostCtl xnet.DHCPResults
	hostCtl.Subnet = ip

	e.mu.Lock()
	err := e.stack.Reset(cfg)
	if err == nil {
		err = e.stack.AssimilateDHCPResults(&hostCtl)
	}
	if err == nil {
		e.subnet = ip
		e.tcpPeers = make([]tcpPeer, e.maxListenerConns)
		e.ephemeralOff = e.stack.Prand32() % 16384
	}
	e.plcfg = xnet.TCPPoolConfig{
		PoolSize:           e.maxListenerConns,
		QueueSize:          e.tcpQueueSize,
		TxBufSize:          e.tcpBufSize,
		RxBufSize:          e.tcpBufSize,
		EstablishedTimeout: lnetoEstablishedTO,
		ClosingTimeout:     lnetoClosingTO,
		NewBackoff:         e.tcpBackoff,
	}
	e.mu.Unlock()
	if err != nil {
		return fmt.Errorf("network: configure lneto stack: %w", err)
	}

	id := e.goroutineID.Add(1)
	e.interruptBackoff()
	go e.lifetimeGoroutine(id)
	if gw.IsValid() && gw.Is4() {
		go e.resolveSetGateway(id, gw)
	}
	return nil
}

func (e *lnetoEngine) stop() {
	e.goroutineID.Add(1)
	e.interruptBackoff()
}

func (e *lnetoEngine) HardwareAddress() (net.HardwareAddr, error) {
	e.mu.Lock()
	hw := e.stack.HardwareAddr()
	e.mu.Unlock()
	return hw[:], nil
}

func (e *lnetoEngine) EnableICMP() error {
	e.mu.Lock()
	err := e.stack.EnableICMP(true)
	e.mu.Unlock()
	return err
}

func (e *lnetoEngine) SetWriteNotify(cb func(auxbuf []byte)) {
	if cb == nil {
		e.notify.Store(nil)
		return
	}
	e.notify.Store(&cb)
}

func (e *lnetoEngine) WriteOutboundPacket(buf []byte) (int, error) {
	e.mu.Lock()
	n, err := e.stack.EgressEthernet(buf)
	if n > 0 {
		e.routeTCPReply(buf[:n])
	}
	e.mu.Unlock()
	if n == 0 && (errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)) {
		// lneto's tcp.Handler.Send returns io.EOF (after h.Abort) when a closing
		// listener connection already terminated, and tcp.Listener.Encapsulate
		// only suppresses net.ErrClosed. Treat per-connection close/EOF as benign
		// so one closed socket cannot fail the entire Link.
		e.interruptBackoff()
		return 0, nil
	}
	if n > 0 {
		e.totalSent.Add(1)
		// Only wake lifetimeGoroutine when a packet was actually dequeued so
		// idle polls back off cleanly under GOMAXPROCS=1 cooperative scheduling.
		e.interruptBackoff()
	}
	return n, err
}

func (e *lnetoEngine) RecvInboundPacket(buf []byte) error {
	e.mu.Lock()
	err := e.stack.IngressEthernet(buf)
	if err == nil {
		e.learnTCPPeer(buf)
	}
	e.mu.Unlock()
	e.interruptBackoff()
	return err
}

func tcpIPv4Frame(b []byte) (ethernet.Frame, ipv4.Frame, bool) {
	eth, err := ethernet.NewFrame(b)
	if err != nil || eth.EtherTypeOrSize() != ethernet.TypeIPv4 {
		return eth, ipv4.Frame{}, false
	}
	ip, err := ipv4.NewFrame(eth.Payload())
	return eth, ip, err == nil && ip.Protocol() == lneto.IPProtoTCP
}

// lneto gives listener replies the gateway MAC, initially broadcast. Its
// passive table never evicts old clients. Remember recently accepted on-link
// TCP traffic so a new host can still connect after the listener pool turns over.
func (e *lnetoEngine) learnTCPPeer(frame []byte) {
	eth, ip, ok := tcpIPv4Frame(frame)
	if !ok || netip.AddrFrom4(*ip.DestinationAddr()) != e.subnet.Addr() {
		return
	}
	source := netip.AddrFrom4(*ip.SourceAddr())
	mac := *eth.SourceHardwareAddr()
	if !e.subnet.Contains(source) || !usableHost(e.subnet, source) || mac == ([6]byte{}) || mac[0]&1 != 0 {
		return
	}
	peer := tcpPeer{ip: *ip.SourceAddr(), mac: mac}
	for i, old := range e.tcpPeers {
		if old.ip == peer.ip || old.ip == ([4]byte{}) || i == len(e.tcpPeers)-1 {
			copy(e.tcpPeers[1:i+1], e.tcpPeers[:i])
			e.tcpPeers[0] = peer
			return
		}
	}
}

func (e *lnetoEngine) routeTCPReply(frame []byte) {
	eth, ip, ok := tcpIPv4Frame(frame)
	if !ok || *eth.DestinationHardwareAddr() != e.stack.GatewayHardwareAddr() {
		return
	}
	for _, peer := range e.tcpPeers {
		if peer.ip == *ip.DestinationAddr() {
			*eth.DestinationHardwareAddr() = peer.mac
			return
		}
	}
}

func (e *lnetoEngine) closeListener(inner *tcp.Listener) error {
	e.mu.Lock()
	err := inner.Close()
	e.mu.Unlock()
	return err
}

func (e *lnetoEngine) abortConnIfID(conn *tcp.Conn, want uint64) {
	if conn == nil {
		return
	}
	e.mu.Lock()
	if want == 0 {
		conn.Abort()
	} else if idPtr := conn.ConnectionID(); idPtr != nil && *idPtr == want {
		conn.Abort()
	}
	e.mu.Unlock()
	e.interruptBackoff()
}

// connLease pins an accepted *tcp.Conn so lneto's TCPPool cannot reuse the
// slot until both lneto's PutTCP and the *lnetoConn wrapper are done with it.
type connLease struct {
	pool         *pinnedTCPPool
	conn         *tcp.Conn
	mu           sync.Mutex
	activeOps    int
	poolReturned bool
	wrapperDone  bool
	released     bool
}

type pinnedTCPPool struct {
	inner  *xnet.TCPPool
	mu     sync.Mutex
	leases map[*tcp.Conn]*connLease
}

func newPinnedTCPPool(inner *xnet.TCPPool) *pinnedTCPPool {
	return &pinnedTCPPool{
		inner:  inner,
		leases: make(map[*tcp.Conn]*connLease),
	}
}

func (p *pinnedTCPPool) GetTCP() (*tcp.Conn, any, tcp.Value) {
	return p.inner.GetTCP()
}

func (p *pinnedTCPPool) pin(conn *tcp.Conn) *connLease {
	lease := &connLease{pool: p, conn: conn}
	p.mu.Lock()
	p.leases[conn] = lease
	p.mu.Unlock()
	return lease
}

func (p *pinnedTCPPool) PutTCP(conn *tcp.Conn) {
	p.mu.Lock()
	lease := p.leases[conn]
	p.mu.Unlock()
	if lease == nil {
		p.inner.PutTCP(conn)
		return
	}
	// Abort immediately so any active Read/Write wakes up, while keeping
	// the pooled conn out of inner until the wrapper closes and drains ops.
	conn.Abort()
	lease.mu.Lock()
	lease.poolReturned = true
	shouldRelease := lease.wrapperDone && lease.activeOps == 0 && !lease.released
	if shouldRelease {
		lease.released = true
	}
	lease.mu.Unlock()
	if shouldRelease {
		p.release(lease)
	}
}

func (p *pinnedTCPPool) release(lease *connLease) {
	p.mu.Lock()
	if p.leases[lease.conn] == lease {
		delete(p.leases, lease.conn)
	}
	p.mu.Unlock()
	p.inner.PutTCP(lease.conn)
}

func (l *connLease) beginOp() bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.wrapperDone || l.poolReturned {
		return false
	}
	l.activeOps++
	return true
}

func (l *connLease) endOp() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.activeOps--
	shouldRelease := l.wrapperDone && l.poolReturned && l.activeOps == 0 && !l.released
	if shouldRelease {
		l.released = true
	}
	l.mu.Unlock()
	if shouldRelease {
		l.pool.release(l)
	}
}

func (l *connLease) beginClose() (alreadyReturned bool) {
	if l == nil {
		return false
	}
	l.mu.Lock()
	l.wrapperDone = true
	if l.poolReturned {
		shouldRelease := l.activeOps == 0 && !l.released
		if shouldRelease {
			l.released = true
		}
		l.mu.Unlock()
		if shouldRelease {
			l.pool.release(l)
		}
		return true
	}
	l.activeOps++
	l.mu.Unlock()
	return false
}

func (e *lnetoEngine) tryAccept(ln *tcp.Listener, pool *pinnedTCPPool) (*tcp.Conn, uint64, net.Addr, *connLease, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ln.NumberOfReadyToAccept() == 0 {
		return nil, 0, nil, nil, nil
	}
	c, _, err := ln.TryAccept()
	if err != nil {
		return nil, 0, nil, nil, err
	}
	var connID uint64
	if idPtr := c.ConnectionID(); idPtr != nil {
		connID = *idPtr
	}
	remoteAddr := &net.TCPAddr{
		IP:   append([]byte(nil), c.RemoteAddr()...),
		Port: int(c.RemotePort()),
	}
	var lease *connLease
	if pool != nil {
		lease = pool.pin(c)
	}
	return c, connID, remoteAddr, lease, nil
}

func (e *lnetoEngine) canCloseConn(conn *tcp.Conn, want uint64) bool {
	if conn == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if want != 0 {
		idPtr := conn.ConnectionID()
		if idPtr == nil || *idPtr != want {
			return false
		}
	}
	return conn.State().TxDataOpen()
}

func (e *lnetoEngine) nextEphemeralPortLocked() uint16 {
	port := uint16(49152 + e.ephemeralOff)
	e.ephemeralOff = (e.ephemeralOff + 1) % 16384
	return port
}

func (e *lnetoEngine) listenTCP(port uint16) (*tcp.Listener, *pinnedTCPPool, netip.AddrPort, error) {
	e.mu.Lock()
	plcfg := e.plcfg
	e.mu.Unlock()

	pool, err := xnet.NewTCPPool(plcfg)
	if err != nil {
		return nil, nil, netip.AddrPort{}, err
	}
	pinned := newPinnedTCPPool(pool)
	ln := new(tcp.Listener)

	e.mu.Lock()
	if port == 0 {
		port = e.nextEphemeralPortLocked()
	}
	err = ln.Reset(port, pinned)
	if err == nil {
		err = e.stack.RegisterListenerTCP(ln)
	}
	ip4 := netip.AddrFrom4(e.stack.Addr4())
	e.mu.Unlock()
	if err != nil {
		return nil, nil, netip.AddrPort{}, err
	}
	e.interruptBackoff()
	return ln, pinned, netip.AddrPortFrom(ip4, port), nil
}

func (e *lnetoEngine) dialTCP(ctx context.Context, remote netip.AddrPort, requestedLocalPort uint16) (*tcp.Conn, netip.AddrPort, uint64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, netip.AddrPort{}, 0, err
	}
	e.mu.Lock()
	plcfg := e.plcfg
	e.mu.Unlock()

	deadline := time.Now().Add(lnetoDialTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	for time.Now().Before(deadline) && ctx.Err() == nil {
		conn := new(tcp.Conn)
		err := conn.Configure(tcp.ConnConfig{
			TxBuf:             make([]byte, plcfg.TxBufSize),
			RxBuf:             make([]byte, plcfg.RxBufSize),
			TxPacketQueueSize: plcfg.QueueSize,
			RWBackoff:         plcfg.NewBackoff(),
		})
		if err != nil {
			return nil, netip.AddrPort{}, 0, err
		}

		localPort := requestedLocalPort
		var localIP netip.Addr
		var connID uint64
		e.mu.Lock()
		localIP = netip.AddrFrom4(e.stack.Addr4())
		if requestedLocalPort == 0 {
			for attempt := 0; attempt < lnetoMaxPortRetries; attempt++ {
				localPort = e.nextEphemeralPortLocked()
				err = e.stack.DialTCP(conn, localPort, remote)
				if err == nil || !errors.Is(err, lneto.ErrAlreadyRegistered) {
					break
				}
			}
		} else {
			err = e.stack.DialTCP(conn, localPort, remote)
		}
		if err == nil {
			if idPtr := conn.ConnectionID(); idPtr != nil {
				connID = *idPtr
			}
		}
		e.mu.Unlock()
		if err != nil {
			return nil, netip.AddrPort{}, 0, err
		}
		e.interruptBackoff()

		err = e.waitDialTCP(ctx, conn, deadline)
		if err == nil {
			return conn, netip.AddrPortFrom(localIP, localPort), connID, nil
		}
		if errors.Is(err, errDialAwaitingSyn) {
			e.mu.Lock()
			if connID == 0 {
				conn.Abort()
			} else if idPtr := conn.ConnectionID(); idPtr != nil && *idPtr == connID {
				conn.Abort()
			}
			if _, arpErr := e.stack.ResultResolveHardwareAddress6(remote.Addr()); arpErr != nil {
				_ = e.stack.DiscardResolveHardwareAddress6(remote.Addr())
			}
			e.mu.Unlock()
			e.interruptBackoff()
			continue
		}
		e.abortConnIfID(conn, connID)
		return nil, netip.AddrPort{}, 0, err
	}
	if err := ctx.Err(); err != nil {
		return nil, netip.AddrPort{}, 0, err
	}
	return nil, netip.AddrPort{}, 0, context.DeadlineExceeded
}

func (e *lnetoEngine) waitDialTCP(ctx context.Context, conn *tcp.Conn, deadline time.Time) error {
	attemptStart := time.Now()
	lastRequeue := attemptStart
	synSent := false
	for step := uint(0); ; step++ {
		// Read AwaitingSynSend before State so a concurrent initial SYN emission
		// cannot flip AwaitingSynSend false while state still holds StateClosed.
		awaitingSyn := conn.AwaitingSynSend()
		state := conn.State()
		switch {
		case state == tcp.StateEstablished || state == tcp.StateCloseWait:
			return nil
		case state == tcp.StateSynSent || state == tcp.StateSynRcvd:
			synSent = true
		case !synSent && awaitingSyn:
			if time.Since(attemptStart) >= lnetoARPRetryInterval && time.Now().Before(deadline) {
				return errDialAwaitingSyn
			}
		default:
			return fmt.Errorf("network: tcp failed to connect (state=%s awaiting=%v)", state, awaitingSyn)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		if time.Since(lastRequeue) >= lnetoRequeueInterval {
			if conn.IsAwaitingControl() {
				conn.RequeueControl()
				e.interruptBackoff()
			}
			lastRequeue = time.Now()
		}
		if err := e.waitDialStep(ctx, step, deadline); err != nil {
			return err
		}
	}
}

func (e *lnetoEngine) waitDialStep(ctx context.Context, step uint, deadline time.Time) error {
	if !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d := e.rawBackoff(step)
	switch d {
	case lneto.BackoffFlagGosched:
		runtime.Gosched()
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		return nil
	case lneto.BackoffFlagNop:
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		return nil
	default:
		if d > 5*time.Millisecond {
			d = 5 * time.Millisecond
		}
		if rem := time.Until(deadline); rem < d {
			d = rem
		}
		if d <= 0 {
			return context.DeadlineExceeded
		}
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-e.backoffIRQ:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case <-timer.C:
			return nil
		}
	}
}

func (e *lnetoEngine) Socket(ctx context.Context, network string, family, sotype int, laddr, raddr net.Addr) (any, error) {
	if family != syscall.AF_INET || sotype != syscall.SOCK_STREAM {
		return nil, lneto.ErrUnsupported
	}
	switch network {
	case "tcp", "tcp4":
	default:
		return nil, lneto.ErrUnsupported
	}
	var localPort uint16
	if laddr != nil {
		local, ok := tcpAddrPort(laddr)
		if !ok {
			return nil, lneto.ErrInvalidAddr
		}
		localPort = local.Port()
	}

	if raddr != nil {
		remote, ok := tcpAddrPort(raddr)
		if !ok {
			return nil, lneto.ErrInvalidAddr
		}
		if remote.IsValid() && !remote.Addr().IsUnspecified() {
			conn, local, connID, err := e.dialTCP(ctx, remote, localPort)
			if err != nil {
				return nil, err
			}
			return &lnetoConn{
				Conn:       conn,
				localAddr:  net.TCPAddrFromAddrPort(local),
				remoteAddr: net.TCPAddrFromAddrPort(remote),
				engine:     e,
				connID:     connID,
			}, nil
		}
	}

	ln, pool, local, err := e.listenTCP(localPort)
	if err != nil {
		return nil, err
	}
	return &lnetoListener{
		inner:     ln,
		pool:      pool,
		localAddr: net.TCPAddrFromAddrPort(local),
		engine:    e,
		closeCh:   make(chan struct{}),
	}, nil
}

func tcpAddrPort(addr net.Addr) (netip.AddrPort, bool) {
	ta, ok := addr.(*net.TCPAddr)
	if !ok || ta == nil {
		return netip.AddrPort{}, false
	}
	if ta.Port < 0 || ta.Port > 65535 {
		return netip.AddrPort{}, false
	}
	if len(ta.IP) == 0 {
		return netip.AddrPortFrom(netip.IPv4Unspecified(), uint16(ta.Port)), true
	}
	ip, ok := netip.AddrFromSlice(ta.IP)
	if !ok {
		return netip.AddrPort{}, false
	}
	ip = ip.Unmap()
	if !ip.Is4() {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ip, uint16(ta.Port)), true
}

func (e *lnetoEngine) interruptBackoff() {
	select {
	case e.backoffIRQ <- struct{}{}:
	default:
	}
}

func (e *lnetoEngine) waitBackoff(consecutive uint) {
	d := e.rawBackoff(consecutive)
	switch d {
	case lneto.BackoffFlagGosched:
		runtime.Gosched()
	case lneto.BackoffFlagNop:
	default:
		timer := time.NewTimer(d)
		select {
		case <-e.backoffIRQ:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (e *lnetoEngine) lifetimeGoroutine(id uint32) {
	writeBuf := make([]byte, gnet.MTU+gnet.EthernetMaximumSize)
	var backoffs uint
	for id == e.goroutineID.Load() {
		before := e.totalSent.Load()
		if cb := e.notify.Load(); cb != nil && *cb != nil && id == e.goroutineID.Load() {
			(*cb)(writeBuf)
		}
		if id != e.goroutineID.Load() {
			break
		}
		if e.totalSent.Load() != before {
			backoffs = 0
			runtime.Gosched()
			continue
		}
		e.waitBackoff(backoffs)
		backoffs++
	}
}

func (e *lnetoEngine) resolveSetGateway(id uint32, gw netip.Addr) {
	for attempt := 0; attempt < lnetoGatewayRetries && id == e.goroutineID.Load(); attempt++ {
		e.mu.Lock()
		err := e.stack.StartResolveHardwareAddress6(gw)
		e.mu.Unlock()
		if err != nil {
			e.waitBackoff(uint(attempt))
			continue
		}
		e.interruptBackoff()
		deadline := time.Now().Add(lnetoGatewayTimeout)
		var backoffs uint
		for time.Now().Before(deadline) && id == e.goroutineID.Load() {
			e.mu.Lock()
			hw, resErr := e.stack.ResultResolveHardwareAddress6(gw)
			if resErr == nil {
				e.stack.SetGatewayHardwareAddr(hw)
				_ = e.stack.DiscardResolveHardwareAddress6(gw)
				e.mu.Unlock()
				return
			}
			e.mu.Unlock()
			e.waitBackoff(backoffs)
			backoffs++
		}
		e.mu.Lock()
		_ = e.stack.DiscardResolveHardwareAddress6(gw)
		e.mu.Unlock()
	}
}

func lnetoDefaultStackBackoff(consecutive uint) time.Duration {
	if consecutive == 0 {
		return lneto.BackoffFlagGosched
	}
	return min(time.Duration(consecutive)*time.Millisecond, 25*time.Millisecond)
}

func lnetoDefaultNewTCPBackoff() lneto.BackoffStrategy {
	return func(consecutive uint) time.Duration {
		if consecutive < 4 {
			return lneto.BackoffFlagGosched
		}
		return min(time.Duration(consecutive-3)*time.Millisecond, 10*time.Millisecond)
	}
}

func lnetoYieldBackoff(uint) time.Duration {
	return lneto.BackoffFlagGosched
}
