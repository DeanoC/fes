// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// RFC 2131 §4.4.5 recommends waiting half of the remaining time until T2 or
	// lease expiration, down to a minimum of 60 seconds, between renewal retries.
	defaultRenewMinInterval = 60 * time.Second
	// Short pause-aware retry used when SendRawFrame reports ErrPaused (e.g. during
	// FPGA reconfiguration) so no request was actually transmitted on the wire.
	defaultRenewPausedRetry = 250 * time.Millisecond
)

var (
	// ErrLeaseNAK is returned when a DHCP server rejects renewal/rebind with DHCPNAK.
	ErrLeaseNAK = errors.New("network: DHCP renewal rejected with NAK")
	// ErrLeaseChanged is returned when renewal yields a different IP prefix or gateway
	// than the currently configured IP stack address.
	ErrLeaseChanged = errors.New("network: DHCP renewal changed IP address or gateway")
	// ErrLeaseExpired is returned when neither T1 renewal nor T2 rebind succeeded before expiry.
	ErrLeaseExpired = errors.New("network: DHCP lease expired")
	// ErrRenewerStarted is returned when LeaseRenewer.Start is called more than once.
	ErrRenewerStarted = errors.New("network: DHCP lease renewer already started")
	// ErrRenewerStopped is returned when LeaseRenewer.Start is called after Stop.
	ErrRenewerStopped = errors.New("network: DHCP lease renewer stopped")
	// ErrRenewerActive is returned when another LeaseRenewer already owns the Interface or Link.
	ErrRenewerActive = errors.New("network: DHCP lease renewer already active")
)

type renewerClaimer interface {
	claimRenewer() bool
	releaseRenewer()
}

// DHCPFramePort is the minimal raw Ethernet frame interface used by DHCP renewal
// over an active Link (satisfied by *Link).
type DHCPFramePort interface {
	SendRawFrame(frame []byte) error
	AddTap(fn func(frame []byte)) (remove func())
	Err() error
}

var _ DHCPFramePort = (*Link)(nil)

// RenewConfig configures RFC 2131 DHCPv4 T1/T2 lease renewal.
type RenewConfig struct {
	Hostname      string          // Optional option 12 hostname (default "fes-native")
	ServerMAC     string          // Optional unicast DHCP server MAC for T1 renew (empty -> broadcast)
	RetryInterval time.Duration   // Minimum retransmit floor for RFC 2131 §4.4.5 remaining-time pacing (default 60s)
	AcquiredAt    time.Time       // Time the initial lease was granted (zero -> time.Now() at NewLeaseRenewer)
	XID           uint32          // Optional fixed transaction ID for deterministic tests (0 -> random)
	OnRenew       func(DHCPLease) // Optional callback invoked after each successful renewal; must not call Stop synchronously.
	OnError       func(error)     // Optional callback invoked when renewal terminates with error; must not call Stop synchronously.
}

// RenewDHCPOnce executes one RFC 2131 RENEWING (rebind=false) or REBINDING
// (rebind=true) DHCPREQUEST -> DHCPACK/DHCPNAK exchange over port.
func RenewDHCPOnce(ctx context.Context, port DHCPFramePort, macStr string, current DHCPLease, rebind bool, cfg RenewConfig) (DHCPLease, error) {
	lease, _, err := renewDHCPOnceWithTime(ctx, port, macStr, current, rebind, cfg)
	return lease, err
}

