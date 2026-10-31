// SPDX-License-Identifier: GPL-3.0-or-later
package modelbackend

import (
	"errors"
	"os"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

// Test-only transport to the C firmware service. No SiP ID or CPU mapping is
// installed by this adapter; the verifier must supply its private RPC process.
type firmwareBridgeActor struct{ *sealedActorCPU }

func actorNanoSnapshot(words []uint64) (nativefabric.BridgeSnapshot, error) {
	if len(words) != 6 || words[2] > 15 || words[3] > 2 || words[4] > 1 || words[5] > 1 {
		return nativefabric.BridgeSnapshot{}, errors.New("invalid C bridge snapshot")
	}
	return nativefabric.BridgeSnapshot{Capabilities: nativefabric.BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: 12, FabricHandshake: nativefabric.FabricHandshake(words[3])},
		Owner: words[0], Generation: words[1], ResetMask: uint32(words[2]), NeverEnabled: words[4] != 0, MastersContained: words[5] != 0}, nil
}
func (a *firmwareBridgeActor) ObserveBridges() (nativefabric.BridgeSnapshot, error) {
	w, err := a.call("bridge-observe")
	if err != nil {
		return nativefabric.BridgeSnapshot{}, err
	}
	return actorNanoSnapshot(w)
}
func (a *firmwareBridgeActor) CloseBridges(r nativefabric.BridgeCloseRequest) (nativefabric.BridgeClosure, error) {
	w, err := a.call("bridge-close %x %x %x %x", r.Before.Owner, r.Before.Generation, actorNanoFlag(r.RequireFabricHandshake), actorNanoFlag(r.PreserveDataContainment))
	if err != nil {
		return nativefabric.BridgeClosure{}, err
	}
	if len(w) != 10 {
		return nativefabric.BridgeClosure{}, errors.New("invalid C bridge closure")
	}
	s, err := actorNanoSnapshot(w[:6])
	return nativefabric.BridgeClosure{After: s, ProducersQuiescent: w[6] == 1, FabricAcknowledged: w[7] == 1, HardwareDrainedMask: uint32(w[8]), DataRemainedContained: w[9] == 1}, err
}
func (a *firmwareBridgeActor) OpenBridges(r nativefabric.BridgeOpenRequest) (nativefabric.BridgeSnapshot, error) {
	w, err := a.call("bridge-open %x %x %x", r.Before.Owner, r.Before.Generation, r.Mask)
	if err != nil {
		return nativefabric.BridgeSnapshot{}, err
	}
	return actorNanoSnapshot(w)
}
func actorNanoFlag(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func actorCPUHooks(t *testing.T, cpu *sealedActorCPU) (*nativefabric.CPUHooks, error) {
	t.Helper()
	if os.Getenv("FES_NANO_BRIDGE_RPC") == "" {
		return nativefabric.NewCPUHooks(cpu, cpu, cpu.base, 256)
	}
	if _, err := cpu.call("bridge-boot f 1 2"); err != nil {
		return nil, err
	}
	service := &firmwareBridgeActor{cpu}
	return nativefabric.NewCPUHooks(service, service, cpu.base, 256)
}
