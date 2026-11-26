// SPDX-License-Identifier: GPL-3.0-or-later

package mdns

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

const (
	defaultAnnounceCount    = 2
	defaultAnnounceInterval = time.Second
	dnsSDMetaServiceFQDN    = "_services._dns-sd._udp.local"
)

var (
	// ErrResponderStarted is returned when Responder.Start is called more than once.
	ErrResponderStarted = errors.New("mdns: responder already started")
	// ErrResponderStopped is returned when Responder.Start is called after Stop.
	ErrResponderStopped = errors.New("mdns: responder stopped")

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

// Responder answers mDNS queries for a configured DNS-SD service and sends
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

	mu        sync.Mutex
	started   bool
	stopped   bool
	stopCh    chan struct{}
	removeTap func()
	wg        sync.WaitGroup
	ipID      atomic.Uint32

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
		stopCh:           make(chan struct{}),
	}, nil
}

// Start installs the frame tap, begins answering mDNS queries, and transmits
// unsolicited multicast service announcements until Stop or ctx cancellation.
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
		emitGoodbye = r.started
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
	return r.sendAnnouncement(r.svc.TTL, false)
}

func (r *Responder) run(ctx context.Context) {
	defer func() {
		r.running.Store(false)
		r.wg.Done()
	}()

	for i := 0; i < r.announceCount; i++ {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-r.stopCh:
			return
		default:
		}
		if err := r.sendAnnouncement(r.svc.TTL, false); err != nil {
			if errors.Is(err, network.ErrPaused) {
				r.pausedDrops.Add(1)
			} else if r.onError != nil {
				r.onError(err)
			}
		}
		if i+1 >= r.announceCount {
			break
		}
		timer := time.NewTimer(r.announceInterval)
		select {
		case <-r.stopCh:
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
	case <-ctx.Done():
	}
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
	if !ok || pkt.msg.Response || len(pkt.msg.Questions) == 0 {
		return
	}
	r.queriesReceived.Add(1)

	var answers []dnsRecord
	var additionals []dnsRecord
	wantUnicast := pkt.srcPort != Port
	ttl := r.svc.TTL

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
		case strings.EqualFold(qName, r.metaServiceFQDN) && (q.Type == TypePTR || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("meta-ptr", r.metaPTRRecord(ttl))
		case strings.EqualFold(qName, r.serviceFQDN) && (q.Type == TypePTR || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("ptr", r.ptrRecord(ttl))
			addAdd("srv", r.srvRecord(ttl))
			addAdd("txt", r.txtRecord(ttl))
			addAdd("a", r.aRecord(ttl))
		case strings.EqualFold(qName, r.instanceFQDN):
			matched := false
			if q.Type == TypeSRV || q.Type == TypeANY {
				addAns("srv", r.srvRecord(ttl))
				addAdd("a", r.aRecord(ttl))
				matched = true
			}
			if q.Type == TypeTXT || q.Type == TypeANY {
				addAns("txt", r.txtRecord(ttl))
				matched = true
			}
			if matched && q.WantQU {
				wantUnicast = true
			}
		case strings.EqualFold(qName, r.hostFQDN) && (q.Type == TypeA || q.Type == TypeANY):
			if q.WantQU {
				wantUnicast = true
			}
			addAns("a", r.aRecord(ttl))
		}
	}

	if len(answers) == 0 {
		return
	}

	resp := dnsMessage{
		Response:   true,
		Answers:    answers,
		Additional: additionals,
	}
	if wantUnicast {
		resp.ID = pkt.msg.ID
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