func renewDHCPOnceWithTime(ctx context.Context, port DHCPFramePort, macStr string, current DHCPLease, rebind bool, cfg RenewConfig) (DHCPLease, time.Time, error) {
	if ctx == nil || port == nil {
		return DHCPLease{}, time.Time{}, fmt.Errorf("%w: nil context or port", ErrDHCPInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return DHCPLease{}, time.Time{}, err
	}
	mac, err := parseUnicastMAC(macStr)
	if err != nil {
		return DHCPLease{}, time.Time{}, fmt.Errorf("%w: %v", ErrDHCPInvalidConfig, err)
	}
	if err := validateRenewConfig(cfg); err != nil {
		return DHCPLease{}, time.Time{}, err
	}
	if _, err := staticConfig(current.Address, current.Gateway); err != nil {
		return DHCPLease{}, time.Time{}, fmt.Errorf("%w: invalid current lease: %v", ErrDHCPInvalidConfig, err)
	}
	prefix := netip.MustParsePrefix(current.Address)
	serverIP, err := netip.ParseAddr(current.Server)
	if err != nil || !serverIP.Is4() || serverIP.IsUnspecified() || serverIP.IsMulticast() {
		return DHCPLease{}, time.Time{}, fmt.Errorf("%w: invalid lease server %q", ErrDHCPInvalidConfig, current.Server)
	}

	var dstMAC [6]byte
	var dstIP [4]byte
	useBroadcast := rebind || cfg.ServerMAC == ""
	if useBroadcast {
		dstMAC = broadcastMAC
		dstIP = [4]byte{255, 255, 255, 255}
	} else {
		dstMAC, err = parseUnicastMAC(cfg.ServerMAC)
		if err != nil {
			return DHCPLease{}, time.Time{}, fmt.Errorf("%w: invalid ServerMAC: %v", ErrDHCPInvalidConfig, err)
		}
		dstIP = serverIP.As4()
	}

	hostname := cfg.Hostname
	if hostname == "" {
		hostname = "fes-native"
	}
	minInterval := cfg.RetryInterval
	if minInterval == 0 {
		minInterval = defaultRenewMinInterval
	}
	xid := cfg.XID
	if xid == 0 {
		xid = randomXID()
	}

	txFrame := buildDHCPRenewFrame(mac, dstMAC, prefix.Addr().As4(), dstIP, xid, hostname, useBroadcast)
	expectedServer := serverIP.As4()

	type renewResult struct {
		lease DHCPLease
		err   error
	}
	replyCh := make(chan renewResult, 1)
	remove := port.AddTap(func(frame []byte) {
		reply, ok := parseDHCPFrame(frame, mac, xid)
		if !ok {
			return
		}
		switch reply.msgType {
		case dhcpMsgNak:
			if !rebind && reply.hasServer && reply.serverID != expectedServer {
				return
			}
			select {
			case replyCh <- renewResult{err: ErrLeaseNAK}:
			default:
			}
		case dhcpMsgAck:
			if !rebind && reply.hasServer && reply.serverID != expectedServer {
				return
			}
			lease, ok := mergeRenewedLease(current, reply)
			if !ok {
				return
			}
			if lease.Address != current.Address || lease.Gateway != current.Gateway {
				select {
				case replyCh <- renewResult{err: ErrLeaseChanged}:
				default:
				}
				return
			}
			select {
			case replyCh <- renewResult{lease: lease}:
			default:
			}
		}
	})
	defer remove()

	var requestSentAt time.Time
	for {
		if err := ctx.Err(); err != nil {
			return DHCPLease{}, time.Time{}, err
		}
		if err := port.Err(); err != nil {
			return DHCPLease{}, time.Time{}, err
		}
		attemptAt := time.Now()
		paused := false
		if err := port.SendRawFrame(txFrame); err != nil {
			// If traffic is temporarily paused (e.g., FPGA reconfiguration), wait briefly
			// and retry rather than applying RFC 2131 unanswered-request backoff.
			if !errors.Is(err, ErrPaused) {
				return DHCPLease{}, time.Time{}, err
			}
			paused = true
		} else if requestSentAt.IsZero() {
			// RFC 2131 §4.4.5: anchor renewed T1/T2/expiration conservatively to when
			// the DHCPREQUEST for this XID was first transmitted.
			requestSentAt = attemptAt
		}
		var wait time.Duration
		if paused {
			pauseRetry := defaultRenewPausedRetry
			if minInterval < pauseRetry {
				pauseRetry = minInterval
			}
			wait = boundedWait(ctx, pauseRetry)
		} else {
			wait = nextRenewWait(ctx, minInterval)
		}
		if wait <= 0 {
			if err := ctx.Err(); err != nil {
				return DHCPLease{}, time.Time{}, err
			}
			return DHCPLease{}, time.Time{}, context.DeadlineExceeded
		}
		timer := time.NewTimer(wait)
		select {
		case res := <-replyCh:
			timer.Stop()
			if requestSentAt.IsZero() {
				requestSentAt = attemptAt
			}
			return res.lease, requestSentAt, res.err
		case <-ctx.Done():
			timer.Stop()
			return DHCPLease{}, time.Time{}, ctx.Err()
		case <-timer.C:
		}
	}
}

// boundedWait clamps d to the remaining context deadline if one is set.
func boundedWait(ctx context.Context, d time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return d
	}
	rem := time.Until(deadline)
	if rem <= 0 {
		return 0
	}
	if d > rem {
		return rem
	}
	return d
}

