// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	defaultDNSCacheMaxEntries = 64
	defaultDNSCacheMaxTTL     = time.Hour
)

// DNSCacheConfig configures a bounded, TTL-aware in-memory DNSv4 cache.
type DNSCacheConfig struct {
	// MaxEntries bounds cached hostnames (default 64 when 0).
	MaxEntries int
	// MinTTL sets a floor for positive TTLs > 0. Zero-TTL responses remain
	// uncached per RFC 1035.
	MinTTL time.Duration
	// MaxTTL caps both positive and negative cache lifetimes (default 1h when 0).
	MaxTTL time.Duration
	// NegativeTTL configures how long ErrDNSNotFound and ErrDNSNoRecords results
	// are cached (0 disables negative caching).
	NegativeTTL time.Duration
	// Now overrides time.Now for deterministic tests.
	Now func() time.Time
}

// DNSCacheStats summarizes current occupancy and cumulative cache telemetry.
type DNSCacheStats struct {
	Entries      int
	MaxEntries   int
	Hits         uint64
	NegativeHits uint64
	Misses       uint64
	Evictions    uint64
	Expirations  uint64
}

type dnsCacheEntry struct {
	addrs       []netip.Addr
	negativeErr error
	expiresAt   time.Time
	lastUsed    uint64
}

// DNSCache stores positive IPv4 DNS answers and optional negative NXDOMAIN /
// no-record responses up to a bounded capacity with TTL expiration and LRU
// eviction.
type DNSCache struct {
	mu           sync.Mutex
	maxEntries   int
	minTTL       time.Duration
	maxTTL       time.Duration
	negativeTTL  time.Duration
	now          func() time.Time
	seq          uint64
	entries      map[string]dnsCacheEntry
	hits         uint64
	negativeHits uint64
	misses       uint64
	evictions    uint64
	expirations  uint64
}

