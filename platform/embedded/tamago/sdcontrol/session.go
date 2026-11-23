// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

// Session binds the actual PIO driver to one private firmware client.
// The caller must exclusively own both controller and secure-call transport.
// Concurrent emulation-core/SD access is outside this closed-bridge contract.
type Session struct {
	control  sync.Mutex
	ioMu     sync.RWMutex
	firmware *Client
	device   *sdhci.Device
	cache    *block.Cache
	active   bool
	retired  atomic.Bool
}

var _ block.Device = (*Session)(nil)

// Config is supplied by the trusted platform, never by package metadata.
type Config struct {
	InputClockHz uint32
	// TransferClockHz is the trusted platform's opt-in post-discovery rate.
	// Zero preserves identification speed; 25 MHz requires card/board support.
	TransferClockHz uint32
	// WriteCard authorizes writes only to this independently expected CID and
	// capacity. Zero selects read-only access. Physical write-protect and
	// exclusive writable-media ownership still need platform qualification.
	WriteCard sdhci.Identity
	// NoSocketWriteProtect is a trusted board-wiring property, not package
	// policy or write permission. An expected WriteCard is still mandatory.
	NoSocketWriteProtect bool
}

// Open grants the lease before the first register access. It returns a retired
// non-nil Session on failures after the grant attempt so the caller can retain
// the last receipt. It never attempts an automatic firmware stop after failure.
// InputClockHz must match the qualified platform's actual controller input.
func Open(cpu Caller, io sdhci.Registers, config Config) (*Session, error) {
	if io == nil || config.InputClockHz < 400000 || config.InputClockHz > 200000000 ||
		(config.TransferClockHz != 0 && config.TransferClockHz != 25000000) ||
		(config.TransferClockHz != 0 && config.InputClockHz < config.TransferClockHz) {
		return nil, fmt.Errorf("sdcontrol: invalid controller or input clock")
	}
	writable := config.WriteCard != (sdhci.Identity{})
	if writable {
		var nonzero, notFF byte
		for _, b := range config.WriteCard.CID {
			nonzero |= b
			notFF |= ^b
		}
		// This driver supports CSD v2 SDHC/SDXC geometry only.
		if nonzero == 0 || notFF == 0 || config.WriteCard.Sectors < 1024 ||
			config.WriteCard.Sectors > 1<<32 || config.WriteCard.Sectors%1024 != 0 {
			return nil, fmt.Errorf("sdcontrol: invalid expected writable card")
		}
	}
	firmware, err := NewClient(cpu)
	if err != nil {
		return nil, err
	}
	s := &Session{firmware: firmware}
	if err = firmware.Resume(); err != nil {
		return s, s.fail(err)
	}
	s.active = true
	s.device, err = sdhci.Open(io, sdhci.Config{InputClockHz: config.InputClockHz, TransferClockHz: config.TransferClockHz, PrepareClock: firmware.Prepare, AllowWrites: writable, NoSocketWriteProtect: config.NoSocketWriteProtect})
	if err != nil {
		return s, s.fail(err)
	}
	if writable && s.device.Identity() != config.WriteCard {
		return s, s.fail(fmt.Errorf("sdcontrol: writable card identity mismatch"))
	}
	return s, nil
}
func (s *Session) fail(err error) error {
	s.retired.Store(true)
	s.ioMu.Lock()
	s.active = false
	if s.cache != nil {
		s.cache.Invalidate()
	}
	s.ioMu.Unlock()
	if s.device != nil {
		s.device.DisableIRQ()
	}
	s.firmware.retire()
	return errors.Join(ErrRetired, err)
}

// EnableCache installs a bounded write-through sector cache on this session.
// The cache is invalidated automatically on every Quiesce, Resume, and fatal
// session retirement, and cached reads are refused whenever the lease is
// quiesced or retired.
func (s *Session) EnableCache(cfg block.CacheConfig) error {
	if s == nil {
		return ErrRetired
	}
	s.control.Lock()
	defer s.control.Unlock()
	if s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	c, err := block.NewCache(s.device, cfg)
	if err != nil {
		return err
	}
	s.ioMu.Lock()
	if s.cache != nil {
		s.cache.Invalidate()
	}
	s.cache = c
	s.ioMu.Unlock()
	return nil
}

// DisableCache invalidates and detaches any active sector cache on this session.
func (s *Session) DisableCache() {
	if s == nil {
		return
	}
	s.control.Lock()
	defer s.control.Unlock()
	s.ioMu.Lock()
	if s.cache != nil {
		s.cache.Invalidate()
		s.cache = nil
	}
	s.ioMu.Unlock()
}

