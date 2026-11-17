// SPDX-License-Identifier: GPL-3.0-or-later
package modelbackend

import (
	"os"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

// Test-only transport to the C firmware service. No SiP ID or CPU mapping is
// installed by this adapter; the verifier must supply its private RPC process.
type firmwareBridgeActor struct {
	*sealedActorCPU
	wire *nativefabric.FirmwareBridges
}

func (a *firmwareBridgeActor) ObserveBridges() (nativefabric.BridgeSnapshot, error) {
	return a.wire.ObserveBridges()
}
func (a *firmwareBridgeActor) CloseBridges(r nativefabric.BridgeCloseRequest) (nativefabric.BridgeClosure, error) {
	return a.wire.CloseBridges(r)
}
func (a *firmwareBridgeActor) OpenBridges(r nativefabric.BridgeOpenRequest) (nativefabric.BridgeSnapshot, error) {
	return a.wire.OpenBridges(r)
}

func actorCPUHooks(t *testing.T, cpu *sealedActorCPU, dataMask uint32) (*nativefabric.CPUHooks, error) {
	t.Helper()
	if os.Getenv("FES_NANO_BRIDGE_RPC") == "" {
		return nativefabric.NewCPUHooks(cpu, cpu, cpu.base, 256, dataMask)
	}
	if _, err := cpu.call("bridge-boot f 1 2"); err != nil {
		return nil, err
	}
	wire, err := nativefabric.NewFirmwareBridges(cpu)
	if err != nil {
		return nil, err
	}
	service := &firmwareBridgeActor{sealedActorCPU: cpu, wire: wire}
	return nativefabric.NewCPUHooks(service, service, cpu.base, 256, dataMask)
}
