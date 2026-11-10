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
	firmware *Client
	device   *sdhci.Device
	active   bool
	retired  atomic.Bool
}

var _ block.Device = (*Session)(nil)

// Config is supplied by the trusted platform, never by package metadata.
type Config struct {
	InputClockHz uint32
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
	if io == nil || config.InputClockHz < 400000 || config.InputClockHz > 200000000 {
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
	s.device, err = sdhci.Open(io, sdhci.Config{InputClockHz: config.InputClockHz, PrepareClock: firmware.Prepare, AllowWrites: writable, NoSocketWriteProtect: config.NoSocketWriteProtect})
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
	s.firmware.retire()
	return errors.Join(ErrRetired, err)
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
	err := s.device.ReadSectors(lba, dst)
	if err != nil && !errors.Is(err, sdhci.ErrQuiesced) && !errors.Is(err, block.ErrBounds) && !errors.Is(err, block.ErrAlignment) {
		return s.fail(err)
	}
	return err
}
func (s *Session) WriteSectors(lba uint64, src []byte) error {
	if s == nil || s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	err := s.device.WriteSectors(lba, src)
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
	err := s.device.Flush()
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
	if err := s.device.Quiesce(); err != nil {
		return s.fail(err)
	}
	if err := s.firmware.Stop(); err != nil {
		return s.fail(err)
	}
	s.active = false
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
	s.active = true
	return nil
}
