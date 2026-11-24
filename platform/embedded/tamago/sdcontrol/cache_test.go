// SPDX-License-Identifier: GPL-3.0-or-later

package sdcontrol

import (
	"bytes"
	"errors"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

func TestSessionCacheHitsQuiesceRefusalAndResumeInvalidation(t *testing.T) {
	var nilSession *Session
	if err := nilSession.EnableCache(block.CacheConfig{Capacity: 8}); !errors.Is(err, ErrRetired) {
		t.Fatalf("nil EnableCache err=%v, want ErrRetired", err)
	}
	if _, ok := nilSession.CacheStats(); ok {
		t.Fatal("nil CacheStats unexpectedly reported enabled")
	}
	nilSession.DisableCache()

	r, c := newRPC(t, "")
	c.image = make([]byte, 8192*512)
	for i := 0; i < 512; i++ {
		c.image[512+i] = byte(0xa0 + (i & 0x0f))
	}

	s, err := Open(r, c, Config{InputClockHz: 200_000_000})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := s.CacheStats(); ok {
		t.Fatal("CacheStats reported enabled before EnableCache")
	}
	if err := s.EnableCache(block.CacheConfig{Capacity: 0}); err == nil {
		t.Fatal("expected invalid cache config error")
	}
	if err := s.EnableCache(block.CacheConfig{Capacity: 16}); err != nil {
		t.Fatalf("EnableCache: %v", err)
	}

	// First read misses and populates cache; second read hits cache without issuing PIO commands.
	buf1 := make([]byte, 512)
	if err := s.ReadSectors(1, buf1); err != nil {
		t.Fatalf("first ReadSectors: %v", err)
	}
	cmdsAfterMiss := c.commands
	buf2 := make([]byte, 512)
	if err := s.ReadSectors(1, buf2); err != nil {
		t.Fatalf("second ReadSectors: %v", err)
	}
	if c.commands != cmdsAfterMiss {
		t.Fatalf("cached read hit hardware: commands %d -> %d", cmdsAfterMiss, c.commands)
	}
	if !bytes.Equal(buf1, buf2) {
		t.Fatal("cached read bytes differ")
	}
	stats, ok := s.CacheStats()
	if !ok || stats.Misses != 1 || stats.Hits != 1 {
		t.Fatalf("CacheStats=%+v ok=%t, want 1 miss and 1 hit", stats, ok)
	}

	// Read-only WriteSectors refuses cleanly without retiring the session.
	if err := s.WriteSectors(1, buf1); !errors.Is(err, block.ErrReadOnly) {
		t.Fatalf("read-only WriteSectors err=%v, want ErrReadOnly", err)
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	// Re-populate sector 1 in the cache before Quiesce so Quiesce invalidates a live entry.
	if err := s.ReadSectors(1, buf1); err != nil {
		t.Fatalf("repopulate ReadSectors: %v", err)
	}
	beforeQuiesce, _ := s.CacheStats()
	if beforeQuiesce.Entries != 1 {
		t.Fatalf("expected 1 cached entry before Quiesce, got %+v", beforeQuiesce)
	}

	// Quiesce must both refuse cached reads AND invalidate cached sectors.
	if err := s.Quiesce(); err != nil {
		t.Fatalf("Quiesce: %v", err)
	}
	afterQuiesce, _ := s.CacheStats()
	if afterQuiesce.Entries != 0 || afterQuiesce.Invalidations <= beforeQuiesce.Invalidations {
		t.Fatalf("Quiesce did not invalidate live cache entry: before=%+v after=%+v", beforeQuiesce, afterQuiesce)
	}
	if err := s.ReadSectors(1, buf2); !errors.Is(err, sdhci.ErrQuiesced) {
		t.Fatalf("ReadSectors while quiesced err=%v, want ErrQuiesced", err)
	}
	if err := s.WriteSectors(1, buf2); !errors.Is(err, sdhci.ErrQuiesced) {
		t.Fatalf("WriteSectors while quiesced err=%v, want ErrQuiesced", err)
	}
	if err := s.Flush(); !errors.Is(err, sdhci.ErrQuiesced) {
		t.Fatalf("Flush while quiesced err=%v, want ErrQuiesced", err)
	}

	// Mutate backing media while quiesced; Resume must serve fresh bytes, never stale cache.
	for i := 0; i < 512; i++ {
		c.image[512+i] = byte(0x50 + (i & 0x0f))
	}
	if err := s.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	buf3 := make([]byte, 512)
	if err := s.ReadSectors(1, buf3); err != nil {
		t.Fatalf("ReadSectors after Resume: %v", err)
	}
	if !bytes.Equal(buf3, c.image[512:1024]) {
		t.Fatal("ReadSectors after Resume returned stale cached data")
	}

	// Replacing cache and disabling cache work cleanly.
	if err := s.EnableCache(block.CacheConfig{Capacity: 8}); err != nil {
		t.Fatalf("re-EnableCache: %v", err)
	}
	s.DisableCache()
	if _, ok := s.CacheStats(); ok {
		t.Fatal("CacheStats still true after DisableCache")
	}
}

func TestSessionCacheWriteThroughAndRetiredRefusal(t *testing.T) {
	r, c := newRPC(t, "")
	c.image = make([]byte, 8192*512)
	probe, err := Open(r, c, Config{InputClockHz: 200_000_000})
	if err != nil {
		t.Fatalf("probe Open: %v", err)
	}
	id := probe.Identity()
	if err := probe.Quiesce(); err != nil {
		t.Fatalf("probe Quiesce: %v", err)
	}

	r2, c2 := newRPC(t, "")
	c2.image = make([]byte, 8192*512)
	s, err := Open(r2, c2, Config{InputClockHz: 200_000_000, WriteCard: id})
	if err != nil {
		t.Fatalf("writable Open: %v", err)
	}
	if err := s.EnableCache(block.CacheConfig{Capacity: 16}); err != nil {
		t.Fatalf("EnableCache: %v", err)
	}

	payload := bytes.Repeat([]byte{0x42}, 512)
	if err := s.WriteSectors(2, payload); err != nil {
		t.Fatalf("WriteSectors: %v", err)
	}
	if !bytes.Equal(c2.image[1024:1536], payload) {
		t.Fatal("write-through did not update backing card")
	}
	cmdsBefore := c2.commands
	got := make([]byte, 512)
	if err := s.ReadSectors(2, got); err != nil {
		t.Fatalf("ReadSectors after write: %v", err)
	}
	if !bytes.Equal(got, payload) || c2.commands != cmdsBefore {
		t.Fatalf("expected cached hit after write-through: cmds %d -> %d", cmdsBefore, c2.commands)
	}

	// Trigger a fatal write fault -> session retires and refuses cache ops.
	c2.failWrite = true
	if err := s.WriteSectors(3, payload); !errors.Is(err, ErrRetired) {
		t.Fatalf("fault WriteSectors err=%v, want ErrRetired", err)
	}
	if err := s.EnableCache(block.CacheConfig{Capacity: 8}); !errors.Is(err, ErrRetired) {
		t.Fatalf("EnableCache after retire err=%v, want ErrRetired", err)
	}
	if err := s.ReadSectors(2, got); !errors.Is(err, ErrRetired) {
		t.Fatalf("ReadSectors after retire err=%v, want ErrRetired", err)
	}
}
