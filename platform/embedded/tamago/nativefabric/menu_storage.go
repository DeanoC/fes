// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"math"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// ResumeStorageAfterReturn resumes only the completed transfer's original
// store after this menu's independent secure closure. Failure consumes the
// attempt; neither framebuffer slot is released or repurposed.
func (s *MenuSession) ResumeStorageAfterReturn() error {
	if s == nil || s.self != s || s.verifier == nil || s.storage == nil {
		return scanout.ErrReaderArgument
	}
	if !s.active.CompareAndSwap(false, true) {
		return scanout.ErrReaderBusy
	}
	defer s.active.Store(false)
	if !s.closed || s.closeErr != nil || s.storageAttempted || s.returned.Frames == 0 ||
		s.returned.After.Generation == 0 || s.returned.After.Generation > math.MaxUint64-3 {
		return scanout.ErrReaderRetired
	}
	s.storageAttempted = true
	v := s.verifier
	if !v.readerMemoryMu.TryLock() {
		return scanout.ErrReaderBusy
	}
	defer v.readerMemoryMu.Unlock()
	before, err := v.Verify(v.Identity())
	if err != nil {
		return err
	}
	if before.Bridges != s.returned.After || before.Digest != s.source.Digest || !v.reconfigurationSource(before) {
		return errors.New("native menu: closure changed before storage return")
	}
	if err := s.checkArena(); err != nil {
		return err
	}
	after, err := v.Verify(v.Identity())
	if err != nil || after != before {
		return errors.New("native menu: storage return lost closed source ownership")
	}
	if err := s.storage.ResumeAfterReturn(after.Bridges.Owner, after.Bridges.Generation); err != nil {
		return err
	}
	s.storageResumed = true
	return nil
}

// NewStoredReconfigurationTransfer consumes this menu's return and freshly
// contained SD cycle once. The existing configuration plan checks original
// store identity, live ownership and the retained arena again before START.
func (s *MenuSession) NewStoredReconfigurationTransfer(plan *StoredConfigurationPlan, handoff *packagestore.Handoff, reserve func([]byte) (*Region, error)) (*ConfigurationTransfer, HPSBootObservation, error) {
	if s == nil || s.self != s || s.verifier == nil || plan == nil || plan.state == nil || plan.state.v != s.verifier || handoff == nil || reserve == nil {
		return nil, HPSBootObservation{}, scanout.ErrReaderArgument
	}
	if !s.active.CompareAndSwap(false, true) {
		return nil, HPSBootObservation{}, scanout.ErrReaderBusy
	}
	defer s.active.Store(false)
	if !s.closed || s.closeErr != nil || !s.storageResumed || s.nextAttempted || s.returned.Frames == 0 {
		return nil, HPSBootObservation{}, scanout.ErrReaderRetired
	}
	s.nextAttempted = true
	if !s.verifier.readerMemoryMu.TryLock() {
		return nil, HPSBootObservation{}, scanout.ErrReaderBusy
	}
	defer s.verifier.readerMemoryMu.Unlock()
	if err := s.checkArena(); err != nil {
		return nil, HPSBootObservation{}, err
	}
	return plan.newTransfer(handoff, reserve, true, nil, s)
}
