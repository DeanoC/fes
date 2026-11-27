// SPDX-License-Identifier: GPL-3.0-or-later

package mdns

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

const (
	defaultAnnounceCount    = 2
	defaultAnnounceInterval = time.Second
	probeAttempts           = 3
	probeInterval           = 250 * time.Millisecond
	defaultTieBreakDelay    = time.Second
	conflictDefendInterval  = 10 * time.Second
	legacyUnicastMaxTTL     = 10
	dnsSDMetaServiceFQDN    = "_services._dns-sd._udp.local"
)

var (
	// ErrResponderStarted is returned when Responder.Start is called more than once.
	ErrResponderStarted = errors.New("mdns: responder already started")
	// ErrResponderStopped is returned when Responder.Start is called after Stop.
	ErrResponderStopped = errors.New("mdns: responder stopped")
	// ErrResponderNameConflict means another host won a unique-name conflict.
	ErrResponderNameConflict = errors.New("mdns: unique name conflict")
	// ErrResponderNotClaimed means unique-name probing has not completed.
	ErrResponderNotClaimed = errors.New("mdns: responder has not completed probing")

	_ network.RawFramePort = (*network.Link)(nil)
)

// Service describes one advertised DNS-SD service instance and host A record.
type Service struct {
	Instance    string     // Human-readable instance label, e.g. "de25-nano"
	ServiceType string     // DNS-SD service type, e.g. "_fogcast._tcp"
	Domain      string     // Service domain (default "local")
	Host        string     // Hostname for SRV target and A record, e.g. "fes-de25.local"
	Port        uint16     // Advertised TCP/UDP service port (> 0)
	IP          netip.Addr // Advertised IPv4 host address
	TXT         []string   // Optional DNS-SD key=value TXT records (each <= 255 octets)
	TTL         uint32     // Record TTL in seconds (default 120)
}

func (s Service) normalized() (Service, string, string, string, error) {
	inst, err := validateInstanceLabel(s.Instance)
	if err != nil {
		return Service{}, "", "", "", err
	}
	svcType, err := validateServiceType(s.ServiceType)
	if err != nil {
		return Service{}, "", "", "", err
	}
	domain := strings.TrimSpace(s.Domain)
	if domain == "" {
		domain = "local"
	}
	normDomain, err := normalizeHostName(domain)
	if err != nil {
		return Service{}, "", "", "", err
	}
	host, err := normalizeHostName(s.Host)
	if err != nil {
		return Service{}, "", "", "", err
	}
	if !strings.HasSuffix(host, "."+normDomain) && host != normDomain {
		host = host + "." + normDomain
	}
	if s.Port == 0 {
		return Service{}, "", "", "", fmt.Errorf("%w: service port must be non-zero", ErrInvalidConfig)
	}
	if !s.IP.Is4() || s.IP.IsUnspecified() || s.IP.IsMulticast() {
		return Service{}, "", "", "", fmt.Errorf("%w: service IP must be a valid IPv4 host address", ErrInvalidConfig)
	}
	for _, entry := range s.TXT {
		if len(entry) > maxTXTOctetLen {
			return Service{}, "", "", "", fmt.Errorf("%w: TXT record entry exceeds %d bytes", ErrInvalidConfig, maxTXTOctetLen)
		}
	}
	ttl := s.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	out := s
	out.Instance = inst
	out.ServiceType = svcType
	out.Domain = normDomain
	out.Host = host
	out.TTL = ttl
	out.TXT = append([]string(nil), s.TXT...)

	serviceFQDN := svcType + "." + normDomain
	instanceFQDN := inst + "." + serviceFQDN
	return out, serviceFQDN, instanceFQDN, host, nil
}

// ResponderConfig configures an RFC 6762/6763 mDNS responder over Port.
type ResponderConfig struct {
	Port             network.RawFramePort
	LocalMAC         [6]byte       // Unicast non-zero source Ethernet MAC
	Service          Service       // Service advertisement definition
	AnnounceCount    int           // Unsolicited startup announcements (default 2; <0 invalid)
	AnnounceInterval time.Duration // Delay between startup announcements (default 1s; <0 invalid)
	OnError          func(error)   // Optional asynchronous transmit error hook
}

// ResponderStats summarizes live state and cumulative mDNS responder telemetry.
type ResponderStats struct {
	Running          bool
	QueriesReceived  uint64
	ResponsesSent    uint64
	MulticastReplies uint64
	UnicastReplies   uint64
	Announcements    uint64
	Goodbyes         uint64
	PausedDrops      uint64
}

type probeNameState struct {
	probing    bool
	claimed    bool
	attempts   int
	nextAction time.Time
}

