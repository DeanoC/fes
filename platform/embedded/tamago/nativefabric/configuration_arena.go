// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
)

// ConfigurationArena leases a platform-owned, uncached mapping that remains
// pinned until reboot. The platform supplies the mapping and ordering hook;
// this type establishes neither physical ownership nor bridge permission.
// A retained program additionally needs secure source release before overwrite.
type ConfigurationArena struct {
	mu                        sync.Mutex
	address                   uint64
	memory                    []byte
	metadata                  []byte
	barrier                   func() error
	active                    *Region
	ready                     bool
	failed                    bool
	retained                  bool
	tapeOwner, tapeGeneration uint64
	tapeRegion                *Region
}

// NewProgramArena reserves the final page for the fixed secure descriptor.
// The complete mapping stays pinned. This supplies no physical ownership or
// programming authority; the platform must qualify the entire mapping.
func NewProgramArena(address uint64, memory []byte, barrier func() error) (*ConfigurationArena, error) {
	if len(memory) <= ProgramMetadataBytes {
		return nil, errors.New("native fabric: program arena lacks its metadata page")
	}
	a, err := NewConfigurationArena(address, memory, barrier)
	if err != nil {
		return nil, err
	}
	limit := len(memory) - ProgramMetadataBytes
	a.memory, a.metadata = memory[:limit:limit], memory[limit:]
	return a, nil
}

// NewRetainedProgramArena keeps completed source and metadata pinned. Reserve
// and initial-tape publication cannot overwrite them. Only the native owner can
// request the next reservation through a successful secure source-release call.
func NewRetainedProgramArena(address uint64, memory []byte, barrier func() error) (*ConfigurationArena, error) {
	a, err := NewProgramArena(address, memory, barrier)
	if err != nil {
		return nil, err
	}
	a.retained = true
	return a, nil
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
	return a.reserveLocked(source, false)
}

