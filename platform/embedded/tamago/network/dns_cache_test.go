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
	"testing"
	"time"
)

func makeDNSResponseWithTTLs(id uint16, flags uint16, qname string, cnames []struct {
	name   string
	target string
	ttl    uint32
}, aRecs []struct {
	name string
	ip   [4]byte
	ttl  uint32
}) []byte {
	var b bytes.Buffer
	var hdr [12]byte
	binary.BigEndian.PutUint16(hdr[0:2], id)
	binary.BigEndian.PutUint16(hdr[2:4], flags)
	binary.BigEndian.PutUint16(hdr[4:6], 1)
	binary.BigEndian.PutUint16(hdr[6:8], uint16(len(cnames)+len(aRecs)))
	b.Write(hdr[:])

	qWire, _ := encodeDNSName(qname)
	b.Write(qWire)
	b.Write([]byte{0, dnsTypeA, 0, dnsClassIN})

	for _, cn := range cnames {
		nWire, _ := encodeDNSName(cn.name)
		tWire, _ := encodeDNSName(cn.target)
		b.Write(nWire)
		var rr [10]byte
		binary.BigEndian.PutUint16(rr[0:2], dnsTypeCNAME)
		binary.BigEndian.PutUint16(rr[2:4], dnsClassIN)
		binary.BigEndian.PutUint32(rr[4:8], cn.ttl)
		binary.BigEndian.PutUint16(rr[8:10], uint16(len(tWire)))
		b.Write(rr[:])
		b.Write(tWire)
	}
	for _, a := range aRecs {
		nWire, _ := encodeDNSName(a.name)
		b.Write(nWire)
		var rr [10]byte
		binary.BigEndian.PutUint16(rr[0:2], dnsTypeA)
		binary.BigEndian.PutUint16(rr[2:4], dnsClassIN)
		binary.BigEndian.PutUint32(rr[4:8], a.ttl)
		binary.BigEndian.PutUint16(rr[8:10], 4)
		b.Write(rr[:])
		b.Write(a.ip[:])
	}
	return b.Bytes()
}

func TestParseDNSResponseWithTTL(t *testing.T) {
	// Minimum across traversed CNAME chain + matching A records, ignoring unrelated CNAME.
	msg := makeDNSResponseWithTTLs(0x1111, 0x8180, "app.example.test",
		[]struct {
			name   string
			target string
			ttl    uint32
		}{
			{"other.example.test", "elsewhere.example.test", 2},
			{"app.example.test", "hop1.example.test", 120},
			{"hop1.example.test", "edge.example.test", 15},
		},
		[]struct {
			name string
			ip   [4]byte
			ttl  uint32
		}{
			{"edge.example.test", [4]byte{192, 0, 2, 10}, 45},
			{"edge.example.test", [4]byte{192, 0, 2, 11}, 9},
		},
	)
	ans, err := ParseDNSResponseWithTTL(msg, 0x1111, "APP.EXAMPLE.TEST.")
	if err != nil {
		t.Fatalf("ParseDNSResponseWithTTL: %v", err)
	}
	if len(ans.Addrs) != 2 || ans.TTL != 9*time.Second {
		t.Fatalf("got addrs=%v ttl=%s, want 2 addrs ttl=9s", ans.Addrs, ans.TTL)
	}

	// Traversed CNAME TTL lower than matched A TTL determines effective TTL.
	cnameMinMsg := makeDNSResponseWithTTLs(0x1112, 0x8180, "alias.example.test",
		[]struct {
			name   string
			target string
			ttl    uint32
		}{{"alias.example.test", "target.example.test", 7}},
		[]struct {
			name string
			ip   [4]byte
			ttl  uint32
		}{{"target.example.test", [4]byte{192, 0, 2, 12}, 300}},
	)
	ans, err = ParseDNSResponseWithTTL(cnameMinMsg, 0x1112, "alias.example.test")
	if err != nil || ans.TTL != 7*time.Second {
		t.Fatalf("CNAME-limited TTL got (%+v, %v), want ttl=7s", ans, err)
	}

	// RFC 2181 section 8: high-bit-set TTLs must clamp to 0 (non-cacheable).
	highBitMsg := makeDNSResponseWithTTLs(0x1113, 0x8180, "signed.example.test", nil,
		[]struct {
			name string
			ip   [4]byte
			ttl  uint32
		}{{"signed.example.test", [4]byte{192, 0, 2, 13}, 0x80000010}},
	)
	ans, err = ParseDNSResponseWithTTL(highBitMsg, 0x1113, "signed.example.test")
	if err != nil || ans.TTL != 0 {
		t.Fatalf("high-bit TTL got (%+v, %v), want ttl=0", ans, err)
	}
}