// Responder probes unique mDNS names, answers DNS-SD queries, and sends
// unsolicited startup announcements and Stop goodbye packets.
type Responder struct {
	port             network.RawFramePort
	localMAC         [6]byte
	localIP          [4]byte
	svc              Service
	serviceFQDN      string
	instanceFQDN     string
	hostFQDN         string
	metaServiceFQDN  string
	announceCount    int
	announceInterval time.Duration
	onError          func(error)

	mu                sync.Mutex
	started           bool
	stopped           bool
	nameMu            sync.Mutex
	hostProbe         probeNameState
	instanceProbe     probeNameState
	probing           bool
	claimed           bool
	conflicted        bool
	goodbyeSent       bool
	lastDefense       time.Time
	initialProbeDelay time.Duration
	tieBreakDelay     time.Duration
	conflictCh        chan struct{}
	tieBreakCh        chan struct{}
	stopCh            chan struct{}
	removeTap         func()
	wg                sync.WaitGroup
	ipID              atomic.Uint32

	running          atomic.Bool
	queriesReceived  atomic.Uint64
	responsesSent    atomic.Uint64
	multicastReplies atomic.Uint64
	unicastReplies   atomic.Uint64
	announcements    atomic.Uint64
	goodbyes         atomic.Uint64
	pausedDrops      atomic.Uint64
}

// NewResponder validates cfg and creates an idle mDNS responder.
func NewResponder(cfg ResponderConfig) (*Responder, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: nil frame port", ErrInvalidConfig)
	}
	if cfg.LocalMAC == ([6]byte{}) || cfg.LocalMAC[0]&1 != 0 {
		return nil, fmt.Errorf("%w: LocalMAC must be a non-zero unicast MAC", ErrInvalidConfig)
	}
	if cfg.AnnounceCount < 0 || cfg.AnnounceInterval < 0 {
		return nil, fmt.Errorf("%w: negative announce configuration", ErrInvalidConfig)
	}
	svc, serviceFQDN, instanceFQDN, hostFQDN, err := cfg.Service.normalized()
	if err != nil {
		return nil, err
	}
	annCount := cfg.AnnounceCount
	if annCount == 0 {
		annCount = defaultAnnounceCount
	}
	annInterval := cfg.AnnounceInterval
	if annInterval == 0 {
		annInterval = defaultAnnounceInterval
	}
	return &Responder{
		port:             cfg.Port,
		localMAC:         cfg.LocalMAC,
		localIP:          svc.IP.As4(),
		svc:              svc,
		serviceFQDN:      serviceFQDN,
		instanceFQDN:     instanceFQDN,
		hostFQDN:         hostFQDN,
		metaServiceFQDN:  dnsSDMetaServiceFQDN,
		announceCount:    annCount,
		announceInterval: annInterval,
		onError:          cfg.OnError,
		tieBreakDelay:    defaultTieBreakDelay,
		conflictCh:       make(chan struct{}),
		tieBreakCh:       make(chan struct{}, 1),
		stopCh:           make(chan struct{}),
	}, nil
}

// Start installs the frame tap, probes the unique names, then answers mDNS
// queries and transmits startup announcements until Stop or ctx cancellation.
func (r *Responder) Start(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("%w: nil responder", ErrInvalidConfig)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.port.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return ErrResponderStopped
	}
	if r.started {
		r.mu.Unlock()
		return ErrResponderStarted
	}
	r.started = true
	r.running.Store(true)
	r.removeTap = r.port.AddTap(r.handleFrame)
	r.wg.Add(1)
	r.mu.Unlock()

	go r.run(ctx)
	return nil
}

// Stop stops the responder, removes its frame tap, waits for background work,
// and emits a best-effort TTL=0 goodbye packet. Safe to call multiple times.
func (r *Responder) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	emitGoodbye := false
	var removeTap func()
	if !r.stopped {
		r.stopped = true
		r.nameMu.Lock()
		emitGoodbye = r.started && r.claimed && !r.goodbyeSent
		if emitGoodbye {
			r.goodbyeSent = true
		}
		r.nameMu.Unlock()
		close(r.stopCh)
		removeTap = r.removeTap
		r.removeTap = nil
	}
	r.mu.Unlock()

	if removeTap != nil {
		removeTap()
	}
	r.wg.Wait()
	r.running.Store(false)

	if emitGoodbye {
		_ = r.sendAnnouncement(0, true)
	}
}

// Stats returns a snapshot of responder state and cumulative packet counters.
func (r *Responder) Stats() ResponderStats {
	if r == nil {
		return ResponderStats{}
	}
	return ResponderStats{
		Running:          r.running.Load(),
		QueriesReceived:  r.queriesReceived.Load(),
		ResponsesSent:    r.responsesSent.Load(),
		MulticastReplies: r.multicastReplies.Load(),
		UnicastReplies:   r.unicastReplies.Load(),
		Announcements:    r.announcements.Load(),
		Goodbyes:         r.goodbyes.Load(),
		PausedDrops:      r.pausedDrops.Load(),
	}
}

