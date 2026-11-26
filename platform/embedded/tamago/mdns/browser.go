// SPDX-License-Identifier: GPL-3.0-or-later

package mdns

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

const (
	defaultBrowseQueryInterval = time.Second
	minBrowseQueryInterval     = time.Second
	maxBrowseQueryInterval     = time.Hour
	cacheFlushGrace            = time.Second
)

type cachedAddress struct {
	expires  time.Time
	received time.Time
	ttl      uint32
}

type browseInstanceRecord struct {
	instance   string
	ptrTarget  string
	ptrExpires time.Time
	ptrTTL     uint32
	hasPTR     bool
	srvHost    string
	srvPort    uint16
	srvExpires time.Time
	hasSRV     bool
	txt        []string
	txtExpires time.Time
	hasTXT     bool
}

// BrowseConfig configures active mDNS browsing and host resolution over a RawFramePort.
type BrowseConfig struct {
	LocalMAC        [6]byte          // Unicast non-zero client Ethernet MAC
	LocalIP         netip.Addr       // Client IPv4 host address used as source IP
	QueryInterval   time.Duration    // Initial retransmit interval (default 1s, then doubles; <0 invalid)
	UnicastResponse bool             // Request unicast replies (sets RFC 6762 QU bit)
	StopOnFirst     bool             // When true, Browse returns as soon as at least one complete service is resolved
	Now             func() time.Time // Optional clock for deterministic TTL expiry tests (default time.Now)
}

// DiscoveredService holds one resolved DNS-SD instance with SRV port, host, TXT, and IPv4 addresses.
type DiscoveredService struct {
	Instance    string
	ServiceType string
	Domain      string
	Host        string
	Port        uint16
	Addrs       []netip.Addr
	TXT         []string
	TTL         uint32
}

var browseSeq atomic.Uint32