// NewDNSCache constructs a bounded DNS cache from cfg.
func NewDNSCache(cfg DNSCacheConfig) (*DNSCache, error) {
	if cfg.MaxEntries < 0 || cfg.MinTTL < 0 || cfg.MaxTTL < 0 || cfg.NegativeTTL < 0 {
		return nil, fmt.Errorf("%w: negative DNS cache configuration parameter", ErrDNSInvalidConfig)
	}
	maxEntries := cfg.MaxEntries
	if maxEntries == 0 {
		maxEntries = defaultDNSCacheMaxEntries
	}
	maxTTL := cfg.MaxTTL
	if maxTTL == 0 {
		maxTTL = defaultDNSCacheMaxTTL
	}
	if cfg.MinTTL > maxTTL {
		return nil, fmt.Errorf("%w: cache MinTTL (%s) exceeds MaxTTL (%s)", ErrDNSInvalidConfig, cfg.MinTTL, maxTTL)
	}
	negTTL := cfg.NegativeTTL
	if negTTL > maxTTL {
		negTTL = maxTTL
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &DNSCache{
		maxEntries:  maxEntries,
		minTTL:      cfg.MinTTL,
		maxTTL:      maxTTL,
		negativeTTL: negTTL,
		now:         nowFn,
		entries:     make(map[string]dnsCacheEntry, maxEntries),
	}, nil
}

// Lookup returns a cached positive address list or cached negative error for
// name if present and unexpired.
func (c *DNSCache) Lookup(name string) (addrs []netip.Addr, negErr error, ok bool) {
	norm, err := normalizeDNSName(name)
	if err != nil {
		return nil, nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	entry, exists := c.entries[norm]
	if !exists {
		c.misses++
		return nil, nil, false
	}
	if !now.Before(entry.expiresAt) {
		delete(c.entries, norm)
		c.expirations++
		c.misses++
		return nil, nil, false
	}
	c.seq++
	entry.lastUsed = c.seq
	c.entries[norm] = entry

	if entry.negativeErr != nil {
		c.negativeHits++
		return nil, entry.negativeErr, true
	}
	c.hits++
	return append([]netip.Addr(nil), entry.addrs...), nil, true
}

// Store records addrs for name with lifetime ttl clamped to [MinTTL, MaxTTL].
// Zero or non-positive TTLs are not cached.
func (c *DNSCache) Store(name string, addrs []netip.Addr, ttl time.Duration) {
	if len(addrs) == 0 || ttl <= 0 {
		return
	}
	norm, err := normalizeDNSName(name)
	if err != nil {
		return
	}
	if ttl < c.minTTL {
		ttl = c.minTTL
	}
	if ttl > c.maxTTL {
		ttl = c.maxTTL
	}
	copied := append([]netip.Addr(nil), addrs...)

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	c.insertLocked(norm, now, dnsCacheEntry{
		addrs:     copied,
		expiresAt: now.Add(ttl),
	})
}

// StoreNegative caches ErrDNSNotFound or ErrDNSNoRecords for name when
// NegativeTTL > 0. Transient server or transport errors are ignored.
func (c *DNSCache) StoreNegative(name string, err error) {
	if c.negativeTTL <= 0 {
		return
	}
	var canonicalErr error
	switch {
	case errors.Is(err, ErrDNSNotFound):
		canonicalErr = ErrDNSNotFound
	case errors.Is(err, ErrDNSNoRecords):
		canonicalErr = ErrDNSNoRecords
	default:
		return
	}
	norm, normErr := normalizeDNSName(name)
	if normErr != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	c.insertLocked(norm, now, dnsCacheEntry{
		negativeErr: canonicalErr,
		expiresAt:   now.Add(c.negativeTTL),
	})
}

func (c *DNSCache) insertLocked(norm string, now time.Time, entry dnsCacheEntry) {
	if _, exists := c.entries[norm]; !exists && len(c.entries) >= c.maxEntries {
		c.pruneLocked(now)
		if len(c.entries) >= c.maxEntries {
			c.evictLRULocked()
		}
	}
	c.seq++
	entry.lastUsed = c.seq
	c.entries[norm] = entry
}

func (c *DNSCache) pruneLocked(now time.Time) int {
	removed := 0
	for k, e := range c.entries {
		if !now.Before(e.expiresAt) {
			delete(c.entries, k)
			c.expirations++
			removed++
		}
	}
	return removed
}

func (c *DNSCache) evictLRULocked() {
	var oldestKey string
	var oldestSeq uint64
	first := true
	for k, e := range c.entries {
		if first || e.lastUsed < oldestSeq {
			oldestKey = k
			oldestSeq = e.lastUsed
			first = false
		}
	}
	if !first {
		delete(c.entries, oldestKey)
		c.evictions++
	}
}

// Delete removes any cached entry for name.
func (c *DNSCache) Delete(name string) {
	norm, err := normalizeDNSName(name)
	if err != nil {
		return
	}
	c.mu.Lock()
	delete(c.entries, norm)
	c.mu.Unlock()
}

// Clear discards all cached entries while preserving cumulative statistics.
func (c *DNSCache) Clear() {
	c.mu.Lock()
	clear(c.entries)
	c.mu.Unlock()
}

// Prune removes all expired entries and returns the count evicted.
func (c *DNSCache) Prune() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pruneLocked(c.now())
}

// Stats returns a snapshot of cache occupancy and cumulative counters.
func (c *DNSCache) Stats() DNSCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return DNSCacheStats{
		Entries:      len(c.entries),
		MaxEntries:   c.maxEntries,
		Hits:         c.hits,
		NegativeHits: c.negativeHits,
		Misses:       c.misses,
		Evictions:    c.evictions,
		Expirations:  c.expirations,
	}
}

