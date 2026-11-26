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
	defaultBrowseQueryInterval = 500 * time.Millisecond
)

// BrowseConfig configures active mDNS browsing and host resolution over a RawFramePort.
type BrowseConfig struct {
	LocalMAC        [6]byte       // Unicast non-zero client Ethernet MAC
	LocalIP         netip.Addr    // Client IPv4 host address used as source IP
	QueryInterval   time.Duration // Retransmit interval while browsing (default 500ms; <0 invalid)
	UnicastResponse bool          // Request unicast replies (sets RFC 6762 QU bit)
	StopOnFirst     bool          // When true, Browse returns as soon as at least one complete service is resolved
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

	type instanceRecord struct {
		instance string
		ttl      uint32
		hasPTR   bool
		srvHost  string
		srvPort  uint16
		hasSRV   bool
		txt      []string
	}

	var mu sync.Mutex
	instances := make(map[string]*instanceRecord)
	hostAddrs := make(map[string][]netip.Addr)
	readyCh := make(chan struct{}, 1)

	getInst := func(key, display string) *instanceRecord {
		rec := instances[key]
		if rec == nil {
			rec = &instanceRecord{instance: display}
			instances[key] = rec
		}
		return rec
	}

	snapshotLocked := func() []DiscoveredService {
		var out []DiscoveredService
		for _, rec := range instances {
			if !rec.hasPTR || !rec.hasSRV || rec.ttl == 0 {
				continue
			}
			addrs := hostAddrs[strings.ToLower(rec.srvHost)]
			if len(addrs) == 0 {
				continue
			}
			sortedAddrs := append([]netip.Addr(nil), addrs...)
			slices.SortFunc(sortedAddrs, func(a, b netip.Addr) int {
				return cmp.Compare(a.String(), b.String())
			})
			out = append(out, DiscoveredService{
				Instance:    rec.instance,
				ServiceType: svcType,
				Domain:      "local",
				Host:        rec.srvHost,
				Port:        rec.srvPort,
				Addrs:       sortedAddrs,
				TXT:         append([]string(nil), rec.txt...),
				TTL:         rec.ttl,
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
		mu.Lock()
		defer mu.Unlock()

		handleRR := func(rr dnsRecord) {
			name := strings.TrimSuffix(rr.Name, ".")
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
						rec.ttl = 0
					}
					return
				}
				display := instFQDN
				if idx := strings.IndexByte(instFQDN, '.'); idx > 0 {
					display = instFQDN[:idx]
				}
				rec := getInst(key, display)
				rec.hasPTR = true
				rec.ttl = rr.TTL
			case TypeSRV:
				key := strings.ToLower(name)
				if rr.TTL == 0 {
					if rec := instances[key]; rec != nil {
						rec.hasSRV = false
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
			case TypeTXT:
				key := strings.ToLower(name)
				display := name
				if idx := strings.IndexByte(name, '.'); idx > 0 {
					display = name[:idx]
				}
				rec := getInst(key, display)
				if rr.TTL == 0 {
					rec.txt = nil
				} else {
					rec.txt = append([]string(nil), rr.TXT...)
				}
			case TypeA:
				hostKey := strings.ToLower(name)
				if rr.TTL == 0 {
					var kept []netip.Addr
					for _, a := range hostAddrs[hostKey] {
						if a != rr.A {
							kept = append(kept, a)
						}
					}
					hostAddrs[hostKey] = kept
					return
				}
				if !slices.Contains(hostAddrs[hostKey], rr.A) {
					hostAddrs[hostKey] = append(hostAddrs[hostKey], rr.A)
				}
			}
		}

		for _, rr := range pkt.msg.Answers {
			handleRR(rr)
		}
		for _, rr := range pkt.msg.Additional {
			handleRR(rr)
		}
		if cfg.StopOnFirst && len(snapshotLocked()) > 0 {
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
		frame := buildMDNSFrame(cfg.LocalMAC, multicastMACBytes, localIP4, multicastIPv4Bytes, Port, Port, uint16(browseSeq.Add(1)), queryPayload)
		return port.SendRawFrame(frame)
	}

	if err := sendQuery(); err != nil {
		return nil, err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-readyCh:
			mu.Lock()
			res := snapshotLocked()
			mu.Unlock()
			return res, nil
		case <-ctx.Done():
			mu.Lock()
			res := snapshotLocked()
			mu.Unlock()
			if len(res) > 0 {
				return res, nil
			}
			return nil, ctx.Err()
		case <-ticker.C:
			if err := sendQuery(); err != nil {
				return nil, err
			}
		}
	}
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
	var resolved []netip.Addr
	foundCh := make(chan struct{}, 1)

	localIP4 := cfg.LocalIP.As4()
	remove := port.AddTap(func(frame []byte) {
		pkt, ok := parseMDNSFrame(frame, cfg.LocalMAC, localIP4, true)
		if !ok || !pkt.msg.Response {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		checkRR := func(rr dnsRecord) {
			if rr.Type != TypeA || rr.TTL == 0 || !rr.A.Is4() {
				return
			}
			if !strings.EqualFold(strings.TrimSuffix(rr.Name, "."), normHost) {
				return
			}
			if !slices.Contains(resolved, rr.A) {
				resolved = append(resolved, rr.A)
			}
		}
		for _, rr := range pkt.msg.Answers {
			checkRR(rr)
		}
		for _, rr := range pkt.msg.Additional {
			checkRR(rr)
		}
		if len(resolved) > 0 {
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
		frame := buildMDNSFrame(cfg.LocalMAC, multicastMACBytes, localIP4, multicastIPv4Bytes, Port, Port, uint16(browseSeq.Add(1)), queryPayload)
		return port.SendRawFrame(frame)
	}

	if err := sendQuery(); err != nil {
		return nil, err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-foundCh:
			mu.Lock()
			out := append([]netip.Addr(nil), resolved...)
			mu.Unlock()
			return out, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if err := sendQuery(); err != nil {
				return nil, err
			}
		}
	}
}