// Browse queries multicast DNS for serviceType (for example "_fogcast._tcp")
// over port until ctx completes (or the first resolved service arrives when
// cfg.StopOnFirst is true), returning discovered services sorted by Instance.
func Browse(ctx context.Context, port network.RawFramePort, cfg BrowseConfig, serviceType string) ([]DiscoveredService, error) {
	if port == nil {
		return nil, fmt.Errorf("%w: nil frame port", ErrInvalidConfig)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.LocalMAC == ([6]byte{}) || cfg.LocalMAC[0]&1 != 0 {
		return nil, fmt.Errorf("%w: LocalMAC must be a non-zero unicast MAC", ErrInvalidConfig)
	}
	if !cfg.LocalIP.Is4() || cfg.LocalIP.IsUnspecified() || cfg.LocalIP.IsMulticast() {
		return nil, fmt.Errorf("%w: LocalIP must be a valid IPv4 host address", ErrInvalidConfig)
	}
	if cfg.QueryInterval < 0 {
		return nil, fmt.Errorf("%w: negative query interval", ErrInvalidConfig)
	}
	svcType, err := validateServiceType(serviceType)
	if err != nil {
		return nil, err
	}
	interval := cfg.QueryInterval
	if interval == 0 {
		interval = defaultBrowseQueryInterval
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	serviceFQDN := svcType + ".local"
	queryPayload, err := encodeMDNSMessage(dnsMessage{
		Questions: []dnsQuestion{{
			Name:   serviceFQDN,
			Type:   TypePTR,
			Class:  ClassIN,
			WantQU: cfg.UnicastResponse,
		}},
	})
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	instances := make(map[string]*browseInstanceRecord)
	hostAddrs := make(map[string]map[netip.Addr]cachedAddress)
	readyCh := make(chan struct{}, 1)

	getInst := func(key, display string) *browseInstanceRecord {
		rec := instances[key]
		if rec == nil {
			rec = &browseInstanceRecord{instance: display}
			instances[key] = rec
		}
		return rec
	}

	snapshotLocked := func(now time.Time) []DiscoveredService {
		for hostKey, addrMap := range hostAddrs {
			for addr, record := range addrMap {
				if !now.Before(record.expires) {
					delete(addrMap, addr)
				}
			}
			if len(addrMap) == 0 {
				delete(hostAddrs, hostKey)
			}
		}

		var out []DiscoveredService
		for key, rec := range instances {
			if !rec.hasPTR || !now.Before(rec.ptrExpires) {
				rec.hasPTR = false
			}
			if !rec.hasSRV || !now.Before(rec.srvExpires) {
				rec.hasSRV = false
			}
			if rec.hasTXT && !now.Before(rec.txtExpires) {
				rec.hasTXT = false
				rec.txt = nil
			}
			if !rec.hasPTR && !rec.hasSRV && !rec.hasTXT {
				delete(instances, key)
				continue
			}
			if !rec.hasPTR || !rec.hasSRV {
				continue
			}
			addrMap := hostAddrs[strings.ToLower(rec.srvHost)]
			if len(addrMap) == 0 {
				continue
			}
			minExpiry := rec.ptrExpires
			if rec.srvExpires.Before(minExpiry) {
				minExpiry = rec.srvExpires
			}
			var sortedAddrs []netip.Addr
			for addr, record := range addrMap {
				sortedAddrs = append(sortedAddrs, addr)
				if record.expires.Before(minExpiry) {
					minExpiry = record.expires
				}
			}
			slices.SortFunc(sortedAddrs, func(a, b netip.Addr) int {
				return cmp.Compare(a.String(), b.String())
			})
			ttlSec := uint32(minExpiry.Sub(now) / time.Second)
			if ttlSec == 0 {
				ttlSec = 1
			}
			var txt []string
			if rec.hasTXT {
				txt = append([]string(nil), rec.txt...)
			}
			out = append(out, DiscoveredService{
				Instance:    rec.instance,
				ServiceType: svcType,
				Domain:      "local",
				Host:        rec.srvHost,
				Port:        rec.srvPort,
				Addrs:       sortedAddrs,
				TXT:         txt,
				TTL:         ttlSec,
			})
		}
		slices.SortFunc(out, func(a, b DiscoveredService) int {
			return cmp.Compare(a.Instance, b.Instance)
		})
		return out
	}

	localIP4 := cfg.LocalIP.As4()
	remove := port.AddTap(func(frame []byte) {
		pkt, ok := parseMDNSFrame(frame, cfg.LocalMAC, localIP4, true)
		if !ok || !pkt.msg.Response {
			return
		}
		now := nowFn()
		mu.Lock()
		defer mu.Unlock()

		handleRR := func(rr dnsRecord) {
			name := strings.TrimSuffix(rr.Name, ".")
			exp := now.Add(time.Duration(rr.TTL) * time.Second)
			switch rr.Type {
			case TypePTR:
				if !strings.EqualFold(name, serviceFQDN) {
					return
				}
				instFQDN := strings.TrimSuffix(rr.PTR, ".")
				key := strings.ToLower(instFQDN)
				if rr.TTL == 0 {
					if rec := instances[key]; rec != nil {
						rec.hasPTR = false
						rec.ptrExpires = time.Time{}
					}
					return
				}
				display := instFQDN
				if idx := strings.IndexByte(instFQDN, '.'); idx > 0 {
					display = instFQDN[:idx]
				}
				rec := getInst(key, display)
				rec.hasPTR = true
				rec.ptrTarget = instFQDN
				rec.ptrExpires = exp
				rec.ptrTTL = rr.TTL
			case TypeSRV:
				key := strings.ToLower(name)
				if rr.TTL == 0 {
					if rec := instances[key]; rec != nil {
						rec.hasSRV = false
						rec.srvExpires = time.Time{}
					}
					return
				}
				display := name
				if idx := strings.IndexByte(name, '.'); idx > 0 {
					display = name[:idx]
				}
				rec := getInst(key, display)
				rec.hasSRV = true
				rec.srvPort = rr.SRVPort
				rec.srvHost = strings.TrimSuffix(rr.SRVTarget, ".")
				rec.srvExpires = exp
			case TypeTXT:
				key := strings.ToLower(name)
				display := name
				if idx := strings.IndexByte(name, '.'); idx > 0 {
					display = name[:idx]
				}
				rec := getInst(key, display)
				if rr.TTL == 0 {
					rec.hasTXT = false
					rec.txt = nil
					rec.txtExpires = time.Time{}
				} else {
					rec.hasTXT = true
					rec.txt = append([]string(nil), rr.TXT...)
					rec.txtExpires = exp
				}
			}
		}

		packetRecords := make([]dnsRecord, 0, len(pkt.msg.Answers)+len(pkt.msg.Additional))
		packetRecords = append(packetRecords, pkt.msg.Answers...)
		packetRecords = append(packetRecords, pkt.msg.Additional...)
		for _, rr := range pkt.msg.Answers {
			handleRR(rr)
		}
		for _, rr := range pkt.msg.Additional {
			handleRR(rr)
		}
		applyARecordPacket(hostAddrs, packetRecords, now)
		if cfg.StopOnFirst && len(snapshotLocked(now)) > 0 {
			select {
			case readyCh <- struct{}{}:
			default:
			}
		}
	})
	defer remove()

	sendQuery := func() error {
		if err := port.Err(); err != nil {
			return err
		}
		mu.Lock()
		knownAnswers := knownPTRAnswers(instances, serviceFQDN, nowFn())
		mu.Unlock()
		payload := queryPayload
		if len(knownAnswers) > 0 {
			payload, err = encodeMDNSMessage(dnsMessage{
				Questions: []dnsQuestion{{
					Name:   serviceFQDN,
					Type:   TypePTR,
					Class:  ClassIN,
					WantQU: cfg.UnicastResponse,
				}},
				Answers: knownAnswers,
			})
			if err != nil {
				return err
			}
		}
		frame := buildMDNSFrame(cfg.LocalMAC, multicastMACBytes, localIP4, multicastIPv4Bytes, Port, Port, uint16(browseSeq.Add(1)), payload)
		return port.SendRawFrame(frame)
	}

	if err := sendQuery(); err != nil {
		return nil, err
	}

	nextInterval := min(max(interval, minBrowseQueryInterval), maxBrowseQueryInterval)
	timer := time.NewTimer(nextInterval)
	defer timer.Stop()

	for {
		select {
		case <-readyCh:
			mu.Lock()
			res := snapshotLocked(nowFn())
			mu.Unlock()
			return res, nil
		case <-ctx.Done():
			mu.Lock()
			res := snapshotLocked(nowFn())
			mu.Unlock()
			if len(res) > 0 {
				return res, nil
			}
			return nil, ctx.Err()
		case <-timer.C:
			if err := sendQuery(); err != nil {
				return nil, err
			}
			nextInterval = nextBrowseQueryInterval(nextInterval)
			timer.Reset(nextInterval)
		}
	}
}

func applyARecordPacket(cache map[string]map[netip.Addr]cachedAddress, records []dnsRecord, now time.Time) {
	byHost := make(map[string][]dnsRecord)
	for _, rr := range records {
		if rr.Type == TypeA && rr.A.Is4() {
			host := strings.ToLower(strings.TrimSuffix(rr.Name, "."))
			byHost[host] = append(byHost[host], rr)
		}
	}
	for host, hostRecords := range byHost {
		addrMap := cache[host]
		if addrMap == nil {
			addrMap = make(map[netip.Addr]cachedAddress)
			cache[host] = addrMap
		}
		incoming := make(map[netip.Addr]struct{})
		flush := false
		for _, rr := range hostRecords {
			if rr.TTL == 0 {
				delete(addrMap, rr.A)
				continue
			}
			incoming[rr.A] = struct{}{}
			addrMap[rr.A] = cachedAddress{expires: now.Add(time.Duration(rr.TTL) * time.Second), received: now, ttl: rr.TTL}
			flush = flush || rr.CacheFlush
		}
		if flush {
			graceExpiry := now.Add(cacheFlushGrace)
			for addr, record := range addrMap {
				if _, inPacket := incoming[addr]; inPacket {
					continue
				}
				if now.Sub(record.received) < cacheFlushGrace {
					continue
				}
				if record.expires.After(graceExpiry) {
					record.expires = graceExpiry
					addrMap[addr] = record
				}
			}
		}
		if len(addrMap) == 0 {
			delete(cache, host)
		}
	}
}

func nextBrowseQueryInterval(current time.Duration) time.Duration {
	if current >= maxBrowseQueryInterval/2 {
		return maxBrowseQueryInterval
	}
	return current * 2
}

func knownPTRAnswers(instances map[string]*browseInstanceRecord, serviceFQDN string, now time.Time) []dnsRecord {
	// Called while the browser cache mutex is held.
	var answers []dnsRecord
	for _, rec := range instances {
		if !rec.hasPTR || rec.ptrTTL == 0 || !now.Before(rec.ptrExpires) {
			continue
		}
		remaining := rec.ptrExpires.Sub(now)
		if remaining < time.Duration(rec.ptrTTL)*time.Second/2 {
			continue
		}
		ttl := uint32(remaining / time.Second)
		if ttl == 0 {
			continue
		}
		answers = append(answers, dnsRecord{
			Name:  serviceFQDN,
			Type:  TypePTR,
			Class: ClassIN,
			TTL:   ttl,
			PTR:   rec.ptrTarget,
		})
	}
	slices.SortFunc(answers, func(a, b dnsRecord) int {
		return strings.Compare(strings.ToLower(a.PTR), strings.ToLower(b.PTR))
	})
	return answers
}

func knownAAnswers(resolved map[netip.Addr]cachedAddress, host string, now time.Time) []dnsRecord {
	var addrs []netip.Addr
	for addr, record := range resolved {
		if !now.Before(record.expires) || record.ttl == 0 || record.expires.Sub(now) < time.Duration(record.ttl)*time.Second/2 {
			continue
		}
		addrs = append(addrs, addr)
	}
	slices.SortFunc(addrs, func(a, b netip.Addr) int {
		return cmp.Compare(a.String(), b.String())
	})
	answers := make([]dnsRecord, 0, len(addrs))
	for _, addr := range addrs {
		record := resolved[addr]
		ttl := uint32(record.expires.Sub(now) / time.Second)
		if ttl == 0 {
			continue
		}
		answers = append(answers, dnsRecord{Name: host, Type: TypeA, Class: ClassIN, TTL: ttl, A: addr})
	}
	return answers
}

func applyTargetARecordPacket(cache map[netip.Addr]cachedAddress, host string, records []dnsRecord, now time.Time) {
	target := strings.ToLower(strings.TrimSuffix(host, "."))
	targetRecords := filterRecords(records, func(rr dnsRecord) bool {
		return rr.Type == TypeA && strings.EqualFold(strings.TrimSuffix(rr.Name, "."), target)
	})
	if len(targetRecords) == 0 {
		return
	}
	applyARecordPacket(map[string]map[netip.Addr]cachedAddress{target: cache}, targetRecords, now)
}

// ResolveHost queries mDNS for IPv4 A records matching host (e.g. "fes-de25.local")
// until at least one record arrives or ctx ends.
func ResolveHost(ctx context.Context, port network.RawFramePort, cfg BrowseConfig, host string) ([]netip.Addr, error) {
	if port == nil {
		return nil, fmt.Errorf("%w: nil frame port", ErrInvalidConfig)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.LocalMAC == ([6]byte{}) || cfg.LocalMAC[0]&1 != 0 {
		return nil, fmt.Errorf("%w: LocalMAC must be a non-zero unicast MAC", ErrInvalidConfig)
	}
	if !cfg.LocalIP.Is4() || cfg.LocalIP.IsUnspecified() || cfg.LocalIP.IsMulticast() {
		return nil, fmt.Errorf("%w: LocalIP must be a valid IPv4 host address", ErrInvalidConfig)
	}
	if cfg.QueryInterval < 0 {
		return nil, fmt.Errorf("%w: negative query interval", ErrInvalidConfig)
	}
	normHost, err := normalizeHostName(host)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(normHost, ".local") {
		normHost += ".local"
	}
	interval := cfg.QueryInterval
	if interval == 0 {
		interval = defaultBrowseQueryInterval
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	queryPayload, err := encodeMDNSMessage(dnsMessage{
		Questions: []dnsQuestion{{
			Name:   normHost,
			Type:   TypeA,
			Class:  ClassIN,
			WantQU: cfg.UnicastResponse,
		}},
	})
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	resolved := make(map[netip.Addr]cachedAddress)
	foundCh := make(chan struct{}, 1)

	activeAddrsLocked := func(now time.Time) []netip.Addr {
		var out []netip.Addr
		for addr, record := range resolved {
			if !now.Before(record.expires) {
				delete(resolved, addr)
				continue
			}
			out = append(out, addr)
		}
		slices.SortFunc(out, func(a, b netip.Addr) int {
			return cmp.Compare(a.String(), b.String())
		})
		return out
	}

	localIP4 := cfg.LocalIP.As4()
	remove := port.AddTap(func(frame []byte) {
		pkt, ok := parseMDNSFrame(frame, cfg.LocalMAC, localIP4, true)
		if !ok || !pkt.msg.Response {
			return
		}
		now := nowFn()
		mu.Lock()
		defer mu.Unlock()
		packetRecords := make([]dnsRecord, 0, len(pkt.msg.Answers)+len(pkt.msg.Additional))
		packetRecords = append(packetRecords, pkt.msg.Answers...)
		packetRecords = append(packetRecords, pkt.msg.Additional...)
		applyTargetARecordPacket(resolved, normHost, packetRecords, now)
		if len(activeAddrsLocked(now)) > 0 {
			select {
			case foundCh <- struct{}{}:
			default:
			}
		}
	})
	defer remove()

	sendQuery := func() error {
		if err := port.Err(); err != nil {
			return err
		}
		mu.Lock()
		knownAnswers := knownAAnswers(resolved, normHost, nowFn())
		mu.Unlock()
		payload := queryPayload
		if len(knownAnswers) > 0 {
			payload, err = encodeMDNSMessage(dnsMessage{
				Questions: []dnsQuestion{{
					Name:   normHost,
					Type:   TypeA,
					Class:  ClassIN,
					WantQU: cfg.UnicastResponse,
				}},
				Answers: knownAnswers,
			})
			if err != nil {
				return err
			}
		}
		frame := buildMDNSFrame(cfg.LocalMAC, multicastMACBytes, localIP4, multicastIPv4Bytes, Port, Port, uint16(browseSeq.Add(1)), payload)
		return port.SendRawFrame(frame)
	}

	if err := sendQuery(); err != nil {
		return nil, err
	}

	nextInterval := min(max(interval, minBrowseQueryInterval), maxBrowseQueryInterval)
	timer := time.NewTimer(nextInterval)
	defer timer.Stop()

	for {
		select {
		case <-foundCh:
			mu.Lock()
			out := activeAddrsLocked(nowFn())
			mu.Unlock()
			if len(out) > 0 {
				return out, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			if err := sendQuery(); err != nil {
				return nil, err
			}
			nextInterval = nextBrowseQueryInterval(nextInterval)
			timer.Reset(nextInterval)
		}
	}
}