// AnnounceNow transmits one unsolicited multicast announcement packet for the
// configured service.
func (r *Responder) AnnounceNow() error {
	if r == nil {
		return fmt.Errorf("%w: nil responder", ErrInvalidConfig)
	}
	r.nameMu.Lock()
	claimed, conflicted := r.claimed, r.conflicted
	r.nameMu.Unlock()
	if conflicted {
		return ErrResponderNameConflict
	}
	if !claimed {
		return ErrResponderNotClaimed
	}
	return r.sendAnnouncement(r.svc.TTL, false)
}

func (r *Responder) run(ctx context.Context) {
	defer func() {
		r.running.Store(false)
		r.wg.Done()
	}()

	if !r.probe(ctx) {
		return
	}

	for i := 0; i < r.announceCount; i++ {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-r.stopCh:
			return
		case <-r.conflictCh:
			return
		default:
		}
		if err := r.sendAnnouncement(r.svc.TTL, false); err != nil && !errors.Is(err, network.ErrPaused) && r.onError != nil {
			r.onError(err)
		}
		if i+1 >= r.announceCount {
			break
		}
		timer := time.NewTimer(r.announceInterval)
		select {
		case <-r.stopCh:
			timer.Stop()
			return
		case <-r.conflictCh:
			timer.Stop()
			return
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}

	select {
	case <-r.stopCh:
	case <-r.conflictCh:
	case <-ctx.Done():
	}
}

func (r *Responder) probe(ctx context.Context) bool {
	initialDelay := r.initialProbeDelay
	if initialDelay <= 0 {
		initialDelay = time.Duration(rand.Int64N(int64(probeInterval)))
	}
	startAt := time.Now().Add(initialDelay)

	// RFC 6762 §8.1: initialize independent per-name probe schedules so a
	// tie-break loss on one name restarts only that name's probing while
	// preserving progress (or claim state) on other names.
	r.nameMu.Lock()
	if r.conflicted {
		r.nameMu.Unlock()
		return false
	}
	r.hostProbe = probeNameState{nextAction: startAt}
	r.instanceProbe = probeNameState{nextAction: startAt}
	r.probing = false
	r.nameMu.Unlock()
	drainSignal(r.tieBreakCh)

	for {
		if ctx.Err() != nil {
			return false
		}
		now := time.Now()
		var sendHost, sendInst bool
		var nextWake time.Time

		r.nameMu.Lock()
		if r.conflicted {
			r.nameMu.Unlock()
			return false
		}
		if r.hostProbe.probing && r.hostProbe.attempts >= probeAttempts && !now.Before(r.hostProbe.nextAction) {
			r.hostProbe.probing = false
			r.hostProbe.claimed = true
		}
		if r.instanceProbe.probing && r.instanceProbe.attempts >= probeAttempts && !now.Before(r.instanceProbe.nextAction) {
			r.instanceProbe.probing = false
			r.instanceProbe.claimed = true
		}
		if r.hostProbe.claimed && r.instanceProbe.claimed {
			r.probing = false
			r.claimed = true
			r.nameMu.Unlock()
			return true
		}

		if !r.hostProbe.claimed && r.hostProbe.attempts < probeAttempts {
			if !now.Before(r.hostProbe.nextAction) {
				r.hostProbe.probing = true
				sendHost = true
			} else if nextWake.IsZero() || r.hostProbe.nextAction.Before(nextWake) {
				nextWake = r.hostProbe.nextAction
			}
		}
		if !r.instanceProbe.claimed && r.instanceProbe.attempts < probeAttempts {
			if !now.Before(r.instanceProbe.nextAction) {
				r.instanceProbe.probing = true
				sendInst = true
			} else if nextWake.IsZero() || r.instanceProbe.nextAction.Before(nextWake) {
				nextWake = r.instanceProbe.nextAction
			}
		}
		if !sendHost && !sendInst {
			if r.hostProbe.probing && !r.hostProbe.claimed && (nextWake.IsZero() || r.hostProbe.nextAction.Before(nextWake)) {
				nextWake = r.hostProbe.nextAction
			}
			if r.instanceProbe.probing && !r.instanceProbe.claimed && (nextWake.IsZero() || r.instanceProbe.nextAction.Before(nextWake)) {
				nextWake = r.instanceProbe.nextAction
			}
		}
		r.probing = r.hostProbe.probing || r.instanceProbe.probing
		r.nameMu.Unlock()

		if sendHost || sendInst {
			if err := r.sendProbeFor(sendHost, sendInst); err != nil {
				retryAt := time.Now().Add(probeInterval)
				r.nameMu.Lock()
				if sendHost && !r.hostProbe.claimed {
					r.hostProbe.probing = false
					r.hostProbe.attempts = 0
					r.hostProbe.nextAction = retryAt
				}
				if sendInst && !r.instanceProbe.claimed {
					r.instanceProbe.probing = false
					r.instanceProbe.attempts = 0
					r.instanceProbe.nextAction = retryAt
				}
				r.probing = r.hostProbe.probing || r.instanceProbe.probing
				r.nameMu.Unlock()
				if errors.Is(err, network.ErrPaused) {
					r.pausedDrops.Add(1)
					continue
				}
				if r.onError != nil {
					r.onError(err)
				}
				return false
			}
			sentAt := time.Now()
			r.nameMu.Lock()
			if sendHost && r.hostProbe.probing && !r.hostProbe.claimed {
				r.hostProbe.attempts++
				r.hostProbe.nextAction = sentAt.Add(probeInterval)
			}
			if sendInst && r.instanceProbe.probing && !r.instanceProbe.claimed {
				r.instanceProbe.attempts++
				r.instanceProbe.nextAction = sentAt.Add(probeInterval)
			}
			r.probing = r.hostProbe.probing || r.instanceProbe.probing
			r.nameMu.Unlock()
			continue
		}

		delay := time.Until(nextWake)
		if !r.waitProbeUntil(ctx, delay) {
			return false
		}
	}
}