// nextRenewWait implements RFC 2131 §4.4.5 retransmission pacing: half of the
// remaining phase duration down to minInterval (default 60s), never exceeding
// the remaining deadline.
func nextRenewWait(ctx context.Context, minInterval time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return minInterval
	}
	rem := time.Until(deadline)
	if rem <= 0 {
		return 0
	}
	wait := rem / 2
	if wait < minInterval {
		wait = minInterval
	}
	if wait > rem {
		wait = rem
	}
	return wait
}

// LeaseRenewer runs an opt-in RFC 2131 T1/T2 DHCPv4 lease renewal loop.
// Creating a LeaseRenewer starts no background work until Start is called.
type LeaseRenewer struct {
	port        DHCPFramePort
	mac         string
	cfg         RenewConfig
	acquiredAt  time.Time
	onClaim     func() error
	onUpdate    func(DHCPLease, time.Time)
	onTerminal  func(error)
	onRelease   func()
	releaseOnce sync.Once

	mu       sync.Mutex
	started  bool
	stopped  bool
	lease    DHCPLease
	lastErr  error
	stopCh   chan struct{}
	wg       sync.WaitGroup
	running  atomic.Bool
	renewals atomic.Uint64
}

// NewLeaseRenewer validates inputs and returns an idle LeaseRenewer.
// If port enforces single-renewer ownership (such as *Link), ownership is
// claimed on Start and released automatically when the renewal loop exits.
func NewLeaseRenewer(port DHCPFramePort, mac string, initial DHCPLease, cfg RenewConfig) (*LeaseRenewer, error) {
	if port == nil {
		return nil, fmt.Errorf("%w: nil frame port", ErrDHCPInvalidConfig)
	}
	if _, err := parseUnicastMAC(mac); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDHCPInvalidConfig, err)
	}
	if _, err := staticConfig(initial.Address, initial.Gateway); err != nil {
		return nil, fmt.Errorf("%w: invalid initial lease: %v", ErrDHCPInvalidConfig, err)
	}
	serverIP, err := netip.ParseAddr(initial.Server)
	if err != nil || !serverIP.Is4() || serverIP.IsUnspecified() || serverIP.IsMulticast() {
		return nil, fmt.Errorf("%w: invalid lease server %q", ErrDHCPInvalidConfig, initial.Server)
	}
	if initial.LeaseDuration <= 0 {
		return nil, fmt.Errorf("%w: lease duration must be > 0", ErrDHCPInvalidConfig)
	}
	if err := validateRenewConfig(cfg); err != nil {
		return nil, err
	}
	var onClaim func() error
	var onRelease func()
	if claimer, ok := port.(renewerClaimer); ok {
		onClaim = func() error {
			if !claimer.claimRenewer() {
				return ErrRenewerActive
			}
			return nil
		}
		onRelease = claimer.releaseRenewer
	}
	acquiredAt := cfg.AcquiredAt
	if acquiredAt.IsZero() {
		acquiredAt = time.Now()
	}
	return &LeaseRenewer{
		port:       port,
		mac:        mac,
		cfg:        cfg,
		acquiredAt: acquiredAt,
		onClaim:    onClaim,
		onRelease:  onRelease,
		lease:      cloneLease(initial),
		stopCh:     make(chan struct{}),
	}, nil
}

// Start launches the background T1/T2 renewal goroutine. Fails closed if already
// started, previously stopped, or another renewer is already active on the same
// Interface or Link. Pre-start rejections never leak ownership.
func (r *LeaseRenewer) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrDHCPInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return ErrRenewerStopped
	}
	if r.started {
		return ErrRenewerStarted
	}
	if r.onClaim != nil {
		if err := r.onClaim(); err != nil {
			return err
		}
	}
	r.started = true
	r.running.Store(true)
	r.wg.Add(1)
	go r.loop(ctx)
	return nil
}