// reclaimed is private to the exact old region's locked successor callback;
// neither a public Region copy nor a firmware observation supplies this grant.
func (a *ConfigurationArena) reserveLocked(source []byte, reclaimed bool) (*Region, error) {
	if a.failed || (a.retained && a.active != nil && !reclaimed) || (a.active != nil && !a.ready) || len(source) == 0 || len(source) > len(a.memory) {
		return nil, errors.New("native fabric: configuration arena lease unavailable")
	}
	// The board's ordering hook also validates its live mapping. Reject a
	// changed resource before the first write to a new or completed lease.
	if err := a.barrier(); err != nil {
		a.failed = true
		return nil, err
	}
	owned := bytes.Clone(source)
	region := &Region{Address: a.address, Bytes: a.memory[:len(owned):len(owned)]}
	a.active, a.ready = region, false
	published := false
	var header []byte
	var rom, romExtension []byte
	var romOffset int
	romUnchanged := func() bool {
		return rom == nil || bytes.Equal(a.memory[romOffset:romOffset+len(rom)], rom)
	}
	if a.metadata != nil {
		region.StageROM = func(image []byte) error {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.failed || a.active != region || a.ready || published || rom != nil {
				return errors.New("native fabric: ROM staging stale or consumed")
			}
			if len(image) != 4096 && len(image) != 8192 {
				return errors.New("native fabric: ROM must be exactly 4/8 KiB")
			}
			image = bytes.Clone(image)
			ext, offset, err := programROMExtension(len(owned), image)
			if err != nil || offset > len(a.memory)-len(image) {
				return errors.New("native fabric: ROM exceeds the pinned program arena")
			}
			if err := a.barrier(); err != nil {
				a.failed = true
				return err
			}
			rom, romExtension, romOffset = image, ext, offset
			clear(a.memory[len(owned):romOffset])
			copy(a.memory[romOffset:romOffset+len(rom)], rom)
			if err := a.barrier(); err != nil {
				a.failed = true
				return err
			}
			if !romUnchanged() {
				a.failed = true
				return errors.New("native fabric: staged ROM copy changed")
			}
			return nil
		}
		region.PublishConfiguration = func(raw []byte) error {
			a.mu.Lock()
			defer a.mu.Unlock()
			want := ProgramHeaderBytes
			roleMode := len(raw) >= 8 && binary.LittleEndian.Uint32(raw[4:]) == programRoleVersion
			if rom != nil || roleMode {
				want += programROMHeaderBytes
			}
			if a.failed || a.active != region || a.ready || published || len(raw) != want {
				return errors.New("native fabric: configuration publication stale, consumed or invalid")
			}
			if roleMode {
				role := programRole(binary.LittleEndian.Uint32(raw[programRoleOffset:]))
				if role.controlABI() == 0 || binary.LittleEndian.Uint32(raw[programRoleOffset+4:]) != role.controlABI() ||
					binary.LittleEndian.Uint32(raw[12:]) != uint32(len(owned)) ||
					role == programRoleMenu && (rom != nil || !bytes.Equal(raw[ProgramHeaderBytes:], make([]byte, programROMHeaderBytes))) ||
					role == programRoleComputer && (rom == nil || !bytes.Equal(raw[ProgramHeaderBytes:], romExtension)) {
					a.failed = true
					return errors.New("native fabric: program role differs from privately staged resources")
				}
			}
			if rom != nil && (!roleMode && binary.LittleEndian.Uint32(raw[4:]) != 2 ||
				binary.LittleEndian.Uint32(raw[12:]) != uint32(len(owned)) ||
				!bytes.Equal(raw[ProgramHeaderBytes:], romExtension)) {
				a.failed = true
				return errors.New("native fabric: descriptor differs from privately staged ROM")
			}
			published = true
			header = bytes.Clone(raw)
			if err := a.barrier(); err != nil {
				a.failed = true
				return err
			}
			if !romUnchanged() {
				a.failed = true
				return errors.New("native fabric: ROM changed before publication")
			}
			copy(a.metadata, header)
			if err := a.barrier(); err != nil {
				a.failed = true
				return err
			}
			if !bytes.Equal(a.metadata[:len(header)], header) || !romUnchanged() {
				a.failed = true
				return errors.New("native fabric: configuration publication changed")
			}
			return nil
		}
	}
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
		if a.metadata != nil && (!published || !bytes.Equal(a.metadata[:len(header)], header) || !romUnchanged()) {
			a.failed = true
			return errors.New("native fabric: configuration metadata missing or changed at completion")
		}
		a.ready = true
		return nil
	}
	if a.retained {
		attempted := false
		region.reserveNext = func(next []byte, release func() error) (*Region, error) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.failed || a.active != region || !a.ready || attempted || release == nil || len(next) == 0 || len(next) > len(a.memory) {
				return nil, errors.New("native fabric: retained source release stale or unavailable")
			}
			attempted = true
			// Clone before calling trusted firmware: callers cannot change which
			// bytes this atomic successor reservation will copy during the call.
			next = bytes.Clone(next)
			unchanged := func() bool {
				return a.active == region && a.ready && bytes.Equal(region.Bytes, owned) &&
					(a.metadata == nil || published && bytes.Equal(a.metadata[:len(header)], header) && romUnchanged())
			}
			if err := a.barrier(); err != nil {
				a.failed = true
				return nil, err
			}
			if !unchanged() {
				a.failed = true
				return nil, errors.New("native fabric: retained source changed before secure release")
			}
			if err := release(); err != nil {
				a.failed = true
				return nil, err
			}
			if err := a.barrier(); err != nil {
				a.failed = true
				return nil, err
			}
			if !unchanged() {
				a.failed = true
				return nil, errors.New("native fabric: retained source changed during secure release")
			}
			return a.reserveLocked(next, true)
		}
	}
	clear(a.metadata)
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