func drainSignal(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (r *Responder) waitProbeUntil(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-r.stopCh:
			return false
		case <-r.conflictCh:
			return false
		case <-ctx.Done():
			return false
		case <-r.tieBreakCh:
			return true
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-r.tieBreakCh:
		return true
	case <-r.stopCh:
		return false
	case <-r.conflictCh:
		return false
	case <-ctx.Done():
		return false
	}
}

func (r *Responder) hostProbingLocked() bool {
	return !r.conflicted && (r.hostProbe.probing || (r.probing && !r.hostProbe.claimed && !r.instanceProbe.probing && !r.instanceProbe.claimed))
}

func (r *Responder) instanceProbingLocked() bool {
	return !r.conflicted && (r.instanceProbe.probing || (r.probing && !r.instanceProbe.claimed && !r.hostProbe.probing && !r.hostProbe.claimed))
}

func (r *Responder) hostClaimedLocked() bool {
	return !r.conflicted && (r.hostProbe.claimed || r.claimed)
}

func (r *Responder) instanceClaimedLocked() bool {
	return !r.conflicted && (r.instanceProbe.claimed || r.claimed)
}

func (r *Responder) claimedNames() (hostClaimed, instClaimed bool) {
	r.nameMu.Lock()
	defer r.nameMu.Unlock()
	return r.hostClaimedLocked(), r.instanceClaimedLocked()
}

func (r *Responder) isClaimed() bool {
	r.nameMu.Lock()
	defer r.nameMu.Unlock()
	return r.claimed && !r.conflicted
}

func (r *Responder) hostProbeRecords() []dnsRecord {
	rec := r.aRecord(r.svc.TTL)
	rec.CacheFlush = false
	return []dnsRecord{rec}
}

func (r *Responder) instanceProbeRecords() []dnsRecord {
	srv := r.srvRecord(r.svc.TTL)
	srv.CacheFlush = false
	txt := r.txtRecord(r.svc.TTL)
	txt.CacheFlush = false
	return []dnsRecord{srv, txt}
}

func (r *Responder) probeRecords() []dnsRecord {
	return append(r.hostProbeRecords(), r.instanceProbeRecords()...)
}

func (r *Responder) sendProbeFor(sendHost, sendInst bool) error {
	var questions []dnsQuestion
	var authorities []dnsRecord
	if sendHost {
		questions = append(questions, dnsQuestion{Name: r.hostFQDN, Type: TypeANY, Class: ClassIN})
		authorities = append(authorities, r.hostProbeRecords()...)
	}
	if sendInst {
		questions = append(questions, dnsQuestion{Name: r.instanceFQDN, Type: TypeANY, Class: ClassIN})
		authorities = append(authorities, r.instanceProbeRecords()...)
	}
	msg := dnsMessage{
		Questions:   questions,
		Authorities: authorities,
	}
	payload, err := encodeMDNSMessage(msg)
	if err != nil {
		return err
	}
	frame := buildMDNSFrame(r.localMAC, multicastMACBytes, r.localIP, multicastIPv4Bytes, Port, Port, uint16(r.ipID.Add(1)), payload)
	return r.port.SendRawFrame(frame)
}

func (r *Responder) reportNameConflict() {
	if r.onError != nil {
		r.onError(ErrResponderNameConflict)
	}
}

func (r *Responder) markNameConflictLocked() {
	if r.conflicted {
		return
	}
	r.conflicted = true
	r.probing = false
	r.hostProbe.probing = false
	r.instanceProbe.probing = false
	close(r.conflictCh)
}