// Stop halts background renewal and waits for the goroutine (including any
// in-flight OnRenew/OnError callback) to exit. Idempotent. Callbacks must not
// call Stop synchronously.
func (r *LeaseRenewer) Stop() {
	r.mu.Lock()
	if !r.stopped {
		r.stopped = true
		close(r.stopCh)
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *LeaseRenewer) release() {
	r.releaseOnce.Do(func() {
		if r.onRelease != nil {
			r.onRelease()
		}
	})
}

// interfaceLease coordinates DHCP lease state and single-renewer ownership for Interface.
type interfaceLease struct {
	mu            sync.RWMutex
	lease         DHCPLease
	acquiredAt    time.Time
	hasLease      bool
	generation    uint64
	activeRenewer *LeaseRenewer
}

func (s *interfaceLease) init(lease DHCPLease, acquiredAt time.Time, hasLease bool) {
	s.mu.Lock()
	s.lease = cloneLease(lease)
	s.acquiredAt = acquiredAt
	s.hasLease = hasLease
	s.mu.Unlock()
}

func (s *interfaceLease) get() (DHCPLease, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.hasLease {
		return DHCPLease{}, false
	}
	return cloneLease(s.lease), true
}

func (s *interfaceLease) newRenewer(port DHCPFramePort, mac string, cfg RenewConfig, checkReady func() error, onTerminal func(error)) (*LeaseRenewer, error) {
	s.mu.RLock()
	hasLease := s.hasLease
	lease := cloneLease(s.lease)
	acquiredAt := s.acquiredAt
	initialGen := s.generation
	s.mu.RUnlock()
	if !hasLease {
		return nil, fmt.Errorf("%w: interface has no active DHCP lease", ErrDHCPInvalidConfig)
	}
	explicitAcquiredAt := !cfg.AcquiredAt.IsZero()
	if !explicitAcquiredAt {
		cfg.AcquiredAt = acquiredAt
	}
	ren, err := NewLeaseRenewer(port, mac, lease, cfg)
	if err != nil {
		return nil, err
	}
	portClaim := ren.onClaim
	portRelease := ren.onRelease
	ren.onClaim = func() error {
		if checkReady != nil {
			if err := checkReady(); err != nil {
				return err
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.hasLease {
			return fmt.Errorf("%w: interface has no active DHCP lease", ErrDHCPInvalidConfig)
		}
		if s.activeRenewer != nil {
			return ErrRenewerActive
		}
		if portClaim != nil {
			if err := portClaim(); err != nil {
				return err
			}
		}
		if s.generation != initialGen {
			ren.lease = cloneLease(s.lease)
			ren.acquiredAt = s.acquiredAt
		} else if !explicitAcquiredAt {
			ren.acquiredAt = s.acquiredAt
		}
		s.activeRenewer = ren
		return nil
	}
	ren.onUpdate = func(next DHCPLease, at time.Time) {
		s.mu.Lock()
		if s.activeRenewer == ren {
			s.lease = cloneLease(next)
			s.acquiredAt = at
			s.generation++
		}
		s.mu.Unlock()
	}
	ren.onTerminal = func(err error) {
		s.mu.RLock()
		owned := s.activeRenewer == ren
		s.mu.RUnlock()
		if owned && onTerminal != nil {
			onTerminal(err)
		}
	}
	ren.onRelease = func() {
		if portRelease != nil {
			portRelease()
		}
		s.mu.Lock()
		if s.activeRenewer == ren {
			s.activeRenewer = nil
		}
		s.mu.Unlock()
	}
	return ren, nil
}

// Running reports whether the background renewal goroutine is active.
func (r *LeaseRenewer) Running() bool { return r.running.Load() }

// Renewals reports the count of completed lease renewals since Start.
func (r *LeaseRenewer) Renewals() uint64 { return r.renewals.Load() }

// Lease returns a snapshot of the most recent active DHCPv4 lease.
func (r *LeaseRenewer) Lease() DHCPLease {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneLease(r.lease)
}

// Err returns the first terminal error that stopped renewal.
func (r *LeaseRenewer) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

func (r *LeaseRenewer) fail(err error) {
	r.mu.Lock()
	if r.lastErr == nil {
		r.lastErr = err
	}
	r.mu.Unlock()
	// RFC 2131 §4.4.5: stop network processing immediately when a lease expires,
	// is NAK'd, or changes address so the stack never transmits with an unleased IP.
	if r.onTerminal != nil {
		r.onTerminal(err)
	} else if f, ok := r.port.(interface{ fail(error) }); ok {
		f.fail(err)
	}
	if r.cfg.OnError != nil {
		r.cfg.OnError(err)
	}
}

func leaseTimers(l DHCPLease) (t1, t2 time.Duration) {
	t1 = l.LeaseDuration / 2
	t2 = (l.LeaseDuration * 7) / 8
	if l.RenewalTime > 0 && l.RenewalTime < l.LeaseDuration {
		t1 = l.RenewalTime
	}
	if l.RebindTime > 0 && l.RebindTime < l.LeaseDuration {
		t2 = l.RebindTime
	}
	if t2 < t1 {
		t1 = t2
	}
	if t1 <= 0 {
		t1 = time.Millisecond
	}
	if t2 <= 0 {
		t2 = l.LeaseDuration
	}
	return t1, t2
}

func (r *LeaseRenewer) loop(parent context.Context) {
	defer func() {
		r.release()
		r.running.Store(false)
		r.wg.Done()
	}()

	runCtx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() {
		select {
		case <-r.stopCh:
			cancel()
		case <-runCtx.Done():
		}
	}()

	acquiredAt := r.acquiredAt
	loopCfg := r.cfg
	for {
		current := r.Lease()
		t1, t2 := leaseTimers(current)
		t1At := acquiredAt.Add(t1)
		t2At := acquiredAt.Add(t2)
		expireAt := acquiredAt.Add(current.LeaseDuration)

		if !waitUntil(runCtx, t1At) {
			return
		}

		applyRenewal := func(nextLease DHCPLease, sentAt time.Time) {
			if nextLease.Server != current.Server {
				loopCfg.ServerMAC = ""
			}
			acquiredAt = sentAt
			r.mu.Lock()
			r.lease = cloneLease(nextLease)
			r.mu.Unlock()
			if r.onUpdate != nil {
				r.onUpdate(cloneLease(nextLease), acquiredAt)
			}
			r.renewals.Add(1)
			if r.cfg.OnRenew != nil {
				r.cfg.OnRenew(cloneLease(nextLease))
			}
		}

		// Phase 1: T1 RENEWING (unicast if ServerMAC known) until T2.
		t1Ctx, t1Cancel := context.WithDeadline(runCtx, t2At)
		nextLease, sentAt, err := renewDHCPOnceWithTime(t1Ctx, r.port, r.mac, current, false, loopCfg)
		t1Cancel()
		if err == nil {
			applyRenewal(nextLease, sentAt)
			continue
		}
		if runCtx.Err() != nil {
			return
		}
		if !errors.Is(err, context.DeadlineExceeded) || r.port.Err() != nil {
			if portErr := r.port.Err(); portErr != nil && errors.Is(err, context.DeadlineExceeded) {
				err = portErr
			}
			r.fail(err)
			return
		}

		// Phase 2: T2 REBINDING (broadcast) until lease expiry.
		t2Ctx, t2Cancel := context.WithDeadline(runCtx, expireAt)
		nextLease, sentAt, err = renewDHCPOnceWithTime(t2Ctx, r.port, r.mac, current, true, loopCfg)
		t2Cancel()
		if err == nil {
			applyRenewal(nextLease, sentAt)
			continue
		}
		if runCtx.Err() != nil {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			r.fail(ErrLeaseExpired)
		} else {
			r.fail(err)
		}
		return
	}
}

func waitUntil(ctx context.Context, target time.Time) bool {
	d := time.Until(target)
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func validateRenewConfig(cfg RenewConfig) error {
	if cfg.RetryInterval < 0 {
		return fmt.Errorf("%w: negative RetryInterval", ErrDHCPInvalidConfig)
	}
	if len(cfg.Hostname) > 255 {
		return fmt.Errorf("%w: hostname exceeds 255 bytes", ErrDHCPInvalidConfig)
	}
	for i := 0; i < len(cfg.Hostname); i++ {
		if cfg.Hostname[i] < 0x20 || cfg.Hostname[i] > 0x7e {
			return fmt.Errorf("%w: non-ASCII hostname", ErrDHCPInvalidConfig)
		}
	}
	if cfg.ServerMAC != "" {
		if _, err := parseUnicastMAC(cfg.ServerMAC); err != nil {
			return fmt.Errorf("%w: invalid ServerMAC: %v", ErrDHCPInvalidConfig, err)
		}
	}
	return nil
}

func mergeRenewedLease(current DHCPLease, r dhcpParsedReply) (DHCPLease, bool) {
	if r.leaseTime <= 0 {
		return DHCPLease{}, false
	}
	curPrefix, err := netip.ParsePrefix(current.Address)
	if err != nil {
		return DHCPLease{}, false
	}
	prefixBits := curPrefix.Bits()
	if r.hasMask {
		bits, ok := maskPrefixBits(r.mask)
		if !ok {
			return DHCPLease{}, false
		}
		prefixBits = bits
	}
	addr := netip.AddrFrom4(r.yiaddr)
	prefix := netip.PrefixFrom(addr, prefixBits)

	var gw string
	if len(r.routers) > 0 {
		for _, candidate := range r.routers {
			g := netip.AddrFrom4(candidate)
			if _, err := staticConfig(prefix.String(), g.String()); err == nil {
				gw = g.String()
				break
			}
		}
		if gw == "" {
			return DHCPLease{}, false
		}
	} else {
		gw = current.Gateway
	}
	cfg, err := staticConfig(prefix.String(), gw)
	if err != nil {
		return DHCPLease{}, false
	}

	server := current.Server
	if r.hasServer {
		srv := netip.AddrFrom4(r.serverID)
		if srv.IsUnspecified() || srv.IsMulticast() {
			return DHCPLease{}, false
		}
		server = srv.String()
	}

	var dns []string
	if len(r.dns) > 0 {
		for _, d := range r.dns {
			a := netip.AddrFrom4(d)
			if !a.IsUnspecified() && !a.IsMulticast() {
				dns = append(dns, a.String())
			}
		}
	} else if len(current.DNS) > 0 {
		dns = append([]string(nil), current.DNS...)
	}

	return DHCPLease{
		Address:       cfg.Address,
		Gateway:       cfg.Gateway,
		Server:        server,
		DNS:           dns,
		LeaseDuration: r.leaseTime,
		RenewalTime:   r.renewalTime,
		RebindTime:    r.rebindTime,
	}, true
}

func cloneLease(l DHCPLease) DHCPLease {
	if len(l.DNS) > 0 {
		l.DNS = append([]string(nil), l.DNS...)
	}
	return l
}

func buildDHCPRenewFrame(srcMAC, dstMAC [6]byte, clientIP, dstIP [4]byte, xid uint32, hostname string, broadcast bool) []byte {
	var opts bytes.Buffer
	opts.Write(dhcpMagicCookie[:])
	opts.Write([]byte{dhcpOptMsgType, 1, dhcpMsgRequest})
	if hostname != "" {
		opts.Write([]byte{dhcpOptHostname, byte(len(hostname))})
		opts.WriteString(hostname)
	}
	opts.Write([]byte{dhcpOptParamList, 7, dhcpOptSubnetMask, dhcpOptRouter, dhcpOptDNS, dhcpOptLeaseTime, dhcpOptServerID, dhcpOptRenewalTime, dhcpOptRebindTime})
	opts.WriteByte(dhcpOptEnd)
	for opts.Len() < 64 {
		opts.WriteByte(dhcpOptPad)
	}

	dhcpLen := bootpFixedLen + opts.Len()
	udpLen := udpHeaderLen + dhcpLen
	ipLen := ipv4MinLen + udpLen
	frame := make([]byte, ethHeaderLen+ipLen)

	copy(frame[0:6], dstMAC[:])
	copy(frame[6:12], srcMAC[:])
	binary.BigEndian.PutUint16(frame[12:14], ethTypeIPv4)

	ip := frame[ethHeaderLen : ethHeaderLen+ipv4MinLen]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipLen))
	binary.BigEndian.PutUint16(ip[4:6], uint16(xid))
	ip[8] = 64
	ip[9] = ipProtoUDP
	copy(ip[12:16], clientIP[:])
	copy(ip[16:20], dstIP[:])
	binary.BigEndian.PutUint16(ip[10:12], internetChecksum(ip))

	udp := frame[ethHeaderLen+ipv4MinLen:]
	binary.BigEndian.PutUint16(udp[0:2], dhcpClientPort)
	binary.BigEndian.PutUint16(udp[2:4], dhcpServerPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))

	bootp := udp[udpHeaderLen:]
	bootp[0] = bootpRequest
	bootp[1] = 1
	bootp[2] = 6
	binary.BigEndian.PutUint32(bootp[4:8], xid)
	if broadcast {
		binary.BigEndian.PutUint16(bootp[10:12], dhcpFlagBroadcast)
	}
	copy(bootp[12:16], clientIP[:]) // RFC 2131 RENEWING/REBINDING ciaddr
	copy(bootp[28:34], srcMAC[:])
	copy(bootp[bootpFixedLen:], opts.Bytes())

	csum := udpChecksum(ip[12:16], ip[16:20], udp)
	if csum == 0 {
		csum = 0xffff
	}
	binary.BigEndian.PutUint16(udp[6:8], csum)
	return frame
}