// Peek returns any unexpired cached entry for name and its remaining TTL
// without altering LRU ordering, hit/miss statistics, or evicting expired rows.
func (c *DNSCache) Peek(name string) (addrs []netip.Addr, negErr error, ttlRemaining time.Duration, ok bool) {
	if c == nil {
		return nil, nil, 0, false
	}
	norm, err := normalizeDNSName(name)
	if err != nil {
		return nil, nil, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	entry, exists := c.entries[norm]
	if !exists || !now.Before(entry.expiresAt) {
		return nil, nil, 0, false
	}
	rem := entry.expiresAt.Sub(now)
	if entry.negativeErr != nil {
		return nil, entry.negativeErr, rem, true
	}
	return append([]netip.Addr(nil), entry.addrs...), nil, rem, true
}

// Names returns a sorted list of currently unexpired cached hostnames without
// altering LRU ordering or cumulative cache counters.
func (c *DNSCache) Names() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	var names []string
	for k, e := range c.entries {
		if now.Before(e.expiresAt) {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	return names
}

// DNSResolver resolves IPv4 A records using a bounded TTL cache backed by
// ResolveAWithTTL over a RawFramePort or an injectable lookup function.
type DNSResolver struct {
	cache  *DNSCache
	lookup func(ctx context.Context, name string) (DNSAnswer, error)
}

// NewDNSResolver constructs a cached DNSv4 resolver over port using cfg.
// If cache is nil, a default-configured DNSCache is created.
func NewDNSResolver(port RawFramePort, cfg DNSConfig, cache *DNSCache) (*DNSResolver, error) {
	if port == nil {
		return nil, fmt.Errorf("%w: nil port", ErrDNSInvalidConfig)
	}
	cfgCopy := cfg
	if len(cfg.Servers) > 0 {
		cfgCopy.Servers = append([]string(nil), cfg.Servers...)
	}
	return NewDNSResolverFunc(func(ctx context.Context, name string) (DNSAnswer, error) {
		return ResolveAWithTTL(ctx, port, cfgCopy, name)
	}, cache)
}

// NewDNSResolverFunc constructs a cached DNSv4 resolver using lookup for cache
// misses. If cache is nil, a default-configured DNSCache is created.
func NewDNSResolverFunc(lookup func(ctx context.Context, name string) (DNSAnswer, error), cache *DNSCache) (*DNSResolver, error) {
	if lookup == nil {
		return nil, fmt.Errorf("%w: nil lookup function", ErrDNSInvalidConfig)
	}
	if cache == nil {
		var err error
		cache, err = NewDNSCache(DNSCacheConfig{})
		if err != nil {
			return nil, err
		}
	}
	return &DNSResolver{
		cache:  cache,
		lookup: lookup,
	}, nil
}

// Cache returns the resolver's backing DNSCache.
func (r *DNSResolver) Cache() *DNSCache { return r.cache }

// ResolveA resolves name to one or more IPv4 addresses, checking the cache
// before querying upstream DNS servers. Numeric IPv4 literals return directly
// without caching.
func (r *DNSResolver) ResolveA(ctx context.Context, name string) ([]netip.Addr, error) {
	return r.resolveHost(ctx, name, true)
}

// ResolveFresh bypasses cached answers to query upstream directly, refreshing
// positive/negative cache entries on response (deleting stale cache state if a
// positive response has zero TTL) and leaving prior cache untouched on transient errors.
func (r *DNSResolver) ResolveFresh(ctx context.Context, name string) ([]netip.Addr, error) {
	return r.resolveHost(ctx, name, false)
}

func (r *DNSResolver) resolveHost(ctx context.Context, name string, useCache bool) ([]netip.Addr, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: nil resolver", ErrDNSInvalidConfig)
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrDNSInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(name)
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		if !addr.Is4() {
			return nil, fmt.Errorf("%w: IPv6 literal unsupported", ErrDNSInvalidConfig)
		}
		return []netip.Addr{addr}, nil
	}
	norm, err := normalizeDNSName(trimmed)
	if err != nil {
		return nil, err
	}
	if useCache {
		if addrs, negErr, ok := r.cache.Lookup(norm); ok {
			if negErr != nil {
				return nil, negErr
			}
			return addrs, nil
		}
	}
	ans, err := r.lookup(ctx, norm)
	if err != nil {
		if errors.Is(err, ErrDNSNotFound) || errors.Is(err, ErrDNSNoRecords) {
			r.cache.StoreNegative(norm, err)
		}
		return nil, err
	}
	if len(ans.Addrs) > 0 && ans.TTL > 0 {
		r.cache.Store(norm, ans.Addrs, ans.TTL)
	} else if !useCache {
		r.cache.Delete(norm)
	}
	return append([]netip.Addr(nil), ans.Addrs...), nil
}