func (r *Responder) handleProbeConflict(proposed []dnsRecord) {
	r.nameMu.Lock()
	hostProbing := r.hostProbingLocked()
	instProbing := r.instanceProbingLocked()
	r.nameMu.Unlock()
	if !hostProbing && !instProbing {
		return
	}

	var activeLocal []dnsRecord
	if hostProbing {
		activeLocal = append(activeLocal, r.hostProbeRecords()...)
	}
	if instProbing {
		activeLocal = append(activeLocal, r.instanceProbeRecords()...)
	}
	outcomes := compareProbeRecordSets(activeLocal, proposed)
	hostLost := hostProbing && outcomes[strings.ToLower(strings.TrimSuffix(r.hostFQDN, "."))] < 0
	instLost := instProbing && outcomes[strings.ToLower(strings.TrimSuffix(r.instanceFQDN, "."))] < 0
	if !hostLost && !instLost {
		return
	}

	retryAt := time.Now().Add(r.tieBreakDelay)
	signal := false
	r.nameMu.Lock()
	if !r.conflicted {
		if hostLost && r.hostProbingLocked() {
			r.hostProbe.probing = false
			r.hostProbe.attempts = 0
			r.hostProbe.nextAction = retryAt
			signal = true
		}
		if instLost && r.instanceProbingLocked() {
			r.instanceProbe.probing = false
			r.instanceProbe.attempts = 0
			r.instanceProbe.nextAction = retryAt
			signal = true
		}
		r.probing = r.hostProbe.probing || r.instanceProbe.probing
	}
	r.nameMu.Unlock()

	if signal {
		select {
		case r.tieBreakCh <- struct{}{}:
		default:
		}
	}
}

func (r *Responder) handleResponseConflict(msg dnsMessage) {
	r.nameMu.Lock()
	if r.conflicted {
		r.nameMu.Unlock()
		return
	}
	hostProbing := r.hostProbingLocked()
	instProbing := r.instanceProbingLocked()
	hostClaimed := r.hostClaimedLocked()
	instClaimed := r.instanceClaimedLocked()
	if !hostProbing && !instProbing && !hostClaimed && !instClaimed {
		r.nameMu.Unlock()
		return
	}
	r.nameMu.Unlock()

	remote := make([]dnsRecord, 0, len(msg.Answers)+len(msg.Authorities)+len(msg.Additional))
	remote = append(remote, msg.Answers...)
	remote = append(remote, msg.Authorities...)
	remote = append(remote, msg.Additional...)

	var probingLocal []dnsRecord
	if hostProbing {
		probingLocal = append(probingLocal, r.hostProbeRecords()...)
	}
	if instProbing {
		probingLocal = append(probingLocal, r.instanceProbeRecords()...)
	}
	var claimedLocal []dnsRecord
	if hostClaimed {
		claimedLocal = append(claimedLocal, r.hostProbeRecords()...)
	}
	if instClaimed {
		claimedLocal = append(claimedLocal, r.instanceProbeRecords()...)
	}
	probeConflict := len(probingLocal) > 0 && probeResponseConflicts(probingLocal, remote)
	claimedConflict := len(claimedLocal) > 0 && recordSetsConflict(claimedLocal, remote)
	if !probeConflict && !claimedConflict {
		return
	}

	defend := false
	relinquish := false
	sendGoodbye := false
	defendHost := false
	defendInst := false
	r.nameMu.Lock()
	if r.conflicted {
		r.nameMu.Unlock()
		return
	}
	if (r.hostProbingLocked() && probeResponseConflicts(r.hostProbeRecords(), remote)) ||
		(r.instanceProbingLocked() && probeResponseConflicts(r.instanceProbeRecords(), remote)) {
		r.markNameConflictLocked()
		relinquish = true
	} else {
		defendHost = r.hostClaimedLocked()
		defendInst = r.instanceClaimedLocked()
		var activeClaimed []dnsRecord
		if defendHost {
			activeClaimed = append(activeClaimed, r.hostProbeRecords()...)
		}
		if defendInst {
			activeClaimed = append(activeClaimed, r.instanceProbeRecords()...)
		}
		if len(activeClaimed) > 0 && recordSetsConflict(activeClaimed, remote) {
			now := time.Now()
			if !r.lastDefense.IsZero() && now.Sub(r.lastDefense) <= conflictDefendInterval {
				r.markNameConflictLocked()
				relinquish = true
				if !r.goodbyeSent {
					r.goodbyeSent = true
					sendGoodbye = true
				}
			} else {
				r.lastDefense = now
				defend = true
			}
		}
	}
	r.nameMu.Unlock()

	if defend {
		if err := r.sendClaimedRecords(defendHost, defendInst, r.svc.TTL, false); err != nil && !errors.Is(err, network.ErrPaused) && r.onError != nil {
			r.onError(err)
		}
	}
	if relinquish {
		r.reportNameConflict()
		if sendGoodbye {
			_ = r.sendClaimedRecords(defendHost, defendInst, 0, true)
		}
	}
}