// CacheStats returns the active sector cache statistics and true when caching
// is enabled, or zero statistics and false otherwise.
func (s *Session) CacheStats() (block.CacheStats, bool) {
	if s == nil {
		return block.CacheStats{}, false
	}
	s.ioMu.RLock()
	defer s.ioMu.RUnlock()
	if s.cache == nil {
		return block.CacheStats{}, false
	}
	return s.cache.Stats(), true
}
func (s *Session) LastReceipt() (Receipt, bool) {
	if s == nil {
		return Receipt{}, false
	}
	receipt, valid := s.firmware.LastReceipt()
	return receipt, valid && !s.retired.Load()
}
func (s *Session) Identity() sdhci.Identity {
	if s == nil || s.device == nil {
		return sdhci.Identity{}
	}
	return s.device.Identity()
}
func (s *Session) SectorCount() uint64 { return s.Identity().Sectors }
func (s *Session) ReadSectors(lba uint64, dst []byte) error {
	if s == nil || s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	s.ioMu.RLock()
	if s.retired.Load() {
		s.ioMu.RUnlock()
		return ErrRetired
	}
	if !s.active {
		s.ioMu.RUnlock()
		return sdhci.ErrQuiesced
	}
	var err error
	if s.cache != nil {
		err = s.cache.ReadSectors(lba, dst)
	} else {
		err = s.device.ReadSectors(lba, dst)
	}
	s.ioMu.RUnlock()
	if err != nil && !errors.Is(err, sdhci.ErrQuiesced) && !errors.Is(err, block.ErrBounds) && !errors.Is(err, block.ErrAlignment) {
		return s.fail(err)
	}
	return err
}
func (s *Session) WriteSectors(lba uint64, src []byte) error {
	if s == nil || s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	s.ioMu.RLock()
	if s.retired.Load() {
		s.ioMu.RUnlock()
		return ErrRetired
	}
	if !s.active {
		s.ioMu.RUnlock()
		return sdhci.ErrQuiesced
	}
	var err error
	if s.cache != nil {
		err = s.cache.WriteSectors(lba, src)
	} else {
		err = s.device.WriteSectors(lba, src)
	}
	s.ioMu.RUnlock()
	if err != nil && !errors.Is(err, sdhci.ErrQuiesced) && !errors.Is(err, block.ErrReadOnly) &&
		!errors.Is(err, block.ErrBounds) && !errors.Is(err, block.ErrAlignment) {
		return s.fail(err)
	}
	return err
}
func (s *Session) Flush() error {
	if s == nil || s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	s.ioMu.RLock()
	if s.retired.Load() {
		s.ioMu.RUnlock()
		return ErrRetired
	}
	if !s.active {
		s.ioMu.RUnlock()
		return sdhci.ErrQuiesced
	}
	var err error
	if s.cache != nil {
		err = s.cache.Flush()
	} else {
		err = s.device.Flush()
	}
	s.ioMu.RUnlock()
	if err != nil && !errors.Is(err, sdhci.ErrQuiesced) {
		return s.fail(err)
	}
	return err
}

// Quiesce fences the PIO driver before asking firmware to independently drain
// and contain SD. A success receipt supplies the new shared bridge generation.
func (s *Session) Quiesce() error {
	if s == nil {
		return ErrRetired
	}
	s.control.Lock()
	defer s.control.Unlock()
	if s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	if !s.active {
		return sdhci.ErrQuiesced
	}
	// Fence and drain in-flight cached/uncached I/O and invalidate the cache
	// before quiescing hardware so no stale sector can survive containment.
	s.ioMu.Lock()
	s.active = false
	if s.cache != nil {
		s.cache.Invalidate()
	}
	s.ioMu.Unlock()
	if err := s.device.Quiesce(); err != nil {
		return s.fail(err)
	}
	if err := s.firmware.Stop(); err != nil {
		return s.fail(err)
	}
	return nil
}

// Resume obtains a new lease, prepares the clock at the host-reset boundary and
// rediscovers the same card before allowing access to the retained filesystem.
func (s *Session) Resume() error { return s.resume(0, 0, false) }

// ResumeAfterReturn accepts an explicit expected closed fabric return. Secure
// firmware independently checks that provenance before granting SD access;
// controller/card reinitialization still requires the original CID/capacity.
func (s *Session) ResumeAfterReturn(owner, generation uint64) error {
	return s.resume(owner, generation, true)
}

func (s *Session) resume(owner, generation uint64, returned bool) error {
	if s == nil {
		return ErrRetired
	}
	s.control.Lock()
	defer s.control.Unlock()
	if s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	if s.active {
		return ErrTransition
	}
	var err error
	if returned {
		last, valid := s.firmware.LastReceipt()
		if !valid {
			return s.fail(ErrRetired)
		}
		if owner != last.Owner || generation <= last.Generation || generation > ^uint64(0)-3 {
			return ErrTransition
		}
		err = s.firmware.ResumeAfterReturn(owner, generation)
	} else {
		err = s.firmware.Resume()
	}
	if err != nil {
		return s.fail(err)
	}
	if err := s.device.Resume(); err != nil {
		return s.fail(err)
	}
	s.ioMu.Lock()
	if s.cache != nil {
		s.cache.Invalidate()
	}
	s.active = true
	s.ioMu.Unlock()
	return nil
}
