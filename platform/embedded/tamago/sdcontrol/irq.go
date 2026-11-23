// SPDX-License-Identifier: GPL-3.0-or-later

package sdcontrol

import (
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

// EnableIRQ binds the owned SDHCI controller to a GICv3 SPI (for example
// de25nano.IRQSDMMC = 128 on Agilex 5) while this session is active. Quiesce
// automatically suspends the SPI before firmware containment (zero MMIO while
// quiesced) and Resume/ResumeAfterReturn re-enables it after card rediscovery.
func (s *Session) EnableIRQ(g *gic.Controller, irqID uint32) error {
	if s == nil {
		return ErrRetired
	}
	s.control.Lock()
	defer s.control.Unlock()
	s.ioMu.RLock()
	defer s.ioMu.RUnlock()
	if s.firmware == nil || s.device == nil || s.retired.Load() {
		return ErrRetired
	}
	if !s.active.Load() {
		return sdhci.ErrQuiesced
	}
	return s.device.EnableIRQ(g, irqID)
}

// DisableIRQ masks and unregisters any GICv3 SPI bound to this session.
// Calling DisableIRQ while quiesced or retired unregisters the SPI with zero
// MMIO accesses.
func (s *Session) DisableIRQ() {
	if s == nil {
		return
	}
	s.control.Lock()
	defer s.control.Unlock()
	if s.device != nil {
		s.device.DisableIRQ()
	}
}

// IRQCount returns the number of SDHCI interrupts handled since EnableIRQ.
func (s *Session) IRQCount() uint64 {
	if s == nil || s.device == nil {
		return 0
	}
	return s.device.IRQCount()
}