func (r *Responder) sendClaimedRecords(hostClaimed, instClaimed bool, ttl uint32, goodbye bool) error {
	if hostClaimed && instClaimed {
		return r.sendAnnouncement(ttl, goodbye)
	}
	var answers []dnsRecord
	if instClaimed {
		answers = append(answers, r.srvRecord(ttl), r.txtRecord(ttl))
	}
	if hostClaimed {
		answers = append(answers, r.aRecord(ttl))
	}
	if len(answers) == 0 {
		return nil
	}
	payload, err := encodeMDNSMessage(dnsMessage{
		Response: true,
		Answers:  answers,
	})
	if err != nil {
		return err
	}
	frame := buildMDNSFrame(r.localMAC, multicastMACBytes, r.localIP, multicastIPv4Bytes, Port, Port, uint16(r.ipID.Add(1)), payload)
	if err := r.port.SendRawFrame(frame); err != nil {
		if errors.Is(err, network.ErrPaused) {
			r.pausedDrops.Add(1)
		}
		return err
	}
	if goodbye {
		r.goodbyes.Add(1)
	} else {
		r.announcements.Add(1)
	}
	return nil
}

func (r *Responder) sendAnnouncement(ttl uint32, goodbye bool) error {
	msg := dnsMessage{
		Response:   true,
		Answers:    []dnsRecord{r.ptrRecord(ttl)},
		Additional: []dnsRecord{r.srvRecord(ttl), r.txtRecord(ttl), r.aRecord(ttl)},
	}
	payload, err := encodeMDNSMessage(msg)
	if err != nil {
		return err
	}
	frame := buildMDNSFrame(r.localMAC, multicastMACBytes, r.localIP, multicastIPv4Bytes, Port, Port, uint16(r.ipID.Add(1)), payload)
	if err := r.port.SendRawFrame(frame); err != nil {
		if errors.Is(err, network.ErrPaused) {
			r.pausedDrops.Add(1)
		}
		return err
	}
	if goodbye {
		r.goodbyes.Add(1)
	} else {
		r.announcements.Add(1)
	}
	return nil
}

func (r *Responder) ptrRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.serviceFQDN,
		Type:       TypePTR,
		Class:      ClassIN,
		CacheFlush: false,
		TTL:        ttl,
		PTR:        r.instanceFQDN,
	}
}

func (r *Responder) metaPTRRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.metaServiceFQDN,
		Type:       TypePTR,
		Class:      ClassIN,
		CacheFlush: false,
		TTL:        ttl,
		PTR:        r.serviceFQDN,
	}
}

func (r *Responder) srvRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.instanceFQDN,
		Type:       TypeSRV,
		Class:      ClassIN,
		CacheFlush: true,
		TTL:        ttl,
		SRVPort:    r.svc.Port,
		SRVTarget:  r.hostFQDN,
	}
}

func (r *Responder) txtRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.instanceFQDN,
		Type:       TypeTXT,
		Class:      ClassIN,
		CacheFlush: true,
		TTL:        ttl,
		TXT:        r.svc.TXT,
	}
}

func (r *Responder) aRecord(ttl uint32) dnsRecord {
	return dnsRecord{
		Name:       r.hostFQDN,
		Type:       TypeA,
		Class:      ClassIN,
		CacheFlush: true,
		TTL:        ttl,
		A:          r.svc.IP,
	}
}