func TestDNSCacheLifecycleTTLAndLRU(t *testing.T) {
	for _, cfg := range []DNSCacheConfig{
		{MaxEntries: -1},
		{MinTTL: -time.Second},
		{MaxTTL: -time.Second},
		{NegativeTTL: -time.Second},
		{MinTTL: 2 * time.Minute, MaxTTL: time.Minute},
		{MinTTL: 2 * defaultDNSCacheMaxTTL},
	} {
		if _, err := NewDNSCache(cfg); !errors.Is(err, ErrDNSInvalidConfig) {
			t.Errorf("NewDNSCache(%+v) err=%v, want ErrDNSInvalidConfig", cfg, err)
		}
	}

	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	cache, err := NewDNSCache(DNSCacheConfig{
		MaxEntries:  2,
		MinTTL:      10 * time.Second,
		MaxTTL:      60 * time.Second,
		NegativeTTL: 120 * time.Second, // clamped to MaxTTL (60s)
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewDNSCache: %v", err)
	}

	// Invalid names & TTL==0 must never enter the cache.
	if _, _, ok := cache.Lookup("bad host"); ok {
		t.Fatal("invalid host returned cache hit")
	}
	cache.Store("zero.example.test", []netip.Addr{netip.MustParseAddr("192.0.2.1")}, 0)
	if _, _, ok := cache.Lookup("zero.example.test"); ok {
		t.Fatal("TTL=0 entry should not be cached even with MinTTL > 0")
	}

	// Store "a" with ttl=2s (clamped up to MinTTL=10s) and mutate caller slice after Store.
	inputA := []netip.Addr{netip.MustParseAddr("192.0.2.1")}
	cache.Store("A.EXAMPLE.TEST.", inputA, 2*time.Second)
	inputA[0] = netip.MustParseAddr("127.0.0.1")

	gotA, negErr, ok := cache.Lookup("a.example.test")
	if !ok || negErr != nil || len(gotA) != 1 || gotA[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("Lookup(a) = (%v, %v, %t)", gotA, negErr, ok)
	}
	// Mutate returned slice; cache entry must stay untouched.
	gotA[0] = netip.MustParseAddr("127.0.0.2")
	gotA2, _, ok := cache.Lookup("a.example.test")
	if !ok || gotA2[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("cache slice mutated via Lookup caller: %v", gotA2)
	}

	// Store "b" with ttl=500s (clamped down to MaxTTL=60s).
	cache.Store("b.example.test", []netip.Addr{netip.MustParseAddr("192.0.2.2")}, 500*time.Second)

	// Access "a" so "b" becomes LRU, then store "c" to trigger LRU eviction of "b".
	_, _, _ = cache.Lookup("a.example.test")
	cache.Store("c.example.test", []netip.Addr{netip.MustParseAddr("192.0.2.3")}, 30*time.Second)

	if _, _, ok := cache.Lookup("b.example.test"); ok {
		t.Fatal("expected LRU entry b.example.test to be evicted")
	}

	// Advance 10s: "a" (MinTTL=10s) expires while "c" (30s) stays live.
	now = now.Add(10 * time.Second)
	if _, _, ok := cache.Lookup("a.example.test"); ok {
		t.Fatal("expected MinTTL-clamped a.example.test to expire at +10s")
	}
	if gotC, _, ok := cache.Lookup("c.example.test"); !ok || gotC[0] != netip.MustParseAddr("192.0.2.3") {
		t.Fatalf("expected c.example.test still cached at +10s, got (%v, %t)", gotC, ok)
	}

	// Negative caching caches only ErrDNSNotFound/ErrDNSNoRecords and ignores transient errors.
	cache.StoreNegative("transient.example.test", ErrDNSServerError)
	if _, _, ok := cache.Lookup("transient.example.test"); ok {
		t.Fatal("transient DNS error should not be cached")
	}
	cache.StoreNegative("missing.example.test", ErrDNSNotFound)
	_, neg, ok := cache.Lookup("missing.example.test")
	if !ok || !errors.Is(neg, ErrDNSNotFound) {
		t.Fatalf("negative lookup got (%v, %t), want ErrDNSNotFound", neg, ok)
	}
	// Advance 60s -> clamped NegativeTTL (60s) expires and Prune reaps it.
	now = now.Add(60 * time.Second)
	if pruned := cache.Prune(); pruned != 2 {
		t.Fatalf("Prune removed %d entries, want 2 (c and missing)", pruned)
	}

	st := cache.Stats()
	if st.Entries != 0 || st.MaxEntries != 2 || st.Hits != 4 || st.NegativeHits != 1 || st.Evictions != 1 || st.Expirations != 3 || st.Misses != 4 {
		t.Fatalf("unexpected DNSCache stats: %+v", st)
	}

	// Delete & Clear helpers.
	cache.Store("del.example.test", []netip.Addr{netip.MustParseAddr("192.0.2.9")}, 20*time.Second)
	cache.Delete("DEL.EXAMPLE.TEST.")
	if cache.Stats().Entries != 0 {
		t.Fatalf("expected 0 entries after Delete, got %d", cache.Stats().Entries)
	}
	cache.Store("keep.example.test", []netip.Addr{netip.MustParseAddr("192.0.2.10")}, 20*time.Second)
	cache.Clear()
	if cache.Stats().Entries != 0 {
		t.Fatalf("expected 0 entries after Clear, got %d", cache.Stats().Entries)
	}

	// NegativeTTL == 0 disables negative caching.
	noNegCache, err := NewDNSCache(DNSCacheConfig{NegativeTTL: 0})
	if err != nil {
		t.Fatal(err)
	}
	noNegCache.StoreNegative("missing.test", ErrDNSNotFound)
	if _, _, ok := noNegCache.Lookup("missing.test"); ok {
		t.Fatal("negative entry stored with NegativeTTL=0")
	}
}

func TestDNSResolverWireCacheTTLAndErrors(t *testing.T) {
	if _, err := NewDNSResolver(nil, DNSConfig{}, nil); !errors.Is(err, ErrDNSInvalidConfig) {
		t.Fatalf("nil port err=%v, want ErrDNSInvalidConfig", err)
	}
	if _, err := NewDNSResolverFunc(nil, nil); !errors.Is(err, ErrDNSInvalidConfig) {
		t.Fatalf("nil lookup err=%v, want ErrDNSInvalidConfig", err)
	}

	clientMAC := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	dnsMAC := [6]byte{0x52, 0x55, 0x0a, 0x00, 0x02, 0x03}
	clientIP := [4]byte{10, 0, 2, 15}
	dnsIP := [4]byte{10, 0, 2, 3}

	port := newFakeRawPort()
	var wireQueries atomic.Int32
	port.onSend = func(frame []byte, p *fakeRawPort) {
		if binary.BigEndian.Uint16(frame[12:14]) != ethTypeIPv4 {
			return
		}
		udp := frame[ethHeaderLen+ipv4MinLen:]
		srcPort := binary.BigEndian.Uint16(udp[0:2])
		dnsPayload := udp[udpHeaderLen:]
		id := binary.BigEndian.Uint16(dnsPayload[0:2])
		wireQueries.Add(1)

		reply := makeDNSResponseWithTTLs(id, 0x8180, "cached.example.test", nil,
			[]struct {
				name string
				ip   [4]byte
				ttl  uint32
			}{{"cached.example.test", [4]byte{203, 0, 113, 5}, 30}},
		)
		go p.inject(buildUDPv4Frame(dnsMAC, clientMAC, dnsIP, clientIP, dnsPort, srcPort, 1, reply))
	}

	now := time.Date(2026, time.November, 25, 13, 0, 0, 0, time.UTC)
	cache, err := NewDNSCache(DNSCacheConfig{
		MaxEntries:  8,
		NegativeTTL: 15 * time.Second,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewDNSResolver(port, DNSConfig{
		LocalMAC:      "52:54:00:12:34:56",
		LocalPrefix:   "10.0.2.15/24",
		Servers:       []string{"10.0.2.3"},
		NextHopMAC:    "52:55:0a:00:02:03",
		Timeout:       200 * time.Millisecond,
		RetryInterval: 5 * time.Millisecond,
	}, cache)
	if err != nil {
		t.Fatalf("NewDNSResolver: %v", err)
	}

	addrs, err := resolver.ResolveA(context.Background(), "cached.example.test")
	if err != nil || len(addrs) != 1 || addrs[0] != netip.MustParseAddr("203.0.113.5") {
		t.Fatalf("1st ResolveA = (%v, %v)", addrs, err)
	}
	if wireQueries.Load() != 1 {
		t.Fatalf("wireQueries after 1st lookup = %d, want 1", wireQueries.Load())
	}

	// Second lookup (with different case + trailing dot) must hit cache with zero wire traffic.
	addrs, err = resolver.ResolveA(context.Background(), "CACHED.EXAMPLE.TEST.")
	if err != nil || len(addrs) != 1 || addrs[0] != netip.MustParseAddr("203.0.113.5") {
		t.Fatalf("2nd ResolveA = (%v, %v)", addrs, err)
	}
	if wireQueries.Load() != 1 {
		t.Fatalf("wireQueries after cached lookup = %d, want 1", wireQueries.Load())
	}

	// Expire the 30s TTL -> third lookup must hit the wire again.
	now = now.Add(31 * time.Second)
	addrs, err = resolver.ResolveA(context.Background(), "cached.example.test")
	if err != nil || wireQueries.Load() != 2 {
		t.Fatalf("post-expiry ResolveA = (%v, %v) wireQueries=%d, want 2", addrs, err, wireQueries.Load())
	}

	// Unit-level tests for negative caching, non-caching of transient errors, TTL=0, and concurrency.
	var calls atomic.Int32
	funcResolver, err := NewDNSResolverFunc(func(_ context.Context, name string) (DNSAnswer, error) {
		n := calls.Add(1)
		switch name {
		case "zero.test":
			return DNSAnswer{Addrs: []netip.Addr{netip.MustParseAddr("198.51.100.1")}, TTL: 0}, nil
		case "nx.test":
			return DNSAnswer{}, ErrDNSNotFound
		case "empty.test":
			return DNSAnswer{}, ErrDNSNoRecords
		case "flaky.test":
			if n%2 == 1 {
				return DNSAnswer{}, ErrDNSServerError
			}
			return DNSAnswer{Addrs: []netip.Addr{netip.MustParseAddr("198.51.100.2")}, TTL: time.Minute}, nil
		default:
			return DNSAnswer{Addrs: []netip.Addr{netip.MustParseAddr(fmt.Sprintf("198.51.100.%d", (n%200)+1))}, TTL: time.Minute}, nil
		}
	}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if funcResolver.Cache() != cache {
		t.Fatal("Cache() did not return passed cache")
	}
	if _, err := funcResolver.ResolveA(nil, "a.test"); !errors.Is(err, ErrDNSInvalidConfig) {
		t.Fatalf("nil ctx err=%v", err)
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := funcResolver.ResolveA(canceledCtx, "a.test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ctx err=%v", err)
	}
	if lit, err := funcResolver.ResolveA(context.Background(), "127.0.0.1"); err != nil || lit[0] != netip.MustParseAddr("127.0.0.1") {
		t.Fatalf("IPv4 literal = (%v, %v)", lit, err)
	}
	if _, err := funcResolver.ResolveA(context.Background(), "::1"); !errors.Is(err, ErrDNSInvalidConfig) {
		t.Fatalf("IPv6 literal err=%v", err)
	}

	// Zero-TTL lookup must not cache.
	callsBeforeZero := calls.Load()
	_, _ = funcResolver.ResolveA(context.Background(), "zero.test")
	_, _ = funcResolver.ResolveA(context.Background(), "zero.test")
	if calls.Load()-callsBeforeZero != 2 {
		t.Fatalf("zero-TTL cached unexpectedly: deltaCalls=%d", calls.Load()-callsBeforeZero)
	}

	// Negative results cache on first miss; second returns cached error without calling lookup.
	callsBeforeNeg := calls.Load()
	if _, err := funcResolver.ResolveA(context.Background(), "nx.test"); !errors.Is(err, ErrDNSNotFound) {
		t.Fatalf("nx err=%v", err)
	}
	if _, err := funcResolver.ResolveA(context.Background(), "nx.test"); !errors.Is(err, ErrDNSNotFound) {
		t.Fatalf("nx cached err=%v", err)
	}
	if _, err := funcResolver.ResolveA(context.Background(), "empty.test"); !errors.Is(err, ErrDNSNoRecords) {
		t.Fatalf("empty err=%v", err)
	}
	if _, err := funcResolver.ResolveA(context.Background(), "empty.test"); !errors.Is(err, ErrDNSNoRecords) {
		t.Fatalf("empty cached err=%v", err)
	}
	if calls.Load()-callsBeforeNeg != 2 {
		t.Fatalf("negative caching called upstream %d times, want 2", calls.Load()-callsBeforeNeg)
	}

	// Transient ErrDNSServerError must not cache.
	for calls.Load()%2 != 0 {
		calls.Add(1)
	}
	if _, err := funcResolver.ResolveA(context.Background(), "flaky.test"); !errors.Is(err, ErrDNSServerError) {
		t.Fatalf("first flaky call err=%v", err)
	}
	if addrs, err := funcResolver.ResolveA(context.Background(), "flaky.test"); err != nil || len(addrs) != 1 {
		t.Fatalf("second flaky call = (%v, %v), want recovery without negative cache", addrs, err)
	}

	// Concurrent -race stress test on default cache construction.
	concResolver, err := NewDNSResolverFunc(func(_ context.Context, _ string) (DNSAnswer, error) {
		return DNSAnswer{Addrs: []netip.Addr{netip.MustParseAddr("198.51.100.77")}, TTL: time.Minute}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				host := fmt.Sprintf("h%d.example.test", (id+i)%10)
				if _, err := concResolver.ResolveA(context.Background(), host); err != nil {
					t.Errorf("concurrent ResolveA(%q): %v", host, err)
				}
				_ = concResolver.Cache().Stats()
			}
		}(g)
	}
	wg.Wait()
}
