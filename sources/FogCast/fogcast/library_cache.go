package fogcast

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

const romCacheMemoTTL = 5 * time.Second

// LibraryCache is GET /api/v1/library/cache. ROM used/free come from a
// lease-free target GET /v2/cache. Cover used/free live on the kit FAT store
// and are not reported here.
type LibraryCache struct {
	ROM        ROMCacheStatus `json:"rom"`
	SyncedUnix int64          `json:"synced_unix,omitempty"`
}

// ROMCacheStatus is the target ROM cache budget. Reachable is false when the
// selected target did not answer; used/free are then omitted as unknown.
type ROMCacheStatus struct {
	UsedBytes int64 `json:"used_bytes"`
	MaxBytes  int64 `json:"max_bytes"`
	FreeBytes int64 `json:"free_bytes"`
	Reachable bool  `json:"reachable"`
}

type cacheIndexClient interface {
	CacheIndex(context.Context) (protocol.CacheIndex, error)
}

type romCacheSnapshot struct {
	at        time.Time
	reachable bool
	index     protocol.CacheIndex
	present   map[string]struct{}
}

func romCacheKey(system protocol.System, digest string) string {
	return string(system) + "/" + digest
}

// LibraryCache reports ROM cache used/free from the selected target. It does
// not claim a kit lease. Cover store accounting stays on the kit.
func (s *Service) LibraryCache(ctx context.Context) (LibraryCache, error) {
	snap := s.romCacheSnapshot(ctx)
	out := LibraryCache{ROM: ROMCacheStatus{Reachable: snap.reachable}}
	if !snap.reachable {
		return out, nil
	}
	out.ROM.UsedBytes = snap.index.UsedBytes
	out.ROM.MaxBytes = snap.index.MaxBytes
	out.ROM.FreeBytes = snap.index.FreeBytes
	if !snap.at.IsZero() {
		out.SyncedUnix = snap.at.Unix()
	}
	return out, nil
}

// ROMCachePresence maps system/sha256 to inventory presence. known is false
// when the target did not answer; callers must omit rom_cached rather than
// invent false.
func (s *Service) ROMCachePresence(ctx context.Context) (map[string]bool, bool) {
	snap := s.romCacheSnapshot(ctx)
	if !snap.reachable {
		return nil, false
	}
	out := make(map[string]bool, len(snap.present))
	for key := range snap.present {
		out[key] = true
	}
	return out, true
}

func (s *Service) romCacheSnapshot(parent context.Context) romCacheSnapshot {
	now := time.Now()
	s.romCacheMu.Lock()
	if !s.romCacheSnap.at.IsZero() && now.Sub(s.romCacheSnap.at) < romCacheMemoTTL {
		snap := s.romCacheSnap
		s.romCacheMu.Unlock()
		return snap
	}
	s.romCacheMu.Unlock()

	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	snap := romCacheSnapshot{at: now, present: map[string]struct{}{}}
	client, ok := s.selectedClientSnapshot()
	if !ok {
		s.storeROMCacheSnap(snap)
		return snap
	}
	indexer, ok := client.(cacheIndexClient)
	if !ok {
		s.storeROMCacheSnap(snap)
		return snap
	}
	index, err := indexer.CacheIndex(ctx)
	if err != nil {
		s.storeROMCacheSnap(snap)
		return snap
	}
	if index.Entries == nil {
		index.Entries = []protocol.CacheIndexEntry{}
	}
	snap.reachable = true
	snap.index = index
	for _, entry := range index.Entries {
		if entry.SHA256 == "" {
			continue
		}
		snap.present[romCacheKey(entry.System, entry.SHA256)] = struct{}{}
	}
	s.storeROMCacheSnap(snap)
	return snap
}

func (s *Service) storeROMCacheSnap(snap romCacheSnapshot) {
	s.romCacheMu.Lock()
	s.romCacheSnap = snap
	s.romCacheMu.Unlock()
}