func (r *Responder) handleFrame(frame []byte) {
	if !r.running.Load() {
		return
	}
	pkt, ok := parseMDNSFrame(frame, r.localMAC, r.localIP, false)
	if !ok {
		return
	}
	if pkt.msg.Response {
		r.handleResponseConflict(pkt.msg)
		return
	}
	if len(pkt.msg.Authorities) > 0 {
		r.handleProbeConflict(pkt.msg.Authorities)
	}
	hostClaimed, instClaimed := r.claimedNames()
	if (!hostClaimed && !instClaimed) || len(pkt.msg.Questions) == 0 {
		return
	}
	r.queriesReceived.Add(1)

	var answers []dnsRecord
	var additionals []dnsRecord
	wantUnicast := pkt.srcPort != Port
	legacyUnicast := pkt.srcPort != Port
	ttl := r.svc.TTL
	if legacyUnicast && ttl > legacyUnicastMaxTTL {
		ttl = legacyUnicastMaxTTL
	}

	hasAnswer := make(map[string]bool)
	addAns := func(key string, rr dnsRecord) {
		if !hasAnswer[key] {
			hasAnswer[key] = true
			answers = append(answers, rr)
		}
	}
	addAdd := func(key string, rr dnsRecord) {
		if !hasAnswer[key] {
			hasAnswer[key] = true
			additionals = append(additionals, rr)
		}
	}

	for _, q := range pkt.msg.Questions {
		if q.Class != ClassIN && q.Class != TypeANY {
			continue
		}
		qName := strings.TrimSuffix(q.Name, ".")
		switch {
		case hostClaimed && instClaimed && strings.EqualFold(qName, r.metaServiceFQDN) && (q.Type == TypePTR || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("meta-ptr", r.metaPTRRecord(ttl))
		case hostClaimed && instClaimed && strings.EqualFold(qName, r.serviceFQDN) && (q.Type == TypePTR || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("ptr", r.ptrRecord(ttl))
			addAdd("srv", r.srvRecord(ttl))
			addAdd("txt", r.txtRecord(ttl))
			addAdd("a", r.aRecord(ttl))
		case instClaimed && strings.EqualFold(qName, r.instanceFQDN):
			matched := false
			if q.Type == TypeSRV || q.Type == TypeANY {
				addAns("srv", r.srvRecord(ttl))
				if hostClaimed {
					addAdd("a", r.aRecord(ttl))
				}
				matched = true
			}
			if q.Type == TypeTXT || q.Type == TypeANY {
				addAns("txt", r.txtRecord(ttl))
				matched = true
			}
			if matched && q.WantQU {
				wantUnicast = true
			}
		case hostClaimed && strings.EqualFold(qName, r.hostFQDN) && (q.Type == TypeA || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("a", r.aRecord(ttl))
		}
	}

	answers = filterRecords(answers, func(rr dnsRecord) bool {
		return !knownAnswerSuppresses(rr, pkt.msg.Answers)
	})
	if len(answers) == 0 {
		return
	}
	allAdditionals := append([]dnsRecord(nil), additionals...)
	additionals = filterRecords(additionals, func(rr dnsRecord) bool {
		return !knownAnswerSuppresses(rr, pkt.msg.Answers) && additionalNeeded(rr, answers, allAdditionals)
	})

	resp := dnsMessage{
		Response:   true,
		Answers:    answers,
		Additional: additionals,
	}
	if legacyUnicast {
		resp.ID = pkt.msg.ID
		resp.Questions = append([]dnsQuestion(nil), pkt.msg.Questions...)
		for i := range resp.Answers {
			resp.Answers[i].CacheFlush = false
		}
		for i := range resp.Additional {
			resp.Additional[i].CacheFlush = false
		}
	}
	payload, err := encodeMDNSMessage(resp)
	if err != nil {
		return
	}

	dstMAC := multicastMACBytes
	dstIP := multicastIPv4Bytes
	dstPort := Port
	if wantUnicast {
		dstMAC = pkt.srcMAC
		dstIP = pkt.srcIP
		dstPort = pkt.srcPort
	}
	outFrame := buildMDNSFrame(r.localMAC, dstMAC, r.localIP, dstIP, Port, dstPort, uint16(r.ipID.Add(1)), payload)
	if err := r.port.SendRawFrame(outFrame); err != nil {
		if errors.Is(err, network.ErrPaused) {
			r.pausedDrops.Add(1)
		} else if r.onError != nil {
			r.onError(err)
		}
		return
	}
	r.responsesSent.Add(1)
	if wantUnicast {
		r.unicastReplies.Add(1)
	} else {
		r.multicastReplies.Add(1)
	}
}

func filterRecords(records []dnsRecord, keep func(dnsRecord) bool) []dnsRecord {
	filtered := records[:0]
	for _, rr := range records {
		if keep(rr) {
			filtered = append(filtered, rr)
		}
	}
	return filtered
}

func knownAnswerSuppresses(rr dnsRecord, known []dnsRecord) bool {
	for _, candidate := range known {
		if candidate.Class == ClassIN && candidate.Type == rr.Type &&
			strings.EqualFold(strings.TrimSuffix(candidate.Name, "."), strings.TrimSuffix(rr.Name, ".")) &&
			bytes.Equal(recordRData(candidate), recordRData(rr)) && uint64(candidate.TTL)*2 >= uint64(rr.TTL) {
			return true
		}
	}
	return false
}

func additionalNeeded(additional dnsRecord, answers, additionals []dnsRecord) bool {
	for _, answer := range answers {
		switch answer.Type {
		case TypePTR:
			if (additional.Type == TypeSRV || additional.Type == TypeTXT) && strings.EqualFold(additional.Name, answer.PTR) {
				return true
			}
			if additional.Type == TypeA {
				for _, dependency := range additionals {
					if dependency.Type == TypeSRV && strings.EqualFold(dependency.Name, answer.PTR) && strings.EqualFold(dependency.SRVTarget, additional.Name) {
						return true
					}
				}
			}
		case TypeSRV:
			if additional.Type == TypeA && strings.EqualFold(additional.Name, answer.SRVTarget) {
				return true
			}
		}
	}
	return false
}

func probeResponseConflicts(local, remote []dnsRecord) bool {
	probedNames := make(map[string]bool, len(local))
	for _, ours := range local {
		probedNames[strings.ToLower(strings.TrimSuffix(ours.Name, "."))] = true
	}
	for _, theirs := range remote {
		if theirs.TTL == 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(theirs.Name, "."))
		if !probedNames[name] {
			continue
		}
		theirsData := recordRData(theirs)
		matched := false
		for _, ours := range local {
			if strings.EqualFold(strings.TrimSuffix(ours.Name, "."), name) &&
				ours.Type == theirs.Type &&
				bytes.Equal(recordRData(ours), theirsData) {
				matched = true
				break
			}
		}
		if !matched {
			return true
		}
	}
	return false
}

func recordSetsConflict(local, remote []dnsRecord) bool {
	for _, ours := range local {
		for _, theirs := range remote {
			if theirs.TTL != 0 && ours.Type == theirs.Type && strings.EqualFold(strings.TrimSuffix(ours.Name, "."), strings.TrimSuffix(theirs.Name, ".")) &&
				!bytes.Equal(recordRData(ours), recordRData(theirs)) {
				return true
			}
		}
	}
	return false
}

func compareProbeRecords(local, remote []dnsRecord) (conflict, localWins bool) {
	outcomes := compareProbeRecordSets(local, remote)
	if len(outcomes) == 0 {
		return false, false
	}
	names := make([]string, 0, len(outcomes))
	for name := range outcomes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		cmp := outcomes[name]
		if cmp < 0 {
			return true, false
		}
		if cmp > 0 {
			conflict = true
		}
	}
	return conflict, conflict
}

func compareProbeRecordSets(local, remote []dnsRecord) map[string]int {
	type tieRecord struct {
		class uint16
		typ   uint16
		rdata []byte
	}
	sortTieRecords := func(recs []tieRecord) {
		slices.SortFunc(recs, func(a, b tieRecord) int {
			if a.class != b.class {
				return int(a.class) - int(b.class)
			}
			if a.typ != b.typ {
				return int(a.typ) - int(b.typ)
			}
			return bytes.Compare(a.rdata, b.rdata)
		})
	}
	compareTieRecord := func(a, b tieRecord) int {
		if a.class != b.class {
			return int(a.class) - int(b.class)
		}
		if a.typ != b.typ {
			return int(a.typ) - int(b.typ)
		}
		return bytes.Compare(a.rdata, b.rdata)
	}

	localByName := make(map[string][]tieRecord)
	for _, rr := range local {
		name := strings.ToLower(strings.TrimSuffix(rr.Name, "."))
		localByName[name] = append(localByName[name], tieRecord{
			class: rr.Class & 0x7fff,
			typ:   rr.Type,
			rdata: recordRData(rr),
		})
	}
	remoteByName := make(map[string][]tieRecord)
	for _, rr := range remote {
		if rr.TTL == 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(rr.Name, "."))
		if _, ok := localByName[name]; ok {
			remoteByName[name] = append(remoteByName[name], tieRecord{
				class: rr.Class & 0x7fff,
				typ:   rr.Type,
				rdata: recordRData(rr),
			})
		}
	}
	if len(remoteByName) == 0 {
		return nil
	}

	names := make([]string, 0, len(remoteByName))
	for name := range remoteByName {
		names = append(names, name)
	}
	slices.Sort(names)

	outcomes := make(map[string]int, len(names))
	for _, name := range names {
		ours, theirs := localByName[name], remoteByName[name]
		sortTieRecords(ours)
		sortTieRecords(theirs)
		limit := min(len(ours), len(theirs))
		nameCmp := 0
		for i := 0; i < limit; i++ {
			if cmp := compareTieRecord(ours[i], theirs[i]); cmp != 0 {
				nameCmp = cmp
				break
			}
		}
		if nameCmp == 0 {
			switch {
			case len(ours) < len(theirs):
				nameCmp = -1
			case len(ours) > len(theirs):
				nameCmp = 1
			}
		}
		outcomes[name] = nameCmp
	}
	return outcomes
}

