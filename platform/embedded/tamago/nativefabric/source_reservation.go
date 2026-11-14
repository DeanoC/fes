// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// Called only after newTransfer has checked the actual returned session and
// newly stopped storage handoff. The arena keeps validation, secure release
// and next-source copying under its own lock; no reusable host receipt escapes.
func (v *HPSContextVerifier) reserveStoredSource(payload []byte, source HPSBootObservation, rearm bool, reserve func([]byte) (*Region, error)) (*Region, error) {
	v.configurationMu.Lock()
	previous := v.completedConfiguration
	if !rearm || previous == nil || previous.binding == nil || previous.binding.sourceRegion == nil || previous.binding.sourceRegion.reserveNext == nil {
		v.configurationMu.Unlock()
		return reserve(payload)
	}
	defer v.configurationMu.Unlock()
	b := previous.binding
	if !previous.done || previous.err != nil || previous.phase != "complete" || previous.region != nil ||
		len(previous.pending) != 0 || previous.accepted != len(previous.source) || previous.offset != len(previous.source) ||
		b.verifier != v || b.source.Digest != source.Digest || b.source.Bridges.Owner != source.Bridges.Owner ||
		b.source.Bridges.Generation >= source.Bridges.Generation || !v.reconfigurationSource(source) {
		return nil, errors.New("native fabric: retained predecessor has no completed closed source")
	}
	region, err := b.sourceRegion.reserveNext(payload, func() error {
		before, err := v.Verify(v.Identity())
		if err != nil || before != source {
			return errors.New("native fabric: source release lost stopped storage ownership")
		}
		r, err := v.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeReleaseSource),
			source.Bridges.Owner, uint64(g.FesEmbeddedBridgeCookie), source.Bridges.Generation})
		if err != nil {
			return err
		}
		if r != ([4]uint64{0, uint64(g.FesEmbeddedBridgeSourceReleaseTag) << 32, source.Bridges.Owner, source.Bridges.Generation}) {
			return errors.New("native fabric: secure source release rejected or incomplete")
		}
		after, err := v.Verify(v.Identity())
		if err != nil || after != before {
			return errors.New("native fabric: secure source release changed closed ownership")
		}
		return nil
	})
	if err == nil {
		v.completedConfiguration = nil
	}
	return region, err
}
