//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

// FabricCPUTransport is an unconfigured access primitive. No board aperture,
// firmware capability, DDR reservation or containment is implied. It is not
// constructed by the Nano boot path. Bus faults and a stuck SMC cannot be
// recovered by this transport; only a qualified platform may call it.
type FabricCPUTransport struct{}

func (FabricCPUTransport) ReadPhysical32(address uint64) (uint32, error) {
	if address == 0 || address%4 != 0 || uint64(uintptr(address)) != address {
		return 0, errors.New("DE25: invalid physical register address")
	}
	return arm64map.WithStableMapping(func() (arm64map.Observation, error) { return deviceMapping(address) },
		func() uint32 { return load32(uintptr(address)) })
}
func (FabricCPUTransport) WritePhysical32(address uint64, value uint32) error {
	if address == 0 || address%4 != 0 || uint64(uintptr(address)) != address {
		return errors.New("DE25: invalid physical register address")
	}
	_, err := arm64map.WithStableMapping(func() (arm64map.Observation, error) { return deviceMapping(address) },
		func() struct{} { store32(uintptr(address), value); return struct{}{} })
	return err
}

func deviceMapping(address uint64) (arm64map.Observation, error) {
	return arm64map.Verify(profileRAMStart, profileRAMSize, []arm64map.Range{{Base: address, Bytes: 4, Kind: arm64map.Device}})
}
func (FabricCPUTransport) Invoke(request [7]uint64) ([4]uint64, error) {
	switch request[0] {
	case 0xc2000300:
		return smc(request), nil
	case 0xc2000001, 0x42000002, 0xc2000003, 0xc2000004:
		return arm64map.WithStableMapping(configurationMapping, func() [4]uint64 { return smc(request) })
	case uint64(g.FesEmbeddedSdCall):
		if request[3] == uint64(g.FesEmbeddedSdCookie) && request[5] == 0 && request[6] == 0 {
			return smc(request), nil
		}
		return [4]uint64{}, errors.New("DE25: owned SD call lacks its explicit cookie")
	case uint64(g.FesEmbeddedBridgeCall):
		if request[3] == uint64(g.FesEmbeddedBridgeCookie) && request[5] == 0 && request[6] == 0 {
			return smc(request), nil
		}
		return [4]uint64{}, errors.New("DE25: owned bridge call lacks its explicit cookie")
	default:
		return [4]uint64{}, errors.New("DE25: firmware call outside fabric protocol")
	}
}

var _ nativefabric.CPUTransport = FabricCPUTransport{}
