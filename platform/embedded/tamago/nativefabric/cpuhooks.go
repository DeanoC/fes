// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"math"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

// CPUTransport provides ordered physical register access and a synchronous
// SMCCC call. A successful bus access cannot prove the mapping is correct.
type CPUTransport interface {
	ReadPhysical32(uint64) (uint32, error)
	WritePhysical32(uint64, uint32) error
	Invoke([7]uint64) ([4]uint64, error)
}

// PlatformServices owns memory, bridges and non-package idle identity. Its
// containment and reservation obligations are the same as Hooks.
type PlatformServices interface {
	BridgeServices
	Resolve(fabricmodel.Identity) (embeddedcontrol.Identity, error)
	Reserve([]byte) (*Region, error)
	Save(fabricmodel.Identity) (bool, error)
}

type CPUHooks struct {
	cpu      CPUTransport
	services PlatformServices
	base     uint64
	span     uint32
	bridge   *BridgeSnapshot
	access   bridgeAccess
	failed   bool
}

var errCPUHooksUnavailable = errors.New("native fabric: CPU hooks unavailable")

func (h *CPUHooks) ready() bool {
	return h != nil && !h.failed && h.cpu != nil && h.services != nil && h.base != 0 && h.span >= g.FesEmbeddedRegAckCookieHi+4
}

// NewCPUHooks deliberately supplies no Nano address or physical capability.
// The caller must qualify the entire aperture and firmware before enabling it.
func NewCPUHooks(cpu CPUTransport, services PlatformServices, base uint64, span uint32) (*CPUHooks, error) {
	if cpu == nil || services == nil || base == 0 || base%4 != 0 || span < g.FesEmbeddedRegAckCookieHi+4 || span > 256 || span%4 != 0 || base > math.MaxUint64-uint64(span-1) {
		return nil, errors.New("native fabric: invalid CPU control aperture")
	}
	return &CPUHooks{cpu: cpu, services: services, base: base, span: span}, nil
}

func (h *CPUHooks) address(offset uint32) (uint64, error) {
	if !h.ready() || offset%4 != 0 || offset > g.FesEmbeddedRegAckCookieHi || offset > h.span-4 {
		return 0, errors.New("native fabric: register outside control aperture")
	}
	return h.base + uint64(offset), nil
}
func (h *CPUHooks) Read32(offset uint32) (uint32, error) {
	address, err := h.address(offset)
	if err != nil {
		return 0, err
	}
	return h.cpu.ReadPhysical32(address)
}
func (h *CPUHooks) Write32(offset, value uint32) error {
	address, err := h.address(offset)
	if err != nil {
		return err
	}
	return h.cpu.WritePhysical32(address, value)
}
func (h *CPUHooks) Invoke(request [7]uint64) ([4]uint64, error) {
	if !h.ready() {
		return [4]uint64{}, errCPUHooksUnavailable
	}
	switch request[0] {
	case start, write, completed, isdone:
		if _, err := h.observeBridges(); err != nil {
			return [4]uint64{}, err
		}
		if h.access != bridgesClosed {
			return [4]uint64{}, h.bridgeFailure("configuration requires owned closed bridges")
		}
	}
	return h.cpu.Invoke(request)
}
func (h *CPUHooks) VerifyBootHandoff(p fabricmodel.PackageIdentity) error {
	if !h.ready() {
		return errCPUHooksUnavailable
	}
	verifier, ok := h.services.(BootHandoffVerifier)
	if !ok {
		return errors.New("native fabric: platform boot handoff unavailable")
	}
	return verifier.VerifyBootHandoff(p)
}
func (h *CPUHooks) Resolve(i fabricmodel.Identity) (embeddedcontrol.Identity, error) {
	if !h.ready() {
		return embeddedcontrol.Identity{}, errCPUHooksUnavailable
	}
	return h.services.Resolve(i)
}
func (h *CPUHooks) Reserve(data []byte) (*Region, error) {
	if !h.ready() {
		return nil, errCPUHooksUnavailable
	}
	return h.services.Reserve(data)
}
func (h *CPUHooks) Save(i fabricmodel.Identity) (bool, error) {
	if !h.ready() {
		return false, errCPUHooksUnavailable
	}
	return h.services.Save(i)
}
