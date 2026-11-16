// SPDX-License-Identifier: GPL-3.0-or-later
// Package embeddedcontrol implements the relative FPGA control aperture used by
// the exploratory native runtime. It supplies no board addresses or MMIO.
package embeddedcontrol

import (
	"errors"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

var (
	ErrBusy     = errors.New("FPGA control snapshot is changing or reader is draining")
	ErrIdentity = errors.New("FPGA control identity mismatch")
	ErrFault    = errors.New("FPGA control fault or invalid state")
)

// IO calls must complete synchronously, preserve order and return a CSR error
// as an error. The platform provides the address, mappings and ordering.
type IO interface {
	Read32(uint32) (uint32, error)
	Write32(uint32, uint32) error
}

type Identity struct {
	ABI   uint32
	Build [4]uint32
}
type Snapshot struct {
	Identity                          Identity
	Generation, AckGeneration, Cookie uint64
	Status, Pending                   uint32
}
type Control struct{ IO IO }

func (c Control) read64(offset uint32) (uint64, error) {
	lo, err := c.IO.Read32(offset)
	if err != nil {
		return 0, err
	}
	hi, err := c.IO.Read32(offset + 4)
	if err != nil {
		return 0, err
	}
	return uint64(hi)<<32 | uint64(lo), nil
}
func (c Control) write64(offset uint32, v uint64) error {
	if err := c.IO.Write32(offset, uint32(v)); err != nil {
		return err
	}
	return c.IO.Write32(offset+4, uint32(v>>32))
}
func (c Control) readSnapshot() (s Snapshot, err error) {
	if c.IO == nil {
		return s, errors.New("FPGA control IO unavailable")
	}
	var magic, version uint32
	if magic, err = c.IO.Read32(g.FesEmbeddedRegMagic); err != nil {
		return
	}
	if version, err = c.IO.Read32(g.FesEmbeddedRegVersion); err != nil {
		return
	}
	if magic != g.FesEmbeddedMagic || version != g.FesEmbeddedVersion {
		err = ErrIdentity
		return
	}
	if s.Identity.ABI, err = c.IO.Read32(g.FesEmbeddedRegAbi); err != nil {
		return
	}
	for i := range s.Identity.Build {
		if s.Identity.Build[i], err = c.IO.Read32(g.FesEmbeddedRegBuild0 + uint32(i)*4); err != nil {
			return
		}
	}
	if s.Generation, err = c.read64(g.FesEmbeddedRegGenerationLo); err != nil {
		return
	}
	if s.AckGeneration, err = c.read64(g.FesEmbeddedRegAckGenerationLo); err != nil {
		return
	}
	if s.Cookie, err = c.read64(g.FesEmbeddedRegAckCookieLo); err != nil {
		return
	}
	if s.Status, err = c.IO.Read32(g.FesEmbeddedRegStatus); err != nil {
		return
	}
	s.Pending, err = c.IO.Read32(g.FesEmbeddedRegPending)
	return
}
func (c Control) Snapshot() (Snapshot, error) {
	a, err := c.readSnapshot()
	if err != nil {
		return Snapshot{}, err
	}
	b, err := c.readSnapshot()
	if err != nil {
		return Snapshot{}, err
	}
	if a != b {
		return Snapshot{}, ErrBusy
	}
	return b, nil
}
func (c Control) Verify(want Identity) error {
	s, err := c.Snapshot()
	if err != nil {
		return err
	}
	if s.Identity != want {
		return ErrIdentity
	}
	if s.Status != 0 || s.Pending != 0 || s.Generation != 0 || s.AckGeneration != 0 || s.Cookie != 0 {
		return ErrFault
	}
	return nil
}
func (c Control) Disable(want Identity, generation, cookie uint64) error {
	if generation == 0 || cookie == 0 {
		return ErrFault
	}
	s, err := c.Snapshot()
	if err != nil {
		return err
	}
	if s.Identity != want || s.Generation != generation {
		return ErrIdentity
	}
	if s.Status != g.FesEmbeddedStatusEnabled {
		return ErrFault
	}
	if err = c.write64(g.FesEmbeddedRegRequestGenerationLo, generation); err != nil {
		return err
	}
	if err = c.write64(g.FesEmbeddedRegRequestCookieLo, cookie); err != nil {
		return err
	}
	return c.IO.Write32(g.FesEmbeddedRegCommand, g.FesEmbeddedCommandDisable)
}
func (c Control) Drained(want Identity, generation, cookie uint64) error {
	s, err := c.Snapshot()
	if err != nil {
		return err
	}
	if s.Identity != want || s.Generation != generation {
		return ErrIdentity
	}
	if s.Status&g.FesEmbeddedStatusFault != 0 {
		return ErrFault
	}
	if s.Status == g.FesEmbeddedStatusDraining {
		return ErrBusy
	}
	if s.Status != g.FesEmbeddedStatusDrained || s.Pending != 0 || s.AckGeneration != generation || s.Cookie != cookie {
		return ErrFault
	}
	return nil
}
func (c Control) Enable(want Identity, generation uint64) error {
	if generation == 0 {
		return ErrFault
	}
	if err := c.write64(g.FesEmbeddedRegRequestGenerationLo, generation); err != nil {
		return err
	}
	if err := c.IO.Write32(g.FesEmbeddedRegCommand, g.FesEmbeddedCommandEnable); err != nil {
		return err
	}
	s, err := c.Snapshot()
	if err != nil {
		return err
	}
	if s.Identity != want || s.Generation != generation {
		return ErrIdentity
	}
	if s.Status != g.FesEmbeddedStatusEnabled {
		return ErrFault
	}
	return nil
}
