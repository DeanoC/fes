// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func TestConfigurationArenaReusesOnlyCompletedLease(t *testing.T) {
	memory := bytes.Repeat([]byte{0xcc}, 512)
	arena, err := NewConfigurationArena(0x94800000, memory, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	first, err := arena.Reserve(bytes.Repeat([]byte{0x12}, 128))
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(memory)
	if _, err := arena.Reserve([]byte{0x13}); err == nil || !bytes.Equal(memory, before) {
		t.Fatal("outstanding lease overwritten")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := arena.Reserve(bytes.Repeat([]byte{0x34}, 64))
	if err != nil || second == first || second.Address != first.Address || cap(second.Bytes) != 64 ||
		!bytes.Equal(second.Bytes, bytes.Repeat([]byte{0x34}, 64)) || !bytes.Equal(memory[64:], before[64:]) {
		t.Fatal("completed lease not reused within its pinned mapping", err)
	}
	if err := first.Release(); err == nil {
		t.Fatal("stale completion released new lease")
	}
	if _, err := arena.Reserve([]byte{0x56}); err == nil {
		t.Fatal("stale release made current memory available")
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err == nil {
		t.Fatal("completion replay accepted")
	}
	third, err := arena.Reserve(bytes.Repeat([]byte{0x56}, 256))
	if err != nil || len(third.Bytes) != 256 || !bytes.Equal(memory[256:], before[256:]) {
		t.Fatal(err)
	}
}

func TestConfigurationArenaRetainsFailedCopyAndCompletion(t *testing.T) {
	for _, mode := range []string{"before-copy", "copy-barrier", "copy-mutation", "release-barrier"} {
		t.Run(mode, func(t *testing.T) {
			memory := make([]byte, 256)
			calls := 0
			arena, err := NewConfigurationArena(0x94800000, memory, func() error {
				calls++
				if mode == "copy-mutation" && calls == 2 {
					memory[0] ^= 1
				}
				if mode == "before-copy" || mode == "copy-barrier" && calls == 2 || mode == "release-barrier" && calls == 3 {
					return errors.New("ordering unavailable")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			region, err := arena.Reserve(bytes.Repeat([]byte{0x12}, 128))
			if mode == "release-barrier" {
				if err != nil || region.Release() == nil {
					t.Fatal("uncertain completion accepted", err)
				}
			} else if err == nil || region != nil {
				t.Fatal("uncertain copy published", err)
			}
			if mode == "before-copy" {
				if !arena.failed || arena.active != nil || !bytes.Equal(memory, make([]byte, 256)) {
					t.Fatal("failed mapping guard wrote or exposed a lease")
				}
			} else if !arena.failed || arena.active == nil {
				t.Fatal("uncertain region not retained")
			}
			before := bytes.Clone(memory)
			oldCalls := calls
			if _, err := arena.Reserve([]byte{0x34}); err == nil || !bytes.Equal(memory, before) || calls != oldCalls {
				t.Fatal("failed arena reused")
			}
			if arena.active != nil {
				if err := arena.active.Release(); err == nil || calls != oldCalls {
					t.Fatal("failed completion replayed")
				}
			}
		})
	}
}

func TestConfigurationArenaSerializesCompetingReservationsAndCompletions(t *testing.T) {
	arena, err := NewConfigurationArena(0x94800000, make([]byte, 256), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	winners := make(chan *Region, 16)
	start := make(chan struct{})
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := arena.Reserve([]byte{0x12})
			if err == nil {
				winners <- r
			}
		}()
	}
	close(start)
	wg.Wait()
	close(winners)
	var region *Region
	count := 0
	for r := range winners {
		region = r
		count++
	}
	if count != 1 {
		t.Fatal("competing reservation count", count)
	}
	completed := make(chan bool, 16)
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); completed <- region.Release() == nil }()
	}
	wg.Wait()
	close(completed)
	count = 0
	for success := range completed {
		if success {
			count++
		}
	}
	if count != 1 {
		t.Fatal("competing completion count", count)
	}
	if _, err := arena.Reserve([]byte{0x34}); err != nil {
		t.Fatal(err)
	}
}