func recordRData(rr dnsRecord) []byte {
	switch rr.Type {
	case TypeA:
		if rr.A.Is4() {
			a4 := rr.A.As4()
			return a4[:]
		}
	case TypeSRV:
		target, err := encodeDNSName(strings.ToLower(strings.TrimSuffix(rr.SRVTarget, ".")))
		if err == nil {
			rdata := make([]byte, 6+len(target))
			binary.BigEndian.PutUint16(rdata[0:2], rr.SRVPriority)
			binary.BigEndian.PutUint16(rdata[2:4], rr.SRVWeight)
			binary.BigEndian.PutUint16(rdata[4:6], rr.SRVPort)
			copy(rdata[6:], target)
			return rdata
		}
	case TypeTXT:
		if len(rr.TXT) == 0 {
			return []byte{0}
		}
		var rdata []byte
		for _, entry := range rr.TXT {
			if len(entry) > maxTXTOctetLen {
				return nil
			}
			rdata = append(rdata, byte(len(entry)))
			rdata = append(rdata, entry...)
		}
		return rdata
	case TypePTR:
		ptr, err := encodeDNSName(strings.ToLower(strings.TrimSuffix(rr.PTR, ".")))
		if err == nil {
			return ptr
		}
	default:
		return rr.RawRData
	}
	return nil
}
