// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"errors"
	"sync"
)

// ConfigurationArena leases a platform-owned, uncached mapping that remains
// pinned until reboot. The platform supplies the mapping and ordering hook;
// this type establishes neither physical ownership nor bridge permission.
// Only a successful transfer's Release permits overwriting the previous lease.
type ConfigurationArena struct {
	mu      sync.Mutex
	address uint64
	memory  []byte
	barrier func() error
	active  *Region
	ready   bool
	failed  bool
}

func NewConfigurationArena(address uint64, memory []byte, barrier func() error) (*ConfigurationArena, error) {
	if address < 0x82000000 || address%64 != 0 || len(memory) == 0 || len(memory) > 8<<20 ||
		address > 0xc0000000-uint64(len(memory)) || barrier == nil {
		return nil, errors.New("native fabric: bounded pinned configuration arena required")
	}
	return &ConfigurationArena{address: address, memory: memory, barrier: barrier}, nil
}

// Reserve never overwrites an outstanding or uncertain lease. Completion
// leaves the mapping pinned; stale callbacks cannot release a later lease.
func (a *ConfigurationArena) Reserve(source []byte) (*Region, error) {
	if a == nil {
		return nil, errors.New("native fabric: configuration arena missing")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failed || (a.active != nil && !a.ready) || len(source) == 0 || len(source) > len(a.memory) {
		return nil, errors.New("native fabric: configuration arena lease unavailable")
	}
	owned := bytes.Clone(source)
	region := &Region{Address: a.address, Bytes: a.memory[:len(owned):len(owned)]}
	a.active, a.ready = region, false
	region.Release = func() error {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.failed || a.active != region || a.ready {
			return errors.New("native fabric: configuration lease completion stale or consumed")
		}
		if err := a.barrier(); err != nil {
			a.failed = true
			return err
		}
		a.ready = true
		return nil
	}
	copy(region.Bytes, owned)
	if err := a.barrier(); err != nil {
		a.failed = true
		return nil, err // The arena retains its active region and full mapping.
	}
	if !bytes.Equal(region.Bytes, owned) {
		a.failed = true
		return nil, errors.New("native fabric: staged configuration copy changed")
	}
	return region, nil
}
