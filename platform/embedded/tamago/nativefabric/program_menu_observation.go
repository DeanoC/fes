// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"encoding/hex"

	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// ProgramMenuObservation copies successful driver evidence. It is telemetry,
// never authority to resume storage, release memory or control a session.
type ProgramMenuObservation struct {
	Scope            string                      `json:"scope"`
	Event            string                      `json:"event"`
	Menu             uint64                      `json:"menu"`
	Identity         fabricmodel.PackageIdentity `json:"identity"`
	SourceSHA256     string                      `json:"source_sha256"`
	SourceGeneration uint64                      `json:"source_generation"`
	Opened           BridgeSnapshot              `json:"opened"`
	After            BridgeSnapshot              `json:"after"`
	Frames           uint64                      `json:"frames"`
	Frame            scanout.MenuFrame           `json:"frame"`
	MemorySHA256     string                      `json:"memory_sha256"`
	ControlSHA256    string                      `json:"control_sha256"`
	AccessSHA256     string                      `json:"access_sha256"`
}

func (p *programService) observeMenu(returned *MenuSessionReturn) error {
	m := p.menu
	if m.observe == nil {
		return nil
	}
	s := m.session
	observation := ProgramMenuObservation{Scope: "nano-program-menu-v1", Event: "frame", Menu: m.index,
		Identity: m.plan.Identity(), SourceSHA256: hex.EncodeToString(s.source.Digest[:]),
		SourceGeneration: s.source.Bridges.Generation, Opened: s.opened, After: s.opened,
		Frames: s.frames, Frame: s.last,
		MemorySHA256:  hex.EncodeToString(s.verifier.menuPublication.MapSHA256[:]),
		ControlSHA256: hex.EncodeToString(s.controlSHA[:]), AccessSHA256: hex.EncodeToString(s.accessSHA[:])}
	if returned != nil {
		observation.Event, observation.After = "return", returned.After
		observation.Frames, observation.Frame = returned.Frames, returned.LastFrame
		observation.MemorySHA256 = hex.EncodeToString(returned.Memory.MapSHA256[:])
		observation.ControlSHA256 = hex.EncodeToString(returned.ControlMapSHA256[:])
		observation.AccessSHA256 = hex.EncodeToString(returned.AccessMapSHA256[:])
	}
	return m.observe(observation)
}
